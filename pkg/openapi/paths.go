package openapi

import (
	"fmt"
)

// generateRestheadSpecPaths generates OpenAPI paths for RestheadSpec endpoints
func (g *Generator) generateRestheadSpecPaths(spec *OpenAPISpec, schema, entity, schemaName string) {
	basePath := fmt.Sprintf("/%s/%s", schema, entity)
	idPath := fmt.Sprintf("/%s/%s/{id}", schema, entity)
	metaPath := fmt.Sprintf("/%s/%s/metadata", schema, entity)

	// Collection endpoint: GET (list), POST (create)
	spec.Paths[basePath] = PathItem{
		Get: &Operation{
			Summary:     fmt.Sprintf("List %s records", entity),
			Description: fmt.Sprintf("Retrieve a list of %s records with optional filtering, sorting, and pagination via headers", entity),
			OperationID: fmt.Sprintf("listRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Parameters:  g.getRestheadSpecHeaders(),
			Responses: map[string]Response{
				"200": {
					Description: "Successful response",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success":  {Type: "boolean"},
									"data":     {Type: "array", Items: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)}},
									"metadata": {Ref: "#/components/schemas/Metadata"},
								},
							},
						},
					},
				},
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Post: &Operation{
			Summary:     fmt.Sprintf("Create %s record", entity),
			Description: fmt.Sprintf("Create a new %s record", entity),
			OperationID: fmt.Sprintf("createRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			RequestBody: &RequestBody{
				Required:    true,
				Description: fmt.Sprintf("%s object to create", entity),
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
					},
				},
			},
			Responses: map[string]Response{
				"201": {
					Description: "Record created successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data":    {Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
								},
							},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Options: &Operation{
			Summary:     "CORS preflight",
			Description: "Handle CORS preflight requests",
			OperationID: fmt.Sprintf("optionsRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Responses: map[string]Response{
				"204": {Description: "No content"},
			},
		},
	}

	// Single record endpoint: GET (read), PUT/PATCH (update), DELETE
	spec.Paths[idPath] = PathItem{
		Get: &Operation{
			Summary:     fmt.Sprintf("Get %s record by ID", entity),
			Description: fmt.Sprintf("Retrieve a single %s record by its ID", entity),
			OperationID: fmt.Sprintf("getRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Description: "Record ID", Schema: &Schema{Type: "integer"}},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Successful response",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data":    {Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
								},
							},
						},
					},
				},
				"404": g.errorResponse("Record not found"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Put: &Operation{
			Summary:     fmt.Sprintf("Update %s record", entity),
			Description: fmt.Sprintf("Update an existing %s record by ID", entity),
			OperationID: fmt.Sprintf("updateRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Description: "Record ID", Schema: &Schema{Type: "integer"}},
			},
			RequestBody: &RequestBody{
				Required:    true,
				Description: fmt.Sprintf("Updated %s object", entity),
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
					},
				},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Record updated successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data":    {Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
								},
							},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"404": g.errorResponse("Record not found"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Patch: &Operation{
			Summary:     fmt.Sprintf("Partially update %s record", entity),
			Description: fmt.Sprintf("Partially update an existing %s record by ID", entity),
			OperationID: fmt.Sprintf("patchRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Description: "Record ID", Schema: &Schema{Type: "integer"}},
			},
			RequestBody: &RequestBody{
				Required:    true,
				Description: fmt.Sprintf("Partial %s object", entity),
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
					},
				},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Record updated successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data":    {Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
								},
							},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"404": g.errorResponse("Record not found"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Delete: &Operation{
			Summary:     fmt.Sprintf("Delete %s record", entity),
			Description: fmt.Sprintf("Delete a %s record by ID", entity),
			OperationID: fmt.Sprintf("deleteRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Description: "Record ID", Schema: &Schema{Type: "integer"}},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Record deleted successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
								},
							},
						},
					},
				},
				"404": g.errorResponse("Record not found"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
	}

	// Metadata endpoint
	spec.Paths[metaPath] = PathItem{
		Get: &Operation{
			Summary:     fmt.Sprintf("Get %s metadata", entity),
			Description: fmt.Sprintf("Retrieve metadata information for %s table", entity),
			OperationID: fmt.Sprintf("metadataRestheadSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (RestheadSpec)", entity)},
			Responses: map[string]Response{
				"200": {
					Description: "Metadata retrieved successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data": {
										Type: "object",
										Properties: map[string]*Schema{
											"schema":  {Type: "string"},
											"table":   {Type: "string"},
											"columns": {Type: "array", Items: &Schema{Type: "object"}},
										},
									},
								},
							},
						},
					},
				},
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
	}
}

