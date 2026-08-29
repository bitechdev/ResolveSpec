package common

import (
	"testing"
)

func TestVectorOperator(t *testing.T) {
	cases := map[string]string{
		"": "<->", "l2": "<->", "euclidean": "<->",
		"cosine": "<=>", "cos": "<=>",
		"ip": "<#>", "inner": "<#>", "dot": "<#>",
	}
	for in, want := range cases {
		if got := VectorOperator(in); got != want {
			t.Errorf("VectorOperator(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVectorLiteral(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{[]float32{1, 2, 3}, "[1,2,3]"},
		{[]float64{1.5, -2}, "[1.5,-2]"},
		{[]int{1, 2}, "[1,2]"},
		{[]any{1.0, 2.0}, "[1,2]"},
		{"[4,5,6]", "[4,5,6]"},
	}
	for _, c := range cases {
		got, err := VectorLiteral(c.in)
		if err != nil || got != c.want {
			t.Errorf("VectorLiteral(%v) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := VectorLiteral("not-a-vector"); err == nil {
		t.Error("expected error for malformed string")
	}
	if _, err := VectorLiteral(42); err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestBuildVectorCondition(t *testing.T) {
	q, args, ok := BuildVectorCondition("embedding", "cosine_within", map[string]any{
		"vector": []any{1.0, 2.0, 3.0}, "distance": 0.5,
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if q != "embedding <=> ? < ?" {
		t.Errorf("query = %q", q)
	}
	if len(args) != 2 || args[0] != "[1,2,3]" || args[1] != 0.5 {
		t.Errorf("args = %v", args)
	}

	// explicit comparator
	q, _, ok = BuildVectorCondition("v", "l2_within", map[string]any{
		"vector": []float32{1}, "lte": 2.0,
	})
	if !ok || q != "v <-> ? <= ?" {
		t.Errorf("lte: q=%q ok=%v", q, ok)
	}

	// unknown operator
	if _, _, ok := BuildVectorCondition("v", "bogus", map[string]any{}); ok {
		t.Error("expected not ok for unknown operator")
	}
	// missing threshold
	if _, _, ok := BuildVectorCondition("v", "l2_within", map[string]any{"vector": []float32{1}}); ok {
		t.Error("expected not ok without threshold")
	}
}

func TestBuildSpatialCondition_Predicates(t *testing.T) {
	q, args, ok := BuildSpatialCondition("geom", "st_intersects", "SRID=4326;POINT(0 0)")
	if !ok {
		t.Fatal("expected ok")
	}
	if q != "ST_Intersects(geom, ST_GeomFromEWKT(?))" {
		t.Errorf("query = %q", q)
	}
	if len(args) != 1 || args[0] != "SRID=4326;POINT(0 0)" {
		t.Errorf("args = %v", args)
	}

	// GeoJSON value
	q, args, ok = BuildSpatialCondition("geom", "st_contains", map[string]any{
		"type": "Point", "coordinates": []any{1.0, 2.0},
	})
	if !ok || q != "ST_Contains(geom, ST_GeomFromGeoJSON(?))" {
		t.Errorf("geojson: q=%q ok=%v", q, ok)
	}
	if len(args) != 1 {
		t.Errorf("args = %v", args)
	}
}

func TestBuildSpatialCondition_DWithin(t *testing.T) {
	q, args, ok := BuildSpatialCondition("geom", "st_dwithin", map[string]any{
		"geom": "SRID=4326;POINT(0 0)", "distance": 1000.0,
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if q != "ST_DWithin(geom, ST_GeomFromEWKT(?), ?)" {
		t.Errorf("query = %q", q)
	}
	if len(args) != 2 || args[1] != 1000.0 {
		t.Errorf("args = %v", args)
	}
}

func TestBuildSpatialCondition_BBox(t *testing.T) {
	q, args, ok := BuildSpatialCondition("geom", "bbox", map[string]any{
		"bbox": []any{0.0, 0.0, 10.0, 10.0}, "srid": 4326.0,
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if q != "geom && ST_MakeEnvelope(?, ?, ?, ?, ?)" {
		t.Errorf("query = %q", q)
	}
	if len(args) != 5 || args[4] != 4326 {
		t.Errorf("args = %v", args)
	}
}

func TestIsSpatialAndVectorOperator(t *testing.T) {
	for _, op := range []string{"st_dwithin", "st_intersects", "bbox", "&&"} {
		if !IsSpatialOperator(op) {
			t.Errorf("%q should be spatial", op)
		}
	}
	for _, op := range []string{"l2_within", "cosine_within", "ip_within"} {
		if !IsVectorOperator(op) {
			t.Errorf("%q should be vector", op)
		}
	}
	if IsSpatialOperator("eq") || IsVectorOperator("eq") {
		t.Error("eq is neither spatial nor vector")
	}
}
