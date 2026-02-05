package dbmanager

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
	"go.mongodb.org/mongo-driver/mongo"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
)

// Connection represents a single named database connection
type Connection interface {
	// Metadata
	Name() string
	Type() DatabaseType

	// ORM Access (SQL databases only)
	Bun() (*bun.DB, error)
	GORM() (*gorm.DB, error)
	Native() (*sql.DB, error)

	// Common Database interface (for SQL databases)
	Database() (common.Database, error)

	// MongoDB Access (MongoDB only)
	MongoDB() (*mongo.Client, error)

	// Lifecycle
	Connect(ctx context.Context) error
	Close() error
	HealthCheck(ctx context.Context) error
	Reconnect(ctx context.Context) error

	// Stats
	Stats() *ConnectionStats
}

// ConnectionStats contains statistics about a database connection
type ConnectionStats struct {
	Name              string
	Type              DatabaseType
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

// sqlConnection implements Connection for SQL databases (PostgreSQL, SQLite, MSSQL)
type sqlConnection struct {
	name     string
	dbType   DatabaseType
	config   ConnectionConfig
	provider Provider

	// Lazy-initialized ORM instances (all wrap the same sql.DB)
	nativeDB *sql.DB
	bunDB    *bun.DB
	gormDB   *gorm.DB

	// Adapters for common.Database interface
	bunAdapter    *database.BunAdapter
	gormAdapter   *database.GormAdapter
	nativeAdapter common.Database

	// State
	connected bool
	mu        sync.RWMutex

	// Health check
	lastHealthCheck   time.Time
	healthCheckStatus string
}

// newSQLConnection creates a new SQL connection
func newSQLConnection(name string, dbType DatabaseType, config ConnectionConfig, provider Provider) *sqlConnection {
	return &sqlConnection{
		name:     name,
		dbType:   dbType,
		config:   config,
		provider: provider,
	}
}

// Name returns the connection name
func (c *sqlConnection) Name() string {
	return c.name
}

// Type returns the database type
func (c *sqlConnection) Type() DatabaseType {
	return c.dbType
}

// Connect establishes the database connection
func (c *sqlConnection) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return ErrAlreadyConnected
	}

	if err := c.provider.Connect(ctx, &c.config); err != nil {
		return NewConnectionError(c.name, "connect", err)
	}

	c.connected = true
	return nil
}

// Close closes the database connection and all ORM instances
func (c *sqlConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	// Close Bun if initialized
	if c.bunDB != nil {
		if err := c.bunDB.Close(); err != nil {
			return NewConnectionError(c.name, "close bun", err)
		}
	}

	// GORM doesn't have a separate close - it uses the underlying sql.DB

	// Close the provider (which closes the underlying sql.DB)
	if err := c.provider.Close(); err != nil {
		return NewConnectionError(c.name, "close", err)
	}

	c.connected = false
	c.nativeDB = nil
	c.bunDB = nil
	c.gormDB = nil
	c.bunAdapter = nil
	c.gormAdapter = nil
	c.nativeAdapter = nil

	return nil
}

