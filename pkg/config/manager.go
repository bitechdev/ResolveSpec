package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// Manager handles configuration loading from multiple sources.
// viper.Viper is not safe for concurrent use, so every access to it is guarded by mu.
type Manager struct {
	mu sync.RWMutex
	v  *viper.Viper
}

var (
	configInstance *Manager
	configMu       sync.Mutex
)

// GetConfigManager returns a singleton configuration manager instance
func GetConfigManager() *Manager {
	configMu.Lock()
	defer configMu.Unlock()
	if configInstance == nil {
		configInstance = NewManager()
	}
	return configInstance
}

// SetConfigManager publishes m as the global manager returned by GetConfigManager.
// NewManager no longer does this implicitly.
func SetConfigManager(m *Manager) {
	configMu.Lock()
	defer configMu.Unlock()
	configInstance = m
}

// NewManager creates a new, isolated configuration manager with defaults.
// It does not replace the global manager; use SetConfigManager for that.
func NewManager() *Manager {
	v := viper.New()

	// Set configuration file settings
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	// Most trusted location first; the working directory is the least trustworthy
	// and is searched last (viper takes the first match).
	v.AddConfigPath("/etc/resolvespec")
	v.AddConfigPath("$HOME/.resolvespec")
	v.AddConfigPath("./config")
	v.AddConfigPath(".")

	// Saved configs may contain secrets; never write them world-readable
	v.SetConfigPermissions(0o600)

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
		m.mu.Lock()
		defer m.mu.Unlock()
		m.v.SetConfigFile(path)
	}
}

// WithConfigName sets the config file name (without extension)
func WithConfigName(name string) Option {
	return func(m *Manager) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.v.SetConfigName(name)
	}
}

// WithConfigPath adds a path to search for config files
func WithConfigPath(path string) Option {
	return func(m *Manager) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.v.AddConfigPath(path)
	}
}

// WithEnvPrefix sets the environment variable prefix
func WithEnvPrefix(prefix string) Option {
	return func(m *Manager) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.v.SetEnvPrefix(prefix)
	}
}

// Load attempts to load configuration from file and environment.
// A missing config file is not an error (defaults and env vars are used); check
// ConfigFileUsed after Load to see whether a file was actually read.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found; will rely on defaults and env vars
	}

	return nil
}

// ConfigFileUsed returns the config file that was read by Load, or "" if none was
// found (i.e. the manager is running on defaults and environment variables only).
func (m *Manager) ConfigFileUsed() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.v.ConfigFileUsed()
}

// GetConfig returns the complete configuration
func (m *Manager) GetConfig() (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var cfg Config
	if err := m.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

// SetConfig sets the complete configuration atomically
func (m *Manager) SetConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.v.Set("servers", cfg.Servers)
	m.v.Set("tracing", cfg.Tracing)
	m.v.Set("cache", cfg.Cache)
	m.v.Set("logger", cfg.Logger)
	m.v.Set("error_tracking", cfg.ErrorTracking)
	m.v.Set("middleware", cfg.Middleware)
	m.v.Set("cors", cfg.CORS)
	m.v.Set("event_broker", cfg.EventBroker)
	m.v.Set("dbmanager", cfg.DBManager)
	m.v.Set("paths", cfg.Paths)
	m.v.Set("extensions", cfg.Extensions)

	return nil
}

// Get returns a configuration value by key
func (m *Manager) Get(key string) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.v.Get(key)
}

// GetString returns a string configuration value
func (m *Manager) GetString(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.v.GetString(key)
}

// GetInt returns an int configuration value
func (m *Manager) GetInt(key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.v.GetInt(key)
}

// GetBool returns a bool configuration value
func (m *Manager) GetBool(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.v.GetBool(key)
}

// Set sets a configuration value
func (m *Manager) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v.Set(key, value)
}

// SaveConfig writes the current configuration to the specified path.
// The file contains the entire merged configuration, including secrets
// (database/redis passwords, error-tracking DSN), so it is written with mode 0600.
// Prefer supplying secrets via RESOLVESPEC_* environment variables.
func (m *Manager) SaveConfig(path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := m.v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("failed to save config to %s: %w", path, err)
	}
	// viper only applies its permissions when creating the file; tighten a pre-existing one too
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to restrict permissions on %s: %w", path, err)
	}
	return nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Server defaults - new structure
	v.SetDefault("servers.default_server", "default")

	// Global server timeout defaults
	v.SetDefault("servers.shutdown_timeout", "30s")
	v.SetDefault("servers.drain_timeout", "25s")
	v.SetDefault("servers.read_timeout", "10s")
	v.SetDefault("servers.write_timeout", "10s")
	v.SetDefault("servers.idle_timeout", "120s")

	// Default server instance
	v.SetDefault("servers.instances.default.name", "default")
	v.SetDefault("servers.instances.default.host", "")
	v.SetDefault("servers.instances.default.port", 8080)
	v.SetDefault("servers.instances.default.description", "Default HTTP server")
	v.SetDefault("servers.instances.default.gzip", false)

	// Tracing defaults
	v.SetDefault("tracing.enabled", false)
	v.SetDefault("tracing.service_name", "resolvespec")
	v.SetDefault("tracing.service_version", "1.0.0")
	v.SetDefault("tracing.endpoint", "")
	v.SetDefault("tracing.insecure", false)
	v.SetDefault("tracing.sample_rate", 0.1)

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

	// Database Manager defaults
	v.SetDefault("dbmanager.default_connection", "default")
	v.SetDefault("dbmanager.max_open_conns", 25)
	v.SetDefault("dbmanager.max_idle_conns", 5)
	v.SetDefault("dbmanager.conn_max_lifetime", "30m")
	v.SetDefault("dbmanager.conn_max_idle_time", "5m")
	v.SetDefault("dbmanager.retry_attempts", 3)
	v.SetDefault("dbmanager.retry_delay", "1s")
	v.SetDefault("dbmanager.retry_max_delay", "10s")
	v.SetDefault("dbmanager.health_check_interval", "30s")
	v.SetDefault("dbmanager.enable_auto_reconnect", true)

	// Default PostgreSQL connection
	v.SetDefault("dbmanager.connections.default.name", "default")
	v.SetDefault("dbmanager.connections.default.type", "postgres")
	v.SetDefault("dbmanager.connections.default.host", "localhost")
	v.SetDefault("dbmanager.connections.default.port", 5432)
	v.SetDefault("dbmanager.connections.default.user", "postgres")
	v.SetDefault("dbmanager.connections.default.password", "")
	v.SetDefault("dbmanager.connections.default.database", "resolvespec")
	v.SetDefault("dbmanager.connections.default.sslmode", "disable")
	v.SetDefault("dbmanager.connections.default.connect_timeout", "10s")
	v.SetDefault("dbmanager.connections.default.query_timeout", "30s")
	v.SetDefault("dbmanager.connections.default.enable_tracing", false)
	v.SetDefault("dbmanager.connections.default.enable_metrics", false)
	v.SetDefault("dbmanager.connections.default.enable_logging", false)
	v.SetDefault("dbmanager.connections.default.default_orm", "bun")

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

	// Paths defaults (common directory paths)
	v.SetDefault("paths.data_dir", "./data")
	v.SetDefault("paths.config_dir", "./config")
	v.SetDefault("paths.logs_dir", "./logs")
	v.SetDefault("paths.temp_dir", "./tmp")

	// Extensions defaults (empty map)
	v.SetDefault("extensions", map[string]interface{}{})
}
