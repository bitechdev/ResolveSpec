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
	expected := `"2024-01-15T10:30:45Z"`
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

// TestSqlString tests SqlString without base64 (plain text)
func TestSqlString_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
		valid    bool
	}{
		{
			name:     "plain string",
			input:    "hello world",
			expected: "hello world",
			valid:    true,
		},
		{
			name:     "plain text",
			input:    "plain text",
			expected: "plain text",
			valid:    true,
		},
		{
			name:     "bytes as string",
			input:    []byte("raw bytes"),
			expected: "raw bytes",
			valid:    true,
		},
		{
			name:     "nil value",
			input:    nil,
			expected: "",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SqlString
			if err := s.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if s.Valid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, s.Valid)
			}
			if tt.valid && s.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s.String())
			}
		})
	}
}

func TestSqlString_JSON(t *testing.T) {
	tests := []struct {
		name           string
		inputValue     string
		expectedJSON   string
		expectedDecode string
	}{
		{
			name:           "simple string",
			inputValue:     "hello world",
			expectedJSON:   `"hello world"`, // plain text, not base64
			expectedDecode: "hello world",
		},
		{
			name:           "special characters",
			inputValue:     "test@#$%",
			expectedJSON:   `"test@#$%"`, // plain text, not base64
			expectedDecode: "test@#$%",
		},
		{
			name:           "unicode string",
			inputValue:     "Hello 世界",
			expectedJSON:   `"Hello 世界"`, // plain text, not base64
			expectedDecode: "Hello 世界",
		},
		{
			name:           "empty string",
			inputValue:     "",
			expectedJSON:   `""`,
			expectedDecode: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test MarshalJSON
			s := NewSqlString(tt.inputValue)
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if string(data) != tt.expectedJSON {
				t.Errorf("Marshal: expected %s, got %s", tt.expectedJSON, string(data))
			}

			// Test UnmarshalJSON
			var s2 SqlString
			if err := json.Unmarshal(data, &s2); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if !s2.Valid {
				t.Error("expected valid=true after unmarshal")
			}
			if s2.String() != tt.expectedDecode {
				t.Errorf("Unmarshal: expected %q, got %q", tt.expectedDecode, s2.String())
			}
		})
	}
}

func TestSqlString_JSON_Null(t *testing.T) {
	// Test null handling
	var s SqlString
	if err := json.Unmarshal([]byte("null"), &s); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
	if s.Valid {
		t.Error("expected invalid after unmarshaling null")
	}

	// Test marshal null
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected null, got %s", string(data))
	}
}

// TestSqlByteArray_Base64 tests SqlByteArray with base64 encoding/decoding
func TestSqlByteArray_Base64_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []byte
		valid    bool
	}{
		{
			name:     "base64 encoded bytes from SQL",
			input:    "aGVsbG8gd29ybGQ=", // "hello world" in base64
			expected: []byte("hello world"),
			valid:    true,
		},
		{
			name:     "plain bytes fallback",
			input:    "plain text",
			expected: []byte("plain text"),
			valid:    true,
		},
		{
			name:     "bytes base64 encoded",
			input:    []byte("SGVsbG8gR29waGVy"), // "Hello Gopher" in base64
			expected: []byte("Hello Gopher"),
			valid:    true,
		},
		{
			name:     "bytes plain fallback",
			input:    []byte("raw bytes"),
			expected: []byte("raw bytes"),
			valid:    true,
		},
		{
			name:     "binary data",
			input:    "AQIDBA==", // []byte{1, 2, 3, 4} in base64
			expected: []byte{1, 2, 3, 4},
			valid:    true,
		},
		{
			name:     "nil value",
			input:    nil,
			expected: nil,
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b SqlByteArray
			if err := b.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if b.Valid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, b.Valid)
			}
			if tt.valid {
				if string(b.Val) != string(tt.expected) {
					t.Errorf("expected %q, got %q", tt.expected, b.Val)
				}
			}
		})
	}
}