// HealthCheck verifies the connection is alive
func (c *sqlConnection) HealthCheck(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("connection is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastHealthCheck = time.Now()

	if !c.connected {
		c.healthCheckStatus = "disconnected"
		return ErrConnectionClosed
	}

	if err := c.provider.HealthCheck(ctx); err != nil {
		c.healthCheckStatus = "unhealthy: " + err.Error()
		return NewConnectionError(c.name, "health check", err)
	}

	c.healthCheckStatus = "healthy"
	return nil
}

// Reconnect closes and re-establishes the connection
func (c *sqlConnection) Reconnect(ctx context.Context) error {
	if err := c.Close(); err != nil {
		return err
	}
	return c.Connect(ctx)
}

// Native returns the native *sql.DB connection
func (c *sqlConnection) Native() (*sql.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.nativeDB != nil {
		defer c.mu.RUnlock()
		return c.nativeDB, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.nativeDB != nil {
		return c.nativeDB, nil
	}

	if !c.connected {
		return nil, ErrConnectionClosed
	}

	// Get native connection from provider
	db, err := c.provider.GetNative()
	if err != nil {
		return nil, NewConnectionError(c.name, "get native", err)
	}

	c.nativeDB = db
	return c.nativeDB, nil
}

// Bun returns a Bun ORM instance wrapping the native connection
func (c *sqlConnection) Bun() (*bun.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.bunDB != nil {
		defer c.mu.RUnlock()
		return c.bunDB, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.bunDB != nil {
		return c.bunDB, nil
	}

	// Get native connection first
	native, err := c.provider.GetNative()
	if err != nil {
		return nil, NewConnectionError(c.name, "get bun", err)
	}

	// Create Bun DB wrapping the same sql.DB
	dialect := c.getBunDialect()
	c.bunDB = bun.NewDB(native, dialect)

	return c.bunDB, nil
}

// GORM returns a GORM instance wrapping the native connection
func (c *sqlConnection) GORM() (*gorm.DB, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.gormDB != nil {
		defer c.mu.RUnlock()
		return c.gormDB, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.gormDB != nil {
		return c.gormDB, nil
	}

	// Get native connection first
	native, err := c.provider.GetNative()
	if err != nil {
		return nil, NewConnectionError(c.name, "get gorm", err)
	}

	// Create GORM DB wrapping the same sql.DB
	dialector := c.getGORMDialector(native)
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, NewConnectionError(c.name, "initialize gorm", err)
	}

	c.gormDB = db
	return c.gormDB, nil
}

// Database returns the common.Database interface using the configured default ORM
func (c *sqlConnection) Database() (common.Database, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	defaultORM := c.config.DefaultORM
	c.mu.RUnlock()

	switch ORMType(defaultORM) {
	case ORMTypeBun:
		return c.getBunAdapter()
	case ORMTypeGORM:
		return c.getGORMAdapter()
	case ORMTypeNative:
		return c.getNativeAdapter()
	default:
		// Default to Bun
		return c.getBunAdapter()
	}
}

// MongoDB returns an error for SQL connections
func (c *sqlConnection) MongoDB() (*mongo.Client, error) {
	return nil, ErrNotMongoDB
}

// Stats returns connection statistics
func (c *sqlConnection) Stats() *ConnectionStats {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &ConnectionStats{
		Name:              c.name,
		Type:              c.dbType,
		Connected:         c.connected,
		LastHealthCheck:   c.lastHealthCheck,
		HealthCheckStatus: c.healthCheckStatus,
	}

	// Get SQL stats if connected
	if c.connected && c.provider != nil {
		if providerStats := c.provider.Stats(); providerStats != nil {
			stats.OpenConnections = providerStats.OpenConnections
			stats.InUse = providerStats.InUse
			stats.Idle = providerStats.Idle
			stats.WaitCount = providerStats.WaitCount
			stats.WaitDuration = providerStats.WaitDuration
			stats.MaxIdleClosed = providerStats.MaxIdleClosed
			stats.MaxLifetimeClosed = providerStats.MaxLifetimeClosed
		}
	}

	return stats
}

// getBunAdapter returns or creates the Bun adapter
func (c *sqlConnection) getBunAdapter() (common.Database, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.bunAdapter != nil {
		defer c.mu.RUnlock()
		return c.bunAdapter, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.bunAdapter != nil {
		return c.bunAdapter, nil
	}

	// Double-check bunDB exists (while already holding write lock)
	if c.bunDB == nil {
		// Get native connection first
		native, err := c.provider.GetNative()
		if err != nil {
			return nil, NewConnectionError(c.name, "get bun", err)
		}

		// Create Bun DB wrapping the same sql.DB
		dialect := c.getBunDialect()
		c.bunDB = bun.NewDB(native, dialect)
	}

	c.bunAdapter = database.NewBunAdapter(c.bunDB)
	return c.bunAdapter, nil
}

// getGORMAdapter returns or creates the GORM adapter
func (c *sqlConnection) getGORMAdapter() (common.Database, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.gormAdapter != nil {
		defer c.mu.RUnlock()
		return c.gormAdapter, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.gormAdapter != nil {
		return c.gormAdapter, nil
	}

	// Double-check gormDB exists (while already holding write lock)
	if c.gormDB == nil {
		// Get native connection first
		native, err := c.provider.GetNative()
		if err != nil {
			return nil, NewConnectionError(c.name, "get gorm", err)
		}

		// Create GORM DB wrapping the same sql.DB
		dialector := c.getGORMDialector(native)
		db, err := gorm.Open(dialector, &gorm.Config{})
		if err != nil {
			return nil, NewConnectionError(c.name, "initialize gorm", err)
		}

		c.gormDB = db
	}

	c.gormAdapter = database.NewGormAdapter(c.gormDB)
	return c.gormAdapter, nil
}

// getNativeAdapter returns or creates the native adapter
func (c *sqlConnection) getNativeAdapter() (common.Database, error) {
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}
	c.mu.RLock()
	if c.nativeAdapter != nil {
		defer c.mu.RUnlock()
		return c.nativeAdapter, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.nativeAdapter != nil {
		return c.nativeAdapter, nil
	}

	// Double-check nativeDB exists (while already holding write lock)
	if c.nativeDB == nil {
		if !c.connected {
			return nil, ErrConnectionClosed
		}

		// Get native connection from provider
		db, err := c.provider.GetNative()
		if err != nil {
			return nil, NewConnectionError(c.name, "get native", err)
		}

		c.nativeDB = db
	}

	// Create a native adapter based on database type
	switch c.dbType {
	case DatabaseTypePostgreSQL:
		c.nativeAdapter = database.NewPgSQLAdapter(c.nativeDB, string(c.dbType))
	case DatabaseTypeSQLite:
		c.nativeAdapter = database.NewPgSQLAdapter(c.nativeDB, string(c.dbType))
	case DatabaseTypeMSSQL:
		c.nativeAdapter = database.NewPgSQLAdapter(c.nativeDB, string(c.dbType))
	default:
		return nil, ErrUnsupportedDatabase
	}

	return c.nativeAdapter, nil
}

// getBunDialect returns the appropriate Bun dialect for the database type
func (c *sqlConnection) getBunDialect() schema.Dialect {

	switch c.dbType {
	case DatabaseTypePostgreSQL:
		return database.GetPostgresDialect()
	case DatabaseTypeSQLite:
		return database.GetSQLiteDialect()
	case DatabaseTypeMSSQL:
		return database.GetMSSQLDialect()
	default:
		// Default to PostgreSQL
		return database.GetPostgresDialect()
	}
}

// getGORMDialector returns the appropriate GORM dialector for the database type
func (c *sqlConnection) getGORMDialector(db *sql.DB) gorm.Dialector {
	switch c.dbType {
	case DatabaseTypePostgreSQL:
		return database.GetPostgresDialector(db)
	case DatabaseTypeSQLite:
		return database.GetSQLiteDialector(db)
	case DatabaseTypeMSSQL:
		return database.GetMSSQLDialector(db)
	default:
		// Default to PostgreSQL
		return database.GetPostgresDialector(db)
	}
}

// mongoConnection implements Connection for MongoDB
type mongoConnection struct {
	name     string
	config   ConnectionConfig
	provider Provider

	// MongoDB client
	client *mongo.Client

	// State
	connected bool
	mu        sync.RWMutex

	// Health check
	lastHealthCheck   time.Time
	healthCheckStatus string
}

// newMongoConnection creates a new MongoDB connection
func newMongoConnection(name string, config ConnectionConfig, provider Provider) *mongoConnection {
	return &mongoConnection{
		name:     name,
		config:   config,
		provider: provider,
	}
}

// Name returns the connection name
func (c *mongoConnection) Name() string {
	return c.name
}

// Type returns the database type (MongoDB)
func (c *mongoConnection) Type() DatabaseType {
	return DatabaseTypeMongoDB
}

// Connect establishes the MongoDB connection
func (c *mongoConnection) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return ErrAlreadyConnected
	}

	if err := c.provider.Connect(ctx, &c.config); err != nil {
		return NewConnectionError(c.name, "connect", err)
	}

	// Get the mongo client
	client, err := c.provider.GetMongo()
	if err != nil {
		return NewConnectionError(c.name, "get mongo client", err)
	}

	c.client = client
	c.connected = true
	return nil
}

// Close closes the MongoDB connection
func (c *mongoConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	if err := c.provider.Close(); err != nil {
		return NewConnectionError(c.name, "close", err)
	}

	c.connected = false
	c.client = nil
	return nil
}

// HealthCheck verifies the MongoDB connection is alive
func (c *mongoConnection) HealthCheck(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastHealthCheck = time.Now()

	if !c.connected {
		c.healthCheckStatus = "disconnected"
		return ErrConnectionClosed
	}

	if err := c.provider.HealthCheck(ctx); err != nil {
		c.healthCheckStatus = "unhealthy: " + err.Error()
		return NewConnectionError(c.name, "health check", err)
	}

	c.healthCheckStatus = "healthy"
	return nil
}

// Reconnect closes and re-establishes the MongoDB connection
func (c *mongoConnection) Reconnect(ctx context.Context) error {
	if err := c.Close(); err != nil {
		return err
	}
	return c.Connect(ctx)
}

// MongoDB returns the MongoDB client
func (c *mongoConnection) MongoDB() (*mongo.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.client == nil {
		return nil, ErrConnectionClosed
	}

	return c.client, nil
}

// Bun returns an error for MongoDB connections
func (c *mongoConnection) Bun() (*bun.DB, error) {
	return nil, ErrNotSQLDatabase
}

// GORM returns an error for MongoDB connections
func (c *mongoConnection) GORM() (*gorm.DB, error) {
	return nil, ErrNotSQLDatabase
}

// Native returns an error for MongoDB connections
func (c *mongoConnection) Native() (*sql.DB, error) {
	return nil, ErrNotSQLDatabase
}

// Database returns an error for MongoDB connections
func (c *mongoConnection) Database() (common.Database, error) {
	return nil, ErrNotSQLDatabase
}

// Stats returns connection statistics for MongoDB
func (c *mongoConnection) Stats() *ConnectionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &ConnectionStats{
		Name:              c.name,
		Type:              DatabaseTypeMongoDB,
		Connected:         c.connected,
		LastHealthCheck:   c.lastHealthCheck,
		HealthCheckStatus: c.healthCheckStatus,
	}
}
