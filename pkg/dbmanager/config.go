package dbmanager

import (
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// DatabaseType represents the type of database
type DatabaseType string

const (
	// DatabaseTypePostgreSQL represents PostgreSQL database
	DatabaseTypePostgreSQL DatabaseType = "postgres"

	// DatabaseTypeSQLite represents SQLite database
	DatabaseTypeSQLite DatabaseType = "sqlite"

	// DatabaseTypeMSSQL represents Microsoft SQL Server database
	DatabaseTypeMSSQL DatabaseType = "mssql"

	// DatabaseTypeMongoDB represents MongoDB database
	DatabaseTypeMongoDB DatabaseType = "mongodb"
)

// ORMType represents the ORM to use for database operations
type ORMType string

const (
	// ORMTypeBun represents Bun ORM
	ORMTypeBun ORMType = "bun"

	// ORMTypeGORM represents GORM
	ORMTypeGORM ORMType = "gorm"

	// ORMTypeNative represents native database/sql
	ORMTypeNative ORMType = "native"
)

// ManagerConfig contains configuration for the database connection manager
type ManagerConfig struct {
	// DefaultConnection is the name of the default connection to use
	DefaultConnection string `mapstructure:"default_connection"`

	// Connections is a map of connection name to connection configuration
	Connections map[string]ConnectionConfig `mapstructure:"connections"`

	// Global connection pool defaults
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`

	// Retry policy
	RetryAttempts int           `mapstructure:"retry_attempts"`
	RetryDelay    time.Duration `mapstructure:"retry_delay"`
	RetryMaxDelay time.Duration `mapstructure:"retry_max_delay"`

	// Health checks
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`
	EnableAutoReconnect bool          `mapstructure:"enable_auto_reconnect"`
}

// ConnectionConfig defines configuration for a single database connection
type ConnectionConfig struct {
	// Name is the unique name of this connection
	Name string `mapstructure:"name"`

	// Type is the database type (postgres, sqlite, mssql, mongodb)
	Type DatabaseType `mapstructure:"type"`

	// DSN is the complete Data Source Name / connection string
	// If provided, this takes precedence over individual connection parameters
	DSN string `mapstructure:"dsn"`

	// Connection parameters (used if DSN is not provided)
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`

	// PostgreSQL/MSSQL specific
	SSLMode string `mapstructure:"sslmode"` // disable, require, verify-ca, verify-full
	Schema  string `mapstructure:"schema"`  // Default schema

	// SQLite specific
	FilePath string `mapstructure:"filepath"`

	// MongoDB specific
	AuthSource     string `mapstructure:"auth_source"`
	ReplicaSet     string `mapstructure:"replica_set"`
	ReadPreference string `mapstructure:"read_preference"` // primary, secondary, etc.

	// Connection pool settings (overrides global defaults)
	MaxOpenConns    *int           `mapstructure:"max_open_conns"`
	MaxIdleConns    *int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime *time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime *time.Duration `mapstructure:"conn_max_idle_time"`

	// Timeouts
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"`
	QueryTimeout   time.Duration `mapstructure:"query_timeout"`

	// Features
	EnableTracing bool `mapstructure:"enable_tracing"`
	EnableMetrics bool `mapstructure:"enable_metrics"`
	EnableLogging bool `mapstructure:"enable_logging"`

	// DefaultORM specifies which ORM to use for the Database() method
	// Options: "bun", "gorm", "native"
	DefaultORM string `mapstructure:"default_orm"`

	// Tags for organization and filtering
	Tags map[string]string `mapstructure:"tags"`
}

// DefaultManagerConfig returns a ManagerConfig with sensible defaults
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		DefaultConnection:   "",
		Connections:         make(map[string]ConnectionConfig),
		MaxOpenConns:        25,
		MaxIdleConns:        5,
		ConnMaxLifetime:     30 * time.Minute,
		ConnMaxIdleTime:     5 * time.Minute,
		RetryAttempts:       3,
		RetryDelay:          1 * time.Second,
		RetryMaxDelay:       10 * time.Second,
		HealthCheckInterval: 15 * time.Second,
		EnableAutoReconnect: true,
	}
}

