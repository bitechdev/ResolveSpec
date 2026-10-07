package resolvemcp

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Operation names used by list_tables and the rule checks.
const (
	opSelect = "select"
	opInsert = "insert"
	opUpdate = "update"
	opDelete = "delete"
)

// filterOperators is the operator list shown by describe_table.
var filterOperators = []string{"=", "!=", ">", ">=", "<", "<=", "like", "ilike", "in", "is_null", "is_not_null"}

// registerMetaTools adds the fixed tool set. Their number does not grow with the models.
func registerMetaTools(h *Handler) {
	readOnly := mcp.WithReadOnlyHintAnnotation(true)
	tableArg := mcp.WithString("table", mcp.Required(), mcp.Description("Table as 'schema.entity' (see list_tables)."))
	filtersArg := mcp.WithArray("filters", mcp.Description(`Filter objects, e.g. [{"column":"status","operator":"=","value":"active"}]. Combine with "logic_operator": "AND" (default) or "OR". Operators: `+strings.Join(filterOperators, " ")+"."))
	idArg := mcp.WithString("id", mcp.Description("Primary key of one row. Use either id or filters."))
	dryRunArg := mcp.WithBoolean("dry_run", mcp.Description("Report how many rows match and a preview of their ids; change nothing."))
	confirmArg := mcp.WithString("confirm_token", mcp.Description("Token from the preview a filter-based call returns first; the write only happens with it."))

	h.mcpServer.AddTool(mcp.NewTool("list_tables", readOnly,
		mcp.WithDescription("List the tables you can use and the operations (select, insert, update, delete) allowed on each.")),
		h.handleListTables)

	h.mcpServer.AddTool(mcp.NewTool("describe_table", readOnly,
		mcp.WithDescription("Describe a table: columns and types, primary key, relations (preloadable), writable fields, allowed operations and server limits."),
		tableArg), h.handleDescribeTable)

	h.mcpServer.AddTool(mcp.NewTool("select_table", readOnly,
		mcp.WithDescription("Read rows from a table. Results are paged: 'limit' defaults to the server default and is capped; use cursor_forward/cursor_backward for deep paging. The total row count is only computed with include_count."),
		tableArg, idArg, filtersArg,
		mcp.WithArray("sort", mcp.Description(`Sort objects, e.g. [{"column":"created_at","direction":"desc"}].`)),
		mcp.WithArray("columns", mcp.Description("Columns to return. Omit for all.")),
		mcp.WithArray("omit_columns", mcp.Description("Columns to leave out.")),
		mcp.WithArray("preloads", mcp.Description(`Relations to load, e.g. [{"relation":"orders"}]. See describe_table for the names and the maximum depth.`)),
		mcp.WithNumber("limit", mcp.Description("Maximum rows to return.")),
		mcp.WithNumber("offset", mcp.Description("Rows to skip.")),
		mcp.WithString("cursor_forward", mcp.Description("Primary key of the last row of the current page; requires sort.")),
		mcp.WithString("cursor_backward", mcp.Description("Primary key of the first row of the current page; requires sort.")),
		mcp.WithBoolean("include_count", mcp.Description("Also return the total number of matching rows (slower on large tables).")),
	), h.handleSelect)

	if !h.config.ReadOnly {
		registerWriteTools(h, tableArg, idArg, filtersArg, dryRunArg, confirmArg)
	}
	if !h.config.ReadOnly || h.config.AllowFunctionCalls {
		registerFunctionTools(h, readOnly)
	}
}

// registerWriteTools adds the tools that change table rows.
func registerWriteTools(h *Handler, tableArg, idArg, filtersArg, dryRunArg, confirmArg mcp.ToolOption) {
	h.mcpServer.AddTool(mcp.NewTool("insert_into_table",
		mcp.WithDescription("Insert one row (object) or several rows (array, one transaction, capped). Unknown or read-only fields are rejected."),
		tableArg, mcp.WithObject("data", mcp.Required(), mcp.Description("A row object or an array of row objects.")),
	), h.handleInsert)

	h.mcpServer.AddTool(mcp.NewTool("update_table",
		mcp.WithDescription("Update rows. Give an id (one row, applied at once) or filters (several rows: the first call returns a preview and a confirm_token, repeat the call with the token to apply; the number of rows is capped). Only the fields in data are changed; null sets NULL."),
		tableArg, idArg, filtersArg,
		mcp.WithObject("data", mcp.Required(), mcp.Description("Fields to change.")),
		dryRunArg, confirmArg,
	), h.handleUpdate)

	h.mcpServer.AddTool(mcp.NewTool("delete_from_table",
		mcp.WithDescription("Delete rows. Give an id (one row, applied at once) or filters (several rows: the first call returns a preview and a confirm_token, repeat the call with the token to delete; the number of rows is capped)."),
		mcp.WithDestructiveHintAnnotation(true),
		tableArg, idArg, filtersArg, dryRunArg, confirmArg,
	), h.handleDelete)
}

