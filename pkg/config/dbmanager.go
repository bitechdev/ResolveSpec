package config

import (
	"fmt"
	"time"
)

// DBManagerConfig contains configuration for the database connection manager
type DBManagerConfig struct {
	// DefaultConnection is the name of the default connection to use
	DefaultConnection string `mapstructure:"default_connection"`

	// Connections is a map of connection name to connection configuration
	Connections map[string]DBConnectionConfig `mapstructure:"connections"`

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

// DBConnectionConfig defines configuration for a single database connection
type DBConnectionConfig struct {
	// Name is the unique name of this connection
	Name string `mapstructure:"name"`

	// Type is the database type (postgres, sqlite, mssql, mongodb)
	Type string `mapstructure:"type"`

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

// ToManagerConfig converts config.DBManagerConfig to dbmanager.ManagerConfig
// This is used to avoid circular dependencies
func (c *DBManagerConfig) ToManagerConfig() interface{} {
	// This will be implemented in the dbmanager package
	// to convert from config types to dbmanager types
	return c
}

// Validate validates the DBManager configuration
func (c *DBManagerConfig) Validate() error {
	if len(c.Connections) == 0 {
		return fmt.Errorf("at least one connection must be configured")
	}

	if c.DefaultConnection != "" {
		if _, ok := c.Connections[c.DefaultConnection]; !ok {
			return fmt.Errorf("default connection '%s' not found in connections", c.DefaultConnection)
		}
	}

	return nil
}
