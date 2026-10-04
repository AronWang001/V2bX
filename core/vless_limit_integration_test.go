//go:build xray && sing

package core_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/core"
	_ "github.com/InazumaV/V2bX/core/sing"
	_ "github.com/InazumaV/V2bX/core/xray"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/juju/ratelimit"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sirupsen/logrus"
)

type vlessFixture struct {
	address     string
	user        panel.UserInfo
	client      *vless.Client
	limiter     *limiter.Limiter
	tag         string
	certificate tls.Certificate
	clientTLS   *tls.Config
	vision      bool
}

func vlessCertificate(t *testing.T) (tls.Certificate, *tls.Config, string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"vless.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, &tls.Config{ServerName: "vless.test", RootCAs: roots, MinVersion: tls.VersionTLS13}, certPath, keyPath
}

func startVLESSFixture(t *testing.T, coreType string, speed int, vision bool) *vlessFixture {
	t.Helper()
	logrus.SetLevel(logrus.ErrorLevel)
	limiter.Init()
	tag := t.Name()
	user := panel.UserInfo{Id: 14, Uuid: "c91cbd8b-0f52-4400-aa26-94f834c37c50", SpeedLimit: speed}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/UniProxy/user" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(panel.UserListBody{Users: []panel.UserInfo{user}})
	}))
	t.Cleanup(api.Close)
	panelClient, err := panel.New(&conf.ApiConfig{APIHost: api.URL, NodeType: "vless", NodeID: 1, Timeout: 2})
	if err != nil {
		t.Fatal(err)
	}
	users, err := panelClient.GetUserList()
	if err != nil || len(users) != 1 || users[0].SpeedLimit != speed {
		t.Fatalf("panel user parsing: %v %v", users, err)
	}
	l := limiter.AddLimiter(tag, &conf.LimitConfig{SpeedLimit: 0}, users, nil)
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	coreCfg := conf.CoreConfig{Type: coreType, XrayConfig: conf.NewXrayConfig(), SingConfig: conf.NewSingConfig()}
	coreCfg.XrayConfig.AssetPath = t.TempDir()
	coreCfg.XrayConfig.LogConfig.Level = "none"
	instance, err := core.NewCore([]conf.CoreConfig{coreCfg})
	if err != nil {
		t.Fatal(err)
	}
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Error(err)
		}
	})
	common := panel.CommonNode{ServerPort: port, ServerName: "vless.test"}
	info := &panel.NodeInfo{Type: "vless", Common: &common, VAllss: &panel.VAllssNode{CommonNode: common, Network: "tcp", NetworkSettings: json.RawMessage(`{}`)}}
	options := &conf.Options{Core: coreType, ListenIP: "127.0.0.1", SendIP: "127.0.0.1", XrayOptions: conf.NewXrayOptions(), SingOptions: conf.NewSingOptions(), CertConfig: conf.NewCertConfig()}
	options.XrayOptions.DisableSniffing = true
	options.SingOptions.SniffEnabled = false
	f := &vlessFixture{address: net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), user: user, limiter: l, tag: tag, vision: vision}
	if vision {
		var certPath, keyPath string
		f.certificate, f.clientTLS, certPath, keyPath = vlessCertificate(t)
		info.Security = panel.Tls
		info.VAllss.Flow = "xtls-rprx-vision"
		options.CertConfig = &conf.CertConfig{CertMode: "file", CertFile: certPath, KeyFile: keyPath}
	}
	if err := instance.AddNode(tag, info, options); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.AddUsers(&core.AddUsersParams{Tag: tag, Users: users, NodeInfo: info}); err != nil {
		t.Fatal(err)
	}
	f.client, err = vless.NewClient(user.Uuid, info.VAllss.Flow, logger.NOP())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *vlessFixture) rawDial(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", f.address, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if f.vision {
		tlsConn := tls.Client(conn, f.clientTLS.Clone())
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		conn = tlsConn
	}
	return conn
}

type vlessZeroReader struct{}

