package eventbroker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// NATSProvider implements Provider interface using NATS JetStream
// Features:
// - Persistent event storage using JetStream
// - Cross-instance pub/sub using NATS subjects
// - Wildcard subscription support
// - Durable consumers for event replay
// - At-least-once delivery semantics
type NATSProvider struct {
	nc            *nats.Conn
	js            jetstream.JetStream
	stream        jetstream.Stream
	streamName    string
	subjectPrefix string
	instanceID    string
	maxAge        time.Duration

	// Subscriptions
	mu          sync.RWMutex
	subscribers map[string]*natsSubscription

	// Statistics
	stats NATSProviderStats

	// Lifecycle
	wg        sync.WaitGroup
	isRunning atomic.Bool
}

// NATSProviderStats contains statistics for the NATS provider
type NATSProviderStats struct {
	TotalEvents       atomic.Int64
	EventsPublished   atomic.Int64
	EventsConsumed    atomic.Int64
	ActiveSubscribers atomic.Int32
	ConsumerErrors    atomic.Int64
}

// natsSubscription represents a single NATS subscription
type natsSubscription struct {
	pattern  string
	consumer jetstream.Consumer
	ch       chan *Event
	ctx      context.Context
	cancel   context.CancelFunc
}

// NATSProviderConfig configures the NATS provider
type NATSProviderConfig struct {
	URL           string
	StreamName    string
	SubjectPrefix string // e.g., "events"
	InstanceID    string
	MaxAge        time.Duration // How long to keep events
	Storage       string        // "file" or "memory"
}

// NewNATSProvider creates a new NATS event provider
func NewNATSProvider(cfg NATSProviderConfig) (*NATSProvider, error) {
	// Apply defaults
	if cfg.URL == "" {
		cfg.URL = nats.DefaultURL
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "RESOLVESPEC_EVENTS"
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "events"
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 7 * 24 * time.Hour // 7 days
	}
	if cfg.Storage == "" {
		cfg.Storage = "file"
	}

	// Connect to NATS
	nc, err := nats.Connect(cfg.URL,
		nats.Name("resolvespec-eventbroker-"+cfg.InstanceID),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	np := &NATSProvider{
		nc:            nc,
		js:            js,
		streamName:    cfg.StreamName,
		subjectPrefix: cfg.SubjectPrefix,
		instanceID:    cfg.InstanceID,
		maxAge:        cfg.MaxAge,
		subscribers:   make(map[string]*natsSubscription),
	}

	np.isRunning.Store(true)

	// Create or update stream
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Determine storage type
	var storage jetstream.StorageType
	if cfg.Storage == "memory" {
		storage = jetstream.MemoryStorage
	} else {
		storage = jetstream.FileStorage
	}

	if err := np.ensureStream(ctx, storage); err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	logger.Info("NATS provider initialized (stream: %s, subject: %s.*, url: %s)",
		cfg.StreamName, cfg.SubjectPrefix, cfg.URL)

	return np, nil
}

// Store stores an event
func (np *NATSProvider) Store(ctx context.Context, event *Event) error {
	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to NATS subject
	// Subject format: events.{source}.{schema}.{entity}.{operation}
	subject := np.buildSubject(event)

	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header: nats.Header{
			"Event-ID":     []string{event.ID},
			"Event-Type":   []string{event.Type},
			"Event-Source": []string{string(event.Source)},
			"Event-Status": []string{string(event.Status)},
			"Instance-ID":  []string{event.InstanceID},
		},
	}

	if _, err := np.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	np.stats.TotalEvents.Add(1)
	return nil
}

