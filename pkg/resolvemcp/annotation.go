package resolvemcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

const annotationToolName = "resolvespec_annotate"

// registerAnnotationTool adds the resolvespec_annotate tool to the MCP server.
// The tool lets models/entities store and retrieve freeform annotation records
// using the resolvespec_set_annotation / resolvespec_get_annotation database procedures.
func registerAnnotationTool(h *Handler) {
	tool := mcp.NewTool(annotationToolName,
		mcp.WithDescription(
			"Store or retrieve annotations for any MCP tool, model, or entity.\n\n"+
				"To set annotations: provide both 'tool_name' and 'annotations'. "+
				"Calls resolvespec_set_annotation(tool_name, annotations) to persist the data.\n\n"+
				"To get annotations: provide only 'tool_name'. "+
				"Calls resolvespec_get_annotation(tool_name) and returns the stored annotations.\n\n"+
				"'tool_name' may be any identifier: an MCP tool name (e.g. 'read_public_users'), "+
				"a model/entity name (e.g. 'public.users'), or any other key.",
		),
		mcp.WithString("tool_name",
			mcp.Description("Name of the tool, model, or entity to annotate (e.g. 'read_public_users', 'public.users')."),
			mcp.Required(),
		),
		mcp.WithObject("annotations",
			mcp.Description("Annotation data to store. Omit to retrieve existing annotations instead of setting them."),
		),
	)

	h.mcpServer.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		toolName, ok := args["tool_name"].(string)
		if !ok || toolName == "" {
			return mcp.NewToolResultError("missing required argument: tool_name"), nil
		}

		annotations, hasAnnotations := args["annotations"]

		if hasAnnotations && annotations != nil {
			return executeSetAnnotation(ctx, h, toolName, annotations)
		}
		return executeGetAnnotation(ctx, h, toolName)
	})
}

func executeSetAnnotation(ctx context.Context, h *Handler, toolName string, annotations interface{}) (*mcp.CallToolResult, error) {
	jsonBytes, err := json.Marshal(annotations)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal annotations: %v", err)), nil
	}

	_, err = h.db.Exec(ctx, "SELECT resolvespec_set_annotation($1, $2)", toolName, string(jsonBytes))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to set annotation: %v", err)), nil
	}

	return marshalResult(map[string]interface{}{
		"success":   true,
		"tool_name": toolName,
		"action":    "set",
	})
}

func executeGetAnnotation(ctx context.Context, h *Handler, toolName string) (*mcp.CallToolResult, error) {
	var rows []map[string]interface{}
	err := h.db.Query(ctx, &rows, "SELECT resolvespec_get_annotation($1)", toolName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get annotation: %v", err)), nil
	}

	var annotations interface{}
	if len(rows) > 0 {
		// The procedure returns a single value; extract the first column of the first row.
		for _, v := range rows[0] {
			annotations = v
			break
		}
	}

	// If the value is a []byte or string containing JSON, decode it so it round-trips cleanly.
	switch v := annotations.(type) {
	case []byte:
		var decoded interface{}
		if json.Unmarshal(v, &decoded) == nil {
			annotations = decoded
		}
	case string:
		var decoded interface{}
		if json.Unmarshal([]byte(v), &decoded) == nil {
			annotations = decoded
		}
	}

	return marshalResult(map[string]interface{}{
		"success":     true,
		"tool_name":   toolName,
		"action":      "get",
		"annotations": annotations,
	})
}
