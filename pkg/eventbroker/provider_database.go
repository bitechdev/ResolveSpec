package eventbroker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// DatabaseProvider implements Provider interface using SQL database
// Features:
// - Persistent event storage in database table
// - Full SQL query support for event history
// - PostgreSQL NOTIFY/LISTEN for real-time updates (optional)
// - Polling-based consumption with configurable interval
// - Good for audit trails and event replay
type DatabaseProvider struct {
	db           common.Database
	tableName    string
	channel      string // PostgreSQL NOTIFY channel name
	pollInterval time.Duration
	instanceID   string
	useNotify    bool // Whether to use PostgreSQL NOTIFY

	// Subscriptions
	mu          sync.RWMutex
	subscribers map[string]*dbSubscription

	// Statistics
	stats DatabaseProviderStats

	// Lifecycle
	stopPolling chan struct{}
	wg          sync.WaitGroup
	isRunning   atomic.Bool
}

// DatabaseProviderStats contains statistics for the database provider
type DatabaseProviderStats struct {
	TotalEvents       atomic.Int64
	EventsPublished   atomic.Int64
	EventsConsumed    atomic.Int64
	ActiveSubscribers atomic.Int32
	PollErrors        atomic.Int64
}

// dbSubscription represents a single database subscription
type dbSubscription struct {
	pattern    string
	ch         chan *Event
	lastSeenID string
	ctx        context.Context
	cancel     context.CancelFunc
}

// DatabaseProviderConfig configures the database provider
type DatabaseProviderConfig struct {
	DB           common.Database
	TableName    string
	Channel      string // PostgreSQL NOTIFY channel (optional)
	PollInterval time.Duration
	InstanceID   string
	UseNotify    bool // Enable PostgreSQL NOTIFY/LISTEN
}

// NewDatabaseProvider creates a new database event provider
func NewDatabaseProvider(cfg DatabaseProviderConfig) (*DatabaseProvider, error) {
	// Apply defaults
	if cfg.TableName == "" {
		cfg.TableName = "events"
	}
	if cfg.Channel == "" {
		cfg.Channel = "resolvespec_events"
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 1 * time.Second
	}

	dp := &DatabaseProvider{
		db:           cfg.DB,
		tableName:    cfg.TableName,
		channel:      cfg.Channel,
		pollInterval: cfg.PollInterval,
		instanceID:   cfg.InstanceID,
		useNotify:    cfg.UseNotify,
		subscribers:  make(map[string]*dbSubscription),
		stopPolling:  make(chan struct{}),
	}

	dp.isRunning.Store(true)

	// Create table if it doesn't exist
	ctx := context.Background()
	if err := dp.createTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to create events table: %w", err)
	}

	// Start polling goroutine for subscriptions
	dp.wg.Add(1)
	go dp.pollLoop()

	logger.Info("Database provider initialized (table: %s, poll_interval: %v, notify: %v)",
		cfg.TableName, cfg.PollInterval, cfg.UseNotify)

	return dp, nil
}

