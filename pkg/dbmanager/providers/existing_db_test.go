package providers

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewExistingDBProvider(t *testing.T) {
	// Open a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create provider
	provider := NewExistingDBProvider(db, "test-db")
	if provider == nil {
		t.Fatal("Expected provider to be created")
	}

	if provider.name != "test-db" {
		t.Errorf("Expected name 'test-db', got '%s'", provider.name)
	}
}

func TestExistingDBProvider_Connect(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	provider := NewExistingDBProvider(db, "test-db")
	ctx := context.Background()

	// Connect should verify the connection works
	err = provider.Connect(ctx, nil)
	if err != nil {
		t.Errorf("Expected Connect to succeed, got error: %v", err)
	}
}

func TestExistingDBProvider_Connect_NilDB(t *testing.T) {
	provider := NewExistingDBProvider(nil, "test-db")
	ctx := context.Background()

	err := provider.Connect(ctx, nil)
	if err == nil {
		t.Error("Expected Connect to fail with nil database")
	}
}

func TestExistingDBProvider_GetNative(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	provider := NewExistingDBProvider(db, "test-db")

	nativeDB, err := provider.GetNative()
	if err != nil {
		t.Errorf("Expected GetNative to succeed, got error: %v", err)
	}

	if nativeDB != db {
		t.Error("Expected GetNative to return the same database instance")
	}
}

func TestExistingDBProvider_GetNative_NilDB(t *testing.T) {
	provider := NewExistingDBProvider(nil, "test-db")

	_, err := provider.GetNative()
	if err == nil {
		t.Error("Expected GetNative to fail with nil database")
	}
}

func TestExistingDBProvider_HealthCheck(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	provider := NewExistingDBProvider(db, "test-db")
	ctx := context.Background()

	err = provider.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Expected HealthCheck to succeed, got error: %v", err)
	}
}

func TestExistingDBProvider_HealthCheck_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	provider := NewExistingDBProvider(db, "test-db")

	// Close the database
	db.Close()

	ctx := context.Background()
	err = provider.HealthCheck(ctx)
	if err == nil {
		t.Error("Expected HealthCheck to fail with closed database")
	}
}

func TestExistingDBProvider_GetMongo(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	provider := NewExistingDBProvider(db, "test-db")

	_, err = provider.GetMongo()
	if err != ErrNotMongoDB {
		t.Errorf("Expected ErrNotMongoDB, got: %v", err)
	}
}

func TestExistingDBProvider_Stats(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Set some connection pool settings to test stats
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	provider := NewExistingDBProvider(db, "test-db")

	stats := provider.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}

	if stats.Name != "test-db" {
		t.Errorf("Expected stats.Name to be 'test-db', got '%s'", stats.Name)
	}

	if stats.Type != "sql" {
		t.Errorf("Expected stats.Type to be 'sql', got '%s'", stats.Type)
	}

	if !stats.Connected {
		t.Error("Expected stats.Connected to be true")
	}
}

func TestExistingDBProvider_Close(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	provider := NewExistingDBProvider(db, "test-db")

	err = provider.Close()
	if err != nil {
		t.Errorf("Expected Close to succeed, got error: %v", err)
	}

	// Verify the database is closed
	err = db.Ping()
	if err == nil {
		t.Error("Expected database to be closed")
	}
}

func TestExistingDBProvider_Close_NilDB(t *testing.T) {
	provider := NewExistingDBProvider(nil, "test-db")

	err := provider.Close()
	if err != nil {
		t.Errorf("Expected Close to succeed with nil database, got error: %v", err)
	}
}