func (vlessZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func checkVLESSPayload(conn io.Reader, size int64) error {
	h := sha256.New()
	if _, err := io.CopyN(h, conn, size); err != nil {
		return err
	}
	expected := sha256.New()
	_, _ = io.CopyN(expected, vlessZeroReader{}, size)
	if !bytes.Equal(h.Sum(nil), expected.Sum(nil)) {
		return fmt.Errorf("payload integrity mismatch")
	}
	return nil
}

func TestVLESSPanelRate(t *testing.T) {
	for _, coreType := range []string{"xray", "sing"} {
		for _, tc := range []struct {
			name           string
			speed, streams int
			upload, vision bool
		}{
			{"tcp_upload60", 60, 1, true, false},
			{"tcp_download30", 30, 1, false, false},
			{"tcp_download40", 40, 1, false, false},
			{"tcp_download50", 50, 1, false, false},
			{"tcp_download60", 60, 1, false, false},
			{"tcp_two_streams60", 60, 2, false, false},
			{"vision_upload60", 60, 1, true, true},
			{"vision_download60", 60, 1, false, true},
		} {
			t.Run(coreType+"/"+tc.name, func(t *testing.T) {
				f := startVLESSFixture(t, coreType, tc.speed, tc.vision)
				origin, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				if tc.vision {
					origin = tls.NewListener(origin, &tls.Config{Certificates: []tls.Certificate{f.certificate}, MinVersion: tls.VersionTLS13})
				}
				t.Cleanup(func() { origin.Close() })
				const seconds = 4
				total := int64(tc.speed) * 1000000 / 8 * seconds
				// Leave room for TLS record overhead below an exact refill boundary.
				// The existing bucket refills in one-second quanta.
				total -= int64(tc.speed) * 1000000 / 8 / 20
				done := make(chan error, tc.streams)
				start := make(chan struct{})
				var startOnce sync.Once
				release := func() { startOnce.Do(func() { close(start) }) }
				var originWG sync.WaitGroup
				originWG.Add(1)
				go func() {
					defer originWG.Done()
					for i := 0; i < tc.streams; i++ {
						conn, err := origin.Accept()
						if err != nil {
							done <- err
							continue
						}
						originWG.Add(1)
						go func() {
							defer originWG.Done()
							defer conn.Close()
							conn.SetDeadline(time.Now().Add(15 * time.Second))
							if tlsConn, ok := conn.(*tls.Conn); ok {
								if err := tlsConn.Handshake(); err != nil {
									done <- err
									return
								}
							}
							<-start
							if tc.upload {
								done <- checkVLESSPayload(conn, total/int64(tc.streams))
							} else {
								_, err := io.CopyN(conn, vlessZeroReader{}, total/int64(tc.streams))
								done <- err
							}
						}()
					}
				}()
				defer func() { release(); origin.Close(); originWG.Wait() }()
				connections := make([]net.Conn, tc.streams)
				for i := range connections {
					connections[i], err = f.client.DialConn(f.rawDial(t), M.ParseSocksaddr(origin.Addr().String()))
					if err != nil {
						t.Fatal(err)
					}
					defer connections[i].Close()
					if tc.vision {
						innerTLS := tls.Client(connections[i], f.clientTLS.Clone())
						if err := innerTLS.Handshake(); err != nil {
							t.Fatal(err)
						}
						connections[i] = innerTLS
						defer innerTLS.Close()
					}
				}
				bucket, reject := f.limiter.CheckLimit(format.UserTag(f.tag, f.user.Uuid), "", true, false)
				if reject || bucket == nil || bucket.Rate() != float64(tc.speed*1000000/8) {
					t.Fatal("panel rate not registered")
				}
				bucket.TakeAvailable(bucket.Capacity())
				began := time.Now()
				release()
				var wg sync.WaitGroup
				clientErrors := make(chan error, tc.streams)
				for _, conn := range connections {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if tc.upload {
							_, err := io.CopyN(conn, vlessZeroReader{}, total/int64(tc.streams))
							clientErrors <- err
						} else {
							clientErrors <- checkVLESSPayload(conn, total/int64(tc.streams))
						}
					}()
				}
				wg.Wait()
				for i := 0; i < tc.streams; i++ {
					if err := <-clientErrors; err != nil {
						t.Fatal(err)
					}
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				mbps := float64(total) * 8 / time.Since(began).Seconds() / 1e6
				t.Logf("core=%s node_limit=0 panel_limit=%d upload=%v vision=%v streams=%d measured=%.3fMbps", coreType, tc.speed, tc.upload, tc.vision, tc.streams, mbps)
				if mbps < float64(tc.speed)*.85 || mbps > float64(tc.speed)*1.15 {
					t.Fatalf("rate%.3fMbps outside panel tier%d tolerance", mbps, tc.speed)
				}
			})
		}
	}
}

type vlessPacketClock struct {
	mu     sync.Mutex
	now    time.Time
	waited time.Duration
}

func (c *vlessPacketClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *vlessPacketClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.waited += d
}

func TestVLESSUDPConsumesPanelBucket(t *testing.T) {
	for _, coreType := range []string{"xray", "sing"} {
		t.Run(coreType, func(t *testing.T) {
			f := startVLESSFixture(t, coreType, 60, false)
			clock := &vlessPacketClock{now: time.Unix(0, 0)}
			bucket := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock)
			f.limiter.SpeedLimiter.Store(format.UserTag(f.tag, f.user.Uuid), bucket)
			origin, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer origin.Close()
			done := make(chan error, 1)
			go func() {
				b := make([]byte, 1500)
				n, addr, err := origin.ReadFrom(b)
				if err == nil {
					_, err = origin.WriteTo(b[:n], addr)
				}
				done <- err
			}()
			conn, err := f.client.DialEarlyPacketConn(f.rawDial(t), M.ParseSocksaddr(origin.LocalAddr().String()))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			payload := bytes.Repeat([]byte{42}, 600)
			if _, err := conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, 1500)
			n, err := conn.Read(response)
			if err != nil || !bytes.Equal(response[:n], payload) {
				t.Fatalf("UDP echo: %d %v", n, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			clock.mu.Lock()
			waited := clock.waited
			clock.mu.Unlock()
			t.Logf("core=%s real_vless_udp_shared_bucket_wait=%s available=%d", coreType, waited, bucket.Available())
			if waited < time.Second {
				t.Fatal("UDP directions bypassed shared user bucket")
			}
		})
	}
}