func TestSqlByteArray_Base64_JSON(t *testing.T) {
	tests := []struct {
		name           string
		inputValue     []byte
		expectedJSON   string
		expectedDecode []byte
	}{
		{
			name:           "text bytes",
			inputValue:     []byte("hello world"),
			expectedJSON:   `"aGVsbG8gd29ybGQ="`, // base64 encoded
			expectedDecode: []byte("hello world"),
		},
		{
			name:           "binary data",
			inputValue:     []byte{0x01, 0x02, 0x03, 0x04, 0xFF},
			expectedJSON:   `"AQIDBP8="`, // base64 encoded
			expectedDecode: []byte{0x01, 0x02, 0x03, 0x04, 0xFF},
		},
		{
			name:           "empty bytes",
			inputValue:     []byte{},
			expectedJSON:   `""`, // base64 of empty bytes
			expectedDecode: []byte{},
		},
		{
			name:           "unicode bytes",
			inputValue:     []byte("Hello 世界"),
			expectedJSON:   `"SGVsbG8g5LiW55WM"`, // base64 encoded
			expectedDecode: []byte("Hello 世界"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test MarshalJSON
			b := NewSqlByteArray(tt.inputValue)
			data, err := json.Marshal(b)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if string(data) != tt.expectedJSON {
				t.Errorf("Marshal: expected %s, got %s", tt.expectedJSON, string(data))
			}

			// Test UnmarshalJSON
			var b2 SqlByteArray
			if err := json.Unmarshal(data, &b2); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if !b2.Valid {
				t.Error("expected valid=true after unmarshal")
			}
			if string(b2.Val) != string(tt.expectedDecode) {
				t.Errorf("Unmarshal: expected %v, got %v", tt.expectedDecode, b2.Val)
			}
		})
	}
}

func TestSqlByteArray_Base64_JSON_Null(t *testing.T) {
	// Test null handling
	var b SqlByteArray
	if err := json.Unmarshal([]byte("null"), &b); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
	if b.Valid {
		t.Error("expected invalid after unmarshaling null")
	}

	// Test marshal null
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected null, got %s", string(data))
	}
}

func TestSqlByteArray_Value(t *testing.T) {
	tests := []struct {
		name     string
		input    SqlByteArray
		expected interface{}
	}{
		{
			name:     "valid bytes",
			input:    NewSqlByteArray([]byte("test data")),
			expected: []byte("test data"),
		},
		{
			name:     "empty bytes",
			input:    NewSqlByteArray([]byte{}),
			expected: []byte{},
		},
		{
			name:     "invalid",
			input:    SqlByteArray{Valid: false},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if err != nil {
				t.Fatalf("Value failed: %v", err)
			}
			if tt.expected == nil && val != nil {
				t.Errorf("expected nil, got %v", val)
			}
			if tt.expected != nil && val == nil {
				t.Errorf("expected %v, got nil", tt.expected)
			}
			if tt.expected != nil && val != nil {
				if string(val.([]byte)) != string(tt.expected.([]byte)) {
					t.Errorf("expected %v, got %v", tt.expected, val)
				}
			}
		})
	}
}

// TestSqlString_RoundTrip tests complete round-trip: Go -> JSON -> Go -> SQL -> Go
func TestSqlString_RoundTrip(t *testing.T) {
	original := "Test String with Special Chars: @#$%^&*()"

	// Go -> JSON
	s1 := NewSqlString(original)
	jsonData, err := json.Marshal(s1)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// JSON -> Go
	var s2 SqlString
	if err := json.Unmarshal(jsonData, &s2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Go -> SQL (Value)
	_, err = s2.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}

	// SQL -> Go (Scan plain text)
	var s3 SqlString
	// Simulate SQL driver returning plain text value
	if err := s3.Scan(original); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify round-trip
	if s3.String() != original {
		t.Errorf("Round-trip failed: expected %q, got %q", original, s3.String())
	}
}

// TestSqlBool_Scan tests SqlBool Scan from various input types.
func TestSqlBool_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
		valid    bool
	}{
		{"bool true", true, true, true},
		{"bool false", false, false, true},
		{"string true", "true", true, true},
		{"string 1", "1", true, true},
		{"int64 1 fallback", int64(1), true, true},
		{"int64 0 fallback", int64(0), false, true},
		{"nil", nil, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b SqlBool
			if err := b.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if b.Valid != tt.valid {
				t.Errorf("expected valid=%v, got valid=%v", tt.valid, b.Valid)
			}
			if tt.valid && b.Val != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, b.Val)
			}
		})
	}
}

