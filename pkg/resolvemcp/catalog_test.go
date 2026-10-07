package resolvemcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

type docItem struct {
	ID    int    `json:"id" bun:"id,pk"`
	Email string `json:"email" bun:"email,comment:Tag comment"`
	Name  string `json:"name" bun:"name" note:"Name from tag"`
}

func (docItem) ModelDescription() string { return "Model-level fallback" }

func newDocHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler(database.NewPgSQLAdapter(nil), modelregistry.NewModelRegistry(), Config{})
	if err := h.RegisterModel("public", "items", &docItem{}); err != nil {
		t.Fatal(err)
	}
	hidden := modelregistry.ModelRules{} // no operation allowed
	if err := h.RegisterModelWithRules("public", "secret", &docItem{}, hidden); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCatalogDescriptionsPrecedence(t *testing.T) {
	h := newDocHandler(t)
	cat := h.BuildCatalog()
	if len(cat.Tables) != 1 || cat.Tables[0].Table != "public.items" {
		t.Fatalf("tables = %+v (hidden table must be left out)", cat.Tables)
	}
	tb := cat.Tables[0]
	if tb.Description != "Model-level fallback" {
		t.Errorf("fallback description = %q", tb.Description)
	}
	col := map[string]string{}
	for _, c := range tb.Columns {
		col[c.Name] = c.Description
	}
	if col["email"] != "Tag comment" || col["name"] != "Name from tag" {
		t.Errorf("tag comments = %v", col)
	}

	path := filepath.Join(t.TempDir(), "desc.json")
	if err := os.WriteFile(path, []byte(`{"public.items":{"description":"From file","columns":{"email":"File email"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := h.LoadModelDescriptions(path); err != nil || n != 1 {
		t.Fatalf("load n=%d err=%v", n, err)
	}
	tb = h.BuildCatalog().Tables[0]
	if tb.Description != "From file" {
		t.Errorf("file must win, got %q", tb.Description)
	}
	for _, c := range tb.Columns {
		switch c.Name {
		case "email":
			if c.Description != "File email" {
				t.Errorf("email = %q", c.Description)
			}
		case "name":
			if c.Description != "Name from tag" {
				t.Errorf("name must fall back to tag, got %q", c.Description)
			}
		}
	}
}

func TestExportCatalogFiles(t *testing.T) {
	h := newDocHandler(t)
	dir := t.TempDir()

	md := filepath.Join(dir, "sub", "catalog.md")
	if err := h.ExportCatalog(md); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(md)
	for _, want := range []string{"# resolvemcp API catalogue", "### public.items", "Model-level fallback", "`list_tables`", "Tag comment"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("markdown missing %q", want)
		}
	}
	if strings.Contains(string(b), "public.secret") {
		t.Error("table without operations leaked into the catalogue")
	}

	js := filepath.Join(dir, "catalog.json")
	if err := h.ExportCatalog(js); err != nil {
		t.Fatal(err)
	}
	var cat Catalog
	b, _ = os.ReadFile(js)
	if err := json.Unmarshal(b, &cat); err != nil || len(cat.Tables) != 1 || len(cat.Tools) == 0 {
		t.Fatalf("json catalog bad: err=%v %+v", err, cat)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".catalog-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestDescribeAndListIncludeDescriptions(t *testing.T) {
	h := newDocHandler(t)
	res, _ := h.handleListTables(nil, callReq(nil))
	tables, _ := payload(t, res)["tables"].([]any)
	if len(tables) != 1 || tables[0].(map[string]any)["description"] != "Model-level fallback" {
		t.Errorf("list_tables = %v", tables)
	}
	res, _ = h.handleDescribeTable(nil, callReq(map[string]any{"table": "public.items"}))
	p := payload(t, res)
	if p["description"] != "Model-level fallback" {
		t.Errorf("describe_table description = %v", p["description"])
	}
}
