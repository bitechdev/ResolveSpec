package security

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
)

func txSettingsDB(t *testing.T) (common.Database, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return database.NewPgSQLAdapter(db), mock
}

func TestApplyTxSettingsStampsInNameOrderOnTx(t *testing.T) {
	pool, mock := txSettingsDB(t)
	sc := &mockSecurityContext{ctx: context.Background()}

	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.tenant', .*decode\('`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`set_config\('app\.user_id', .*decode\('`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := pool.RunInTransaction(context.Background(), func(tx common.Database) error {
		return ApplyTxSettings(sc, tx, map[string]string{"app.user_id": "7", "app.tenant": "o'x?"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyTxSettingsValueIsNeverInlined(t *testing.T) {
	pool, mock := txSettingsDB(t)
	sc := &mockSecurityContext{ctx: context.Background()}
	var seen string
	mock.ExpectBegin()
	mock.ExpectExec(`set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	_ = pool.RunInTransaction(context.Background(), func(tx common.Database) error {
		// Capture via a wrapper so the raw statement can be inspected.
		return ApplyTxSettings(sc, &queryRecorder{Database: tx, got: &seen}, map[string]string{"app.v": "'; DROP TABLE x; --?"})
	})
	if strings.Contains(seen, "DROP") || strings.Contains(seen, "?") {
		t.Fatalf("value leaked into SQL text: %s", seen)
	}
}

type queryRecorder struct {
	common.Database
	got *string
}

func (q *queryRecorder) Exec(ctx context.Context, query string, args ...interface{}) (common.Result, error) {
	*q.got = query
	return q.Database.Exec(ctx, query, args...)
}

func TestApplyTxSettingsRejectsBadNameAndDriver(t *testing.T) {
	pool, _ := txSettingsDB(t)
	sc := &mockSecurityContext{ctx: context.Background()}

	for _, name := range []string{"user_id", "app.x'); DROP", "app..x", "app.x y"} {
		if err := ApplyTxSettings(sc, pool, map[string]string{name: "1"}); err == nil {
			t.Fatalf("name %q must be rejected", name)
		}
	}
	if err := ApplyTxSettings(sc, pool, nil); err != nil {
		t.Fatalf("empty settings must be a no-op: %v", err)
	}
	if err := ApplyTxSettings(sc, &driverStub{Database: pool, name: "sqlite"}, map[string]string{"app.x": "1"}); err == nil {
		t.Fatal("non-postgres driver must fail closed")
	}
}

type driverStub struct {
	common.Database
	name string
}

func (d *driverStub) DriverName() string { return d.name }

func TestStampTxSettingsUsesConfiguredFunc(t *testing.T) {
	pool, mock := txSettingsDB(t)
	sc := &mockSecurityContext{ctx: context.Background()}
	list, err := NewSecurityList(&mockSecurityProvider{})
	if err != nil {
		t.Fatal(err)
	}

	// Nil list and unset func are no-ops (no SQL expected).
	if err := StampTxSettings(sc, nil, pool); err != nil {
		t.Fatal(err)
	}
	if err := StampTxSettings(sc, list, pool); err != nil {
		t.Fatal(err)
	}

	list.SetTxSettings(func(SecurityContext) (map[string]string, error) {
		return map[string]string{"app.user_id": "7"}, nil
	})
	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.user_id'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := pool.RunInTransaction(context.Background(), func(tx common.Database) error {
		return StampTxSettings(sc, list, tx)
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("no tenant")
	list.SetTxSettings(func(SecurityContext) (map[string]string, error) { return nil, boom })
	if err := StampTxSettings(sc, list, pool); !errors.Is(err, boom) {
		t.Fatalf("func error must propagate, got %v", err)
	}
}
