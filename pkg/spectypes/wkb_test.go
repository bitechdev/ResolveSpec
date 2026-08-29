package spectypes

import (
	"encoding/json"
	"testing"
)

func TestDecodeEWKBHex(t *testing.T) {
	tests := []struct {
		name     string
		hex      string
		wantSRID int
		wantJSON string
	}{
		{
			// SRID=4326;POINT(1 2)
			name:     "point with srid",
			hex:      "0101000020E6100000000000000000F03F0000000000000040",
			wantSRID: 4326,
			wantJSON: `{"type":"Point","coordinates":[1,2]}`,
		},
		{
			// POINT(1 2) no SRID, little endian
			name:     "point no srid",
			hex:      "0101000000000000000000F03F0000000000000040",
			wantSRID: 0,
			wantJSON: `{"type":"Point","coordinates":[1,2]}`,
		},
		{
			// SRID=4326;LINESTRING(0 0, 1 1, 2 2)
			name: "linestring",
			hex: "0102000020E610000003000000000000000000000000000000000000000000000000" +
				"00F03F000000000000F03F00000000000000400000000000000040",
			wantSRID: 4326,
			wantJSON: `{"type":"LineString","coordinates":[[0,0],[1,1],[2,2]]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gj, srid, err := DecodeEWKBHex(tt.hex)
			if err != nil {
				t.Fatalf("DecodeEWKBHex: %v", err)
			}
			if srid != tt.wantSRID {
				t.Errorf("srid = %d, want %d", srid, tt.wantSRID)
			}
			if !jsonEqual(t, gj, tt.wantJSON) {
				t.Errorf("geojson = %s, want %s", gj, tt.wantJSON)
			}
			// Round-trip through WKT parser.
			wkt, err := GeoJSONToWKT(gj)
			if err != nil {
				t.Fatalf("GeoJSONToWKT: %v", err)
			}
			gj2, err := wktToGeoJSON(wkt)
			if err != nil {
				t.Fatalf("wktToGeoJSON(%q): %v", wkt, err)
			}
			if !jsonEqual(t, gj2, tt.wantJSON) {
				t.Errorf("round-trip geojson = %s, want %s", gj2, tt.wantJSON)
			}
		})
	}
}

func TestWKTPolygon(t *testing.T) {
	src := `POLYGON ((0 0, 4 0, 4 4, 0 4, 0 0), (1 1, 2 1, 2 2, 1 2, 1 1))`
	gj, err := wktToGeoJSON(src)
	if err != nil {
		t.Fatalf("wktToGeoJSON: %v", err)
	}
	want := `{"type":"Polygon","coordinates":[[[0,0],[4,0],[4,4],[0,4],[0,0]],[[1,1],[2,1],[2,2],[1,2],[1,1]]]}`
	if !jsonEqual(t, gj, want) {
		t.Errorf("geojson = %s, want %s", gj, want)
	}
}

func jsonEqual(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
