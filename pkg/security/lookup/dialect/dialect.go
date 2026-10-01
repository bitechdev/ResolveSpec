// Package dialect holds the per-database adaptors used by the lookup direct backend.
// An adaptor supplies only what differs between databases (placeholders, quoting,
// booleans, time and JSON handling, insert-returning-id); the backend builds queries from it.
// It imports only the standard library.
package dialect

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dialect is the adaptor for one database type.
type Dialect interface {
	// Name is the registry name, e.g. "postgres".
	Name() string
	// Matches reports whether a driver type identifier (lowercased "<pkgpath>.<Type>")
	// belongs to this database. Used by Detect.
	Matches(driver string) bool
	// Placeholder returns the bind placeholder for the n-th (1-based) argument.
	Placeholder(n int) string
	// Quote quotes an identifier. A dotted name is quoted per part ("schema.table").
	// Embedded quote characters are escaped, never interpolated raw.
	Quote(ident string) string
	// Bool converts a Go bool to the value bound as a boolean column argument.
	Bool(v bool) any
	// ScanBool reads a boolean column value that the driver returned as bool, integer, string or bytes.
	ScanBool(src any) (bool, error)
	// ScanTime reads a time column value that the driver returned as time.Time, string or bytes.
	// NULL (nil) yields the zero time.
	ScanTime(src any) (time.Time, error)
	// EncodeJSON converts a value to the argument bound to a JSON/TEXT column. A nil
	// value, map or slice yields nil (SQL NULL).
	EncodeJSON(v any) (any, error)
	// DecodeJSON reads a JSON/TEXT column value into dst. NULL and empty values leave dst untouched.
	DecodeJSON(src any, dst any) error
	// InsertReturningID builds an INSERT of cols into table and describes how to read the new id.
	// Arguments are bound positionally in cols order.
	InsertReturningID(table string, cols []string, idCol string) Insert
}

// InsertStrategy tells how the generated id is read after an Insert.
type InsertStrategy int

const (
	// ReturningQuery means the statement returns the id as a single row (QueryRow + Scan).
	ReturningQuery InsertStrategy = iota
	// LastInsertID means the id is read from sql.Result.LastInsertId after Exec.
	LastInsertID
)

// Insert is a generated INSERT statement and how to read the id it creates.
type Insert struct {
	SQL      string
	Strategy InsertStrategy
}

// Querier is implemented by *sql.DB, *sql.Tx and *sql.Conn.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Run executes the insert and returns the generated id.
func (i Insert) Run(ctx context.Context, q Querier, args ...any) (int64, error) {
	switch i.Strategy {
	case ReturningQuery:
		var id int64
		if err := q.QueryRowContext(ctx, i.SQL, args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	case LastInsertID:
		res, err := q.ExecContext(ctx, i.SQL, args...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	return 0, fmt.Errorf("dialect: unknown insert strategy %d", i.Strategy)
}

// Factory creates a Dialect.
type Factory func() Dialect

var (
	regMu    sync.RWMutex
	registry = map[string]Factory{}
)

// Register adds a dialect under name. Registering a name twice replaces it, so applications
// can override a built-in. Adding a database = implementing Dialect and calling Register.
func Register(name string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[strings.ToLower(name)] = f
}

// Get returns the dialect registered under name.
func Get(name string) (Dialect, error) {
	regMu.RLock()
	f, ok := registry[strings.ToLower(name)]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("dialect: unknown dialect %q (registered: %s)", name, strings.Join(Names(), ", "))
	}
	return f(), nil
}

// Names lists the registered dialect names, sorted.
func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Detect picks the dialect for db from its driver type.
func Detect(db *sql.DB) (Dialect, error) {
	if db == nil {
		return nil, fmt.Errorf("dialect: nil database")
	}
	return DetectDriver(driverID(db.Driver()))
}

// DetectDriver picks the dialect for a driver type identifier (see Dialect.Matches).
func DetectDriver(driver string) (Dialect, error) {
	driver = strings.ToLower(driver)
	for _, n := range Names() {
		d, _ := Get(n)
		if d != nil && d.Matches(driver) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("dialect: cannot detect a dialect for driver %q; set the dialect explicitly", driver)
}

// driverID builds "<pkgpath>.<Type>" for a driver value, lowercased.
func driverID(drv any) string {
	t := reflect.TypeOf(drv)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return strings.ToLower(t.PkgPath() + "." + t.Name())
}

func init() {
	Register("postgres", func() Dialect { return postgres{} })
	Register("sqlite", func() Dialect { return sqlite{} })
	Register("mysql", func() Dialect { return mysql{} })
	Register("mssql", func() Dialect { return mssql{} })
}

// --- shared helpers -------------------------------------------------------

// quoteWith quotes each dotted part of ident with open/close, doubling embedded close characters.
func quoteWith(ident, open, closeq string) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		parts[i] = open + strings.ReplaceAll(p, closeq, closeq+closeq) + closeq
	}
	return strings.Join(parts, ".")
}

func scanBool(src any) (bool, error) {
	switch v := src.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case int64:
		return v != 0, nil
	case int:
		return v != 0, nil
	case int32:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case []byte:
		return scanBool(string(v))
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "t", "true", "y", "yes", "on":
			return true, nil
		case "", "0", "f", "false", "n", "no", "off":
			return false, nil
		}
	}
	return false, fmt.Errorf("dialect: cannot read %T (%v) as bool", src, src)
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

func scanTime(src any) (time.Time, error) {
	switch v := src.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return v, nil
	case []byte:
		return scanTime(string(v))
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return time.Time{}, nil
		}
		// Some drivers append the Go monotonic/zone suffix ("+0000 UTC"); drop it.
		if i := strings.Index(s, " m="); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSuffix(s, " UTC")
		s = strings.TrimSuffix(s, " +0000 +0000")
		for _, l := range timeLayouts {
			if t, err := time.Parse(l, s); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("dialect: cannot read %T (%v) as time", src, src)
}

func encodeJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("dialect: encode json: %w", err)
	}
	return string(b), nil
}

func decodeJSON(src any, dst any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("dialect: cannot read %T as json", src)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("dialect: decode json: %w", err)
	}
	return nil
}

// insertSQL assembles "INSERT INTO t (cols) <mid> VALUES (...) <tail>" for a dialect.
func insertSQL(d Dialect, table string, cols []string, mid, tail, defaults string) string {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(d.Quote(table))
	if len(cols) == 0 {
		if mid != "" {
			b.WriteString(" " + mid)
		}
		b.WriteString(" " + defaults)
		if tail != "" {
			b.WriteString(" " + tail)
		}
		return b.String()
	}
	qc := make([]string, len(cols))
	ph := make([]string, len(cols))
	for i, c := range cols {
		qc[i] = d.Quote(c)
		ph[i] = d.Placeholder(i + 1)
	}
	b.WriteString(" (" + strings.Join(qc, ", ") + ")")
	if mid != "" {
		b.WriteString(" " + mid)
	}
	b.WriteString(" VALUES (" + strings.Join(ph, ", ") + ")")
	if tail != "" {
		b.WriteString(" " + tail)
	}
	return b.String()
}
