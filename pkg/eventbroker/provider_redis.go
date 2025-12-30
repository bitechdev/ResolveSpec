package eventbroker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// RedisProvider implements Provider interface using Redis Streams
// Features:
// - Persistent event storage using Redis Streams
// - Cross-instance pub/sub using consumer groups
// - Pattern-based subscription routing
// - Automatic stream trimming to prevent unbounded growth
type RedisProvider struct {
	client        *redis.Client
	streamName    string
	consumerGroup string
	consumerName  string
	instanceID    string
	maxLen        int64

	// Subscriptions
	mu          sync.RWMutex
	subscribers map[string]*redisSubscription

	// Statistics
	stats RedisProviderStats

	// Lifecycle
	stopListeners chan struct{}
	wg            sync.WaitGroup
	isRunning     atomic.Bool
}

// RedisProviderStats contains statistics for the Redis provider
type RedisProviderStats struct {
	TotalEvents       atomic.Int64
	EventsPublished   atomic.Int64
	EventsConsumed    atomic.Int64
	ActiveSubscribers atomic.Int32
	ConsumerErrors    atomic.Int64
}

// redisSubscription represents a single subscription
type redisSubscription struct {
	pattern string
	ch      chan *Event
	ctx     context.Context
	cancel  context.CancelFunc
}

// RedisProviderConfig configures the Redis provider
type RedisProviderConfig struct {
	Host          string
	Port          int
	Password      string
	DB            int
	StreamName    string
	ConsumerGroup string
	ConsumerName  string
	InstanceID    string
	MaxLen        int64 // Maximum stream length (0 = unlimited)
}

// NewRedisProvider creates a new Redis event provider
func NewRedisProvider(cfg RedisProviderConfig) (*RedisProvider, error) {
	// Apply defaults
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 6379
	}
	if cfg.StreamName == "" {
		cfg.StreamName = "resolvespec:events"
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = "resolvespec-workers"
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = cfg.InstanceID
	}
	if cfg.MaxLen == 0 {
		cfg.MaxLen = 10000 // Default max stream length
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: 10,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rp := &RedisProvider{
		client:        client,
		streamName:    cfg.StreamName,
		consumerGroup: cfg.ConsumerGroup,
		consumerName:  cfg.ConsumerName,
		instanceID:    cfg.InstanceID,
		maxLen:        cfg.MaxLen,
		subscribers:   make(map[string]*redisSubscription),
		stopListeners: make(chan struct{}),
	}

	rp.isRunning.Store(true)

	// Create consumer group if it doesn't exist
	if err := rp.ensureConsumerGroup(ctx); err != nil {
		logger.Warn("Failed to create consumer group: %v (may already exist)", err)
	}

	logger.Info("Redis provider initialized (stream: %s, consumer_group: %s, consumer: %s)",
		cfg.StreamName, cfg.ConsumerGroup, cfg.ConsumerName)

	return rp, nil
}

// Store stores an event
func (rp *RedisProvider) Store(ctx context.Context, event *Event) error {
	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Store in Redis Stream
	args := &redis.XAddArgs{
		Stream: rp.streamName,
		MaxLen: rp.maxLen,
		Approx: true, // Use approximate trimming for better performance
		Values: map[string]interface{}{
			"event":       data,
			"id":          event.ID,
			"type":        event.Type,
			"source":      string(event.Source),
			"status":      string(event.Status),
			"instance_id": event.InstanceID,
		},
	}

	if _, err := rp.client.XAdd(ctx, args).Result(); err != nil {
		return fmt.Errorf("failed to add event to stream: %w", err)
	}

	rp.stats.TotalEvents.Add(1)
	return nil
}

// Get retrieves an event by ID
// Note: This scans the stream which can be slow for large streams
// Consider using a separate hash for fast lookups if needed
func (rp *RedisProvider) Get(ctx context.Context, id string) (*Event, error) {
	// Scan stream for event with matching ID
	args := &redis.XReadArgs{
		Streams: []string{rp.streamName, "0"},
		Count:   1000, // Read in batches
	}

	for {
		streams, err := rp.client.XRead(ctx, args).Result()
		if err == redis.Nil {
			return nil, fmt.Errorf("event not found: %s", id)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read stream: %w", err)
		}

		if len(streams) == 0 {
			return nil, fmt.Errorf("event not found: %s", id)
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				// Check if this is the event we're looking for
				if eventID, ok := message.Values["id"].(string); ok && eventID == id {
					// Parse event
					if eventData, ok := message.Values["event"].(string); ok {
						var event Event
						if err := json.Unmarshal([]byte(eventData), &event); err != nil {
							return nil, fmt.Errorf("failed to unmarshal event: %w", err)
						}
						return &event, nil
					}
				}
			}

			// If we've read messages, update start position for next iteration
			if len(stream.Messages) > 0 {
				args.Streams[1] = stream.Messages[len(stream.Messages)-1].ID
			} else {
				// No more messages
				return nil, fmt.Errorf("event not found: %s", id)
			}
		}
	}
}

