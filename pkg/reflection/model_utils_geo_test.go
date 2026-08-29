package reflection

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

type geoModel struct {
	ID        int64                   `json:"id"`
	Location  spectypes.SqlGeometry   `json:"location"`
	Area      spectypes.SqlGeography  `json:"area"`
	Embedding spectypes.SqlVector     `json:"embedding"`
	HalfEmb   spectypes.SqlHalfVector `json:"half_emb"`
	Name      spectypes.SqlString     `json:"name"`
}

func TestGetColumnSQLTypeName(t *testing.T) {
	m := geoModel{}
	cases := map[string]string{
		"location":  "geometry",
		"area":      "geography",
		"embedding": "vector",
		"half_emb":  "halfvec",
		"name":      "text",
	}
	for col, want := range cases {
		got, ok := GetColumnSQLTypeName(m, col)
		if !ok || got != want {
			t.Errorf("GetColumnSQLTypeName(%q) = %q, %v; want %q", col, got, ok, want)
		}
	}
	if _, ok := GetColumnSQLTypeName(m, "id"); ok {
		t.Error("id is not a spectypes column")
	}
}

func TestIsSpatialColumn(t *testing.T) {
	m := geoModel{}
	if !IsSpatialColumn(m, "location") || !IsSpatialColumn(m, "area") {
		t.Error("location/area should be spatial")
	}
	if IsSpatialColumn(m, "embedding") || IsSpatialColumn(m, "name") {
		t.Error("embedding/name should not be spatial")
	}
}

func TestIsVectorColumn(t *testing.T) {
	m := geoModel{}
	if !IsVectorColumn(m, "embedding") || !IsVectorColumn(m, "half_emb") {
		t.Error("embedding/half_emb should be vector")
	}
	if IsVectorColumn(m, "location") || IsVectorColumn(m, "name") {
		t.Error("location/name should not be vector")
	}
}
