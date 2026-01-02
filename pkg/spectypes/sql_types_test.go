package spectypes

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNewSqlInt16 tests NewSqlInt16 type
func TestNewSqlInt16(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected SqlInt16
	}{
		{"int", 42, Null(int16(42), true)},
		{"int32", int32(100), NewSqlInt16(100)},
		{"int64", int64(200), NewSqlInt16(200)},
		{"string", "123", NewSqlInt16(123)},
		{"nil", nil, Null(int16(0), false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n SqlInt16
			if err := n.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if n != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, n)
			}
		})
	}
}

func TestNewSqlInt16_Value(t *testing.T) {
	tests := []struct {
		name     string
		input    SqlInt16
		expected driver.Value
	}{
		{"zero", Null(int16(0), false), nil},
		{"positive", NewSqlInt16(42), int16(42)},
		{"negative", NewSqlInt16(-10), int16(-10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if err != nil {
				t.Fatalf("Value failed: %v", err)
			}
			if val != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, val)
			}
		})
	}
}

func TestNewSqlInt16_JSON(t *testing.T) {
	n := NewSqlInt16(42)

	// Marshal
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expected := "42"
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}

	// Unmarshal
	var n2 SqlInt16
	if err := json.Unmarshal([]byte("123"), &n2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if n2.Int64() != 123 {
		t.Errorf("expected 123, got %d", n2.Int64())
	}
}

// TestNewSqlInt64 tests NewSqlInt64 type
func TestNewSqlInt64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected SqlInt64
	}{
		{"int", 42, NewSqlInt64(42)},
		{"int32", int32(100), NewSqlInt64(100)},
		{"int64", int64(9223372036854775807), NewSqlInt64(9223372036854775807)},
		{"uint32", uint32(100), NewSqlInt64(100)},
		{"uint64", uint64(200), NewSqlInt64(200)},
		{"nil", nil, SqlInt64{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n SqlInt64
			if err := n.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if n != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, n)
			}
		})
	}
}

// TestSqlFloat64 tests SqlFloat64 type
func TestSqlFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected float64
		valid    bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", 42, 42.0, true},
		{"int64", int64(100), 100.0, true},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n SqlFloat64
			if err := n.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if n.Valid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, n.Valid)
			}
			if tt.valid && n.Float64() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, n.Float64())
			}
		})
	}
}

// TestSqlTimeStamp tests SqlTimeStamp type
func TestSqlTimeStamp(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		input interface{}
	}{
		{"time.Time", now},
		{"string RFC3339", now.Format(time.RFC3339)},
		{"string date", "2024-01-15"},
		{"string datetime", "2024-01-15T10:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ts SqlTimeStamp
			if err := ts.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if ts.Time().IsZero() {
				t.Error("expected non-zero time")
			}
		})
	}
}

func TestSqlTimeStamp_JSON(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	ts := NewSqlTimeStamp(now)

	// Marshal
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expected := `"2024-01-15T10:30:45"`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}

	// Unmarshal
	var ts2 SqlTimeStamp
	if err := json.Unmarshal([]byte(`"2024-01-15T10:30:45"`), &ts2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if ts2.Time().Year() != 2024 {
		t.Errorf("expected year 2024, got %d", ts2.Time().Year())
	}

	// Test null
	var ts3 SqlTimeStamp
	if err := json.Unmarshal([]byte("null"), &ts3); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
}

// TestSqlDate tests SqlDate type
func TestSqlDate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		input interface{}
	}{
		{"time.Time", now},
		{"string date", "2024-01-15"},
		{"string UK format", "15/01/2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d SqlDate
			if err := d.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if d.String() == "0" {
				t.Error("expected non-zero date")
			}
		})
	}
}

func TestSqlDate_JSON(t *testing.T) {
	date := NewSqlDate(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))

	// Marshal
	data, err := json.Marshal(date)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expected := `"2024-01-15"`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}

	// Unmarshal
	var d2 SqlDate
	if err := json.Unmarshal([]byte(`"2024-01-15"`), &d2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
}

// TestSqlTime tests SqlTime type
func TestSqlTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"time.Time", now, now.Format("15:04:05")},
		{"string time", "10:30:45", "10:30:45"},
		{"string short time", "10:30", "10:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tm SqlTime
			if err := tm.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if tm.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tm.String())
			}
		})
	}
}

// TestSqlJSONB tests SqlJSONB type
func TestSqlJSONB_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"string JSON object", `{"key":"value"}`, `{"key":"value"}`},
		{"string JSON array", `[1,2,3]`, `[1,2,3]`},
		{"bytes", []byte(`{"test":true}`), `{"test":true}`},
		{"nil", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j SqlJSONB
			if err := j.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if tt.expected == "" && j == nil {
				return // nil case
			}
			if string(j) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(j))
			}
		})
	}
}

