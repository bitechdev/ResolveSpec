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
	// connMu serialises use of the single pgx.Conn: it is not safe for
	// concurrent use, so the notification wait and LISTEN/UNLISTEN/NOTIFY take
	// turns. Lock order: connMu before mu.
	connMu sync.Mutex

	// Lifecycle management
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	closeMu    sync.Mutex
	reconnectC chan struct{}
	startOnce  sync.Once // background goroutines start exactly once
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

// Connect establishes a dedicated connection for listening and starts the
// background loops (once per listener).
func (l *PostgresListener) Connect(ctx context.Context) error {
	conn, err := l.dial(ctx)
	if err != nil {
		return err
	}

	l.swapConn(conn)

	l.startOnce.Do(func() {
		go l.handleNotifications()
		go l.handleReconnection()
	})

	if l.config.GetEnableLogging() {
		logger.Info("PostgreSQL listener connected: name=%s", l.config.GetName())
	}

	return nil
}

// dial opens and verifies a new dedicated connection, with retries.
func (l *PostgresListener) dial(ctx context.Context) (*pgx.Conn, error) {
	connConfig, err := buildPGXConfig(l.config)
	if err != nil {
		return nil, err
	}

	var lastErr error

	retryAttempts, retryDelay, retryMaxDelay := retryPolicy(l.config)

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, retryMaxDelay)
			if l.config.GetEnableLogging() {
				logger.Info("Retrying PostgreSQL listener connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		conn, err := pgx.ConnectConfig(ctx, connConfig)
		if err != nil {
			lastErr = err
			if l.config.GetEnableLogging() {
				logger.Warn("Failed to connect PostgreSQL listener: %v", err)
			}
			continue
		}

		// Test the connection
		if err = conn.Ping(ctx); err != nil {
			lastErr = err
			_ = closeConnBounded(conn)
			if l.config.GetEnableLogging() {
				logger.Warn("Failed to ping PostgreSQL listener: %v", err)
			}
			continue
		}

		return conn, nil
	}

	return nil, fmt.Errorf("failed to connect listener after %d attempts: %w", retryAttempts, lastErr)
}

// closeConnBounded closes a pgx connection without ever waiting on a dead socket.
func closeConnBounded(conn *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), listenerCloseTimeout)
	defer cancel()
	return conn.Close(ctx)
}

const (
	listenerCloseTimeout     = 2 * time.Second
	notificationPollInterval = 500 * time.Millisecond
)

// currentConn returns the live connection, or an error if the listener is
// closed or not yet connected.
func (l *PostgresListener) currentConn() (*pgx.Conn, error) {
	l.closeMu.Lock()
	closed := l.closed
	l.closeMu.Unlock()
	if closed {
		return nil, fmt.Errorf("listener is closed")
	}

	l.mu.RLock()
	conn := l.conn
	l.mu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("listener connection is not initialized")
	}
	return conn, nil
}

// Listen subscribes to a PostgreSQL notification channel
func (l *PostgresListener) Listen(channel string, handler NotificationHandler) error {
	// Take the connection between notification waits (each wait is short).
	l.connMu.Lock()
	defer l.connMu.Unlock()

	conn, err := l.currentConn()
	if err != nil {
		return err
	}

	if _, err := conn.Exec(l.ctx, fmt.Sprintf("LISTEN %s", pgx.Identifier{channel}.Sanitize())); err != nil {
		return fmt.Errorf("failed to listen on channel %s: %w", channel, err)
	}

	l.mu.Lock()
	l.channels[channel] = handler
	l.mu.Unlock()

	if l.config.GetEnableLogging() {
		logger.Info("Listening on channel: name=%s, channel=%s", l.config.GetName(), channel)
	}

	return nil
}

// Unlisten unsubscribes from a PostgreSQL notification channel
func (l *PostgresListener) Unlisten(channel string) error {
	l.connMu.Lock()
	defer l.connMu.Unlock()

	conn, err := l.currentConn()
	if err != nil {
		return err
	}

	if _, err := conn.Exec(l.ctx, fmt.Sprintf("UNLISTEN %s", pgx.Identifier{channel}.Sanitize())); err != nil {
		return fmt.Errorf("failed to unlisten from channel %s: %w", channel, err)
	}

	l.mu.Lock()
	delete(l.channels, channel)
	l.mu.Unlock()

	if l.config.GetEnableLogging() {
		logger.Info("Unlistened from channel: name=%s, channel=%s", l.config.GetName(), channel)
	}

	return nil
}

