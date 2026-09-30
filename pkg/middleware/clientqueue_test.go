package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func newTestQueue(t *testing.T, cfg ClientQueueConfig) *ClientQueue {
	q := NewClientQueue(cfg)
	t.Cleanup(q.Close)
	return q
}

func TestClientQueueKey(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{})
	mk := func(id, auth string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		if id != "" {
			r.Header.Set("X-Client-Id", id)
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		return r
	}
	withCtxSession := func(r *http.Request, sid string) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), security.SessionIDKey, sid))
	}

	// 1. client id wins over everything else.
	r := withCtxSession(mk("tab1", "Bearer a"), "sess")
	r.AddCookie(&http.Cookie{Name: "session_token", Value: "cookie"})
	if got := q.clientKey(r); got != "cid:tab1" {
		t.Errorf("id: %q", got)
	}
	// 2. Authorization next, hashed.
	got := q.clientKey(withCtxSession(mk("", "Bearer secret"), "sess"))
	if !strings.HasPrefix(got, "auth:") || strings.Contains(got, "secret") {
		t.Errorf("auth must be hashed: %q", got)
	}
	// 3. built-in session from the context, then the cookie.
	got = q.clientKey(withCtxSession(mk("", ""), "sess"))
	if !strings.HasPrefix(got, "session:") || strings.Contains(strings.TrimPrefix(got, "session:"), "sess") {
		t.Errorf("context session: %q", got)
	}
	rc := mk("", "")
	rc.AddCookie(&http.Cookie{Name: "session_token", Value: "cookie"})
	if got := q.clientKey(rc); !strings.HasPrefix(got, "session:") {
		t.Errorf("cookie session: %q", got)
	}
	// 4. IP last.
	if got := q.clientKey(mk("", "")); got != "ip:10.0.0.1" {
		t.Errorf("ip: %q", got)
	}
	// Oversized ids are ignored and fall through.
	if got := q.clientKey(mk(strings.Repeat("x", 200), "")); got != "ip:10.0.0.1" {
		t.Errorf("oversized id should be ignored: %q", got)
	}
	// Tabs with different ids get separate queues; same id shares one.
	if q.clientKey(mk("tab1", "Bearer a")) == q.clientKey(mk("tab2", "Bearer a")) {
		t.Error("different ids must not share a queue")
	}
	if q.clientKey(mk("", "Bearer a")) != q.clientKey(mk("", "Bearer a")) {
		t.Error("same session without id must share a queue")
	}
}

func TestClientQueueLimitsConcurrencyPerClient(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 3})
	var cur, peak atomic.Int32
	h := q.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		cur.Add(-1)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Client-Id", "tab1")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Errorf("code %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p > 3 {
		t.Fatalf("peak concurrency %d, want <= 3", p)
	}
}

func TestClientQueueClientsAreIndependent(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 1})
	block := make(chan struct{})
	started := make(chan struct{}, 2)
	h := q.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-block
	}))
	for _, id := range []string{"a", "b"} {
		go func(id string) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Client-Id", id)
			h.ServeHTTP(httptest.NewRecorder(), r)
		}(id)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("a second client was blocked by the first")
		}
	}
	close(block)
}

func TestClientQueueFullAndTimeout(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 1, MaxQueue: 1, MaxWait: 50 * time.Millisecond})
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	h := q.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-block
	}))
	do := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Client-Id", "tab1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	go do() // takes the only slot
	<-started

	queued := make(chan *httptest.ResponseRecorder, 1)
	go func() { queued <- do() }()
	time.Sleep(10 * time.Millisecond) // let it enter the queue

	if w := do(); w.Code != http.StatusTooManyRequests {
		t.Errorf("queue full: got %d, want 429", w.Code)
	}
	if w := <-queued; w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("wait timeout: got %d, want 503 with Retry-After", w.Code)
	}
	close(block)
}

