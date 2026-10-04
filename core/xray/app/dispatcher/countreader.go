package dispatcher

import (
	"sync/atomic"
	"time"

	"github.com/juju/ratelimit"
	"github.com/xtls/xray-core/common/buf"
)

var _ buf.TimeoutReader = (*CounterReader)(nil)

type CounterReader struct {
	Reader  buf.TimeoutReader
	Counter *atomic.Int64
	Limiter *ratelimit.Bucket
}

func (c *CounterReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb, err := c.Reader.ReadMultiBufferTimeout(timeout)
	c.account(mb)
	return mb, err
}

func (c *CounterReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := c.Reader.ReadMultiBuffer()
	c.account(mb)
	return mb, err
}

func (c *CounterReader) account(mb buf.MultiBuffer) {
	if n := int64(mb.Len()); n > 0 {
		if c.Limiter != nil {
			c.Limiter.Wait(n)
		}
		c.Counter.Add(n)
	}
}