// Store stores an event
func (dp *DatabaseProvider) Store(ctx context.Context, event *Event) error {
	// Marshal metadata to JSON
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Insert event
	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, source, type, status, retry_count, error,
			payload, user_id, session_id, instance_id,
			schema, entity, operation,
			created_at, processed_at, completed_at, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13,
			$14, $15, $16, $17
		)
	`, dp.tableName)

	_, err = dp.db.Exec(ctx, query,
		event.ID, event.Source, event.Type, event.Status, event.RetryCount, event.Error,
		event.Payload, event.UserID, event.SessionID, event.InstanceID,
		event.Schema, event.Entity, event.Operation,
		event.CreatedAt, event.ProcessedAt, event.CompletedAt, metadataJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	dp.stats.TotalEvents.Add(1)
	return nil
}

// Get retrieves an event by ID
func (dp *DatabaseProvider) Get(ctx context.Context, id string) (*Event, error) {
	event := &Event{}
	var metadataJSON []byte
	var processedAt, completedAt sql.NullTime

	// Query into individual fields
	query := fmt.Sprintf(`
		SELECT id, source, type, status, retry_count, error,
		       payload, user_id, session_id, instance_id,
		       schema, entity, operation,
		       created_at, processed_at, completed_at, metadata
		FROM %s
		WHERE id = $1
	`, dp.tableName)

	var source, eventType, status, operation string

	// Execute raw query
	rows, err := dp.db.GetUnderlyingDB().(interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	}).QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query event: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("event not found: %s", id)
	}

	if err := rows.Scan(
		&event.ID, &source, &eventType, &status, &event.RetryCount, &event.Error,
		&event.Payload, &event.UserID, &event.SessionID, &event.InstanceID,
		&event.Schema, &event.Entity, &operation,
		&event.CreatedAt, &processedAt, &completedAt, &metadataJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to scan event: %w", err)
	}

	// Set enum values
	event.Source = EventSource(source)
	event.Type = eventType
	event.Status = EventStatus(status)
	event.Operation = operation

	// Handle nullable timestamps
	if processedAt.Valid {
		event.ProcessedAt = &processedAt.Time
	}
	if completedAt.Valid {
		event.CompletedAt = &completedAt.Time
	}

	// Unmarshal metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &event.Metadata); err != nil {
			logger.Warn("Failed to unmarshal metadata: %v", err)
		}
	}

	return event, nil
}

// List lists events with optional filters
func (dp *DatabaseProvider) List(ctx context.Context, filter *EventFilter) ([]*Event, error) {
	query := fmt.Sprintf("SELECT id, source, type, status, retry_count, error, "+
		"payload, user_id, session_id, instance_id, "+
		"schema, entity, operation, "+
		"created_at, processed_at, completed_at, metadata "+
		"FROM %s WHERE 1=1", dp.tableName)

	args := []interface{}{}
	argNum := 1

	// Build WHERE clause
	if filter != nil {
		if filter.Source != nil {
			query += fmt.Sprintf(" AND source = $%d", argNum)
			args = append(args, string(*filter.Source))
			argNum++
		}
		if filter.Status != nil {
			query += fmt.Sprintf(" AND status = $%d", argNum)
			args = append(args, string(*filter.Status))
			argNum++
		}
		if filter.UserID != nil {
			query += fmt.Sprintf(" AND user_id = $%d", argNum)
			args = append(args, *filter.UserID)
			argNum++
		}
		if filter.Schema != "" {
			query += fmt.Sprintf(" AND schema = $%d", argNum)
			args = append(args, filter.Schema)
			argNum++
		}
		if filter.Entity != "" {
			query += fmt.Sprintf(" AND entity = $%d", argNum)
			args = append(args, filter.Entity)
			argNum++
		}
		if filter.Operation != "" {
			query += fmt.Sprintf(" AND operation = $%d", argNum)
			args = append(args, filter.Operation)
			argNum++
		}
		if filter.InstanceID != "" {
			query += fmt.Sprintf(" AND instance_id = $%d", argNum)
			args = append(args, filter.InstanceID)
			argNum++
		}
		if filter.StartTime != nil {
			query += fmt.Sprintf(" AND created_at >= $%d", argNum)
			args = append(args, *filter.StartTime)
			argNum++
		}
		if filter.EndTime != nil {
			query += fmt.Sprintf(" AND created_at <= $%d", argNum)
			args = append(args, *filter.EndTime)
			argNum++
		}
	}

	// Add ORDER BY
	query += " ORDER BY created_at DESC"

	// Add LIMIT and OFFSET
	if filter != nil {
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", argNum)
			args = append(args, filter.Limit)
			argNum++
		}
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", argNum)
			args = append(args, filter.Offset)
		}
	}

	// Execute query
	rows, err := dp.db.GetUnderlyingDB().(interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	}).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var results []*Event
	for rows.Next() {
		event := &Event{}
		var source, eventType, status, operation string
		var metadataJSON []byte
		var processedAt, completedAt sql.NullTime

		err := rows.Scan(
			&event.ID, &source, &eventType, &status, &event.RetryCount, &event.Error,
			&event.Payload, &event.UserID, &event.SessionID, &event.InstanceID,
			&event.Schema, &event.Entity, &operation,
			&event.CreatedAt, &processedAt, &completedAt, &metadataJSON,
		)
		if err != nil {
			logger.Warn("Failed to scan event: %v", err)
			continue
		}

		// Set enum values
		event.Source = EventSource(source)
		event.Type = eventType
		event.Status = EventStatus(status)
		event.Operation = operation

		// Handle nullable timestamps
		if processedAt.Valid {
			event.ProcessedAt = &processedAt.Time
		}
		if completedAt.Valid {
			event.CompletedAt = &completedAt.Time
		}

		// Unmarshal metadata
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &event.Metadata); err != nil {
				logger.Warn("Failed to unmarshal metadata: %v", err)
			}
		}

		results = append(results, event)
	}

	return results, nil
}

// UpdateStatus updates the status of an event
func (dp *DatabaseProvider) UpdateStatus(ctx context.Context, id string, status EventStatus, errorMsg string) error {
	query := fmt.Sprintf(`
		UPDATE %s
		SET status = $1, error = $2
		WHERE id = $3
	`, dp.tableName)

	_, err := dp.db.Exec(ctx, query, string(status), errorMsg, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// Delete deletes an event by ID
func (dp *DatabaseProvider) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", dp.tableName)

	_, err := dp.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete event: %w", err)
	}

	dp.stats.TotalEvents.Add(-1)
	return nil
}

// Stream returns a channel of events for real-time consumption
func (dp *DatabaseProvider) Stream(ctx context.Context, pattern string) (<-chan *Event, error) {
	ch := make(chan *Event, 100)

	subCtx, cancel := context.WithCancel(ctx)

	sub := &dbSubscription{
		pattern:    pattern,
		ch:         ch,
		lastSeenID: "",
		ctx:        subCtx,
		cancel:     cancel,
	}

	dp.mu.Lock()
	dp.subscribers[pattern] = sub
	dp.stats.ActiveSubscribers.Add(1)
	dp.mu.Unlock()

	return ch, nil
}

// Publish publishes an event to all subscribers
func (dp *DatabaseProvider) Publish(ctx context.Context, event *Event) error {
	// Store the event first
	if err := dp.Store(ctx, event); err != nil {
		return err
	}

	dp.stats.EventsPublished.Add(1)

	// If using PostgreSQL NOTIFY, send notification
	if dp.useNotify {
		if err := dp.notify(ctx, event.ID); err != nil {
			logger.Warn("Failed to send NOTIFY: %v", err)
		}
	}

	return nil
}

// Close closes the provider and releases resources
func (dp *DatabaseProvider) Close() error {
	if !dp.isRunning.Load() {
		return nil
	}

	dp.isRunning.Store(false)

	// Cancel all subscriptions
	dp.mu.Lock()
	for _, sub := range dp.subscribers {
		sub.cancel()
	}
	dp.mu.Unlock()

	// Stop polling
	close(dp.stopPolling)

	// Wait for goroutines
	dp.wg.Wait()

	logger.Info("Database provider closed")
	return nil
}

// Stats returns provider statistics
func (dp *DatabaseProvider) Stats(ctx context.Context) (*ProviderStats, error) {
	// Get counts by status
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending') as pending,
			COUNT(*) FILTER (WHERE status = 'processing') as processing,
			COUNT(*) FILTER (WHERE status = 'completed') as completed,
			COUNT(*) FILTER (WHERE status = 'failed') as failed,
			COUNT(*) as total
		FROM %s
	`, dp.tableName)

	var pending, processing, completed, failed, total int64

	rows, err := dp.db.GetUnderlyingDB().(interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	}).QueryContext(ctx, query)
	if err != nil {
		logger.Warn("Failed to get stats: %v", err)
	} else {
		defer rows.Close()
		if rows.Next() {
			if err := rows.Scan(&pending, &processing, &completed, &failed, &total); err != nil {
				logger.Warn("Failed to scan stats: %v", err)
			}
		}
	}

	return &ProviderStats{
		ProviderType:      "database",
		TotalEvents:       total,
		PendingEvents:     pending,
		ProcessingEvents:  processing,
		CompletedEvents:   completed,
		FailedEvents:      failed,
		EventsPublished:   dp.stats.EventsPublished.Load(),
		EventsConsumed:    dp.stats.EventsConsumed.Load(),
		ActiveSubscribers: int(dp.stats.ActiveSubscribers.Load()),
		ProviderSpecific: map[string]interface{}{
			"table_name":    dp.tableName,
			"poll_interval": dp.pollInterval.String(),
			"use_notify":    dp.useNotify,
			"poll_errors":   dp.stats.PollErrors.Load(),
		},
	}, nil
}

