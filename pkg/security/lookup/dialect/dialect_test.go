package dialect_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
)

func get(t *testing.T, name string) dialect.Dialect {
	t.Helper()
	d, err := dialect.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRegistryHasBuiltins(t *testing.T) {
	got := strings.Join(dialect.Names(), ",")
	if got != "mssql,mysql,postgres,sqlite" {
		t.Errorf("names = %s", got)
	}
	if _, err := dialect.Get("oracle"); err == nil {
		t.Error("unknown dialect should error")
	}
}

func TestRegisterCustomDialect(t *testing.T) {
	// A new database is added by registering a dialect; no core change needed.
	dialect.Register("custom", func() dialect.Dialect { return customDialect{get(t, "sqlite")} })
	d, err := dialect.Get("CUSTOM")
	if err != nil || d.Name() != "custom" {
		t.Fatalf("got %v, %v", d, err)
	}
	if got, _ := dialect.DetectDriver("example.com/driver.customdriver"); got == nil || got.Name() != "custom" {
		t.Errorf("custom detection failed: %v", got)
	}
}

type customDialect struct{ dialect.Dialect }

func (customDialect) Name() string               { return "custom" }
func (customDialect) Matches(driver string) bool { return strings.Contains(driver, "customdriver") }

func TestPlaceholderAndQuote(t *testing.T) {
	cases := []struct {
		name, ph1, ph3, quote, quoteDotted, quoteEscape string
	}{
		{"postgres", "$1", "$3", `"users"`, `"auth"."users"`, `"a""b"`},
		{"sqlite", "?", "?", `"users"`, `"auth"."users"`, `"a""b"`},
		{"mysql", "?", "?", "`users`", "`auth`.`users`", "`a``b`"},
		{"mssql", "@p1", "@p3", "[users]", "[auth].[users]", "[a]]b]"},
	}
	for _, c := range cases {
		d := get(t, c.name)
		if d.Placeholder(1) != c.ph1 || d.Placeholder(3) != c.ph3 {
			t.Errorf("%s placeholders: %s %s", c.name, d.Placeholder(1), d.Placeholder(3))
		}
		if d.Quote("users") != c.quote {
			t.Errorf("%s quote: %s", c.name, d.Quote("users"))
		}
		if d.Quote("auth.users") != c.quoteDotted {
			t.Errorf("%s dotted: %s", c.name, d.Quote("auth.users"))
		}
		raw := map[string]string{"postgres": `a"b`, "sqlite": `a"b`, "mysql": "a`b", "mssql": "a]b"}[c.name]
		q := c.quoteEscape
		if got := d.Quote(raw); got != q {
			t.Errorf("%s escape: got %s want %s", c.name, got, q)
		}
	}
}

func TestInsertReturningID(t *testing.T) {
	cols := []string{"user_id", "name"}
	cases := []struct {
		name     string
		wantSQL  string
		strategy dialect.InsertStrategy
		empty    string
	}{
		{"postgres", `INSERT INTO "t" ("user_id", "name") VALUES ($1, $2) RETURNING "id"`, dialect.ReturningQuery, `INSERT INTO "t" DEFAULT VALUES RETURNING "id"`},
		{"sqlite", `INSERT INTO "t" ("user_id", "name") VALUES (?, ?)`, dialect.LastInsertID, `INSERT INTO "t" DEFAULT VALUES`},
		{"mysql", "INSERT INTO `t` (`user_id`, `name`) VALUES (?, ?)", dialect.LastInsertID, "INSERT INTO `t` () VALUES ()"},
		{"mssql", `INSERT INTO [t] ([user_id], [name]) OUTPUT INSERTED.[id] VALUES (@p1, @p2)`, dialect.ReturningQuery, `INSERT INTO [t] OUTPUT INSERTED.[id] DEFAULT VALUES`},
	}
	for _, c := range cases {
		ins := get(t, c.name).InsertReturningID("t", cols, "id")
		if ins.SQL != c.wantSQL || ins.Strategy != c.strategy {
			t.Errorf("%s: got %q (%d)", c.name, ins.SQL, ins.Strategy)
		}
		if e := get(t, c.name).InsertReturningID("t", nil, "id"); e.SQL != c.empty {
			t.Errorf("%s empty: got %q", c.name, e.SQL)
		}
	}
}

func TestBoolRoundTrip(t *testing.T) {
	for _, name := range dialect.Names() {
		if name == "custom" {
			continue
		}
		d := get(t, name)
		for _, v := range []bool{true, false} {
			got, err := d.ScanBool(d.Bool(v))
			if err != nil || got != v {
				t.Errorf("%s: Bool(%v) round trip = %v, %v", name, v, got, err)
			}
		}
		for _, c := range []struct {
			in   any
			want bool
		}{{int64(1), true}, {int64(0), false}, {"1", true}, {"false", false}, {[]byte("t"), true}, {nil, false}} {
			if got, err := d.ScanBool(c.in); err != nil || got != c.want {
				t.Errorf("%s: ScanBool(%v) = %v, %v", name, c.in, got, err)
			}
		}
		if _, err := d.ScanBool("maybe"); err == nil {
			t.Errorf("%s: ScanBool(maybe) should fail", name)
		}
	}
}

func TestScanTime(t *testing.T) {
	d := get(t, "sqlite")
	want := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, in := range []any{
		want,
		"2026-03-04 05:06:07+00:00",
		"2026-03-04T05:06:07Z",
		"2026-03-04 05:06:07",
		[]byte("2026-03-04 05:06:07"),
		"2026-03-04 05:06:07 +0000 UTC",
	} {
		got, err := d.ScanTime(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("ScanTime(%v) = %v, %v", in, got, err)
		}
	}
	if got, err := d.ScanTime(nil); err != nil || !got.IsZero() {
		t.Errorf("ScanTime(nil) = %v, %v", got, err)
	}
	if _, err := d.ScanTime("not a time"); err == nil {
		t.Error("ScanTime(garbage) should fail")
	}
}

func TestJSON(t *testing.T) {
	for _, name := range []string{"postgres", "sqlite", "mysql", "mssql"} {
		d := get(t, name)
		enc, err := d.EncodeJSON([]string{"a", "b"})
		if err != nil || enc != `["a","b"]` {
			t.Errorf("%s encode = %v, %v", name, enc, err)
		}
		var nilSlice []string
		var nilMap map[string]any
		for _, v := range []any{nil, nilSlice, nilMap} {
			if enc, err := d.EncodeJSON(v); err != nil || enc != nil {
				t.Errorf("%s: EncodeJSON(%T nil) = %v, %v; want SQL NULL", name, v, enc, err)
			}
		}
		var out []string
		if err := d.DecodeJSON([]byte(`["x"]`), &out); err != nil || len(out) != 1 || out[0] != "x" {
			t.Errorf("%s decode bytes = %v, %v", name, out, err)
		}
		out = []string{"keep"}
		if err := d.DecodeJSON(nil, &out); err != nil || out[0] != "keep" {
			t.Errorf("%s decode NULL should leave dst: %v, %v", name, out, err)
		}
		if err := d.DecodeJSON("", &out); err != nil || out[0] != "keep" {
			t.Errorf("%s decode empty should leave dst: %v, %v", name, out, err)
		}
		if err := d.DecodeJSON("{bad", &out); err == nil {
			t.Errorf("%s decode of invalid json should fail", name)
		}
	}
}

func TestDetectDriver(t *testing.T) {
	cases := map[string]string{
		"github.com/jackc/pgx/v5/stdlib.driver":      "postgres",
		"github.com/lib/pq.driver":                   "postgres",
		"github.com/mattn/go-sqlite3.sqlitedriver":   "sqlite",
		"modernc.org/sqlite.driver":                  "sqlite",
		"github.com/go-sql-driver/mysql.mysqldriver": "mysql",
		"github.com/microsoft/go-mssqldb.driver":     "mssql",
		"github.com/denisenkom/go-mssqldb.driver":    "mssql",
	}
	for drv, want := range cases {
		d, err := dialect.DetectDriver(drv)
		if err != nil || d.Name() != want {
			t.Errorf("%s: got %v, %v; want %s", drv, d, err, want)
		}
	}
	if _, err := dialect.DetectDriver("example.com/unknown.driver"); err == nil {
		t.Error("unknown driver should fail with an explicit-dialect hint")
	}
	if _, err := dialect.Detect(nil); err == nil {
		t.Error("nil db should fail")
	}
}

// TestSQLiteRoundTrip runs the sqlite dialect against a real in-memory database.
func TestSQLiteRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	d, err := dialect.Detect(db)
	if err != nil || d.Name() != "sqlite" {
		t.Fatalf("Detect = %v, %v", d, err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, active BOOLEAN, scopes TEXT, at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	scopes, _ := d.EncodeJSON([]string{"read", "write"})
	ins := d.InsertReturningID("t", []string{"name", "active", "scopes", "at"}, "id")
	id, err := ins.Run(ctx, db, "k1", d.Bool(true), scopes, now)
	if err != nil || id != 1 {
		t.Fatalf("insert = %d, %v", id, err)
	}
	id, err = ins.Run(ctx, db, "k2", d.Bool(false), nil, now)
	if err != nil || id != 2 {
		t.Fatalf("second insert = %d, %v", id, err)
	}

	q := "SELECT active, scopes, at FROM " + d.Quote("t") + " WHERE " + d.Quote("id") + " = " + d.Placeholder(1)
	var active, scopesRaw, at any
	if err := db.QueryRowContext(ctx, q, 1).Scan(&active, &scopesRaw, &at); err != nil {
		t.Fatal(err)
	}
	if b, err := d.ScanBool(active); err != nil || !b {
		t.Errorf("active = %v, %v", b, err)
	}
	var got []string
	if err := d.DecodeJSON(scopesRaw, &got); err != nil || len(got) != 2 {
		t.Errorf("scopes = %v, %v", got, err)
	}
	if ts, err := d.ScanTime(at); err != nil || !ts.Equal(now) {
		t.Errorf("at = %v (%T), %v", ts, at, err)
	}

	if err := db.QueryRowContext(ctx, q, 2).Scan(&active, &scopesRaw, &at); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.ScanBool(active); b {
		t.Error("second row should be inactive")
	}
	if scopesRaw != nil {
		t.Errorf("nil scopes should be NULL, got %v", scopesRaw)
	}

	// Insert inside a transaction goes through the same Querier.
	tx, _ := db.BeginTx(ctx, nil)
	if id, err := d.InsertReturningID("t", nil, "id").Run(ctx, tx); err != nil || id != 3 {
		t.Errorf("DEFAULT VALUES insert in tx = %d, %v", id, err)
	}
	_ = tx.Rollback()
}
