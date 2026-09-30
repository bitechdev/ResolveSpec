package restheadspec

import (
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// recordTxOrder records the order hooks fire in and the Tx each one saw.
func recordTxOrder(h *Handler, beginErr error) (*[]string, *[]common.Database) {
	var order []string
	var txs []common.Database
	h.Hooks().Register(OnTxBegin, func(ctx *HookContext) error {
		order = append(order, "begin")
		txs = append(txs, ctx.Tx)
		return beginErr
	})
	h.Hooks().Register(BeforeDelete, func(ctx *HookContext) error {
		order = append(order, "before_delete")
		return nil
	})
	return &order, &txs
}

func TestOnTxBeginFiresOnceFirstOnTx(t *testing.T) {
	cases := map[string]struct {
		id   string
		data interface{}
		exec int
	}{
		"single": {id: "7", exec: 1},
		"batch":  {data: []interface{}{"1", "2"}, exec: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, mock, _ := newDeleteHarness(t)
			order, txs := recordTxOrder(h, nil)

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
			if len(*txs) != 1 || (*txs)[0] == nil || (*txs)[0] == h.db {
				t.Fatalf("OnTxBegin must fire once on the transaction, got %v", *txs)
			}
			if (*order)[0] != "begin" {
				t.Fatalf("OnTxBegin must fire before other hooks, got %v", *order)
			}
		})
	}
}

func TestOnTxBeginErrorRollsBackWithoutQueries(t *testing.T) {
	h, mock, _ := newDeleteHarness(t)
	order, _ := recordTxOrder(h, errors.New("no user"))

	mock.ExpectBegin()
	mock.ExpectRollback()

	if rec := runDelete(h, "7", nil); rec.Code < http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(*order) != 1 {
		t.Fatalf("no hook may run after a failed OnTxBegin, got %v", *order)
	}
}
