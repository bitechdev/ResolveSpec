package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Manager handles configuration loading from multiple sources
type Manager struct {
	v *viper.Viper
}

// NewManager creates a new configuration manager with defaults
func NewManager() *Manager {
	v := viper.New()

	// Set configuration file settings
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/resolvespec")
	v.AddConfigPath("$HOME/.resolvespec")

	// Enable environment variable support
	v.SetEnvPrefix("RESOLVESPEC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set default values
	setDefaults(v)

	return &Manager{v: v}
}

// NewManagerWithOptions creates a new configuration manager with custom options
func NewManagerWithOptions(opts ...Option) *Manager {
	m := NewManager()
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Option is a functional option for configuring the Manager
type Option func(*Manager)

// WithConfigFile sets a specific config file path
func WithConfigFile(path string) Option {
	return func(m *Manager) {
		m.v.SetConfigFile(path)
	}
}

// WithConfigName sets the config file name (without extension)
func WithConfigName(name string) Option {
	return func(m *Manager) {
		m.v.SetConfigName(name)
	}
}

// WithConfigPath adds a path to search for config files
func WithConfigPath(path string) Option {
	return func(m *Manager) {
		m.v.AddConfigPath(path)
	}
}

// WithEnvPrefix sets the environment variable prefix
func WithEnvPrefix(prefix string) Option {
	return func(m *Manager) {
		m.v.SetEnvPrefix(prefix)
	}
}

// Load attempts to load configuration from file and environment
func (m *Manager) Load() error {
	// Try to read config file (not an error if it doesn't exist)
	if err := m.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; will rely on defaults and env vars
	}

	return nil
}

// GetConfig returns the complete configuration
func (m *Manager) GetConfig() (*Config, error) {
	var cfg Config
	if err := m.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

// Get returns a configuration value by key
func (m *Manager) Get(key string) interface{} {
	return m.v.Get(key)
}

// GetString returns a string configuration value
func (m *Manager) GetString(key string) string {
	return m.v.GetString(key)
}

// GetInt returns an int configuration value
func (m *Manager) GetInt(key string) int {
	return m.v.GetInt(key)
}

// GetBool returns a bool configuration value
func (m *Manager) GetBool(key string) bool {
	return m.v.GetBool(key)
}

// Set sets a configuration value
func (m *Manager) Set(key string, value interface{}) {
	m.v.Set(key, value)
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.addr", ":8080")
	v.SetDefault("server.shutdown_timeout", "30s")
	v.SetDefault("server.drain_timeout", "25s")
	v.SetDefault("server.read_timeout", "10s")
	v.SetDefault("server.write_timeout", "10s")
	v.SetDefault("server.idle_timeout", "120s")

	// Tracing defaults
	v.SetDefault("tracing.enabled", false)
	v.SetDefault("tracing.service_name", "resolvespec")
	v.SetDefault("tracing.service_version", "1.0.0")
	v.SetDefault("tracing.endpoint", "")

	// Cache defaults
	v.SetDefault("cache.provider", "memory")
	v.SetDefault("cache.redis.host", "localhost")
	v.SetDefault("cache.redis.port", 6379)
	v.SetDefault("cache.redis.password", "")
	v.SetDefault("cache.redis.db", 0)
	v.SetDefault("cache.memcache.servers", []string{"localhost:11211"})
	v.SetDefault("cache.memcache.max_idle_conns", 10)
	v.SetDefault("cache.memcache.timeout", "100ms")

	// Logger defaults
	v.SetDefault("logger.dev", false)
	v.SetDefault("logger.path", "")

	// Middleware defaults
	v.SetDefault("middleware.rate_limit_rps", 100.0)
	v.SetDefault("middleware.rate_limit_burst", 200)
	v.SetDefault("middleware.max_request_size", 10485760) // 10MB

	// CORS defaults
	v.SetDefault("cors.allowed_origins", []string{"*"})
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"*"})
	v.SetDefault("cors.max_age", 3600)

	// Database defaults
	v.SetDefault("database.url", "")

	// Event Broker defaults
	v.SetDefault("event_broker.enabled", false)
	v.SetDefault("event_broker.provider", "memory")
	v.SetDefault("event_broker.mode", "async")
	v.SetDefault("event_broker.worker_count", 10)
	v.SetDefault("event_broker.buffer_size", 1000)
	v.SetDefault("event_broker.instance_id", "")

	// Event Broker - Redis defaults
	v.SetDefault("event_broker.redis.stream_name", "resolvespec:events")
	v.SetDefault("event_broker.redis.consumer_group", "resolvespec-workers")
	v.SetDefault("event_broker.redis.max_len", 10000)
	v.SetDefault("event_broker.redis.host", "localhost")
	v.SetDefault("event_broker.redis.port", 6379)
	v.SetDefault("event_broker.redis.password", "")
	v.SetDefault("event_broker.redis.db", 0)

	// Event Broker - NATS defaults
	v.SetDefault("event_broker.nats.url", "nats://localhost:4222")
	v.SetDefault("event_broker.nats.stream_name", "RESOLVESPEC_EVENTS")
	v.SetDefault("event_broker.nats.subjects", []string{"events.>"})
	v.SetDefault("event_broker.nats.storage", "file")
	v.SetDefault("event_broker.nats.max_age", "24h")

	// Event Broker - Database defaults
	v.SetDefault("event_broker.database.table_name", "events")
	v.SetDefault("event_broker.database.channel", "resolvespec_events")
	v.SetDefault("event_broker.database.poll_interval", "1s")

	// Event Broker - Retry Policy defaults
	v.SetDefault("event_broker.retry_policy.max_retries", 3)
	v.SetDefault("event_broker.retry_policy.initial_delay", "1s")
	v.SetDefault("event_broker.retry_policy.max_delay", "30s")
	v.SetDefault("event_broker.retry_policy.backoff_factor", 2.0)
}
