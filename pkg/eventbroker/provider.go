package eventbroker

import (
	"context"
	"time"
)

// Provider defines the storage backend interface for events
// Implementations: MemoryProvider, RedisProvider, NATSProvider, DatabaseProvider
type Provider interface {
	// Store stores an event
	Store(ctx context.Context, event *Event) error

	// Get retrieves an event by ID
	Get(ctx context.Context, id string) (*Event, error)

	// List lists events with optional filters
	List(ctx context.Context, filter *EventFilter) ([]*Event, error)

	// UpdateStatus updates the status of an event
	UpdateStatus(ctx context.Context, id string, status EventStatus, errorMsg string) error

	// Delete deletes an event by ID
	Delete(ctx context.Context, id string) error

	// Stream returns a channel of events for real-time consumption
	// Used for cross-instance pub/sub
	// The channel is closed when the context is canceled or an error occurs
	Stream(ctx context.Context, pattern string) (<-chan *Event, error)

	// Publish publishes an event to all subscribers (for distributed providers)
	// For in-memory provider, this is the same as Store
	// For Redis/NATS/Database, this triggers cross-instance delivery
	Publish(ctx context.Context, event *Event) error

	// Close closes the provider and releases resources
	Close() error

	// Stats returns provider statistics
	Stats(ctx context.Context) (*ProviderStats, error)
}

// EventFilter defines filter criteria for listing events
type EventFilter struct {
	Source     *EventSource
	Status     *EventStatus
	UserID     *int
	Schema     string
	Entity     string
	Operation  string
	InstanceID string
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
	Offset     int
}

// ProviderStats contains statistics about the provider
type ProviderStats struct {
	ProviderType      string                 `json:"provider_type"`
	TotalEvents       int64                  `json:"total_events"`
	PendingEvents     int64                  `json:"pending_events"`
	ProcessingEvents  int64                  `json:"processing_events"`
	CompletedEvents   int64                  `json:"completed_events"`
	FailedEvents      int64                  `json:"failed_events"`
	EventsPublished   int64                  `json:"events_published"`
	EventsConsumed    int64                  `json:"events_consumed"`
	ActiveSubscribers int                    `json:"active_subscribers"`
	ProviderSpecific  map[string]interface{} `json:"provider_specific,omitempty"`
}
