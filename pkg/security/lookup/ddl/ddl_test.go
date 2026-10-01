package ddl_test

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/ddl"
)

func TestStatementsAllDialects(t *testing.T) {
	for _, d := range []string{"postgres", "sqlite", "mysql", "mssql"} {
		st, err := ddl.Statements(d)
		if err != nil {
			t.Fatal(err)
		}
		if len(st) < 12 {
			t.Errorf("%s: %d statements", d, len(st))
		}
		all := strings.Join(st, "\n")
		for _, tbl := range []string{"users", "user_sessions", "user_keys", "oauth_codes", "sec_group_members", "sec_column_rules", "sec_row_rules"} {
			if !strings.Contains(all, tbl+" (") {
				t.Errorf("%s: missing table %s", d, tbl)
			}
		}
	}
	if _, err := ddl.SQL("oracle"); err == nil {
		t.Error("unknown dialect must error")
	}
}

// Every table and column the default schema names must exist in the sqlite reference DDL.
func TestSQLiteMatchesDefaultSchema(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s, _ := ddl.SQL("sqlite")
	if _, err := db.Exec(s); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(s); err != nil {
		t.Fatalf("script must be re-runnable: %v", err)
	}
	sc := lookup.DefaultSchema()
	for _, tbl := range sc {
		for _, c := range tbl.Columns {
			if _, err := db.Exec("SELECT " + c + " FROM " + tbl.Name + " WHERE 1=0"); err != nil {
				t.Errorf("%s.%s: %v", tbl.Name, c, err)
			}
		}
	}
}
