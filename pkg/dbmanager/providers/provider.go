package providers

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// Common errors
var (
	// ErrNotSQLDatabase is returned when attempting SQL operations on a non-SQL database
	ErrNotSQLDatabase = errors.New("not a SQL database")

	// ErrNotMongoDB is returned when attempting MongoDB operations on a non-MongoDB connection
	ErrNotMongoDB = errors.New("not a MongoDB connection")
)

// ConnectionStats contains statistics about a database connection
type ConnectionStats struct {
	Name              string
	Type              string // Database type as string to avoid circular dependency
	Connected         bool
	LastHealthCheck   time.Time
	HealthCheckStatus string

	// SQL connection pool stats
	OpenConnections   int
	InUse             int
	Idle              int
	WaitCount         int64
	WaitDuration      time.Duration
	MaxIdleClosed     int64
	MaxLifetimeClosed int64
}

// ConnectionConfig is a minimal interface for configuration
// The actual implementation is in dbmanager package
type ConnectionConfig interface {
	BuildDSN() (string, error)
	GetName() string
	GetType() string
	GetHost() string
	GetPort() int
	GetUser() string
	GetPassword() string
	GetDatabase() string
	GetFilePath() string
	GetConnectTimeout() time.Duration
	GetQueryTimeout() time.Duration
	GetEnableLogging() bool
	GetEnableMetrics() bool
	GetMaxOpenConns() *int
	GetMaxIdleConns() *int
	GetConnMaxLifetime() *time.Duration
	GetConnMaxIdleTime() *time.Duration
	GetReadPreference() string
}

// Provider creates and manages the underlying database connection
type Provider interface {
	// Connect establishes the database connection
	Connect(ctx context.Context, cfg ConnectionConfig) error

	// Close closes the connection
	Close() error

	// HealthCheck verifies the connection is alive
	HealthCheck(ctx context.Context) error

	// GetNative returns the native *sql.DB (SQL databases only)
	// Returns an error for non-SQL databases
	GetNative() (*sql.DB, error)

	// GetMongo returns the MongoDB client (MongoDB only)
	// Returns an error for non-MongoDB databases
	GetMongo() (*mongo.Client, error)

	// Stats returns connection statistics
	Stats() *ConnectionStats
}
