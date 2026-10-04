package sing

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/juju/ratelimit"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

type packetClock struct{ now time.Time }

func (c *packetClock) Now() time.Time        { return c.now }
func (c *packetClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

type testPacketConn struct{ net.Conn }

func (c *testPacketConn) ReadPacket(b *buf.Buffer) (M.Socksaddr, error) {
	_, _ = b.Write(make([]byte, 400))
	return M.ParseSocksaddr("127.0.0.1:53"), nil
}
func (c *testPacketConn) WritePacket(b *buf.Buffer, _ M.Socksaddr) error { b.Release(); return nil }

func TestRoutedPacketConnectionConsumesUserBucket(t *testing.T) {
	limiter.Init()
	tag := t.Name()
	user := panel.UserInfo{Id: 14, Uuid: "test-user", SpeedLimit: 60}
	l := limiter.AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{user}, nil)
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })
	clock := &packetClock{now: time.Unix(0, 0)}
	bucket := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock)
	l.SpeedLimiter.Store(format.UserTag(tag, user.Uuid), bucket)
	h := &HookServer{}
	c := h.RoutedPacketConnection(context.Background(), &testPacketConn{}, adapter.InboundContext{Inbound: tag, User: user.Uuid, Source: M.ParseSocksaddr("127.0.0.1:12345"), Destination: M.ParseSocksaddr("127.0.0.1:53")}, nil, nil)
	b := buf.NewPacket()
	if _, err := c.ReadPacket(b); err != nil {
		t.Fatal(err)
	}
	if err := c.WritePacket(b, M.ParseSocksaddr("127.0.0.1:53")); err != nil {
		t.Fatal(err)
	}
	if got := bucket.Available(); got != 200 {
		t.Fatalf("UDP upload/download bypassed user bucket: available=%d, want200", got)
	}
}
