// Package common provides nullable SQL types with automatic casting and conversion methods.
package common

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// tryParseDT attempts to parse a string into a time.Time using various formats.
func tryParseDT(str string) (time.Time, error) {
	var lasterror error
	tryFormats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000",
		"06-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"02/01/2006",
		"02-01-2006",
		"2006-01-02",
		"15:04:05.000",
		"15:04:05",
		"15:04",
	}
	for _, f := range tryFormats {
		tx, err := time.Parse(f, str)
		if err == nil {
			return tx, nil
		}
		lasterror = err
	}
	return time.Time{}, lasterror // Return zero time on failure
}

// ToJSONDT formats a time.Time to RFC3339 string.
func ToJSONDT(dt time.Time) string {
	return dt.Format(time.RFC3339)
}

// SqlNull is a generic nullable type that behaves like sql.NullXXX with auto-casting.
type SqlNull[T any] struct {
	Val   T
	Valid bool
}

// Scan implements sql.Scanner.
func (n *SqlNull[T]) Scan(value any) error {
	if value == nil {
		n.Valid = false
		n.Val = *new(T)
		return nil
	}

	// Try standard sql.Null[T] first.
	var sqlNull sql.Null[T]
	if err := sqlNull.Scan(value); err == nil {
		n.Val = sqlNull.V
		n.Valid = sqlNull.Valid
		return nil
	}

	// Fallback: parse from string/bytes.
	switch v := value.(type) {
	case string:
		return n.fromString(v)
	case []byte:
		return n.fromString(string(v))
	default:
		return n.fromString(fmt.Sprintf("%v", value))
	}
}
func (n *SqlNull[T]) fromString(s string) error {
	s = strings.TrimSpace(s)
	n.Valid = false
	n.Val = *new(T)

	if s == "" || strings.EqualFold(s, "null") {
		return nil
	}

	var zero T
	switch any(zero).(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			reflect.ValueOf(&n.Val).Elem().SetInt(i)
			n.Valid = true
		}
	case float32, float64:
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			reflect.ValueOf(&n.Val).Elem().SetFloat(f)
			n.Valid = true
		}
	case bool:
		if b, err := strconv.ParseBool(s); err == nil {
			n.Val = any(b).(T)
			n.Valid = true
		}
	case time.Time:
		if t, err := tryParseDT(s); err == nil && !t.IsZero() {
			n.Val = any(t).(T)
			n.Valid = true
		}
	case uuid.UUID:
		if u, err := uuid.Parse(s); err == nil {
			n.Val = any(u).(T)
			n.Valid = true
		}
	case string:
		n.Val = any(s).(T)
		n.Valid = true
	}
	return nil
}

// Value implements driver.Valuer.
func (n SqlNull[T]) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return any(n.Val), nil
}

// MarshalJSON implements json.Marshaler.
func (n SqlNull[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Val)
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *SqlNull[T]) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" || strings.TrimSpace(string(b)) == "" {
		n.Valid = false
		n.Val = *new(T)
		return nil
	}

	// Try direct unmarshal.
	var val T
	if err := json.Unmarshal(b, &val); err == nil {
		n.Val = val
		n.Valid = true
		return nil
	}

	// Fallback: unmarshal as string and parse.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return n.fromString(s)
	}

	return fmt.Errorf("cannot unmarshal %s into SqlNull[%T]", b, n.Val)
}

// String implements fmt.Stringer.
func (n SqlNull[T]) String() string {
	if !n.Valid {
		return ""
	}
	return fmt.Sprintf("%v", n.Val)
}

// Int64 converts to int64 or 0 if invalid.
func (n SqlNull[T]) Int64() int64 {
	if !n.Valid {
		return 0
	}
	v := reflect.ValueOf(any(n.Val))
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint())
	case reflect.Float32, reflect.Float64:
		return int64(v.Float())
	case reflect.String:
		i, _ := strconv.ParseInt(v.String(), 10, 64)
		return i
	case reflect.Bool:
		if v.Bool() {
			return 1
		}
		return 0
	}
	return 0
}

// Float64 converts to float64 or 0.0 if invalid.
func (n SqlNull[T]) Float64() float64 {
	if !n.Valid {
		return 0.0
	}
	v := reflect.ValueOf(any(n.Val))
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint())
	case reflect.String:
		f, _ := strconv.ParseFloat(v.String(), 64)
		return f
	}
	return 0.0
}