// List lists events with optional filters
// Note: This scans the entire stream which can be slow
// Consider using time-based or ID-based ranges for better performance
func (rp *RedisProvider) List(ctx context.Context, filter *EventFilter) ([]*Event, error) {
	var results []*Event

	// Read from stream
	args := &redis.XReadArgs{
		Streams: []string{rp.streamName, "0"},
		Count:   1000,
	}

	for {
		streams, err := rp.client.XRead(ctx, args).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read stream: %w", err)
		}

		if len(streams) == 0 {
			break
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				if eventData, ok := message.Values["event"].(string); ok {
					var event Event
					if err := json.Unmarshal([]byte(eventData), &event); err != nil {
						logger.Warn("Failed to unmarshal event: %v", err)
						continue
					}

					if rp.matchesFilter(&event, filter) {
						results = append(results, &event)
					}
				}
			}

			// Update start position for next iteration
			if len(stream.Messages) > 0 {
				args.Streams[1] = stream.Messages[len(stream.Messages)-1].ID
			} else {
				// No more messages
				goto done
			}
		}
	}

done:
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
// Note: Redis Streams are append-only, so we need to store status updates separately
// This uses a separate hash for status tracking
func (rp *RedisProvider) UpdateStatus(ctx context.Context, id string, status EventStatus, errorMsg string) error {
	statusKey := fmt.Sprintf("%s:status:%s", rp.streamName, id)

	fields := map[string]interface{}{
		"status":     string(status),
		"updated_at": time.Now().Format(time.RFC3339),
	}

	if errorMsg != "" {
		fields["error"] = errorMsg
	}

	if err := rp.client.HSet(ctx, statusKey, fields).Err(); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Set TTL on status key to prevent unbounded growth
	rp.client.Expire(ctx, statusKey, 7*24*time.Hour) // 7 days

	return nil
}

// Delete deletes an event by ID
// Note: Redis Streams don't support deletion by field value
// This marks the event as deleted in a separate set
func (rp *RedisProvider) Delete(ctx context.Context, id string) error {
	deletedKey := fmt.Sprintf("%s:deleted", rp.streamName)

	if err := rp.client.SAdd(ctx, deletedKey, id).Err(); err != nil {
		return fmt.Errorf("failed to mark event as deleted: %w", err)
	}

	// Also delete the status hash if it exists
	statusKey := fmt.Sprintf("%s:status:%s", rp.streamName, id)
	rp.client.Del(ctx, statusKey)

	return nil
}

// Stream returns a channel of events for real-time consumption
// Uses Redis Streams consumer group for distributed processing
func (rp *RedisProvider) Stream(ctx context.Context, pattern string) (<-chan *Event, error) {
	ch := make(chan *Event, 100)

	subCtx, cancel := context.WithCancel(ctx)

	sub := &redisSubscription{
		pattern: pattern,
		ch:      ch,
		ctx:     subCtx,
		cancel:  cancel,
	}

	rp.mu.Lock()
	rp.subscribers[pattern] = sub
	rp.stats.ActiveSubscribers.Add(1)
	rp.mu.Unlock()

	// Start consumer goroutine
	rp.wg.Add(1)
	go rp.consumeStream(sub)

	return ch, nil
}

// Publish publishes an event to all subscribers (cross-instance)
func (rp *RedisProvider) Publish(ctx context.Context, event *Event) error {
	// Store the event first
	if err := rp.Store(ctx, event); err != nil {
		return err
	}

	rp.stats.EventsPublished.Add(1)
	return nil
}

