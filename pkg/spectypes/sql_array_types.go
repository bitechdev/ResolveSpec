package spectypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// parsePostgresArrayElements parses a PostgreSQL array literal (e.g. `{a,"b,c",d}`)
// into a slice of raw string elements. Each element retains its unquoted/unescaped value.
func parsePostgresArrayElements(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "null") || strings.EqualFold(s, "NULL") {
		return nil, nil
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil, fmt.Errorf("not a valid PostgreSQL array literal: %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return []string{}, nil
	}

	var result []string
	var cur strings.Builder
	inQuotes := false
	i := 0
	for i < len(inner) {
		c := inner[i]
		switch {
		case c == '"' && !inQuotes:
			inQuotes = true
		case c == '"' && inQuotes:
			if i+1 < len(inner) && inner[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQuotes = false
			}
		case c == '\\' && inQuotes:
			if i+1 < len(inner) {
				cur.WriteByte(inner[i+1])
				i++
			}
		case c == ',' && !inQuotes:
			result = append(result, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
		i++
	}
	result = append(result, cur.String())
	return result, nil
}

// formatPostgresStringArray formats a []string back into a PostgreSQL array literal.
func formatPostgresStringArray(vals []string) string {
	if vals == nil {
		return "NULL"
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		// Quote if value contains comma, double-quote, backslash, braces, whitespace, or is empty.
		needsQuote := v == "" || strings.ContainsAny(v, `,"\\{}` + "\t\n\r ")
		if needsQuote {
			v = strings.ReplaceAll(v, `\`, `\\`)
			v = strings.ReplaceAll(v, `"`, `""`)
			parts[i] = `"` + v + `"`
		} else {
			parts[i] = v
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ── SqlStringArray ───────────────────────────────────────────────────────────

// SqlStringArray is a nullable PostgreSQL text[] / varchar[] array.
type SqlStringArray struct {
	Val   []string
	Valid bool
}

func (a *SqlStringArray) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlStringArray: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = elems
	a.Valid = true
	return nil
}

func (a SqlStringArray) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	return formatPostgresStringArray(a.Val), nil
}

func (a SqlStringArray) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlStringArray) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []string
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlStringArray(v []string) SqlStringArray {
	return SqlStringArray{Val: v, Valid: true}
}

// ── SqlInt16Array ────────────────────────────────────────────────────────────

type SqlInt16Array struct {
	Val   []int16
	Valid bool
}

func (a *SqlInt16Array) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlInt16Array: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]int16, len(elems))
	for i, e := range elems {
		n, err := strconv.ParseInt(strings.TrimSpace(e), 10, 16)
		if err != nil {
			return fmt.Errorf("SqlInt16Array: element %d %q: %w", i, e, err)
		}
		a.Val[i] = int16(n)
	}
	a.Valid = true
	return nil
}

func (a SqlInt16Array) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlInt16Array) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlInt16Array) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []int16
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlInt16Array(v []int16) SqlInt16Array {
	return SqlInt16Array{Val: v, Valid: true}
}

// ── SqlInt32Array ────────────────────────────────────────────────────────────

type SqlInt32Array struct {
	Val   []int32
	Valid bool
}

func (a *SqlInt32Array) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlInt32Array: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]int32, len(elems))
	for i, e := range elems {
		n, err := strconv.ParseInt(strings.TrimSpace(e), 10, 32)
		if err != nil {
			return fmt.Errorf("SqlInt32Array: element %d %q: %w", i, e, err)
		}
		a.Val[i] = int32(n)
	}
	a.Valid = true
	return nil
}

func (a SqlInt32Array) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlInt32Array) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlInt32Array) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []int32
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlInt32Array(v []int32) SqlInt32Array {
	return SqlInt32Array{Val: v, Valid: true}
}

// ── SqlInt64Array ────────────────────────────────────────────────────────────

type SqlInt64Array struct {
	Val   []int64
	Valid bool
}

func (a *SqlInt64Array) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlInt64Array: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]int64, len(elems))
	for i, e := range elems {
		n, err := strconv.ParseInt(strings.TrimSpace(e), 10, 64)
		if err != nil {
			return fmt.Errorf("SqlInt64Array: element %d %q: %w", i, e, err)
		}
		a.Val[i] = n
	}
	a.Valid = true
	return nil
}

func (a SqlInt64Array) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlInt64Array) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlInt64Array) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []int64
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlInt64Array(v []int64) SqlInt64Array {
	return SqlInt64Array{Val: v, Valid: true}
}

// ── SqlFloat32Array ──────────────────────────────────────────────────────────

type SqlFloat32Array struct {
	Val   []float32
	Valid bool
}

