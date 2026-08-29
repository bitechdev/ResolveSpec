package spectypes

import (
	"reflect"
	"testing"
)

func TestSQLTypeName(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{SqlVector{}, "vector"},
		{SqlHalfVector{}, "halfvec"},
		{SqlSparseVector{}, "sparsevec"},
		{SqlBitVector{}, "bit"},
		{SqlGeometry{}, "geometry"},
		{SqlGeography{}, "geography"},
		{SqlJSONB{}, "jsonb"},
		{SqlStringArray{}, "text[]"},
		{SqlString{}, "text"},
		{SqlInt64{}, "bigint"},
	}
	for _, c := range cases {
		got, ok := SQLTypeName(reflect.TypeOf(c.val))
		if !ok || got != c.want {
			t.Errorf("SQLTypeName(%T) = %q, %v; want %q", c.val, got, ok, c.want)
		}
	}

	// Pointer is unwrapped.
	if got, ok := SQLTypeName(reflect.TypeOf(&SqlGeometry{})); !ok || got != "geometry" {
		t.Errorf("pointer: got %q, %v", got, ok)
	}

	// Non-spectypes type.
	if _, ok := SQLTypeName(reflect.TypeOf("")); ok {
		t.Error("expected false for string")
	}
}

func TestIsSpatialType(t *testing.T) {
	if !IsSpatialType(reflect.TypeOf(SqlGeometry{})) {
		t.Error("SqlGeometry should be spatial")
	}
	if !IsSpatialType(reflect.TypeOf(SqlGeography{})) {
		t.Error("SqlGeography should be spatial")
	}
	if IsSpatialType(reflect.TypeOf(SqlVector{})) {
		t.Error("SqlVector should not be spatial")
	}
}

func TestIsVectorType(t *testing.T) {
	for _, v := range []any{SqlVector{}, SqlHalfVector{}, SqlSparseVector{}} {
		if !IsVectorType(reflect.TypeOf(v)) {
			t.Errorf("%T should be vector", v)
		}
	}
	if IsVectorType(reflect.TypeOf(SqlBitVector{})) {
		t.Error("SqlBitVector is not a vector type")
	}
	if IsVectorType(reflect.TypeOf(SqlGeometry{})) {
		t.Error("SqlGeometry is not a vector type")
	}
}
