package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"
)

// PrometheusProvider implements the Provider interface using Prometheus
type PrometheusProvider struct {
	requestDuration  *prometheus.HistogramVec
	requestTotal     *prometheus.CounterVec
	requestsInFlight prometheus.Gauge
	dbQueryDuration  *prometheus.HistogramVec
	dbQueryTotal     *prometheus.CounterVec
	cacheHits        *prometheus.CounterVec
	cacheMisses      *prometheus.CounterVec
	cacheSize        *prometheus.GaugeVec
	eventPublished   *prometheus.CounterVec
	eventProcessed   *prometheus.CounterVec
	eventDuration    *prometheus.HistogramVec
	eventQueueSize   prometheus.Gauge
	panicsTotal      *prometheus.CounterVec

	// Pushgateway fields (optional)
	pushgatewayURL     string
	pushgatewayJobName string
	pusher             *push.Pusher
	pushTicker         *time.Ticker
	pushStop           chan bool
}

// NewPrometheusProvider creates a new Prometheus metrics provider
// If cfg is nil, default configuration will be used
func NewPrometheusProvider(cfg *Config) *PrometheusProvider {
	// Use default config if none provided
	if cfg == nil {
		cfg = DefaultConfig()
	} else {
		// Apply defaults for any missing values
		cfg.ApplyDefaults()
	}

	// Helper to add namespace prefix if configured
	metricName := func(name string) string {
		if cfg.Namespace != "" {
			return cfg.Namespace + "_" + name
		}
		return name
	}

	p := &PrometheusProvider{
		requestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    metricName("http_request_duration_seconds"),
				Help:    "HTTP request duration in seconds",
				Buckets: cfg.HTTPRequestBuckets,
			},
			[]string{"method", "path", "status"},
		),
		requestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("http_requests_total"),
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),

		requestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: metricName("http_requests_in_flight"),
				Help: "Current number of HTTP requests being processed",
			},
		),
		dbQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    metricName("db_query_duration_seconds"),
				Help:    "Database query duration in seconds",
				Buckets: cfg.DBQueryBuckets,
			},
			[]string{"operation", "table"},
		),
		dbQueryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("db_queries_total"),
				Help: "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),
		cacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("cache_hits_total"),
				Help: "Total number of cache hits",
			},
			[]string{"provider"},
		),
		cacheMisses: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("cache_misses_total"),
				Help: "Total number of cache misses",
			},
			[]string{"provider"},
		),
		cacheSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: metricName("cache_size_items"),
				Help: "Number of items in cache",
			},
			[]string{"provider"},
		),
		eventPublished: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("events_published_total"),
				Help: "Total number of events published",
			},
			[]string{"source", "event_type"},
		),
		eventProcessed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("events_processed_total"),
				Help: "Total number of events processed",
			},
			[]string{"source", "event_type", "status"},
		),
		eventDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    metricName("event_processing_duration_seconds"),
				Help:    "Event processing duration in seconds",
				Buckets: cfg.DBQueryBuckets, // Events are typically fast like DB queries
			},
			[]string{"source", "event_type"},
		),
		eventQueueSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: metricName("event_queue_size"),
				Help: "Current number of events in queue",
			},
		),
		panicsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("panics_total"),
				Help: "Total number of panics",
			},
			[]string{"method"},
		),

		pushgatewayURL:     cfg.PushgatewayURL,
		pushgatewayJobName: cfg.PushgatewayJobName,
	}

	// Initialize pushgateway if configured
	if cfg.PushgatewayURL != "" {
		p.pusher = push.New(cfg.PushgatewayURL, cfg.PushgatewayJobName).
			Gatherer(prometheus.DefaultGatherer)

		// Start automatic pushing if interval is configured
		if cfg.PushgatewayInterval > 0 {
			p.pushStop = make(chan bool)
			p.pushTicker = time.NewTicker(time.Duration(cfg.PushgatewayInterval) * time.Second)
			go p.startAutoPush()
		}
	}

	return p
}

