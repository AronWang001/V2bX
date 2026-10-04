package hy2

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/apernet/hysteria/core/v2/client"
	"github.com/apernet/hysteria/core/v2/server"
	"github.com/juju/ratelimit"
	"go.uber.org/zap"
)

func startIntegrationServer(t *testing.T, nodeSpeed, userSpeed int) (client.Client, *HookServer, *limiter.Limiter, *ratelimit.Bucket) {
	t.Helper()
	user := panel.UserInfo{Id: 14, Uuid: "integration-user", SpeedLimit: userSpeed}
	h, l := newTrafficFixture(t, nodeSpeed, user)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"limiter.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udp.Close() })
	srv, err := server.NewServer(&server.Config{Conn: udp, TLSConfig: server.TLSConfig{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}}}, Authenticator: &V2bX{usersMap: map[string]int{user.Uuid: user.Id}}, EventLogger: &serverLogger{Tag: h.Tag, logger: zap.NewNop()}, TrafficLogger: h, IgnoreClientBandwidth: true})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		srv.Serve()
	}()
	t.Cleanup(func() {
		srv.Close()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("local Hysteria2 server did not stop")
		}
	})
	c, _, err := client.NewClient(&client.Config{ServerAddr: udp.LocalAddr(), Auth: user.Uuid, TLSConfig: client.TLSConfig{ServerName: "limiter.test", RootCAs: roots}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, ok := l.SpeedLimiter.Load(format.UserTag(h.Tag, user.Uuid)); ok {
			return c, h, l, v.(*ratelimit.Bucket)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("authenticated connection did not register its bucket")
	return nil, nil, nil, nil
}

type zeroPayload struct{}

func (zeroPayload) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestHysteria2TCPDownloadSpeed(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		nodeSpeed, userSpeed, streams int
		size                          int64
		min, max                      float64
	}{
		{"node5_user60", 5, 60, 1, 3125000, 4, 6},
		{"node0_user60", 0, 60, 1, 30000000, 55, 65},
		{"node5_two_streams", 5, 60, 2, 3125000, 4, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, bucket := startIntegrationServer(t, tc.nodeSpeed, tc.userSpeed)
			origin, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { origin.Close() })
			startSend := make(chan struct{})
			originDone := make(chan error, tc.streams)
			go func() {
				for i := 0; i < tc.streams; i++ {
					conn, err := origin.Accept()
					if err != nil {
						originDone <- err
						continue
					}
					go func() {
						defer conn.Close()
						<-startSend
						_, err := io.CopyN(conn, zeroPayload{}, tc.size/int64(tc.streams))
						originDone <- err
					}()
				}
			}()
			connections := make([]net.Conn, tc.streams)
			for i := range connections {
				connections[i], err = c.TCP(origin.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer connections[i].Close()
				connections[i].SetDeadline(time.Now().Add(15 * time.Second))
			}
			// Exclude the normal one-second startup burst from the sustained-rate check.
			bucket.TakeAvailable(bucket.Capacity())
			start := time.Now()
			close(startSend)
			var wg sync.WaitGroup
			readErrors := make(chan error, tc.streams)
			for _, conn := range connections {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := io.CopyN(io.Discard, conn, tc.size/int64(tc.streams))
					readErrors <- err
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)
			mbps := float64(tc.size) * 8 / elapsed.Seconds() / 1e6
			for i := 0; i < tc.streams; i++ {
				if err := <-readErrors; err != nil {
					t.Fatal(err)
				}
				if err := <-originDone; err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("node=%d user=%d streams=%d bytes=%d elapsed=%s throughput=%.3fMbps", tc.nodeSpeed, tc.userSpeed, tc.streams, tc.size, elapsed, mbps)
			if mbps < tc.min || mbps > tc.max {
				t.Fatalf("throughput %.3fMbps outside %.0f..%.0f", mbps, tc.min, tc.max)
			}
		})
	}
}

func TestHysteria2TCPUploadSpeed(t *testing.T) {
	c, _, _, bucket := startIntegrationServer(t, 5, 60)
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { origin.Close() })
	const size int64 = 3125000
	done := make(chan error, 1)
	go func() {
		conn, err := origin.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(15 * time.Second))
		_, err = io.CopyN(io.Discard, conn, size)
		done <- err
	}()
	conn, err := c.TCP(origin.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	bucket.TakeAvailable(bucket.Capacity())
	start := time.Now()
	if _, err := io.CopyN(conn, zeroPayload{}, size); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	mbps := float64(size) * 8 / elapsed.Seconds() / 1e6
	t.Logf("node5 upload bytes=%d elapsed=%s throughput=%.3fMbps", size, elapsed, mbps)
	if mbps < 4 || mbps > 6 {
		t.Fatalf("upload %.3fMbps outside4..6", mbps)
	}
}

func TestHysteria2UDPConsumesBucket(t *testing.T) {
	c, h, l, _ := startIntegrationServer(t, 5, 60)
	clock := &trafficClock{now: time.Unix(0, 0)}
	l.SpeedLimiter.Store(format.UserTag(h.Tag, "integration-user"), ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock))
	origin, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { origin.Close() })
	go func() {
		buf := make([]byte, 1500)
		n, addr, err := origin.ReadFrom(buf)
		if err == nil {
			origin.WriteTo(buf[:n], addr)
		}
	}()
	u, err := c.UDP()
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	payload := bytes.Repeat([]byte{42}, 600)
	if err := u.Send(payload, origin.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	type udpResult struct {
		data []byte
		err  error
	}
	done := make(chan udpResult, 1)
	go func() { b, _, err := u.Receive(); done <- udpResult{b, err} }()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !bytes.Equal(result.data, payload) {
			t.Fatal("UDP payload mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UDP echo timed out")
	}
	clock.mu.Lock()
	waited := clock.waited
	clock.mu.Unlock()
	if waited != time.Second {
		t.Fatalf("real UDP upload/download did not consume shared bucket: %v", waited)
	}
	up, down := trafficCounts(t, h, "integration-user")
	if up != 600 || down != 600 {
		t.Fatalf("UDP counters %d,%d, want600,600", up, down)
	}
}
