package dbmanager

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestBackgroundHealthChecker(t *testing.T) {
	// Create a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create manager config with a short health check interval for testing
	cfg := ManagerConfig{
		DefaultConnection: "test",
		Connections: map[string]ConnectionConfig{
			"test": {
				Name:     "test",
				Type:     DatabaseTypeSQLite,
				FilePath: ":memory:",
			},
		},
		HealthCheckInterval: 1 * time.Second, // Short interval for testing
		EnableAutoReconnect: true,
	}

	// Create manager
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Connect - this should start the background health checker
	ctx := context.Background()
	err = mgr.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer mgr.Close()

	// Get the connection to verify it's healthy
	conn, err := mgr.Get("test")
	if err != nil {
		t.Fatalf("Failed to get connection: %v", err)
	}

	// Verify initial health check
	err = conn.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Initial health check failed: %v", err)
	}

	// Wait for a few health check cycles
	time.Sleep(3 * time.Second)

	// Get stats to verify the connection is still healthy
	stats := conn.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}

	if !stats.Connected {
		t.Error("Expected connection to still be connected")
	}

	if stats.HealthCheckStatus == "" {
		t.Error("Expected health check status to be set")
	}

	// Verify the manager has started the health checker
	if cm, ok := mgr.(*connectionManager); ok {
		if cm.healthTicker == nil {
			t.Error("Expected health ticker to be running")
		}
	}
}

func TestDefaultHealthCheckInterval(t *testing.T) {
	// Verify the default health check interval is 15 seconds
	defaults := DefaultManagerConfig()

	expectedInterval := 15 * time.Second
	if defaults.HealthCheckInterval != expectedInterval {
		t.Errorf("Expected default health check interval to be %v, got %v",
			expectedInterval, defaults.HealthCheckInterval)
	}

	if !defaults.EnableAutoReconnect {
		t.Error("Expected EnableAutoReconnect to be true by default")
	}
}

func TestApplyDefaultsEnablesAutoReconnect(t *testing.T) {
	// Create a config without setting EnableAutoReconnect
	cfg := ManagerConfig{
		Connections: map[string]ConnectionConfig{
			"test": {
				Name:     "test",
				Type:     DatabaseTypeSQLite,
				FilePath: ":memory:",
			},
		},
	}

	// Verify it's false initially (Go's zero value for bool)
	if cfg.EnableAutoReconnect {
		t.Error("Expected EnableAutoReconnect to be false before ApplyDefaults")
	}

	// Apply defaults
	cfg.ApplyDefaults()

	// Verify it's now true
	if !cfg.EnableAutoReconnect {
		t.Error("Expected EnableAutoReconnect to be true after ApplyDefaults")
	}

	// Verify health check interval is also set
	if cfg.HealthCheckInterval != 15*time.Second {
		t.Errorf("Expected health check interval to be 15s, got %v", cfg.HealthCheckInterval)
	}
}

func TestManagerHealthCheck(t *testing.T) {
	// Create a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create manager config
	cfg := ManagerConfig{
		DefaultConnection: "test",
		Connections: map[string]ConnectionConfig{
			"test": {
				Name:     "test",
				Type:     DatabaseTypeSQLite,
				FilePath: ":memory:",
			},
		},
		HealthCheckInterval: 15 * time.Second,
		EnableAutoReconnect: true,
	}

	// Create and connect manager
	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	err = mgr.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer mgr.Close()

	// Perform health check on all connections
	err = mgr.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Health check failed: %v", err)
	}

	// Get stats
	stats := mgr.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}

	if stats.TotalConnections != 1 {
		t.Errorf("Expected 1 total connection, got %d", stats.TotalConnections)
	}

	if stats.HealthyCount != 1 {
		t.Errorf("Expected 1 healthy connection, got %d", stats.HealthyCount)
	}

	if stats.UnhealthyCount != 0 {
		t.Errorf("Expected 0 unhealthy connections, got %d", stats.UnhealthyCount)
	}
}

func TestManagerStatsAfterClose(t *testing.T) {
	cfg := ManagerConfig{
		DefaultConnection: "test",
		Connections: map[string]ConnectionConfig{
			"test": {
				Name:     "test",
				Type:     DatabaseTypeSQLite,
				FilePath: ":memory:",
			},
		},
		HealthCheckInterval: 15 * time.Second,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	ctx := context.Background()
	err = mgr.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Close the manager
	err = mgr.Close()
	if err != nil {
		t.Errorf("Failed to close manager: %v", err)
	}

	// Stats should show no connections
	stats := mgr.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("Expected 0 total connections after close, got %d", stats.TotalConnections)
	}
}