// Notify sends a notification to a PostgreSQL channel
func (l *PostgresListener) Notify(ctx context.Context, channel string, payload string) error {
	l.connMu.Lock()
	defer l.connMu.Unlock()

	conn, err := l.currentConn()
	if err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "SELECT pg_notify($1, $2)", channel, payload); err != nil {
		return fmt.Errorf("failed to notify channel %s: %w", channel, err)
	}

	return nil
}

// Close closes the listener and all subscriptions. Closing the connection drops
// every subscription server-side, so no UNLISTEN round trips are needed, and
// the close itself is bounded so a dead socket cannot hang the caller.
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

	// The cancelled ctx makes the notification wait return promptly, releasing
	// connMu; closing the conn while it is being read would race inside pgx.
	l.connMu.Lock()
	l.mu.Lock()
	conn := l.conn
	l.conn = nil
	l.channels = make(map[string]NotificationHandler)
	l.mu.Unlock()

	if conn == nil {
		l.connMu.Unlock()
		return nil
	}

	err := closeConnBounded(conn)
	l.connMu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to close listener connection: %w", err)
	}

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

		l.connMu.Lock()
		l.mu.RLock()
		conn := l.conn
		l.mu.RUnlock()

		if conn == nil {
			l.connMu.Unlock()
			// Connection not available, wait for reconnection
			if !l.sleep(100 * time.Millisecond) {
				return
			}
			continue
		}

		// Wait for a notification with a short timeout, so Listen/Unlisten/Notify
		// waiting on connMu are served promptly.
		ctx, cancel := context.WithTimeout(l.ctx, notificationPollInterval)
		notification, err := conn.WaitForNotification(ctx)
		cancel()
		l.connMu.Unlock()

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
				logger.Warn("Notification error, triggering reconnection: %v", err)
			}
			select {
			case l.reconnectC <- struct{}{}:
			default:
			}
			if !l.sleep(1 * time.Second) {
				return
			}
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

// sleep waits for d or until the listener is closed; it reports whether the
// listener is still running.
func (l *PostgresListener) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-l.ctx.Done():
		return false
	}
}

// handleReconnection manages automatic reconnection. It runs as a single
// goroutine and dials replacement connections directly rather than through the
// public Connect, so no extra loops are started.
func (l *PostgresListener) handleReconnection() {
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.reconnectC:
			if l.config.GetEnableLogging() {
				logger.Info("Attempting to reconnect listener: name=%s", l.config.GetName())
			}

			ctx, cancel := context.WithTimeout(l.ctx, 30*time.Second)
			err := l.reconnect(ctx)
			cancel()

			if err != nil {
				if l.ctx.Err() != nil {
					return
				}
				if l.config.GetEnableLogging() {
					logger.Error("Failed to reconnect listener: name=%s, error=%v", l.config.GetName(), err)
				}
				// Retry after delay
				if !l.sleep(5 * time.Second) {
					return
				}
				select {
				case l.reconnectC <- struct{}{}:
				default:
				}
				continue
			}

			if l.config.GetEnableLogging() {
				logger.Info("Listener reconnected successfully: name=%s", l.config.GetName())
			}
		}
	}
}

// reconnect replaces the connection and resubscribes every channel on the new
// connection before publishing it, so the notification loop never touches a
// half-initialised conn.
func (l *PostgresListener) reconnect(ctx context.Context) error {
	conn, err := l.dial(ctx)
	if err != nil {
		return err
	}

	l.mu.RLock()
	channels := make([]string, 0, len(l.channels))
	for ch := range l.channels {
		channels = append(channels, ch)
	}
	l.mu.RUnlock()

	for _, ch := range channels {
		if _, err := conn.Exec(ctx, fmt.Sprintf("LISTEN %s", pgx.Identifier{ch}.Sanitize())); err != nil {
			_ = closeConnBounded(conn)
			return fmt.Errorf("failed to resubscribe to channel %s: %w", ch, err)
		}
	}

	if l.ctx.Err() != nil {
		_ = closeConnBounded(conn)
		return l.ctx.Err()
	}
	l.swapConn(conn)
	return nil
}

// swapConn installs conn and closes the previous one. The old connection is
// closed under connMu so it is never closed while another goroutine is using it.
func (l *PostgresListener) swapConn(conn *pgx.Conn) {
	l.connMu.Lock()
	l.mu.Lock()
	old := l.conn
	l.conn = conn
	l.mu.Unlock()
	if old != nil {
		_ = closeConnBounded(old)
	}
	l.connMu.Unlock()
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
