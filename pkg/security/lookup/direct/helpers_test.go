package direct

import (
	"database/sql"
	"testing"

	_ "github.com/glebarez/go-sqlite"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/ddl"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/procedure"
)

func newTestDB(t *testing.T, extraDDL ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ref, err := ddl.SQL("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append([]string{ref}, extraDDL...) {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	return db
}

func newTestBase(t *testing.T, db *sql.DB, schema lookup.Schema) *Base {
	t.Helper()
	d, err := dialect.Detect(db)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBase(procedure.NewDB(db, nil, nil), d, schema)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
