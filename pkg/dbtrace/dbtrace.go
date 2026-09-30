// Package dbtrace counts database calls per request and logs the heavy ones.
// It is off until Configure enables it; the hot-path helpers are no-ops for
// contexts without a tracker.
//
// Connection use per request ≈ tx + pooled + raw (each pooled/raw call takes
// its own pool connection; calls inside RunInTransaction share the tx's).
package dbtrace

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/config"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Options controls tracing.
type Options struct {
	Enabled     bool
	MinCalls    int           // log when tx+pooled+raw >= MinCalls
	MinDuration time.Duration // or when the request took at least this (0 = off)
	PoolLog     bool          // log pool deltas on each dbmanager metrics publish
}

// FromConfig maps the application config to Options.
func FromConfig(c config.DBTraceConfig) Options {
	return Options{Enabled: c.Enabled, MinCalls: c.MinCalls, MinDuration: c.MinDuration, PoolLog: c.PoolLog}
}

var (
	opts atomic.Pointer[Options]
)

// Configure sets the active options. Safe to call at any time.
func Configure(o Options) {
	if o.MinCalls <= 0 {
		o.MinCalls = 1
	}
	opts.Store(&o)
}

// Enabled reports whether per-request tracing is on.
func Enabled() bool {
	o := opts.Load()
	return o != nil && o.Enabled
}

// PoolLogEnabled reports whether pool logging is on.
func PoolLogEnabled() bool {
	o := opts.Load()
	return o != nil && o.PoolLog
}

// Tracker holds one request's counters.
type Tracker struct {
	pooled  atomic.Int32 // adapter calls outside a transaction
	inTx    atomic.Int32 // adapter calls inside RunInTransaction
	tx      atomic.Int32 // transactions begun
	txDepth atomic.Int32
	raw     atomic.Int32 // direct *sql.DB calls (auth, security, keystore)

	mu      sync.Mutex
	rawKind map[string]int
}

type ctxKey struct{}

// Start attaches a new Tracker to ctx.
func Start(ctx context.Context) (context.Context, *Tracker) {
	t := &Tracker{}
	return context.WithValue(ctx, ctxKey{}, t), t
}

// From returns the Tracker on ctx, or nil.
func From(ctx context.Context) *Tracker {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(ctxKey{}).(*Tracker)
	return t
}

// Query counts one adapter query.
func Query(ctx context.Context) {
	t := From(ctx)
	if t == nil {
		return
	}
	if t.txDepth.Load() > 0 {
		t.inTx.Add(1)
	} else {
		t.pooled.Add(1)
	}
}

// TxBegin counts a transaction and returns a func to call when it ends.
// Queries between the two are attributed to the transaction's connection.
func TxBegin(ctx context.Context) func() {
	t := From(ctx)
	if t == nil {
		return func() {}
	}
	t.tx.Add(1)
	t.txDepth.Add(1)
	return func() { t.txDepth.Add(-1) }
}

// Raw counts one direct *sql.DB call, labelled by what issued it.
func Raw(ctx context.Context, kind string) {
	t := From(ctx)
	if t == nil {
		return
	}
	t.raw.Add(1)
	t.mu.Lock()
	if t.rawKind == nil {
		t.rawKind = make(map[string]int)
	}
	t.rawKind[kind]++
	t.mu.Unlock()
}

func (t *Tracker) total() int { return int(t.tx.Load() + t.pooled.Load() + t.raw.Load()) }

// Summary renders the counters, e.g. "tx=1 tx_queries=3 pooled=2 raw=1 [auth.session=1]".
func (t *Tracker) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tx=%d tx_queries=%d pooled=%d raw=%d", t.tx.Load(), t.inTx.Load(), t.pooled.Load(), t.raw.Load())
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.rawKind) > 0 {
		kinds := make([]string, 0, len(t.rawKind))
		for k, n := range t.rawKind {
			kinds = append(kinds, fmt.Sprintf("%s=%d", k, n))
		}
		sort.Strings(kinds)
		fmt.Fprintf(&b, " [%s]", strings.Join(kinds, " "))
	}
	return b.String()
}

// Middleware tracks every request and logs those over the configured
// thresholds once the handler returns. Options are read per request, so
// Configure can toggle it at runtime. Raw calls made by detached goroutines
// after the response (e.g. session activity) are not included in the log line.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := opts.Load()
		if o == nil || !o.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		ctx, t := Start(r.Context())
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		elapsed := time.Since(start)
		if t.total() >= o.MinCalls || (o.MinDuration > 0 && elapsed >= o.MinDuration) {
			logger.Info("dbtrace: %s %s %s duration=%s", r.Method, r.URL.Path, t.Summary(), elapsed.Round(time.Millisecond))
		}
	})
}
