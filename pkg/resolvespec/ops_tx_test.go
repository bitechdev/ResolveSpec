package resolvespec

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func opCtx(t *testing.T) (context.Context, common.ResponseWriter, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	w, _ := common.WrapHTTPRequest(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return WithRequestData(base, "public", "items", "items", &delItem{}, &delItem{}), w, rec
}

// hookTrace records hook order and the Tx each hook saw.
type hookTrace struct {
	order []string
	tx    map[HookType][]common.Database
}

func traceHooks(h *Handler, types ...HookType) *hookTrace {
	tr := &hookTrace{tx: map[HookType][]common.Database{}}
	for _, ht := range types {
		ht := ht
		h.Hooks().Register(ht, func(c *HookContext) error {
			tr.order = append(tr.order, string(ht))
			tr.tx[ht] = append(tr.tx[ht], c.Tx)
			return nil
		})
	}
	return tr
}

func (tr *hookTrace) mustBeOn(t *testing.T, tx common.Database, types ...HookType) {
	t.Helper()
	for _, ht := range types {
		if len(tr.tx[ht]) == 0 || tr.tx[ht][0] != tx {
			t.Fatalf("%s must run on the OnTxBegin transaction", ht)
		}
	}
}

func TestReadRunsHooksOnOneTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeRead)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()

	ctx, w, rec := opCtx(t)
	h.handleRead(ctx, w, "7", common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(tr.tx[OnTxBegin]) != 1 || tr.order[0] != "on_tx_begin" {
		t.Fatalf("OnTxBegin must fire once and first, got %v", tr.order)
	}
	tr.mustBeOn(t, tr.tx[OnTxBegin][0], BeforeRead)
}

func TestReadBeforeHookErrorRollsBackWithoutQueries(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(BeforeRead, func(*HookContext) error { return errors.New("denied") })

	mock.ExpectBegin()
	mock.ExpectRollback()

	ctx, w, rec := opCtx(t)
	h.handleRead(ctx, w, "7", common.RequestOptions{})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failing BeforeRead must not return data: %s", rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRunsHooksOnTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeCreate)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectCommit()

	ctx, w, rec := opCtx(t)
	h.handleCreate(ctx, w, map[string]interface{}{"name": "a"}, common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if tr.order[0] != "on_tx_begin" {
		t.Fatalf("OnTxBegin must fire first, got %v", tr.order)
	}
	for _, tx := range tr.tx[BeforeCreate] {
		if tx == nil || tx == h.db {
			t.Fatal("BeforeCreate must not get the pool")
		}
	}
}

func TestCreateBeforeHookErrorRollsBack(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(BeforeCreate, func(*HookContext) error { return errors.New("denied") })

	mock.ExpectBegin()
	mock.ExpectRollback()

	ctx, w, rec := opCtx(t)
	h.handleCreate(ctx, w, map[string]interface{}{"name": "a"}, common.RequestOptions{})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failing BeforeCreate must not create: %s", rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAfterHookErrorRollsBackAndSkipsRefetch(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(AfterUpdate, func(*HookContext) error { return errors.New("audit failed") })

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	ctx, w, rec := opCtx(t)
	h.handleUpdate(ctx, w, "7", nil, map[string]interface{}{"name": "b"}, common.RequestOptions{})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failing AfterUpdate must fail the request: %s", rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRunsBeforeAndAfterOnFirstTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeUpdate, AfterUpdate)

	cols := []string{"id", "name"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "b"))
	mock.ExpectCommit()

	ctx, w, rec := opCtx(t)
	h.handleUpdate(ctx, w, "7", nil, map[string]interface{}{"name": "b"}, common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(tr.tx[OnTxBegin]) != 2 {
		t.Fatalf("expected OnTxBegin twice, got %d", len(tr.tx[OnTxBegin]))
	}
	tr.mustBeOn(t, tr.tx[OnTxBegin][0], BeforeUpdate, AfterUpdate)
}
