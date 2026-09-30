package middleware

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// DefaultClientIDHeader is the header a client (one per browser tab) sends to
// identify itself to the ClientQueue.
const DefaultClientIDHeader = "X-Client-Id"

const maxClientIDLen = 128

// Client queue metrics. They are aggregated over all clients (no per-client
// labels, to keep cardinality bounded) and over all ClientQueue instances.
//
// A burst is one client's busy period: the peak number of its requests
// outstanding (running plus waiting) between the moment it goes from idle to
// busy and the moment it is idle again. A page load firing 15 requests at
// once is a burst of 15. Average burst is burst_size_sum / burst_size_count;
// the highest burst seen is clientqueue_burst_max.
var (
	queueRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "clientqueue_requests_total",
		Help: "Requests by outcome: immediate (ran without waiting), queued (waited, then ran), rejected_full, timeout, canceled",
	}, []string{"result"})

	queueWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "clientqueue_wait_seconds",
		Help:    "Time admitted requests spent waiting for a slot (0 for immediate ones)",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	})

	queueBurst = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "clientqueue_burst_size",
		Help:    "Peak outstanding requests (running + waiting) per client busy period",
		Buckets: []float64{1, 2, 3, 4, 5, 8, 10, 15, 20, 30, 50, 100},
	})

	queueBurstMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "clientqueue_burst_max",
		Help: "Largest burst seen since the process started",
	})

	queueWaitMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "clientqueue_wait_max_seconds",
		Help: "Longest queue wait seen since the process started",
	})

	queueActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "clientqueue_active",
		Help: "Requests currently running through the queue",
	})

	queueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "clientqueue_queue_depth",
		Help: "Requests currently waiting for a slot",
	})

	queueClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "clientqueue_clients",
		Help: "Clients currently tracked by the queue",
	})

	maxMu      sync.Mutex
	maxBurst   float64
	maxWaitSec float64
)

// observeMax raises a high-water gauge when v exceeds the recorded maximum.
func observeMax(g prometheus.Gauge, cur *float64, v float64) {
	maxMu.Lock()
	if v > *cur {
		*cur = v
		g.Set(v)
	}
	maxMu.Unlock()
}

var (
	errQueueFull = errors.New("client queue full")
	errQueueWait = errors.New("client queue wait exceeded")
)

// ClientQueueConfig configures a ClientQueue. Zero values use the defaults.
type ClientQueueConfig struct {
	// MaxConcurrent is how many requests one client may have running at once.
	// Default 4.
	MaxConcurrent int
	// MaxQueue is how many further requests one client may have waiting.
	// Requests beyond it are rejected with 429. Default 50.
	MaxQueue int
	// MaxWait is the longest a request waits for a slot before it is rejected
	// with 503. Default 30s.
	MaxWait time.Duration
	// IdleTimeout is how long a client with no requests is remembered.
	// Default 5m.
	IdleTimeout time.Duration
	// HeaderName is the client id header. Default X-Client-Id.
	HeaderName string
}

func (c *ClientQueueConfig) applyDefaults() {
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 4
	}
	if c.MaxQueue <= 0 {
		c.MaxQueue = 50
	}
	if c.MaxWait <= 0 {
		c.MaxWait = 30 * time.Second
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 5 * time.Minute
	}
	if c.HeaderName == "" {
		c.HeaderName = DefaultClientIDHeader
	}
}

// ClientQueue limits how many requests each client runs concurrently and
// queues the rest first-in-first-out, smoothing bursts such as a page load
// that fires many requests at once. A client is identified by, in order:
// the client id header, the Authorization header (session), then the IP.
type ClientQueue struct {
	cfg     ClientQueueConfig
	mu      sync.Mutex
	clients map[string]*queueClient
	stop    chan struct{}
	once    sync.Once
}

type queueClient struct {
	active      int
	waiters     list.List // of *queueWaiter
	lastUsed    time.Time
	outstanding int // running + waiting
	peak        int // highest outstanding in the current busy period
}

// enter records a request entering the client's outstanding set.
func (c *queueClient) enter() {
	c.outstanding++
	if c.outstanding > c.peak {
		c.peak = c.outstanding
	}
}

// leave records a request leaving it, closing the burst when the client goes idle.
func (c *queueClient) leave() {
	c.outstanding--
	if c.outstanding == 0 {
		queueBurst.Observe(float64(c.peak))
		observeMax(queueBurstMax, &maxBurst, float64(c.peak))
		c.peak = 0
	}
}

type queueWaiter struct {
	ready   chan struct{}
	granted bool
	elem    *list.Element
}

// NewClientQueue creates a ClientQueue and starts its idle-client cleanup.
// Call Close to stop it.
func NewClientQueue(cfg ClientQueueConfig) *ClientQueue {
	cfg.applyDefaults()
	q := &ClientQueue{
		cfg:     cfg,
		clients: make(map[string]*queueClient),
		stop:    make(chan struct{}),
	}
	go q.cleanupRoutine()
	return q
}

// Close stops the cleanup goroutine. Requests in flight are unaffected.
func (q *ClientQueue) Close() { q.once.Do(func() { close(q.stop) }) }

