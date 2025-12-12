package metrics

import (
	"net/http"
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
	RecordDBQuery(operation, table string, duration time.Duration, err error)

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

	// Handler returns an HTTP handler for exposing metrics (e.g., /metrics endpoint)
	Handler() http.Handler
}

// globalProvider is the global metrics provider
var globalProvider Provider

// SetProvider sets the global metrics provider
func SetProvider(p Provider) {
	globalProvider = p
}

// GetProvider returns the current metrics provider
func GetProvider() Provider {
	if globalProvider == nil {
		// Return no-op provider if none is set
		return &NoOpProvider{}
	}
	return globalProvider
}

// NoOpProvider is a no-op implementation of Provider
type NoOpProvider struct{}

func (n *NoOpProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {}
func (n *NoOpProvider) IncRequestsInFlight()                                                  {}
func (n *NoOpProvider) DecRequestsInFlight()                                                  {}
func (n *NoOpProvider) RecordDBQuery(operation, table string, duration time.Duration, err error) {
}
func (n *NoOpProvider) RecordCacheHit(provider string)                {}
func (n *NoOpProvider) RecordCacheMiss(provider string)               {}
func (n *NoOpProvider) UpdateCacheSize(provider string, size int64)   {}
func (n *NoOpProvider) RecordEventPublished(source, eventType string) {}
func (n *NoOpProvider) RecordEventProcessed(source, eventType, status string, duration time.Duration) {
}
func (n *NoOpProvider) UpdateEventQueueSize(size int64) {}
func (n *NoOpProvider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, err := w.Write([]byte("Metrics provider not configured"))
		if err != nil {
			logger.Warn("Failed to write. %v", err)
		}
	})
}
