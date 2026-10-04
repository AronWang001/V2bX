package hy2

import (
	"sync"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/counter"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/juju/ratelimit"
	"go.uber.org/zap"
)

type trafficClock struct {
	mu     sync.Mutex
	now    time.Time
	waited time.Duration
}

func (c *trafficClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *trafficClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.waited += d
}

func newTrafficFixture(t *testing.T, nodeSpeed int, users ...panel.UserInfo) (*HookServer, *limiter.Limiter) {
	t.Helper()
	limiter.Init()
	tag := t.Name()
	l := limiter.AddLimiter(tag, &conf.LimitConfig{SpeedLimit: nodeSpeed}, users, nil)
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })
	return &HookServer{Tag: tag, logger: zap.NewNop()}, l
}

func trafficCounts(t *testing.T, h *HookServer, id string) (int64, int64) {
	t.Helper()
	v, ok := h.Counter.Load(h.Tag)
	if !ok {
		t.Fatal("accepted traffic was not counted")
	}
	c := v.(*counter.TrafficCounter)
	return c.GetUpCount(id), c.GetDownCount(id)
}

func TestLogTrafficConsumesSharedBucket(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tx, rx uint64
	}{
		{"upload", 2000, 0}, {"download", 0, 2000}, {"both_directions", 1000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, l := newTrafficFixture(t, 5, panel.UserInfo{Id: 14, Uuid: "user-a", SpeedLimit: 60})
			clock := &trafficClock{now: time.Unix(0, 0)}
			l.SpeedLimiter.Store(format.UserTag(h.Tag, "user-a"), ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock))
			if !h.LogTraffic("user-a", tc.tx, tc.rx) {
				t.Fatal("valid traffic rejected")
			}
			if clock.waited != time.Second {
				t.Fatalf("traffic must consume tokens: waited %v, want 1s", clock.waited)
			}
			up, down := trafficCounts(t, h, "user-a")
			if up != int64(tc.tx) || down != int64(tc.rx) {
				t.Fatalf("counts=(%d,%d), want=(%d,%d)", up, down, tc.tx, tc.rx)
			}
		})
	}
}

func TestLogTrafficSharesLimitAcrossCallbacks(t *testing.T) {
	h, l := newTrafficFixture(t, 5, panel.UserInfo{Id: 14, Uuid: "user-a", SpeedLimit: 60})
	clock := &trafficClock{now: time.Unix(0, 0)}
	b := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock)
	l.SpeedLimiter.Store(format.UserTag(h.Tag, "user-a"), b)
	if !h.LogTraffic("user-a", 1000, 0) || !h.LogTraffic("user-a", 0, 1000) {
		t.Fatal("traffic rejected")
	}
	if clock.waited != time.Second {
		t.Fatalf("upload and download must share tokens, waited %v", clock.waited)
	}
}

func TestLogTrafficCreatesBucketAndHonorsUserChanges(t *testing.T) {
	user := panel.UserInfo{Id: 14, Uuid: "user-a", SpeedLimit: 60}
	h, l := newTrafficFixture(t, 5, user)
	if !h.LogTraffic(user.Uuid, 0, 1) {
		t.Fatal("valid traffic rejected")
	}
	v, ok := l.SpeedLimiter.Load(format.UserTag(h.Tag, user.Uuid))
	if !ok {
		t.Fatal("first traffic must initialize its speed bucket")
	}
	if got := v.(*ratelimit.Bucket).Rate(); got != 625000 {
		t.Fatalf("node5/user60 rate=%v, want625000 Byte/s", got)
	}
	l.UpdateUser(h.Tag, nil, []panel.UserInfo{user})
	if h.LogTraffic(user.Uuid, 0, 1) {
		t.Fatal("removed user traffic must be rejected")
	}
	user.SpeedLimit = 30
	l.SpeedLimit = 0
	l.UpdateUser(h.Tag, []panel.UserInfo{user}, nil)
	if !h.LogTraffic(user.Uuid, 0, 1) {
		t.Fatal("updated user traffic rejected")
	}
	v, ok = l.SpeedLimiter.Load(format.UserTag(h.Tag, user.Uuid))
	if !ok || v.(*ratelimit.Bucket).Rate() != 3750000 {
		t.Fatal("existing traffic must use the updated30Mbps bucket")
	}
}

func TestLogTrafficUnlimitedAndOverLimit(t *testing.T) {
	h, l := newTrafficFixture(t, 0, panel.UserInfo{Id: 1, Uuid: "unlimited"})
	if !h.LogTraffic("unlimited", 1234, 5678) {
		t.Fatal("unlimited traffic rejected")
	}
	if _, ok := l.SpeedLimiter.Load(format.UserTag(h.Tag, "unlimited")); ok {
		t.Fatal("unlimited user must not get a bucket")
	}
	v, _ := l.UserLimitInfo.Load(format.UserTag(h.Tag, "unlimited"))
	v.(*limiter.UserLimitInfo).OverLimit = true
	if h.LogTraffic("unlimited", 1, 1) {
		t.Fatal("over-limit traffic must be rejected")
	}
	up, down := trafficCounts(t, h, "unlimited")
	if up != 1234 || down != 5678 {
		t.Fatalf("rejected traffic changed counters: %d,%d", up, down)
	}
	if h.LogTraffic("unknown", 1, 1) {
		t.Fatal("unknown user must be rejected")
	}
}

func TestLogTrafficConcurrentStreamsShareTokensAndCounters(t *testing.T) {
	h, l := newTrafficFixture(t, 5, panel.UserInfo{Id: 14, Uuid: "user-a", SpeedLimit: 60})
	clock := &trafficClock{now: time.Unix(0, 0)}
	b := ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000000, 1000000, clock)
	l.SpeedLimiter.Store(format.UserTag(h.Tag, "user-a"), b)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if !h.LogTraffic("user-a", 100, 100) {
					t.Error("valid concurrent traffic rejected")
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := b.Available(); got != 360000 {
		t.Fatalf("streams bypassed shared bucket: available=%d, want360000", got)
	}
	up, down := trafficCounts(t, h, "user-a")
	if up != 320000 || down != 320000 {
		t.Fatalf("concurrent counters lost traffic: %d,%d", up, down)
	}
}

func TestLogTrafficUsersHaveIndependentBuckets(t *testing.T) {
	h, l := newTrafficFixture(t, 5, panel.UserInfo{Id: 1, Uuid: "user-a"}, panel.UserInfo{Id: 2, Uuid: "user-b"})
	for _, id := range []string{"user-a", "user-b"} {
		clock := &trafficClock{now: time.Unix(0, 0)}
		l.SpeedLimiter.Store(format.UserTag(h.Tag, id), ratelimit.NewBucketWithQuantumAndClock(time.Second, 1000, 1000, clock))
	}
	if !h.LogTraffic("user-a", 0, 1000) {
		t.Fatal("user-a rejected")
	}
	v, _ := l.SpeedLimiter.Load(format.UserTag(h.Tag, "user-b"))
	if v.(*ratelimit.Bucket).Available() != 1000 {
		t.Fatal("user-a consumed user-b tokens")
	}
}
