package eventbroker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// MemoryProvider implements Provider interface using in-memory storage
// Features:
// - Thread-safe event storage with RW mutex
// - LRU eviction when max events reached
// - In-process pub/sub (not cross-instance)
// - Automatic cleanup of old completed events
type MemoryProvider struct {
	mu              sync.RWMutex
	events          map[string]*Event
	eventOrder      []string // For LRU tracking
	subscribers     map[string][]chan *Event
	instanceID      string
	maxEvents       int
	cleanupInterval time.Duration
	maxAge          time.Duration

	// Statistics
	stats MemoryProviderStats

	// Lifecycle
	stopCleanup chan struct{}
	wg          sync.WaitGroup
	isRunning   atomic.Bool
}

// MemoryProviderStats contains statistics for the memory provider
type MemoryProviderStats struct {
	TotalEvents       atomic.Int64
	PendingEvents     atomic.Int64
	ProcessingEvents  atomic.Int64
	CompletedEvents   atomic.Int64
	FailedEvents      atomic.Int64
	EventsPublished   atomic.Int64
	EventsConsumed    atomic.Int64
	ActiveSubscribers atomic.Int32
	Evictions         atomic.Int64
}

// MemoryProviderOptions configures the memory provider
type MemoryProviderOptions struct {
	InstanceID      string
	MaxEvents       int
	CleanupInterval time.Duration
	MaxAge          time.Duration
}

// NewMemoryProvider creates a new in-memory event provider
func NewMemoryProvider(opts MemoryProviderOptions) *MemoryProvider {
	if opts.MaxEvents == 0 {
		opts.MaxEvents = 10000 // Default
	}
	if opts.CleanupInterval == 0 {
		opts.CleanupInterval = 5 * time.Minute // Default
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 24 * time.Hour // Default: keep events for 24 hours
	}

	mp := &MemoryProvider{
		events:          make(map[string]*Event),
		eventOrder:      make([]string, 0),
		subscribers:     make(map[string][]chan *Event),
		instanceID:      opts.InstanceID,
		maxEvents:       opts.MaxEvents,
		cleanupInterval: opts.CleanupInterval,
		maxAge:          opts.MaxAge,
		stopCleanup:     make(chan struct{}),
	}

	mp.isRunning.Store(true)

	// Start cleanup goroutine
	mp.wg.Add(1)
	go mp.cleanupLoop()

	logger.Info("Memory provider initialized (max_events: %d, cleanup: %v, max_age: %v)",
		opts.MaxEvents, opts.CleanupInterval, opts.MaxAge)

	return mp
}

// Store stores an event
func (mp *MemoryProvider) Store(ctx context.Context, event *Event) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Check if we need to evict oldest events
	if len(mp.events) >= mp.maxEvents {
		mp.evictOldestLocked()
	}

	// Store event
	mp.events[event.ID] = event.Clone()
	mp.eventOrder = append(mp.eventOrder, event.ID)

	// Update statistics
	mp.stats.TotalEvents.Add(1)
	mp.updateStatusCountsLocked(event.Status, 1)

	return nil
}

// Get retrieves an event by ID
func (mp *MemoryProvider) Get(ctx context.Context, id string) (*Event, error) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	event, exists := mp.events[id]
	if !exists {
		return nil, fmt.Errorf("event not found: %s", id)
	}

	return event.Clone(), nil
}

// List lists events with optional filters
func (mp *MemoryProvider) List(ctx context.Context, filter *EventFilter) ([]*Event, error) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var results []*Event

	for _, event := range mp.events {
		if mp.matchesFilter(event, filter) {
			results = append(results, event.Clone())
		}
	}

	// Apply limit and offset
	if filter != nil {
		if filter.Offset > 0 && filter.Offset < len(results) {
			results = results[filter.Offset:]
		}
		if filter.Limit > 0 && filter.Limit < len(results) {
			results = results[:filter.Limit]
		}
	}

	return results, nil
}

