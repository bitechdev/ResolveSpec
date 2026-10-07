package metrics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

var errMetricsDisabled = errors.New("metrics: disabled")

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
	aiRequests       *prometheus.CounterVec
	aiDuration       *prometheus.HistogramVec
	aiTokens         *prometheus.CounterVec

	pathLimiter    *pathLimiter
	pathNormalizer func(*http.Request) string

	enabled  bool
	endpoint *endpointPusher

	// Pushgateway fields (optional)
	pushgatewayURL     string
	pushgatewayJobName string
	resetOnPush        bool
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
		enabled: cfg.Enabled,
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
			[]string{"operation", "schema", "entity", "table"},
		),
		dbQueryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("db_queries_total"),
				Help: "Total number of database queries",
			},
			[]string{"operation", "schema", "entity", "table", "status"},
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
		aiRequests: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("aiproxy_requests_total"),
				Help: "Total number of requests handled by the AI proxy",
			},
			[]string{"upstream", "kind", "model", "status", "outcome"},
		),
		aiDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    metricName("aiproxy_request_duration_seconds"),
				Help:    "AI proxy request duration in seconds",
				Buckets: cfg.HTTPRequestBuckets,
			},
			[]string{"upstream", "kind"},
		),
		aiTokens: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName("aiproxy_tokens_total"),
				Help: "Tokens reported by AI proxy upstreams",
			},
			[]string{"upstream", "model", "type"},
		),

		pathLimiter:    newPathLimiter(cfg.HTTPMaxPaths),
		pathNormalizer: cfg.HTTPPathNormalizer,

		pushgatewayURL:     cfg.PushgatewayURL,
		pushgatewayJobName: cfg.PushgatewayJobName,
		resetOnPush:        cfg.PushgatewayResetOnPush,
	}

	// Initialize pushgateway if configured
	// Pushing is never started for a disabled provider
	if cfg.PushgatewayURL != "" && cfg.Enabled {
		p.pusher = push.New(cfg.PushgatewayURL, cfg.PushgatewayJobName).
			Gatherer(prometheus.DefaultGatherer)

		// Start automatic pushing if interval is configured
		if cfg.PushgatewayInterval > 0 {
			p.pushStop = make(chan bool)
			p.pushTicker = time.NewTicker(time.Duration(cfg.PushgatewayInterval) * time.Second)
			go p.startAutoPush()
		}
	}

	if cfg.PushEndpointURL != "" && cfg.Enabled {
		p.endpoint = newEndpointPusher(cfg, p)
		if cfg.PushEndpointInterval > 0 {
			p.endpoint.start(time.Duration(cfg.PushEndpointInterval) * time.Second)
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
// The path is normalized and capped to keep label cardinality bounded.
func (p *PrometheusProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	if !p.enabled {
		return
	}
	path = p.pathLimiter.label(NormalizePath(path))
	p.requestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
	p.requestTotal.WithLabelValues(method, path, status).Inc()
}

// IncRequestsInFlight implements Provider interface
func (p *PrometheusProvider) IncRequestsInFlight() {
	if !p.enabled {
		return
	}
	p.requestsInFlight.Inc()
}

// DecRequestsInFlight implements Provider interface
func (p *PrometheusProvider) DecRequestsInFlight() {
	if !p.enabled {
		return
	}
	p.requestsInFlight.Dec()
}

// RecordDBQuery implements Provider interface
func (p *PrometheusProvider) RecordDBQuery(operation, schema, entity, table string, duration time.Duration, err error) {
	if !p.enabled {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	p.dbQueryDuration.WithLabelValues(operation, schema, entity, table).Observe(duration.Seconds())
	p.dbQueryTotal.WithLabelValues(operation, schema, entity, table, status).Inc()
}

// RecordCacheHit implements Provider interface
func (p *PrometheusProvider) RecordCacheHit(provider string) {
	if !p.enabled {
		return
	}
	p.cacheHits.WithLabelValues(provider).Inc()
}

// RecordCacheMiss implements Provider interface
func (p *PrometheusProvider) RecordCacheMiss(provider string) {
	if !p.enabled {
		return
	}
	p.cacheMisses.WithLabelValues(provider).Inc()
}

// UpdateCacheSize implements Provider interface
func (p *PrometheusProvider) UpdateCacheSize(provider string, size int64) {
	if !p.enabled {
		return
	}
	p.cacheSize.WithLabelValues(provider).Set(float64(size))
}

// RecordEventPublished implements Provider interface
func (p *PrometheusProvider) RecordEventPublished(source, eventType string) {
	if !p.enabled {
		return
	}
	p.eventPublished.WithLabelValues(source, eventType).Inc()
}

// RecordEventProcessed implements Provider interface
func (p *PrometheusProvider) RecordEventProcessed(source, eventType, status string, duration time.Duration) {
	if !p.enabled {
		return
	}
	p.eventProcessed.WithLabelValues(source, eventType, status).Inc()
	p.eventDuration.WithLabelValues(source, eventType).Observe(duration.Seconds())
}

// UpdateEventQueueSize implements Provider interface
func (p *PrometheusProvider) UpdateEventQueueSize(size int64) {
	if !p.enabled {
		return
	}
	p.eventQueueSize.Set(float64(size))
}

// RecordPanic implements the Provider interface
func (p *PrometheusProvider) RecordPanic(methodName string) {
	if !p.enabled {
		return
	}
	p.panicsTotal.WithLabelValues(methodName).Inc()
}

// RecordAIProxy implements AIProxyRecorder
func (p *PrometheusProvider) RecordAIProxy(upstream, kind, model, statusClass, outcome string, duration time.Duration, promptTokens, completionTokens int64) {
	if !p.enabled {
		return
	}
	p.aiRequests.WithLabelValues(upstream, kind, model, statusClass, outcome).Inc()
	p.aiDuration.WithLabelValues(upstream, kind).Observe(duration.Seconds())
	if promptTokens > 0 {
		p.aiTokens.WithLabelValues(upstream, model, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		p.aiTokens.WithLabelValues(upstream, model, "completion").Add(float64(completionTokens))
	}
}

// Handler implements Provider interface
// It responds 404 when metrics are disabled.
func (p *PrometheusProvider) Handler() http.Handler {
	if !p.enabled {
		return disabledHandler()
	}
	return promhttp.Handler()
}

// JSONHandler returns an HTTP handler serving the current metrics as JSON
// (same shape as the "json" push endpoint format). Only GET and HEAD are
// accepted, and it responds 404 when metrics are disabled. It performs no
// authentication; mount it on an internal/protected route.
func (p *PrometheusProvider) JSONHandler() http.Handler {
	if !p.enabled {
		return disabledHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mfs, err := prometheus.DefaultGatherer.Gather()
		if err != nil && len(mfs) == 0 {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body, contentType, err := encodeMetrics(mfs, "json")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		if r.Method == http.MethodGet {
			if _, err := w.Write(body); err != nil {
				logger.Warn("Failed to write metrics JSON: %v", err)
			}
		}
	})
}

func disabledHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "metrics disabled", http.StatusNotFound)
	})
}

