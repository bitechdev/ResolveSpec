package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
}

// NewPrometheusProvider creates a new Prometheus metrics provider
func NewPrometheusProvider() *PrometheusProvider {
	return &PrometheusProvider{
		requestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status"},
		),
		requestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),

		requestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_requests_in_flight",
				Help: "Current number of HTTP requests being processed",
			},
		),
		dbQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "db_query_duration_seconds",
				Help:    "Database query duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "table"},
		),
		dbQueryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),
		cacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_hits_total",
				Help: "Total number of cache hits",
			},
			[]string{"provider"},
		),
		cacheMisses: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_misses_total",
				Help: "Total number of cache misses",
			},
			[]string{"provider"},
		),
		cacheSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cache_size_items",
				Help: "Number of items in cache",
			},
			[]string{"provider"},
		),
	}
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
