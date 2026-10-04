package hy2

import (
	"sync"

	"github.com/InazumaV/V2bX/common/counter"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/apernet/hysteria/core/v2/server"
	quic "github.com/apernet/quic-go"
	"github.com/juju/ratelimit"
	"go.uber.org/zap"
)

var _ server.TrafficLogger = (*HookServer)(nil)

type HookServer struct {
	Tag                   string
	logger                *zap.Logger
	Counter               sync.Map
	ReportMinTrafficBytes int64
}

func (h *HookServer) TraceStream(stream quic.Stream, stats *server.StreamStats) {
}

func (h *HookServer) UntraceStream(stream quic.Stream) {
}

func (h *HookServer) LogTraffic(id string, tx, rx uint64) (ok bool) {
	var c interface{}
	var exists bool

	limiterinfo, err := limiter.GetLimiter(h.Tag)
	if err != nil {
		h.logger.Error("Get limiter error", zap.String("tag", h.Tag), zap.Error(err))
		return false
	}

	taguuid := format.UserTag(h.Tag, id)
	userLimit, ok := limiterinfo.UserLimitInfo.Load(taguuid)
	if !ok {
		return false
	}
	if ok {
		userlimitInfo := userLimit.(*limiter.UserLimitInfo)
		if userlimitInfo.OverLimit {
			userlimitInfo.OverLimit = false
			return false
		}
	}

	// The native core calls this before forwarding TCP bytes and UDP datagrams.
	// Share the existing user bucket across streams, sessions and both directions.
	var bucket *ratelimit.Bucket
	if value, loaded := limiterinfo.SpeedLimiter.Load(taguuid); loaded {
		bucket = value.(*ratelimit.Bucket)
	} else {
		// Existing connections also need a new bucket after a user update.
		var reject bool
		bucket, reject = limiterinfo.CheckLimit(taguuid, "", false, false)
		if reject {
			return false
		}
	}
	if bucket != nil {
		bucket.Wait(int64(tx + rx))
	}

	if c, exists = h.Counter.Load(h.Tag); !exists {
		c, _ = h.Counter.LoadOrStore(h.Tag, counter.NewTrafficCounter())
	}

	if tc, ok := c.(*counter.TrafficCounter); ok {
		tc.Rx(id, int(rx))
		tc.Tx(id, int(tx))
		return true
	}

	return false
}

func (s *HookServer) LogOnlineState(id string, online bool) {
}
