package spectypes

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSqlHalfVector_RoundTrip(t *testing.T) {
	v := NewSqlHalfVector([]float32{1, 2.5, -3})

	dv, err := v.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if dv != "[1,2.5,-3]" {
		t.Errorf("Value = %v, want [1,2.5,-3]", dv)
	}

	var back SqlHalfVector
	if err := back.Scan(dv.(string)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !back.Valid || !reflect.DeepEqual(back.Val, v.Val) {
		t.Errorf("Scan = %+v, want %+v", back, v)
	}

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != "[1,2.5,-3]" {
		t.Errorf("json = %s, want [1,2.5,-3]", b)
	}

	var fromJSON SqlHalfVector
	if err := json.Unmarshal(b, &fromJSON); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(fromJSON.Val, v.Val) {
		t.Errorf("json round-trip = %+v", fromJSON)
	}
}

func TestSqlHalfVector_Null(t *testing.T) {
	var v SqlHalfVector
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if v.Valid {
		t.Error("expected invalid after Scan(nil)")
	}
	dv, err := v.Value()
	if err != nil || dv != nil {
		t.Errorf("Value = %v, %v; want nil, nil", dv, err)
	}
	b, _ := json.Marshal(v)
	if string(b) != "null" {
		t.Errorf("json = %s, want null", b)
	}
}

func TestSqlSparseVector_RoundTrip(t *testing.T) {
	v := NewSqlSparseVector(8, []int32{1, 4}, []float32{0.5, 0.2})

	dv, err := v.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if dv != "{1:0.5,4:0.2}/8" {
		t.Errorf("Value = %v, want {1:0.5,4:0.2}/8", dv)
	}

	var back SqlSparseVector
	if err := back.Scan(dv.(string)); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if back.Dim != 8 || !reflect.DeepEqual(back.Indices, []int32{1, 4}) ||
		!reflect.DeepEqual(back.Values, []float32{0.5, 0.2}) {
		t.Errorf("Scan = %+v", back)
	}

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"dim":8,"indices":[1,4],"values":[0.5,0.2]}`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}

	var fromJSON SqlSparseVector
	if err := json.Unmarshal([]byte(want), &fromJSON); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if fromJSON.Dim != 8 || !fromJSON.Valid {
		t.Errorf("json round-trip = %+v", fromJSON)
	}
}

func TestSqlSparseVector_ScanInvalid(t *testing.T) {
	var v SqlSparseVector
	for _, s := range []string{"[1,2,3]", "{1:0.5}", "{1:0.5}/x", "bad"} {
		if err := v.Scan(s); err == nil {
			t.Errorf("Scan(%q) expected error", s)
		}
	}
}

func TestSqlBitVector_RoundTrip(t *testing.T) {
	v := NewSqlBitVector([]bool{true, false, true, true})

	dv, err := v.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if dv != "1011" {
		t.Errorf("Value = %v, want 1011", dv)
	}

	var back SqlBitVector
	if err := back.Scan("1011"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(back.Val, v.Val) {
		t.Errorf("Scan = %+v", back)
	}

	b, _ := json.Marshal(v)
	if string(b) != "[true,false,true,true]" {
		t.Errorf("json = %s", b)
	}

	// JSON also accepts a "0101" string.
	var fromStr SqlBitVector
	if err := json.Unmarshal([]byte(`"1011"`), &fromStr); err != nil {
		t.Fatalf("Unmarshal string: %v", err)
	}
	if !reflect.DeepEqual(fromStr.Val, v.Val) {
		t.Errorf("string json = %+v", fromStr)
	}
}

func TestSqlBitVector_ScanInvalid(t *testing.T) {
	var v SqlBitVector
	if err := v.Scan("1021"); err == nil {
		t.Error("expected error for invalid bit")
	}
}