// Middleware returns an HTTP middleware that collects metrics
// When metrics are disabled it returns next unchanged.
func (p *PrometheusProvider) Middleware(next http.Handler) http.Handler {
	if !p.enabled {
		return next
	}
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

		// Read the label after next has run so the router has set r.Pattern.
		p.RecordHTTPRequest(r.Method, routeLabel(r, p.pathNormalizer), status, duration)
	})
}

// Push manually pushes metrics to the configured Pushgateway
// Returns an error if pushing fails or if Pushgateway is not configured
func (p *PrometheusProvider) Push() error {
	if !p.enabled {
		return errMetricsDisabled
	}
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
			var err error
			if p.resetOnPush {
				err = p.PushAndReset()
			} else {
				err = p.Push()
			}
			if err != nil {
				// Log and keep going; the next tick retries (and nothing was reset)
				logger.Warn("Failed to push metrics to Pushgateway: %v", err)
			}
		case <-p.pushStop:
			p.pushTicker.Stop()
			return
		}
	}
}

// Reset clears all recorded counters, histograms and labelled gauges (cache size)
// and forgets the tracked HTTP path labels. Live gauges (requests in flight,
// event queue size) are left untouched since they reflect current state.
// Prometheus treats the drop in counters as a counter reset, so rate() and
// increase() keep working on the scraper side.
func (p *PrometheusProvider) Reset() {
	p.requestDuration.Reset()
	p.requestTotal.Reset()
	p.dbQueryDuration.Reset()
	p.dbQueryTotal.Reset()
	p.cacheHits.Reset()
	p.cacheMisses.Reset()
	p.cacheSize.Reset()
	p.eventPublished.Reset()
	p.eventProcessed.Reset()
	p.eventDuration.Reset()
	p.panicsTotal.Reset()
	p.aiRequests.Reset()
	p.aiDuration.Reset()
	p.aiTokens.Reset()
	p.pathLimiter.reset()
}

// PushAndReset pushes metrics to the Pushgateway and, only if the push
// succeeded, clears the local stats. Returns an error if Pushgateway is not
// configured, so stats are never discarded without being delivered. Observations
// recorded between the push and the reset are lost.
func (p *PrometheusProvider) PushAndReset() error {
	if !p.enabled {
		return errMetricsDisabled
	}
	if p.pusher == nil {
		return errors.New("metrics: pushgateway not configured, refusing to reset")
	}
	if err := p.pusher.Push(); err != nil {
		return err
	}
	p.Reset()
	return nil
}

// PushToEndpoint POSTs the current metrics to the configured PushEndpointURL.
// If PushEndpointResetOnSuccess is set, local stats are cleared after a 2xx reply.
// Returns an error if no endpoint is configured.
func (p *PrometheusProvider) PushToEndpoint(ctx context.Context) error {
	if !p.enabled {
		return errMetricsDisabled
	}
	if p.endpoint == nil {
		return errors.New("metrics: push endpoint not configured")
	}
	return p.endpoint.push(ctx)
}

// ResetHandler returns an HTTP handler that clears local stats on POST.
// With ?push=true it first pushes to the Pushgateway and only resets on success.
// The handler performs no authentication; mount it on an internal/protected route.
func (p *PrometheusProvider) ResetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("push") == "true" {
			if err := p.PushAndReset(); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
		} else {
			p.Reset()
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// StopAutoPush stops the automatic push goroutine
// This should be called when shutting down the application
func (p *PrometheusProvider) StopAutoPush() {
	if p.pushStop != nil {
		close(p.pushStop)
		p.pushStop = nil
	}
	if p.endpoint != nil {
		p.endpoint.stop()
	}
}
