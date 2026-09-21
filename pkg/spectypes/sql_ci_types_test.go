package spectypes

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestCIString_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected CIString
	}{
		{name: "plain string", input: "MixedCase", expected: "MixedCase"},
		{name: "bytes as string", input: []byte("FromBytes"), expected: "FromBytes"},
		{name: "nil value", input: nil, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s CIString
			if err := s.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if s != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestCIString_Scan_InvalidType(t *testing.T) {
	var s CIString
	if err := s.Scan(123); err == nil {
		t.Fatal("expected error scanning int into CIString, got nil")
	}
}

func TestCIString_Value(t *testing.T) {
	s := CIString("MixedCase")
	v, err := s.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if v != "MixedCase" {
		t.Errorf("expected %q, got %q (case must be preserved)", "MixedCase", v)
	}
}

func TestCIString_String(t *testing.T) {
	s := CIString("MixedCase")
	if s.String() != "MixedCase" {
		t.Errorf("expected %q, got %q", "MixedCase", s.String())
	}
}

func TestCIString_Equal(t *testing.T) {
	tests := []struct {
		name     string
		a, b     CIString
		expected bool
	}{
		{name: "same case", a: "ABC", b: "ABC", expected: true},
		{name: "different case", a: "ABC", b: "abc", expected: true},
		{name: "mixed case", a: "AbC", b: "aBc", expected: true},
		{name: "not equal", a: "ABC", b: "XYZ", expected: false},
		{name: "both empty", a: "", b: "", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.expected {
				t.Errorf("Equal(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestCIString_EqualString(t *testing.T) {
	s := CIString("ABC")
	if !s.EqualString("abc") {
		t.Error("expected EqualString to match case-insensitively")
	}
	if s.EqualString("xyz") {
		t.Error("expected EqualString to not match different strings")
	}
}

func TestCIString_Compare(t *testing.T) {
	tests := []struct {
		name     string
		a, b     CIString
		expected int
	}{
		{name: "equal same case", a: "abc", b: "abc", expected: 0},
		{name: "equal different case", a: "ABC", b: "abc", expected: 0},
		{name: "less", a: "abc", b: "xyz", expected: -1},
		{name: "less different case", a: "ABC", b: "xyz", expected: -1},
		{name: "greater", a: "xyz", b: "abc", expected: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.expected {
				t.Errorf("Compare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestCIString_Less(t *testing.T) {
	if !CIString("abc").Less("xyz") {
		t.Error("expected abc < xyz")
	}
	if CIString("xyz").Less("abc") {
		t.Error("expected xyz not < abc")
	}
	if CIString("ABC").Less("abc") {
		t.Error("expected ABC not < abc (equal ignoring case)")
	}
}

func TestCIString_Sort(t *testing.T) {
	vals := []CIString{"banana", "Apple", "cherry", "apple"}
	sort.Slice(vals, func(i, j int) bool { return vals[i].Less(vals[j]) })

	// After a case-insensitive sort, "Apple"/"apple" must be adjacent and first,
	// followed by banana then cherry.
	if !vals[0].EqualString("apple") || !vals[1].EqualString("apple") {
		t.Errorf("expected the two apple variants first, got %v", vals)
	}
	if !vals[2].EqualString("banana") {
		t.Errorf("expected banana third, got %v", vals)
	}
	if !vals[3].EqualString("cherry") {
		t.Errorf("expected cherry fourth, got %v", vals)
	}
}

func TestLCString_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected LCString
	}{
		{name: "mixed case string", input: "MixedCase", expected: "mixedcase"},
		{name: "bytes mixed case", input: []byte("FromBytes"), expected: "frombytes"},
		{name: "nil value", input: nil, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s LCString
			if err := s.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if s != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestLCString_Scan_InvalidType(t *testing.T) {
	var s LCString
	if err := s.Scan(123); err == nil {
		t.Fatal("expected error scanning int into LCString, got nil")
	}
}

func TestLCString_Value(t *testing.T) {
	s := LCString("MixedCase")
	v, err := s.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if v != "mixedcase" {
		t.Errorf("expected %q, got %q", "mixedcase", v)
	}
}

func TestLCString_String(t *testing.T) {
	s := LCString("MixedCase")
	if s.String() != "mixedcase" {
		t.Errorf("expected %q, got %q", "mixedcase", s.String())
	}
}

func TestLCString_Equal(t *testing.T) {
	if !LCString("ABC").Equal(LCString("abc")) {
		t.Error("expected ABC and abc to be equal")
	}
	if LCString("ABC").Equal(LCString("xyz")) {
		t.Error("expected ABC and xyz to not be equal")
	}
}

func TestLCString_EqualString(t *testing.T) {
	if !LCString("ABC").EqualString("abc") {
		t.Error("expected EqualString to match case-insensitively")
	}
}

func TestUCString_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected UCString
	}{
		{name: "mixed case string", input: "MixedCase", expected: "MIXEDCASE"},
		{name: "bytes mixed case", input: []byte("FromBytes"), expected: "FROMBYTES"},
		{name: "nil value", input: nil, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s UCString
			if err := s.Scan(tt.input); err != nil {
				t.Fatalf("Scan failed: %v", err)
			}
			if s != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, s)
			}
		})
	}
}

func TestUCString_Scan_InvalidType(t *testing.T) {
	var s UCString
	if err := s.Scan(123); err == nil {
		t.Fatal("expected error scanning int into UCString, got nil")
	}
}

func TestUCString_Value(t *testing.T) {
	s := UCString("MixedCase")
	v, err := s.Value()
	if err != nil {
		t.Fatalf("Value failed: %v", err)
	}
	if v != "MIXEDCASE" {
		t.Errorf("expected %q, got %q", "MIXEDCASE", v)
	}
}

func TestUCString_String(t *testing.T) {
	s := UCString("MixedCase")
	if s.String() != "MIXEDCASE" {
		t.Errorf("expected %q, got %q", "MIXEDCASE", s.String())
	}
}

func TestUCString_Equal(t *testing.T) {
	if !UCString("ABC").Equal(UCString("abc")) {
		t.Error("expected ABC and abc to be equal")
	}
	if UCString("ABC").Equal(UCString("xyz")) {
		t.Error("expected ABC and xyz to not be equal")
	}
}

func TestUCString_EqualString(t *testing.T) {
	if !UCString("ABC").EqualString("abc") {
		t.Error("expected EqualString to match case-insensitively")
	}
}

// TestLCString_MarshalJSON_NotFromDB verifies a value constructed directly
// in Go (never passed through Scan) still normalizes on JSON marshal.
func TestLCString_MarshalJSON_NotFromDB(t *testing.T) {
	s := LCString("MixedCase")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != `"mixedcase"` {
		t.Errorf("expected %s, got %s", `"mixedcase"`, b)
	}
}

func TestLCString_UnmarshalJSON(t *testing.T) {
	var s LCString
	if err := json.Unmarshal([]byte(`"MixedCase"`), &s); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if s != "mixedcase" {
		t.Errorf("expected %q, got %q", "mixedcase", s)
	}
}

func TestLCString_JSON_StructField(t *testing.T) {
	type wrapper struct {
		Code LCString `json:"code"`
	}
	in := wrapper{Code: "MixedCase"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != `{"code":"mixedcase"}` {
		t.Errorf("expected %s, got %s", `{"code":"mixedcase"}`, b)
	}

	var out wrapper
	if err := json.Unmarshal([]byte(`{"code":"AnotherMixedCase"}`), &out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if out.Code != "anothermixedcase" {
		t.Errorf("expected %q, got %q", "anothermixedcase", out.Code)
	}
}

// TestUCString_MarshalJSON_NotFromDB verifies a value constructed directly
// in Go (never passed through Scan) still normalizes on JSON marshal.
func TestUCString_MarshalJSON_NotFromDB(t *testing.T) {
	s := UCString("MixedCase")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != `"MIXEDCASE"` {
		t.Errorf("expected %s, got %s", `"MIXEDCASE"`, b)
	}
}

func TestUCString_UnmarshalJSON(t *testing.T) {
	var s UCString
	if err := json.Unmarshal([]byte(`"MixedCase"`), &s); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if s != "MIXEDCASE" {
		t.Errorf("expected %q, got %q", "MIXEDCASE", s)
	}
}

func TestUCString_JSON_StructField(t *testing.T) {
	type wrapper struct {
		Code UCString `json:"code"`
	}
	in := wrapper{Code: "MixedCase"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != `{"code":"MIXEDCASE"}` {
		t.Errorf("expected %s, got %s", `{"code":"MIXEDCASE"}`, b)
	}

	var out wrapper
	if err := json.Unmarshal([]byte(`{"code":"AnotherMixedCase"}`), &out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if out.Code != "ANOTHERMIXEDCASE" {
		t.Errorf("expected %q, got %q", "ANOTHERMIXEDCASE", out.Code)
	}
}

// TestCIString_JSON_PreservesCase confirms CIString needs no custom JSON
// methods: it should never normalize case, only its DB Value/Scan and the
// Equal/EqualString comparisons apply case-insensitivity.
func TestCIString_JSON_PreservesCase(t *testing.T) {
	s := CIString("MixedCase")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != `"MixedCase"` {
		t.Errorf("expected %s, got %s", `"MixedCase"`, b)
	}

	var out CIString
	if err := json.Unmarshal([]byte(`"AnotherMixedCase"`), &out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if out != "AnotherMixedCase" {
		t.Errorf("expected case to be preserved, got %q", out)
	}
}
