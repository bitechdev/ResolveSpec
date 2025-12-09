package openapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// Test models
type TestUser struct {
	ID        int       `json:"id" gorm:"primaryKey" description:"User ID"`
	Name      string    `json:"name" gorm:"not null" description:"User's full name"`
	Email     string    `json:"email" gorm:"unique" description:"Email address"`
	Age       int       `json:"age" description:"User age"`
	IsActive  bool      `json:"is_active" description:"Active status"`
	CreatedAt time.Time `json:"created_at" description:"Creation timestamp"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" description:"Last update timestamp"`
	Roles     []string  `json:"roles,omitempty" description:"User roles"`
}

type TestProduct struct {
	ID          int     `json:"id" gorm:"primaryKey"`
	Name        string  `json:"name" gorm:"not null"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	InStock     bool    `json:"in_stock"`
}

type TestOrder struct {
	ID         int    `json:"id" gorm:"primaryKey"`
	UserID     int    `json:"user_id" gorm:"not null"`
	ProductID  int    `json:"product_id" gorm:"not null"`
	Quantity   int    `json:"quantity"`
	TotalPrice float64 `json:"total_price"`
}

func TestNewGenerator(t *testing.T) {
	registry := modelregistry.NewModelRegistry()

	tests := []struct {
		name   string
		config GeneratorConfig
		want   string // expected title
	}{
		{
			name: "with all fields",
			config: GeneratorConfig{
				Title:       "Test API",
				Description: "Test Description",
				Version:     "1.0.0",
				BaseURL:     "http://localhost:8080",
				Registry:    registry,
			},
			want: "Test API",
		},
		{
			name: "with defaults",
			config: GeneratorConfig{
				Registry: registry,
			},
			want: "ResolveSpec API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(tt.config)
			if gen == nil {
				t.Fatal("NewGenerator returned nil")
			}
			if gen.config.Title != tt.want {
				t.Errorf("Title = %v, want %v", gen.config.Title, tt.want)
			}
		})
	}
}

func TestGenerateBasicSpec(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	err := registry.RegisterModel("public.users", TestUser{})
	if err != nil {
		t.Fatalf("Failed to register model: %v", err)
	}

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		Registry:            registry,
		IncludeRestheadSpec: true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test basic spec structure
	if spec.OpenAPI != "3.0.0" {
		t.Errorf("OpenAPI version = %v, want 3.0.0", spec.OpenAPI)
	}
	if spec.Info.Title != "Test API" {
		t.Errorf("Title = %v, want Test API", spec.Info.Title)
	}
	if spec.Info.Version != "1.0.0" {
		t.Errorf("Version = %v, want 1.0.0", spec.Info.Version)
	}

	// Test that common schemas are added
	if spec.Components.Schemas["Response"].Type != "object" {
		t.Error("Response schema not found or invalid")
	}
	if spec.Components.Schemas["Metadata"].Type != "object" {
		t.Error("Metadata schema not found or invalid")
	}

	// Test that model schema is added
	if _, exists := spec.Components.Schemas["Users"]; !exists {
		t.Error("Users schema not found")
	}

	// Test that security schemes are added
	if len(spec.Components.SecuritySchemes) == 0 {
		t.Error("Security schemes not added")
	}
}

func TestGenerateModelSchema(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	gen := NewGenerator(GeneratorConfig{Registry: registry})

	schema := gen.generateModelSchema(TestUser{})

	// Test basic properties
	if schema.Type != "object" {
		t.Errorf("Schema type = %v, want object", schema.Type)
	}

	// Test that properties are generated
	expectedProps := []string{"id", "name", "email", "age", "is_active", "created_at", "updated_at", "roles"}
	for _, prop := range expectedProps {
		if _, exists := schema.Properties[prop]; !exists {
			t.Errorf("Property %s not found in schema", prop)
		}
	}

	// Test property types
	if schema.Properties["id"].Type != "integer" {
		t.Errorf("id type = %v, want integer", schema.Properties["id"].Type)
	}
	if schema.Properties["name"].Type != "string" {
		t.Errorf("name type = %v, want string", schema.Properties["name"].Type)
	}
	if schema.Properties["is_active"].Type != "boolean" {
		t.Errorf("is_active type = %v, want boolean", schema.Properties["is_active"].Type)
	}

	// Test array type
	if schema.Properties["roles"].Type != "array" {
		t.Errorf("roles type = %v, want array", schema.Properties["roles"].Type)
	}
	if schema.Properties["roles"].Items.Type != "string" {
		t.Errorf("roles items type = %v, want string", schema.Properties["roles"].Items.Type)
	}

	// Test time.Time format
	if schema.Properties["created_at"].Type != "string" {
		t.Errorf("created_at type = %v, want string", schema.Properties["created_at"].Type)
	}
	if schema.Properties["created_at"].Format != "date-time" {
		t.Errorf("created_at format = %v, want date-time", schema.Properties["created_at"].Format)
	}

	// Test required fields (non-pointer, no omitempty)
	requiredFields := map[string]bool{}
	for _, field := range schema.Required {
		requiredFields[field] = true
	}
	if !requiredFields["id"] {
		t.Error("id should be required")
	}
	if !requiredFields["name"] {
		t.Error("name should be required")
	}
	if requiredFields["updated_at"] {
		t.Error("updated_at should not be required (pointer + omitempty)")
	}
	if requiredFields["roles"] {
		t.Error("roles should not be required (omitempty)")
	}

	// Test descriptions
	if schema.Properties["id"].Description != "User ID" {
		t.Errorf("id description = %v, want 'User ID'", schema.Properties["id"].Description)
	}
}

func TestGenerateRestheadSpecPaths(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	err := registry.RegisterModel("public.users", TestUser{})
	if err != nil {
		t.Fatalf("Failed to register model: %v", err)
	}

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		Registry:            registry,
		IncludeRestheadSpec: true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that paths are generated
	expectedPaths := []string{
		"/public/users",
		"/public/users/{id}",
		"/public/users/metadata",
	}

	for _, path := range expectedPaths {
		if _, exists := spec.Paths[path]; !exists {
			t.Errorf("Path %s not found", path)
		}
	}

	// Test collection endpoint methods
	usersPath := spec.Paths["/public/users"]
	if usersPath.Get == nil {
		t.Error("GET method not found for /public/users")
	}
	if usersPath.Post == nil {
		t.Error("POST method not found for /public/users")
	}
	if usersPath.Options == nil {
		t.Error("OPTIONS method not found for /public/users")
	}

	// Test single record endpoint methods
	userIDPath := spec.Paths["/public/users/{id}"]
	if userIDPath.Get == nil {
		t.Error("GET method not found for /public/users/{id}")
	}
	if userIDPath.Put == nil {
		t.Error("PUT method not found for /public/users/{id}")
	}
	if userIDPath.Patch == nil {
		t.Error("PATCH method not found for /public/users/{id}")
	}
	if userIDPath.Delete == nil {
		t.Error("DELETE method not found for /public/users/{id}")
	}

	// Test metadata endpoint
	metadataPath := spec.Paths["/public/users/metadata"]
	if metadataPath.Get == nil {
		t.Error("GET method not found for /public/users/metadata")
	}

	// Test operation details
	getOp := usersPath.Get
	if getOp.Summary == "" {
		t.Error("GET operation summary is empty")
	}
	if getOp.OperationID == "" {
		t.Error("GET operation ID is empty")
	}
	if len(getOp.Tags) == 0 {
		t.Error("GET operation has no tags")
	}
	if len(getOp.Parameters) == 0 {
		t.Error("GET operation has no parameters")
	}

	// Test RestheadSpec headers
	hasFiltersHeader := false
	for _, param := range getOp.Parameters {
		if param.Name == "X-Filters" && param.In == "header" {
			hasFiltersHeader = true
			break
		}
	}
	if !hasFiltersHeader {
		t.Error("X-Filters header parameter not found")
	}
}

func TestGenerateResolveSpecPaths(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	err := registry.RegisterModel("public.products", TestProduct{})
	if err != nil {
		t.Fatalf("Failed to register model: %v", err)
	}

	config := GeneratorConfig{
		Title:              "Test API",
		Version:            "1.0.0",
		Registry:           registry,
		IncludeResolveSpec: true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that paths are generated
	expectedPaths := []string{
		"/resolve/public/products",
		"/resolve/public/products/{id}",
	}

	for _, path := range expectedPaths {
		if _, exists := spec.Paths[path]; !exists {
			t.Errorf("Path %s not found", path)
		}
	}

	// Test collection endpoint methods
	productsPath := spec.Paths["/resolve/public/products"]
	if productsPath.Post == nil {
		t.Error("POST method not found for /resolve/public/products")
	}
	if productsPath.Get == nil {
		t.Error("GET method not found for /resolve/public/products")
	}
	if productsPath.Options == nil {
		t.Error("OPTIONS method not found for /resolve/public/products")
	}

	// Test POST operation has request body
	postOp := productsPath.Post
	if postOp.RequestBody == nil {
		t.Error("POST operation has no request body")
	}
	if _, exists := postOp.RequestBody.Content["application/json"]; !exists {
		t.Error("POST operation request body has no application/json content")
	}

	// Test request body schema references ResolveSpecRequest
	reqBodySchema := postOp.RequestBody.Content["application/json"].Schema
	if reqBodySchema.Ref != "#/components/schemas/ResolveSpecRequest" {
		t.Errorf("Request body schema ref = %v, want #/components/schemas/ResolveSpecRequest", reqBodySchema.Ref)
	}
}

func TestGenerateFuncSpecPaths(t *testing.T) {
	registry := modelregistry.NewModelRegistry()

	funcSpecEndpoints := map[string]FuncSpecEndpoint{
		"/api/reports/sales": {
			Path:        "/api/reports/sales",
			Method:      "GET",
			Summary:     "Get sales report",
			Description: "Returns sales data",
			Parameters:  []string{"start_date", "end_date"},
		},
		"/api/analytics/users": {
			Path:        "/api/analytics/users",
			Method:      "POST",
			Summary:     "Get user analytics",
			Description: "Returns user activity",
			Parameters:  []string{"user_id"},
		},
	}

	config := GeneratorConfig{
		Title:             "Test API",
		Version:           "1.0.0",
		Registry:          registry,
		IncludeFuncSpec:   true,
		FuncSpecEndpoints: funcSpecEndpoints,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that FuncSpec paths are generated
	salesPath := spec.Paths["/api/reports/sales"]
	if salesPath.Get == nil {
		t.Error("GET method not found for /api/reports/sales")
	}
	if salesPath.Get.Summary != "Get sales report" {
		t.Errorf("GET summary = %v, want 'Get sales report'", salesPath.Get.Summary)
	}
	if len(salesPath.Get.Parameters) != 2 {
		t.Errorf("GET has %d parameters, want 2", len(salesPath.Get.Parameters))
	}

	analyticsPath := spec.Paths["/api/analytics/users"]
	if analyticsPath.Post == nil {
		t.Error("POST method not found for /api/analytics/users")
	}
	if len(analyticsPath.Post.Parameters) != 1 {
		t.Errorf("POST has %d parameters, want 1", len(analyticsPath.Post.Parameters))
	}
}

func TestGenerateJSON(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	err := registry.RegisterModel("public.users", TestUser{})
	if err != nil {
		t.Fatalf("Failed to register model: %v", err)
	}

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		Registry:            registry,
		IncludeRestheadSpec: true,
	}

	gen := NewGenerator(config)
	jsonStr, err := gen.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	// Test that it's valid JSON
	var spec OpenAPISpec
	if err := json.Unmarshal([]byte(jsonStr), &spec); err != nil {
		t.Fatalf("Generated JSON is invalid: %v", err)
	}

	// Test basic structure
	if spec.OpenAPI != "3.0.0" {
		t.Errorf("OpenAPI version = %v, want 3.0.0", spec.OpenAPI)
	}
	if spec.Info.Title != "Test API" {
		t.Errorf("Title = %v, want Test API", spec.Info.Title)
	}

	// Test that JSON contains expected fields
	if !strings.Contains(jsonStr, `"openapi"`) {
		t.Error("JSON doesn't contain 'openapi' field")
	}
	if !strings.Contains(jsonStr, `"paths"`) {
		t.Error("JSON doesn't contain 'paths' field")
	}
	if !strings.Contains(jsonStr, `"components"`) {
		t.Error("JSON doesn't contain 'components' field")
	}
}

func TestMultipleModels(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	registry.RegisterModel("public.users", TestUser{})
	registry.RegisterModel("public.products", TestProduct{})
	registry.RegisterModel("public.orders", TestOrder{})

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		Registry:            registry,
		IncludeRestheadSpec: true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that all model schemas are generated
	expectedSchemas := []string{"Users", "Products", "Orders"}
	for _, schemaName := range expectedSchemas {
		if _, exists := spec.Components.Schemas[schemaName]; !exists {
			t.Errorf("Schema %s not found", schemaName)
		}
	}

	// Test that all paths are generated
	expectedPaths := []string{
		"/public/users",
		"/public/products",
		"/public/orders",
	}
	for _, path := range expectedPaths {
		if _, exists := spec.Paths[path]; !exists {
			t.Errorf("Path %s not found", path)
		}
	}
}

func TestModelNameParsing(t *testing.T) {
	tests := []struct {
		name         string
		fullName     string
		wantSchema   string
		wantEntity   string
	}{
		{
			name:       "with schema",
			fullName:   "public.users",
			wantSchema: "public",
			wantEntity: "users",
		},
		{
			name:       "without schema",
			fullName:   "users",
			wantSchema: "public",
			wantEntity: "users",
		},
		{
			name:       "custom schema",
			fullName:   "custom.products",
			wantSchema: "custom",
			wantEntity: "products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, entity := parseModelName(tt.fullName)
			if schema != tt.wantSchema {
				t.Errorf("schema = %v, want %v", schema, tt.wantSchema)
			}
			if entity != tt.wantEntity {
				t.Errorf("entity = %v, want %v", entity, tt.wantEntity)
			}
		})
	}
}

func TestSchemaNameFormatting(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		entity     string
		wantName   string
	}{
		{
			name:     "public schema",
			schema:   "public",
			entity:   "users",
			wantName: "Users",
		},
		{
			name:     "custom schema",
			schema:   "custom",
			entity:   "products",
			wantName: "CustomProducts",
		},
		{
			name:     "multi-word entity",
			schema:   "public",
			entity:   "user_profiles",
			wantName: "User_profiles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := formatSchemaName(tt.schema, tt.entity)
			if name != tt.wantName {
				t.Errorf("formatSchemaName() = %v, want %v", name, tt.wantName)
			}
		})
	}
}

func TestToTitleCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"users", "Users"},
		{"products", "Products"},
		{"userProfiles", "UserProfiles"},
		{"a", "A"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toTitleCase(tt.input)
			if got != tt.want {
				t.Errorf("toTitleCase(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateWithBaseURL(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	registry.RegisterModel("public.users", TestUser{})

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		BaseURL:             "https://api.example.com",
		Registry:            registry,
		IncludeRestheadSpec: true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that server is added
	if len(spec.Servers) == 0 {
		t.Fatal("No servers added")
	}
	if spec.Servers[0].URL != "https://api.example.com" {
		t.Errorf("Server URL = %v, want https://api.example.com", spec.Servers[0].URL)
	}
	if spec.Servers[0].Description != "API Server" {
		t.Errorf("Server description = %v, want 'API Server'", spec.Servers[0].Description)
	}
}

func TestGenerateCombinedFrameworks(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	registry.RegisterModel("public.users", TestUser{})

	config := GeneratorConfig{
		Title:               "Test API",
		Version:             "1.0.0",
		Registry:            registry,
		IncludeRestheadSpec: true,
		IncludeResolveSpec:  true,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that both RestheadSpec and ResolveSpec paths are generated
	restheadPath := "/public/users"
	resolveSpecPath := "/resolve/public/users"

	if _, exists := spec.Paths[restheadPath]; !exists {
		t.Errorf("RestheadSpec path %s not found", restheadPath)
	}
	if _, exists := spec.Paths[resolveSpecPath]; !exists {
		t.Errorf("ResolveSpec path %s not found", resolveSpecPath)
	}
}

func TestNilRegistry(t *testing.T) {
	config := GeneratorConfig{
		Title:   "Test API",
		Version: "1.0.0",
	}

	gen := NewGenerator(config)
	_, err := gen.Generate()
	if err == nil {
		t.Error("Expected error for nil registry, got nil")
	}
	if !strings.Contains(err.Error(), "registry") {
		t.Errorf("Error message should mention registry, got: %v", err)
	}
}

func TestSecuritySchemes(t *testing.T) {
	registry := modelregistry.NewModelRegistry()
	config := GeneratorConfig{
		Registry: registry,
	}

	gen := NewGenerator(config)
	spec, err := gen.Generate()
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that all security schemes are present
	expectedSchemes := []string{"BearerAuth", "SessionToken", "CookieAuth", "HeaderAuth"}
	for _, scheme := range expectedSchemes {
		if _, exists := spec.Components.SecuritySchemes[scheme]; !exists {
			t.Errorf("Security scheme %s not found", scheme)
		}
	}

	// Test BearerAuth scheme details
	bearerAuth := spec.Components.SecuritySchemes["BearerAuth"]
	if bearerAuth.Type != "http" {
		t.Errorf("BearerAuth type = %v, want http", bearerAuth.Type)
	}
	if bearerAuth.Scheme != "bearer" {
		t.Errorf("BearerAuth scheme = %v, want bearer", bearerAuth.Scheme)
	}
	if bearerAuth.BearerFormat != "JWT" {
		t.Errorf("BearerAuth format = %v, want JWT", bearerAuth.BearerFormat)
	}

	// Test HeaderAuth scheme details
	headerAuth := spec.Components.SecuritySchemes["HeaderAuth"]
	if headerAuth.Type != "apiKey" {
		t.Errorf("HeaderAuth type = %v, want apiKey", headerAuth.Type)
	}
	if headerAuth.In != "header" {
		t.Errorf("HeaderAuth in = %v, want header", headerAuth.In)
	}
	if headerAuth.Name != "X-User-ID" {
		t.Errorf("HeaderAuth name = %v, want X-User-ID", headerAuth.Name)
	}
}