// Bool converts to bool or false if invalid.
func (n SqlNull[T]) Bool() bool {
	if !n.Valid {
		return false
	}
	v := reflect.ValueOf(any(n.Val))
	if v.Kind() == reflect.Bool {
		return v.Bool()
	}
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(n.Val)))
	return s == "true" || s == "t" || s == "1" || s == "yes" || s == "on"
}

// Time converts to time.Time or zero if invalid.
func (n SqlNull[T]) Time() time.Time {
	if !n.Valid {
		return time.Time{}
	}
	if t, ok := any(n.Val).(time.Time); ok {
		return t
	}
	return time.Time{}
}

// UUID converts to uuid.UUID or Nil if invalid.
func (n SqlNull[T]) UUID() uuid.UUID {
	if !n.Valid {
		return uuid.Nil
	}
	if u, ok := any(n.Val).(uuid.UUID); ok {
		return u
	}
	return uuid.Nil
}

// Type aliases for common types.
type (
	SqlInt16   = SqlNull[int16]
	SqlInt32   = SqlNull[int32]
	SqlInt64   = SqlNull[int64]
	SqlFloat64 = SqlNull[float64]
	SqlBool    = SqlNull[bool]
	SqlString  = SqlNull[string]
	SqlUUID    = SqlNull[uuid.UUID]
)

// SqlTimeStamp - Timestamp with custom formatting (YYYY-MM-DDTHH:MM:SS).
type SqlTimeStamp struct{ SqlNull[time.Time] }

func (t SqlTimeStamp) MarshalJSON() ([]byte, error) {
	if !t.Valid || t.Val.IsZero() || t.Val.Before(time.Date(0002, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, t.Val.Format("2006-01-02T15:04:05"))), nil
}

func (t *SqlTimeStamp) UnmarshalJSON(b []byte) error {
	if err := t.SqlNull.UnmarshalJSON(b); err != nil {
		return err
	}
	if t.Valid && (t.Val.IsZero() || t.Val.Format("2006-01-02T15:04:05") == "0001-01-01T00:00:00") {
		t.Valid = false
	}
	return nil
}

func (t SqlTimeStamp) Value() (driver.Value, error) {
	if !t.Valid || t.Val.IsZero() || t.Val.Before(time.Date(0002, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return nil, nil
	}
	return t.Val.Format("2006-01-02T15:04:05"), nil
}

func SqlTimeStampNow() SqlTimeStamp {
	return SqlTimeStamp{SqlNull: SqlNull[time.Time]{Val: time.Now(), Valid: true}}
}

// SqlDate - Date only (YYYY-MM-DD).
type SqlDate struct{ SqlNull[time.Time] }

func (d SqlDate) MarshalJSON() ([]byte, error) {
	if !d.Valid || d.Val.IsZero() {
		return []byte("null"), nil
	}
	s := d.Val.Format("2006-01-02")
	if strings.HasPrefix(s, "0001-01-01") {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, s)), nil
}

func (d *SqlDate) UnmarshalJSON(b []byte) error {
	if err := d.SqlNull.UnmarshalJSON(b); err != nil {
		return err
	}
	if d.Valid && d.Val.Format("2006-01-02") <= "0001-01-01" {
		d.Valid = false
	}
	return nil
}

func (d SqlDate) Value() (driver.Value, error) {
	if !d.Valid || d.Val.IsZero() {
		return nil, nil
	}
	s := d.Val.Format("2006-01-02")
	if s <= "0001-01-01" {
		return nil, nil
	}
	return s, nil
}

func (d SqlDate) String() string {
	if !d.Valid {
		return ""
	}
	s := d.Val.Format("2006-01-02")
	if strings.HasPrefix(s, "0001-01-01") || strings.HasPrefix(s, "1800-12-31") {
		return ""
	}
	return s
}

func SqlDateNow() SqlDate {
	return SqlDate{SqlNull: SqlNull[time.Time]{Val: time.Now(), Valid: true}}
}

// SqlTime - Time only (HH:MM:SS).
type SqlTime struct{ SqlNull[time.Time] }

func (t SqlTime) MarshalJSON() ([]byte, error) {
	if !t.Valid || t.Val.IsZero() {
		return []byte("null"), nil
	}
	s := t.Val.Format("15:04:05")
	if s == "00:00:00" {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, s)), nil
}

