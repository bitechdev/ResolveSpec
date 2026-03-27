package resolvemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// toolName builds the MCP tool name for a given operation and model.
func toolName(operation, schema, entity string) string {
	if schema == "" {
		return fmt.Sprintf("%s_%s", operation, entity)
	}
	return fmt.Sprintf("%s_%s_%s", operation, schema, entity)
}

// registerModelTools registers the four CRUD tools and resource for a model.
func registerModelTools(h *Handler, schema, entity string, model interface{}) {
	info := buildModelInfo(schema, entity, model)
	registerReadTool(h, schema, entity, info)
	registerCreateTool(h, schema, entity, info)
	registerUpdateTool(h, schema, entity, info)
	registerDeleteTool(h, schema, entity, info)
	registerModelResource(h, schema, entity, info)

	logger.Info("[resolvemcp] Registered MCP tools for %s", info.fullName)
}

// --------------------------------------------------------------------------
// Model introspection
// --------------------------------------------------------------------------

// modelInfo holds pre-computed metadata for a model used in tool descriptions.
type modelInfo struct {
	fullName      string // e.g. "public.users"
	pkName        string // e.g. "id"
	columns       []columnInfo
	relationNames []string
	schemaDoc     string // formatted multi-line schema listing
}

type columnInfo struct {
	jsonName  string
	sqlName   string
	goType    string
	sqlType   string
	isPrimary bool
	isUnique  bool
	isFK      bool
	nullable  bool
}

// buildModelInfo extracts column metadata and pre-builds the schema documentation string.
func buildModelInfo(schema, entity string, model interface{}) modelInfo {
	info := modelInfo{
		fullName: buildModelName(schema, entity),
		pkName:   reflection.GetPrimaryKeyName(model),
	}

	// Unwrap to base struct type
	modelType := reflect.TypeOf(model)
	for modelType != nil && (modelType.Kind() == reflect.Ptr || modelType.Kind() == reflect.Slice) {
		modelType = modelType.Elem()
	}
	if modelType == nil || modelType.Kind() != reflect.Struct {
		return info
	}

	details := reflection.GetModelColumnDetail(reflect.New(modelType).Elem())

	for _, d := range details {
		// Derive the JSON name from the struct field
		jsonName := fieldJSONName(modelType, d.Name)
		if jsonName == "" || jsonName == "-" {
			continue
		}

		// Skip relation fields (slice or user-defined struct that isn't time.Time).
		fieldType, found := modelType.FieldByName(d.Name)
		if found {
			ft := fieldType.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			isUserStruct := ft.Kind() == reflect.Struct && ft.Name() != "Time" && ft.PkgPath() != ""
			if ft.Kind() == reflect.Slice || isUserStruct {
				info.relationNames = append(info.relationNames, jsonName)
				continue
			}
		}

		sqlName := d.SQLName
		if sqlName == "" {
			sqlName = jsonName
		}

		// Derive Go type name, unwrapping pointer if needed.
		goType := d.DataType
		if goType == "" && found {
			ft := fieldType.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			goType = ft.Name()
		}

		// isPrimary: use both the GORM-tag detection and a name comparison against
		// the known primary key (handles camelCase "primaryKey" tags correctly).
		isPrimary := d.SQLKey == "primary_key" ||
			(info.pkName != "" && (sqlName == info.pkName || jsonName == info.pkName))

		ci := columnInfo{
			jsonName:  jsonName,
			sqlName:   sqlName,
			goType:    goType,
			sqlType:   d.SQLDataType,
			isPrimary: isPrimary,
			isUnique:  d.SQLKey == "unique" || d.SQLKey == "uniqueindex",
			isFK:      d.SQLKey == "foreign_key",
			nullable:  d.Nullable,
		}
		info.columns = append(info.columns, ci)
	}

	info.schemaDoc = buildSchemaDoc(info)
	return info
}

// fieldJSONName returns the JSON tag name for a struct field, falling back to the field name.
func fieldJSONName(modelType reflect.Type, fieldName string) string {
	field, ok := modelType.FieldByName(fieldName)
	if !ok {
		return fieldName
	}
	tag := field.Tag.Get("json")
	if tag == "" {
		return fieldName
	}
	parts := strings.SplitN(tag, ",", 2)
	if parts[0] == "" {
		return fieldName
	}
	return parts[0]
}

