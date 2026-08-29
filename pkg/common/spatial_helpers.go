package common

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// This file implements PostGIS spatial and pgvector similarity filter operators.
// The builders return parameterised SQL fragments (with `?` placeholders) plus
// their args, matching the style of BuildInCondition / BuildArrayOverlapCondition.
// PostgreSQL only — on other databases these operators simply will not resolve.

// ── vector similarity ───────────────────────────────────────────────────────

// VectorOperator maps a metric name to its pgvector distance operator.
//
//	"l2" / "euclidean" / ""  -> <->
//	"cosine"                 -> <=>
//	"ip" / "inner" / "dot"   -> <#>
func VectorOperator(metric string) string {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "cosine", "cos":
		return "<=>"
	case "ip", "inner", "dot", "innerproduct", "inner_product":
		return "<#>"
	default:
		return "<->"
	}
}

// VectorLiteral converts a vector value into a pgvector literal string
// "[1,2,3]". Accepts []float32, []float64, []int, []any (of numbers), or an
// already-formatted string.
func VectorLiteral(value any) (string, error) {
	switch v := value.(type) {
	case string:
		s := strings.TrimSpace(v)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			return s, nil
		}
		return "", fmt.Errorf("vector literal: malformed string %q", v)
	case []float32:
		return floatsToVectorLiteral(len(v), func(i int) float64 { return float64(v[i]) }), nil
	case []float64:
		return floatsToVectorLiteral(len(v), func(i int) float64 { return v[i] }), nil
	case []int:
		return floatsToVectorLiteral(len(v), func(i int) float64 { return float64(v[i]) }), nil
	case []any:
		nums := make([]float64, len(v))
		for i, e := range v {
			f, ok := toFloat(e)
			if !ok {
				return "", fmt.Errorf("vector literal: element %d is not a number (%T)", i, e)
			}
			nums[i] = f
		}
		return floatsToVectorLiteral(len(nums), func(i int) float64 { return nums[i] }), nil
	default:
		return "", fmt.Errorf("vector literal: unsupported type %T", value)
	}
}

