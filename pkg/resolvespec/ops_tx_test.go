package resolvespec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// resetTotalCache empties the process-wide query-total cache so a total cached by
// another test cannot skip the count query and desync the mock.
func resetTotalCache(t *testing.T) {
	t.Helper()
	_ = cache.GetDefaultCache().Clear(context.Background())
	t.Cleanup(func() { _ = cache.GetDefaultCache().Clear(context.Background()) })
}

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
	resetTotalCache(t)
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

func TestAfterReadRunsOnReadTransactionForSingleAndList(t *testing.T) {
	for name, id := range map[string]string{"single": "7", "list": ""} {
		t.Run(name, func(t *testing.T) {
			resetTotalCache(t)
			h, mock, _ := newDeleteHarness(t)
			tr := traceHooks(h, OnTxBegin, AfterRead)
			var resultType string
			h.Hooks().Register(AfterRead, func(c *HookContext) error {
				resultType = fmt.Sprintf("%T", c.Result)
				return nil
			})

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
			mock.ExpectCommit()

			ctx, w, rec := opCtx(t)
			h.handleRead(ctx, w, id, common.RequestOptions{})

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if len(tr.tx[AfterRead]) != 1 {
				t.Fatalf("AfterRead must fire once, got %d", len(tr.tx[AfterRead]))
			}
			tr.mustBeOn(t, tr.tx[OnTxBegin][0], AfterRead)
			if !strings.HasPrefix(resultType, "*[]") {
				t.Fatalf("AfterRead Result must be the scanned slice, got %s", resultType)
			}
		})
	}
}

func TestAfterReadErrorFailsReadAndRollsBack(t *testing.T) {
	resetTotalCache(t)
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(AfterRead, func(*HookContext) error { return errors.New("masking failed") })

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectRollback()

	ctx, w, rec := opCtx(t)
	h.handleRead(ctx, w, "7", common.RequestOptions{})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failing AfterRead must not return data (fail closed): %s", rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type columnSecProvider struct{ security.SecurityProvider }

func (columnSecProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
	return []security.ColumnSecurity{{
		Schema: schema, Tablename: table, Path: []string{"name"}, UserID: userID, Accesstype: "hide",
	}}, nil
}

func (columnSecProvider) GetRowSecurity(ctx context.Context, userRef any, schema, table string) (security.RowSecurity, error) {
	return security.RowSecurity{}, nil
}

// Column-level security is applied by an AfterRead hook; before AfterRead was wired
// into resolvespec reads, the configured masking was silently skipped.
func TestColumnSecurityHidesColumnOnRead(t *testing.T) {
	for name, id := range map[string]string{"single": "7", "list": ""} {
		t.Run(name, func(t *testing.T) {
			resetTotalCache(t)
			h, mock, _ := newDeleteHarness(t)
			list, err := security.NewSecurityList(columnSecProvider{})
			if err != nil {
				t.Fatal(err)
			}
			RegisterSecurityHooks(h, list)

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "secret"))
			mock.ExpectCommit()

			ctx, w, rec := opCtx(t)
			ctx = context.WithValue(ctx, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
			ctx = context.WithValue(ctx, security.UserIDKey, 7)
			h.handleRead(ctx, w, id, common.RequestOptions{})

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "secret") {
				t.Fatalf("hidden column leaked: %s", rec.Body)
			}
		})
	}
}

func TestAfterCreateRunsInsideCreateTransaction(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	tr := traceHooks(h, OnTxBegin, AfterCreate)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()

	ctx, w, rec := opCtx(t)
	h.handleCreate(ctx, w, map[string]interface{}{"name": "a"}, common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(tr.tx[AfterCreate]) != 1 {
		t.Fatalf("AfterCreate must fire once, got %d", len(tr.tx[AfterCreate]))
	}
	tr.mustBeOn(t, tr.tx[OnTxBegin][0], AfterCreate)
}

func TestAfterCreateFiresPerItemInBatch(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	tr := traceHooks(h, OnTxBegin, AfterCreate)

	mock.ExpectBegin()
	for i := 1; i <= 2; i++ {
		mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(i))
		mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(i, "a"))
	}
	mock.ExpectCommit()

	ctx, w, rec := opCtx(t)
	items := []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}
	h.handleCreate(ctx, w, items, common.RequestOptions{})

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(tr.tx[AfterCreate]) != 2 || len(tr.tx[OnTxBegin]) != 1 {
		t.Fatalf("AfterCreate must fire per item in one transaction, got %d in %d tx", len(tr.tx[AfterCreate]), len(tr.tx[OnTxBegin]))
	}
	tr.mustBeOn(t, tr.tx[OnTxBegin][0], AfterCreate)
}

