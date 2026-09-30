package restheadspec

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

type delItem struct {
	ID   int    `json:"id" bun:"id,pk"`
	Name string `json:"name" bun:"name"`
}

func newDeleteHarness(t *testing.T) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	// One connection: any statement that bypasses the transaction while it is
	// open cannot get a connection and fails on the request context timeout.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return NewHandler(database.NewPgSQLAdapter(db), modelregistry.NewModelRegistry()), mock, db
}

func runDelete(h *Handler, id string, data interface{}) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx := WithSchema(base, "public")
	ctx = WithEntity(ctx, "items")
	ctx = WithTableName(ctx, "items")
	ctx = WithModel(ctx, &delItem{})
	h.handleDelete(ctx, w, id, data)
	return rec
}

// recordDeleteHook registers a BeforeDelete hook that captures hookCtx.Tx.
func recordDeleteHook(h *Handler, hookErr error) *[]common.Database {
	var seen []common.Database
	h.Hooks().Register(BeforeDelete, func(ctx *HookContext) error {
		seen = append(seen, ctx.Tx)
		return hookErr
	})
	return &seen
}

func TestDeleteSingleUsesOneTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	seen := recordDeleteHook(h, nil)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := runDelete(h, "7", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] == h.db {
		t.Fatalf("BeforeDelete must run once on the transaction, got %v", *seen)
	}
}

func TestDeleteSingleNotFoundRollsBack(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
	mock.ExpectRollback()

	if rec := runDelete(h, "7", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteHookErrorRollsBack(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	recordDeleteHook(h, errors.New("denied"))

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectRollback()

	if rec := runDelete(h, "7", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSingleExecErrorRollsBack(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE FROM`).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	if rec := runDelete(h, "7", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteBatchUsesOneTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	seen := recordDeleteHook(h, nil)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := runDelete(h, "", []interface{}{"1", "2"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	// restheadspec fires the hook per item
	if len(*seen) != 2 || (*seen)[0] == h.db || (*seen)[1] == h.db {
		t.Fatalf("BeforeDelete must run per item on the transaction, got %v", *seen)
	}
	var resp map[string]float64
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp["deleted"] != 2 {
		t.Fatalf("unexpected body %s (%v)", rec.Body, err)
	}
}

func TestDeleteBatchFailureRollsBackAll(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM`).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	if rec := runDelete(h, "", []string{"1", "2"}); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
