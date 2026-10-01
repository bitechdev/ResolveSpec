package resolvemcp

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

type txItem struct {
	ID   int    `json:"id" bun:"id,pk"`
	Name string `json:"name" bun:"name"`
}

func newTxHarness(t *testing.T) (*Handler, sqlmock.Sqlmock, context.Context) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	// One connection: any statement bypassing the open tx cannot get a
	// connection and fails on the context timeout.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(database.NewPgSQLAdapter(db), modelregistry.NewModelRegistry(), Config{})
	if err := h.RegisterModel("public", "items", &txItem{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return h, mock, ctx
}

// trace records hook firing order and the Tx each hook saw.
type trace struct {
	order []string
	txs   map[string][]common.Database
}

func traceHooks(h *Handler, types ...HookType) *trace {
	tr := &trace{txs: map[string][]common.Database{}}
	for _, ht := range types {
		ht := ht
		h.Hooks().Register(ht, func(c *HookContext) error {
			tr.order = append(tr.order, string(ht))
			tr.txs[string(ht)] = append(tr.txs[string(ht)], c.Tx)
			return nil
		})
	}
	return tr
}

func (tr *trace) assertOrder(t *testing.T, want ...string) {
	t.Helper()
	if len(tr.order) != len(want) {
		t.Fatalf("hook order %v, want %v", tr.order, want)
	}
	for i := range want {
		if tr.order[i] != want[i] {
			t.Fatalf("hook order %v, want %v", tr.order, want)
		}
	}
}

func TestDeleteRunsHooksInOneTransaction(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeDelete, AfterDelete)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := h.executeDelete(ctx, "public", "items", "7"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	tr.assertOrder(t, "on_tx_begin", "before_delete", "after_delete")
	if tr.txs["before_delete"][0] != tr.txs["on_tx_begin"][0] || tr.txs["after_delete"][0] != tr.txs["on_tx_begin"][0] {
		t.Fatal("OnTxBegin, BeforeDelete and AfterDelete must share one transaction")
	}
}

func TestDeleteBeforeHookErrorRollsBack(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	h.Hooks().Register(BeforeDelete, func(*HookContext) error { return sql.ErrConnDone })

	mock.ExpectBegin()
	mock.ExpectRollback()

	if _, err := h.executeDelete(ctx, "public", "items", "7"); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadRunsInOneTransaction(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeRead, AfterRead)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()

	if _, _, err := h.executeRead(ctx, "public", "items", "7", common.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	tr.assertOrder(t, "on_tx_begin", "before_read", "after_read")
	for _, ht := range []string{"before_read", "after_read"} {
		if tr.txs[ht][0] != tr.txs["on_tx_begin"][0] {
			t.Fatalf("%s must run on the OnTxBegin transaction", ht)
		}
	}
}

func TestCreateSingleUsesTwoTransactions(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeCreate, AfterCreate)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()

	if _, err := h.executeCreate(ctx, "public", "items", map[string]interface{}{"name": "a"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	tr.assertOrder(t, "on_tx_begin", "before_create", "on_tx_begin", "after_create")
	if tr.txs["before_create"][0] != tr.txs["on_tx_begin"][0] {
		t.Fatal("BeforeCreate must run on the first transaction")
	}
	if tr.txs["after_create"][0] != tr.txs["on_tx_begin"][1] || tr.txs["on_tx_begin"][0] == tr.txs["on_tx_begin"][1] {
		t.Fatal("AfterCreate must run on a second, distinct transaction")
	}
}

func TestCreateBatchRefetchOnSecondTransaction(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	tr := traceHooks(h, OnTxBegin, AfterCreate)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a"))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(2, "b"))
	mock.ExpectCommit()

	items := []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}
	if _, err := h.executeCreate(ctx, "public", "items", items); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	tr.assertOrder(t, "on_tx_begin", "on_tx_begin", "after_create")
}

func TestUpdateRefetchRunsInSecondTransaction(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	tr := traceHooks(h, OnTxBegin, BeforeUpdate, AfterUpdate)

	cols := []string{"id", "name"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "b"))
	mock.ExpectCommit()

	if _, err := h.executeUpdate(ctx, "public", "items", "7", map[string]interface{}{"name": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	tr.assertOrder(t, "on_tx_begin", "before_update", "after_update", "on_tx_begin")
	if tr.txs["on_tx_begin"][0] == tr.txs["on_tx_begin"][1] {
		t.Fatal("re-fetch must run on a second transaction")
	}
	for _, ht := range []string{"before_update", "after_update"} {
		if tr.txs[ht][0] != tr.txs["on_tx_begin"][0] {
			t.Fatalf("%s must run on the first transaction", ht)
		}
	}
}

func TestAfterDeleteErrorRollsBackDelete(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	h.Hooks().Register(AfterDelete, func(*HookContext) error { return sql.ErrConnDone })

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	if _, err := h.executeDelete(ctx, "public", "items", "7"); err == nil {
		t.Fatal("a failing AfterDelete must fail the request")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateBeforeHookErrorRollsBackWithoutInsert(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	h.Hooks().Register(BeforeCreate, func(*HookContext) error { return sql.ErrConnDone })

	mock.ExpectBegin()
	mock.ExpectRollback()

	if _, err := h.executeCreate(ctx, "public", "items", map[string]interface{}{"name": "a"}); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateInsertErrorRollsBackBatch(t *testing.T) {
	h, mock, ctx := newTxHarness(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`INSERT`).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	items := []interface{}{map[string]interface{}{"name": "a"}, map[string]interface{}{"name": "b"}}
	if _, err := h.executeCreate(ctx, "public", "items", items); err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnTxBeginErrorRollsBackEveryOperation(t *testing.T) {
	ops := map[string]func(h *Handler, ctx context.Context) error{
		"read": func(h *Handler, ctx context.Context) error {
			_, _, err := h.executeRead(ctx, "public", "items", "7", common.RequestOptions{})
			return err
		},
		"create": func(h *Handler, ctx context.Context) error {
			_, err := h.executeCreate(ctx, "public", "items", map[string]interface{}{"name": "a"})
			return err
		},
		"update": func(h *Handler, ctx context.Context) error {
			_, err := h.executeUpdate(ctx, "public", "items", "7", map[string]interface{}{"name": "a"})
			return err
		},
		"delete": func(h *Handler, ctx context.Context) error {
			_, err := h.executeDelete(ctx, "public", "items", "7")
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			h, mock, ctx := newTxHarness(t)
			h.Hooks().Register(OnTxBegin, func(*HookContext) error { return sql.ErrConnDone })
			mock.ExpectBegin()
			mock.ExpectRollback()
			if err := op(h, ctx); err == nil {
				t.Fatal("expected error")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type stubProvider struct{ security.SecurityProvider }

func (stubProvider) GetColumnSecurity(context.Context, int, string, string) ([]security.ColumnSecurity, error) {
	return nil, nil
}

func (stubProvider) GetRowSecurity(context.Context, any, string, string) (security.RowSecurity, error) {
	return security.RowSecurity{}, nil
}

func TestSecurityHooksStampTxSettingsOnEveryTransaction(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	list, err := security.NewSecurityList(stubProvider{})
	if err != nil {
		t.Fatal(err)
	}
	list.SetTxSettings(func(security.SecurityContext) (map[string]string, error) {
		return map[string]string{"app.user_id": "7"}, nil
	})
	RegisterSecurityHooks(h, list)
	ctx = context.WithValue(ctx, security.UserContextKey, &security.UserContext{UserID: 7, UserName: "u"})
	ctx = context.WithValue(ctx, security.UserIDKey, 7)

	// Update opens two transactions; each must be stamped before any other SQL.
	cols := []string{"id", "name"}
	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.user_id'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "a"))
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`set_config\('app\.user_id'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(cols).AddRow(7, "b"))
	mock.ExpectCommit()

	if _, err := h.executeUpdate(ctx, "public", "items", "7", map[string]interface{}{"name": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