func (t *SqlTime) UnmarshalJSON(b []byte) error {
	if err := t.SqlNull.UnmarshalJSON(b); err != nil {
		return err
	}
	if t.Valid && t.Val.Format("15:04:05") == "00:00:00" {
		t.Valid = false
	}
	return nil
}

func (t SqlTime) Value() (driver.Value, error) {
	if !t.Valid || t.Val.IsZero() {
		return nil, nil
	}
	return t.Val.Format("15:04:05"), nil
}

func (t SqlTime) String() string {
	if !t.Valid {
		return ""
	}
	return t.Val.Format("15:04:05")
}

func SqlTimeNow() SqlTime {
	return SqlTime{SqlNull: SqlNull[time.Time]{Val: time.Now(), Valid: true}}
}

// SqlJSONB - Nullable JSONB as []byte.
type SqlJSONB []byte

// Scan implements sql.Scanner.
func (n *SqlJSONB) Scan(value any) error {
	if value == nil {
		*n = nil
		return nil
	}
	switch v := value.(type) {
	case string:
		*n = []byte(v)
	case []byte:
		*n = v
	default:
		dat, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value to JSON: %v", err)
		}
		*n = dat
	}
	return nil
}

// Value implements driver.Valuer.
func (n SqlJSONB) Value() (driver.Value, error) {
	if len(n) == 0 {
		return nil, nil
	}
	var js any
	if err := json.Unmarshal(n, &js); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return string(n), nil
}

// MarshalJSON implements json.Marshaler.
func (n SqlJSONB) MarshalJSON() ([]byte, error) {
	if len(n) == 0 {
		return []byte("null"), nil
	}
	var obj any
	if err := json.Unmarshal(n, &obj); err != nil {
		return []byte("null"), nil
	}
	return n, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *SqlJSONB) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" || (!strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[")) {
		*n = nil
		return nil
	}
	*n = b
	return nil
}

func (n SqlJSONB) AsMap() (map[string]any, error) {
	if len(n) == 0 {
		return nil, nil
	}
	js := make(map[string]any)
	if err := json.Unmarshal(n, &js); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return js, nil
}

func (n SqlJSONB) AsSlice() ([]any, error) {
	if len(n) == 0 {
		return nil, nil
	}
	js := make([]any, 0)
	if err := json.Unmarshal(n, &js); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return js, nil
}

// TryIfInt64 tries to parse any value to int64 with default.
func TryIfInt64(v any, def int64) int64 {
	switch val := v.(type) {
	case string:
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return def
		}
		return i
	case int:
		return int64(val)
	case int8:
		return int64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case uint:
		return int64(val)
	case uint8:
		return int64(val)
	case uint16:
		return int64(val)
	case uint32:
		return int64(val)
	case uint64:
		return int64(val)
	case float32:
		return int64(val)
	case float64:
		return int64(val)
	case []byte:
		i, err := strconv.ParseInt(string(val), 10, 64)
		if err != nil {
			return def
		}
		return i
	default:
		return def
	}
}

// Constructor helpers - clean and fast value creation
func Null[T any](v T, valid bool) SqlNull[T] {
	return SqlNull[T]{Val: v, Valid: valid}
}

func NewSqlInt16(v int16) SqlInt16 {
	return SqlInt16{Val: v, Valid: true}
}

func NewSqlInt32(v int32) SqlInt32 {
	return SqlInt32{Val: v, Valid: true}
}

func NewSqlInt64(v int64) SqlInt64 {
	return SqlInt64{Val: v, Valid: true}
}

func NewSqlFloat64(v float64) SqlFloat64 {
	return SqlFloat64{Val: v, Valid: true}
}

func NewSqlBool(v bool) SqlBool {
	return SqlBool{Val: v, Valid: true}
}

func NewSqlString(v string) SqlString {
	return SqlString{Val: v, Valid: true}
}

func NewSqlUUID(v uuid.UUID) SqlUUID {
	return SqlUUID{Val: v, Valid: true}
}

func NewSqlTimeStamp(v time.Time) SqlTimeStamp {
	return SqlTimeStamp{SqlNull: SqlNull[time.Time]{Val: v, Valid: true}}
}

func NewSqlDate(v time.Time) SqlDate {
	return SqlDate{SqlNull: SqlNull[time.Time]{Val: v, Valid: true}}
}

func NewSqlTime(v time.Time) SqlTime {
	return SqlTime{SqlNull: SqlNull[time.Time]{Val: v, Valid: true}}
}