// registerFunctionTools adds list_functions and call_function.
func registerFunctionTools(h *Handler, readOnly mcp.ToolOption) {
	h.mcpServer.AddTool(mcp.NewTool("list_functions", readOnly,
		mcp.WithDescription("List the functions you can call with call_function, with their parameters.")),
		h.handleListFunctions)

	h.mcpServer.AddTool(mcp.NewTool("call_function",
		mcp.WithDescription("Call a registered function by name with validated arguments (see list_functions). Runs in a transaction."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Function name.")),
		mcp.WithObject("arguments", mcp.Description("Arguments by parameter name.")),
	), h.handleCallFunction)
}

// --------------------------------------------------------------------------
// Tables and rules
// --------------------------------------------------------------------------

// splitTable parses "schema.entity" (or a bare entity).
func splitTable(table string) (schema, entity string, err error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return "", "", invalidArg("missing required argument: table")
	}
	if i := strings.LastIndex(table, "."); i >= 0 {
		return table[:i], table[i+1:], nil
	}
	return "", table, nil
}

// modelRules returns the rules of a registered model (defaults when the registry keeps none).
func (h *Handler) modelRules(schema, entity string) modelregistry.ModelRules {
	if reg, ok := h.registry.(*modelregistry.DefaultModelRegistry); ok {
		if r, err := reg.GetModelRules(buildModelName(schema, entity)); err == nil {
			return r
		}
	}
	return modelregistry.DefaultModelRules()
}

// opsFor lists the operations the rules allow. A read-only server allows select only.
func (h *Handler) opsFor(r modelregistry.ModelRules) []string {
	var ops []string
	if r.CanRead {
		ops = append(ops, opSelect)
	}
	if h.config.ReadOnly {
		return ops
	}
	if r.CanCreate {
		ops = append(ops, opInsert)
	}
	if r.CanUpdate {
		ops = append(ops, opUpdate)
	}
	if r.CanDelete {
		ops = append(ops, opDelete)
	}
	return ops
}

// resolveTable finds a registered model and checks the rule for op. A model the rules forbid
// for op is reported like a missing one for reads of the table list, but with a plain
// "not allowed" here so the agent learns the operation is off, not that it misspelled the name.
func (h *Handler) resolveTable(args map[string]any, op string) (schema, entity string, err error) {
	table, _ := args["table"].(string)
	schema, entity, err = splitTable(table)
	if err != nil {
		return "", "", err
	}
	if _, err := h.registry.GetModelByEntity(schema, entity); err != nil {
		return "", "", invalidArg("unknown table %q; see list_tables", truncate(table))
	}
	if op != "" && op != opSelect && h.config.ReadOnly {
		return "", "", NewClientError(CodeForbidden, "this server is read-only: writes are disabled")
	}
	if op != "" {
		allowed := false
		for _, o := range h.opsFor(h.modelRules(schema, entity)) {
			if o == op {
				allowed = true
			}
		}
		if !allowed {
			return "", "", NewClientError(CodeForbidden, fmt.Sprintf("%s is not allowed on %s", op, buildModelName(schema, entity)))
		}
	}
	return schema, entity, nil
}

func (h *Handler) handleListTables(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type table struct {
		Table       string   `json:"table"`
		Description string   `json:"description,omitempty"`
		Operations  []string `json:"operations"`
	}
	var tables []table
	for name := range h.registry.GetAllModels() {
		schema, entity, _ := splitTable(name)
		if ops := h.opsFor(h.modelRules(schema, entity)); len(ops) > 0 {
			tables = append(tables, table{Table: name, Description: h.modelDocs(schema, entity).Description, Operations: ops})
		}
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Table < tables[j].Table })
	return marshalResult(map[string]any{"success": true, "tables": tables})
}

