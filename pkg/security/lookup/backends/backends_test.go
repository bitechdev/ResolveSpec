package backends

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/glebarez/go-sqlite"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/ddl"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

func sqliteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ddl, err := ddl.SQL("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLiteDefaultsToDirect(t *testing.T) {
	ctx := context.Background()
	p, err := New(sqliteDB(t), lookup.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := p.Auth.Register(ctx, sectypes.RegisterRequest{Username: "a", Email: "a@x.io", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Auth.Session(ctx, reg.Token, "authenticate"); err != nil {
		t.Fatal(err)
	}
	if on, err := p.TOTP.Status(ctx, reg.User.UserID); err != nil || on {
		t.Fatalf("%v %v", on, err)
	}
}

func TestProcedureModeRejectedOnSQLite(t *testing.T) {
	_, err := New(sqliteDB(t), lookup.Config{Overrides: map[lookup.Op]lookup.Mode{lookup.OpLogin: lookup.ModeProcedure}}, Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCustomSchemaAndUnknownDialect(t *testing.T) {
	if _, err := New(sqliteDB(t), lookup.Config{Dialect: "nosuch"}, Options{}); err == nil {
		t.Fatal("unknown dialect accepted")
	}
	bad := lookup.Config{Schema: lookup.Schema{lookup.EntityUsers: {Name: "x; drop"}}}
	if _, err := New(sqliteDB(t), bad, Options{}); err == nil {
		t.Fatal("unsafe schema accepted")
	}
	if _, err := New(nil, lookup.Config{}, Options{}); err == nil {
		t.Fatal("nil db accepted")
	}
}

func TestPostgresDefaultsToProcedure(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	p, err := New(db, lookup.Config{Dialect: lookup.DialectPostgres}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("resolvespec_totp_get_status").WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_enabled"}).AddRow(true, nil, true))
	if on, err := p.TOTP.Status(context.Background(), 1); err != nil || !on {
		t.Fatalf("%v %v", on, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAutoProbesOnce(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	p, err := New(db, lookup.Config{Dialect: lookup.DialectPostgres, Mode: lookup.ModeAuto}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("pg_proc").WithArgs("resolvespec_totp_get_status").
		WillReturnRows(sqlmock.NewRows([]string{"e"}).AddRow(true))
	for i := 0; i < 2; i++ { // second call must reuse the cached probe
		mock.ExpectQuery("resolvespec_totp_get_status").WithArgs(1).
			WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_enabled"}).AddRow(true, nil, true))
		if on, err := p.TOTP.Status(context.Background(), 1); err != nil || !on {
			t.Fatalf("%v %v", on, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailed(t *testing.T) {
	p := Failed(errors.New("boom"))
	if _, err := p.Auth.Login(context.Background(), sectypes.LoginRequest{}); err == nil || err.Error() != "boom" {
		t.Fatalf("got %v", err)
	}
	if _, err := p.Policy.RowSecurity(context.Background(), 1, "s", "t"); err == nil {
		t.Fatal("want error")
	}
}