// buildSchemaDoc builds a human-readable column listing for inclusion in tool descriptions.
func buildSchemaDoc(info modelInfo) string {
	if len(info.columns) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Columns:\n")
	for _, c := range info.columns {
		line := fmt.Sprintf("  • %s", c.jsonName)

		typeDesc := c.goType
		if c.sqlType != "" {
			typeDesc = c.sqlType
		}
		if typeDesc != "" {
			line += fmt.Sprintf(" (%s)", typeDesc)
		}

		var flags []string
		if c.isPrimary {
			flags = append(flags, "primary key")
		}
		if c.isUnique {
			flags = append(flags, "unique")
		}
		if c.isFK {
			flags = append(flags, "foreign key")
		}
		if !c.nullable && !c.isPrimary {
			flags = append(flags, "not null")
		} else if c.nullable {
			flags = append(flags, "nullable")
		}
		if len(flags) > 0 {
			line += " — " + strings.Join(flags, ", ")
		}

		sb.WriteString(line + "\n")
	}

	if len(info.relationNames) > 0 {
		sb.WriteString("Relations (preloadable): " + strings.Join(info.relationNames, ", ") + "\n")
	}

	return sb.String()
}

// columnNameList returns a comma-separated list of JSON column names (for descriptions).
func columnNameList(cols []columnInfo) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.jsonName
	}
	return strings.Join(names, ", ")
}

// writableColumnNames returns JSON names for all non-primary-key columns.
func writableColumnNames(cols []columnInfo) []string {
	var names []string
	for _, c := range cols {
		if !c.isPrimary {
			names = append(names, c.jsonName)
		}
	}
	return names
}

// --------------------------------------------------------------------------
// Read tool
// --------------------------------------------------------------------------

func registerReadTool(h *Handler, schema, entity string, info modelInfo) {
	name := toolName("read", schema, entity)

	var descParts []string
	descParts = append(descParts, fmt.Sprintf("Read records from the '%s' database table.", info.fullName))
	if info.pkName != "" {
		descParts = append(descParts, fmt.Sprintf("Primary key: '%s'. Pass it via 'id' to fetch a single record.", info.pkName))
	}
	if info.schemaDoc != "" {
		descParts = append(descParts, info.schemaDoc)
	}
	descParts = append(descParts,
		"Pagination: use 'limit'/'offset' for offset-based paging, or 'cursor_forward'/'cursor_backward' (pass the primary key value of the last/first record on the current page) for cursor-based paging.",
		"Filtering: each filter object requires 'column' (JSON field name) and 'operator'. Supported operators: = != > < >= <= like ilike in is_null is_not_null. Combine with 'logic_operator': AND (default) or OR.",
		"Sorting: each sort object requires 'column' and 'direction' (asc or desc).",
	)
	if len(info.relationNames) > 0 {
		descParts = append(descParts, fmt.Sprintf("Preloadable relations: %s. Pass relation name in 'preloads'.", strings.Join(info.relationNames, ", ")))
	}

	description := strings.Join(descParts, "\n\n")

	filterDesc := fmt.Sprintf(`Array of filter objects. Example: [{"column":"status","operator":"=","value":"active"},{"column":"age","operator":">","value":18,"logic_operator":"AND"}]`)
	if len(info.columns) > 0 {
		filterDesc += fmt.Sprintf(" Available columns: %s.", columnNameList(info.columns))
	}

	sortDesc := `Array of sort objects. Example: [{"column":"created_at","direction":"desc"}]`
	if len(info.columns) > 0 {
		sortDesc += fmt.Sprintf(" Available columns: %s.", columnNameList(info.columns))
	}

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description(fmt.Sprintf("Primary key (%s) of a single record to fetch. Omit to return multiple records.", info.pkName)),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of records to return per page. Recommended: 10–100."),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of records to skip (for offset-based pagination). Use with 'limit'."),
		),
		mcp.WithString("cursor_forward",
			mcp.Description(fmt.Sprintf("Cursor for the next page: pass the '%s' value of the last record on the current page. Requires 'sort' to be set.", info.pkName)),
		),
		mcp.WithString("cursor_backward",
			mcp.Description(fmt.Sprintf("Cursor for the previous page: pass the '%s' value of the first record on the current page. Requires 'sort' to be set.", info.pkName)),
		),
		mcp.WithArray("columns",
			mcp.Description(fmt.Sprintf("Columns to include in the result. Omit to return all columns. Available: %s.", columnNameList(info.columns))),
		),
		mcp.WithArray("omit_columns",
			mcp.Description(fmt.Sprintf("Columns to exclude from the result. Available: %s.", columnNameList(info.columns))),
		),
		mcp.WithArray("filters",
			mcp.Description(filterDesc),
		),
		mcp.WithArray("sort",
			mcp.Description(sortDesc),
		),
		mcp.WithArray("preloads",
			mcp.Description(buildPreloadDesc(info)),
		),
	)

	h.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)
		options := parseRequestOptions(args)

		data, metadata, err := h.executeRead(ctx, schema, entity, id, options)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return marshalResult(map[string]interface{}{
			"success":  true,
			"data":     data,
			"metadata": metadata,
		})
	})
}

