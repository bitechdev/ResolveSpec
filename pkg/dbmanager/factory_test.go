package dbmanager

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewConnectionFromDB(t *testing.T) {
	// Open a SQLite in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a connection from the existing database
	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	if conn == nil {
		t.Fatal("Expected connection to be created")
	}

	// Verify connection properties
	if conn.Name() != "test-connection" {
		t.Errorf("Expected name 'test-connection', got '%s'", conn.Name())
	}

	if conn.Type() != DatabaseTypeSQLite {
		t.Errorf("Expected type DatabaseTypeSQLite, got '%s'", conn.Type())
	}
}

func TestNewConnectionFromDB_Connect(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	// Connect should verify the existing connection works
	err = conn.Connect(ctx)
	if err != nil {
		t.Errorf("Expected Connect to succeed, got error: %v", err)
	}

	// Cleanup
	conn.Close()
}

func TestNewConnectionFromDB_Native(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Get native DB
	nativeDB, err := conn.Native()
	if err != nil {
		t.Errorf("Expected Native to succeed, got error: %v", err)
	}

	if nativeDB != db {
		t.Error("Expected Native to return the same database instance")
	}
}

func TestNewConnectionFromDB_Bun(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Get Bun ORM
	bunDB, err := conn.Bun()
	if err != nil {
		t.Errorf("Expected Bun to succeed, got error: %v", err)
	}

	if bunDB == nil {
		t.Error("Expected Bun to return a non-nil instance")
	}
}

func TestNewConnectionFromDB_GORM(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Get GORM
	gormDB, err := conn.GORM()
	if err != nil {
		t.Errorf("Expected GORM to succeed, got error: %v", err)
	}

	if gormDB == nil {
		t.Error("Expected GORM to return a non-nil instance")
	}
}

func TestNewConnectionFromDB_HealthCheck(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Health check should succeed
	err = conn.HealthCheck(ctx)
	if err != nil {
		t.Errorf("Expected HealthCheck to succeed, got error: %v", err)
	}
}

func TestNewConnectionFromDB_Stats(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-connection", DatabaseTypeSQLite, db)
	ctx := context.Background()

	err = conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	stats := conn.Stats()
	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}

	if stats.Name != "test-connection" {
		t.Errorf("Expected stats.Name to be 'test-connection', got '%s'", stats.Name)
	}

	if stats.Type != DatabaseTypeSQLite {
		t.Errorf("Expected stats.Type to be DatabaseTypeSQLite, got '%s'", stats.Type)
	}

	if !stats.Connected {
		t.Error("Expected stats.Connected to be true")
	}
}

func TestNewConnectionFromDB_PostgreSQL(t *testing.T) {
	// This test just verifies the factory works with PostgreSQL type
	// It won't actually connect since we're using SQLite
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	conn := NewConnectionFromDB("test-pg", DatabaseTypePostgreSQL, db)
	if conn == nil {
		t.Fatal("Expected connection to be created")
	}

	if conn.Type() != DatabaseTypePostgreSQL {
		t.Errorf("Expected type DatabaseTypePostgreSQL, got '%s'", conn.Type())
	}
}