// pollLoop periodically polls for new events
func (dp *DatabaseProvider) pollLoop() {
	defer dp.wg.Done()

	ticker := time.NewTicker(dp.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dp.pollEvents()
		case <-dp.stopPolling:
			return
		}
	}
}

// pollEvents polls for new events and delivers to subscribers
func (dp *DatabaseProvider) pollEvents() {
	dp.mu.RLock()
	subscribers := make([]*dbSubscription, 0, len(dp.subscribers))
	for _, sub := range dp.subscribers {
		subscribers = append(subscribers, sub)
	}
	dp.mu.RUnlock()

	for _, sub := range subscribers {
		// Query for new events since last seen
		query := fmt.Sprintf(`
			SELECT id, source, type, status, retry_count, error,
			       payload, user_id, session_id, instance_id,
			       schema, entity, operation,
			       created_at, processed_at, completed_at, metadata
			FROM %s
			WHERE id > $1
			ORDER BY created_at ASC
			LIMIT 100
		`, dp.tableName)

		lastSeenID := sub.lastSeenID
		if lastSeenID == "" {
			lastSeenID = "00000000-0000-0000-0000-000000000000"
		}

		rows, err := dp.db.GetUnderlyingDB().(interface {
			QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		}).QueryContext(sub.ctx, query, lastSeenID)
		if err != nil {
			dp.stats.PollErrors.Add(1)
			logger.Warn("Failed to poll events: %v", err)
			continue
		}

		for rows.Next() {
			event := &Event{}
			var source, eventType, status, operation string
			var metadataJSON []byte
			var processedAt, completedAt sql.NullTime

			err := rows.Scan(
				&event.ID, &source, &eventType, &status, &event.RetryCount, &event.Error,
				&event.Payload, &event.UserID, &event.SessionID, &event.InstanceID,
				&event.Schema, &event.Entity, &operation,
				&event.CreatedAt, &processedAt, &completedAt, &metadataJSON,
			)
			if err != nil {
				logger.Warn("Failed to scan event: %v", err)
				continue
			}

			// Set enum values
			event.Source = EventSource(source)
			event.Type = eventType
			event.Status = EventStatus(status)
			event.Operation = operation

			// Handle nullable timestamps
			if processedAt.Valid {
				event.ProcessedAt = &processedAt.Time
			}
			if completedAt.Valid {
				event.CompletedAt = &completedAt.Time
			}

			// Unmarshal metadata
			if len(metadataJSON) > 0 {
				if err := json.Unmarshal(metadataJSON, &event.Metadata); err != nil {
					logger.Warn("Failed to unmarshal metadata: %v", err)
				}
			}

			// Check if event matches pattern
			if matchPattern(sub.pattern, event.Type) {
				select {
				case sub.ch <- event:
					dp.stats.EventsConsumed.Add(1)
					sub.lastSeenID = event.ID
				case <-sub.ctx.Done():
					rows.Close()
					return
				default:
					// Channel full, skip
					logger.Warn("Subscriber channel full for pattern: %s", sub.pattern)
				}
			}

			sub.lastSeenID = event.ID
		}

		rows.Close()
	}
}

