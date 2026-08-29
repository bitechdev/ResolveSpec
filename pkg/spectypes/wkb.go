package spectypes

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Minimal self-contained EWKB (PostGIS extended WKB) <-> GeoJSON / WKT codec.
// Supports 2D and 3D (Z) geometries of type Point, LineString, Polygon,
// MultiPoint, MultiLineString, MultiPolygon and GeometryCollection. The M
// dimension is parsed but dropped (GeoJSON has no M). SRID is tracked separately
// from the GeoJSON payload (GeoJSON assumes CRS84 / EPSG:4326).

// EWKB type flag bits (PostGIS).
const (
	ewkbZ    = 0x80000000
	ewkbM    = 0x40000000
	ewkbSRID = 0x20000000
)

// geom is the intermediate geometry representation used by the codec.
//
//	Point               -> coord  ([]float64, len 2 or 3)
//	LineString/MultiPt   -> line   ([][]float64)
//	Polygon/MultiLine    -> poly   ([][][]float64)
//	MultiPolygon         -> multi  ([][][][]float64)
//	GeometryCollection   -> geoms  ([]geom)
type geom struct {
	typ   string
	coord []float64
	line  [][]float64
	poly  [][][]float64
	multi [][][][]float64
	geoms []geom
}

// wkbReader consumes an EWKB byte stream.
type wkbReader struct {
	buf []byte
	pos int
}

func (r *wkbReader) readByte() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, fmt.Errorf("wkb: unexpected end of input")
	}
	b := r.buf[r.pos]
	r.pos++
	return b, nil
}

func (r *wkbReader) readUint32(bo binary.ByteOrder) (uint32, error) {
	if r.pos+4 > len(r.buf) {
		return 0, fmt.Errorf("wkb: unexpected end of input")
	}
	v := bo.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *wkbReader) readFloat64(bo binary.ByteOrder) (float64, error) {
	if r.pos+8 > len(r.buf) {
		return 0, fmt.Errorf("wkb: unexpected end of input")
	}
	v := math.Float64frombits(bo.Uint64(r.buf[r.pos:]))
	r.pos += 8
	return v, nil
}

// DecodeEWKBHex decodes a PostGIS hex-EWKB string (the default text
// representation of a geometry column) into a GeoJSON geometry object and its
// SRID. An SRID of 0 means "unspecified".
func DecodeEWKBHex(s string) (geojson []byte, srid int, err error) {
	s = strings.TrimSpace(s)
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, 0, fmt.Errorf("wkb: invalid hex: %w", err)
	}
	return DecodeEWKB(raw)
}

// DecodeEWKB decodes raw PostGIS EWKB bytes into a GeoJSON geometry object and
// its SRID.
func DecodeEWKB(raw []byte) (geojson []byte, srid int, err error) {
	r := &wkbReader{buf: raw}
	g, sr, err := readGeom(r)
	if err != nil {
		return nil, 0, err
	}
	out, err := geomToGeoJSON(g)
	if err != nil {
		return nil, 0, err
	}
	return out, sr, nil
}

