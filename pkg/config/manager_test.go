package config

import (
	"os"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("Expected manager to be non-nil")
	}

	if mgr.v == nil {
		t.Fatal("Expected viper instance to be non-nil")
	}
}

func TestDefaultValues(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test default values
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"server.addr", cfg.Server.Addr, ":8080"},
		{"server.shutdown_timeout", cfg.Server.ShutdownTimeout, 30 * time.Second},
		{"tracing.enabled", cfg.Tracing.Enabled, false},
		{"tracing.service_name", cfg.Tracing.ServiceName, "resolvespec"},
		{"cache.provider", cfg.Cache.Provider, "memory"},
		{"cache.redis.host", cfg.Cache.Redis.Host, "localhost"},
		{"cache.redis.port", cfg.Cache.Redis.Port, 6379},
		{"logger.dev", cfg.Logger.Dev, false},
		{"middleware.rate_limit_rps", cfg.Middleware.RateLimitRPS, 100.0},
		{"middleware.rate_limit_burst", cfg.Middleware.RateLimitBurst, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestEnvironmentVariableOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("RESOLVESPEC_SERVER_ADDR", ":9090")
	os.Setenv("RESOLVESPEC_TRACING_ENABLED", "true")
	os.Setenv("RESOLVESPEC_CACHE_PROVIDER", "redis")
	os.Setenv("RESOLVESPEC_LOGGER_DEV", "true")
	defer func() {
		os.Unsetenv("RESOLVESPEC_SERVER_ADDR")
		os.Unsetenv("RESOLVESPEC_TRACING_ENABLED")
		os.Unsetenv("RESOLVESPEC_CACHE_PROVIDER")
		os.Unsetenv("RESOLVESPEC_LOGGER_DEV")
	}()

	mgr := NewManager()
	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test environment variable overrides
	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"server.addr", cfg.Server.Addr, ":9090"},
		{"tracing.enabled", cfg.Tracing.Enabled, true},
		{"cache.provider", cfg.Cache.Provider, "redis"},
		{"logger.dev", cfg.Logger.Dev, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestProgrammaticConfiguration(t *testing.T) {
	mgr := NewManager()
	mgr.Set("server.addr", ":7070")
	mgr.Set("tracing.service_name", "test-service")

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if cfg.Server.Addr != ":7070" {
		t.Errorf("server.addr: got %s, want :7070", cfg.Server.Addr)
	}

	if cfg.Tracing.ServiceName != "test-service" {
		t.Errorf("tracing.service_name: got %s, want test-service", cfg.Tracing.ServiceName)
	}
}

func TestGetterMethods(t *testing.T) {
	mgr := NewManager()
	mgr.Set("test.string", "value")
	mgr.Set("test.int", 42)
	mgr.Set("test.bool", true)

	if got := mgr.GetString("test.string"); got != "value" {
		t.Errorf("GetString: got %s, want value", got)
	}

	if got := mgr.GetInt("test.int"); got != 42 {
		t.Errorf("GetInt: got %d, want 42", got)
	}

	if got := mgr.GetBool("test.bool"); !got {
		t.Errorf("GetBool: got %v, want true", got)
	}
}

func TestWithOptions(t *testing.T) {
	mgr := NewManagerWithOptions(
		WithEnvPrefix("MYAPP"),
		WithConfigName("myconfig"),
	)

	if mgr == nil {
		t.Fatal("Expected manager to be non-nil")
	}

	// Set environment variable with custom prefix
	os.Setenv("MYAPP_SERVER_ADDR", ":5000")
	defer os.Unsetenv("MYAPP_SERVER_ADDR")

	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if cfg.Server.Addr != ":5000" {
		t.Errorf("server.addr: got %s, want :5000", cfg.Server.Addr)
	}
}
