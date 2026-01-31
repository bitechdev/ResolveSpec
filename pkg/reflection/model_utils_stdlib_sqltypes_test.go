package reflection_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

func TestMapToStruct_StandardSqlNullTypes(t *testing.T) {
	// Test model with standard library sql.Null* types
	type TestModel struct {
		ID        int64           `bun:"id,pk" json:"id"`
		Age       sql.NullInt64   `bun:"age" json:"age"`
		Name      sql.NullString  `bun:"name" json:"name"`
		Score     sql.NullFloat64 `bun:"score" json:"score"`
		Active    sql.NullBool    `bun:"active" json:"active"`
		UpdatedAt sql.NullTime    `bun:"updated_at" json:"updated_at"`
	}

	now := time.Now()
	dataMap := map[string]any{
		"id":         int64(100),
		"age":        int64(25),
		"name":       "John Doe",
		"score":      95.5,
		"active":     true,
		"updated_at": now,
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	// Verify ID
	if result.ID != 100 {
		t.Errorf("ID = %v, want 100", result.ID)
	}

	// Verify Age (sql.NullInt64)
	if !result.Age.Valid {
		t.Error("Age.Valid = false, want true")
	}
	if result.Age.Int64 != 25 {
		t.Errorf("Age.Int64 = %v, want 25", result.Age.Int64)
	}

	// Verify Name (sql.NullString)
	if !result.Name.Valid {
		t.Error("Name.Valid = false, want true")
	}
	if result.Name.String != "John Doe" {
		t.Errorf("Name.String = %v, want 'John Doe'", result.Name.String)
	}

	// Verify Score (sql.NullFloat64)
	if !result.Score.Valid {
		t.Error("Score.Valid = false, want true")
	}
	if result.Score.Float64 != 95.5 {
		t.Errorf("Score.Float64 = %v, want 95.5", result.Score.Float64)
	}

	// Verify Active (sql.NullBool)
	if !result.Active.Valid {
		t.Error("Active.Valid = false, want true")
	}
	if !result.Active.Bool {
		t.Error("Active.Bool = false, want true")
	}

	// Verify UpdatedAt (sql.NullTime)
	if !result.UpdatedAt.Valid {
		t.Error("UpdatedAt.Valid = false, want true")
	}
	if !result.UpdatedAt.Time.Equal(now) {
		t.Errorf("UpdatedAt.Time = %v, want %v", result.UpdatedAt.Time, now)
	}

	t.Log("All standard library sql.Null* types handled correctly!")
}

func TestMapToStruct_StandardSqlNullTypes_WithNil(t *testing.T) {
	// Test nil handling for standard library sql.Null* types
	type TestModel struct {
		ID   int64          `bun:"id,pk" json:"id"`
		Age  sql.NullInt64  `bun:"age" json:"age"`
		Name sql.NullString `bun:"name" json:"name"`
	}

	dataMap := map[string]any{
		"id":   int64(200),
		"age":  int64(30),
		"name": nil, // Explicitly nil
	}

	var result TestModel
	err := reflection.MapToStruct(dataMap, &result)
	if err != nil {
		t.Fatalf("MapToStruct() error = %v", err)
	}

	// Age should be valid
	if !result.Age.Valid {
		t.Error("Age.Valid = false, want true")
	}
	if result.Age.Int64 != 30 {
		t.Errorf("Age.Int64 = %v, want 30", result.Age.Int64)
	}

	// Name should be invalid (null)
	if result.Name.Valid {
		t.Error("Name.Valid = true, want false (null)")
	}

	t.Log("Nil handling for sql.Null* types works correctly!")
}
