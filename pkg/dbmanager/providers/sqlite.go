package providers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/glebarez/sqlite" // Pure Go SQLite driver
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// SQLiteProvider implements Provider for SQLite databases
type SQLiteProvider struct {
	db     *sql.DB
	config ConnectionConfig
}

// NewSQLiteProvider creates a new SQLite provider
func NewSQLiteProvider() *SQLiteProvider {
	return &SQLiteProvider{}
}

// Connect establishes a SQLite connection
func (p *SQLiteProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	// Build DSN
	dsn, err := cfg.BuildDSN()
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	// Open database connection
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open SQLite connection: %w", err)
	}

	// Test the connection with context timeout
	connectCtx, cancel := context.WithTimeout(ctx, cfg.GetConnectTimeout())
	err = db.PingContext(connectCtx)
	cancel()

	if err != nil {
		db.Close()
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// Configure connection pool
	// Note: SQLite works best with MaxOpenConns=1 for write operations
	// but can handle multiple readers
	if cfg.GetMaxOpenConns() != nil {
		db.SetMaxOpenConns(*cfg.GetMaxOpenConns())
	} else {
		// Default to 1 for SQLite to avoid "database is locked" errors
		db.SetMaxOpenConns(1)
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

	// Enable WAL mode for better concurrent access
	_, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL")
	if err != nil {
		if cfg.GetEnableLogging() {
			logger.Warn("Failed to enable WAL mode for SQLite", "error", err)
		}
		// Don't fail connection if WAL mode cannot be enabled
	}

	// Set busy timeout to handle locked database (minimum 2 minutes = 120000ms)
	busyTimeout := cfg.GetQueryTimeout().Milliseconds()
	if busyTimeout < 120000 {
		busyTimeout = 120000 // Enforce minimum of 2 minutes
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout))
	if err != nil {
		if cfg.GetEnableLogging() {
			logger.Warn("Failed to set busy timeout for SQLite", "error", err)
		}
	}

	p.db = db
	p.config = cfg

	if cfg.GetEnableLogging() {
		logger.Info("SQLite connection established: name=%s, filepath=%s", cfg.GetName(), cfg.GetFilePath())
	}

	return nil
}

// Close closes the SQLite connection
func (p *SQLiteProvider) Close() error {
	if p.db == nil {
		return nil
	}

	err := p.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close SQLite connection: %w", err)
	}

	if p.config.GetEnableLogging() {
		logger.Info("SQLite connection closed: name=%s", p.config.GetName())
	}

	p.db = nil
	return nil
}

// HealthCheck verifies the SQLite connection is alive
func (p *SQLiteProvider) HealthCheck(ctx context.Context) error {
	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Use a short timeout for health checks
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Execute a simple query to verify the database is accessible
	var result int
	err := p.db.QueryRowContext(healthCtx, "SELECT 1").Scan(&result)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if result != 1 {
		return fmt.Errorf("health check returned unexpected result: %d", result)
	}

	return nil
}

// GetNative returns the native *sql.DB connection
func (p *SQLiteProvider) GetNative() (*sql.DB, error) {
	if p.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}
	return p.db, nil
}

// GetMongo returns an error for SQLite (not a MongoDB connection)
func (p *SQLiteProvider) GetMongo() (*mongo.Client, error) {
	return nil, ErrNotMongoDB
}

// Stats returns connection pool statistics
func (p *SQLiteProvider) Stats() *ConnectionStats {
	if p.db == nil {
		return &ConnectionStats{
			Name:      p.config.GetName(),
			Type:      "sqlite",
			Connected: false,
		}
	}

	stats := p.db.Stats()

	return &ConnectionStats{
		Name:              p.config.GetName(),
		Type:              "sqlite",
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