// ApplyDefaults applies default values to the manager configuration
func (c *ManagerConfig) ApplyDefaults() {
	defaults := DefaultManagerConfig()

	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = defaults.MaxOpenConns
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = defaults.MaxIdleConns
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = defaults.ConnMaxLifetime
	}
	if c.ConnMaxIdleTime == 0 {
		c.ConnMaxIdleTime = defaults.ConnMaxIdleTime
	}
	if c.RetryAttempts == 0 {
		c.RetryAttempts = defaults.RetryAttempts
	}
	if c.RetryDelay == 0 {
		c.RetryDelay = defaults.RetryDelay
	}
	if c.RetryMaxDelay == 0 {
		c.RetryMaxDelay = defaults.RetryMaxDelay
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = defaults.HealthCheckInterval
	}
	// EnableAutoReconnect defaults to true - apply if not explicitly set
	// Since this is a boolean, we apply the default unconditionally when it's false
	if !c.EnableAutoReconnect {
		c.EnableAutoReconnect = defaults.EnableAutoReconnect
	}
}

// Validate validates the manager configuration
func (c *ManagerConfig) Validate() error {
	if len(c.Connections) == 0 {
		return NewConfigurationError("connections", fmt.Errorf("at least one connection must be configured"))
	}

	if c.DefaultConnection != "" {
		if _, ok := c.Connections[c.DefaultConnection]; !ok {
			return NewConfigurationError("default_connection", fmt.Errorf("default connection '%s' not found in connections", c.DefaultConnection))
		}
	}

	// Validate each connection
	for name := range c.Connections {
		conn := c.Connections[name]
		if err := conn.Validate(); err != nil {
			return fmt.Errorf("connection '%s': %w", name, err)
		}
	}

	return nil
}

// ApplyDefaults applies default values and global settings to the connection configuration
func (cc *ConnectionConfig) ApplyDefaults(global *ManagerConfig) {
	// Set name if not already set
	if cc.Name == "" {
		cc.Name = "unnamed"
	}

	// Apply global pool settings if not overridden
	if cc.MaxOpenConns == nil && global != nil {
		maxOpen := global.MaxOpenConns
		cc.MaxOpenConns = &maxOpen
	}
	if cc.MaxIdleConns == nil && global != nil {
		maxIdle := global.MaxIdleConns
		cc.MaxIdleConns = &maxIdle
	}
	if cc.ConnMaxLifetime == nil && global != nil {
		lifetime := global.ConnMaxLifetime
		cc.ConnMaxLifetime = &lifetime
	}
	if cc.ConnMaxIdleTime == nil && global != nil {
		idleTime := global.ConnMaxIdleTime
		cc.ConnMaxIdleTime = &idleTime
	}

	// Default timeouts
	if cc.ConnectTimeout == 0 {
		cc.ConnectTimeout = 10 * time.Second
	}
	if cc.QueryTimeout == 0 {
		cc.QueryTimeout = 30 * time.Second
	}

	// Default ORM
	if cc.DefaultORM == "" {
		cc.DefaultORM = string(ORMTypeBun)
	}

	// Default PostgreSQL port
	if cc.Type == DatabaseTypePostgreSQL && cc.Port == 0 && cc.DSN == "" {
		cc.Port = 5432
	}

	// Default MSSQL port
	if cc.Type == DatabaseTypeMSSQL && cc.Port == 0 && cc.DSN == "" {
		cc.Port = 1433
	}

	// Default MongoDB port
	if cc.Type == DatabaseTypeMongoDB && cc.Port == 0 && cc.DSN == "" {
		cc.Port = 27017
	}

	// Default MongoDB auth source
	if cc.Type == DatabaseTypeMongoDB && cc.AuthSource == "" {
		cc.AuthSource = "admin"
	}
}