// Get retrieves an event by ID
// Note: This is inefficient with JetStream - consider using a separate KV store for lookups
func (np *NATSProvider) Get(ctx context.Context, id string) (*Event, error) {
	// We need to scan messages which is not ideal
	// For production, consider using NATS KV store for fast lookups
	consumer, err := np.stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "get-" + id,
		FilterSubject: np.subjectPrefix + ".>",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Fetch messages in batches
	msgs, err := consumer.Fetch(1000, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	for msg := range msgs.Messages() {
		if msg.Headers().Get("Event-ID") == id {
			var event Event
			if err := json.Unmarshal(msg.Data(), &event); err != nil {
				_ = msg.Nak()
				continue
			}
			_ = msg.Ack()

			// Delete temporary consumer
			_ = np.stream.DeleteConsumer(ctx, "get-"+id)

			return &event, nil
		}
		_ = msg.Ack()
	}

	// Delete temporary consumer
	_ = np.stream.DeleteConsumer(ctx, "get-"+id)

	return nil, fmt.Errorf("event not found: %s", id)
}

// List lists events with optional filters
func (np *NATSProvider) List(ctx context.Context, filter *EventFilter) ([]*Event, error) {
	var results []*Event

	// Create temporary consumer
	consumer, err := np.stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          fmt.Sprintf("list-%d", time.Now().UnixNano()),
		FilterSubject: np.subjectPrefix + ".>",
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	defer func() { _ = np.stream.DeleteConsumer(ctx, consumer.CachedInfo().Name) }()

	// Fetch messages in batches
	msgs, err := consumer.Fetch(1000, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	for msg := range msgs.Messages() {
		var event Event
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			logger.Warn("Failed to unmarshal event: %v", err)
			_ = msg.Nak()
			continue
		}

		if np.matchesFilter(&event, filter) {
			results = append(results, &event)
		}

		_ = msg.Ack()
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
// Note: NATS streams are append-only, so we publish a status update event
func (np *NATSProvider) UpdateStatus(ctx context.Context, id string, status EventStatus, errorMsg string) error {
	// Publish a status update message
	subject := fmt.Sprintf("%s.status.%s", np.subjectPrefix, id)

	statusUpdate := map[string]interface{}{
		"event_id":   id,
		"status":     string(status),
		"error":      errorMsg,
		"updated_at": time.Now(),
	}

	data, err := json.Marshal(statusUpdate)
	if err != nil {
		return fmt.Errorf("failed to marshal status update: %w", err)
	}

	if _, err := np.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("failed to publish status update: %w", err)
	}

	return nil
}

// Delete deletes an event by ID
// Note: NATS streams don't support deletion - this just marks it in a separate subject
func (np *NATSProvider) Delete(ctx context.Context, id string) error {
	subject := fmt.Sprintf("%s.deleted.%s", np.subjectPrefix, id)

	deleteMsg := map[string]interface{}{
		"event_id":   id,
		"deleted_at": time.Now(),
	}

	data, err := json.Marshal(deleteMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal delete message: %w", err)
	}

	if _, err := np.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("failed to publish delete message: %w", err)
	}

	return nil
}

// Stream returns a channel of events for real-time consumption
func (np *NATSProvider) Stream(ctx context.Context, pattern string) (<-chan *Event, error) {
	ch := make(chan *Event, 100)

	// Convert glob pattern to NATS subject pattern
	natsSubject := np.patternToSubject(pattern)

	// Create durable consumer
	consumerName := fmt.Sprintf("consumer-%s-%d", np.instanceID, time.Now().UnixNano())
	consumer, err := np.stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: natsSubject,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	subCtx, cancel := context.WithCancel(ctx)

	sub := &natsSubscription{
		pattern:  pattern,
		consumer: consumer,
		ch:       ch,
		ctx:      subCtx,
		cancel:   cancel,
	}

	np.mu.Lock()
	np.subscribers[pattern] = sub
	np.stats.ActiveSubscribers.Add(1)
	np.mu.Unlock()

	// Start consumer goroutine
	np.wg.Add(1)
	go np.consumeMessages(sub)

	return ch, nil
}

// Publish publishes an event to all subscribers
func (np *NATSProvider) Publish(ctx context.Context, event *Event) error {
	// Store the event first
	if err := np.Store(ctx, event); err != nil {
		return err
	}

	np.stats.EventsPublished.Add(1)
	return nil
}

// Close closes the provider and releases resources
func (np *NATSProvider) Close() error {
	if !np.isRunning.Load() {
		return nil
	}

	np.isRunning.Store(false)

	// Cancel all subscriptions
	np.mu.Lock()
	for _, sub := range np.subscribers {
		sub.cancel()
	}
	np.mu.Unlock()

	// Wait for goroutines
	np.wg.Wait()

	// Close NATS connection
	np.nc.Close()

	logger.Info("NATS provider closed")
	return nil
}

// Stats returns provider statistics
func (np *NATSProvider) Stats(ctx context.Context) (*ProviderStats, error) {
	streamInfo, err := np.stream.Info(ctx)
	if err != nil {
		logger.Warn("Failed to get stream info: %v", err)
	}

	stats := &ProviderStats{
		ProviderType:      "nats",
		TotalEvents:       np.stats.TotalEvents.Load(),
		EventsPublished:   np.stats.EventsPublished.Load(),
		EventsConsumed:    np.stats.EventsConsumed.Load(),
		ActiveSubscribers: int(np.stats.ActiveSubscribers.Load()),
		ProviderSpecific: map[string]interface{}{
			"stream_name":     np.streamName,
			"subject_prefix":  np.subjectPrefix,
			"max_age":         np.maxAge.String(),
			"consumer_errors": np.stats.ConsumerErrors.Load(),
		},
	}

	if streamInfo != nil {
		stats.ProviderSpecific["messages"] = streamInfo.State.Msgs
		stats.ProviderSpecific["bytes"] = streamInfo.State.Bytes
		stats.ProviderSpecific["consumers"] = streamInfo.State.Consumers
	}

	return stats, nil
}

// ensureStream creates or updates the JetStream stream
func (np *NATSProvider) ensureStream(ctx context.Context, storage jetstream.StorageType) error {
	streamConfig := jetstream.StreamConfig{
		Name:      np.streamName,
		Subjects:  []string{np.subjectPrefix + ".>"},
		MaxAge:    np.maxAge,
		Storage:   storage,
		Retention: jetstream.LimitsPolicy,
		Discard:   jetstream.DiscardOld,
	}

	stream, err := np.js.CreateStream(ctx, streamConfig)
	if err != nil {
		// Try to update if already exists
		stream, err = np.js.UpdateStream(ctx, streamConfig)
		if err != nil {
			return fmt.Errorf("failed to create/update stream: %w", err)
		}
	}

	np.stream = stream
	return nil
}

// consumeMessages consumes messages from NATS for a subscription
func (np *NATSProvider) consumeMessages(sub *natsSubscription) {
	defer np.wg.Done()
	defer close(sub.ch)
	defer func() {
		np.mu.Lock()
		delete(np.subscribers, sub.pattern)
		np.stats.ActiveSubscribers.Add(-1)
		np.mu.Unlock()
	}()

	logger.Debug("Starting NATS consumer for pattern: %s", sub.pattern)

	// Consume messages
	cc, err := sub.consumer.Consume(func(msg jetstream.Msg) {
		var event Event
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			logger.Warn("Failed to unmarshal event: %v", err)
			_ = msg.Nak()
			return
		}

		// Check if event matches pattern (additional filtering)
		if matchPattern(sub.pattern, event.Type) {
			select {
			case sub.ch <- &event:
				np.stats.EventsConsumed.Add(1)
				_ = msg.Ack()
			case <-sub.ctx.Done():
				_ = msg.Nak()
				return
			}
		} else {
			_ = msg.Ack()
		}
	})

	if err != nil {
		np.stats.ConsumerErrors.Add(1)
		logger.Error("Failed to start consumer: %v", err)
		return
	}

	// Wait for context cancellation
	<-sub.ctx.Done()

	// Stop consuming
	cc.Stop()

	logger.Debug("NATS consumer stopped for pattern: %s", sub.pattern)
}

// buildSubject creates a NATS subject from an event
// Format: events.{source}.{schema}.{entity}.{operation}
func (np *NATSProvider) buildSubject(event *Event) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s",
		np.subjectPrefix,
		event.Source,
		event.Schema,
		event.Entity,
		event.Operation,
	)
}

// patternToSubject converts a glob pattern to NATS subject pattern
// Examples:
//   - "*" -> "events.>"
//   - "public.users.*" -> "events.*.public.users.*"
//   - "public.*.*" -> "events.*.public.*.*"
func (np *NATSProvider) patternToSubject(pattern string) string {
	if pattern == "*" {
		return np.subjectPrefix + ".>"
	}

	// For specific patterns, we need to match the event type structure
	// Event type: schema.entity.operation
	// NATS subject: events.{source}.{schema}.{entity}.{operation}
	// We use wildcard for source since pattern doesn't include it
	return fmt.Sprintf("%s.*.%s", np.subjectPrefix, pattern)
}

// matchesFilter checks if an event matches the filter criteria
func (np *NATSProvider) matchesFilter(event *Event, filter *EventFilter) bool {
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
