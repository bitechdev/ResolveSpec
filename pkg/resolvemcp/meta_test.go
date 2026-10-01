package resolvemcp

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

func callReq(args map[string]any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Arguments = args
	return r
}

// payload decodes a tool result's text.
func payload(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty result")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T", res.Content[0])
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("not JSON: %q", tc.Text)
	}
	return m
}

func errCode(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected an error result, got %v", payload(t, res))
	}
	e, _ := payload(t, res)["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestMetaToolSetIsFixed(t *testing.T) {
	h, _, _ := newTxHarness(t)
	for _, name := range []string{"x1", "x2", "x3"} {
		if err := h.RegisterModel("public", name, &txItem{}); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for name := range h.mcpServer.ListTools() {
		got = append(got, name)
	}
	sort.Strings(got)
	want := "call_function delete_from_table describe_table insert_into_table list_functions list_tables select_table update_table"
	if strings.Join(got, " ") != want {
		t.Fatalf("tools = %v\nwant   %s", got, want)
	}
}

func TestListTablesShowsOnlyAllowedOperations(t *testing.T) {
	h, _, ctx := newTxHarness(t)
	_ = h.RegisterModelWithRules("public", "ro", &txItem{}, modelregistry.ModelRules{CanRead: true})
	_ = h.RegisterModelWithRules("public", "hidden", &txItem{}, modelregistry.ModelRules{})
	res, _ := h.handleListTables(ctx, callReq(nil))
	tables, _ := payload(t, res)["tables"].([]any)
	seen := map[string][]any{}
	for _, tb := range tables {
		m := tb.(map[string]any)
		seen[m["table"].(string)] = m["operations"].([]any)
	}
	if _, ok := seen["public.hidden"]; ok {
		t.Error("a table with no allowed operation must not be listed")
	}
	if ops := seen["public.ro"]; len(ops) != 1 || ops[0] != "select" {
		t.Errorf("ro ops = %v", ops)
	}
	if ops := seen["public.items"]; len(ops) != 4 {
		t.Errorf("default rules allow all four, got %v", ops)
	}
	// describe_table on a table with no allowed operation reads as unknown.
	res, _ = h.handleDescribeTable(ctx, callReq(map[string]any{"table": "public.hidden"}))
	if errCode(t, res) != CodeInvalidArgument {
		t.Error("describe of a hidden table must look like an unknown table")
	}
}

func TestDescribeTable(t *testing.T) {
	h, _, ctx := newTxHarness(t)
	if err := h.RegisterModel("public", "witems", &wItem{}); err != nil {
		t.Fatal(err)
	}
	res, _ := h.handleDescribeTable(ctx, callReq(map[string]any{"table": "public.witems"}))
	p := payload(t, res)
	if p["primary_key"] != "id" {
		t.Errorf("pk = %v", p["primary_key"])
	}
	w, _ := p["writable_columns"].([]any)
	got := map[string]bool{}
	for _, c := range w {
		got[c.(string)] = true
	}
	if !got["name"] || !got["fullName"] || got["id"] || got["owner"] {
		t.Errorf("writable columns = %v", w)
	}
	if lim, _ := p["limits"].(map[string]any); lim["max_limit"] != float64(1000) {
		t.Errorf("limits = %v", p["limits"])
	}
}

func TestSelectRespectsOperationRule(t *testing.T) {
	h, _, ctx := newTxHarness(t)
	_ = h.RegisterModelWithRules("public", "nowrite", &txItem{}, modelregistry.ModelRules{CanRead: true})
	for tool, fn := range map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"insert": h.handleInsert, "update": h.handleUpdate, "delete": h.handleDelete,
	} {
		res, _ := fn(ctx, callReq(map[string]any{"table": "public.nowrite", "data": map[string]any{"name": "a"}, "id": "1"}))
		if errCode(t, res) != CodeForbidden {
			t.Errorf("%s on a read-only table must be forbidden", tool)
		}
	}
	res, _ := h.handleSelect(ctx, callReq(map[string]any{"table": "public.missing"}))
	if errCode(t, res) != CodeInvalidArgument {
		t.Error("unknown table must be invalid_argument")
	}
}

