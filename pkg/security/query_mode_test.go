package security

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"
)

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestShouldUseProcedure_ModeProcedure_AlwaysTrue(t *testing.T) {
	db := openTestSQLite(t)
	c := newDBCapability()
	if !c.ShouldUseProcedure(context.Background(), ModeProcedure, db, "resolvespec_login") {
		t.Error("ModeProcedure should always resolve to true")
	}
}

func TestShouldUseProcedure_ModeDirect_AlwaysFalse(t *testing.T) {
	db := openTestSQLite(t)
	c := newDBCapability()
	if c.ShouldUseProcedure(context.Background(), ModeDirect, db, "resolvespec_login") {
		t.Error("ModeDirect should always resolve to false")
	}
}

func TestShouldUseProcedure_Auto_SQLite_ResolvesDirect(t *testing.T) {
	db := openTestSQLite(t)
	c := newDBCapability()
	if c.ShouldUseProcedure(context.Background(), ModeAuto, db, "resolvespec_login") {
		t.Error("ModeAuto against a SQLite connection should resolve to Direct (false)")
	}
}

func TestShouldUseProcedure_Auto_UnknownDriver_DefaultsToProcedure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	c := newDBCapability()
	if !c.ShouldUseProcedure(context.Background(), ModeAuto, db, "resolvespec_login") {
		t.Error("ModeAuto against an unrecognized driver (e.g. a test double) should default to Procedure (true) to preserve existing behavior")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestShouldUseProcedure_CachesResult(t *testing.T) {
	db := openTestSQLite(t)
	c := newDBCapability()
	first := c.ShouldUseProcedure(context.Background(), ModeAuto, db, "resolvespec_login")
	if _, ok := c.funcExists.Load("resolvespec_login"); !ok {
		t.Error("expected result to be cached")
	}
	second := c.ShouldUseProcedure(context.Background(), ModeAuto, db, "resolvespec_login")
	if first != second {
		t.Errorf("cached result changed: first=%v second=%v", first, second)
	}
}

func TestDBCapability_Reset_ClearsCache(t *testing.T) {
	c := newDBCapability()
	c.funcExists.Store("resolvespec_login", true)
	c.reset()
	if _, ok := c.funcExists.Load("resolvespec_login"); ok {
		t.Error("reset should clear all cached entries")
	}
}

func TestProbeFunctionExists_Postgres_FunctionExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_proc WHERE proname = \$1 LIMIT 1\)`).
		WithArgs("resolvespec_login").
		WillReturnRows(rows)

	if !probeFunctionExists(context.Background(), db, "resolvespec_login") {
		t.Error("expected probeFunctionExists to return true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestProbeFunctionExists_Postgres_FunctionMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(false)
	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_proc WHERE proname = \$1 LIMIT 1\)`).
		WithArgs("resolvespec_login").
		WillReturnRows(rows)

	if probeFunctionExists(context.Background(), db, "resolvespec_login") {
		t.Error("expected probeFunctionExists to return false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestProbeFunctionExists_QueryError_ReturnsFalse(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM pg_proc WHERE proname = \$1 LIMIT 1\)`).
		WithArgs("resolvespec_login").
		WillReturnError(sql.ErrConnDone)

	if probeFunctionExists(context.Background(), db, "resolvespec_login") {
		t.Error("expected probeFunctionExists to return false on query error")
	}
}

func TestProbeFunctionExists_NilDB(t *testing.T) {
	if probeFunctionExists(context.Background(), nil, "resolvespec_login") {
		t.Error("expected probeFunctionExists(nil) to return false")
	}
}

func TestRewritePlaceholders_NonPostgres_NoOp(t *testing.T) {
	db := openTestSQLite(t)
	q := "SELECT * FROM users WHERE id = ? AND name = ?"
	if got := rewritePlaceholders(db, q); got != q {
		t.Errorf("rewritePlaceholders on non-Postgres = %q, want unchanged %q", got, q)
	}
}

func TestGenerateSessionToken_Format(t *testing.T) {
	token, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken() error = %v", err)
	}
	if len(token) < len("sess_")+64+2 {
		t.Errorf("generateSessionToken() = %q, unexpected length", token)
	}
	if token[:5] != "sess_" {
		t.Errorf("generateSessionToken() = %q, want prefix 'sess_'", token)
	}
}
