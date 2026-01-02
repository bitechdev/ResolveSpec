package providers

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// PostgresProvider implements Provider for PostgreSQL databases
type PostgresProvider struct {
	db       *sql.DB
	config   ConnectionConfig
	listener *PostgresListener
	mu       sync.Mutex
}

// NewPostgresProvider creates a new PostgreSQL provider
func NewPostgresProvider() *PostgresProvider {
	return &PostgresProvider{}
}

// Connect establishes a PostgreSQL connection
func (p *PostgresProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	// Build DSN
	dsn, err := cfg.BuildDSN()
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	// Connect with retry logic
	var db *sql.DB
	var lastErr error

	retryAttempts := 3 // Default retry attempts
	retryDelay := 1 * time.Second

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, 10*time.Second)
			if cfg.GetEnableLogging() {
				logger.Info("Retrying PostgreSQL connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Open database connection
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			lastErr = err
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to open PostgreSQL connection", "error", err)
			}
			continue
		}

		// Test the connection with context timeout
		connectCtx, cancel := context.WithTimeout(ctx, cfg.GetConnectTimeout())
		err = db.PingContext(connectCtx)
		cancel()

		if err != nil {
			lastErr = err
			db.Close()
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to ping PostgreSQL database", "error", err)
			}
			continue
		}

		// Connection successful
		break
	}

	if err != nil {
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
	p.config = cfg

	if cfg.GetEnableLogging() {
		logger.Info("PostgreSQL connection established: name=%s, host=%s, database=%s", cfg.GetName(), cfg.GetHost(), cfg.GetDatabase())
	}

	return nil
}

// Close closes the PostgreSQL connection
func (p *PostgresProvider) Close() error {
	// Close listener if it exists
	p.mu.Lock()
	if p.listener != nil {
		if err := p.listener.Close(); err != nil {
			p.mu.Unlock()
			return fmt.Errorf("failed to close listener: %w", err)
		}
		p.listener = nil
	}
	p.mu.Unlock()

	if p.db == nil {
		return nil
	}

	err := p.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close PostgreSQL connection: %w", err)
	}

	if p.config.GetEnableLogging() {
		logger.Info("PostgreSQL connection closed: name=%s", p.config.GetName())
	}

	p.db = nil
	return nil
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
		Name:              p.config.GetName(),
		Type:              "postgres",
		Connected:         true,
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
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
