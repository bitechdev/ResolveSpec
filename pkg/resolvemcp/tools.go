package resolvemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
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
	registerReadTool(h, schema, entity)
	registerCreateTool(h, schema, entity)
	registerUpdateTool(h, schema, entity)
	registerDeleteTool(h, schema, entity)
	registerModelResource(h, schema, entity)

	logger.Info("[resolvemcp] Registered MCP tools for %s.%s", schema, entity)
}

// --------------------------------------------------------------------------
// Read tool
// --------------------------------------------------------------------------

func registerReadTool(h *Handler, schema, entity string) {
	name := toolName("read", schema, entity)
	description := fmt.Sprintf("Read records from %s", buildModelName(schema, entity))

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description("Primary key of a single record to fetch (optional)"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of records to return"),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of records to skip"),
		),
		mcp.WithString("cursor_forward",
			mcp.Description("Cursor value for the next page (primary key of last record on current page)"),
		),
		mcp.WithString("cursor_backward",
			mcp.Description("Cursor value for the previous page"),
		),
		mcp.WithArray("columns",
			mcp.Description("List of column names to include in the result"),
		),
		mcp.WithArray("omit_columns",
			mcp.Description("List of column names to exclude from the result"),
		),
		mcp.WithArray("filters",
			mcp.Description(`Array of filter objects. Each object: {"column":"name","operator":"=","value":"val","logic_operator":"AND|OR"}. Operators: =, !=, >, <, >=, <=, like, ilike, in, is_null, is_not_null`),
		),
		mcp.WithArray("sort",
			mcp.Description(`Array of sort objects. Each object: {"column":"name","direction":"asc|desc"}`),
		),
		mcp.WithArray("preloads",
			mcp.Description(`Array of relation preload objects. Each object: {"relation":"RelationName","columns":["col1"]}`),
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

// --------------------------------------------------------------------------
// Create tool
// --------------------------------------------------------------------------

func registerCreateTool(h *Handler, schema, entity string) {
	name := toolName("create", schema, entity)
	description := fmt.Sprintf("Create one or more records in %s", buildModelName(schema, entity))

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithObject("data",
			mcp.Description("Record fields to create (single object), or pass an array as the 'items' key"),
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

func registerUpdateTool(h *Handler, schema, entity string) {
	name := toolName("update", schema, entity)
	description := fmt.Sprintf("Update an existing record in %s", buildModelName(schema, entity))

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description("Primary key of the record to update"),
		),
		mcp.WithObject("data",
			mcp.Description("Fields to update (non-null fields will be merged into the existing record)"),
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

func registerDeleteTool(h *Handler, schema, entity string) {
	name := toolName("delete", schema, entity)
	description := fmt.Sprintf("Delete a record from %s by primary key", buildModelName(schema, entity))

	tool := mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("id",
			mcp.Description("Primary key of the record to delete"),
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

func registerModelResource(h *Handler, schema, entity string) {
	resourceURI := buildModelName(schema, entity)
	displayName := entity
	if schema != "" {
		displayName = schema + "." + entity
	}

	resource := mcp.NewResource(
		resourceURI,
		displayName,
		mcp.WithResourceDescription(fmt.Sprintf("Database table: %s", displayName)),
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

	// limit
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit := int(n)
			options.Limit = &limit
		case int:
			options.Limit = &n
		}
	}

	// offset
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset := int(n)
			options.Offset = &offset
		case int:
			options.Offset = &n
		}
	}

	// cursor_forward / cursor_backward
	if v, ok := args["cursor_forward"].(string); ok {
		options.CursorForward = v
	}
	if v, ok := args["cursor_backward"].(string); ok {
		options.CursorBackward = v
	}

	// columns
	options.Columns = parseStringArray(args["columns"])

	// omit_columns
	options.OmitColumns = parseStringArray(args["omit_columns"])

	// filters — marshal each item and unmarshal into FilterOption
	options.Filters = parseFilters(args["filters"])

	// sort
	options.Sort = parseSortOptions(args["sort"])

	// preloads
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
		// Normalise logic operator
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
