package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// NotificationHandler is called when a notification is received
type NotificationHandler func(channel string, payload string)

// PostgresListener manages PostgreSQL LISTEN/NOTIFY functionality
type PostgresListener struct {
	config ConnectionConfig
	conn   *pgx.Conn

	// Channel subscriptions
	channels map[string]NotificationHandler
	mu       sync.RWMutex

	// Lifecycle management
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	closeMu    sync.Mutex
	reconnectC chan struct{}
}

// NewPostgresListener creates a new PostgreSQL listener
func NewPostgresListener(cfg ConnectionConfig) *PostgresListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &PostgresListener{
		config:     cfg,
		channels:   make(map[string]NotificationHandler),
		ctx:        ctx,
		cancel:     cancel,
		reconnectC: make(chan struct{}, 1),
	}
}

// Connect establishes a dedicated connection for listening
func (l *PostgresListener) Connect(ctx context.Context) error {
	dsn, err := l.config.BuildDSN()
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	// Parse connection config
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Connect with retry logic
	var conn *pgx.Conn
	var lastErr error

	retryAttempts := 3
	retryDelay := 1 * time.Second

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, 10*time.Second)
			if l.config.GetEnableLogging() {
				logger.Info("Retrying PostgreSQL listener connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		conn, err = pgx.ConnectConfig(ctx, connConfig)
		if err != nil {
			lastErr = err
			if l.config.GetEnableLogging() {
				logger.Warn("Failed to connect PostgreSQL listener", "error", err)
			}
			continue
		}

		// Test the connection
		if err = conn.Ping(ctx); err != nil {
			lastErr = err
			conn.Close(ctx)
			if l.config.GetEnableLogging() {
				logger.Warn("Failed to ping PostgreSQL listener", "error", err)
			}
			continue
		}

		// Connection successful
		break
	}

	if err != nil {
		return fmt.Errorf("failed to connect listener after %d attempts: %w", retryAttempts, lastErr)
	}

	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()

	// Start notification handler
	go l.handleNotifications()

	// Start reconnection handler
	go l.handleReconnection()

	if l.config.GetEnableLogging() {
		logger.Info("PostgreSQL listener connected: name=%s", l.config.GetName())
	}

	return nil
}

// Listen subscribes to a PostgreSQL notification channel
func (l *PostgresListener) Listen(channel string, handler NotificationHandler) error {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return fmt.Errorf("listener is closed")
	}
	l.closeMu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		return fmt.Errorf("listener connection is not initialized")
	}

	// Execute LISTEN command
	_, err := l.conn.Exec(l.ctx, fmt.Sprintf("LISTEN %s", pgx.Identifier{channel}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to listen on channel %s: %w", channel, err)
	}

	// Store the handler
	l.channels[channel] = handler

	if l.config.GetEnableLogging() {
		logger.Info("Listening on channel: name=%s, channel=%s", l.config.GetName(), channel)
	}

	return nil
}

// Unlisten unsubscribes from a PostgreSQL notification channel
func (l *PostgresListener) Unlisten(channel string) error {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return fmt.Errorf("listener is closed")
	}
	l.closeMu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		return fmt.Errorf("listener connection is not initialized")
	}

	// Execute UNLISTEN command
	_, err := l.conn.Exec(l.ctx, fmt.Sprintf("UNLISTEN %s", pgx.Identifier{channel}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to unlisten from channel %s: %w", channel, err)
	}

	// Remove the handler
	delete(l.channels, channel)

	if l.config.GetEnableLogging() {
		logger.Info("Unlistened from channel: name=%s, channel=%s", l.config.GetName(), channel)
	}

	return nil
}

// Notify sends a notification to a PostgreSQL channel
func (l *PostgresListener) Notify(ctx context.Context, channel string, payload string) error {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return fmt.Errorf("listener is closed")
	}
	l.closeMu.Unlock()

	l.mu.RLock()
	conn := l.conn
	l.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("listener connection is not initialized")
	}

	// Execute NOTIFY command
	_, err := conn.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload)
	if err != nil {
		return fmt.Errorf("failed to notify channel %s: %w", channel, err)
	}

	return nil
}

