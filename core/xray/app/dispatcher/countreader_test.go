package dispatcher

import (
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juju/ratelimit"
	"github.com/xtls/xray-core/common/buf"
)

type counterReaderClock struct{}

func (counterReaderClock) Now() time.Time      { return time.Unix(0, 0) }
func (counterReaderClock) Sleep(time.Duration) { panic("unexpected bucket wait") }

type tailReader struct{ timeout time.Duration }

func (*tailReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	return buf.MultiBuffer{buf.FromBytes(make([]byte, 100))}, io.EOF
}
func (r *tailReader) ReadMultiBufferTimeout(d time.Duration) (buf.MultiBuffer, error) {
	r.timeout = d
	return r.ReadMultiBuffer()
}

func TestCounterReaderThrottlesAndPreservesTail(t *testing.T) {
	for _, timed := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "timeout_read"}[timed], func(t *testing.T) {
			upstream := &tailReader{}
			bucket := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, counterReaderClock{})
			count := &atomic.Int64{}
			reader := &CounterReader{Reader: upstream, Counter: count, Limiter: bucket}
			var mb buf.MultiBuffer
			var err error
			if timed {
				mb, err = reader.ReadMultiBufferTimeout(250 * time.Millisecond)
			} else {
				mb, err = reader.ReadMultiBuffer()
			}
			defer buf.ReleaseMulti(mb)
			if mb.Len() != 100 || err != io.EOF {
				t.Fatalf("tail payload lost: len=%d err=%v", mb.Len(), err)
			}
			if count.Load() != 100 || bucket.Available() != 900 {
				t.Fatalf("tail did not consume/count actual bytes: count=%d available=%d", count.Load(), bucket.Available())
			}
			if timed && upstream.timeout != 250*time.Millisecond {
				t.Fatalf("caller timeout changed to %s", upstream.timeout)
			}
		})
	}
}
