package resolvespec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestUpdateRefetchRunsInSecondTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	var begins []common.Database
	h.Hooks().Register(OnTxBegin, func(ctx *HookContext) error {
		begins = append(begins, ctx.Tx)
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
	ctx := WithRequestData(base, "public", "items", "items", &delItem{}, &delItem{})
	h.handleUpdate(ctx, w, "7", nil, map[string]interface{}{"name": "b"}, common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(begins) != 2 || begins[0] == begins[1] {
		t.Fatalf("OnTxBegin must fire once per transaction (2), got %d", len(begins))
	}
}