// Close closes the listener and all subscriptions
func (l *PostgresListener) Close() error {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return nil
	}
	l.closed = true
	l.closeMu.Unlock()

	// Cancel context to stop background goroutines
	l.cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		return nil
	}

	// Unlisten from all channels
	for channel := range l.channels {
		_, _ = l.conn.Exec(context.Background(), fmt.Sprintf("UNLISTEN %s", pgx.Identifier{channel}.Sanitize()))
	}

	// Close connection
	err := l.conn.Close(context.Background())
	if err != nil {
		return fmt.Errorf("failed to close listener connection: %w", err)
	}

	l.conn = nil
	l.channels = make(map[string]NotificationHandler)

	if l.config.GetEnableLogging() {
		logger.Info("PostgreSQL listener closed: name=%s", l.config.GetName())
	}

	return nil
}

// handleNotifications processes incoming notifications
func (l *PostgresListener) handleNotifications() {
	for {
		select {
		case <-l.ctx.Done():
			return
		default:
		}

		l.mu.RLock()
		conn := l.conn
		l.mu.RUnlock()

		if conn == nil {
			// Connection not available, wait for reconnection
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Wait for notification with timeout
		ctx, cancel := context.WithTimeout(l.ctx, 5*time.Second)
		notification, err := conn.WaitForNotification(ctx)
		cancel()

		if err != nil {
			// Check if context was cancelled
			if l.ctx.Err() != nil {
				return
			}

			// Check if it's a connection error
			if pgconn.Timeout(err) {
				// Timeout is normal, continue waiting
				continue
			}

			// Connection error, trigger reconnection
			if l.config.GetEnableLogging() {
				logger.Warn("Notification error, triggering reconnection", "error", err)
			}
			select {
			case l.reconnectC <- struct{}{}:
			default:
			}
			time.Sleep(1 * time.Second)
			continue
		}

		// Process notification
		l.mu.RLock()
		handler, exists := l.channels[notification.Channel]
		l.mu.RUnlock()

		if exists && handler != nil {
			// Call handler in a goroutine to avoid blocking
			go func(ch, payload string) {
				defer func() {
					if r := recover(); r != nil {
						if l.config.GetEnableLogging() {
							logger.Error("Notification handler panic: channel=%s, error=%v", ch, r)
						}
					}
				}()
				handler(ch, payload)
			}(notification.Channel, notification.Payload)
		}
	}
}

// handleReconnection manages automatic reconnection
func (l *PostgresListener) handleReconnection() {
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.reconnectC:
			if l.config.GetEnableLogging() {
				logger.Info("Attempting to reconnect listener: name=%s", l.config.GetName())
			}

			// Close existing connection
			l.mu.Lock()
			if l.conn != nil {
				l.conn.Close(context.Background())
				l.conn = nil
			}

			// Save current subscriptions
			channels := make(map[string]NotificationHandler)
			for ch, handler := range l.channels {
				channels[ch] = handler
			}
			l.mu.Unlock()

			// Attempt reconnection
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := l.Connect(ctx)
			cancel()

			if err != nil {
				if l.config.GetEnableLogging() {
					logger.Error("Failed to reconnect listener: name=%s, error=%v", l.config.GetName(), err)
				}
				// Retry after delay
				time.Sleep(5 * time.Second)
				select {
				case l.reconnectC <- struct{}{}:
				default:
				}
				continue
			}

			// Resubscribe to all channels
			for channel, handler := range channels {
				if err := l.Listen(channel, handler); err != nil {
					if l.config.GetEnableLogging() {
						logger.Error("Failed to resubscribe to channel: name=%s, channel=%s, error=%v", l.config.GetName(), channel, err)
					}
				}
			}

			if l.config.GetEnableLogging() {
				logger.Info("Listener reconnected successfully: name=%s", l.config.GetName())
			}
		}
	}
}

// IsConnected returns true if the listener is connected
func (l *PostgresListener) IsConnected() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.conn != nil
}

// Channels returns the list of channels currently being listened to
func (l *PostgresListener) Channels() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	channels := make([]string, 0, len(l.channels))
	for ch := range l.channels {
		channels = append(channels, ch)
	}
	return channels
}
