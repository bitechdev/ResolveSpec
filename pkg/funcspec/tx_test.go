package funcspec

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// txFactory returns a pool whose every transaction is a distinct MockDatabase.
func txFactory(queries *int) *MockDatabase {
	return &MockDatabase{
		RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
			return fn(&MockDatabase{
				QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
					*queries++
					if rows, ok := dest.(*[]map[string]interface{}); ok {
						*rows = []map[string]interface{}{{"id": float64(1)}}
					}
					return nil
				},
			})
		},
	}
}

func TestOnTxBeginFirstAndBeforeResponseOnSecondTx(t *testing.T) {
	var queries int
	h := NewHandler(txFactory(&queries))
	var order []string
	txs := map[HookType][]common.Database{}
	for _, ht := range []HookType{OnTxBegin, BeforeQuery, AfterQuery, BeforeResponse} {
		ht := ht
		h.Hooks().Register(ht, func(c *HookContext) error {
			order = append(order, string(ht))
			txs[ht] = append(txs[ht], c.Tx)
			return nil
		})
	}

	w := httptest.NewRecorder()
	h.SqlQuery("SELECT * FROM users WHERE id = 1", SqlQueryOptions{})(w, createTestRequest("GET", "/t", nil, nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if got := strings.Join(order, ","); got != "on_tx_begin,before_query,after_query,on_tx_begin,before_response" {
		t.Fatalf("hook order %s", got)
	}
	if txs[BeforeQuery][0] != txs[OnTxBegin][0] || txs[AfterQuery][0] != txs[OnTxBegin][0] {
		t.Fatal("query hooks must run on the OnTxBegin transaction")
	}
	if txs[OnTxBegin][0] == txs[OnTxBegin][1] || txs[BeforeResponse][0] != txs[OnTxBegin][1] {
		t.Fatal("BeforeResponse must run on a second, distinct transaction")
	}
}

func TestOnTxBeginListBeforeResponseOnSecondTx(t *testing.T) {
	var queries int
	h := NewHandler(txFactory(&queries))
	var txs []common.Database
	h.Hooks().Register(OnTxBegin, func(c *HookContext) error { txs = append(txs, c.Tx); return nil })
	var respTx common.Database
	h.Hooks().Register(BeforeResponse, func(c *HookContext) error { respTx = c.Tx; return nil })

	w := httptest.NewRecorder()
	h.SqlQueryList("SELECT * FROM users", SqlQueryOptions{NoCount: true})(w, createTestRequest("GET", "/t", nil, nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if len(txs) != 2 || txs[0] == txs[1] || respTx != txs[1] {
		t.Fatalf("expected 2 distinct transactions with BeforeResponse on the second, got %d", len(txs))
	}
}

func TestOnTxBeginErrorAnswersTransactionError(t *testing.T) {
	var queries int
	h := NewHandler(txFactory(&queries))
	h.Hooks().Register(OnTxBegin, func(*HookContext) error { return errors.New("secret detail") })

	for name, run := range map[string]HTTPFuncType{
		"single": h.SqlQuery("SELECT * FROM users WHERE id = 1", SqlQueryOptions{}),
		"list":   h.SqlQueryList("SELECT * FROM users", SqlQueryOptions{NoCount: true}),
	} {
		w := httptest.NewRecorder()
		run(w, createTestRequest("GET", "/t", nil, nil, nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s: status %d body %s", name, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "secret detail") {
			t.Fatalf("%s: hook error leaked to client: %s", name, w.Body)
		}
	}
	if queries != 0 {
		t.Fatalf("no query may run after a failed OnTxBegin, ran %d", queries)
	}
}