func readGeom(r *wkbReader) (geom, int, error) {
	order, err := r.readByte()
	if err != nil {
		return geom{}, 0, err
	}
	var bo binary.ByteOrder
	switch order {
	case 0:
		bo = binary.BigEndian
	case 1:
		bo = binary.LittleEndian
	default:
		return geom{}, 0, fmt.Errorf("wkb: invalid byte order %d", order)
	}

	rawType, err := r.readUint32(bo)
	if err != nil {
		return geom{}, 0, err
	}
	hasZ := rawType&ewkbZ != 0
	hasM := rawType&ewkbM != 0
	hasSRID := rawType&ewkbSRID != 0
	baseType := rawType & 0xff

	srid := 0
	if hasSRID {
		s, err := r.readUint32(bo)
		if err != nil {
			return geom{}, 0, err
		}
		srid = int(s)
	}

	dims := 2
	if hasZ {
		dims = 3
	}
	// M is consumed but not retained.
	stride := dims
	if hasM {
		stride++
	}

	readCoord := func() ([]float64, error) {
		c := make([]float64, 0, dims)
		for i := 0; i < stride; i++ {
			v, err := r.readFloat64(bo)
			if err != nil {
				return nil, err
			}
			if i < dims {
				c = append(c, v)
			}
		}
		return c, nil
	}
	readLine := func() ([][]float64, error) {
		n, err := r.readUint32(bo)
		if err != nil {
			return nil, err
		}
		pts := make([][]float64, n)
		for i := range pts {
			pts[i], err = readCoord()
			if err != nil {
				return nil, err
			}
		}
		return pts, nil
	}
	readPoly := func() ([][][]float64, error) {
		n, err := r.readUint32(bo)
		if err != nil {
			return nil, err
		}
		rings := make([][][]float64, n)
		for i := range rings {
			rings[i], err = readLine()
			if err != nil {
				return nil, err
			}
		}
		return rings, nil
	}

	switch baseType {
	case 1: // Point
		c, err := readCoord()
		if err != nil {
			return geom{}, 0, err
		}
		return geom{typ: "Point", coord: c}, srid, nil
	case 2: // LineString
		l, err := readLine()
		if err != nil {
			return geom{}, 0, err
		}
		return geom{typ: "LineString", line: l}, srid, nil
	case 3: // Polygon
		p, err := readPoly()
		if err != nil {
			return geom{}, 0, err
		}
		return geom{typ: "Polygon", poly: p}, srid, nil
	case 4, 5, 6: // Multi*
		n, err := r.readUint32(bo)
		if err != nil {
			return geom{}, 0, err
		}
		parts := make([]geom, n)
		for i := range parts {
			sub, _, err := readGeom(r)
			if err != nil {
				return geom{}, 0, err
			}
			parts[i] = sub
		}
		switch baseType {
		case 4:
			pts := make([][]float64, len(parts))
			for i, p := range parts {
				pts[i] = p.coord
			}
			return geom{typ: "MultiPoint", line: pts}, srid, nil
		case 5:
			lines := make([][][]float64, len(parts))
			for i, p := range parts {
				lines[i] = p.line
			}
			return geom{typ: "MultiLineString", poly: lines}, srid, nil
		default:
			polys := make([][][][]float64, len(parts))
			for i, p := range parts {
				polys[i] = p.poly
			}
			return geom{typ: "MultiPolygon", multi: polys}, srid, nil
		}
	case 7: // GeometryCollection
		n, err := r.readUint32(bo)
		if err != nil {
			return geom{}, 0, err
		}
		parts := make([]geom, n)
		for i := range parts {
			sub, _, err := readGeom(r)
			if err != nil {
				return geom{}, 0, err
			}
			parts[i] = sub
		}
		return geom{typ: "GeometryCollection", geoms: parts}, srid, nil
	default:
		return geom{}, 0, fmt.Errorf("wkb: unsupported geometry type %d", baseType)
	}
}

// ── GeoJSON ──────────────────────────────────────────────────────────────────

type geoJSON struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates,omitempty"`
	Geometries  []geoJSON       `json:"geometries,omitempty"`
}

func geomToGeoJSON(g geom) ([]byte, error) {
	gj, err := geomToGeoJSONStruct(g)
	if err != nil {
		return nil, err
	}
	return json.Marshal(gj)
}

func geomToGeoJSONStruct(g geom) (geoJSON, error) {
	var coords any
	switch g.typ {
	case "Point":
		coords = g.coord
	case "LineString", "MultiPoint":
		coords = g.line
	case "Polygon", "MultiLineString":
		coords = g.poly
	case "MultiPolygon":
		coords = g.multi
	case "GeometryCollection":
		subs := make([]geoJSON, len(g.geoms))
		for i, sub := range g.geoms {
			s, err := geomToGeoJSONStruct(sub)
			if err != nil {
				return geoJSON{}, err
			}
			subs[i] = s
		}
		return geoJSON{Type: "GeometryCollection", Geometries: subs}, nil
	default:
		return geoJSON{}, fmt.Errorf("wkb: cannot encode geometry type %q", g.typ)
	}
	rc, err := json.Marshal(coords)
	if err != nil {
		return geoJSON{}, err
	}
	return geoJSON{Type: g.typ, Coordinates: rc}, nil
}

func geoJSONToGeom(data []byte) (geom, error) {
	var gj geoJSON
	if err := json.Unmarshal(data, &gj); err != nil {
		return geom{}, fmt.Errorf("geojson: %w", err)
	}
	return geoJSONStructToGeom(gj)
}