func TestSqlBool_Value(t *testing.T) {
	b := NewSqlBool(true)
	val, err := b.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if val != true {
		t.Errorf("expected true, got %v", val)
	}

	b2 := SqlBool{Valid: false}
	val2, err := b2.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if val2 != nil {
		t.Errorf("expected nil, got %v", val2)
	}
}

func TestSqlBool_JSON(t *testing.T) {
	b := NewSqlBool(true)
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != "true" {
		t.Errorf("expected true, got %s", string(data))
	}

	var b2 SqlBool
	if err := json.Unmarshal([]byte("false"), &b2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !b2.Valid || b2.Val != false {
		t.Errorf("expected valid=true val=false, got valid=%v val=%v", b2.Valid, b2.Val)
	}

	var b3 SqlBool
	if err := json.Unmarshal([]byte("null"), &b3); err != nil {
		t.Fatalf("Unmarshal null failed: %v", err)
	}
	if b3.Valid {
		t.Error("expected invalid after unmarshaling null")
	}
}

// TestSqlNull_FromString_EdgeCases tests FromString edge cases shared by all SqlNull instantiations.
func TestSqlNull_FromString_EdgeCases(t *testing.T) {
	t.Run("empty string is null", func(t *testing.T) {
		var n SqlInt64
		if err := n.FromString(""); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if n.Valid {
			t.Error("expected invalid for empty string")
		}
	})

	t.Run("NULL case-insensitive", func(t *testing.T) {
		var n SqlString
		if err := n.FromString("NuLL"); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if n.Valid {
			t.Error("expected invalid for 'NuLL'")
		}
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		var n SqlInt64
		if err := n.FromString("  42  "); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if !n.Valid || n.Val != 42 {
			t.Errorf("expected valid=true val=42, got valid=%v val=%v", n.Valid, n.Val)
		}
	})

	t.Run("invalid int string stays invalid", func(t *testing.T) {
		var n SqlInt64
		if err := n.FromString("not-a-number"); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if n.Valid {
			t.Error("expected invalid for non-numeric string")
		}
	})

	t.Run("float string truncated into int type", func(t *testing.T) {
		var n SqlInt64
		if err := n.FromString("3.7"); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if !n.Valid || n.Val != 3 {
			t.Errorf("expected valid=true val=3, got valid=%v val=%v", n.Valid, n.Val)
		}
	})

	t.Run("invalid bool string stays invalid", func(t *testing.T) {
		var n SqlBool
		if err := n.FromString("maybe"); err != nil {
			t.Fatalf("FromString failed: %v", err)
		}
		if n.Valid {
			t.Error("expected invalid for non-bool string")
		}
	})
}

// TestSqlNull_String tests the String() stringer fallback.
func TestSqlNull_String(t *testing.T) {
	t.Run("invalid returns empty", func(t *testing.T) {
		n := SqlInt64{Valid: false}
		if n.String() != "" {
			t.Errorf("expected empty string, got %q", n.String())
		}
	})

	t.Run("stringer type delegates", func(t *testing.T) {
		u := uuid.New()
		n := NewSqlUUID(u)
		if n.String() != u.String() {
			t.Errorf("expected %s, got %s", u.String(), n.String())
		}
	})

	t.Run("non-stringer falls back to fmt", func(t *testing.T) {
		n := NewSqlInt64(42)
		if n.String() != "42" {
			t.Errorf("expected 42, got %s", n.String())
		}
	})
}

// TestNewSql_Generic tests the generic NewSql constructor.
func TestNewSql_Generic(t *testing.T) {
	t.Run("exact type match", func(t *testing.T) {
		n := NewSql[int64](int64(5))
		if !n.Valid || n.Val != 5 {
			t.Errorf("expected valid=true val=5, got valid=%v val=%v", n.Valid, n.Val)
		}
	})

	t.Run("nil value", func(t *testing.T) {
		n := NewSql[int64](nil)
		if n.Valid {
			t.Error("expected invalid for nil")
		}
	})

	t.Run("from another SqlNull", func(t *testing.T) {
		src := SqlNull[int64]{Val: 9, Valid: true}
		n := NewSql[int64](src)
		if !n.Valid || n.Val != 9 {
			t.Errorf("expected valid=true val=9, got valid=%v val=%v", n.Valid, n.Val)
		}
	})

	t.Run("string conversion fallback", func(t *testing.T) {
		n := NewSql[string](42)
		if !n.Valid || n.Val != "42" {
			t.Errorf("expected valid=true val=42, got valid=%v val=%q", n.Valid, n.Val)
		}
	})
}

// TestSqlNull_Int64_Conversions tests Int64() across differently-typed SqlNull values.
func TestSqlNull_Int64_Conversions(t *testing.T) {
	if v := (SqlNull[string]{Val: "42", Valid: true}).Int64(); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
	if v := (SqlNull[bool]{Val: true, Valid: true}).Int64(); v != 1 {
		t.Errorf("expected 1, got %d", v)
	}
	if v := (SqlNull[bool]{Val: false, Valid: true}).Int64(); v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
	if v := (SqlNull[float64]{Val: 3.9, Valid: true}).Int64(); v != 3 {
		t.Errorf("expected 3, got %d", v)
	}
	if v := (SqlNull[int64]{Valid: false}).Int64(); v != 0 {
		t.Errorf("expected 0 for invalid, got %d", v)
	}
}

// TestSqlNull_Float64_Conversions tests Float64() across differently-typed SqlNull values.
func TestSqlNull_Float64_Conversions(t *testing.T) {
	if v := (SqlNull[string]{Val: "3.14", Valid: true}).Float64(); v != 3.14 {
		t.Errorf("expected 3.14, got %v", v)
	}
	if v := (SqlNull[int64]{Val: 10, Valid: true}).Float64(); v != 10.0 {
		t.Errorf("expected 10.0, got %v", v)
	}
	if v := (SqlNull[float64]{Valid: false}).Float64(); v != 0.0 {
		t.Errorf("expected 0.0 for invalid, got %v", v)
	}
}

// TestSqlNull_Bool_Conversions tests Bool() across differently-typed SqlNull values.
func TestSqlNull_Bool_Conversions(t *testing.T) {
	if v := (SqlNull[string]{Val: "YES", Valid: true}).Bool(); v != true {
		t.Error("expected true for 'YES'")
	}
	if v := (SqlNull[string]{Val: "no", Valid: true}).Bool(); v != false {
		t.Error("expected false for 'no'")
	}
	if v := (SqlNull[int]{Val: 1, Valid: true}).Bool(); v != true {
		t.Error("expected true for int 1")
	}
	if v := (SqlNull[int]{Val: 0, Valid: true}).Bool(); v != false {
		t.Error("expected false for int 0")
	}
	if v := (SqlNull[bool]{Valid: false}).Bool(); v != false {
		t.Error("expected false for invalid")
	}
}

// TestSqlNull_Time_NonTimeType verifies Time() returns zero value when T is not time.Time.
func TestSqlNull_Time_NonTimeType(t *testing.T) {
	n := SqlNull[string]{Val: "2024-01-15", Valid: true}
	if !n.Time().IsZero() {
		t.Error("expected zero time for non-time.Time SqlNull")
	}
}

// TestSqlNull_UUID_NonUUIDType verifies UUID() returns uuid.Nil when T is not uuid.UUID.
func TestSqlNull_UUID_NonUUIDType(t *testing.T) {
	n := SqlNull[string]{Val: "not-a-uuid", Valid: true}
	if n.UUID() != uuid.Nil {
		t.Error("expected uuid.Nil for non-uuid.UUID SqlNull")
	}
}

// TestSqlTime_Midnight verifies midnight times are serialized as "00:00:00", not null.
func TestSqlTime_Midnight(t *testing.T) {
	midnight := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tm := NewSqlTime(midnight)

	data, err := json.Marshal(tm)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(data) != `"00:00:00"` {
		t.Errorf("expected \"00:00:00\", got %s", string(data))
	}

	val, err := tm.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if val != "00:00:00" {
		t.Errorf("expected 00:00:00, got %v", val)
	}
}

func TestSqlTime_Value_Invalid(t *testing.T) {
	tm := SqlTime{}
	val, err := tm.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil, got %v", val)
	}
}

