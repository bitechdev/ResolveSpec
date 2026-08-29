package spectypes

import (
	"encoding/json"
	"testing"
)

// SRID=4326;POINT (1 2)
const pointHexEWKB = "0101000020E6100000000000000000F03F0000000000000040"

func TestSqlGeometry_ScanHexEWKB(t *testing.T) {
	var g SqlGeometry
	if err := g.Scan(pointHexEWKB); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !g.Valid || g.SRID != 4326 {
		t.Fatalf("got Valid=%v SRID=%d", g.Valid, g.SRID)
	}
	if !jsonEqual(t, g.GeoJSON, `{"type":"Point","coordinates":[1,2]}`) {
		t.Errorf("GeoJSON = %s", g.GeoJSON)
	}
}

func TestSqlGeometry_ScanGeoJSON(t *testing.T) {
	var g SqlGeometry
	if err := g.Scan(`{"type":"Point","coordinates":[3,4]}`); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !g.Valid {
		t.Fatal("expected valid")
	}
	if !jsonEqual(t, g.GeoJSON, `{"type":"Point","coordinates":[3,4]}`) {
		t.Errorf("GeoJSON = %s", g.GeoJSON)
	}
}

func TestSqlGeometry_ScanEWKT(t *testing.T) {
	var g SqlGeometry
	if err := g.Scan("SRID=3857;POINT (5 6)"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if g.SRID != 3857 {
		t.Errorf("SRID = %d, want 3857", g.SRID)
	}
	if !jsonEqual(t, g.GeoJSON, `{"type":"Point","coordinates":[5,6]}`) {
		t.Errorf("GeoJSON = %s", g.GeoJSON)
	}
}

func TestSqlGeometry_Value(t *testing.T) {
	g, err := NewSqlGeometryFromGeoJSON([]byte(`{"type":"Point","coordinates":[1,2]}`), 4326)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v, err := g.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != "SRID=4326;POINT (1 2)" {
		t.Errorf("Value = %v, want SRID=4326;POINT (1 2)", v)
	}
}

func TestSqlGeometry_ValueDefaultsSRID(t *testing.T) {
	g, _ := NewSqlGeometryFromGeoJSON([]byte(`{"type":"Point","coordinates":[1,2]}`), 0)
	v, _ := g.Value()
	if v != "SRID=4326;POINT (1 2)" {
		t.Errorf("Value = %v", v)
	}
}

func TestSqlGeometry_JSON(t *testing.T) {
	g, _ := NewSqlGeometryFromEWKT("SRID=4326;POINT (1 2)")
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !jsonEqual(t, b, `{"type":"Point","coordinates":[1,2]}`) {
		t.Errorf("json = %s", b)
	}

	var back SqlGeometry
	if err := json.Unmarshal([]byte(`{"type":"Point","coordinates":[7,8]}`), &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !back.Valid || !jsonEqual(t, back.GeoJSON, `{"type":"Point","coordinates":[7,8]}`) {
		t.Errorf("unmarshal = %+v", back)
	}

	// Unmarshal also accepts an EWKT string.
	var fromStr SqlGeometry
	if err := json.Unmarshal([]byte(`"SRID=4326;POINT(9 10)"`), &fromStr); err != nil {
		t.Fatalf("Unmarshal string: %v", err)
	}
	if !jsonEqual(t, fromStr.GeoJSON, `{"type":"Point","coordinates":[9,10]}`) {
		t.Errorf("fromStr = %s", fromStr.GeoJSON)
	}
}

func TestSqlGeometry_Null(t *testing.T) {
	var g SqlGeometry
	if err := g.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if g.Valid {
		t.Error("expected invalid")
	}
	v, err := g.Value()
	if err != nil || v != nil {
		t.Errorf("Value = %v, %v", v, err)
	}
	b, _ := json.Marshal(g)
	if string(b) != "null" {
		t.Errorf("json = %s", b)
	}
}

func TestSqlGeography_Embeds(t *testing.T) {
	var g SqlGeography
	if err := g.Scan("SRID=4326;POINT (1 2)"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !g.Valid || !jsonEqual(t, g.GeoJSON, `{"type":"Point","coordinates":[1,2]}`) {
		t.Errorf("geography scan = %+v", g)
	}
}