func buildPreloadDesc(info modelInfo) string {
	if len(info.relationNames) == 0 {
		return `Array of relation preload objects. Each object: {"relation":"RelationName"}. No relations defined on this model.`
	}
	return fmt.Sprintf(
		`Array of relation preload objects. Each object: {"relation":"RelationName","columns":["col1","col2"]}. Available relations: %s.`,
		strings.Join(info.relationNames, ", "),
	)
}

// --------------------------------------------------------------------------
// Create tool
// --------------------------------------------------------------------------

func registerCreateTool(h *Handler, schema, entity string, info modelInfo) {
	name := toolName("create", schema, entity)

	writable := writableColumnNames(info.columns)

	var descParts []string
	descParts = append(descParts, fmt.Sprintf("Create one or more new records in the '%s' table.", info.fullName))
	if len(writable) > 0 {
		descParts = append(descParts, fmt.Sprintf("Writable fields: %s.", strings.Join(writable, ", ")))
	}
	if info.pkName != "" {
		descParts = append(descParts, fmt.Sprintf("The primary key ('%s') is typically auto-generated — omit it unless you need to supply it explicitly.", info.pkName))
	}
	descParts = append(descParts,
		"Pass a single JSON object to 'data' to create one record. Pass an array of objects to create multiple records in a single transaction (all succeed or all fail).",
	)
	if info.schemaDoc != "" {
		descParts = append(descParts, info.schemaDoc)
	}

	description := strings.Join(descParts, "\n\n")

	dataDesc := "Record fields to create."
	if len(writable) > 0 {
		dataDesc += fmt.Sprintf(" Writable fields: %s.", strings.Join(writable, ", "))
	}
	dataDesc += " Pass a single object or an array of objects."

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithObject("data",
			mcp.Description(dataDesc),
			mcp.Required(),
		),
	)

	h.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		data, ok := args["data"]
		if !ok {
			return mcp.NewToolResultError("missing required argument: data"), nil
		}

		result, err := h.executeCreate(ctx, schema, entity, data)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return marshalResult(map[string]interface{}{
			"success": true,
			"data":    result,
		})
	})
}

// --------------------------------------------------------------------------
// Update tool
// --------------------------------------------------------------------------

func registerUpdateTool(h *Handler, schema, entity string, info modelInfo) {
	name := toolName("update", schema, entity)

	writable := writableColumnNames(info.columns)

	var descParts []string
	descParts = append(descParts, fmt.Sprintf("Update an existing record in the '%s' table.", info.fullName))
	if info.pkName != "" {
		descParts = append(descParts, fmt.Sprintf("Identify the record by its primary key ('%s') via the 'id' argument or by including '%s' inside 'data'.", info.pkName, info.pkName))
	}
	if len(writable) > 0 {
		descParts = append(descParts, fmt.Sprintf("Updatable fields: %s.", strings.Join(writable, ", ")))
	}
	descParts = append(descParts,
		"Only non-null, non-empty fields in 'data' are applied — existing values are preserved for fields you omit. Returns the merged record as stored.",
	)
	if info.schemaDoc != "" {
		descParts = append(descParts, info.schemaDoc)
	}

	description := strings.Join(descParts, "\n\n")

	idDesc := fmt.Sprintf("Primary key ('%s') of the record to update. Can also be included inside 'data'.", info.pkName)

	dataDesc := "Fields to update (non-null, non-empty values are merged into the existing record)."
	if len(writable) > 0 {
		dataDesc += fmt.Sprintf(" Updatable fields: %s.", strings.Join(writable, ", "))
	}

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description(idDesc),
		),
		mcp.WithObject("data",
			mcp.Description(dataDesc),
			mcp.Required(),
		),
	)

	h.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)

		data, ok := args["data"]
		if !ok {
			return mcp.NewToolResultError("missing required argument: data"), nil
		}
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("data must be an object"), nil
		}

		result, err := h.executeUpdate(ctx, schema, entity, id, dataMap)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return marshalResult(map[string]interface{}{
			"success": true,
			"data":    result,
		})
	})
}