// UpdateStatus updates the status of an event
func (mp *MemoryProvider) UpdateStatus(ctx context.Context, id string, status EventStatus, errorMsg string) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	event, exists := mp.events[id]
	if !exists {
		return fmt.Errorf("event not found: %s", id)
	}

	// Update status counts
	mp.updateStatusCountsLocked(event.Status, -1)
	mp.updateStatusCountsLocked(status, 1)

	// Update event
	event.Status = status
	if errorMsg != "" {
		event.Error = errorMsg
	}

	return nil
}

// Delete deletes an event by ID
func (mp *MemoryProvider) Delete(ctx context.Context, id string) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	event, exists := mp.events[id]
	if !exists {
		return fmt.Errorf("event not found: %s", id)
	}

	// Update counts
	mp.stats.TotalEvents.Add(-1)
	mp.updateStatusCountsLocked(event.Status, -1)

	// Delete event
	delete(mp.events, id)

	// Remove from order tracking
	for i, eid := range mp.eventOrder {
		if eid == id {
			mp.eventOrder = append(mp.eventOrder[:i], mp.eventOrder[i+1:]...)
			break
		}
	}

	return nil
}

// Stream returns a channel of events for real-time consumption
// Note: This is in-process only, not cross-instance
func (mp *MemoryProvider) Stream(ctx context.Context, pattern string) (<-chan *Event, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Create buffered channel for events
	ch := make(chan *Event, 100)

	// Store subscriber
	mp.subscribers[pattern] = append(mp.subscribers[pattern], ch)
	mp.stats.ActiveSubscribers.Add(1)

	// Goroutine to clean up on context cancellation
	mp.wg.Add(1)
	go func() {
		defer mp.wg.Done()
		<-ctx.Done()

		mp.mu.Lock()
		defer mp.mu.Unlock()

		// Remove subscriber
		subs := mp.subscribers[pattern]
		for i, subCh := range subs {
			if subCh == ch {
				mp.subscribers[pattern] = append(subs[:i], subs[i+1:]...)
				break
			}
		}

		mp.stats.ActiveSubscribers.Add(-1)
		close(ch)
	}()

	logger.Debug("Stream created for pattern: %s", pattern)
	return ch, nil
}

// Publish publishes an event to all subscribers
func (mp *MemoryProvider) Publish(ctx context.Context, event *Event) error {
	// Store the event first
	if err := mp.Store(ctx, event); err != nil {
		return err
	}

	mp.stats.EventsPublished.Add(1)

	// Notify subscribers
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	for pattern, channels := range mp.subscribers {
		if matchPattern(pattern, event.Type) {
			for _, ch := range channels {
				select {
				case ch <- event.Clone():
					mp.stats.EventsConsumed.Add(1)
				default:
					// Channel full, skip
					logger.Warn("Subscriber channel full for pattern: %s", pattern)
				}
			}
		}
	}

	return nil
}

// Close closes the provider and releases resources
func (mp *MemoryProvider) Close() error {
	if !mp.isRunning.Load() {
		return nil
	}

	mp.isRunning.Store(false)

	// Stop cleanup loop
	close(mp.stopCleanup)

	// Wait for goroutines
	mp.wg.Wait()

	// Close all subscriber channels
	mp.mu.Lock()
	for _, channels := range mp.subscribers {
		for _, ch := range channels {
			close(ch)
		}
	}
	mp.subscribers = make(map[string][]chan *Event)
	mp.mu.Unlock()

	logger.Info("Memory provider closed")
	return nil
}

