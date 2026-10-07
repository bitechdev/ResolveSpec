package resolvemcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// usageGuide is the short agent-facing guide sent as the MCP server instructions and
// embedded in the exported catalogue. It is generic: it never mentions concrete models.
const usageGuide = `This server exposes database tables through a fixed set of tools.
1. Call list_tables to see the tables you may use, what they hold and the operations allowed.
2. Call describe_table for a table before using it: columns, types, primary key, relations (preloadable), writable columns and limits.
3. Read with select_table (filters, sort, columns, preloads). Results are paged; use limit/offset or cursors, and include_count only when you need a total.
4. Write with insert_into_table, update_table, delete_from_table. Address one row by id, or several by filters. A filter-based write first returns a preview; repeat the call with the confirm_token to apply it (dry_run only previews).
5. Use list_functions / call_function for registered functions.
Read the error message when a call fails: it says which argument was wrong.`

// readOnlyGuide replaces usageGuide on a read-only server.
const readOnlyGuide = `This server exposes database tables through a fixed set of tools. It is READ-ONLY: you cannot insert, update or delete data or write annotations, and no tool for that exists. Do not attempt a write; tell the user it is not possible through this server.
1. Call list_tables to see the tables you may read and what they hold.
2. Call describe_table for a table before using it: columns, types, primary key, relations (preloadable) and limits.
3. Read with select_table (filters, sort, columns, preloads). Results are paged; use limit/offset or cursors, and include_count only when you need a total.
Read the error message when a call fails: it says which argument was wrong.`

// readOnlyFunctionsGuide is the extra step of a read-only server that still allows functions.
const readOnlyFunctionsGuide = `
4. Use list_functions / call_function for the registered functions. Only call functions that fit a read-only server; the server decides what is allowed.`

// guideFor returns the usage guide for the server mode.
func guideFor(readOnly, functions bool) string {
	if !readOnly {
		return usageGuide
	}
	if functions {
		return readOnlyGuide + readOnlyFunctionsGuide
	}
	return readOnlyGuide
}

// Catalog is a snapshot of what the server offers: the usage guide, the tools, the limits
// and every table with its columns, relations, allowed operations and descriptions.
type Catalog struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Server      string         `json:"server"`
	Version     string         `json:"version"`
	ReadOnly    bool           `json:"read_only"`
	Guide       string         `json:"guide"`
	Limits      CatalogLimits  `json:"limits"`
	Tools       []CatalogTool  `json:"tools"`
	Tables      []CatalogTable `json:"tables"`
}

// CatalogLimits mirrors the configured server limits.
type CatalogLimits struct {
	DefaultLimit    int `json:"default_limit"`
	MaxLimit        int `json:"max_limit"`
	MaxOffset       int `json:"max_offset"`
	MaxBatch        int `json:"max_batch"`
	MaxPreloadDepth int `json:"max_preload_depth"`
	MaxWriteRows    int `json:"max_write_rows"`
}

