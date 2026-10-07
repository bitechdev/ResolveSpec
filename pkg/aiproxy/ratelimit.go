package aiproxy

import (
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	limiterSweepEvery = 5 * time.Minute
	limiterIdleAfter  = 10 * time.Minute
)

type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}

// limiters keeps one token bucket per key and drops idle ones.
type limiters struct {
	mu        sync.Mutex
	entries   map[string]*limiterEntry
	lastSweep time.Time
}

func newLimiters() *limiters {
	return &limiters{entries: make(map[string]*limiterEntry), lastSweep: time.Now()}
}

// allow takes a token. When denied it returns how long to wait.
func (l *limiters) allow(key string, rl *RateLimit) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) > limiterSweepEvery {
		for k, e := range l.entries {
			if now.Sub(e.last) > limiterIdleAfter {
				delete(l.entries, k)
			}
		}
		l.lastSweep = now
	}

	e, ok := l.entries[key]
	if !ok {
		burst := rl.Burst
		if burst < 1 {
			burst = int(math.Ceil(rl.PerSecond))
			if burst < 1 {
				burst = 1
			}
		}
		e = &limiterEntry{lim: rate.NewLimiter(rate.Limit(rl.PerSecond), burst)}
		l.entries[key] = e
	}
	e.last = now

	res := e.lim.ReserveN(now, 1)
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return false, d
	}
	return true, 0
}

// forget drops the buckets of an upstream (after it was replaced or removed).
func (l *limiters) forget(upstream string) {
	prefix := upstream + "|"
	l.mu.Lock()
	defer l.mu.Unlock()
	for k := range l.entries {
		if strings.HasPrefix(k, prefix) {
			delete(l.entries, k)
		}
	}
}