func TestSqlJSONB_Value(t *testing.T) {
	tests := []struct {
		name     string
		input    SqlJSONB
		expected string
		wantErr  bool
	}{
		{"valid object", SqlJSONB(`{"key":"value"}`), `{"key":"value"}`, false},
		{"valid array", SqlJSONB(`[1,2,3]`), `[1,2,3]`, false},
		{"empty", SqlJSONB{}, "", false},
		{"nil", nil, "", false},
		{"invalid JSON", SqlJSONB(`{invalid`), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Value failed: %v", err)
			}
			if tt.expected == "" && val == nil {
				return // nil case
			}
			if val.(string) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, val)
			}
		})
	}
}

func TestSqlJSONB_JSON(t *testing.T) {
	// Marshal
	j := SqlJSONB(`{"name":"test","count":42}`)
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("expected name=test, got %v", result["name"])
	}

	// Unmarshal
	var j2 SqlJSONB
	if err := json.Unmarshal([]byte(`{"key":"value"}`), &j2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if string(j2) != `{"key":"value"}` {
		t.Errorf("expected {\"key\":\"value\"}, got %s", string(j2))
	}

	// Test null
	var j3 SqlJSONB
	if err := json.Unmarshal([]byte("null"), &j3); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
}

func TestSqlJSONB_AsMap(t *testing.T) {
	tests := []struct {
		name    string
		input   SqlJSONB
		wantErr bool
		wantNil bool
	}{
		{"valid object", SqlJSONB(`{"name":"test","age":30}`), false, false},
		{"empty", SqlJSONB{}, false, true},
		{"nil", nil, false, true},
		{"invalid JSON", SqlJSONB(`{invalid`), true, false},
		{"array not object", SqlJSONB(`[1,2,3]`), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := tt.input.AsMap()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("AsMap failed: %v", err)
			}
			if tt.wantNil {
				if m != nil {
					t.Errorf("expected nil, got %v", m)
				}
				return
			}
			if m == nil {
				t.Error("expected non-nil map")
			}
		})
	}
}

func TestSqlJSONB_AsSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   SqlJSONB
		wantErr bool
		wantNil bool
	}{
		{"valid array", SqlJSONB(`[1,2,3]`), false, false},
		{"empty", SqlJSONB{}, false, true},
		{"nil", nil, false, true},
		{"invalid JSON", SqlJSONB(`[invalid`), true, false},
		{"object not array", SqlJSONB(`{"key":"value"}`), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := tt.input.AsSlice()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("AsSlice failed: %v", err)
			}
			if tt.wantNil {
				if s != nil {
					t.Errorf("expected nil, got %v", s)
				}
				return
			}
			if s == nil {
				t.Error("expected non-nil slice")
			}
		})
	}
}

// TestSqlUUID tests SqlUUID type
func TestSqlUUID_Scan(t *testing.T) {
	testUUID := uuid.New()
	testUUIDStr := testUUID.String()

	tests := []struct {
		name     string
		input    interface{}
		expected string
		valid    bool
	}{
		{"string UUID", testUUIDStr, testUUIDStr, true},
		{"bytes UUID", []byte(testUUIDStr), testUUIDStr, true},
		{"nil", nil, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u SqlUUID
			if err := u.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if u.Valid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, u.Valid)
			}
			if tt.valid && u.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, u.String())
			}
		})
	}
}

func TestSqlUUID_Value(t *testing.T) {
	testUUID := uuid.New()
	u := NewSqlUUID(testUUID)

	val, err := u.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	// Value() should return a string for driver compatibility
	if val != testUUID.String() {
		t.Errorf("expected %s, got %s", testUUID.String(), val)
	}

	// Test invalid UUID
	u2 := SqlUUID{Valid: false}
	val2, err := u2.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if val2 != nil {
		t.Errorf("expected nil, got %v", val2)
	}
}

func TestSqlUUID_JSON(t *testing.T) {
	testUUID := uuid.New()
	u := NewSqlUUID(testUUID)

	// Marshal
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expected := `"` + testUUID.String() + `"`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}

	// Unmarshal
	var u2 SqlUUID
	if err := json.Unmarshal([]byte(`"`+testUUID.String()+`"`), &u2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if u2.String() != testUUID.String() {
		t.Errorf("expected %s, got %s", testUUID.String(), u2.String())
	}

	// Test null
	var u3 SqlUUID
	if err := json.Unmarshal([]byte("null"), &u3); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
	if u3.Valid {
		t.Error("expected invalid UUID")
	}
}

// TestTryIfInt64 tests the TryIfInt64 helper function
func TestTryIfInt64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		def      int64
		expected int64
	}{
		{"string valid", "123", 0, 123},
		{"string invalid", "abc", 99, 99},
		{"int", 42, 0, 42},
		{"int32", int32(100), 0, 100},
		{"int64", int64(200), 0, 200},
		{"uint32", uint32(50), 0, 50},
		{"uint64", uint64(75), 0, 75},
		{"float32", float32(3.14), 0, 3},
		{"float64", float64(2.71), 0, 2},
		{"bytes", []byte("456"), 0, 456},
		{"unknown type", struct{}{}, 999, 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TryIfInt64(tt.input, tt.def)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}
