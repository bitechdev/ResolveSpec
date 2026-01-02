package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
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

// PopulateFromDSN parses a DSN and populates the connection fields
func (cc *DBConnectionConfig) PopulateFromDSN() error {
	if cc.DSN == "" {
		return nil // Nothing to populate
	}

	switch cc.Type {
	case "postgres":
		return cc.populatePostgresDSN()
	case "mongodb":
		return cc.populateMongoDSN()
	case "mssql":
		return cc.populateMSSQLDSN()
	case "sqlite":
		return cc.populateSQLiteDSN()
	default:
		return fmt.Errorf("cannot parse DSN for unsupported database type: %s", cc.Type)
	}
}

// populatePostgresDSN parses PostgreSQL DSN format
// Example: host=localhost port=5432 user=postgres password=secret dbname=mydb sslmode=disable
func (cc *DBConnectionConfig) populatePostgresDSN() error {
	parts := strings.Fields(cc.DSN)
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, value := kv[0], kv[1]

		switch key {
		case "host":
			cc.Host = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid port in DSN: %w", err)
			}
			cc.Port = port
		case "user":
			cc.User = value
		case "password":
			cc.Password = value
		case "dbname":
			cc.Database = value
		case "sslmode":
			cc.SSLMode = value
		case "search_path":
			cc.Schema = value
		}
	}
	return nil
}

// populateMongoDSN parses MongoDB DSN format
// Example: mongodb://user:password@host:port/database?authSource=admin&replicaSet=rs0
func (cc *DBConnectionConfig) populateMongoDSN() error {
	u, err := url.Parse(cc.DSN)
	if err != nil {
		return fmt.Errorf("invalid MongoDB DSN: %w", err)
	}

	// Extract user and password
	if u.User != nil {
		cc.User = u.User.Username()
		if password, ok := u.User.Password(); ok {
			cc.Password = password
		}
	}

	// Extract host and port
	if u.Host != "" {
		host := u.Host
		if strings.Contains(host, ":") {
			hostPort := strings.SplitN(host, ":", 2)
			cc.Host = hostPort[0]
			if port, err := strconv.Atoi(hostPort[1]); err == nil {
				cc.Port = port
			}
		} else {
			cc.Host = host
		}
	}

	// Extract database
	if u.Path != "" {
		cc.Database = strings.TrimPrefix(u.Path, "/")
	}

	// Extract query parameters
	params := u.Query()
	if authSource := params.Get("authSource"); authSource != "" {
		cc.AuthSource = authSource
	}
	if replicaSet := params.Get("replicaSet"); replicaSet != "" {
		cc.ReplicaSet = replicaSet
	}
	if readPref := params.Get("readPreference"); readPref != "" {
		cc.ReadPreference = readPref
	}

	return nil
}

// populateMSSQLDSN parses MSSQL DSN format
// Example: sqlserver://username:password@host:port?database=dbname&schema=dbo
func (cc *DBConnectionConfig) populateMSSQLDSN() error {
	u, err := url.Parse(cc.DSN)
	if err != nil {
		return fmt.Errorf("invalid MSSQL DSN: %w", err)
	}

	// Extract user and password
	if u.User != nil {
		cc.User = u.User.Username()
		if password, ok := u.User.Password(); ok {
			cc.Password = password
		}
	}

	// Extract host and port
	if u.Host != "" {
		host := u.Host
		if strings.Contains(host, ":") {
			hostPort := strings.SplitN(host, ":", 2)
			cc.Host = hostPort[0]
			if port, err := strconv.Atoi(hostPort[1]); err == nil {
				cc.Port = port
			}
		} else {
			cc.Host = host
		}
	}

	// Extract query parameters
	params := u.Query()
	if database := params.Get("database"); database != "" {
		cc.Database = database
	}
	if schema := params.Get("schema"); schema != "" {
		cc.Schema = schema
	}

	return nil
}

// populateSQLiteDSN parses SQLite DSN format
// Example: /path/to/database.db or :memory:
func (cc *DBConnectionConfig) populateSQLiteDSN() error {
	cc.FilePath = cc.DSN
	return nil
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
