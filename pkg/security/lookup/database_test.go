package lookup_test

import (
	"database/sql"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

func TestFromDatabase(t *testing.T) {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()

	gdb, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		db   common.Database
		same *sql.DB // expected handle; nil = just must be non-nil
	}{
		"pgsql": {database.NewPgSQLAdapter(sqldb, "sqlite"), sqldb},
		"bun":   {database.NewBunAdapter(bun.NewDB(sqldb, sqlitedialect.New())), sqldb},
		"gorm":  {database.NewGormAdapter(gdb), nil},
	}
	for name, c := range cases {
		got, dialectName, err := lookup.FromDatabase(c.db)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got == nil || (c.same != nil && got != c.same) {
			t.Errorf("%s: unexpected *sql.DB %v", name, got)
		}
		if dialectName != "sqlite" {
			t.Errorf("%s: dialect = %q, want sqlite", name, dialectName)
		}
		if err := got.Ping(); err != nil {
			t.Errorf("%s: handle not usable: %v", name, err)
		}
	}
}

func TestFromDatabaseRejects(t *testing.T) {
	if _, _, err := lookup.FromDatabase(nil); err == nil {
		t.Error("nil database should fail")
	}
	// A database that does not expose a *sql.DB (the embedded interface is nil; only the type matters).
	if _, _, err := lookup.FromDatabase(struct{ common.Database }{}); err == nil {
		t.Error("adapter without SQLDB should fail")
	}
}

func TestResolveDialect(t *testing.T) {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()

	d, err := lookup.Config{}.ResolveDialect(sqldb)
	if err != nil || d.Name() != "sqlite" {
		t.Errorf("detected = %v, %v", d, err)
	}
	d, err = lookup.Config{Dialect: "mysql"}.ResolveDialect(sqldb)
	if err != nil || d.Name() != "mysql" {
		t.Errorf("explicit dialect should win: %v, %v", d, err)
	}
	if _, err := (lookup.Config{Dialect: "oracle"}).Resolve(); err == nil {
		t.Error("unknown dialect should fail Resolve")
	}
}
