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

func TestAfterReadRunsInSecondTransaction(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	h := NewHandler(database.NewBunAdapter(bun.NewDB(sqlDB, pgdialect.New())), modelregistry.NewModelRegistry())
	var begins []common.Database
	var readTx, afterTx common.Database
	h.Hooks().Register(OnTxBegin, func(c *HookContext) error { begins = append(begins, c.Tx); return nil })
	h.Hooks().Register(BeforeRead, func(c *HookContext) error { readTx = c.Tx; return nil })
	h.Hooks().Register(AfterRead, func(c *HookContext) error { afterTx = c.Tx; return nil })

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx := WithSchema(base, "public")
	ctx = WithEntity(ctx, "items")
	ctx = WithTableName(ctx, "items")
	ctx = WithModel(ctx, delItem{})
	h.handleRead(ctx, w, "7", ExtendedRequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(begins) != 2 || begins[0] == begins[1] {
		t.Fatalf("OnTxBegin must fire once per transaction (2), got %d", len(begins))
	}
	if readTx != begins[0] || afterTx != begins[1] || afterTx == h.db {
		t.Fatal("BeforeRead must run on the first tx and AfterRead on the second")
	}
}
