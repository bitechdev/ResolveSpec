package lookup_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
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
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.tenant', .*decode\('`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`set_config\('app\.user_id', .*decode\('`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := pool.RunInTransaction(context.Background(), func(tx common.Database) error {
		return lookup.ApplyTxSettings(ctx, tx, map[string]string{"app.user_id": "7", "app.tenant": "o'x?"})
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
	ctx := context.Background()
	var seen string
	mock.ExpectBegin()
	mock.ExpectExec(`set_config`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	_ = pool.RunInTransaction(context.Background(), func(tx common.Database) error {
		// Capture via a wrapper so the raw statement can be inspected.
		return lookup.ApplyTxSettings(ctx, &queryRecorder{Database: tx, got: &seen}, map[string]string{"app.v": "'; DROP TABLE x; --?"})
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
	ctx := context.Background()

	for _, name := range []string{"user_id", "app.x'); DROP", "app..x", "app.x y"} {
		if err := lookup.ApplyTxSettings(ctx, pool, map[string]string{name: "1"}); err == nil {
			t.Fatalf("name %q must be rejected", name)
		}
	}
	if err := lookup.ApplyTxSettings(ctx, pool, nil); err != nil {
		t.Fatalf("empty settings must be a no-op: %v", err)
	}
	if err := lookup.ApplyTxSettings(ctx, &driverStub{Database: pool, name: "sqlite"}, map[string]string{"app.x": "1"}); err == nil {
		t.Fatal("non-postgres driver must fail closed")
	}
}

type driverStub struct {
	common.Database
	name string
}

func (d *driverStub) DriverName() string { return d.name }
