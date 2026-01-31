package reflection

import (
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
	"github.com/google/uuid"
)

// TestModel contains all spectypes custom types
type TestModel struct {
	ID        int64                    `bun:"id,pk" json:"id"`
	Name      spectypes.SqlString      `bun:"name" json:"name"`
	Age       spectypes.SqlInt64       `bun:"age" json:"age"`
	Score     spectypes.SqlFloat64     `bun:"score" json:"score"`
	Active    spectypes.SqlBool        `bun:"active" json:"active"`
	UUID      spectypes.SqlUUID        `bun:"uuid" json:"uuid"`
	CreatedAt spectypes.SqlTimeStamp   `bun:"created_at" json:"created_at"`
	BirthDate spectypes.SqlDate        `bun:"birth_date" json:"birth_date"`
	StartTime spectypes.SqlTime        `bun:"start_time" json:"start_time"`
	Metadata  spectypes.SqlJSONB       `bun:"metadata" json:"metadata"`
	Count16   spectypes.SqlInt16       `bun:"count16" json:"count16"`
	Count32   spectypes.SqlInt32       `bun:"count32" json:"count32"`
}

// TestMapToStruct_AllSpectypes verifies that MapToStruct can convert all spectypes correctly
func TestMapToStruct_AllSpectypes(t *testing.T) {
	testUUID := uuid.New()
	testTime := time.Now()

	tests := []struct {
		name      string
		dataMap   map[string]interface{}
		validator func(*testing.T, *TestModel)
	}{
		{
			name: "SqlString from string",
			dataMap: map[string]interface{}{
				"name": "John Doe",
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Name.Valid || m.Name.String() != "John Doe" {
					t.Errorf("expected name='John Doe', got valid=%v, value=%s", m.Name.Valid, m.Name.String())
				}
			},
		},
		{
			name: "SqlInt64 from int64",
			dataMap: map[string]interface{}{
				"age": int64(42),
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Age.Valid || m.Age.Int64() != 42 {
					t.Errorf("expected age=42, got valid=%v, value=%d", m.Age.Valid, m.Age.Int64())
				}
			},
		},
		{
			name: "SqlInt64 from string",
			dataMap: map[string]interface{}{
				"age": "99",
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Age.Valid || m.Age.Int64() != 99 {
					t.Errorf("expected age=99, got valid=%v, value=%d", m.Age.Valid, m.Age.Int64())
				}
			},
		},
		{
			name: "SqlFloat64 from float64",
			dataMap: map[string]interface{}{
				"score": float64(98.5),
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Score.Valid || m.Score.Float64() != 98.5 {
					t.Errorf("expected score=98.5, got valid=%v, value=%f", m.Score.Valid, m.Score.Float64())
				}
			},
		},
		{
			name: "SqlBool from bool",
			dataMap: map[string]interface{}{
				"active": true,
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Active.Valid || !m.Active.Bool() {
					t.Errorf("expected active=true, got valid=%v, value=%v", m.Active.Valid, m.Active.Bool())
				}
			},
		},
		{
			name: "SqlUUID from string",
			dataMap: map[string]interface{}{
				"uuid": testUUID.String(),
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.UUID.Valid || m.UUID.UUID() != testUUID {
					t.Errorf("expected uuid=%s, got valid=%v, value=%s", testUUID.String(), m.UUID.Valid, m.UUID.UUID().String())
				}
			},
		},
		{
			name: "SqlTimeStamp from time.Time",
			dataMap: map[string]interface{}{
				"created_at": testTime,
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.CreatedAt.Valid {
					t.Errorf("expected created_at to be valid")
				}
				// Check if times are close enough (within a second)
				diff := m.CreatedAt.Time().Sub(testTime)
				if diff < -time.Second || diff > time.Second {
					t.Errorf("time difference too large: %v", diff)
				}
			},
		},
		{
			name: "SqlTimeStamp from string",
			dataMap: map[string]interface{}{
				"created_at": "2024-01-15T10:30:00",
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.CreatedAt.Valid {
					t.Errorf("expected created_at to be valid")
				}
				expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
				if m.CreatedAt.Time().Year() != expected.Year() ||
					m.CreatedAt.Time().Month() != expected.Month() ||
					m.CreatedAt.Time().Day() != expected.Day() {
					t.Errorf("expected date 2024-01-15, got %v", m.CreatedAt.Time())
				}
			},
		},
		{
			name: "SqlDate from string",
			dataMap: map[string]interface{}{
				"birth_date": "2000-05-20",
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.BirthDate.Valid {
					t.Errorf("expected birth_date to be valid")
				}
				expected := "2000-05-20"
				if m.BirthDate.String() != expected {
					t.Errorf("expected date=%s, got %s", expected, m.BirthDate.String())
				}
			},
		},
		{
			name: "SqlTime from string",
			dataMap: map[string]interface{}{
				"start_time": "14:30:00",
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.StartTime.Valid {
					t.Errorf("expected start_time to be valid")
				}
				if m.StartTime.String() != "14:30:00" {
					t.Errorf("expected time=14:30:00, got %s", m.StartTime.String())
				}
			},
		},
		{
			name: "SqlJSONB from map",
			dataMap: map[string]interface{}{
				"metadata": map[string]interface{}{
					"key1": "value1",
					"key2": 123,
				},
			},
			validator: func(t *testing.T, m *TestModel) {
				if len(m.Metadata) == 0 {
					t.Errorf("expected metadata to have data")
				}
				asMap, err := m.Metadata.AsMap()
				if err != nil {
					t.Fatalf("failed to convert metadata to map: %v", err)
				}
				if asMap["key1"] != "value1" {
					t.Errorf("expected key1=value1, got %v", asMap["key1"])
				}
			},
		},
		{
			name: "SqlJSONB from string",
			dataMap: map[string]interface{}{
				"metadata": `{"test":"data"}`,
			},
			validator: func(t *testing.T, m *TestModel) {
				if len(m.Metadata) == 0 {
					t.Errorf("expected metadata to have data")
				}
				asMap, err := m.Metadata.AsMap()
				if err != nil {
					t.Fatalf("failed to convert metadata to map: %v", err)
				}
				if asMap["test"] != "data" {
					t.Errorf("expected test=data, got %v", asMap["test"])
				}
			},
		},
		{
			name: "SqlJSONB from []byte",
			dataMap: map[string]interface{}{
				"metadata": []byte(`{"byte":"array"}`),
			},
			validator: func(t *testing.T, m *TestModel) {
				if len(m.Metadata) == 0 {
					t.Errorf("expected metadata to have data")
				}
				if string(m.Metadata) != `{"byte":"array"}` {
					t.Errorf("expected {\"byte\":\"array\"}, got %s", string(m.Metadata))
				}
			},
		},
		{
			name: "SqlInt16 from int16",
			dataMap: map[string]interface{}{
				"count16": int16(100),
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Count16.Valid || m.Count16.Int64() != 100 {
					t.Errorf("expected count16=100, got valid=%v, value=%d", m.Count16.Valid, m.Count16.Int64())
				}
			},
		},
		{
			name: "SqlInt32 from int32",
			dataMap: map[string]interface{}{
				"count32": int32(5000),
			},
			validator: func(t *testing.T, m *TestModel) {
				if !m.Count32.Valid || m.Count32.Int64() != 5000 {
					t.Errorf("expected count32=5000, got valid=%v, value=%d", m.Count32.Valid, m.Count32.Int64())
				}
			},
		},
		{
			name: "nil values create invalid nulls",
			dataMap: map[string]interface{}{
				"name":       nil,
				"age":        nil,
				"active":     nil,
				"created_at": nil,
			},
			validator: func(t *testing.T, m *TestModel) {
				if m.Name.Valid {
					t.Error("expected name to be invalid for nil value")
				}
				if m.Age.Valid {
					t.Error("expected age to be invalid for nil value")
				}
				if m.Active.Valid {
					t.Error("expected active to be invalid for nil value")
				}
				if m.CreatedAt.Valid {
					t.Error("expected created_at to be invalid for nil value")
				}
			},
		},
		{
			name: "all types together",
			dataMap: map[string]interface{}{
				"id":         int64(1),
				"name":       "Test User",
				"age":        int64(30),
				"score":      float64(95.7),
				"active":     true,
				"uuid":       testUUID.String(),
				"created_at": "2024-01-15T10:30:00",
				"birth_date": "1994-06-15",
				"start_time": "09:00:00",
				"metadata":   map[string]interface{}{"role": "admin"},
				"count16":    int16(50),
				"count32":    int32(1000),
			},
			validator: func(t *testing.T, m *TestModel) {
				if m.ID != 1 {
					t.Errorf("expected id=1, got %d", m.ID)
				}
				if !m.Name.Valid || m.Name.String() != "Test User" {
					t.Errorf("expected name='Test User', got valid=%v, value=%s", m.Name.Valid, m.Name.String())
				}
				if !m.Age.Valid || m.Age.Int64() != 30 {
					t.Errorf("expected age=30, got valid=%v, value=%d", m.Age.Valid, m.Age.Int64())
				}
				if !m.Score.Valid || m.Score.Float64() != 95.7 {
					t.Errorf("expected score=95.7, got valid=%v, value=%f", m.Score.Valid, m.Score.Float64())
				}
				if !m.Active.Valid || !m.Active.Bool() {
					t.Errorf("expected active=true, got valid=%v, value=%v", m.Active.Valid, m.Active.Bool())
				}
				if !m.UUID.Valid {
					t.Error("expected uuid to be valid")
				}
				if !m.CreatedAt.Valid {
					t.Error("expected created_at to be valid")
				}
				if !m.BirthDate.Valid || m.BirthDate.String() != "1994-06-15" {
					t.Errorf("expected birth_date=1994-06-15, got valid=%v, value=%s", m.BirthDate.Valid, m.BirthDate.String())
				}
				if !m.StartTime.Valid || m.StartTime.String() != "09:00:00" {
					t.Errorf("expected start_time=09:00:00, got valid=%v, value=%s", m.StartTime.Valid, m.StartTime.String())
				}
				if len(m.Metadata) == 0 {
					t.Error("expected metadata to have data")
				}
				if !m.Count16.Valid || m.Count16.Int64() != 50 {
					t.Errorf("expected count16=50, got valid=%v, value=%d", m.Count16.Valid, m.Count16.Int64())
				}
				if !m.Count32.Valid || m.Count32.Int64() != 1000 {
					t.Errorf("expected count32=1000, got valid=%v, value=%d", m.Count32.Valid, m.Count32.Int64())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &TestModel{}
			if err := MapToStruct(tt.dataMap, model); err != nil {
				t.Fatalf("MapToStruct failed: %v", err)
			}
			tt.validator(t, model)
		})
	}
}

// TestMapToStruct_PartialUpdate tests that partial updates preserve unset fields
func TestMapToStruct_PartialUpdate(t *testing.T) {
	// Create initial model with some values
	initial := &TestModel{
		ID:   1,
		Name: spectypes.NewSqlString("Original Name"),
		Age:  spectypes.NewSqlInt64(25),
	}

	// Update only the age field
	partialUpdate := map[string]interface{}{
		"age": int64(30),
	}

	// Apply partial update
	if err := MapToStruct(partialUpdate, initial); err != nil {
		t.Fatalf("MapToStruct failed: %v", err)
	}

	// Verify age was updated
	if !initial.Age.Valid || initial.Age.Int64() != 30 {
		t.Errorf("expected age=30, got valid=%v, value=%d", initial.Age.Valid, initial.Age.Int64())
	}

	// Verify name was preserved (not overwritten with zero value)
	if !initial.Name.Valid || initial.Name.String() != "Original Name" {
		t.Errorf("expected name='Original Name' to be preserved, got valid=%v, value=%s", initial.Name.Valid, initial.Name.String())
	}

	// Verify ID was preserved
	if initial.ID != 1 {
		t.Errorf("expected id=1 to be preserved, got %d", initial.ID)
	}
}