func floatsToVectorLiteral(n int, at func(int) float64) string {
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = strconv.FormatFloat(at(i), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// BuildVectorCondition builds a pgvector distance-threshold filter.
//
//	operator: "l2_within" | "cosine_within" | "ip_within"
//	value:    {"vector": [...], "distance": <n>}
//	          {"vector": [...], "lt"|"lte"|"gt"|"gte": <n>}
//
// Produces e.g. `embedding <=> ? < ?` with args [vectorLiteral, threshold].
func BuildVectorCondition(column, operator string, value any) (query string, args []interface{}, ok bool) {
	var op string
	switch strings.ToLower(operator) {
	case "l2_within", "l2distance_within", "euclidean_within":
		op = "<->"
	case "cosine_within", "cosinedistance_within":
		op = "<=>"
	case "ip_within", "inner_within", "negativeinnerproduct_within":
		op = "<#>"
	default:
		return "", nil, false
	}

	m, mok := value.(map[string]any)
	if !mok {
		return "", nil, false
	}
	lit, err := VectorLiteral(m["vector"])
	if err != nil {
		return "", nil, false
	}

	cmp := "<"
	var threshold any
	if t, ok := m["distance"]; ok {
		threshold = t
	} else {
		for _, k := range []string{"lt", "lte", "gt", "gte"} {
			if t, ok := m[k]; ok {
				threshold = t
				switch k {
				case "lt":
					cmp = "<"
				case "lte":
					cmp = "<="
				case "gt":
					cmp = ">"
				case "gte":
					cmp = ">="
				}
				break
			}
		}
	}
	if threshold == nil {
		return "", nil, false
	}
	f, fok := toFloat(threshold)
	if !fok {
		return "", nil, false
	}

	return fmt.Sprintf("%s %s ? %s ?", column, op, cmp), []interface{}{lit, f}, true
}

// ── PostGIS spatial ─────────────────────────────────────────────────────────

var spatialPredicates = map[string]string{
	"st_intersects": "ST_Intersects",
	"st_contains":   "ST_Contains",
	"st_within":     "ST_Within",
	"st_covers":     "ST_Covers",
	"st_coveredby":  "ST_CoveredBy",
	"st_overlaps":   "ST_Overlaps",
	"st_touches":    "ST_Touches",
	"st_crosses":    "ST_Crosses",
	"st_equals":     "ST_Equals",
	"st_disjoint":   "ST_Disjoint",
}

// BuildSpatialCondition builds a PostGIS spatial filter.
//
//	"st_dwithin"    value: {"geom": <geojson|ewkt|hex>, "distance": <n>}
//	"st_intersects" / "st_contains" / "st_within" / "st_covers" /
//	"st_coveredby" / "st_overlaps" / "st_touches" / "st_crosses" /
//	"st_equals" / "st_disjoint"     value: <geojson|ewkt|hex>
//	"bbox" (alias "&&")             value: <geom> or {"bbox":[minx,miny,maxx,maxy],"srid":4326}
func BuildSpatialCondition(column, operator string, value any) (query string, args []interface{}, ok bool) {
	operator = strings.ToLower(strings.TrimSpace(operator))

	if fn, isPred := spatialPredicates[operator]; isPred {
		expr, arg, err := geomArgExpr(value)
		if err != nil {
			return "", nil, false
		}
		return fmt.Sprintf("%s(%s, %s)", fn, column, expr), []interface{}{arg}, true
	}

	switch operator {
	case "st_dwithin":
		m, mok := value.(map[string]any)
		if !mok {
			return "", nil, false
		}
		expr, arg, err := geomArgExpr(m["geom"])
		if err != nil {
			return "", nil, false
		}
		dist, dok := toFloat(m["distance"])
		if !dok {
			return "", nil, false
		}
		return fmt.Sprintf("ST_DWithin(%s, %s, ?)", column, expr), []interface{}{arg, dist}, true

	case "bbox", "&&":
		if m, mok := value.(map[string]any); mok {
			if bboxRaw, has := m["bbox"]; has {
				coords, cok := toFloatSlice(bboxRaw)
				if !cok || len(coords) != 4 {
					return "", nil, false
				}
				srid := 4326
				if s, sok := toFloat(m["srid"]); sok {
					srid = int(s)
				}
				return fmt.Sprintf("%s && ST_MakeEnvelope(?, ?, ?, ?, ?)", column),
					[]interface{}{coords[0], coords[1], coords[2], coords[3], srid}, true
			}
			// fall through: treat the map as a GeoJSON geometry
		}
		expr, arg, err := geomArgExpr(value)
		if err != nil {
			return "", nil, false
		}
		return fmt.Sprintf("%s && %s", column, expr), []interface{}{arg}, true
	}

	return "", nil, false
}

// geomArgExpr inspects a geometry value and returns the SQL placeholder
// expression that turns a bound argument into a geometry, plus that argument.
//
//	GeoJSON object -> "ST_GeomFromGeoJSON(?)", <json string>
//	hex EWKB       -> "?::geometry",            <hex string>
//	WKT / EWKT     -> "ST_GeomFromEWKT(?)",     <ewkt string>
func geomArgExpr(value any) (expr string, arg any, err error) {
	switch v := value.(type) {
	case nil:
		return "", nil, fmt.Errorf("geometry: nil value")
	case map[string]any:
		b, mErr := json.Marshal(v)
		if mErr != nil {
			return "", nil, mErr
		}
		return "ST_GeomFromGeoJSON(?)", string(b), nil
	case json.RawMessage:
		return "ST_GeomFromGeoJSON(?)", string(v), nil
	case []byte:
		return geomArgExpr(string(v))
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return "", nil, fmt.Errorf("geometry: empty value")
		}
		if strings.HasPrefix(s, "{") {
			return "ST_GeomFromGeoJSON(?)", s, nil
		}
		if isHexString(s) {
			return "?::geometry", s, nil
		}
		return "ST_GeomFromEWKT(?)", s, nil
	default:
		return "", nil, fmt.Errorf("geometry: unsupported type %T", value)
	}
}

func isHexString(s string) bool {
	if len(s) < 10 || len(s)%2 != 0 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toFloatSlice(v any) ([]float64, bool) {
	switch s := v.(type) {
	case []float64:
		return s, true
	case []any:
		out := make([]float64, len(s))
		for i, e := range s {
			f, ok := toFloat(e)
			if !ok {
				return nil, false
			}
			out[i] = f
		}
		return out, true
	default:
		return nil, false
	}
}

// IsSpatialOperator reports whether op is a spatial filter operator handled by
// BuildSpatialCondition.
func IsSpatialOperator(op string) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	if _, ok := spatialPredicates[op]; ok {
		return true
	}
	return op == "st_dwithin" || op == "bbox" || op == "&&"
}

// IsVectorOperator reports whether op is a vector similarity filter operator
// handled by BuildVectorCondition.
func IsVectorOperator(op string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "l2_within", "l2distance_within", "euclidean_within",
		"cosine_within", "cosinedistance_within",
		"ip_within", "inner_within", "negativeinnerproduct_within":
		return true
	default:
		return false
	}
}
