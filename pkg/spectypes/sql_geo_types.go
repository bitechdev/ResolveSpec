package spectypes

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ── PostGIS geometry / geography ─────────────────────────────────────────────

// SqlGeometry is a nullable PostGIS `geometry` column.
//
// On Scan it accepts the PostGIS default hex-EWKB text output, a raw GeoJSON
// object, or a WKT/EWKT string (e.g. when the column is selected via
// ST_AsGeoJSON / ST_AsText). Internally it holds a canonical GeoJSON geometry
// object plus the SRID.
//
// On Value it emits `SRID=<n>;<WKT>` text. PostGIS registers an implicit
// text -> geometry cast, so parameterised inserts/updates work without wrapping
// the placeholder in a constructor function.
//
// MarshalJSON emits the GeoJSON geometry object (or null).
type SqlGeometry struct {
	GeoJSON json.RawMessage
	SRID    int
	Valid   bool
}

// SqlGeography is identical to SqlGeometry but maps to a PostGIS `geography`
// column. Coordinates are always lon/lat and the default SRID is 4326.
type SqlGeography struct {
	SqlGeometry
}

func (g *SqlGeometry) Scan(value any) error {
	if value == nil {
		g.Valid = false
		g.GeoJSON = nil
		g.SRID = 0
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlGeometry: cannot scan type %T", value)
	}

	s = strings.TrimSpace(s)
	if s == "" {
		g.Valid = false
		g.GeoJSON = nil
		return nil
	}

	switch {
	case strings.HasPrefix(s, "{"):
		// GeoJSON object.
		if _, err := geoJSONToGeom([]byte(s)); err != nil {
			return fmt.Errorf("SqlGeometry: invalid GeoJSON: %w", err)
		}
		g.GeoJSON = json.RawMessage(s)
		g.Valid = true
		return nil
	case isHex(s):
		gj, srid, err := DecodeEWKBHex(s)
		if err != nil {
			return fmt.Errorf("SqlGeometry: %w", err)
		}
		g.GeoJSON = gj
		g.SRID = srid
		g.Valid = true
		return nil
	default:
		// WKT / EWKT text.
		srid, wkt := splitEWKT(s)
		gj, err := wktToGeoJSON(wkt)
		if err != nil {
			return fmt.Errorf("SqlGeometry: %w", err)
		}
		g.GeoJSON = gj
		g.SRID = srid
		g.Valid = true
		return nil
	}
}

func (g SqlGeometry) Value() (driver.Value, error) {
	if !g.Valid || len(g.GeoJSON) == 0 {
		return nil, nil
	}
	wkt, err := GeoJSONToWKT(g.GeoJSON)
	if err != nil {
		return nil, err
	}
	srid := g.SRID
	if srid == 0 {
		srid = 4326
	}
	return fmt.Sprintf("SRID=%d;%s", srid, wkt), nil
}

func (g SqlGeometry) MarshalJSON() ([]byte, error) {
	if !g.Valid || len(g.GeoJSON) == 0 {
		return []byte("null"), nil
	}
	return g.GeoJSON, nil
}

func (g *SqlGeometry) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		g.Valid = false
		g.GeoJSON = nil
		g.SRID = 0
		return nil
	}

	if strings.HasPrefix(s, "{") {
		if _, err := geoJSONToGeom(b); err != nil {
			return fmt.Errorf("SqlGeometry: invalid GeoJSON: %w", err)
		}
		g.GeoJSON = append(json.RawMessage(nil), b...)
		g.Valid = true
		return nil
	}

	// String value: EWKT / WKT / hex-EWKB.
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return fmt.Errorf("SqlGeometry: cannot unmarshal %s", b)
	}
	str = strings.TrimSpace(str)
	if str == "" {
		g.Valid = false
		g.GeoJSON = nil
		return nil
	}
	if isHex(str) {
		gj, srid, err := DecodeEWKBHex(str)
		if err != nil {
			return fmt.Errorf("SqlGeometry: %w", err)
		}
		g.GeoJSON = gj
		g.SRID = srid
		g.Valid = true
		return nil
	}
	srid, wkt := splitEWKT(str)
	gj, err := wktToGeoJSON(wkt)
	if err != nil {
		return fmt.Errorf("SqlGeometry: %w", err)
	}
	g.GeoJSON = gj
	g.SRID = srid
	g.Valid = true
	return nil
}

// WKT returns the geometry as a plain WKT string (no SRID prefix).
func (g SqlGeometry) WKT() string {
	if !g.Valid || len(g.GeoJSON) == 0 {
		return ""
	}
	wkt, err := GeoJSONToWKT(g.GeoJSON)
	if err != nil {
		return ""
	}
	return wkt
}

// EWKT returns the geometry as `SRID=<n>;<WKT>`.
func (g SqlGeometry) EWKT() string {
	wkt := g.WKT()
	if wkt == "" {
		return ""
	}
	srid := g.SRID
	if srid == 0 {
		srid = 4326
	}
	return fmt.Sprintf("SRID=%d;%s", srid, wkt)
}

// NewSqlGeometryFromGeoJSON builds a SqlGeometry from a GeoJSON geometry object.
func NewSqlGeometryFromGeoJSON(geojson []byte, srid int) (SqlGeometry, error) {
	if _, err := geoJSONToGeom(geojson); err != nil {
		return SqlGeometry{}, err
	}
	return SqlGeometry{GeoJSON: append(json.RawMessage(nil), geojson...), SRID: srid, Valid: true}, nil
}

// NewSqlGeometryFromEWKT builds a SqlGeometry from an EWKT or WKT string.
func NewSqlGeometryFromEWKT(ewkt string) (SqlGeometry, error) {
	srid, wkt := splitEWKT(strings.TrimSpace(ewkt))
	gj, err := wktToGeoJSON(wkt)
	if err != nil {
		return SqlGeometry{}, err
	}
	return SqlGeometry{GeoJSON: gj, SRID: srid, Valid: true}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func isHex(s string) bool {
	if len(s) < 10 || len(s)%2 != 0 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// splitEWKT separates an optional `SRID=<n>;` prefix from a WKT body.
func splitEWKT(s string) (srid int, wkt string) {
	if strings.HasPrefix(strings.ToUpper(s), "SRID=") {
		if idx := strings.Index(s, ";"); idx > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(s[5:idx])); err == nil {
				return n, strings.TrimSpace(s[idx+1:])
			}
		}
	}
	return 0, s
}

// wktToGeoJSON parses a (subset of) WKT into a GeoJSON geometry object.
func wktToGeoJSON(wkt string) ([]byte, error) {
	g, err := parseWKT(wkt)
	if err != nil {
		return nil, err
	}
	return geomToGeoJSON(g)
}
