package providers

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"go.mongodb.org/mongo-driver/mongo"
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

// Close closes the underlying database connection
func (p *ExistingDBProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.db == nil {
		return nil
	}

	return p.db.Close()
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
		stats.InUse = dbStats.InUse
		stats.Idle = dbStats.Idle
		stats.WaitCount = dbStats.WaitCount
		stats.WaitDuration = dbStats.WaitDuration
		stats.MaxIdleClosed = dbStats.MaxIdleClosed
		stats.MaxLifetimeClosed = dbStats.MaxLifetimeClosed
	}

	return stats
}