// ResponseWriter wraps http.ResponseWriter to capture status code
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (rw *ResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RecordHTTPRequest implements Provider interface
func (p *PrometheusProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	p.requestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
	p.requestTotal.WithLabelValues(method, path, status).Inc()
}

// IncRequestsInFlight implements Provider interface
func (p *PrometheusProvider) IncRequestsInFlight() {
	p.requestsInFlight.Inc()
}

// DecRequestsInFlight implements Provider interface
func (p *PrometheusProvider) DecRequestsInFlight() {
	p.requestsInFlight.Dec()
}

// RecordDBQuery implements Provider interface
func (p *PrometheusProvider) RecordDBQuery(operation, table string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	p.dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
	p.dbQueryTotal.WithLabelValues(operation, table, status).Inc()
}

// RecordCacheHit implements Provider interface
func (p *PrometheusProvider) RecordCacheHit(provider string) {
	p.cacheHits.WithLabelValues(provider).Inc()
}

// RecordCacheMiss implements Provider interface
func (p *PrometheusProvider) RecordCacheMiss(provider string) {
	p.cacheMisses.WithLabelValues(provider).Inc()
}

// UpdateCacheSize implements Provider interface
func (p *PrometheusProvider) UpdateCacheSize(provider string, size int64) {
	p.cacheSize.WithLabelValues(provider).Set(float64(size))
}

// RecordEventPublished implements Provider interface
func (p *PrometheusProvider) RecordEventPublished(source, eventType string) {
	p.eventPublished.WithLabelValues(source, eventType).Inc()
}

// RecordEventProcessed implements Provider interface
func (p *PrometheusProvider) RecordEventProcessed(source, eventType, status string, duration time.Duration) {
	p.eventProcessed.WithLabelValues(source, eventType, status).Inc()
	p.eventDuration.WithLabelValues(source, eventType).Observe(duration.Seconds())
}

// UpdateEventQueueSize implements Provider interface
func (p *PrometheusProvider) UpdateEventQueueSize(size int64) {
	p.eventQueueSize.Set(float64(size))
}

// RecordPanic implements the Provider interface
func (p *PrometheusProvider) RecordPanic(methodName string) {
	p.panicsTotal.WithLabelValues(methodName).Inc()
}

// Handler implements Provider interface
func (p *PrometheusProvider) Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware returns an HTTP middleware that collects metrics
func (p *PrometheusProvider) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Increment in-flight requests
		p.IncRequestsInFlight()
		defer p.DecRequestsInFlight()

		// Wrap response writer to capture status code
		rw := NewResponseWriter(w)

		// Call next handler
		next.ServeHTTP(rw, r)

		// Record metrics
		duration := time.Since(start)
		status := strconv.Itoa(rw.statusCode)

		p.RecordHTTPRequest(r.Method, r.URL.Path, status, duration)
	})
}

// Push manually pushes metrics to the configured Pushgateway
// Returns an error if pushing fails or if Pushgateway is not configured
func (p *PrometheusProvider) Push() error {
	if p.pusher == nil {
		return nil // Pushgateway not configured, silently skip
	}
	return p.pusher.Push()
}

// startAutoPush runs in a goroutine and periodically pushes metrics to Pushgateway
func (p *PrometheusProvider) startAutoPush() {
	for {
		select {
		case <-p.pushTicker.C:
			if err := p.Push(); err != nil {
				// Log error but continue pushing
				// Note: In production, you might want to use a proper logger
				_ = err
			}
		case <-p.pushStop:
			p.pushTicker.Stop()
			return
		}
	}
}

// StopAutoPush stops the automatic push goroutine
// This should be called when shutting down the application
func (p *PrometheusProvider) StopAutoPush() {
	if p.pushStop != nil {
		close(p.pushStop)
	}
}