// CatalogTool is one MCP tool.
type CatalogTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CatalogTable is one table in the catalogue.
type CatalogTable struct {
	Table       string          `json:"table"`
	Description string          `json:"description,omitempty"`
	Purpose     string          `json:"purpose,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Operations  []string        `json:"operations"`
	PrimaryKey  string          `json:"primary_key,omitempty"`
	Columns     []CatalogColumn `json:"columns"`
	Relations   []string        `json:"relations,omitempty"`
}

// CatalogColumn is one column of a table.
type CatalogColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Nullable    bool   `json:"nullable"`
	PrimaryKey  bool   `json:"primary_key,omitempty"`
	Unique      bool   `json:"unique,omitempty"`
	Writable    bool   `json:"writable"`
	Description string `json:"description,omitempty"`
}

// modelDocs returns the effective documentation of a table. Registry info (including a
// loaded descriptions file) wins, then the model's ModelDescription().
func (h *Handler) modelDocs(schema, entity string) modelregistry.ModelInfo {
	if reg, ok := h.registry.(*modelregistry.DefaultModelRegistry); ok {
		return reg.ResolveModelInfo(buildModelName(schema, entity))
	}
	return modelregistry.ModelInfo{}
}

// columnDescription picks a column's description: the registry/file map first, then the
// struct-tag comment.
func columnDescription(docs modelregistry.ModelInfo, c columnInfo) string {
	if d := docs.Columns[c.jsonName]; d != "" {
		return d
	}
	return c.comment
}

// SetModelDescription stores documentation for a registered or soon-to-be-registered table.
// It returns an error when the handler's registry does not keep model info.
func (h *Handler) SetModelDescription(schema, entity string, info modelregistry.ModelInfo) error {
	reg, ok := h.registry.(*modelregistry.DefaultModelRegistry)
	if !ok {
		return fmt.Errorf("resolvemcp: registry does not support model descriptions (use NewHandlerWithGORM/Bun/DB)")
	}
	reg.SetModelInfo(buildModelName(schema, entity), info)
	return nil
}

// LoadModelDescriptions loads an external JSON map of descriptions keyed by "schema.entity"
// (see modelregistry.LoadModelInfo for the format) and returns how many tables it covered.
// Loaded entries override the model's own comments.
func (h *Handler) LoadModelDescriptions(path string) (int, error) {
	reg, ok := h.registry.(*modelregistry.DefaultModelRegistry)
	if !ok {
		return 0, fmt.Errorf("resolvemcp: registry does not support model descriptions (use NewHandlerWithGORM/Bun/DB)")
	}
	return reg.LoadModelInfoFile(path)
}

// BuildCatalog snapshots the server's tools and tables. Tables with no allowed operation are
// left out, exactly as list_tables does.
func (h *Handler) BuildCatalog() Catalog {
	cat := Catalog{
		GeneratedAt: time.Now().UTC(),
		Server:      h.name,
		Version:     h.version,
		ReadOnly:    h.config.readOnly,
		Guide:       guideFor(h.config.readOnly, h.config.AllowFunctionCalls),
		Limits: CatalogLimits{
			DefaultLimit:    h.config.DefaultLimit,
			MaxLimit:        h.config.MaxLimit,
			MaxOffset:       h.config.MaxOffset,
			MaxBatch:        h.config.MaxBatch,
			MaxPreloadDepth: h.config.MaxPreloadDepth,
			MaxWriteRows:    h.config.MaxWriteRows,
		},
		Tools:  []CatalogTool{},
		Tables: []CatalogTable{},
	}

	for name, tool := range h.mcpServer.ListTools() {
		cat.Tools = append(cat.Tools, CatalogTool{Name: name, Description: tool.Tool.Description})
	}
	sort.Slice(cat.Tools, func(i, j int) bool { return cat.Tools[i].Name < cat.Tools[j].Name })

	for name, model := range h.registry.GetAllModels() {
		schema, entity, _ := splitTable(name)
		rules := h.modelRules(schema, entity)
		ops := h.opsFor(rules)
		if len(ops) == 0 {
			continue
		}
		info := buildModelInfo(schema, entity, model)
		docs := h.modelDocs(schema, entity)

		writable := map[string]bool{}
		mt := reflect.TypeOf(model)
		for mt != nil && (mt.Kind() == reflect.Pointer || mt.Kind() == reflect.Slice) {
			mt = mt.Elem()
		}
		if !h.config.readOnly && mt != nil && mt.Kind() == reflect.Struct {
			for k := range reflectionJSONColumns(mt) {
				writable[k] = true
			}
		}

		t := CatalogTable{
			Table:       info.fullName,
			Description: docs.Description,
			Purpose:     docs.Purpose,
			Tags:        docs.Tags,
			Operations:  ops,
			PrimaryKey:  info.pkName,
			Relations:   info.relationNames,
			Columns:     make([]CatalogColumn, 0, len(info.columns)),
		}
		for _, c := range info.columns {
			typ := c.sqlType
			if typ == "" {
				typ = c.goType
			}
			t.Columns = append(t.Columns, CatalogColumn{
				Name: c.jsonName, Type: typ, Nullable: c.nullable, PrimaryKey: c.isPrimary,
				Unique: c.isUnique, Writable: writable[c.jsonName], Description: columnDescription(docs, c),
			})
		}
		cat.Tables = append(cat.Tables, t)
	}
	sort.Slice(cat.Tables, func(i, j int) bool { return cat.Tables[i].Table < cat.Tables[j].Table })
	return cat
}

// ExportCatalog writes the catalogue to path. A ".json" extension writes JSON; anything
// else writes Markdown. The file is replaced atomically (written to a temp file in the same
// directory, then renamed) and created with mode 0600. It lists every table the registry
// allows any operation on, regardless of caller, so keep it out of public directories.
func (h *Handler) ExportCatalog(path string) error {
	cat := h.BuildCatalog()

	var data []byte
	if strings.EqualFold(filepath.Ext(path), ".json") {
		b, err := json.MarshalIndent(cat, "", "  ")
		if err != nil {
			return err
		}
		data = b
		data = append(data, '\n')
	} else {
		data = []byte(cat.Markdown())
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("resolvemcp: export catalog: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".catalog-*")
	if err != nil {
		return fmt.Errorf("resolvemcp: export catalog: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmpName, path)
	}
	if werr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("resolvemcp: export catalog: %w", werr)
	}
	return nil
}

// Markdown renders the catalogue as a Markdown document.
func (c Catalog) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s API catalogue\n\nGenerated %s.\n\n", c.Server, c.GeneratedAt.Format(time.RFC3339))
	if c.ReadOnly {
		sb.WriteString("**This server is read-only.**\n\n")
	}
	sb.WriteString("## How to use\n\n" + c.Guide + "\n\n")
	fmt.Fprintf(&sb, "## Limits\n\ndefault limit %d, max limit %d, max offset %d, max batch %d, max preload depth %d, max rows per filter write %d.\n\n",
		c.Limits.DefaultLimit, c.Limits.MaxLimit, c.Limits.MaxOffset, c.Limits.MaxBatch, c.Limits.MaxPreloadDepth, c.Limits.MaxWriteRows)

	sb.WriteString("## Tools\n\n")
	for _, t := range c.Tools {
		fmt.Fprintf(&sb, "- `%s`: %s\n", t.Name, oneLine(t.Description))
	}

	sb.WriteString("\n## Tables\n\n")
	if len(c.Tables) == 0 {
		sb.WriteString("No tables are registered.\n")
	}
	for i := range c.Tables {
		t := &c.Tables[i]
		fmt.Fprintf(&sb, "### %s\n\n", t.Table)
		if t.Description != "" {
			sb.WriteString(t.Description + "\n\n")
		}
		if t.Purpose != "" {
			sb.WriteString("Purpose: " + t.Purpose + "\n\n")
		}
		if len(t.Tags) > 0 {
			sb.WriteString("Tags: " + strings.Join(t.Tags, ", ") + "\n\n")
		}
		fmt.Fprintf(&sb, "Operations: %s", strings.Join(t.Operations, ", "))
		if t.PrimaryKey != "" {
			fmt.Fprintf(&sb, " · Primary key: `%s`", t.PrimaryKey)
		}
		sb.WriteString("\n\n| Column | Type | Flags | Description |\n|---|---|---|---|\n")
		for _, col := range t.Columns {
			var flags []string
			if col.PrimaryKey {
				flags = append(flags, "pk")
			}
			if col.Unique {
				flags = append(flags, "unique")
			}
			if col.Nullable {
				flags = append(flags, "nullable")
			}
			if !col.Writable {
				flags = append(flags, "read-only")
			}
			fmt.Fprintf(&sb, "| `%s` | %s | %s | %s |\n", col.Name, mdCell(col.Type), strings.Join(flags, ", "), mdCell(col.Description))
		}
		if len(t.Relations) > 0 {
			sb.WriteString("\nRelations (preloadable): " + strings.Join(t.Relations, ", ") + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func mdCell(s string) string {
	return strings.ReplaceAll(oneLine(s), "|", `\|`)
}

// reflectionJSONColumns returns the JSON names of the columns a write may set.
func reflectionJSONColumns(t reflect.Type) map[string]string {
	return reflection.BuildJSONToDBColumnMap(t)
}
