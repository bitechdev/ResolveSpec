package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// OpenAPISpec represents the OpenAPI 3.0 specification structure
type OpenAPISpec struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Paths      map[string]PathItem   `json:"paths"`
	Components Components            `json:"components"`
	Security   []map[string][]string `json:"security,omitempty"`
}

type Info struct {
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Version     string  `json:"version"`
	Contact     *Contact `json:"contact,omitempty"`
}

type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Options *Operation `json:"options,omitempty"`
}

type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"` // "query", "header", "path", "cookie"
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
	Example     interface{} `json:"example,omitempty"`
}

type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

type MediaType struct {
	Schema  *Schema     `json:"schema,omitempty"`
	Example interface{} `json:"example,omitempty"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type Components struct {
	Schemas         map[string]Schema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes,omitempty"`
}

type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
	Enum                 []interface{}      `json:"enum,omitempty"`
	Example              interface{}        `json:"example,omitempty"`
	AdditionalProperties interface{}        `json:"additionalProperties,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
}

type SecurityScheme struct {
	Type        string `json:"type"` // "apiKey", "http", "oauth2", "openIdConnect"
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`        // For apiKey
	In          string `json:"in,omitempty"`          // For apiKey: "query", "header", "cookie"
	Scheme      string `json:"scheme,omitempty"`      // For http: "basic", "bearer"
	BearerFormat string `json:"bearerFormat,omitempty"` // For http bearer
}

// GeneratorConfig holds configuration for OpenAPI spec generation
type GeneratorConfig struct {
	Title           string
	Description     string
	Version         string
	BaseURL         string
	Registry        *modelregistry.DefaultModelRegistry
	IncludeRestheadSpec bool
	IncludeResolveSpec  bool
	IncludeFuncSpec     bool
	FuncSpecEndpoints   map[string]FuncSpecEndpoint // path -> endpoint info
}

// FuncSpecEndpoint represents a FuncSpec endpoint for OpenAPI generation
type FuncSpecEndpoint struct {
	Path        string
	Method      string
	Summary     string
	Description string
	SQLQuery    string
	Parameters  []string // Parameter names extracted from SQL
}

// Generator creates OpenAPI specifications
type Generator struct {
	config GeneratorConfig
}

// NewGenerator creates a new OpenAPI generator
func NewGenerator(config GeneratorConfig) *Generator {
	if config.Title == "" {
		config.Title = "ResolveSpec API"
	}
	if config.Version == "" {
		config.Version = "1.0.0"
	}
	return &Generator{config: config}
}

// Generate creates the complete OpenAPI specification
func (g *Generator) Generate() (*OpenAPISpec, error) {
	spec := &OpenAPISpec{
		OpenAPI: "3.0.0",
		Info: Info{
			Title:       g.config.Title,
			Description: g.config.Description,
			Version:     g.config.Version,
		},
		Paths: make(map[string]PathItem),
		Components: Components{
			Schemas:         make(map[string]Schema),
			SecuritySchemes: g.generateSecuritySchemes(),
		},
	}

	if g.config.BaseURL != "" {
		spec.Servers = []Server{
			{URL: g.config.BaseURL, Description: "API Server"},
		}
	}

	// Add common schemas
	g.addCommonSchemas(spec)

	// Generate paths and schemas from registered models
	if err := g.generateFromModels(spec); err != nil {
		return nil, err
	}

	return spec, nil
}

// GenerateJSON generates OpenAPI spec as JSON string
func (g *Generator) GenerateJSON() (string, error) {
	spec, err := g.Generate()
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal spec: %w", err)
	}

	return string(data), nil
}

// generateSecuritySchemes creates security scheme definitions
func (g *Generator) generateSecuritySchemes() map[string]SecurityScheme {
	return map[string]SecurityScheme{
		"BearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "JWT Bearer token authentication",
		},
		"SessionToken": {
			Type:        "apiKey",
			In:          "header",
			Name:        "Authorization",
			Description: "Session token authentication",
		},
		"CookieAuth": {
			Type:        "apiKey",
			In:          "cookie",
			Name:        "session_token",
			Description: "Cookie-based session authentication",
		},
		"HeaderAuth": {
			Type:        "apiKey",
			In:          "header",
			Name:        "X-User-ID",
			Description: "Header-based user authentication",
		},
	}
}

