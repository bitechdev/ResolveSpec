package providers

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// ExistingDBProvider wraps an existing *sql.DB connection
// This allows using dbmanager features with a database connection
// that was opened outside of the dbmanager package
type ExistingDBProvider struct {
	db   *sql.DB
	name string
	mu   sync.RWMutex
}

// NewExistingDBProvider creates a new provider wrapping an existing *sql.DB
func NewExistingDBProvider(db *sql.DB, name string) *ExistingDBProvider {
	return &ExistingDBProvider{
		db:   db,
		name: name,
	}
}

// Connect verifies the existing database connection is valid
// It does NOT create a new connection, but ensures the existing one works
func (p *ExistingDBProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Verify the connection works
	if err := p.db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping existing database: %w", err)
	}

	return nil
}

// Refresh verifies the wrapped database is still reachable. The pool belongs to
// the caller and cannot be re-dialed here, so it is never closed to "reconnect".
func (p *ExistingDBProvider) Refresh(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return p.db.PingContext(ctx)
}

// OwnsDB reports whether Close releases the wrapped database. It never does:
// the *sql.DB was opened by the caller, who is responsible for closing it.
func (p *ExistingDBProvider) OwnsDB() bool { return false }

// Close is a no-op for the wrapped database. The pool belongs to the caller, so
// closing it here would break the caller's other users of it.
func (p *ExistingDBProvider) Close() error {
	logger.Warn("Not closing externally provided database: name=%s; the caller owns this *sql.DB and must close it", p.name)
	return nil
}

// HealthCheck verifies the connection is alive
func (p *ExistingDBProvider) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	return p.db.PingContext(ctx)
}

// GetNative returns the wrapped *sql.DB
func (p *ExistingDBProvider) GetNative() (*sql.DB, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	return p.db, nil
}

// GetMongo returns an error since this is a SQL database
func (p *ExistingDBProvider) GetMongo() (*mongo.Client, error) {
	return nil, ErrNotMongoDB
}

// Stats returns connection statistics
func (p *ExistingDBProvider) Stats() *ConnectionStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := &ConnectionStats{
		Name:      p.name,
		Type:      "sql", // Generic since we don't know the specific type
		Connected: p.db != nil,
	}

	if p.db != nil {
		dbStats := p.db.Stats()
		stats.OpenConnections = dbStats.OpenConnections
		stats.MaxOpenConnections = dbStats.MaxOpenConnections
		// The pool was opened outside dbmanager so dials cannot be counted.
		// Open plus every connection database/sql retired for idle or
		// lifetime limits is a close lower bound (it misses connections
		// dropped as broken).
		stats.TotalOpened = int64(dbStats.OpenConnections) + dbStats.MaxIdleClosed + dbStats.MaxIdleTimeClosed + dbStats.MaxLifetimeClosed
		stats.InUse = dbStats.InUse
		stats.Idle = dbStats.Idle
		stats.WaitCount = dbStats.WaitCount
		stats.WaitDuration = dbStats.WaitDuration
		stats.MaxIdleClosed = dbStats.MaxIdleClosed
		stats.MaxLifetimeClosed = dbStats.MaxLifetimeClosed
	}

	return stats
}
