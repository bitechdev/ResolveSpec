package security

import (
	"context"
	"errors"
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