// Stats returns provider statistics
func (mp *MemoryProvider) Stats(ctx context.Context) (*ProviderStats, error) {
	return &ProviderStats{
		ProviderType:      "memory",
		TotalEvents:       mp.stats.TotalEvents.Load(),
		PendingEvents:     mp.stats.PendingEvents.Load(),
		ProcessingEvents:  mp.stats.ProcessingEvents.Load(),
		CompletedEvents:   mp.stats.CompletedEvents.Load(),
		FailedEvents:      mp.stats.FailedEvents.Load(),
		EventsPublished:   mp.stats.EventsPublished.Load(),
		EventsConsumed:    mp.stats.EventsConsumed.Load(),
		ActiveSubscribers: int(mp.stats.ActiveSubscribers.Load()),
		ProviderSpecific: map[string]interface{}{
			"max_events":       mp.maxEvents,
			"cleanup_interval": mp.cleanupInterval.String(),
			"max_age":          mp.maxAge.String(),
			"evictions":        mp.stats.Evictions.Load(),
		},
	}, nil
}

// cleanupLoop periodically cleans up old completed events
func (mp *MemoryProvider) cleanupLoop() {
	defer mp.wg.Done()

	ticker := time.NewTicker(mp.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mp.cleanup()
		case <-mp.stopCleanup:
			return
		}
	}
}

// cleanup removes old completed/failed events
func (mp *MemoryProvider) cleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	cutoff := time.Now().Add(-mp.maxAge)
	removed := 0

	for id, event := range mp.events {
		// Only clean up completed or failed events that are old
		if (event.Status == EventStatusCompleted || event.Status == EventStatusFailed) &&
			event.CreatedAt.Before(cutoff) {

			delete(mp.events, id)
			mp.stats.TotalEvents.Add(-1)
			mp.updateStatusCountsLocked(event.Status, -1)

			// Remove from order tracking
			for i, eid := range mp.eventOrder {
				if eid == id {
					mp.eventOrder = append(mp.eventOrder[:i], mp.eventOrder[i+1:]...)
					break
				}
			}

			removed++
		}
	}

	if removed > 0 {
		logger.Debug("Cleanup removed %d old events", removed)
	}
}

// evictOldestLocked evicts the oldest event (LRU)
// Caller must hold write lock
func (mp *MemoryProvider) evictOldestLocked() {
	if len(mp.eventOrder) == 0 {
		return
	}

	// Get oldest event ID
	oldestID := mp.eventOrder[0]
	mp.eventOrder = mp.eventOrder[1:]

	// Remove event
	if event, exists := mp.events[oldestID]; exists {
		delete(mp.events, oldestID)
		mp.stats.TotalEvents.Add(-1)
		mp.updateStatusCountsLocked(event.Status, -1)
		mp.stats.Evictions.Add(1)

		logger.Debug("Evicted oldest event: %s", oldestID)
	}
}

// matchesFilter checks if an event matches the filter criteria
func (mp *MemoryProvider) matchesFilter(event *Event, filter *EventFilter) bool {
	if filter == nil {
		return true
	}

	if filter.Source != nil && event.Source != *filter.Source {
		return false
	}
	if filter.Status != nil && event.Status != *filter.Status {
		return false
	}
	if filter.UserID != nil && event.UserID != *filter.UserID {
		return false
	}
	if filter.Schema != "" && event.Schema != filter.Schema {
		return false
	}
	if filter.Entity != "" && event.Entity != filter.Entity {
		return false
	}
	if filter.Operation != "" && event.Operation != filter.Operation {
		return false
	}
	if filter.InstanceID != "" && event.InstanceID != filter.InstanceID {
		return false
	}
	if filter.StartTime != nil && event.CreatedAt.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && event.CreatedAt.After(*filter.EndTime) {
		return false
	}

	return true
}

// updateStatusCountsLocked updates status statistics
// Caller must hold write lock
func (mp *MemoryProvider) updateStatusCountsLocked(status EventStatus, delta int64) {
	switch status {
	case EventStatusPending:
		mp.stats.PendingEvents.Add(delta)
	case EventStatusProcessing:
		mp.stats.ProcessingEvents.Add(delta)
	case EventStatusCompleted:
		mp.stats.CompletedEvents.Add(delta)
	case EventStatusFailed:
		mp.stats.FailedEvents.Add(delta)
	}
}