// notify sends a PostgreSQL NOTIFY message
func (dp *DatabaseProvider) notify(ctx context.Context, eventID string) error {
	query := fmt.Sprintf("NOTIFY %s, '%s'", dp.channel, eventID)
	_, err := dp.db.Exec(ctx, query)
	return err
}

// createTable creates the events table if it doesn't exist
func (dp *DatabaseProvider) createTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			source VARCHAR(50) NOT NULL,
			type VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL,
			retry_count INTEGER DEFAULT 0,
			error TEXT,
			payload JSONB,
			user_id INTEGER,
			session_id VARCHAR(255),
			instance_id VARCHAR(255),
			schema VARCHAR(255),
			entity VARCHAR(255),
			operation VARCHAR(50),
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			processed_at TIMESTAMP,
			completed_at TIMESTAMP,
			metadata JSONB
		)
	`, dp.tableName)

	if _, err := dp.db.Exec(ctx, query); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_source ON %s(source)", dp.tableName, dp.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_type ON %s(type)", dp.tableName, dp.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_status ON %s(status)", dp.tableName, dp.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s(created_at)", dp.tableName, dp.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_instance_id ON %s(instance_id)", dp.tableName, dp.tableName),
	}

	for _, indexQuery := range indexes {
		if _, err := dp.db.Exec(ctx, indexQuery); err != nil {
			logger.Warn("Failed to create index: %v", err)
		}
	}

	return nil
}
