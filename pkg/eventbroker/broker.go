package eventbroker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Broker is the main interface for event publishing and subscription
type Broker interface {
	// Publish publishes an event (mode-dependent: sync or async)
	Publish(ctx context.Context, event *Event) error

	// PublishSync publishes an event synchronously (blocks until all handlers complete)
	PublishSync(ctx context.Context, event *Event) error

	// PublishAsync publishes an event asynchronously (returns immediately)
	PublishAsync(ctx context.Context, event *Event) error

	// Subscribe registers a handler for events matching the pattern
	Subscribe(pattern string, handler EventHandler) (SubscriptionID, error)

	// Unsubscribe removes a subscription
	Unsubscribe(id SubscriptionID) error

	// Start starts the broker (begins processing events)
	Start(ctx context.Context) error

	// Stop stops the broker gracefully (flushes pending events)
	Stop(ctx context.Context) error

	// Stats returns broker statistics
	Stats(ctx context.Context) (*BrokerStats, error)

	// InstanceID returns the instance ID of this broker
	InstanceID() string
}

// ProcessingMode determines how events are processed
type ProcessingMode string

const (
	ProcessingModeSync  ProcessingMode = "sync"
	ProcessingModeAsync ProcessingMode = "async"
)

// BrokerStats contains broker statistics
type BrokerStats struct {
	InstanceID        string                 `json:"instance_id"`
	Mode              ProcessingMode         `json:"mode"`
	IsRunning         bool                   `json:"is_running"`
	TotalPublished    int64                  `json:"total_published"`
	TotalProcessed    int64                  `json:"total_processed"`
	TotalFailed       int64                  `json:"total_failed"`
	ActiveSubscribers int                    `json:"active_subscribers"`
	QueueSize         int                    `json:"queue_size,omitempty"`     // For async mode
	ActiveWorkers     int                    `json:"active_workers,omitempty"` // For async mode
	ProviderStats     *ProviderStats         `json:"provider_stats,omitempty"`
	AdditionalStats   map[string]interface{} `json:"additional_stats,omitempty"`
}

// EventBroker implements the Broker interface
type EventBroker struct {
	provider      Provider
	subscriptions *subscriptionManager
	mode          ProcessingMode
	instanceID    string
	retryPolicy   *RetryPolicy

	// Async mode fields (initialized in Phase 4)
	workerPool *workerPool

	// Runtime state
	isRunning atomic.Bool
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// Statistics
	statsPublished atomic.Int64
	statsProcessed atomic.Int64
	statsFailed    atomic.Int64
}

// RetryPolicy defines how failed events should be retried
type RetryPolicy struct {
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

// DefaultRetryPolicy returns a sensible default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:    3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
	}
}

// Options for creating a new broker
type Options struct {
	Provider    Provider
	Mode        ProcessingMode
	WorkerCount int // For async mode
	BufferSize  int // For async mode
	RetryPolicy *RetryPolicy
	InstanceID  string
}

// NewBroker creates a new event broker with the given options
func NewBroker(opts Options) (*EventBroker, error) {
	if opts.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if opts.InstanceID == "" {
		return nil, fmt.Errorf("instance ID is required")
	}
	if opts.Mode == "" {
		opts.Mode = ProcessingModeAsync // Default to async
	}
	if opts.RetryPolicy == nil {
		opts.RetryPolicy = DefaultRetryPolicy()
	}

	broker := &EventBroker{
		provider:      opts.Provider,
		subscriptions: newSubscriptionManager(),
		mode:          opts.Mode,
		instanceID:    opts.InstanceID,
		retryPolicy:   opts.RetryPolicy,
		stopCh:        make(chan struct{}),
	}

	// Worker pool will be initialized in Phase 4 for async mode
	if opts.Mode == ProcessingModeAsync {
		if opts.WorkerCount == 0 {
			opts.WorkerCount = 10 // Default
		}
		if opts.BufferSize == 0 {
			opts.BufferSize = 1000 // Default
		}
		broker.workerPool = newWorkerPool(opts.WorkerCount, opts.BufferSize, broker.processEvent)
	}

	return broker, nil
}

