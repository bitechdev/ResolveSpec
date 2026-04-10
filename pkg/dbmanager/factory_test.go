package dbmanager

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/dbmanager/providers"
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

func TestDatabaseNativeAdapterReconnectFactory(t *testing.T) {
	conn := newSQLConnection("test-native", DatabaseTypeSQLite, ConnectionConfig{
		Name:           "test-native",
		Type:           DatabaseTypeSQLite,
		FilePath:       ":memory:",
		DefaultORM:     string(ORMTypeNative),
		ConnectTimeout: 2 * time.Second,
	}, providers.NewSQLiteProvider())

	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	db, err := conn.Database()
	if err != nil {
		t.Fatalf("Failed to get database adapter: %v", err)
	}

	adapter, ok := db.(*database.PgSQLAdapter)
	if !ok {
		t.Fatalf("Expected PgSQLAdapter, got %T", db)
	}

	underlyingBefore, ok := adapter.GetUnderlyingDB().(*sql.DB)
	if !ok {
		t.Fatalf("Expected underlying *sql.DB, got %T", adapter.GetUnderlyingDB())
	}

	if err := underlyingBefore.Close(); err != nil {
		t.Fatalf("Failed to close underlying database: %v", err)
	}

	if _, err := db.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("Expected native adapter to reconnect, got error: %v", err)
	}

	underlyingAfter, ok := adapter.GetUnderlyingDB().(*sql.DB)
	if !ok {
		t.Fatalf("Expected reconnected *sql.DB, got %T", adapter.GetUnderlyingDB())
	}

	if underlyingAfter == underlyingBefore {
		t.Fatal("Expected adapter to swap to a fresh *sql.DB after reconnect")
	}
}

func TestDatabaseBunAdapterReconnectFactory(t *testing.T) {
	conn := newSQLConnection("test-bun", DatabaseTypeSQLite, ConnectionConfig{
		Name:           "test-bun",
		Type:           DatabaseTypeSQLite,
		FilePath:       ":memory:",
		DefaultORM:     string(ORMTypeBun),
		ConnectTimeout: 2 * time.Second,
	}, providers.NewSQLiteProvider())

	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	db, err := conn.Database()
	if err != nil {
		t.Fatalf("Failed to get database adapter: %v", err)
	}

	adapter, ok := db.(*database.BunAdapter)
	if !ok {
		t.Fatalf("Expected BunAdapter, got %T", db)
	}

	underlyingBefore, ok := adapter.GetUnderlyingDB().(interface{ Close() error })
	if !ok {
		t.Fatalf("Expected underlying Bun DB with Close method, got %T", adapter.GetUnderlyingDB())
	}

	if err := underlyingBefore.Close(); err != nil {
		t.Fatalf("Failed to close underlying Bun database: %v", err)
	}

	if _, err := db.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("Expected Bun adapter to reconnect, got error: %v", err)
	}

	underlyingAfter := adapter.GetUnderlyingDB()
	if underlyingAfter == underlyingBefore {
		t.Fatal("Expected adapter to swap to a fresh Bun DB after reconnect")
	}
}

func TestDatabaseGormAdapterReconnectFactory(t *testing.T) {
	conn := newSQLConnection("test-gorm", DatabaseTypeSQLite, ConnectionConfig{
		Name:           "test-gorm",
		Type:           DatabaseTypeSQLite,
		FilePath:       ":memory:",
		DefaultORM:     string(ORMTypeGORM),
		ConnectTimeout: 2 * time.Second,
	}, providers.NewSQLiteProvider())

	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	db, err := conn.Database()
	if err != nil {
		t.Fatalf("Failed to get database adapter: %v", err)
	}

	adapter, ok := db.(*database.GormAdapter)
	if !ok {
		t.Fatalf("Expected GormAdapter, got %T", db)
	}

	gormBefore, ok := adapter.GetUnderlyingDB().(*gorm.DB)
	if !ok {
		t.Fatalf("Expected underlying *gorm.DB, got %T", adapter.GetUnderlyingDB())
	}

	sqlBefore, err := gormBefore.DB()
	if err != nil {
		t.Fatalf("Failed to get underlying *sql.DB: %v", err)
	}

	if err := sqlBefore.Close(); err != nil {
		t.Fatalf("Failed to close underlying database: %v", err)
	}

	count, err := db.NewSelect().Table("sqlite_master").Count(ctx)
	if err != nil {
		t.Fatalf("Expected GORM query builder to reconnect, got error: %v", err)
	}
	if count < 0 {
		t.Fatalf("Expected non-negative count, got %d", count)
	}

	gormAfter, ok := adapter.GetUnderlyingDB().(*gorm.DB)
	if !ok {
		t.Fatalf("Expected reconnected *gorm.DB, got %T", adapter.GetUnderlyingDB())
	}

	sqlAfter, err := gormAfter.DB()
	if err != nil {
		t.Fatalf("Failed to get reconnected *sql.DB: %v", err)
	}

	if sqlAfter == sqlBefore {
		t.Fatal("Expected GORM adapter to use a fresh *sql.DB after reconnect")
	}
}