// addCommonSchemas adds common reusable schemas
func (g *Generator) addCommonSchemas(spec *OpenAPISpec) {
	// Response wrapper schema
	spec.Components.Schemas["Response"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"success": {Type: "boolean", Description: "Indicates if the operation was successful"},
			"data":    {Description: "The response data"},
			"metadata": {Ref: "#/components/schemas/Metadata"},
			"error":    {Ref: "#/components/schemas/APIError"},
		},
	}

	// Metadata schema
	spec.Components.Schemas["Metadata"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"total":     {Type: "integer", Description: "Total number of records"},
			"count":     {Type: "integer", Description: "Number of records in this response"},
			"filtered":  {Type: "integer", Description: "Number of records after filtering"},
			"limit":     {Type: "integer", Description: "Limit applied"},
			"offset":    {Type: "integer", Description: "Offset applied"},
			"rowNumber": {Type: "integer", Description: "Row number for cursor pagination"},
		},
	}

	// APIError schema
	spec.Components.Schemas["APIError"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"code":    {Type: "string", Description: "Error code"},
			"message": {Type: "string", Description: "Error message"},
			"details": {Type: "string", Description: "Detailed error information"},
		},
	}

	// RequestOptions schema
	spec.Components.Schemas["RequestOptions"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"preload": {
				Type:        "array",
				Description: "Relations to eager load",
				Items:       &Schema{Ref: "#/components/schemas/PreloadOption"},
			},
			"columns": {
				Type:        "array",
				Description: "Columns to select",
				Items:       &Schema{Type: "string"},
			},
			"omitColumns": {
				Type:        "array",
				Description: "Columns to exclude",
				Items:       &Schema{Type: "string"},
			},
			"filters": {
				Type:        "array",
				Description: "Filter conditions",
				Items:       &Schema{Ref: "#/components/schemas/FilterOption"},
			},
			"sort": {
				Type:        "array",
				Description: "Sort specifications",
				Items:       &Schema{Ref: "#/components/schemas/SortOption"},
			},
			"limit":  {Type: "integer", Description: "Maximum number of records"},
			"offset": {Type: "integer", Description: "Number of records to skip"},
		},
	}

	// FilterOption schema
	spec.Components.Schemas["FilterOption"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"column":        {Type: "string", Description: "Column name"},
			"operator":      {Type: "string", Description: "Comparison operator", Enum: []interface{}{"eq", "neq", "gt", "lt", "gte", "lte", "like", "ilike", "in", "not_in", "between", "is_null", "is_not_null"}},
			"value":         {Description: "Filter value"},
			"logicOperator": {Type: "string", Description: "Logic operator", Enum: []interface{}{"AND", "OR"}},
		},
	}

	// SortOption schema
	spec.Components.Schemas["SortOption"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"column":    {Type: "string", Description: "Column name"},
			"direction": {Type: "string", Description: "Sort direction", Enum: []interface{}{"asc", "desc"}},
		},
	}

	// PreloadOption schema
	spec.Components.Schemas["PreloadOption"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"relation": {Type: "string", Description: "Relation name"},
			"columns": {
				Type:        "array",
				Description: "Columns to select from related table",
				Items:       &Schema{Type: "string"},
			},
		},
	}

	// ResolveSpec RequestBody schema
	spec.Components.Schemas["ResolveSpecRequest"] = Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"operation": {Type: "string", Description: "Operation type", Enum: []interface{}{"read", "create", "update", "delete", "meta"}},
			"data":      {Description: "Payload data (object or array)"},
			"id":        {Type: "integer", Description: "Record ID for single operations"},
			"options":   {Ref: "#/components/schemas/RequestOptions"},
		},
	}
}

// generateFromModels generates paths and schemas from registered models
func (g *Generator) generateFromModels(spec *OpenAPISpec) error {
	if g.config.Registry == nil {
		return fmt.Errorf("model registry is required")
	}

	models := g.config.Registry.GetAllModels()

	for name, model := range models {
		// Parse schema.entity from model name
		schema, entity := parseModelName(name)

		// Generate schema for this model
		modelSchema := g.generateModelSchema(model)
		schemaName := formatSchemaName(schema, entity)
		spec.Components.Schemas[schemaName] = modelSchema

		// Generate paths for different frameworks
		if g.config.IncludeRestheadSpec {
			g.generateRestheadSpecPaths(spec, schema, entity, schemaName)
		}

		if g.config.IncludeResolveSpec {
			g.generateResolveSpecPaths(spec, schema, entity, schemaName)
		}
	}

	// Generate FuncSpec paths if configured
	if g.config.IncludeFuncSpec && len(g.config.FuncSpecEndpoints) > 0 {
		g.generateFuncSpecPaths(spec)
	}

	return nil
}

// generateModelSchema creates an OpenAPI schema from a Go struct
func (g *Generator) generateModelSchema(model interface{}) Schema {
	schema := Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
		Required:   []string{},
	}

	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return schema
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get JSON tag name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName == "" {
			fieldName = field.Name
		}

		// Generate property schema
		propSchema := g.generatePropertySchema(field)
		schema.Properties[fieldName] = propSchema

		// Check if field is required (not a pointer and no omitempty)
		if field.Type.Kind() != reflect.Ptr && !strings.Contains(jsonTag, "omitempty") {
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// generatePropertySchema creates a schema for a struct field
func (g *Generator) generatePropertySchema(field reflect.StructField) *Schema {
	schema := &Schema{}

	fieldType := field.Type
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	// Get description from tag
	if desc := field.Tag.Get("description"); desc != "" {
		schema.Description = desc
	}

	switch fieldType.Kind() {
	case reflect.String:
		schema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Slice, reflect.Array:
		schema.Type = "array"
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}
		if elemType.Kind() == reflect.Struct {
			// Complex type - would need recursive handling
			schema.Items = &Schema{Type: "object"}
		} else {
			schema.Items = g.generatePropertySchema(reflect.StructField{Type: elemType})
		}
	case reflect.Struct:
		// Check for time.Time
		if fieldType.String() == "time.Time" {
			schema.Type = "string"
			schema.Format = "date-time"
		} else {
			schema.Type = "object"
		}
	default:
		schema.Type = "string"
	}

	// Check for custom format from gorm/bun tags
	if gormTag := field.Tag.Get("gorm"); gormTag != "" {
		if strings.Contains(gormTag, "type:uuid") {
			schema.Format = "uuid"
		}
	}

	return schema
}

// parseModelName splits "schema.entity" or returns "public" and entity
func parseModelName(name string) (schema, entity string) {
	parts := strings.Split(name, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "public", name
}

// formatSchemaName creates a component schema name
func formatSchemaName(schema, entity string) string {
	if schema == "public" {
		return toTitleCase(entity)
	}
	return toTitleCase(schema) + toTitleCase(entity)
}

// toTitleCase converts a string to title case (first letter uppercase)
func toTitleCase(s string) string {
	if s == "" {
		return ""
	}
	if len(s) == 1 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