// TestSqlDate_ZeroValueString verifies String() blanks out sentinel zero dates.
func TestSqlDate_ZeroValueString(t *testing.T) {
	d := SqlDate{SqlNull: SqlNull[time.Time]{Val: time.Time{}, Valid: true}}
	if d.String() != "" {
		t.Errorf("expected empty string for zero date, got %q", d.String())
	}

	sentinel := time.Date(1800, 12, 31, 0, 0, 0, 0, time.UTC)
	d2 := SqlDate{SqlNull: SqlNull[time.Time]{Val: sentinel, Valid: true}}
	if d2.String() != "" {
		t.Errorf("expected empty string for 1800-12-31 sentinel, got %q", d2.String())
	}
}

// TestSqlTimeStamp_Value tests driver.Valuer for SqlTimeStamp, including the pre-year-2 cutoff.
func TestSqlTimeStamp_Value(t *testing.T) {
	t.Run("valid recent timestamp", func(t *testing.T) {
		ts := NewSqlTimeStamp(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC))
		val, err := ts.Value()
		if err != nil {
			t.Fatalf("Value failed: %v", err)
		}
		if val != "2024-01-15T10:30:00Z" {
			t.Errorf("expected 2024-01-15T10:30:00Z, got %v", val)
		}
	})

	t.Run("year 1 is treated as null", func(t *testing.T) {
		ts := NewSqlTimeStamp(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC))
		val, err := ts.Value()
		if err != nil {
			t.Fatalf("Value failed: %v", err)
		}
		if val != nil {
			t.Errorf("expected nil, got %v", val)
		}
	})

	t.Run("invalid is null", func(t *testing.T) {
		ts := SqlTimeStamp{}
		val, err := ts.Value()
		if err != nil {
			t.Fatalf("Value failed: %v", err)
		}
		if val != nil {
			t.Errorf("expected nil, got %v", val)
		}
	})
}