// generateResolveSpecPaths generates OpenAPI paths for ResolveSpec endpoints
func (g *Generator) generateResolveSpecPaths(spec *OpenAPISpec, schema, entity, schemaName string) {
	basePath := fmt.Sprintf("/resolve/%s/%s", schema, entity)
	idPath := fmt.Sprintf("/resolve/%s/%s/{id}", schema, entity)

	// Collection endpoint: POST (operations)
	spec.Paths[basePath] = PathItem{
		Post: &Operation{
			Summary:     fmt.Sprintf("Perform operation on %s", entity),
			Description: fmt.Sprintf("Execute read, create, or meta operations on %s records", entity),
			OperationID: fmt.Sprintf("operateResolveSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (ResolveSpec)", entity)},
			RequestBody: &RequestBody{
				Required:    true,
				Description: "Operation request with operation type and options",
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{Ref: "#/components/schemas/ResolveSpecRequest"},
						Example: map[string]interface{}{
							"operation": "read",
							"options": map[string]interface{}{
								"limit": 10,
								"filters": []map[string]interface{}{
									{"column": "status", "operator": "eq", "value": "active"},
								},
							},
						},
					},
				},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Operation completed successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success":  {Type: "boolean"},
									"data":     {Type: "array", Items: &Schema{Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)}},
									"metadata": {Ref: "#/components/schemas/Metadata"},
								},
							},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Get: &Operation{
			Summary:     fmt.Sprintf("Get %s metadata", entity),
			Description: fmt.Sprintf("Retrieve metadata for %s", entity),
			OperationID: fmt.Sprintf("metadataResolveSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (ResolveSpec)", entity)},
			Responses: map[string]Response{
				"200": {
					Description: "Metadata retrieved successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{Ref: "#/components/schemas/Response"},
						},
					},
				},
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
		Options: &Operation{
			Summary:     "CORS preflight",
			Description: "Handle CORS preflight requests",
			OperationID: fmt.Sprintf("optionsResolveSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (ResolveSpec)", entity)},
			Responses: map[string]Response{
				"204": {Description: "No content"},
			},
		},
	}

	// Single record endpoint: POST (update/delete)
	spec.Paths[idPath] = PathItem{
		Post: &Operation{
			Summary:     fmt.Sprintf("Update or delete %s record", entity),
			Description: fmt.Sprintf("Execute update or delete operation on a specific %s record", entity),
			OperationID: fmt.Sprintf("modifyResolveSpec%s%s", formatSchemaName(schema, ""), formatSchemaName("", entity)),
			Tags:        []string{fmt.Sprintf("%s (ResolveSpec)", entity)},
			Parameters: []Parameter{
				{Name: "id", In: "path", Required: true, Description: "Record ID", Schema: &Schema{Type: "integer"}},
			},
			RequestBody: &RequestBody{
				Required:    true,
				Description: "Operation request (update or delete)",
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{Ref: "#/components/schemas/ResolveSpecRequest"},
						Example: map[string]interface{}{
							"operation": "update",
							"data": map[string]interface{}{
								"status": "inactive",
							},
						},
					},
				},
			},
			Responses: map[string]Response{
				"200": {
					Description: "Operation completed successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{
								Type: "object",
								Properties: map[string]*Schema{
									"success": {Type: "boolean"},
									"data":    {Ref: fmt.Sprintf("#/components/schemas/%s", schemaName)},
								},
							},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"404": g.errorResponse("Record not found"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		},
	}
}