func geoJSONStructToGeom(gj geoJSON) (geom, error) {
	switch gj.Type {
	case "Point":
		var c []float64
		if err := json.Unmarshal(gj.Coordinates, &c); err != nil {
			return geom{}, err
		}
		return geom{typ: "Point", coord: c}, nil
	case "LineString", "MultiPoint":
		var l [][]float64
		if err := json.Unmarshal(gj.Coordinates, &l); err != nil {
			return geom{}, err
		}
		return geom{typ: gj.Type, line: l}, nil
	case "Polygon", "MultiLineString":
		var p [][][]float64
		if err := json.Unmarshal(gj.Coordinates, &p); err != nil {
			return geom{}, err
		}
		return geom{typ: gj.Type, poly: p}, nil
	case "MultiPolygon":
		var m [][][][]float64
		if err := json.Unmarshal(gj.Coordinates, &m); err != nil {
			return geom{}, err
		}
		return geom{typ: gj.Type, multi: m}, nil
	case "GeometryCollection":
		subs := make([]geom, len(gj.Geometries))
		for i, s := range gj.Geometries {
			g, err := geoJSONStructToGeom(s)
			if err != nil {
				return geom{}, err
			}
			subs[i] = g
		}
		return geom{typ: "GeometryCollection", geoms: subs}, nil
	default:
		return geom{}, fmt.Errorf("geojson: unsupported type %q", gj.Type)
	}
}

// ── WKT ──────────────────────────────────────────────────────────────────────