func TestSelectCountOnlyWhenRequested(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a"))
	mock.ExpectCommit()
	res, _ := h.handleSelect(ctx, callReq(map[string]any{"table": "public.items"}))
	if res.IsError {
		t.Fatal(payload(t, res))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(41))
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a"))
	mock.ExpectCommit()
	res, _ = h.handleSelect(ctx, callReq(map[string]any{"table": "public.items", "include_count": true}))
	meta, _ := payload(t, res)["metadata"].(map[string]any)
	if res.IsError || meta["total"] != float64(41) {
		t.Fatalf("metadata = %v", meta)
	}
}

// --- filter writes ---

func matchRowsQuery(mock sqlmock.Sqlmock, ids ...int) {
	rows := sqlmock.NewRows([]string{"id"})
	for _, id := range ids {
		rows.AddRow(id)
	}
	mock.ExpectQuery(`SELECT`).WillReturnRows(rows)
}

func updReq(filters any, extra map[string]any) mcp.CallToolRequest {
	a := map[string]any{"table": "public.items", "data": map[string]any{"name": "z"}, "filters": filters}
	for k, v := range extra {
		a[k] = v
	}
	return callReq(a)
}

var statusFilter = []any{map[string]any{"column": "name", "operator": "=", "value": "a"}}

func TestFilterUpdateNeedsPreviewThenToken(t *testing.T) {
	h, mock, ctx := newTxHarness(t)

	// 1. preview: counts, lists ids, issues a token, writes nothing.
	mock.ExpectBegin()
	matchRowsQuery(mock, 1, 2)
	mock.ExpectCommit()
	res, _ := h.handleUpdate(ctx, updReq(statusFilter, nil))
	r, _ := payload(t, res)["result"].(map[string]any)
	tok, _ := r["confirm_token"].(string)
	if res.IsError || tok == "" || r["requires_confirmation"] != true || r["matched"] != float64(2) {
		t.Fatalf("preview = %v", payload(t, res))
	}

	// 2. with the token: re-matches inside the tx, then writes.
	mock.ExpectBegin()
	matchRowsQuery(mock, 1, 2)
	mock.ExpectExec(`UPDATE .* WHERE "id" IN \(\$2, \$3\)`).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	res, _ = h.handleUpdate(ctx, updReq(statusFilter, map[string]any{"confirm_token": tok}))
	r, _ = payload(t, res)["result"].(map[string]any)
	if res.IsError || r["affected"] != float64(2) {
		t.Fatalf("confirmed = %v", payload(t, res))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// 3. a token is single use.
	mock.ExpectBegin()
	matchRowsQuery(mock, 1, 2)
	mock.ExpectRollback()
	res, _ = h.handleUpdate(ctx, updReq(statusFilter, map[string]any{"confirm_token": tok}))
	if errCode(t, res) != CodeInvalidArgument {
		t.Error("a spent token must be rejected")
	}
}

func TestConfirmTokenBinding(t *testing.T) {
	issue := func(t *testing.T) (*Handler, sqlmock.Sqlmock, context.Context, string) {
		h, mock, ctx := newTxHarness(t)
		mock.ExpectBegin()
		matchRowsQuery(mock, 1)
		mock.ExpectCommit()
		res, _ := h.handleUpdate(ctx, updReq(statusFilter, nil))
		r, _ := payload(t, res)["result"].(map[string]any)
		return h, mock, ctx, r["confirm_token"].(string)
	}
	reject := func(t *testing.T, h *Handler, mock sqlmock.Sqlmock, ctx context.Context, req mcp.CallToolRequest, rows ...int) {
		t.Helper()
		mock.ExpectBegin()
		matchRowsQuery(mock, rows...)
		mock.ExpectRollback()
		res, _ := h.handleUpdate(ctx, req)
		if errCode(t, res) != CodeInvalidArgument {
			t.Fatal("token must be rejected")
		}
	}
	t.Run("changed data", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		req := updReq(statusFilter, map[string]any{"confirm_token": tok, "data": map[string]any{"name": "different"}})
		reject(t, h, mock, ctx, req, 1)
	})
	t.Run("changed filters", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		other := []any{map[string]any{"column": "name", "operator": "=", "value": "b"}}
		reject(t, h, mock, ctx, updReq(other, map[string]any{"confirm_token": tok}), 1)
	})
	t.Run("rows changed since preview", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		reject(t, h, mock, ctx, updReq(statusFilter, map[string]any{"confirm_token": tok}), 1, 2)
	})
	t.Run("other user", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		other := context.WithValue(ctx, security.UserContextKey, &security.UserContext{UserID: 99, UserName: "mallory"})
		reject(t, h, mock, other, updReq(statusFilter, map[string]any{"confirm_token": tok}), 1)
	})
	t.Run("other table", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		if err := h.RegisterModel("public", "other", &txItem{}); err != nil {
			t.Fatal(err)
		}
		req := updReq(statusFilter, map[string]any{"confirm_token": tok, "table": "public.other"})
		reject(t, h, mock, ctx, req, 1)
	})
	t.Run("expired", func(t *testing.T) {
		h, mock, ctx, tok := issue(t)
		h.confirms.now = func() time.Time { return time.Now().Add(time.Hour) }
		reject(t, h, mock, ctx, updReq(statusFilter, map[string]any{"confirm_token": tok}), 1)
	})
}