func (h *Handler) handleDescribeTable(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	schema, entity, err := h.resolveTable(req.GetArguments(), "")
	if err != nil {
		return toolError("describe_table", err), nil
	}
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return toolError("describe_table", invalidArg("unknown table")), nil
	}
	rules := h.modelRules(schema, entity)
	if len(h.opsFor(rules)) == 0 {
		return toolError("describe_table", invalidArg("unknown table %q; see list_tables", buildModelName(schema, entity))), nil
	}
	info := buildModelInfo(schema, entity, model)
	docs := h.modelDocs(schema, entity)

	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice) {
		modelType = modelType.Elem()
	}
	writable := map[string]bool{}
	if !h.config.ReadOnly && modelType != nil && modelType.Kind() == reflect.Struct {
		for jsonKey := range reflection.BuildJSONToDBColumnMap(modelType) {
			writable[jsonKey] = true
		}
	}

	type column struct {
		Name       string `json:"name"`
		Type       string `json:"type,omitempty"`
		Nullable   bool   `json:"nullable"`
		PrimaryKey bool   `json:"primary_key,omitempty"`
		Unique     bool   `json:"unique,omitempty"`
		Writable   bool   `json:"writable"`
		Comment    string `json:"description,omitempty"`
	}
	cols := make([]column, 0, len(info.columns))
	var writableNames []string
	for _, c := range info.columns {
		typ := c.sqlType
		if typ == "" {
			typ = c.goType
		}
		w := writable[c.jsonName]
		cols = append(cols, column{Name: c.jsonName, Type: typ, Nullable: c.nullable, PrimaryKey: c.isPrimary, Unique: c.isUnique, Writable: w, Comment: columnDescription(docs, c)})
		if w && !c.isPrimary {
			writableNames = append(writableNames, c.jsonName)
		}
	}
	return marshalResult(map[string]any{
		"success":          true,
		"table":            info.fullName,
		"description":      docs.Description,
		"purpose":          docs.Purpose,
		"tags":             docs.Tags,
		"primary_key":      info.pkName,
		"columns":          cols,
		"relations":        info.relationNames,
		"writable_columns": writableNames,
		"operations":       h.opsFor(rules),
		"read_only":        h.config.ReadOnly,
		"filter_operators": filterOperators,
		"limits": map[string]any{
			"default_limit":     h.config.DefaultLimit,
			"max_limit":         h.config.MaxLimit,
			"max_offset":        h.config.MaxOffset,
			"max_batch":         h.config.MaxBatch,
			"max_preload_depth": h.config.MaxPreloadDepth,
			"max_write_rows":    h.config.MaxWriteRows,
		},
	})
}

// --------------------------------------------------------------------------
// Row tools
// --------------------------------------------------------------------------

// argID reads the id argument, which clients send as a string or a number.
func argID(args map[string]any) string {
	switch v := args["id"].(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprint(v)
	}
	return ""
}

// parseFiltersStrict is parseFilters for writes: a malformed filter is an error, never dropped.
func parseFiltersStrict(raw any) ([]common.FilterOption, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, invalidArg("filters must be an array")
	}
	parsed := parseFilters(raw)
	if len(parsed) != len(items) {
		return nil, invalidArg("every filter needs a column and an operator")
	}
	return parsed, nil
}

func (h *Handler) handleSelect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, entity, err := h.resolveTable(args, opSelect)
	if err != nil {
		return toolError("select_table", err), nil
	}
	count, _ := args["include_count"].(bool)
	data, meta, err := h.executeReadCounted(ctx, schema, entity, argID(args), parseRequestOptions(args), count)
	if err != nil {
		return toolError("select_table", err), nil
	}
	if !count && meta != nil {
		meta.Total, meta.Filtered = 0, 0
	}
	return marshalResult(map[string]any{"success": true, "data": data, "metadata": meta})
}

func (h *Handler) handleInsert(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, entity, err := h.resolveTable(args, opInsert)
	if err != nil {
		return toolError("insert_into_table", err), nil
	}
	data, ok := args["data"]
	if !ok {
		return toolError("insert_into_table", invalidArg("missing required argument: data")), nil
	}
	result, err := h.executeCreate(ctx, schema, entity, data)
	if err != nil {
		return toolError("insert_into_table", err), nil
	}
	return marshalResult(map[string]any{"success": true, "data": result})
}

