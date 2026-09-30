package providers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/glebarez/sqlite" // Pure Go SQLite driver
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// SQLiteProvider implements Provider for SQLite databases
type SQLiteProvider struct {
	db     *sql.DB
	dbMu   sync.RWMutex
	config ConnectionConfig
}

// NewSQLiteProvider creates a new SQLite provider
func NewSQLiteProvider() *SQLiteProvider {
	return &SQLiteProvider{}
}

// isMemoryDSN reports whether the SQLite DSN refers to a private in-memory
// database (each pooled connection would get its own empty database).
func isMemoryDSN(dsn string) bool {
	path := dsn
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == ":memory:" || path == "" {
		return true
	}
	if strings.Contains(dsn, "mode=memory") && !strings.Contains(dsn, "cache=shared") {
		return true
	}
	return path == "file::memory:" && !strings.Contains(dsn, "cache=shared")
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
		db.Close() //nolint:gosec // G104: best-effort call, error intentionally ignored
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	if isMemoryDSN(dsn) {
		// A private in-memory database exists per connection and disappears when
		// that connection closes, so pin the pool to one connection that is
		// never recycled.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)
	} else {
		// SQLite works best with few writers; default to 1 unless configured.
		if cfg.GetMaxOpenConns() != nil {
			db.SetMaxOpenConns(*cfg.GetMaxOpenConns())
		} else {
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
	}

	p.dbMu.Lock()
	p.db = db
	p.dbMu.Unlock()
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
	if err := p.getDB().QueryRowContext(healthCtx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if result != 1 {
		return fmt.Errorf("health check returned unexpected result: %d", result)
	}

	return nil
}

func (p *SQLiteProvider) getDB() *sql.DB {
	p.dbMu.RLock()
	defer p.dbMu.RUnlock()
	return p.db
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
