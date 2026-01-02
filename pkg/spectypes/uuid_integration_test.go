package spectypes

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// TestUUIDWithRealDatabase tests that SqlUUID works with actual database operations
func TestUUIDWithRealDatabase(t *testing.T) {
	// Open an in-memory SQLite database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create a test table with UUID column
	_, err = db.Exec(`
		CREATE TABLE test_users (
			id INTEGER PRIMARY KEY,
			user_id TEXT,
			name TEXT
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Test 1: Insert with UUID
	testUUID1 := uuid.New()
	sqlUUID1 := NewSqlUUID(testUUID1)

	_, err = db.Exec("INSERT INTO test_users (id, user_id, name) VALUES (?, ?, ?)",
		1, sqlUUID1, "Alice")
	if err != nil {
		t.Fatalf("Failed to insert record: %v", err)
	}

	// Test 2: Update with UUID
	testUUID2 := uuid.New()
	sqlUUID2 := NewSqlUUID(testUUID2)

	_, err = db.Exec("UPDATE test_users SET user_id = ? WHERE id = ?",
		sqlUUID2, 1)
	if err != nil {
		t.Fatalf("Failed to update record: %v", err)
	}

	// Test 3: Read back and verify
	var retrievedID string
	var name string
	err = db.QueryRow("SELECT user_id, name FROM test_users WHERE id = ?", 1).Scan(&retrievedID, &name)
	if err != nil {
		t.Fatalf("Failed to query record: %v", err)
	}

	if retrievedID != testUUID2.String() {
		t.Errorf("Expected UUID %s, got %s", testUUID2.String(), retrievedID)
	}

	if name != "Alice" {
		t.Errorf("Expected name 'Alice', got '%s'", name)
	}

	// Test 4: Insert with NULL UUID
	nullUUID := SqlUUID{Valid: false}
	_, err = db.Exec("INSERT INTO test_users (id, user_id, name) VALUES (?, ?, ?)",
		2, nullUUID, "Bob")
	if err != nil {
		t.Fatalf("Failed to insert record with NULL UUID: %v", err)
	}

	// Test 5: Read NULL UUID back
	var retrievedNullID sql.NullString
	err = db.QueryRow("SELECT user_id FROM test_users WHERE id = ?", 2).Scan(&retrievedNullID)
	if err != nil {
		t.Fatalf("Failed to query NULL UUID record: %v", err)
	}

	if retrievedNullID.Valid {
		t.Errorf("Expected NULL UUID, got %s", retrievedNullID.String)
	}

	t.Logf("All database operations with UUID succeeded!")
}

// TestUUIDValueReturnsString verifies that Value() returns string, not uuid.UUID
func TestUUIDValueReturnsString(t *testing.T) {
	testUUID := uuid.New()
	sqlUUID := NewSqlUUID(testUUID)

	val, err := sqlUUID.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	// The value should be a string, not a uuid.UUID
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("Expected Value() to return string, got %T", val)
	}

	if strVal != testUUID.String() {
		t.Errorf("Expected %s, got %s", testUUID.String(), strVal)
	}

	t.Logf("✓ Value() correctly returns string: %s", strVal)
}

// CustomStringableType is a custom type that implements fmt.Stringer
type CustomStringableType string

func (c CustomStringableType) String() string {
	return "custom:" + string(c)
}

// TestCustomStringableType verifies that any type implementing fmt.Stringer works
func TestCustomStringableType(t *testing.T) {
	customVal := CustomStringableType("test-value")
	sqlCustom := SqlNull[CustomStringableType]{
		Val:   customVal,
		Valid: true,
	}

	val, err := sqlCustom.Value()
	if err != nil {
		t.Fatalf("Value() failed: %v", err)
	}

	// Should return the result of String() method
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("Expected Value() to return string, got %T", val)
	}

	expected := "custom:test-value"
	if strVal != expected {
		t.Errorf("Expected %s, got %s", expected, strVal)
	}

	t.Logf("✓ Custom Stringer type correctly converted to string: %s", strVal)
}

// TestStringMethodUsesStringer verifies that String() method also uses fmt.Stringer
func TestStringMethodUsesStringer(t *testing.T) {
	// Test with UUID
	testUUID := uuid.New()
	sqlUUID := NewSqlUUID(testUUID)

	strResult := sqlUUID.String()
	if strResult != testUUID.String() {
		t.Errorf("Expected UUID String() to return %s, got %s", testUUID.String(), strResult)
	}
	t.Logf("✓ UUID String() method: %s", strResult)

	// Test with custom Stringer type
	customVal := CustomStringableType("test-value")
	sqlCustom := SqlNull[CustomStringableType]{
		Val:   customVal,
		Valid: true,
	}

	customStr := sqlCustom.String()
	expected := "custom:test-value"
	if customStr != expected {
		t.Errorf("Expected custom String() to return %s, got %s", expected, customStr)
	}
	t.Logf("✓ Custom Stringer String() method: %s", customStr)

	// Test with regular type (should use fmt.Sprintf)
	sqlInt := NewSqlInt64(42)
	intStr := sqlInt.String()
	if intStr != "42" {
		t.Errorf("Expected int String() to return '42', got '%s'", intStr)
	}
	t.Logf("✓ Regular type String() method: %s", intStr)
}
