package backends

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/conformance"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/ddl"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
)

// Every backend/dialect runs the same suite. SQLite runs always. The others run only when a
// DSN is set, and only against a database you are happy to add rows to (all names the suite
// creates carry a unique "cf<hex>_" prefix and are removed afterwards):
//
//	RESOLVESPEC_TEST_PG_DSN         Postgres with the procedure schema installed
//	                                (lookup/database_schema.sql + keystore_schema.sql): procedure mode
//	RESOLVESPEC_TEST_PG_DIRECT_DSN  Postgres for direct mode; ddl/postgres.sql is applied if the
//	                                tables are missing. Do not point it at the procedure schema:
//	                                the column types differ.
//	RESOLVESPEC_TEST_MYSQL_DSN      MySQL (needs a "mysql" database/sql driver linked into the test binary)
//	RESOLVESPEC_TEST_MSSQL_DSN      SQL Server (driver "sqlserver")
func TestConformance(t *testing.T) {
	t.Run("sqlite/direct", func(t *testing.T) {
		runConformance(t, sqliteDB(t), "sqlite", lookup.Config{Mode: lookup.ModeDirect}, false)
	})
	t.Run("sqlite/default", func(t *testing.T) {
		runConformance(t, sqliteDB(t), "sqlite", lookup.Config{}, false)
	})

	real := []struct {
		name, env, driver, dialect string
		cfg                        lookup.Config
		applyDDL                   bool
	}{
		{"postgres/procedure", "RESOLVESPEC_TEST_PG_DSN", "pgx", "postgres", lookup.Config{Mode: lookup.ModeProcedure}, false},
		{"postgres/direct", "RESOLVESPEC_TEST_PG_DIRECT_DSN", "pgx", "postgres", lookup.Config{Mode: lookup.ModeDirect}, true},
		{"mysql/direct", "RESOLVESPEC_TEST_MYSQL_DSN", "mysql", "mysql", lookup.Config{}, true},
		{"mssql/direct", "RESOLVESPEC_TEST_MSSQL_DSN", "sqlserver", "mssql", lookup.Config{}, true},
	}
	for _, r := range real {
		t.Run(r.name, func(t *testing.T) {
			dsn := os.Getenv(r.env)
			if dsn == "" {
				t.Skipf("%s not set", r.env)
			}
			runOnServer(t, r.driver, dsn, r.dialect, r.cfg, r.applyDDL)
		})
	}
}

// runOnServer opens dsn, optionally applies the reference DDL, and runs the suite.
func runOnServer(t *testing.T, driver, dsn, dialectName string, cfg lookup.Config, applyDDL bool) {
	t.Helper()
	if !slices.Contains(sql.Drivers(), driver) {
		t.Skipf("database/sql driver %q is not linked into this test binary", driver)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if applyDDL {
		stmts, err := ddl.Statements(dialectName)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range stmts {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("apply ddl: %v\n%s", err, s)
			}
		}
	}
	runConformance(t, db, dialectName, cfg, true)
}

func runConformance(t *testing.T, db *sql.DB, dialectName string, cfg lookup.Config, shared bool) {
	t.Helper()
	d, err := dialect.Get(dialectName)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Dialect = dialectName
	p, err := New(db, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var b [3]byte
	_, _ = rand.Read(b[:])
	env := conformance.Env{Provider: p, DB: db, Dialect: d, Prefix: "cf" + hex.EncodeToString(b[:]) + "_"}
	if shared {
		env.Cleanup = func(t *testing.T) { cleanup(t, db, d, env.Prefix) }
	}
	conformance.Run(t, env)
}

// cleanup deletes the rows a conformance run created, by prefix. Child rows go with their user
// through the foreign keys; tables without one are cleaned explicitly.
func cleanup(t *testing.T, db *sql.DB, d dialect.Dialect, prefix string) {
	t.Helper()
	like := prefix + "%"
	for _, q := range []struct{ table, col string }{
		{"oauth_codes", "code"},
		{"oauth_clients", "client_id"},
		{"token_blacklist", "token"},
		{"sec_column_rules", "schema_name"},
		{"sec_row_rules", "schema_name"},
		{"users", "username"},
	} {
		if _, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE %s LIKE %s", q.table, q.col, d.Placeholder(1)), like); err != nil {
			t.Logf("cleanup %s: %v", q.table, err)
		}
	}
}
