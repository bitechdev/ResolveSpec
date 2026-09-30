package dbmanager

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
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

	// Health checks. A zero HealthCheckInterval selects the default (15s); a
	// negative value disables the background health checker.
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`

	// Deprecated: ignored. The manager never closes a pool to recover from an
	// error because database/sql already replaces broken connections; closing
	// it would invalidate every handle handed out. Use Connection.Reconnect for
	// an explicit, handle-preserving refresh.
	EnableAutoReconnect bool `mapstructure:"enable_auto_reconnect"`
}

// DefaultApplicationName is used when a connection does not set ApplicationName.
const DefaultApplicationName = "ResolveSpec"

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

	// ApplicationName identifies this client to the server (postgres
	// application_name, mssql app name, mongodb appName)
	ApplicationName string `mapstructure:"application_name"`

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

	// Retry policy for the initial connect (inherited from the manager config)
	RetryAttempts int           `mapstructure:"retry_attempts"`
	RetryDelay    time.Duration `mapstructure:"retry_delay"`
	RetryMaxDelay time.Duration `mapstructure:"retry_max_delay"`

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

	if cc.ApplicationName == "" {
		cc.ApplicationName = DefaultApplicationName
	}

	// Default timeouts
	if cc.ConnectTimeout == 0 {
		cc.ConnectTimeout = 10 * time.Second
	}
	if cc.QueryTimeout == 0 {
		cc.QueryTimeout = 2 * time.Minute // Default to 2 minutes
	}

	if global != nil {
		if cc.RetryAttempts == 0 {
			cc.RetryAttempts = global.RetryAttempts
		}
		if cc.RetryDelay == 0 {
			cc.RetryDelay = global.RetryDelay
		}
		if cc.RetryMaxDelay == 0 {
			cc.RetryMaxDelay = global.RetryMaxDelay
		}
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

// buildPostgresDSN builds a postgres:// URL so credentials and other values are
// escaped rather than spliced into a key=value string. statement_timeout is
// applied by the provider as a runtime parameter.
func (cc *ConnectionConfig) buildPostgresDSN() string {
	q := url.Values{}
	if cc.SSLMode != "" {
		q.Set("sslmode", cc.SSLMode)
	} else {
		// prefer: use TLS when the server offers it, without failing on
		// servers that do not.
		q.Set("sslmode", "prefer")
	}
	if cc.Schema != "" {
		q.Set("search_path", cc.Schema)
	}
	if cc.ApplicationName != "" {
		q.Set("application_name", cc.ApplicationName)
	}

	u := url.URL{
		Scheme:   "postgres",
		Host:     hostPort(cc.Host, cc.Port),
		Path:     "/" + cc.Database,
		RawQuery: q.Encode(),
	}
	if cc.User != "" || cc.Password != "" {
		u.User = url.UserPassword(cc.User, cc.Password)
	}
	return u.String()
}

func hostPort(host string, port int) string {
	if port == 0 {
		return host
	}
	// JoinHostPort brackets IPv6 literals.
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// buildSQLiteDSN puts per-connection settings in the DSN as _pragma parameters
// so every pooled connection gets them, not just the one that ran an Exec.
func (cc *ConnectionConfig) buildSQLiteDSN() string {
	filepath := cc.FilePath
	if filepath == "" {
		filepath = ":memory:"
	}

	var pragmas []string
	if cc.QueryTimeout > 0 {
		pragmas = append(pragmas, fmt.Sprintf("busy_timeout(%d)", cc.QueryTimeout.Milliseconds()))
	}
	if filepath != ":memory:" {
		pragmas = append(pragmas, "journal_mode(WAL)")
	}
	if len(pragmas) == 0 {
		return filepath
	}

	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	sep := "?"
	if strings.Contains(filepath, "?") {
		sep = "&"
	}
	return filepath + sep + q.Encode()
}

func (cc *ConnectionConfig) buildMSSQLDSN() string {
	// Format: sqlserver://username:password@host:port?database=dbname
	q := url.Values{}
	q.Set("database", cc.Database)
	if cc.Schema != "" {
		q.Set("schema", cc.Schema)
	}
	if cc.ApplicationName != "" {
		q.Set("app name", cc.ApplicationName)
	}
	if cc.ConnectTimeout > 0 {
		sec := strconv.Itoa(int(cc.ConnectTimeout.Seconds()))
		q.Set("connection timeout", sec)
		q.Set("dial timeout", sec)
	}
	if cc.QueryTimeout > 0 {
		q.Set("read timeout", strconv.Itoa(int(cc.QueryTimeout.Seconds())))
	}

	u := url.URL{
		Scheme:   "sqlserver",
		Host:     hostPort(cc.Host, cc.Port),
		RawQuery: q.Encode(),
	}
	if cc.User != "" || cc.Password != "" {
		u.User = url.UserPassword(cc.User, cc.Password)
	}
	return u.String()
}

func (cc *ConnectionConfig) buildMongoDSN() string {
	// Format: mongodb://username:password@host:port/database?authSource=admin
	q := url.Values{}
	if cc.AuthSource != "" {
		q.Set("authSource", cc.AuthSource)
	}
	if cc.ReplicaSet != "" {
		q.Set("replicaSet", cc.ReplicaSet)
	}
	if cc.ReadPreference != "" {
		q.Set("readPreference", cc.ReadPreference)
	}
	if cc.ApplicationName != "" {
		q.Set("appName", cc.ApplicationName)
	}

	u := url.URL{
		Scheme:   "mongodb",
		Host:     hostPort(cc.Host, cc.Port),
		Path:     "/" + cc.Database,
		RawQuery: q.Encode(),
	}
	if cc.User != "" && cc.Password != "" {
		u.User = url.UserPassword(cc.User, cc.Password)
	}
	return u.String()
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
			ApplicationName: connCfg.ApplicationName,
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
func (cc *ConnectionConfig) GetApplicationName() string         { return cc.ApplicationName }
func (cc *ConnectionConfig) GetRetryAttempts() int              { return cc.RetryAttempts }
func (cc *ConnectionConfig) GetRetryDelay() time.Duration       { return cc.RetryDelay }
func (cc *ConnectionConfig) GetRetryMaxDelay() time.Duration    { return cc.RetryMaxDelay }
