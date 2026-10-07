package resolvemcp

import (
	"context"
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

func newReadOnlyHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler(database.NewPgSQLAdapter(nil), modelregistry.NewModelRegistry(),
		Config{EnableAnnotations: true})
	if err := h.RegisterModel("public", "items", &docItem{}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestReadOnlyToolSet(t *testing.T) {
	h := newReadOnlyHandler(t)
	tools := h.mcpServer.ListTools()
	for _, name := range []string{"list_tables", "describe_table", "select_table"} {
		if tools[name] == nil {
			t.Errorf("read tool %s missing", name)
		}
	}
	for _, name := range []string{"insert_into_table", "update_table", "delete_from_table", "call_function", "list_functions", annotationToolName} {
		if tools[name] != nil {
			t.Errorf("tool %s must not be registered on a read-only server", name)
		}
	}
}

func TestReadOnlyRefusesWritesAndReportsIt(t *testing.T) {
	h := newReadOnlyHandler(t)
	ctx := context.Background()
	args := map[string]any{"table": "public.items", "data": map[string]any{"name": "x"}, "id": 1}

	for name, fn := range map[string]func() map[string]any{
		"insert": func() map[string]any { r, _ := h.handleInsert(ctx, callReq(args)); return payload(t, r) },
		"update": func() map[string]any { r, _ := h.handleUpdate(ctx, callReq(args)); return payload(t, r) },
		"delete": func() map[string]any { r, _ := h.handleDelete(ctx, callReq(args)); return payload(t, r) },
	} {
		e, _ := fn()["error"].(map[string]any)
		if e["code"] != CodeForbidden || !strings.Contains(e["message"].(string), "read-only") {
			t.Errorf("%s: error = %v", name, e)
		}
	}

	res, _ := h.handleListTables(ctx, callReq(nil))
	tb := payload(t, res)["tables"].([]any)[0].(map[string]any)
	if ops := tb["operations"].([]any); len(ops) != 1 || ops[0] != opSelect {
		t.Errorf("list_tables operations = %v", ops)
	}

	res, _ = h.handleDescribeTable(ctx, callReq(map[string]any{"table": "public.items"}))
	p := payload(t, res)
	if p["read_only"] != true {
		t.Errorf("describe_table read_only = %v", p["read_only"])
	}
	if w, _ := p["writable_columns"].([]any); len(w) != 0 {
		t.Errorf("writable_columns = %v", w)
	}

	cat := h.BuildCatalog()
	if !cat.ReadOnly || !strings.Contains(cat.Guide, "READ-ONLY") || !strings.Contains(cat.Markdown(), "read-only") {
		t.Error("catalogue must say the server is read-only")
	}
	for _, c := range cat.Tables[0].Columns {
		if c.Writable {
			t.Errorf("column %s marked writable", c.Name)
		}
	}
	if !strings.Contains(guideFor(true, false), "READ-ONLY") || strings.Contains(guideFor(false, false), "READ-ONLY") {
		t.Error("guideFor")
	}
}

func newFnHandler(t *testing.T, cfg Config) *Handler {
	t.Helper()
	h := NewHandler(database.NewPgSQLAdapter(nil), modelregistry.NewModelRegistry(), cfg)
	for _, name := range []string{"alpha", "beta"} {
		name := name
		err := h.RegisterFunction(Function{Name: name, Handler: func(context.Context, common.Database, map[string]any) (any, error) {
			return name, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func TestReadOnlyAllowFunctionCalls(t *testing.T) {
	h := newFnHandler(t, Config{AllowFunctionCalls: true})
	tools := h.mcpServer.ListTools()
	if tools["list_functions"] == nil || tools["call_function"] == nil {
		t.Error("function tools must be registered")
	}
	if tools["insert_into_table"] != nil || tools["update_table"] != nil {
		t.Error("write tools must stay off")
	}
	if g := guideFor(true, true); !strings.Contains(g, "READ-ONLY") || !strings.Contains(g, "call_function") {
		t.Error("guide must mention functions")
	}
	if h.mcpServer.ListTools()["call_function"] == nil {
		t.Error("call_function missing")
	}
}

func TestAllowedFunctions(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		allowed []string
		visible []string
	}{
		"empty allows all": {nil, []string{"alpha", "beta"}},
		"only listed":      {[]string{"beta"}, []string{"beta"}},
		"unknown name":     {[]string{"zzz"}, nil},
	} {
		h := newFnHandler(t, Config{AllowedFunctions: tc.allowed})
		var got []string
		for _, f := range h.visibleFunctions(ctx) {
			got = append(got, f.Name)
		}
		if strings.Join(got, ",") != strings.Join(tc.visible, ",") {
			t.Errorf("%s: visible = %v, want %v", name, got, tc.visible)
		}
		for _, fn := range []string{"alpha", "beta"} {
			listed := false
			for _, v := range tc.visible {
				listed = listed || v == fn
			}
			if h.functionAllowed(fn) != listed {
				t.Errorf("%s: functionAllowed(%s) = %v, want %v", name, fn, !listed, listed)
			}
			if !listed {
				// refused before any database work, and indistinguishable from a missing function
				if _, err := h.executeCall(ctx, fn, nil); err == nil || !strings.Contains(err.Error(), "unknown function") {
					t.Errorf("%s: %s must be reported unknown, err=%v", name, fn, err)
				}
			}
		}
	}
}

func TestReadOnlyDefaultsOnAndCanBeDisabled(t *testing.T) {
	if !(Config{}).withDefaults().readOnly {
		t.Error("ReadOnly must default to on")
	}
	if !(Config{ReadOnly: Bool(true)}).withDefaults().readOnly {
		t.Error("explicit true")
	}
	if (Config{ReadOnly: Bool(false)}).withDefaults().readOnly {
		t.Error("Bool(false) must enable writes")
	}
	h := NewHandler(database.NewPgSQLAdapter(nil), modelregistry.NewModelRegistry(), Config{ReadOnly: Bool(false)})
	if h.mcpServer.ListTools()["insert_into_table"] == nil {
		t.Error("write tools must register when ReadOnly is Bool(false)")
	}
	if strings.Contains(h.BuildCatalog().Guide, "READ-ONLY") {
		t.Error("guide must not claim read-only")
	}
}