// Close closes the provider and releases resources
func (rp *RedisProvider) Close() error {
	if !rp.isRunning.Load() {
		return nil
	}

	rp.isRunning.Store(false)

	// Cancel all subscriptions
	rp.mu.Lock()
	for _, sub := range rp.subscribers {
		sub.cancel()
	}
	rp.mu.Unlock()

	// Stop listeners
	close(rp.stopListeners)

	// Wait for goroutines
	rp.wg.Wait()

	// Close Redis client
	if err := rp.client.Close(); err != nil {
		return fmt.Errorf("failed to close Redis client: %w", err)
	}

	logger.Info("Redis provider closed")
	return nil
}

// Stats returns provider statistics
func (rp *RedisProvider) Stats(ctx context.Context) (*ProviderStats, error) {
	// Get stream info
	streamInfo, err := rp.client.XInfoStream(ctx, rp.streamName).Result()
	if err != nil && err != redis.Nil {
		logger.Warn("Failed to get stream info: %v", err)
	}

	stats := &ProviderStats{
		ProviderType:      "redis",
		TotalEvents:       rp.stats.TotalEvents.Load(),
		EventsPublished:   rp.stats.EventsPublished.Load(),
		EventsConsumed:    rp.stats.EventsConsumed.Load(),
		ActiveSubscribers: int(rp.stats.ActiveSubscribers.Load()),
		ProviderSpecific: map[string]interface{}{
			"stream_name":     rp.streamName,
			"consumer_group":  rp.consumerGroup,
			"consumer_name":   rp.consumerName,
			"max_len":         rp.maxLen,
			"consumer_errors": rp.stats.ConsumerErrors.Load(),
		},
	}

	if streamInfo != nil {
		stats.ProviderSpecific["stream_length"] = streamInfo.Length
		stats.ProviderSpecific["first_entry_id"] = streamInfo.FirstEntry.ID
		stats.ProviderSpecific["last_entry_id"] = streamInfo.LastEntry.ID
	}

	return stats, nil
}

// consumeStream consumes events from the Redis Stream for a subscription
func (rp *RedisProvider) consumeStream(sub *redisSubscription) {
	defer rp.wg.Done()
	defer close(sub.ch)
	defer func() {
		rp.mu.Lock()
		delete(rp.subscribers, sub.pattern)
		rp.stats.ActiveSubscribers.Add(-1)
		rp.mu.Unlock()
	}()

	logger.Debug("Starting stream consumer for pattern: %s", sub.pattern)

	// Use consumer group for distributed processing
	for {
		select {
		case <-sub.ctx.Done():
			logger.Debug("Stream consumer stopped for pattern: %s", sub.pattern)
			return
		default:
			// Read from consumer group
			args := &redis.XReadGroupArgs{
				Group:    rp.consumerGroup,
				Consumer: rp.consumerName,
				Streams:  []string{rp.streamName, ">"},
				Count:    10,
				Block:    1 * time.Second,
			}

			streams, err := rp.client.XReadGroup(sub.ctx, args).Result()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				if sub.ctx.Err() != nil {
					return
				}
				rp.stats.ConsumerErrors.Add(1)
				logger.Warn("Failed to read from consumer group: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			for _, stream := range streams {
				for _, message := range stream.Messages {
					if eventData, ok := message.Values["event"].(string); ok {
						var event Event
						if err := json.Unmarshal([]byte(eventData), &event); err != nil {
							logger.Warn("Failed to unmarshal event: %v", err)
							// Acknowledge message anyway to prevent redelivery
							rp.client.XAck(sub.ctx, rp.streamName, rp.consumerGroup, message.ID)
							continue
						}

						// Check if event matches pattern
						if matchPattern(sub.pattern, event.Type) {
							select {
							case sub.ch <- &event:
								rp.stats.EventsConsumed.Add(1)
								// Acknowledge message
								rp.client.XAck(sub.ctx, rp.streamName, rp.consumerGroup, message.ID)
							case <-sub.ctx.Done():
								return
							}
						} else {
							// Acknowledge message even if it doesn't match pattern
							rp.client.XAck(sub.ctx, rp.streamName, rp.consumerGroup, message.ID)
						}
					}
				}
			}
		}
	}
}

// ensureConsumerGroup creates the consumer group if it doesn't exist
func (rp *RedisProvider) ensureConsumerGroup(ctx context.Context) error {
	// Try to create the stream and consumer group
	// MKSTREAM creates the stream if it doesn't exist
	err := rp.client.XGroupCreateMkStream(ctx, rp.streamName, rp.consumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// matchesFilter checks if an event matches the filter criteria
func (rp *RedisProvider) matchesFilter(event *Event, filter *EventFilter) bool {
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