// Functional option pattern helpers
func WithProvider(p Provider) func(*Options) {
	return func(o *Options) { o.Provider = p }
}

func WithMode(m ProcessingMode) func(*Options) {
	return func(o *Options) { o.Mode = m }
}

func WithWorkerCount(count int) func(*Options) {
	return func(o *Options) { o.WorkerCount = count }
}

func WithBufferSize(size int) func(*Options) {
	return func(o *Options) { o.BufferSize = size }
}

func WithRetryPolicy(policy *RetryPolicy) func(*Options) {
	return func(o *Options) { o.RetryPolicy = policy }
}

func WithInstanceID(id string) func(*Options) {
	return func(o *Options) { o.InstanceID = id }
}

// Start starts the broker
func (b *EventBroker) Start(ctx context.Context) error {
	if b.isRunning.Load() {
		return fmt.Errorf("broker already running")
	}

	b.isRunning.Store(true)

	// Start worker pool for async mode
	if b.mode == ProcessingModeAsync && b.workerPool != nil {
		b.workerPool.Start()
	}

	logger.Info("Event broker started (mode: %s, instance: %s)", b.mode, b.instanceID)
	return nil
}

// Stop stops the broker gracefully
func (b *EventBroker) Stop(ctx context.Context) error {
	var stopErr error

	b.stopOnce.Do(func() {
		logger.Info("Stopping event broker...")

		// Mark as not running
		b.isRunning.Store(false)

		// Close the stop channel
		close(b.stopCh)

		// Stop worker pool for async mode
		if b.mode == ProcessingModeAsync && b.workerPool != nil {
			if err := b.workerPool.Stop(ctx); err != nil {
				logger.Error("Error stopping worker pool: %v", err)
				stopErr = err
			}
		}

		// Wait for all goroutines
		b.wg.Wait()

		// Close provider
		if err := b.provider.Close(); err != nil {
			logger.Error("Error closing provider: %v", err)
			if stopErr == nil {
				stopErr = err
			}
		}

		logger.Info("Event broker stopped")
	})

	return stopErr
}

// Publish publishes an event based on the broker's mode
func (b *EventBroker) Publish(ctx context.Context, event *Event) error {
	if b.mode == ProcessingModeSync {
		return b.PublishSync(ctx, event)
	}
	return b.PublishAsync(ctx, event)
}

// PublishSync publishes an event synchronously
func (b *EventBroker) PublishSync(ctx context.Context, event *Event) error {
	if !b.isRunning.Load() {
		return fmt.Errorf("broker is not running")
	}

	// Validate event
	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	// Store event in provider
	if err := b.provider.Publish(ctx, event); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	b.statsPublished.Add(1)

	// Record metrics
	recordEventPublished(event)

	// Process event synchronously
	if err := b.processEvent(ctx, event); err != nil {
		logger.Error("Failed to process event %s: %v", event.ID, err)
		b.statsFailed.Add(1)
		return err
	}

	b.statsProcessed.Add(1)
	return nil
}

// PublishAsync publishes an event asynchronously
func (b *EventBroker) PublishAsync(ctx context.Context, event *Event) error {
	if !b.isRunning.Load() {
		return fmt.Errorf("broker is not running")
	}

	// Validate event
	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	// Store event in provider
	if err := b.provider.Publish(ctx, event); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	b.statsPublished.Add(1)

	// Record metrics
	recordEventPublished(event)

	// Queue for async processing
	if b.mode == ProcessingModeAsync && b.workerPool != nil {
		// Update queue size metrics
		updateQueueSize(int64(b.workerPool.QueueSize()))
		return b.workerPool.Submit(ctx, event)
	}

	// Fallback to sync if async not configured
	return b.processEvent(ctx, event)
}

// Subscribe adds a subscription for events matching the pattern
func (b *EventBroker) Subscribe(pattern string, handler EventHandler) (SubscriptionID, error) {
	return b.subscriptions.Subscribe(pattern, handler)
}

// Unsubscribe removes a subscription
func (b *EventBroker) Unsubscribe(id SubscriptionID) error {
	return b.subscriptions.Unsubscribe(id)
}