// Validate validates the connection configuration
func (cc *ConnectionConfig) Validate() error {
	// Validate database type
	switch cc.Type {
	case DatabaseTypePostgreSQL, DatabaseTypeSQLite, DatabaseTypeMSSQL, DatabaseTypeMongoDB:
		// Valid types
	default:
		return NewConfigurationError("type", fmt.Errorf("unsupported database type: %s", cc.Type))
	}

	// Validate that either DSN or connection parameters are provided
	if cc.DSN == "" {
		switch cc.Type {
		case DatabaseTypePostgreSQL, DatabaseTypeMSSQL, DatabaseTypeMongoDB:
			if cc.Host == "" {
				return NewConfigurationError("host", fmt.Errorf("host is required when DSN is not provided"))
			}
			if cc.Database == "" {
				return NewConfigurationError("database", fmt.Errorf("database is required when DSN is not provided"))
			}
		case DatabaseTypeSQLite:
			if cc.FilePath == "" {
				return NewConfigurationError("filepath", fmt.Errorf("filepath is required for SQLite when DSN is not provided"))
			}
		}
	}

	// Validate ORM type
	if cc.DefaultORM != "" {
		switch ORMType(cc.DefaultORM) {
		case ORMTypeBun, ORMTypeGORM, ORMTypeNative:
			// Valid ORM types
		default:
			return NewConfigurationError("default_orm", fmt.Errorf("unsupported ORM type: %s", cc.DefaultORM))
		}
	}

	return nil
}

// BuildDSN builds a connection string from individual parameters
func (cc *ConnectionConfig) BuildDSN() (string, error) {
	// If DSN is already provided, use it
	if cc.DSN != "" {
		return cc.DSN, nil
	}

	switch cc.Type {
	case DatabaseTypePostgreSQL:
		return cc.buildPostgresDSN(), nil
	case DatabaseTypeSQLite:
		return cc.buildSQLiteDSN(), nil
	case DatabaseTypeMSSQL:
		return cc.buildMSSQLDSN(), nil
	case DatabaseTypeMongoDB:
		return cc.buildMongoDSN(), nil
	default:
		return "", fmt.Errorf("cannot build DSN for database type: %s", cc.Type)
	}
}

func (cc *ConnectionConfig) buildPostgresDSN() string {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		cc.Host, cc.Port, cc.User, cc.Password, cc.Database)

	if cc.SSLMode != "" {
		dsn += fmt.Sprintf(" sslmode=%s", cc.SSLMode)
	} else {
		dsn += " sslmode=disable"
	}

	if cc.Schema != "" {
		dsn += fmt.Sprintf(" search_path=%s", cc.Schema)
	}

	return dsn
}

func (cc *ConnectionConfig) buildSQLiteDSN() string {
	if cc.FilePath != "" {
		return cc.FilePath
	}
	return ":memory:"
}

func (cc *ConnectionConfig) buildMSSQLDSN() string {
	// Format: sqlserver://username:password@host:port?database=dbname
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
		cc.User, cc.Password, cc.Host, cc.Port, cc.Database)

	if cc.Schema != "" {
		dsn += fmt.Sprintf("&schema=%s", cc.Schema)
	}

	return dsn
}

func (cc *ConnectionConfig) buildMongoDSN() string {
	// Format: mongodb://username:password@host:port/database?authSource=admin
	var dsn string

	if cc.User != "" && cc.Password != "" {
		dsn = fmt.Sprintf("mongodb://%s:%s@%s:%d/%s",
			cc.User, cc.Password, cc.Host, cc.Port, cc.Database)
	} else {
		dsn = fmt.Sprintf("mongodb://%s:%d/%s", cc.Host, cc.Port, cc.Database)
	}

	params := ""
	if cc.AuthSource != "" {
		params += fmt.Sprintf("authSource=%s", cc.AuthSource)
	}
	if cc.ReplicaSet != "" {
		if params != "" {
			params += "&"
		}
		params += fmt.Sprintf("replicaSet=%s", cc.ReplicaSet)
	}
	if cc.ReadPreference != "" {
		if params != "" {
			params += "&"
		}
		params += fmt.Sprintf("readPreference=%s", cc.ReadPreference)
	}

	if params != "" {
		dsn += "?" + params
	}

	return dsn
}

