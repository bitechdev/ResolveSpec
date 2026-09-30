package providers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// PostgresProvider implements Provider for PostgreSQL databases
type PostgresProvider struct {
	db        *sql.DB
	connector *pgConnector
	config    ConnectionConfig
	listener  *PostgresListener
	mu        sync.Mutex
	opened    atomic.Int64
}

// NewPostgresProvider creates a new PostgreSQL provider
func NewPostgresProvider() *PostgresProvider {
	return &PostgresProvider{}
}

// Connect establishes a PostgreSQL connection
func (p *PostgresProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	connCfg, err := buildPGXConfig(cfg)
	if err != nil {
		return err
	}

	// The connector and *sql.DB are created once; the pool is never closed to
	// recover from errors (see Refresh).
	connector := newPGConnector(connCfg)
	db := sql.OpenDB(&countingConnector{Connector: connector, opened: &p.opened})

	// Connect with retry logic
	var lastErr error
	retryAttempts, retryDelay, retryMaxDelay := retryPolicy(cfg)

	connected := false
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, retryMaxDelay)
			if cfg.GetEnableLogging() {
				logger.Info("Retrying PostgreSQL connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				db.Close() //nolint:gosec // G104: best-effort call, error intentionally ignored
				return ctx.Err()
			}
		}

		// Test the connection with context timeout
		connectCtx, cancel := context.WithTimeout(ctx, cfg.GetConnectTimeout())
		err = db.PingContext(connectCtx)
		cancel()

		if err != nil {
			lastErr = err
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to ping PostgreSQL database: %v", err)
			}
			continue
		}

		connected = true
		break
	}

	if !connected {
		db.Close() //nolint:gosec // G104: best-effort call, error intentionally ignored
		return fmt.Errorf("failed to connect after %d attempts: %w", retryAttempts, lastErr)
	}

	// Configure connection pool
	if cfg.GetMaxOpenConns() != nil {
		db.SetMaxOpenConns(*cfg.GetMaxOpenConns())
	}
	if cfg.GetMaxIdleConns() != nil {
		db.SetMaxIdleConns(*cfg.GetMaxIdleConns())
	}
	if cfg.GetConnMaxLifetime() != nil {
		db.SetConnMaxLifetime(*cfg.GetConnMaxLifetime())
	}
	if cfg.GetConnMaxIdleTime() != nil {
		db.SetConnMaxIdleTime(*cfg.GetConnMaxIdleTime())
	}

	p.db = db
	p.connector = connector
	p.config = cfg

	if cfg.GetEnableLogging() {
		logger.Info("PostgreSQL connection established: name=%s, host=%s, database=%s", cfg.GetName(), cfg.GetHost(), cfg.GetDatabase())
	}

	return nil
}

// Refresh retires every pooled connection and dials fresh ones on demand,
// without closing the *sql.DB. Handles already handed out keep working:
// connections in use finish their current query and are then discarded.
func (p *PostgresProvider) Refresh(ctx context.Context) error {
	if p.db == nil || p.connector == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	connCfg, err := buildPGXConfig(p.config)
	if err != nil {
		return err
	}
	p.connector.swap(connCfg)

	pingCtx, cancel := context.WithTimeout(ctx, p.config.GetConnectTimeout())
	defer cancel()
	if err := p.db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("failed to ping after refresh: %w", err)
	}
	return nil
}

// Close closes the PostgreSQL connection. A listener failure does not stop the
// pool from being closed.
func (p *PostgresProvider) Close() error {
	var errs []error

	p.mu.Lock()
	listener := p.listener
	p.listener = nil
	p.mu.Unlock()

	if listener != nil {
		if err := listener.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close listener: %w", err))
		}
	}

	if p.db != nil {
		if err := p.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close PostgreSQL connection: %w", err))
		} else if p.config.GetEnableLogging() {
			logger.Info("PostgreSQL connection closed: name=%s", p.config.GetName())
		}
		p.db = nil
		p.connector = nil
	}

	return errors.Join(errs...)
}

// HealthCheck verifies the PostgreSQL connection is alive
func (p *PostgresProvider) HealthCheck(ctx context.Context) error {
	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Use a short timeout for health checks
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := p.db.PingContext(healthCtx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// GetNative returns the native *sql.DB connection
func (p *PostgresProvider) GetNative() (*sql.DB, error) {
	if p.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}
	return p.db, nil
}

// GetMongo returns an error for PostgreSQL (not a MongoDB connection)
func (p *PostgresProvider) GetMongo() (*mongo.Client, error) {
	return nil, ErrNotMongoDB
}

// Stats returns connection pool statistics
func (p *PostgresProvider) Stats() *ConnectionStats {
	if p.db == nil {
		return &ConnectionStats{
			Name:      p.config.GetName(),
			Type:      "postgres",
			Connected: false,
		}
	}

	stats := p.db.Stats()

	return &ConnectionStats{
		Name:               p.config.GetName(),
		Type:               "postgres",
		Connected:          true,
		OpenConnections:    stats.OpenConnections,
		MaxOpenConnections: stats.MaxOpenConnections,
		TotalOpened:        p.opened.Load(),
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDuration:       stats.WaitDuration,
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}
}

// GetListener returns a PostgreSQL listener for NOTIFY/LISTEN functionality
// The listener is lazily initialized on first call and reused for subsequent calls
func (p *PostgresProvider) GetListener(ctx context.Context) (*PostgresListener, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return existing listener if already created
	if p.listener != nil {
		return p.listener, nil
	}

	// Create new listener
	listener := NewPostgresListener(p.config)

	// Connect the listener
	if err := listener.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect listener: %w", err)
	}

	p.listener = listener
	return p.listener, nil
}

// calculateBackoff calculates exponential backoff delay
func calculateBackoff(attempt int, initial, maxDelay time.Duration) time.Duration {
	delay := initial * time.Duration(math.Pow(2, float64(attempt)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}
