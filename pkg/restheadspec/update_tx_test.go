package restheadspec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

func TestUpdateRefetchAndAfterUpdateRunInSecondTransaction(t *testing.T) {
	// The bun adapter builds model-based updates; the pgsql adapter does not.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	h := NewHandler(database.NewBunAdapter(bun.NewDB(sqlDB, pgdialect.New())), modelregistry.NewModelRegistry())
	var begins []common.Database
	var order []string
	var afterTx common.Database
	h.Hooks().Register(OnTxBegin, func(ctx *HookContext) error {
		begins = append(begins, ctx.Tx)
		order = append(order, "begin")
		return nil
	})
	h.Hooks().Register(AfterUpdate, func(ctx *HookContext) error {
		afterTx = ctx.Tx
		order = append(order, "after_update")
		return nil
	})

	cols := []string{"id", "name"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "b"))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx := WithSchema(base, "public")
	ctx = WithEntity(ctx, "items")
	ctx = WithTableName(ctx, "items")
	ctx = WithModel(ctx, delItem{})
	h.handleUpdate(ctx, w, "7", nil, map[string]interface{}{"name": "b"}, ExtendedRequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(begins) != 2 || begins[0] == begins[1] {
		t.Fatalf("OnTxBegin must fire once per transaction (2), got %d", len(begins))
	}
	if afterTx == nil || afterTx == h.db || afterTx != begins[1] {
		t.Fatalf("AfterUpdate must run on the second transaction")
	}
	if len(order) != 3 || order[2] != "after_update" {
		t.Fatalf("unexpected hook order %v", order)
	}
}

func TestAfterCreateRunsInSecondTransaction(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	h := NewHandler(database.NewBunAdapter(bun.NewDB(sqlDB, pgdialect.New())), modelregistry.NewModelRegistry())

	var begins []common.Database
	var afterTx common.Database
	h.Hooks().Register(OnTxBegin, func(ctx *HookContext) error {
		begins = append(begins, ctx.Tx)
		return nil
	})
	h.Hooks().Register(AfterCreate, func(ctx *HookContext) error {
		afterTx = ctx.Tx
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx := WithSchema(base, "public")
	ctx = WithEntity(ctx, "items")
	ctx = WithTableName(ctx, "items")
	ctx = WithModel(ctx, delItem{})
	h.handleCreate(ctx, w, map[string]interface{}{"name": "a"}, ExtendedRequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(begins) != 2 || afterTx == nil || afterTx == h.db || afterTx != begins[1] {
		t.Fatalf("AfterCreate must run on the second transaction, begins=%d", len(begins))
	}
}