func TestSqlTimeStamp_UnmarshalJSON_YearOneInvalid(t *testing.T) {
	var ts SqlTimeStamp
	if err := json.Unmarshal([]byte(`"0001-01-01T00:00:00Z"`), &ts); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if ts.Valid {
		t.Error("expected invalid for year 0001 timestamp")
	}
}

// TestTryParseDT tests the internal multi-format date/time parser.
func TestTryParseDT(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"RFC3339", "2024-01-15T10:30:00Z"},
		{"date only", "2024-01-15"},
		{"datetime no tz", "2024-01-15T10:30:00"},
		{"space separated", "2024-01-15 10:30:00"},
		{"UK date slash", "15/01/2024"},
		{"UK date dash", "15-01-2024"},
		{"time only", "10:30:00"},
		{"short time", "10:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, err := tryParseDT(tt.input)
			if err != nil {
				t.Fatalf("tryParseDT failed for %q: %v", tt.input, err)
			}
			if tm.IsZero() {
				t.Errorf("expected non-zero time for %q", tt.input)
			}
		})
	}

	t.Run("invalid format", func(t *testing.T) {
		_, err := tryParseDT("not a date at all")
		if err == nil {
			t.Error("expected error for unparseable string")
		}
	})
}

// TestToJSONDT tests RFC3339 formatting helper.
func TestToJSONDT(t *testing.T) {
	dt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	expected := dt.Format(time.RFC3339)
	if got := ToJSONDT(dt); got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

// TestSqlByteArray_Base64_RoundTrip tests complete round-trip: Go -> JSON -> Go -> SQL -> Go
func TestSqlByteArray_Base64_RoundTrip(t *testing.T) {
	original := []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0xFF, 0xFE} // "Hello " + binary data

	// Go -> JSON
	b1 := NewSqlByteArray(original)
	jsonData, err := json.Marshal(b1)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// JSON -> Go
	var b2 SqlByteArray
	if err := json.Unmarshal(jsonData, &b2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Go -> SQL (Value)
	_, err = b2.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}

	// SQL -> Go (Scan with base64)
	var b3 SqlByteArray
	// Simulate SQL driver returning base64 encoded value
	if err := b3.Scan("SGVsbG8g//4="); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify round-trip
	if string(b3.Val) != string(original) {
		t.Errorf("Round-trip failed: expected %v, got %v", original, b3.Val)
	}
}