func TestAfterCreateErrorRollsBackCreate(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(AfterCreate, func(*HookContext) error { return errors.New("audit failed") })

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectRollback()

	ctx, w, rec := opCtx(t)
	h.handleCreate(ctx, w, map[string]interface{}{"name": "a"}, common.RequestOptions{})

	if rec.Code == http.StatusOK {
		t.Fatalf("a failing AfterCreate must fail the request: %s", rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAfterDeleteRunsInsideDeleteTransaction(t *testing.T) {
	for name, tc := range map[string]struct {
		id   string
		data interface{}
		exec int
	}{"single": {id: "7", exec: 1}, "batch": {data: []interface{}{"1", "2"}, exec: 2}} {
		t.Run(name, func(t *testing.T) {
			h, mock, _ := newDeleteHarness(t)
			tr := traceHooks(h, OnTxBegin, AfterDelete)

			mock.ExpectBegin()
			if tc.id != "" {
				mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
			}
			for i := 0; i < tc.exec; i++ {
				mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()

			if rec := runDelete(h, tc.id, tc.data); rec.Code != http.StatusOK {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if len(tr.tx[AfterDelete]) != 1 {
				t.Fatalf("AfterDelete must fire once per request, got %d", len(tr.tx[AfterDelete]))
			}
			tr.mustBeOn(t, tr.tx[OnTxBegin][0], AfterDelete)
		})
	}
}

func TestAfterDeleteErrorRollsBackDelete(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	h.Hooks().Register(AfterDelete, func(*HookContext) error { return errors.New("audit failed") })

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	if rec := runDelete(h, "7", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failing AfterDelete must fail and roll back the delete: status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Columns hidden or masked for the user cannot be written. Handlers are called
// directly here, so the BeforeHandle preload is done by hand.
func TestColumnSecurityDropsHiddenColumnOnWrite(t *testing.T) {
	setup := func(t *testing.T) (*Handler, sqlmock.Sqlmock, context.Context, common.ResponseWriter, *httptest.ResponseRecorder) {
		h, mock, _ := newDeleteHarness(t)
		list, err := security.NewSecurityList(columnSecProvider{})
		if err != nil {
			t.Fatal(err)
		}
		RegisterSecurityHooks(h, list)

		ctx, w, rec := opCtx(t)
		ctx = context.WithValue(ctx, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
		ctx = context.WithValue(ctx, security.UserIDKey, 7)
		pre := newSecurityContext(&HookContext{Context: ctx, Schema: "public", Entity: "items", Model: &delItem{}})
		for _, op := range []string{"create", "update"} {
			if err := security.PreloadSecurityRules(pre, list, op); err != nil {
				t.Fatal(err)
			}
		}
		return h, mock, ctx, w, rec
	}

	t.Run("create", func(t *testing.T) {
		h, mock, ctx, w, rec := setup(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT`).WithArgs(7).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
		mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, ""))
		mock.ExpectCommit()

		payload := map[string]interface{}{"id": 7, "name": "secret"}
		h.handleCreate(ctx, w, payload, common.RequestOptions{})
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
		if _, ok := payload["name"]; ok {
			t.Fatalf("hidden column must be dropped from the insert payload: %v", payload)
		}
	})

	t.Run("update", func(t *testing.T) {
		h, mock, ctx, w, rec := setup(t)
		cols := []string{"id", "name"}
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
		mock.ExpectExec(`UPDATE`).WithArgs(float64(7), "a", "7").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
		mock.ExpectCommit()

		payload := map[string]interface{}{"name": "secret"}
		h.handleUpdate(ctx, w, "7", nil, payload, common.RequestOptions{})
		if _, ok := payload["name"]; ok {
			t.Fatalf("hidden column must be dropped from the update payload: %v (status %d %s)", payload, rec.Code, rec.Body)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("update must keep the stored value: %v", err)
		}
	})
}
