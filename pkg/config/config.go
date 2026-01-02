package config

import "time"

// Config represents the complete application configuration
type Config struct {
	Servers       ServersConfig          `mapstructure:"servers"`
	Tracing       TracingConfig          `mapstructure:"tracing"`
	Cache         CacheConfig            `mapstructure:"cache"`
	Logger        LoggerConfig           `mapstructure:"logger"`
	ErrorTracking ErrorTrackingConfig    `mapstructure:"error_tracking"`
	Middleware    MiddlewareConfig       `mapstructure:"middleware"`
	CORS          CORSConfig             `mapstructure:"cors"`
	EventBroker   EventBrokerConfig      `mapstructure:"event_broker"`
	DBManager     DBManagerConfig        `mapstructure:"dbmanager"`
	Paths         PathsConfig            `mapstructure:"paths"`
	Extensions    map[string]interface{} `mapstructure:"extensions"`
}

// ServersConfig contains configuration for the server manager
type ServersConfig struct {
	// DefaultServer is the name of the default server to use
	DefaultServer string `mapstructure:"default_server"`

	// Instances is a map of server name to server configuration
	Instances map[string]ServerInstanceConfig `mapstructure:"instances"`

	// Global timeout defaults (can be overridden per instance)
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	DrainTimeout    time.Duration `mapstructure:"drain_timeout"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
}

// ServerInstanceConfig defines configuration for a single server instance
type ServerInstanceConfig struct {
	// Name is the unique name of this server instance
	Name string `mapstructure:"name"`

	// Host is the host to bind to (e.g., "localhost", "0.0.0.0", "")
	Host string `mapstructure:"host"`

	// Port is the port number to listen on
	Port int `mapstructure:"port"`

	// Description is a human-readable description of this server
	Description string `mapstructure:"description"`

	// GZIP enables GZIP compression middleware
	GZIP bool `mapstructure:"gzip"`

	// TLS/HTTPS configuration options (mutually exclusive)
	// Option 1: Provide certificate and key files directly
	SSLCert string `mapstructure:"ssl_cert"`
	SSLKey  string `mapstructure:"ssl_key"`

	// Option 2: Use self-signed certificate (for development/testing)
	SelfSignedSSL bool `mapstructure:"self_signed_ssl"`

	// Option 3: Use Let's Encrypt / AutoTLS
	AutoTLS         bool     `mapstructure:"auto_tls"`
	AutoTLSDomains  []string `mapstructure:"auto_tls_domains"`
	AutoTLSCacheDir string   `mapstructure:"auto_tls_cache_dir"`
	AutoTLSEmail    string   `mapstructure:"auto_tls_email"`

	// Timeout configurations (overrides global defaults)
	ShutdownTimeout *time.Duration `mapstructure:"shutdown_timeout"`
	DrainTimeout    *time.Duration `mapstructure:"drain_timeout"`
	ReadTimeout     *time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    *time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     *time.Duration `mapstructure:"idle_timeout"`

	// Tags for organization and filtering
	Tags map[string]string `mapstructure:"tags"`
}

// TracingConfig holds OpenTelemetry tracing configuration
type TracingConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	ServiceName    string `mapstructure:"service_name"`
	ServiceVersion string `mapstructure:"service_version"`
	Endpoint       string `mapstructure:"endpoint"`
}

// CacheConfig holds cache provider configuration
type CacheConfig struct {
	Provider string         `mapstructure:"provider"` // memory, redis, memcache
	Redis    RedisConfig    `mapstructure:"redis"`
	Memcache MemcacheConfig `mapstructure:"memcache"`
}

// RedisConfig holds Redis-specific configuration
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// MemcacheConfig holds Memcache-specific configuration
type MemcacheConfig struct {
	Servers      []string      `mapstructure:"servers"`
	MaxIdleConns int           `mapstructure:"max_idle_conns"`
	Timeout      time.Duration `mapstructure:"timeout"`
}

// LoggerConfig holds logger configuration
type LoggerConfig struct {
	Dev  bool   `mapstructure:"dev"`
	Path string `mapstructure:"path"`
}

// MiddlewareConfig holds middleware configuration
type MiddlewareConfig struct {
	RateLimitRPS   float64 `mapstructure:"rate_limit_rps"`
	RateLimitBurst int     `mapstructure:"rate_limit_burst"`
	MaxRequestSize int64   `mapstructure:"max_request_size"`
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	AllowedMethods []string `mapstructure:"allowed_methods"`
	AllowedHeaders []string `mapstructure:"allowed_headers"`
	MaxAge         int      `mapstructure:"max_age"`
}

// ErrorTrackingConfig holds error tracking configuration
type ErrorTrackingConfig struct {
	Enabled          bool    `mapstructure:"enabled"`
	Provider         string  `mapstructure:"provider"`           // sentry, noop
	DSN              string  `mapstructure:"dsn"`                // Sentry DSN
	Environment      string  `mapstructure:"environment"`        // e.g., production, staging, development
	Release          string  `mapstructure:"release"`            // Application version/release
	Debug            bool    `mapstructure:"debug"`              // Enable debug mode
	SampleRate       float64 `mapstructure:"sample_rate"`        // Error sample rate (0.0-1.0)
	TracesSampleRate float64 `mapstructure:"traces_sample_rate"` // Traces sample rate (0.0-1.0)
}

// EventBrokerConfig contains configuration for the event broker
type EventBrokerConfig struct {
	Enabled     bool                         `mapstructure:"enabled"`
	Provider    string                       `mapstructure:"provider"` // memory, redis, nats, database
	Mode        string                       `mapstructure:"mode"`     // sync, async
	WorkerCount int                          `mapstructure:"worker_count"`
	BufferSize  int                          `mapstructure:"buffer_size"`
	InstanceID  string                       `mapstructure:"instance_id"`
	Redis       EventBrokerRedisConfig       `mapstructure:"redis"`
	NATS        EventBrokerNATSConfig        `mapstructure:"nats"`
	Database    EventBrokerDatabaseConfig    `mapstructure:"database"`
	RetryPolicy EventBrokerRetryPolicyConfig `mapstructure:"retry_policy"`
}

// EventBrokerRedisConfig contains Redis-specific configuration
type EventBrokerRedisConfig struct {
	StreamName    string `mapstructure:"stream_name"`
	ConsumerGroup string `mapstructure:"consumer_group"`
	MaxLen        int64  `mapstructure:"max_len"`
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Password      string `mapstructure:"password"`
	DB            int    `mapstructure:"db"`
}

// EventBrokerNATSConfig contains NATS-specific configuration
type EventBrokerNATSConfig struct {
	URL        string        `mapstructure:"url"`
	StreamName string        `mapstructure:"stream_name"`
	Subjects   []string      `mapstructure:"subjects"`
	Storage    string        `mapstructure:"storage"` // file, memory
	MaxAge     time.Duration `mapstructure:"max_age"`
}

// EventBrokerDatabaseConfig contains database provider configuration
type EventBrokerDatabaseConfig struct {
	TableName    string        `mapstructure:"table_name"`
	Channel      string        `mapstructure:"channel"` // PostgreSQL NOTIFY channel name
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

// EventBrokerRetryPolicyConfig contains retry policy configuration
type EventBrokerRetryPolicyConfig struct {
	MaxRetries    int           `mapstructure:"max_retries"`
	InitialDelay  time.Duration `mapstructure:"initial_delay"`
	MaxDelay      time.Duration `mapstructure:"max_delay"`
	BackoffFactor float64       `mapstructure:"backoff_factor"`
}

// PathsConfig contains configuration for named file system paths
// This is a map of path name to file system path
// Example: "data_dir": "/var/lib/myapp/data"
type PathsConfig map[string]string
