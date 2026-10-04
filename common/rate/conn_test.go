package rate

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/juju/ratelimit"
)

type rateClock struct{ now time.Time }

func (c *rateClock) Now() time.Time        { return c.now }
func (c *rateClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

type shortReadConn struct {
	net.Conn
	remaining int
}

func (c *shortReadConn) Read(p []byte) (int, error) {
	if c.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), c.remaining)
	c.remaining -= n
	return n, nil
}

func TestConnReadChargesActualBytes(t *testing.T) {
	clock := &rateClock{now: time.Unix(0, 0)}
	b := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock)
	c := NewConnRateLimiter(&shortReadConn{remaining: 100}, b)
	if n, err := c.Read(make([]byte, 8192)); n != 100 || err != nil {
		t.Fatalf("read=%d,%v", n, err)
	}
	if got := b.Available(); got != 900 {
		t.Fatalf("charged buffer capacity instead of100 received bytes: available=%d", got)
	}
	if n, err := c.Read(make([]byte, 8192)); n != 0 || err != io.EOF {
		t.Fatalf("EOF=%d,%v", n, err)
	}
	if got := b.Available(); got != 900 {
		t.Fatalf("zero-byte EOF consumed tokens: available=%d", got)
	}
}