func (a *SqlFloat32Array) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlFloat32Array: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]float32, len(elems))
	for i, e := range elems {
		f, err := strconv.ParseFloat(strings.TrimSpace(e), 32)
		if err != nil {
			return fmt.Errorf("SqlFloat32Array: element %d %q: %w", i, e, err)
		}
		a.Val[i] = float32(f)
	}
	a.Valid = true
	return nil
}

func (a SqlFloat32Array) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlFloat32Array) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlFloat32Array) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []float32
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlFloat32Array(v []float32) SqlFloat32Array {
	return SqlFloat32Array{Val: v, Valid: true}
}

// ── SqlFloat64Array ──────────────────────────────────────────────────────────

type SqlFloat64Array struct {
	Val   []float64
	Valid bool
}

func (a *SqlFloat64Array) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlFloat64Array: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]float64, len(elems))
	for i, e := range elems {
		f, err := strconv.ParseFloat(strings.TrimSpace(e), 64)
		if err != nil {
			return fmt.Errorf("SqlFloat64Array: element %d %q: %w", i, e, err)
		}
		a.Val[i] = f
	}
	a.Valid = true
	return nil
}

func (a SqlFloat64Array) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlFloat64Array) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlFloat64Array) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []float64
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlFloat64Array(v []float64) SqlFloat64Array {
	return SqlFloat64Array{Val: v, Valid: true}
}

// ── SqlBoolArray ─────────────────────────────────────────────────────────────

type SqlBoolArray struct {
	Val   []bool
	Valid bool
}

func (a *SqlBoolArray) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlBoolArray: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]bool, len(elems))
	for i, e := range elems {
		e = strings.ToLower(strings.TrimSpace(e))
		a.Val[i] = e == "t" || e == "true" || e == "1" || e == "yes"
	}
	a.Valid = true
	return nil
}

func (a SqlBoolArray) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		if v {
			parts[i] = "t"
		} else {
			parts[i] = "f"
		}
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlBoolArray) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlBoolArray) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []bool
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlBoolArray(v []bool) SqlBoolArray {
	return SqlBoolArray{Val: v, Valid: true}
}

// ── SqlUUIDArray ─────────────────────────────────────────────────────────────

type SqlUUIDArray struct {
	Val   []uuid.UUID
	Valid bool
}

func (a *SqlUUIDArray) Scan(value any) error {
	if value == nil {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("SqlUUIDArray: cannot scan type %T", value)
	}
	elems, err := parsePostgresArrayElements(s)
	if err != nil {
		return err
	}
	a.Val = make([]uuid.UUID, len(elems))
	for i, e := range elems {
		u, err := uuid.Parse(strings.TrimSpace(e))
		if err != nil {
			return fmt.Errorf("SqlUUIDArray: element %d %q: %w", i, e, err)
		}
		a.Val[i] = u
	}
	a.Valid = true
	return nil
}

func (a SqlUUIDArray) Value() (driver.Value, error) {
	if !a.Valid {
		return nil, nil
	}
	parts := make([]string, len(a.Val))
	for i, v := range a.Val {
		parts[i] = v.String()
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a SqlUUIDArray) MarshalJSON() ([]byte, error) {
	if !a.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(a.Val)
}

func (a *SqlUUIDArray) UnmarshalJSON(b []byte) error {
	if strings.TrimSpace(string(b)) == "null" {
		a.Valid = false
		a.Val = nil
		return nil
	}
	var vals []uuid.UUID
	if err := json.Unmarshal(b, &vals); err != nil {
		return err
	}
	a.Val = vals
	a.Valid = true
	return nil
}

func NewSqlUUIDArray(v []uuid.UUID) SqlUUIDArray {
	return SqlUUIDArray{Val: v, Valid: true}
}

// ── SqlVector ────────────────────────────────────────────────────────────────

// SqlVector is a nullable pgvector `vector` type backed by []float32.
// Wire format: `[1.0,2.0,3.0]` (square brackets, comma-separated floats).
type SqlVector struct {
	Val   []float32
	Valid bool
}

func (v *SqlVector) Scan(value any) error {
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
		return fmt.Errorf("SqlVector: cannot scan type %T", value)
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return fmt.Errorf("SqlVector: not a valid vector literal: %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		v.Val = []float32{}
		v.Valid = true
		return nil
	}
	parts := strings.Split(inner, ",")
	v.Val = make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("SqlVector: element %d %q: %w", i, p, err)
		}
		v.Val[i] = float32(f)
	}
	v.Valid = true
	return nil
}

func (v SqlVector) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	parts := make([]string, len(v.Val))
	for i, f := range v.Val {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func (v SqlVector) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(v.Val)
}

func (v *SqlVector) UnmarshalJSON(b []byte) error {
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

func NewSqlVector(val []float32) SqlVector {
	return SqlVector{Val: val, Valid: true}
}