// GeoJSONToWKT converts a GeoJSON geometry object to its WKT representation.
func GeoJSONToWKT(geojson []byte) (string, error) {
	g, err := geoJSONToGeom(geojson)
	if err != nil {
		return "", err
	}
	return geomToWKT(g)
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func coordWKT(c []float64) string {
	parts := make([]string, len(c))
	for i, v := range c {
		parts[i] = fmtNum(v)
	}
	return strings.Join(parts, " ")
}

func lineWKT(pts [][]float64) string {
	parts := make([]string, len(pts))
	for i, p := range pts {
		parts[i] = coordWKT(p)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func polyWKT(rings [][][]float64) string {
	parts := make([]string, len(rings))
	for i, r := range rings {
		parts[i] = lineWKT(r)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// ── WKT parsing ─────────────────────────────────────────────────────────────

// parseWKT parses a subset of WKT (2D/3D, no M) into the intermediate geom.
func parseWKT(s string) (geom, error) {
	p := &wktParser{s: s}
	p.skipSpace()
	g, err := p.parseGeom()
	if err != nil {
		return geom{}, err
	}
	p.skipSpace()
	if p.pos != len(p.s) {
		return geom{}, fmt.Errorf("wkt: trailing input %q", p.s[p.pos:])
	}
	return g, nil
}

type wktParser struct {
	s   string
	pos int
}

func (p *wktParser) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n' || p.s[p.pos] == '\r') {
		p.pos++
	}
}

func (p *wktParser) parseGeom() (geom, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] >= 'A' && p.s[p.pos] <= 'Z' || p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z') {
		p.pos++
	}
	kw := strings.ToUpper(p.s[start:p.pos])
	p.skipSpace()
	// Optional Z / M / ZM dimension tag — coordinates carry their own arity.
	if p.pos < len(p.s) && (p.s[p.pos] == 'Z' || p.s[p.pos] == 'M' || p.s[p.pos] == 'z' || p.s[p.pos] == 'm') {
		for p.pos < len(p.s) && p.s[p.pos] != '(' {
			p.pos++
		}
	}
	p.skipSpace()

	switch kw {
	case "POINT":
		pts, err := p.parseCoordList()
		if err != nil {
			return geom{}, err
		}
		if len(pts) != 1 {
			return geom{}, fmt.Errorf("wkt: POINT needs exactly one coordinate")
		}
		return geom{typ: "Point", coord: pts[0]}, nil
	case "LINESTRING":
		pts, err := p.parseCoordList()
		if err != nil {
			return geom{}, err
		}
		return geom{typ: "LineString", line: pts}, nil
	case "MULTIPOINT":
		pts, err := p.parseMultiPoint()
		if err != nil {
			return geom{}, err
		}
		return geom{typ: "MultiPoint", line: pts}, nil
	case "POLYGON":
		rings, err := p.parseRingList()
		if err != nil {
			return geom{}, err
		}
		return geom{typ: "Polygon", poly: rings}, nil
	case "MULTILINESTRING":
		lines, err := p.parseRingList()
		if err != nil {
			return geom{}, err
		}
		return geom{typ: "MultiLineString", poly: lines}, nil
	case "MULTIPOLYGON":
		polys, err := p.parsePolyList()
		if err != nil {
			return geom{}, err
		}
		return geom{typ: "MultiPolygon", multi: polys}, nil
	case "GEOMETRYCOLLECTION":
		return p.parseCollection()
	default:
		return geom{}, fmt.Errorf("wkt: unsupported geometry %q", kw)
	}
}

func (p *wktParser) expect(c byte) error {
	p.skipSpace()
	if p.pos >= len(p.s) || p.s[p.pos] != c {
		return fmt.Errorf("wkt: expected %q at offset %d", string(c), p.pos)
	}
	p.pos++
	return nil
}

func (p *wktParser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// parseCoordList parses `(x y[, x y]...)`.
func (p *wktParser) parseCoordList() ([][]float64, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var out [][]float64
	for {
		c, err := p.parseCoord()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		break
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *wktParser) parseCoord() ([]float64, error) {
	p.skipSpace()
	// Some MULTIPOINT forms wrap each coord in parentheses.
	wrapped := false
	if p.peek() == '(' {
		p.pos++
		wrapped = true
	}
	var nums []float64
	for {
		p.skipSpace()
		start := p.pos
		for p.pos < len(p.s) {
			ch := p.s[p.pos]
			if ch == '-' || ch == '+' || ch == '.' || ch == 'e' || ch == 'E' || (ch >= '0' && ch <= '9') {
				p.pos++
				continue
			}
			break
		}
		if p.pos == start {
			break
		}
		f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("wkt: bad number %q", p.s[start:p.pos])
		}
		nums = append(nums, f)
		p.skipSpace()
		if p.pos < len(p.s) && p.s[p.pos] == ' ' {
			continue
		}
	}
	if wrapped {
		if err := p.expect(')'); err != nil {
			return nil, err
		}
	}
	if len(nums) < 2 {
		return nil, fmt.Errorf("wkt: coordinate needs at least 2 numbers")
	}
	if len(nums) > 3 {
		nums = nums[:3]
	}
	return nums, nil
}

func (p *wktParser) parseMultiPoint() ([][]float64, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var out [][]float64
	for {
		c, err := p.parseCoord()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		break
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return out, nil
}

// parseRingList parses `((x y, ...), (...))`.
func (p *wktParser) parseRingList() ([][][]float64, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var out [][][]float64
	for {
		ring, err := p.parseCoordList()
		if err != nil {
			return nil, err
		}
		out = append(out, ring)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		break
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return out, nil
}

// parsePolyList parses `(((...)), ((...)))`.
func (p *wktParser) parsePolyList() ([][][][]float64, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var out [][][][]float64
	for {
		poly, err := p.parseRingList()
		if err != nil {
			return nil, err
		}
		out = append(out, poly)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		break
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *wktParser) parseCollection() (geom, error) {
	if err := p.expect('('); err != nil {
		return geom{}, err
	}
	var subs []geom
	for {
		g, err := p.parseGeom()
		if err != nil {
			return geom{}, err
		}
		subs = append(subs, g)
		if p.peek() == ',' {
			p.pos++
			continue
		}
		break
	}
	if err := p.expect(')'); err != nil {
		return geom{}, err
	}
	return geom{typ: "GeometryCollection", geoms: subs}, nil
}

func geomToWKT(g geom) (string, error) {
	switch g.typ {
	case "Point":
		return "POINT (" + coordWKT(g.coord) + ")", nil
	case "LineString":
		return "LINESTRING " + lineWKT(g.line), nil
	case "MultiPoint":
		return "MULTIPOINT " + lineWKT(g.line), nil
	case "Polygon":
		return "POLYGON " + polyWKT(g.poly), nil
	case "MultiLineString":
		return "MULTILINESTRING " + polyWKT(g.poly), nil
	case "MultiPolygon":
		parts := make([]string, len(g.multi))
		for i, p := range g.multi {
			parts[i] = polyWKT(p)
		}
		return "MULTIPOLYGON (" + strings.Join(parts, ", ") + ")", nil
	case "GeometryCollection":
		parts := make([]string, len(g.geoms))
		for i, sub := range g.geoms {
			w, err := geomToWKT(sub)
			if err != nil {
				return "", err
			}
			parts[i] = w
		}
		return "GEOMETRYCOLLECTION (" + strings.Join(parts, ", ") + ")", nil
	default:
		return "", fmt.Errorf("wkt: cannot encode geometry type %q", g.typ)
	}
}