// FromConfig converts config.DBManagerConfig to internal ManagerConfig
func FromConfig(cfg config.DBManagerConfig) ManagerConfig {
	mgr := ManagerConfig{
		DefaultConnection:   cfg.DefaultConnection,
		Connections:         make(map[string]ConnectionConfig),
		MaxOpenConns:        cfg.MaxOpenConns,
		MaxIdleConns:        cfg.MaxIdleConns,
		ConnMaxLifetime:     cfg.ConnMaxLifetime,
		ConnMaxIdleTime:     cfg.ConnMaxIdleTime,
		RetryAttempts:       cfg.RetryAttempts,
		RetryDelay:          cfg.RetryDelay,
		RetryMaxDelay:       cfg.RetryMaxDelay,
		HealthCheckInterval: cfg.HealthCheckInterval,
		EnableAutoReconnect: cfg.EnableAutoReconnect,
	}

	// Convert connections
	for name := range cfg.Connections {
		connCfg := cfg.Connections[name]
		mgr.Connections[name] = ConnectionConfig{
			Name:            connCfg.Name,
			Type:            DatabaseType(connCfg.Type),
			DSN:             connCfg.DSN,
			Host:            connCfg.Host,
			Port:            connCfg.Port,
			User:            connCfg.User,
			Password:        connCfg.Password,
			Database:        connCfg.Database,
			SSLMode:         connCfg.SSLMode,
			Schema:          connCfg.Schema,
			FilePath:        connCfg.FilePath,
			AuthSource:      connCfg.AuthSource,
			ReplicaSet:      connCfg.ReplicaSet,
			ReadPreference:  connCfg.ReadPreference,
			MaxOpenConns:    connCfg.MaxOpenConns,
			MaxIdleConns:    connCfg.MaxIdleConns,
			ConnMaxLifetime: connCfg.ConnMaxLifetime,
			ConnMaxIdleTime: connCfg.ConnMaxIdleTime,
			ConnectTimeout:  connCfg.ConnectTimeout,
			QueryTimeout:    connCfg.QueryTimeout,
			EnableTracing:   connCfg.EnableTracing,
			EnableMetrics:   connCfg.EnableMetrics,
			EnableLogging:   connCfg.EnableLogging,
			DefaultORM:      connCfg.DefaultORM,
			Tags:            connCfg.Tags,
		}
	}

	return mgr
}

// Getter methods to implement providers.ConnectionConfig interface
func (cc *ConnectionConfig) GetName() string                    { return cc.Name }
func (cc *ConnectionConfig) GetType() string                    { return string(cc.Type) }
func (cc *ConnectionConfig) GetHost() string                    { return cc.Host }
func (cc *ConnectionConfig) GetPort() int                       { return cc.Port }
func (cc *ConnectionConfig) GetUser() string                    { return cc.User }
func (cc *ConnectionConfig) GetPassword() string                { return cc.Password }
func (cc *ConnectionConfig) GetDatabase() string                { return cc.Database }
func (cc *ConnectionConfig) GetFilePath() string                { return cc.FilePath }
func (cc *ConnectionConfig) GetConnectTimeout() time.Duration   { return cc.ConnectTimeout }
func (cc *ConnectionConfig) GetEnableLogging() bool             { return cc.EnableLogging }
func (cc *ConnectionConfig) GetMaxOpenConns() *int              { return cc.MaxOpenConns }
func (cc *ConnectionConfig) GetMaxIdleConns() *int              { return cc.MaxIdleConns }
func (cc *ConnectionConfig) GetConnMaxLifetime() *time.Duration { return cc.ConnMaxLifetime }
func (cc *ConnectionConfig) GetConnMaxIdleTime() *time.Duration { return cc.ConnMaxIdleTime }
func (cc *ConnectionConfig) GetQueryTimeout() time.Duration     { return cc.QueryTimeout }
func (cc *ConnectionConfig) GetEnableMetrics() bool             { return cc.EnableMetrics }
func (cc *ConnectionConfig) GetReadPreference() string          { return cc.ReadPreference }