// generateFuncSpecPaths generates OpenAPI paths for FuncSpec endpoints
func (g *Generator) generateFuncSpecPaths(spec *OpenAPISpec) {
	for path, endpoint := range g.config.FuncSpecEndpoints {
		operation := &Operation{
			Summary:     endpoint.Summary,
			Description: endpoint.Description,
			OperationID: fmt.Sprintf("funcSpec%s", sanitizeOperationID(path)),
			Tags:        []string{"FuncSpec"},
			Parameters:  g.extractFuncSpecParameters(endpoint.Parameters),
			Responses: map[string]Response{
				"200": {
					Description: "Query executed successfully",
					Content: map[string]MediaType{
						"application/json": {
							Schema: &Schema{Ref: "#/components/schemas/Response"},
						},
					},
				},
				"400": g.errorResponse("Bad request"),
				"401": g.errorResponse("Unauthorized"),
				"500": g.errorResponse("Internal server error"),
			},
			Security: g.securityRequirements(),
		}

		pathItem := spec.Paths[path]
		switch endpoint.Method {
		case "GET":
			pathItem.Get = operation
		case "POST":
			pathItem.Post = operation
		case "PUT":
			pathItem.Put = operation
		case "DELETE":
			pathItem.Delete = operation
		}
		spec.Paths[path] = pathItem
	}
}

// getRestheadSpecHeaders returns all RestheadSpec header parameters
func (g *Generator) getRestheadSpecHeaders() []Parameter {
	return []Parameter{
		{Name: "X-Filters", In: "header", Description: "JSON array of filter conditions", Schema: &Schema{Type: "string"}},
		{Name: "X-Columns", In: "header", Description: "Comma-separated list of columns to select", Schema: &Schema{Type: "string"}},
		{Name: "X-Sort", In: "header", Description: "JSON array of sort specifications", Schema: &Schema{Type: "string"}},
		{Name: "X-Limit", In: "header", Description: "Maximum number of records to return", Schema: &Schema{Type: "integer"}},
		{Name: "X-Offset", In: "header", Description: "Number of records to skip", Schema: &Schema{Type: "integer"}},
		{Name: "X-Preload", In: "header", Description: "Relations to eager load (comma-separated)", Schema: &Schema{Type: "string"}},
		{Name: "X-Expand", In: "header", Description: "Relations to expand with LEFT JOIN (comma-separated)", Schema: &Schema{Type: "string"}},
		{Name: "X-Distinct", In: "header", Description: "Enable DISTINCT query (true/false)", Schema: &Schema{Type: "boolean"}},
		{Name: "X-Response-Format", In: "header", Description: "Response format", Schema: &Schema{Type: "string", Enum: []interface{}{"detail", "simple", "syncfusion"}}},
		{Name: "X-Clean-JSON", In: "header", Description: "Remove null/empty fields from response (true/false)", Schema: &Schema{Type: "boolean"}},
		{Name: "X-Custom-SQL-Where", In: "header", Description: "Custom SQL WHERE clause (AND)", Schema: &Schema{Type: "string"}},
		{Name: "X-Custom-SQL-Or", In: "header", Description: "Custom SQL WHERE clause (OR)", Schema: &Schema{Type: "string"}},
	}
}

// extractFuncSpecParameters creates OpenAPI parameters from parameter names
func (g *Generator) extractFuncSpecParameters(paramNames []string) []Parameter {
	params := []Parameter{}
	for _, name := range paramNames {
		params = append(params, Parameter{
			Name:        name,
			In:          "query",
			Description: fmt.Sprintf("Parameter: %s", name),
			Schema:      &Schema{Type: "string"},
		})
	}
	return params
}

// errorResponse creates a standard error response
func (g *Generator) errorResponse(description string) Response {
	return Response{
		Description: description,
		Content: map[string]MediaType{
			"application/json": {
				Schema: &Schema{Ref: "#/components/schemas/APIError"},
			},
		},
	}
}

// securityRequirements returns all security options (user can use any)
func (g *Generator) securityRequirements() []map[string][]string {
	return []map[string][]string{
		{"BearerAuth": {}},
		{"SessionToken": {}},
		{"CookieAuth": {}},
		{"HeaderAuth": {}},
	}
}

// sanitizeOperationID removes invalid characters from operation IDs
func sanitizeOperationID(path string) string {
	result := ""
	for _, char := range path {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result += string(char)
		}
	}
	return result
}
