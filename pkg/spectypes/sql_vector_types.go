package spectypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// pgvector column types beyond the plain `vector` (SqlVector, in
// sql_array_types.go): `halfvec`, `sparsevec` and `bit`.

// parseVectorLiteral parses a pgvector dense literal `[1,2,3]` into []float32.
func parseVectorLiteral(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("not a valid vector literal: %q", s)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []float32{}, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("vector element %d %q: %w", i, p, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}

func formatVectorLiteral(vals []float32) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// ── SqlHalfVector ────────────────────────────────────────────────────────────

// SqlHalfVector is a nullable pgvector `halfvec` (half-precision) column, backed
// by []float32. Wire format matches `vector`: `[1,2,3]`.
type SqlHalfVector struct {
	Val   []float32
	Valid bool
}

func (v *SqlHalfVector) Scan(value any) error {
	if value == nil {
		v.Valid = false
		v.Val = nil
		return nil
	}
	var s string
	switch val := value.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	default:
		return fmt.Errorf("SqlHalfVector: cannot scan type %T", value)
	}
	parsed, err := parseVectorLiteral(s)
	if err != nil {
		return fmt.Errorf("SqlHalfVector: %w", err)
	}
	v.Val = parsed
	v.Valid = true
	return nil
}

func (v SqlHalfVector) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	return formatVectorLiteral(v.Val), nil
}

func (v SqlHalfVector) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(v.Val)
}

func (v *SqlHalfVector) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		v.Valid = false
		v.Val = nil
		return nil
	}
	var vals []float32
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	v.Val = vals
	v.Valid = true
	return nil
}

func NewSqlHalfVector(val []float32) SqlHalfVector {
	return SqlHalfVector{Val: val, Valid: true}
}

// ── SqlSparseVector ──────────────────────────────────────────────────────────

// SqlSparseVector is a nullable pgvector `sparsevec` column. Wire format:
// `{1:0.5,4:0.2}/8` (1-based indices). JSON:
// `{"dim":8,"indices":[1,4],"values":[0.5,0.2]}`.
type SqlSparseVector struct {
	Dim     int
	Indices []int32
	Values  []float32
	Valid   bool
}

func (v *SqlSparseVector) Scan(value any) error {
	if value == nil {
		v.Valid = false
		v.Dim, v.Indices, v.Values = 0, nil, nil
		return nil
	}
	var s string
	switch val := value.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	default:
		return fmt.Errorf("SqlSparseVector: cannot scan type %T", value)
	}
	s = strings.TrimSpace(s)
	slash := strings.LastIndex(s, "/")
	if !strings.HasPrefix(s, "{") || slash < 0 || !strings.Contains(s[:slash], "}") {
		return fmt.Errorf("SqlSparseVector: invalid literal %q", s)
	}
	dim, err := strconv.Atoi(strings.TrimSpace(s[slash+1:]))
	if err != nil {
		return fmt.Errorf("SqlSparseVector: bad dimension: %w", err)
	}
	body := strings.TrimSpace(s[1:strings.LastIndex(s, "}")])
	var idx []int32
	var vals []float32
	if body != "" {
		for _, pair := range strings.Split(body, ",") {
			kv := strings.SplitN(pair, ":", 2)
			if len(kv) != 2 {
				return fmt.Errorf("SqlSparseVector: bad pair %q", pair)
			}
			k, err := strconv.Atoi(strings.TrimSpace(kv[0]))
			if err != nil {
				return fmt.Errorf("SqlSparseVector: bad index %q: %w", kv[0], err)
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 32)
			if err != nil {
				return fmt.Errorf("SqlSparseVector: bad value %q: %w", kv[1], err)
			}
			idx = append(idx, int32(k))
			vals = append(vals, float32(f))
		}
	}
	v.Dim, v.Indices, v.Values, v.Valid = dim, idx, vals, true
	return nil
}

func (v SqlSparseVector) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	pairs := make([]string, len(v.Indices))
	for i, k := range v.Indices {
		val := float32(0)
		if i < len(v.Values) {
			val = v.Values[i]
		}
		pairs[i] = strconv.Itoa(int(k)) + ":" + strconv.FormatFloat(float64(val), 'f', -1, 32)
	}
	return "{" + strings.Join(pairs, ",") + "}/" + strconv.Itoa(v.Dim), nil
}

type sparseVectorJSON struct {
	Dim     int       `json:"dim"`
	Indices []int32   `json:"indices"`
	Values  []float32 `json:"values"`
}

func (v SqlSparseVector) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(sparseVectorJSON{Dim: v.Dim, Indices: v.Indices, Values: v.Values})
}

func (v *SqlSparseVector) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		v.Valid = false
		v.Dim, v.Indices, v.Values = 0, nil, nil
		return nil
	}
	var j sparseVectorJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	v.Dim, v.Indices, v.Values, v.Valid = j.Dim, j.Indices, j.Values, true
	return nil
}

func NewSqlSparseVector(dim int, indices []int32, values []float32) SqlSparseVector {
	return SqlSparseVector{Dim: dim, Indices: indices, Values: values, Valid: true}
}

// ── SqlBitVector ─────────────────────────────────────────────────────────────

// SqlBitVector is a nullable Postgres `bit(n)` / `varbit` column (used by
// pgvector for Hamming/Jaccard distance), backed by []bool. Wire format: a
// string of '0'/'1' characters. JSON: a bool array.
type SqlBitVector struct {
	Val   []bool
	Valid bool
}

func (v *SqlBitVector) Scan(value any) error {
	if value == nil {
		v.Valid = false
		v.Val = nil
		return nil
	}
	var s string
	switch val := value.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	default:
		return fmt.Errorf("SqlBitVector: cannot scan type %T", value)
	}
	s = strings.TrimSpace(s)
	out := make([]bool, len(s))
	for i, c := range s {
		switch c {
		case '1':
			out[i] = true
		case '0':
			out[i] = false
		default:
			return fmt.Errorf("SqlBitVector: invalid bit %q", string(c))
		}
	}
	v.Val = out
	v.Valid = true
	return nil
}

func (v SqlBitVector) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	var b strings.Builder
	b.Grow(len(v.Val))
	for _, bit := range v.Val {
		if bit {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String(), nil
}

func (v SqlBitVector) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(v.Val)
}

func (v *SqlBitVector) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		v.Valid = false
		v.Val = nil
		return nil
	}
	// Accept both a bool array and a "0101" string.
	if strings.HasPrefix(s, "\"") {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		return v.Scan(str)
	}
	var vals []bool
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	v.Val = vals
	v.Valid = true
	return nil
}

func NewSqlBitVector(val []bool) SqlBitVector {
	return SqlBitVector{Val: val, Valid: true}
}