// processEvent processes an event by calling all matching handlers
func (b *EventBroker) processEvent(ctx context.Context, event *Event) error {
	startTime := time.Now()

	// Get all handlers matching this event type
	handlers := b.subscriptions.GetMatching(event.Type)

	if len(handlers) == 0 {
		logger.Debug("No handlers for event type: %s", event.Type)
		return nil
	}

	logger.Debug("Processing event %s with %d handler(s)", event.ID, len(handlers))

	// Mark event as processing
	event.MarkProcessing()
	if err := b.provider.UpdateStatus(ctx, event.ID, EventStatusProcessing, ""); err != nil {
		logger.Warn("Failed to update event status: %v", err)
	}

	// Execute all handlers
	var lastErr error
	for i, handler := range handlers {
		if err := b.executeHandlerWithRetry(ctx, handler, event); err != nil {
			logger.Error("Handler %d failed for event %s: %v", i+1, event.ID, err)
			lastErr = err
			// Continue processing other handlers
		}
	}

	// Update final status
	if lastErr != nil {
		event.MarkFailed(lastErr)
		if err := b.provider.UpdateStatus(ctx, event.ID, EventStatusFailed, lastErr.Error()); err != nil {
			logger.Warn("Failed to update event status: %v", err)
		}

		// Record metrics
		recordEventProcessed(event, time.Since(startTime))

		return lastErr
	}

	event.MarkCompleted()
	if err := b.provider.UpdateStatus(ctx, event.ID, EventStatusCompleted, ""); err != nil {
		logger.Warn("Failed to update event status: %v", err)
	}

	// Record metrics
	recordEventProcessed(event, time.Since(startTime))

	return nil
}

// executeHandlerWithRetry executes a handler with retry logic
func (b *EventBroker) executeHandlerWithRetry(ctx context.Context, handler EventHandler, event *Event) error {
	var lastErr error

	for attempt := 0; attempt <= b.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			delay := b.calculateBackoff(attempt)
			logger.Debug("Retrying event %s (attempt %d/%d) after %v",
				event.ID, attempt, b.retryPolicy.MaxRetries, delay)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			event.IncrementRetry()
		}

		// Execute handler
		if err := handler.Handle(ctx, event); err != nil {
			lastErr = err
			logger.Warn("Handler failed for event %s (attempt %d): %v", event.ID, attempt+1, err)
			continue
		}

		// Success
		return nil
	}

	return fmt.Errorf("handler failed after %d attempts: %w", b.retryPolicy.MaxRetries+1, lastErr)
}

// calculateBackoff calculates the backoff delay for a retry attempt
func (b *EventBroker) calculateBackoff(attempt int) time.Duration {
	delay := float64(b.retryPolicy.InitialDelay) * pow(b.retryPolicy.BackoffFactor, float64(attempt-1))
	if delay > float64(b.retryPolicy.MaxDelay) {
		delay = float64(b.retryPolicy.MaxDelay)
	}
	return time.Duration(delay)
}

// pow is a simple integer power function
func pow(base float64, exp float64) float64 {
	result := 1.0
	for i := 0.0; i < exp; i++ {
		result *= base
	}
	return result
}

// Stats returns broker statistics
func (b *EventBroker) Stats(ctx context.Context) (*BrokerStats, error) {
	providerStats, err := b.provider.Stats(ctx)
	if err != nil {
		logger.Warn("Failed to get provider stats: %v", err)
	}

	stats := &BrokerStats{
		InstanceID:        b.instanceID,
		Mode:              b.mode,
		IsRunning:         b.isRunning.Load(),
		TotalPublished:    b.statsPublished.Load(),
		TotalProcessed:    b.statsProcessed.Load(),
		TotalFailed:       b.statsFailed.Load(),
		ActiveSubscribers: b.subscriptions.Count(),
		ProviderStats:     providerStats,
	}

	// Add async-specific stats
	if b.mode == ProcessingModeAsync && b.workerPool != nil {
		stats.QueueSize = b.workerPool.QueueSize()
		stats.ActiveWorkers = b.workerPool.ActiveWorkers()
	}

	return stats, nil
}

// InstanceID returns the instance ID
func (b *EventBroker) InstanceID() string {
	return b.instanceID
}