// writeTarget parses id/filters/dry_run/confirm_token shared by update and delete.
func writeTarget(args map[string]any) (id string, filters []common.FilterOption, dryRun bool, token string, err error) {
	id = argID(args)
	if filters, err = parseFiltersStrict(args["filters"]); err != nil {
		return
	}
	if id != "" && len(filters) > 0 {
		err = invalidArg("use either id or filters, not both")
		return
	}
	if id == "" && len(filters) == 0 {
		err = invalidArg("provide an id or at least one filter")
		return
	}
	dryRun, _ = args["dry_run"].(bool)
	token, _ = args["confirm_token"].(string)
	return
}

func (h *Handler) handleUpdate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, entity, err := h.resolveTable(args, opUpdate)
	if err != nil {
		return toolError("update_table", err), nil
	}
	data, ok := args["data"].(map[string]any)
	if !ok {
		return toolError("update_table", invalidArg("data must be an object")), nil
	}
	id, filters, dryRun, token, err := writeTarget(args)
	if err != nil {
		return toolError("update_table", err), nil
	}
	if id != "" && !dryRun {
		result, err := h.executeUpdate(ctx, schema, entity, id, data)
		if err != nil {
			return toolError("update_table", err), nil
		}
		return marshalResult(map[string]any{"success": true, "data": result})
	}
	return h.runWhere(ctx, "update_table", whereRequest{schema: schema, entity: entity, op: "update", filters: h.idFilters(schema, entity, id, filters), data: data, dryRun: dryRun, confirmToken: token})
}

func (h *Handler) handleDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schema, entity, err := h.resolveTable(args, opDelete)
	if err != nil {
		return toolError("delete_from_table", err), nil
	}
	id, filters, dryRun, token, err := writeTarget(args)
	if err != nil {
		return toolError("delete_from_table", err), nil
	}
	if id != "" && !dryRun {
		result, err := h.executeDelete(ctx, schema, entity, id)
		if err != nil {
			return toolError("delete_from_table", err), nil
		}
		return marshalResult(map[string]any{"success": true, "data": result})
	}
	return h.runWhere(ctx, "delete_from_table", whereRequest{schema: schema, entity: entity, op: "delete", filters: h.idFilters(schema, entity, id, filters), dryRun: dryRun, confirmToken: token})
}

// idFilters turns an id into a primary-key filter so a dry run of an id write goes through the
// same matching as a filter write.
func (h *Handler) idFilters(schema, entity, id string, filters []common.FilterOption) []common.FilterOption {
	if id == "" {
		return filters
	}
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		return filters
	}
	return []common.FilterOption{{Column: reflection.GetPrimaryKeyName(model), Operator: "eq", Value: id, LogicOperator: "AND"}}
}

func (h *Handler) runWhere(ctx context.Context, op string, req whereRequest) (*mcp.CallToolResult, error) {
	res, err := h.executeWhere(ctx, req)
	if err != nil {
		return toolError(op, err), nil
	}
	return marshalResult(map[string]any{"success": true, "result": res})
}

// --------------------------------------------------------------------------
// Functions
// --------------------------------------------------------------------------

func (h *Handler) handleListFunctions(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type param struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Required    bool   `json:"required"`
		Description string `json:"description,omitempty"`
	}
	type fn struct {
		Name        string  `json:"name"`
		Description string  `json:"description,omitempty"`
		Parameters  []param `json:"parameters"`
	}
	out := []fn{}
	for _, f := range h.visibleFunctions(ctx) {
		item := fn{Name: f.Name, Description: f.Description, Parameters: []param{}}
		for _, p := range f.Params {
			item.Parameters = append(item.Parameters, param{p.Name, p.Type, p.Required, p.Description})
		}
		out = append(out, item)
	}
	return marshalResult(map[string]any{"success": true, "functions": out})
}

func (h *Handler) handleCallFunction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		return toolError("call_function", invalidArg("missing required argument: name")), nil
	}
	fnArgs := map[string]any{}
	if raw, ok := args["arguments"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return toolError("call_function", invalidArg("arguments must be an object")), nil
		}
		fnArgs = m
	}
	result, err := h.executeCall(ctx, name, fnArgs)
	if err != nil {
		return toolError("call_function", err), nil
	}
	return marshalResult(map[string]any{"success": true, "result": result})
}