func (q *ClientQueue) cleanupRoutine() {
	interval := q.cfg.IdleTimeout / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-q.stop:
			return
		case now := <-ticker.C:
			q.evictIdle(now)
		}
	}
}

func (q *ClientQueue) evictIdle(now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for key, c := range q.clients {
		if c.active == 0 && c.waiters.Len() == 0 && now.Sub(c.lastUsed) >= q.cfg.IdleTimeout {
			delete(q.clients, key)
			queueClients.Dec()
		}
	}
}

// clientKey identifies the caller by the first of these that is present:
// the client id header, the Authorization header, the built-in server session
// (from the request context once security auth has run, else the session
// cookie), then the IP. Secrets are hashed so they are never held in memory
// or logs. The session is only in the context if the auth middleware runs
// before this one (see Chain).
func (q *ClientQueue) clientKey(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get(q.cfg.HeaderName))
	if id != "" && len(id) <= maxClientIDLen && !strings.ContainsAny(id, "\r\n\x00") {
		return "cid:" + id
	}
	if a := r.Header.Get("Authorization"); a != "" {
		return "auth:" + hashKey(a)
	}
	if sid, ok := security.GetSessionID(r.Context()); ok && sid != "" {
		return "session:" + hashKey(sid)
	}
	if c := security.GetSessionCookie(r); c != "" {
		return "session:" + hashKey(c)
	}
	return "ip:" + getClientIP(r)
}

func hashKey(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:16])
}

// acquire blocks until key has a free slot, the queue is full, ctx ends, or
// MaxWait passes. On success the caller must call release(key).
func (q *ClientQueue) acquire(ctx context.Context, key string) error {
	start := time.Now()
	q.mu.Lock()
	c := q.clients[key]
	if c == nil {
		c = &queueClient{}
		q.clients[key] = c
		queueClients.Inc()
	}
	c.lastUsed = start
	if c.active < q.cfg.MaxConcurrent && c.waiters.Len() == 0 {
		c.active++
		c.enter()
		q.mu.Unlock()
		queueActive.Inc()
		queueWait.Observe(0)
		queueRequests.WithLabelValues("immediate").Inc()
		return nil
	}
	if c.waiters.Len() >= q.cfg.MaxQueue {
		q.mu.Unlock()
		queueRequests.WithLabelValues("rejected_full").Inc()
		return errQueueFull
	}
	w := &queueWaiter{ready: make(chan struct{})}
	w.elem = c.waiters.PushBack(w)
	c.enter()
	q.mu.Unlock()
	queueDepth.Inc()

	timer := time.NewTimer(q.cfg.MaxWait)
	defer timer.Stop()

	var err error
	select {
	case <-w.ready:
		waited := time.Since(start).Seconds()
		queueWait.Observe(waited)
		observeMax(queueWaitMax, &maxWaitSec, waited)
		queueRequests.WithLabelValues("queued").Inc()
		return nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = errQueueWait
	}

	q.mu.Lock()
	if w.granted {
		// A slot was handed over as we gave up; pass it on.
		q.releaseLocked(key)
	} else {
		c.waiters.Remove(w.elem)
		c.leave()
		queueDepth.Dec()
	}
	q.mu.Unlock()
	if errors.Is(err, errQueueWait) {
		queueRequests.WithLabelValues("timeout").Inc()
	} else {
		queueRequests.WithLabelValues("canceled").Inc()
	}
	return err
}

func (q *ClientQueue) release(key string) {
	q.mu.Lock()
	q.releaseLocked(key)
	q.mu.Unlock()
}

// releaseLocked hands the slot to the longest-waiting request, or frees it.
func (q *ClientQueue) releaseLocked(key string) {
	c := q.clients[key]
	if c == nil {
		return
	}
	c.lastUsed = time.Now()
	c.leave()
	queueActive.Dec()
	if front := c.waiters.Front(); front != nil {
		w := c.waiters.Remove(front).(*queueWaiter)
		w.granted = true
		queueDepth.Dec()
		queueActive.Inc()
		close(w.ready)
		return
	}
	c.active--
}

// Middleware queues requests per client. CORS preflights and connection
// upgrades (websockets) bypass the queue, since they are short or long-lived
// respectively and should not hold or wait for a slot.
func (q *ClientQueue) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}

		key := q.clientKey(r)
		if err := q.acquire(r.Context(), key); err != nil {
			switch {
			case errors.Is(err, errQueueFull):
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"queue_full","message":"Too many queued requests"}`, http.StatusTooManyRequests)
			case errors.Is(err, errQueueWait):
				w.Header().Set("Retry-After", strconv.Itoa(int(q.cfg.MaxWait.Seconds())))
				http.Error(w, `{"error":"queue_timeout","message":"Timed out waiting in the request queue"}`, http.StatusServiceUnavailable)
			}
			// Otherwise the client went away; there is no one to answer.
			return
		}
		defer q.release(key)

		next.ServeHTTP(w, r)
	})
}

// Chain composes middlewares into one, for the single middleware slot of
// restheadspec.SetupMuxRoutes and friends. The first argument is the
// outermost: Chain(auth, q.Middleware) authenticates before a request may
// occupy a queue slot, so unauthenticated traffic cannot fill queues.
func Chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			if mws[i] != nil {
				next = mws[i](next)
			}
		}
		return next
	}
}
