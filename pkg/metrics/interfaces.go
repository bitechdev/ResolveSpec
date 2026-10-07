package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Provider defines the interface for metric collection
type Provider interface {
	// RecordHTTPRequest records metrics for an HTTP request
	RecordHTTPRequest(method, path, status string, duration time.Duration)

	// IncRequestsInFlight increments the in-flight requests counter
	IncRequestsInFlight()

	// DecRequestsInFlight decrements the in-flight requests counter
	DecRequestsInFlight()

	// RecordDBQuery records metrics for a database query
	RecordDBQuery(operation, schema, entity, table string, duration time.Duration, err error)

	// RecordCacheHit records a cache hit
	RecordCacheHit(provider string)

	// RecordCacheMiss records a cache miss
	RecordCacheMiss(provider string)

	// UpdateCacheSize updates the cache size metric
	UpdateCacheSize(provider string, size int64)

	// RecordEventPublished records an event publication
	RecordEventPublished(source, eventType string)

	// RecordEventProcessed records an event processing with its status
	RecordEventProcessed(source, eventType, status string, duration time.Duration)

	// UpdateEventQueueSize updates the event queue size metric
	UpdateEventQueueSize(size int64)

	// RecordPanic records a panic event
	RecordPanic(methodName string)

	// Handler returns an HTTP handler for exposing metrics (e.g., /metrics endpoint)
	Handler() http.Handler
}

// AIProxyRecorder is optionally implemented by providers that record aiproxy traffic.
// pkg/aiproxy uses it when the global provider implements it.
type AIProxyRecorder interface {
	// RecordAIProxy records one proxied (or refused) request. statusClass is "2xx".."5xx",
	// outcome is ok, upstream_error, denied or rate_limited, model may be empty.
	RecordAIProxy(upstream, kind, model, statusClass, outcome string, duration time.Duration, promptTokens, completionTokens int64)
}

// Resetter is optionally implemented by providers that can clear their recorded stats.
type Resetter interface {
	Reset()
}

// Reset clears the current provider's stats if it supports resetting.
// It returns false if the provider does not implement Resetter.
func Reset() bool {
	if r, ok := GetProvider().(Resetter); ok {
		r.Reset()
		return true
	}
	return false
}

// globalProvider is the global metrics provider, protected by globalProviderMu.
var (
	globalProviderMu sync.RWMutex
	globalProvider   Provider
)

// SetProvider sets the global metrics provider.
func SetProvider(p Provider) {
	globalProviderMu.Lock()
	globalProvider = p
	globalProviderMu.Unlock()
}

// GetProvider returns the current metrics provider.
func GetProvider() Provider {
	globalProviderMu.RLock()
	p := globalProvider
	globalProviderMu.RUnlock()
	if p == nil {
		return &NoOpProvider{}
	}
	return p
}

// NoOpProvider is a no-op implementation of Provider
type NoOpProvider struct{}

func (n *NoOpProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {}
func (n *NoOpProvider) IncRequestsInFlight()                                                  {}
func (n *NoOpProvider) DecRequestsInFlight()                                                  {}
func (n *NoOpProvider) RecordDBQuery(operation, schema, entity, table string, duration time.Duration, err error) {
}
func (n *NoOpProvider) RecordCacheHit(provider string)                {}
func (n *NoOpProvider) RecordCacheMiss(provider string)               {}
func (n *NoOpProvider) UpdateCacheSize(provider string, size int64)   {}
func (n *NoOpProvider) RecordEventPublished(source, eventType string) {}
func (n *NoOpProvider) RecordEventProcessed(source, eventType, status string, duration time.Duration) {
}
func (n *NoOpProvider) UpdateEventQueueSize(size int64) {}
func (n *NoOpProvider) RecordPanic(methodName string)   {}
func (n *NoOpProvider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, err := w.Write([]byte("Metrics provider not configured"))
		if err != nil {
			logger.Warn("Failed to write. %v", err)
		}
	})
}