func TestClientQueueCancelledWaiterFreesQueue(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 1})
	if err := q.acquire(t.Context(), "k"); err != nil {
		t.Fatal(err)
	}
	cctx, ccancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- q.acquire(cctx, "k") }()
	time.Sleep(10 * time.Millisecond)
	ccancel()
	if err := <-done; err == nil {
		t.Fatal("expected cancellation error")
	}
	q.release("k")

	q.mu.Lock()
	c := q.clients["k"]
	active, waiting := c.active, c.waiters.Len()
	q.mu.Unlock()
	if active != 0 || waiting != 0 {
		t.Fatalf("leaked state: active=%d waiting=%d", active, waiting)
	}
}

func TestClientQueueBypassesPreflightAndEvictsIdle(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 1, IdleTimeout: time.Minute})
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	h := q.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			started <- struct{}{}
			<-block
		}
	}))
	go func() {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Client-Id", "tab1")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}()
	<-started

	r := httptest.NewRequest("OPTIONS", "/", nil)
	r.Header.Set("X-Client-Id", "tab1")
	done := make(chan struct{})
	go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("preflight was queued")
	}
	close(block)
	time.Sleep(20 * time.Millisecond)

	q.evictIdle(time.Now().Add(2 * time.Minute))
	q.mu.Lock()
	n := len(q.clients)
	q.mu.Unlock()
	if n != 0 {
		t.Fatalf("idle client not evicted: %d tracked", n)
	}
}

func TestChainOrder(t *testing.T) {
	var order []string
	tag := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(tag("auth"), nil, tag("queue"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if got := len(order); got != 3 || order[0] != "auth" || order[1] != "queue" || order[2] != "handler" {
		t.Fatalf("order = %v", order)
	}
}

func gaugeVal(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()
	var d dto.Metric
	if err := m.Write(&d); err != nil {
		t.Fatal(err)
	}
	switch {
	case d.Gauge != nil:
		return d.Gauge.GetValue()
	case d.Counter != nil:
		return d.Counter.GetValue()
	}
	t.Fatal("not a gauge or counter")
	return 0
}

func histVal(t *testing.T, h prometheus.Histogram) (count uint64, sum float64) {
	t.Helper()
	var d dto.Metric
	if err := h.Write(&d); err != nil {
		t.Fatal(err)
	}
	return d.Histogram.GetSampleCount(), d.Histogram.GetSampleSum()
}

func TestClientQueueMetrics(t *testing.T) {
	q := newTestQueue(t, ClientQueueConfig{MaxConcurrent: 2})
	imm0 := gaugeVal(t, queueRequests.WithLabelValues("immediate"))
	que0 := gaugeVal(t, queueRequests.WithLabelValues("queued"))
	bc0, bs0 := histVal(t, queueBurst)
	wc0, _ := histVal(t, queueWait)

	block := make(chan struct{})
	h := q.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Client-Id", "metrics-tab")
			h.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	// Wait for 2 running and 3 queued.
	deadline := time.Now().Add(2 * time.Second)
	for {
		q.mu.Lock()
		c := q.clients["cid:metrics-tab"]
		ok := c != nil && c.active == 2 && c.waiters.Len() == 3
		q.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("requests did not reach 2 running + 3 queued")
		}
		time.Sleep(time.Millisecond)
	}
	if got := gaugeVal(t, queueDepth); got < 3 {
		t.Errorf("queue depth = %v, want >= 3", got)
	}
	close(block)
	wg.Wait()

	if got := gaugeVal(t, queueRequests.WithLabelValues("immediate")) - imm0; got != 2 {
		t.Errorf("immediate = %v, want 2", got)
	}
	if got := gaugeVal(t, queueRequests.WithLabelValues("queued")) - que0; got != 3 {
		t.Errorf("queued = %v, want 3", got)
	}
	bc, bs := histVal(t, queueBurst)
	if bc-bc0 != 1 || bs-bs0 != 5 {
		t.Errorf("burst observations = %d (sum %v), want 1 burst of size 5", bc-bc0, bs-bs0)
	}
	if wc, _ := histVal(t, queueWait); wc-wc0 != 5 {
		t.Errorf("wait observations = %d, want 5", wc-wc0)
	}
	if got := gaugeVal(t, queueBurstMax); got < 5 {
		t.Errorf("burst max = %v, want >= 5", got)
	}
}
