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
		{"servers.default_server", cfg.Servers.DefaultServer, "default"},
		{"servers.shutdown_timeout", cfg.Servers.ShutdownTimeout, 30 * time.Second},
		{"tracing.enabled", cfg.Tracing.Enabled, false},
		{"tracing.service_name", cfg.Tracing.ServiceName, "resolvespec"},
		{"cache.provider", cfg.Cache.Provider, "memory"},
		{"cache.redis.host", cfg.Cache.Redis.Host, "localhost"},
		{"cache.redis.port", cfg.Cache.Redis.Port, 6379},
		{"logger.dev", cfg.Logger.Dev, false},
		{"middleware.rate_limit_rps", cfg.Middleware.RateLimitRPS, 100.0},
		{"middleware.rate_limit_burst", cfg.Middleware.RateLimitBurst, 200},
	}

	// Test default server instance
	defaultServer, ok := cfg.Servers.Instances["default"]
	if !ok {
		t.Fatal("Default server instance not found")
	}
	if defaultServer.Port != 8080 {
		t.Errorf("default server port: got %d, want 8080", defaultServer.Port)
	}
	if defaultServer.Name != "default" {
		t.Errorf("default server name: got %s, want default", defaultServer.Name)
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
	os.Setenv("RESOLVESPEC_SERVERS_INSTANCES_DEFAULT_PORT", "9090")
	os.Setenv("RESOLVESPEC_TRACING_ENABLED", "true")
	os.Setenv("RESOLVESPEC_CACHE_PROVIDER", "redis")
	os.Setenv("RESOLVESPEC_LOGGER_DEV", "true")
	defer func() {
		os.Unsetenv("RESOLVESPEC_SERVERS_INSTANCES_DEFAULT_PORT")
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

	// Test server port override
	defaultServer := cfg.Servers.Instances["default"]
	if defaultServer.Port != 9090 {
		t.Errorf("server port: got %d, want 9090", defaultServer.Port)
	}
}

func TestProgrammaticConfiguration(t *testing.T) {
	mgr := NewManager()
	mgr.Set("servers.instances.default.port", 7070)
	mgr.Set("tracing.service_name", "test-service")

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if cfg.Servers.Instances["default"].Port != 7070 {
		t.Errorf("server port: got %d, want 7070", cfg.Servers.Instances["default"].Port)
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
	os.Setenv("MYAPP_SERVERS_INSTANCES_DEFAULT_PORT", "5000")
	defer os.Unsetenv("MYAPP_SERVERS_INSTANCES_DEFAULT_PORT")

	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if cfg.Servers.Instances["default"].Port != 5000 {
		t.Errorf("server port: got %d, want 5000", cfg.Servers.Instances["default"].Port)
	}
}

func TestServersConfig(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test default server exists
	if cfg.Servers.DefaultServer != "default" {
		t.Errorf("Expected default_server to be 'default', got %s", cfg.Servers.DefaultServer)
	}

	// Test default instance
	defaultServer, ok := cfg.Servers.Instances["default"]
	if !ok {
		t.Fatal("Default server instance not found")
	}

	if defaultServer.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", defaultServer.Port)
	}

	if defaultServer.Name != "default" {
		t.Errorf("Expected default name 'default', got %s", defaultServer.Name)
	}

	if defaultServer.Description != "Default HTTP server" {
		t.Errorf("Expected description 'Default HTTP server', got %s", defaultServer.Description)
	}
}

func TestMultipleServerInstances(t *testing.T) {
	mgr := NewManager()

	// Add additional server instances (default instance exists from defaults)
	mgr.Set("servers.default_server", "api")
	mgr.Set("servers.instances.api.name", "api")
	mgr.Set("servers.instances.api.host", "0.0.0.0")
	mgr.Set("servers.instances.api.port", 8080)
	mgr.Set("servers.instances.admin.name", "admin")
	mgr.Set("servers.instances.admin.host", "localhost")
	mgr.Set("servers.instances.admin.port", 8081)

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Should have default + api + admin = 3 instances
	if len(cfg.Servers.Instances) < 2 {
		t.Errorf("Expected at least 2 server instances, got %d", len(cfg.Servers.Instances))
	}

	// Verify api instance
	apiServer, ok := cfg.Servers.Instances["api"]
	if !ok {
		t.Fatal("API server instance not found")
	}
	if apiServer.Port != 8080 {
		t.Errorf("Expected API port 8080, got %d", apiServer.Port)
	}

	// Verify admin instance
	adminServer, ok := cfg.Servers.Instances["admin"]
	if !ok {
		t.Fatal("Admin server instance not found")
	}
	if adminServer.Port != 8081 {
		t.Errorf("Expected admin port 8081, got %d", adminServer.Port)
	}

	// Validate default server
	if err := cfg.Servers.Validate(); err != nil {
		t.Errorf("Server config validation failed: %v", err)
	}

	// Get default
	defaultSrv, err := cfg.Servers.GetDefault()
	if err != nil {
		t.Fatalf("Failed to get default server: %v", err)
	}
	if defaultSrv.Name != "api" {
		t.Errorf("Expected default server 'api', got '%s'", defaultSrv.Name)
	}
}

func TestExtensionsField(t *testing.T) {
	mgr := NewManager()

	// Set custom extensions
	mgr.Set("extensions.custom_feature.enabled", true)
	mgr.Set("extensions.custom_feature.api_key", "test-key")
	mgr.Set("extensions.another_extension.timeout", "5s")

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	if cfg.Extensions == nil {
		t.Fatal("Extensions should not be nil")
	}

	// Verify extensions are accessible
	customFeature := mgr.Get("extensions.custom_feature")
	if customFeature == nil {
		t.Error("custom_feature extension not found")
	}

	// Verify via config manager methods
	if !mgr.GetBool("extensions.custom_feature.enabled") {
		t.Error("Expected custom_feature.enabled to be true")
	}

	if mgr.GetString("extensions.custom_feature.api_key") != "test-key" {
		t.Error("Expected api_key to be 'test-key'")
	}
}

func TestServerInstanceValidation(t *testing.T) {
	tests := []struct {
		name      string
		instance  ServerInstanceConfig
		expectErr bool
	}{
		{
			name: "valid basic config",
			instance: ServerInstanceConfig{
				Name: "test",
				Port: 8080,
			},
			expectErr: false,
		},
		{
			name: "invalid port - too high",
			instance: ServerInstanceConfig{
				Name: "test",
				Port: 99999,
			},
			expectErr: true,
		},
		{
			name: "invalid port - zero",
			instance: ServerInstanceConfig{
				Name: "test",
				Port: 0,
			},
			expectErr: true,
		},
		{
			name: "empty name",
			instance: ServerInstanceConfig{
				Name: "",
				Port: 8080,
			},
			expectErr: true,
		},
		{
			name: "conflicting TLS options",
			instance: ServerInstanceConfig{
				Name:          "test",
				Port:          8080,
				SelfSignedSSL: true,
				AutoTLS:       true,
			},
			expectErr: true,
		},
		{
			name: "incomplete SSL cert config",
			instance: ServerInstanceConfig{
				Name:    "test",
				Port:    8080,
				SSLCert: "/path/to/cert.pem",
				// Missing SSLKey
			},
			expectErr: true,
		},
		{
			name: "AutoTLS without domains",
			instance: ServerInstanceConfig{
				Name:    "test",
				Port:    8080,
				AutoTLS: true,
				// Missing AutoTLSDomains
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.instance.Validate()
			if tt.expectErr && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestApplyGlobalDefaults(t *testing.T) {
	globals := ServersConfig{
		ShutdownTimeout: 30 * time.Second,
		DrainTimeout:    25 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
	}

	instance := ServerInstanceConfig{
		Name: "test",
		Port: 8080,
	}

	// Apply global defaults
	instance.ApplyGlobalDefaults(globals)

	// Check that defaults were applied
	if instance.ShutdownTimeout == nil || *instance.ShutdownTimeout != 30*time.Second {
		t.Error("ShutdownTimeout not applied correctly")
	}
	if instance.DrainTimeout == nil || *instance.DrainTimeout != 25*time.Second {
		t.Error("DrainTimeout not applied correctly")
	}
	if instance.ReadTimeout == nil || *instance.ReadTimeout != 10*time.Second {
		t.Error("ReadTimeout not applied correctly")
	}
	if instance.WriteTimeout == nil || *instance.WriteTimeout != 10*time.Second {
		t.Error("WriteTimeout not applied correctly")
	}
	if instance.IdleTimeout == nil || *instance.IdleTimeout != 120*time.Second {
		t.Error("IdleTimeout not applied correctly")
	}

	// Test that explicit overrides are not replaced
	customTimeout := 60 * time.Second
	instance2 := ServerInstanceConfig{
		Name:            "test2",
		Port:            8081,
		ShutdownTimeout: &customTimeout,
	}

	instance2.ApplyGlobalDefaults(globals)

	if instance2.ShutdownTimeout == nil || *instance2.ShutdownTimeout != 60*time.Second {
		t.Error("Custom ShutdownTimeout was overridden")
	}
}

func TestPathsConfig(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test default paths exist
	if !cfg.Paths.Has("data_dir") {
		t.Error("Expected data_dir path to exist")
	}
	if !cfg.Paths.Has("config_dir") {
		t.Error("Expected config_dir path to exist")
	}
	if !cfg.Paths.Has("logs_dir") {
		t.Error("Expected logs_dir path to exist")
	}
	if !cfg.Paths.Has("temp_dir") {
		t.Error("Expected temp_dir path to exist")
	}

	// Test Get method
	dataDir, err := cfg.Paths.Get("data_dir")
	if err != nil {
		t.Errorf("Failed to get data_dir: %v", err)
	}
	if dataDir != "./data" {
		t.Errorf("Expected data_dir to be './data', got '%s'", dataDir)
	}

	// Test GetOrDefault
	existing := cfg.Paths.GetOrDefault("data_dir", "/default/path")
	if existing != "./data" {
		t.Errorf("Expected existing path, got '%s'", existing)
	}

	nonExisting := cfg.Paths.GetOrDefault("nonexistent", "/default/path")
	if nonExisting != "/default/path" {
		t.Errorf("Expected default path, got '%s'", nonExisting)
	}
}

func TestPathsConfigMethods(t *testing.T) {
	pc := PathsConfig{
		"base": "/var/myapp",
		"data": "/var/myapp/data",
	}

	// Test Get
	path, err := pc.Get("base")
	if err != nil {
		t.Errorf("Failed to get path: %v", err)
	}
	if path != "/var/myapp" {
		t.Errorf("Expected '/var/myapp', got '%s'", path)
	}

	// Test Get non-existent
	_, err = pc.Get("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}

	// Test Set
	pc.Set("new_path", "/new/location")
	newPath, err := pc.Get("new_path")
	if err != nil {
		t.Errorf("Failed to get newly set path: %v", err)
	}
	if newPath != "/new/location" {
		t.Errorf("Expected '/new/location', got '%s'", newPath)
	}

	// Test Has
	if !pc.Has("base") {
		t.Error("Expected 'base' path to exist")
	}
	if pc.Has("nonexistent") {
		t.Error("Expected 'nonexistent' path to not exist")
	}

	// Test List
	names := pc.List()
	if len(names) != 3 {
		t.Errorf("Expected 3 paths, got %d", len(names))
	}

	// Test Join
	joined, err := pc.Join("base", "subdir", "file.txt")
	if err != nil {
		t.Errorf("Failed to join paths: %v", err)
	}
	expected := "/var/myapp/subdir/file.txt"
	if joined != expected {
		t.Errorf("Expected '%s', got '%s'", expected, joined)
	}
}

func TestPathsConfigEnvironmentVariables(t *testing.T) {
	// Set environment variables for paths
	os.Setenv("RESOLVESPEC_PATHS_DATA_DIR", "/custom/data")
	os.Setenv("RESOLVESPEC_PATHS_LOGS_DIR", "/custom/logs")
	defer func() {
		os.Unsetenv("RESOLVESPEC_PATHS_DATA_DIR")
		os.Unsetenv("RESOLVESPEC_PATHS_LOGS_DIR")
	}()

	mgr := NewManager()
	if err := mgr.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test environment variable override of existing default path
	dataDir, err := cfg.Paths.Get("data_dir")
	if err != nil {
		t.Errorf("Failed to get data_dir: %v", err)
	}
	if dataDir != "/custom/data" {
		t.Errorf("Expected '/custom/data', got '%s'", dataDir)
	}

	// Test another environment variable override
	logsDir, err := cfg.Paths.Get("logs_dir")
	if err != nil {
		t.Errorf("Failed to get logs_dir: %v", err)
	}
	if logsDir != "/custom/logs" {
		t.Errorf("Expected '/custom/logs', got '%s'", logsDir)
	}
}

func TestPathsConfigProgrammatic(t *testing.T) {
	mgr := NewManager()

	// Set custom paths programmatically
	mgr.Set("paths.custom_dir", "/my/custom/dir")
	mgr.Set("paths.cache_dir", "/var/cache/myapp")

	cfg, err := mgr.GetConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Verify custom paths
	customDir, err := cfg.Paths.Get("custom_dir")
	if err != nil {
		t.Errorf("Failed to get custom_dir: %v", err)
	}
	if customDir != "/my/custom/dir" {
		t.Errorf("Expected '/my/custom/dir', got '%s'", customDir)
	}

	cacheDir, err := cfg.Paths.Get("cache_dir")
	if err != nil {
		t.Errorf("Failed to get cache_dir: %v", err)
	}
	if cacheDir != "/var/cache/myapp" {
		t.Errorf("Expected '/var/cache/myapp', got '%s'", cacheDir)
	}
}
