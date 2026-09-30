package websocketspec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

type txItem struct {
	ID   int    `json:"id" bun:"id,pk"`
	Name string `json:"name" bun:"name"`
}

func newTxHarness(t *testing.T) (*Handler, sqlmock.Sqlmock, *Connection, *HookContext) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	// One connection: any statement that bypasses the transaction while it is
	// open cannot get a connection and fails on the context timeout.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(database.NewPgSQLAdapter(db), modelregistry.NewModelRegistry())
	conn := NewConnection("c1", nil, h)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   h,
		Schema:    "public",
		Entity:    "items",
		TableName: "public.items",
		Model:     txItem{},
		ModelPtr:  &txItem{},
		ID:        "7",
		Tx:        h.db,
		Metadata:  map[string]interface{}{},
	}
	return h, mock, conn, hookCtx
}

func response(t *testing.T, conn *Connection) ResponseMessage {
	t.Helper()
	select {
	case raw := <-conn.send:
		var resp ResponseMessage
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	default:
		t.Fatal("no response sent")
		return ResponseMessage{}
	}
}

func TestDeleteRunsHooksAndDeleteInOneTransaction(t *testing.T) {
	h, mock, conn, hookCtx := newTxHarness(t)
	var order []string
	var txs []common.Database
	h.Hooks().Register(OnTxBegin, func(c *HookContext) error {
		order = append(order, "begin")
		txs = append(txs, c.Tx)
		return nil
	})
	h.Hooks().Register(BeforeDelete, func(c *HookContext) error {
		order = append(order, "before")
		return nil
	})
	h.Hooks().Register(AfterDelete, func(c *HookContext) error {
		order = append(order, "after")
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	h.handleDelete(conn, &Message{ID: "m1"}, hookCtx)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if !response(t, conn).Success {
		t.Fatal("expected success")
	}
	if len(txs) != 1 || txs[0] == h.db || len(order) != 3 || order[0] != "begin" {
		t.Fatalf("OnTxBegin must fire once, first, on the transaction: %v", order)
	}
}

func TestUpdateRefetchAndAfterHookRunInSecondTransaction(t *testing.T) {
	h, mock, conn, hookCtx := newTxHarness(t)
	hookCtx.Data = map[string]interface{}{"name": "b"}
	var begins []common.Database
	var afterTx common.Database
	h.Hooks().Register(OnTxBegin, func(c *HookContext) error {
		begins = append(begins, c.Tx)
		return nil
	})
	h.Hooks().Register(AfterUpdate, func(c *HookContext) error {
		afterTx = c.Tx
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "b"))
	mock.ExpectCommit()

	h.handleUpdate(conn, &Message{ID: "m1"}, hookCtx)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if !response(t, conn).Success {
		t.Fatal("expected success")
	}
	if len(begins) != 2 || begins[0] == begins[1] || afterTx != begins[1] {
		t.Fatalf("OnTxBegin must fire per transaction and AfterUpdate run on the second, got %d", len(begins))
	}
}

func TestReadRunsInOneTransaction(t *testing.T) {
	h, mock, conn, hookCtx := newTxHarness(t)
	var beforeTx common.Database
	h.Hooks().Register(BeforeRead, func(c *HookContext) error {
		beforeTx = c.Tx
		return nil
	})

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectCommit()

	h.handleRead(conn, &Message{ID: "m1"}, hookCtx)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if !response(t, conn).Success || beforeTx == nil || beforeTx == h.db {
		t.Fatal("BeforeRead must run on the transaction")
	}
}

func TestOnTxBeginErrorRollsBackWithoutDetail(t *testing.T) {
	h, mock, conn, hookCtx := newTxHarness(t)
	h.Hooks().Register(OnTxBegin, func(c *HookContext) error { return errors.New("secret detail") })

	mock.ExpectBegin()
	mock.ExpectRollback()

	h.handleDelete(conn, &Message{ID: "m1"}, hookCtx)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	resp := response(t, conn)
	if resp.Success || resp.Error == nil || resp.Error.Code != "transaction_error" || resp.Error.Message != "Transaction failed" {
		t.Fatalf("unexpected response %+v", resp)
	}
}