func TestFilterWriteGuardrails(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	for name, req := range map[string]mcp.CallToolRequest{
		"neither id nor filters": callReq(map[string]any{"table": "public.items", "data": map[string]any{"name": "z"}}),
		"both id and filters":    updReq(statusFilter, map[string]any{"id": "1"}),
		"malformed filter":       updReq([]any{map[string]any{"column": "name"}}, nil),
		"unknown column":         updReq([]any{map[string]any{"column": "secret", "operator": "=", "value": 1}}, nil),
		"injection in column":    updReq([]any{map[string]any{"column": "name) OR (1=1", "operator": "=", "value": 1}}, nil),
		"unknown operator":       updReq([]any{map[string]any{"column": "name", "operator": "ɸ", "value": 1}}, nil),
		"missing value":          updReq([]any{map[string]any{"column": "name", "operator": "="}}, nil),
		"unknown data field":     callReq(map[string]any{"table": "public.items", "data": map[string]any{"role": "x"}, "filters": statusFilter}),
	} {
		res, _ := h.handleUpdate(ctx, req)
		if errCode(t, res) != CodeInvalidArgument {
			t.Errorf("%s: want invalid_argument", name)
		}
	}
	// Rejected before any SQL ran.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFilterWriteRowCap(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	h.config.MaxWriteRows = 2
	mock.ExpectBegin()
	matchRowsQuery(mock, 1, 2, 3)
	mock.ExpectRollback()
	res, _ := h.handleDelete(ctx, callReq(map[string]any{"table": "public.items", "filters": statusFilter}))
	if errCode(t, res) != CodeLimitExceeded {
		t.Fatal("want limit_exceeded")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	mock.ExpectBegin()
	matchRowsQuery(mock, 5)
	mock.ExpectCommit()
	res, _ := h.handleDelete(ctx, callReq(map[string]any{"table": "public.items", "filters": statusFilter, "dry_run": true}))
	r, _ := payload(t, res)["result"].(map[string]any)
	if res.IsError || r["dry_run"] != true || r["matched"] != float64(1) || r["confirm_token"] != nil {
		t.Fatalf("dry run = %v", payload(t, res))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestIDWriteNeedsNoToken(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(7, "a"))
	mock.ExpectExec(`DELETE`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	res, _ := h.handleDelete(ctx, callReq(map[string]any{"table": "public.items", "id": float64(7)}))
	if res.IsError {
		t.Fatal(payload(t, res))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// --- functions ---

func TestRegisterFunctionValidation(t *testing.T) {
	h, _, _ := newTxHarness(t)
	noop := func(context.Context, common.Database, map[string]any) (any, error) { return nil, nil }
	bad := map[string]Function{
		"bad name":        {Name: "1x", Handler: noop},
		"neither":         {Name: "f"},
		"both":            {Name: "f", Handler: noop, Procedure: "p"},
		"bad procedure":   {Name: "f", Procedure: "p(); drop table x"},
		"bad param type":  {Name: "f", Handler: noop, Params: []FunctionParam{{Name: "a", Type: "blob"}}},
		"duplicate param": {Name: "f", Handler: noop, Params: []FunctionParam{{Name: "a", Type: "string"}, {Name: "a", Type: "string"}}},
		"bad param name":  {Name: "f", Handler: noop, Params: []FunctionParam{{Name: "a b", Type: "string"}}},
	}
	for name, f := range bad {
		if err := h.RegisterFunction(f); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := h.RegisterFunction(Function{Name: "ok", Handler: noop}); err != nil {
		t.Fatal(err)
	}
	if err := h.RegisterFunction(Function{Name: "ok", Handler: noop}); err == nil {
		t.Error("duplicate name must fail")
	}
}

func TestCallFunctionValidatesAndRunsInTx(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	var gotArgs map[string]any
	var gotTx common.Database
	if err := h.RegisterFunction(Function{
		Name: "greet", Description: "says hi",
		Params: []FunctionParam{{Name: "who", Type: ParamString, Required: true}, {Name: "n", Type: ParamInteger}},
		Handler: func(_ context.Context, tx common.Database, args map[string]any) (any, error) {
			gotArgs, gotTx = args, tx
			return map[string]any{"hello": args["who"]}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	tr := traceHooks(h, OnTxBegin, BeforeCall, AfterCall)

	for name, args := range map[string]map[string]any{
		"missing required": {},
		"wrong type":       {"who": 5},
		"fractional int":   {"who": "x", "n": 1.5},
		"unknown arg":      {"who": "x", "extra": 1},
	} {
		res, _ := h.handleCallFunction(ctx, callReq(map[string]any{"name": "greet", "arguments": args}))
		if errCode(t, res) != CodeInvalidArgument {
			t.Errorf("%s: want invalid_argument", name)
		}
	}
	res, _ := h.handleCallFunction(ctx, callReq(map[string]any{"name": "nope"}))
	if errCode(t, res) != CodeInvalidArgument {
		t.Error("unknown function must be invalid_argument")
	}

	mock.ExpectBegin()
	mock.ExpectCommit()
	res, _ = h.handleCallFunction(ctx, callReq(map[string]any{"name": "greet", "arguments": map[string]any{"who": "kim", "n": float64(2)}}))
	if res.IsError || gotArgs["who"] != "kim" || gotTx == nil {
		t.Fatalf("call = %v", payload(t, res))
	}
	tr.assertOrder(t, "on_tx_begin", "before_call", "after_call")
	if tr.txs["before_call"][0] != tr.txs["on_tx_begin"][0] {
		t.Error("the call must run in the OnTxBegin transaction")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFunctionAuthorizeHidesAndBlocks(t *testing.T) {
	h, _, ctx := newTxHarness(t)
	noop := func(context.Context, common.Database, map[string]any) (any, error) { return "ran", nil }
	_ = h.RegisterFunction(Function{Name: "open", Handler: noop})
	_ = h.RegisterFunction(Function{Name: "admin_only", Handler: noop, Authorize: func(context.Context) error { return errors.New("no") }})

	res, _ := h.handleListFunctions(ctx, callReq(nil))
	fns, _ := payload(t, res)["functions"].([]any)
	if len(fns) != 1 || fns[0].(map[string]any)["name"] != "open" {
		t.Fatalf("visible functions = %v", fns)
	}
	res, _ = h.handleCallFunction(ctx, callReq(map[string]any{"name": "admin_only"}))
	if errCode(t, res) != CodeInvalidArgument {
		t.Error("an unauthorized function must look unknown")
	}
}

func TestProcedureFunctionCallShape(t *testing.T) {
	h, mock, ctx := newTxHarness(t)
	if err := h.RegisterFunction(Function{
		Name: "recalc", Procedure: "app.recalc_totals",
		Params: []FunctionParam{{Name: "account", Type: ParamInteger, Required: true}, {Name: "opts", Type: ParamObject}},
	}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT \* FROM app\.recalc_totals\(\$1, \$2::jsonb\)`).WithArgs(float64(3), nil).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(10))
	mock.ExpectCommit()
	res, _ := h.handleCallFunction(ctx, callReq(map[string]any{"name": "recalc", "arguments": map[string]any{"account": float64(3)}}))
	if res.IsError {
		t.Fatal(payload(t, res))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