// --------------------------------------------------------------------------
// Delete tool
// --------------------------------------------------------------------------

func registerDeleteTool(h *Handler, schema, entity string, info modelInfo) {
	name := toolName("delete", schema, entity)

	descParts := []string{
		fmt.Sprintf("Delete a record from the '%s' table by its primary key.", info.fullName),
	}
	if info.pkName != "" {
		descParts = append(descParts, fmt.Sprintf("Pass the '%s' value of the record to delete via the 'id' argument.", info.pkName))
	}
	descParts = append(descParts, "Returns the deleted record. This operation is irreversible.")

	description := strings.Join(descParts, " ")

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description(fmt.Sprintf("Primary key ('%s') of the record to delete.", info.pkName)),
			mcp.Required(),
		),
	)

	h.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		id, _ := args["id"].(string)

		result, err := h.executeDelete(ctx, schema, entity, id)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return marshalResult(map[string]interface{}{
			"success": true,
			"data":    result,
		})
	})
}

// --------------------------------------------------------------------------
// Resource registration
// --------------------------------------------------------------------------

func registerModelResource(h *Handler, schema, entity string, info modelInfo) {
	resourceURI := info.fullName

	var resourceDesc strings.Builder
	resourceDesc.WriteString(fmt.Sprintf("Database table: %s", info.fullName))
	if info.pkName != "" {
		resourceDesc.WriteString(fmt.Sprintf(" (primary key: %s)", info.pkName))
	}
	if info.schemaDoc != "" {
		resourceDesc.WriteString("\n\n")
		resourceDesc.WriteString(info.schemaDoc)
	}

	resource := mcp.NewResource(
		resourceURI,
		entity,
		mcp.WithResourceDescription(resourceDesc.String()),
		mcp.WithMIMEType("application/json"),
	)

	h.mcpServer.AddResource(resource, func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		limit := 100
		options := common.RequestOptions{Limit: &limit}

		data, metadata, err := h.executeRead(ctx, schema, entity, "", options)
		if err != nil {
			return nil, err
		}

		payload := map[string]interface{}{
			"data":     data,
			"metadata": metadata,
		}
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("error marshaling resource: %w", err)
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(jsonBytes),
			},
		}, nil
	})
}

// --------------------------------------------------------------------------
// Argument parsing helpers
// --------------------------------------------------------------------------

// parseRequestOptions converts raw MCP tool arguments into common.RequestOptions.
func parseRequestOptions(args map[string]interface{}) common.RequestOptions {
	options := common.RequestOptions{}

	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit := int(n)
			options.Limit = &limit
		case int:
			options.Limit = &n
		}
	}

	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset := int(n)
			options.Offset = &offset
		case int:
			options.Offset = &n
		}
	}

	if v, ok := args["cursor_forward"].(string); ok {
		options.CursorForward = v
	}
	if v, ok := args["cursor_backward"].(string); ok {
		options.CursorBackward = v
	}

	options.Columns = parseStringArray(args["columns"])
	options.OmitColumns = parseStringArray(args["omit_columns"])
	options.Filters = parseFilters(args["filters"])
	options.Sort = parseSortOptions(args["sort"])
	options.Preload = parsePreloadOptions(args["preloads"])

	return options
}

func parseStringArray(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func parseFilters(raw interface{}) []common.FilterOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.FilterOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var f common.FilterOption
		if err := json.Unmarshal(b, &f); err != nil {
			continue
		}
		if f.Column == "" || f.Operator == "" {
			continue
		}
		if strings.EqualFold(f.LogicOperator, "or") {
			f.LogicOperator = "OR"
		} else {
			f.LogicOperator = "AND"
		}
		result = append(result, f)
	}
	return result
}

func parseSortOptions(raw interface{}) []common.SortOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.SortOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var s common.SortOption
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		if s.Column == "" {
			continue
		}
		result = append(result, s)
	}
	return result
}

func parsePreloadOptions(raw interface{}) []common.PreloadOption {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]common.PreloadOption, 0, len(items))
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var p common.PreloadOption
		if err := json.Unmarshal(b, &p); err != nil {
			continue
		}
		if p.Relation == "" {
			continue
		}
		result = append(result, p)
	}
	return result
}

// marshalResult marshals a value to JSON and returns it as an MCP text result.
func marshalResult(v interface{}) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("error marshaling result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
