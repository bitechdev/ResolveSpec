package openapi

import (
	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/resolvespec"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

// ExampleRestheadSpec shows how to configure OpenAPI generation for RestheadSpec
func ExampleRestheadSpec(db *gorm.DB) {
	// 1. Create registry and register models
	registry := modelregistry.NewModelRegistry()
	// registry.RegisterModel("public.users", User{})
	// registry.RegisterModel("public.products", Product{})

	// 2. Create handler with custom registry
	// import "github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	// gormAdapter := database.NewGormAdapter(db)
	// handler := restheadspec.NewHandler(gormAdapter, registry)
	// Or use the convenience function (creates its own registry):
	handler := restheadspec.NewHandlerWithGORM(db)

	// 3. Configure OpenAPI generator
	handler.SetOpenAPIGenerator(func() (string, error) {
		generator := NewGenerator(GeneratorConfig{
			Title:               "My API",
			Description:         "API documentation for my application",
			Version:             "1.0.0",
			BaseURL:             "http://localhost:8080",
			Registry:            registry,
			IncludeRestheadSpec: true,
			IncludeResolveSpec:  false,
			IncludeFuncSpec:     false,
		})
		return generator.GenerateJSON()
	})

	// 4. Setup routes (includes /openapi endpoint)
	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, handler, nil)

	// Now the following endpoints are available:
	// GET /openapi                     - Full OpenAPI spec
	// GET /public/users?openapi        - OpenAPI spec
	// GET /public/products?openapi     - OpenAPI spec
	// etc.
}

// ExampleResolveSpec shows how to configure OpenAPI generation for ResolveSpec
func ExampleResolveSpec(db *gorm.DB) {
	// 1. Create registry and register models
	registry := modelregistry.NewModelRegistry()
	// registry.RegisterModel("public.users", User{})
	// registry.RegisterModel("public.products", Product{})

	// 2. Create handler with custom registry
	// import "github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	// gormAdapter := database.NewGormAdapter(db)
	// handler := resolvespec.NewHandler(gormAdapter, registry)
	// Or use the convenience function (creates its own registry):
	handler := resolvespec.NewHandlerWithGORM(db)
	// Note: handler.RegisterModel("schema", "entity", model) can be used

	// 3. Configure OpenAPI generator
	handler.SetOpenAPIGenerator(func() (string, error) {
		generator := NewGenerator(GeneratorConfig{
			Title:               "My API",
			Description:         "API documentation for my application",
			Version:             "1.0.0",
			BaseURL:             "http://localhost:8080",
			Registry:            registry,
			IncludeRestheadSpec: false,
			IncludeResolveSpec:  true,
			IncludeFuncSpec:     false,
		})
		return generator.GenerateJSON()
	})

	// 4. Setup routes (includes /openapi endpoint)
	router := mux.NewRouter()
	resolvespec.SetupMuxRoutes(router, handler, nil)

	// Now the following endpoints are available:
	// GET /openapi                          - Full OpenAPI spec
	// POST /resolve/public/users?openapi    - OpenAPI spec
	// POST /resolve/public/products?openapi - OpenAPI spec
	// etc.
}

// ExampleBothSpecs shows how to combine both RestheadSpec and ResolveSpec
func ExampleBothSpecs(db *gorm.DB) {
	// Create shared registry
	sharedRegistry := modelregistry.NewModelRegistry()
	// Register models once
	// sharedRegistry.RegisterModel("public.users", User{})
	// sharedRegistry.RegisterModel("public.products", Product{})

	// Create handlers - they will have separate registries initially
	restheadHandler := restheadspec.NewHandlerWithGORM(db)
	resolveHandler := resolvespec.NewHandlerWithGORM(db)

	// Note: If you want to use a shared registry, create handlers manually:
	// import "github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	// gormAdapter := database.NewGormAdapter(db)
	// restheadHandler := restheadspec.NewHandler(gormAdapter, sharedRegistry)
	// resolveHandler := resolvespec.NewHandler(gormAdapter, sharedRegistry)

	// Configure OpenAPI generator for both
	generatorFunc := func() (string, error) {
		generator := NewGenerator(GeneratorConfig{
			Title:               "My Unified API",
			Description:         "Complete API documentation with both RestheadSpec and ResolveSpec endpoints",
			Version:             "1.0.0",
			BaseURL:             "http://localhost:8080",
			Registry:            sharedRegistry,
			IncludeRestheadSpec: true,
			IncludeResolveSpec:  true,
			IncludeFuncSpec:     false,
		})
		return generator.GenerateJSON()
	}

	restheadHandler.SetOpenAPIGenerator(generatorFunc)
	resolveHandler.SetOpenAPIGenerator(generatorFunc)

	// Setup routes
	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, restheadHandler, nil)

	// Add ResolveSpec routes under /resolve prefix
	resolveRouter := router.PathPrefix("/resolve").Subrouter()
	resolvespec.SetupMuxRoutes(resolveRouter, resolveHandler, nil)

	// Now you have both styles of API available:
	// GET /openapi                              - Full OpenAPI spec (both styles)
	// GET /public/users                         - RestheadSpec list endpoint
	// POST /resolve/public/users                - ResolveSpec operation endpoint
	// GET /public/users?openapi                 - OpenAPI spec
	// POST /resolve/public/users?openapi        - OpenAPI spec
}

// ExampleWithFuncSpec shows how to add FuncSpec endpoints to OpenAPI
func ExampleWithFuncSpec() {
	// FuncSpec endpoints need to be registered manually since they don't use model registry
	generatorFunc := func() (string, error) {
		funcSpecEndpoints := map[string]FuncSpecEndpoint{
			"/api/reports/sales": {
				Path:        "/api/reports/sales",
				Method:      "GET",
				Summary:     "Get sales report",
				Description: "Returns sales data for the specified date range",
				SQLQuery:    "SELECT * FROM sales WHERE date BETWEEN [start_date] AND [end_date]",
				Parameters:  []string{"start_date", "end_date"},
			},
			"/api/analytics/users": {
				Path:        "/api/analytics/users",
				Method:      "GET",
				Summary:     "Get user analytics",
				Description: "Returns user activity analytics",
				SQLQuery:    "SELECT * FROM user_analytics WHERE user_id = [user_id]",
				Parameters:  []string{"user_id"},
			},
		}

		generator := NewGenerator(GeneratorConfig{
			Title:               "My API with Custom Queries",
			Description:         "API with FuncSpec custom SQL endpoints",
			Version:             "1.0.0",
			BaseURL:             "http://localhost:8080",
			Registry:            modelregistry.NewModelRegistry(),
			IncludeRestheadSpec: false,
			IncludeResolveSpec:  false,
			IncludeFuncSpec:     true,
			FuncSpecEndpoints:   funcSpecEndpoints,
		})
		return generator.GenerateJSON()
	}

	// Use this generator function with your handlers
	_ = generatorFunc
}

// ExampleWithUIHandler shows how to serve OpenAPI documentation with a web UI
func ExampleWithUIHandler(db *gorm.DB) {
	// Create handler and configure OpenAPI generator
	handler := restheadspec.NewHandlerWithGORM(db)
	registry := modelregistry.NewModelRegistry()

	handler.SetOpenAPIGenerator(func() (string, error) {
		generator := NewGenerator(GeneratorConfig{
			Title:               "My API",
			Description:         "API documentation with interactive UI",
			Version:             "1.0.0",
			BaseURL:             "http://localhost:8080",
			Registry:            registry,
			IncludeRestheadSpec: true,
		})
		return generator.GenerateJSON()
	})

	// Setup routes
	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, handler, nil)

	// Add UI handlers for different frameworks
	// Swagger UI at /docs (most popular)
	SetupUIRoute(router, "/docs", UIConfig{
		UIType:  SwaggerUI,
		SpecURL: "/openapi",
		Title:   "My API - Swagger UI",
		Theme:   "light",
	})

	// RapiDoc at /rapidoc (modern alternative)
	SetupUIRoute(router, "/rapidoc", UIConfig{
		UIType:  RapiDoc,
		SpecURL: "/openapi",
		Title:   "My API - RapiDoc",
	})

	// Redoc at /redoc (clean and responsive)
	SetupUIRoute(router, "/redoc", UIConfig{
		UIType:  Redoc,
		SpecURL: "/openapi",
		Title:   "My API - Redoc",
	})

	// Scalar at /scalar (modern and sleek)
	SetupUIRoute(router, "/scalar", UIConfig{
		UIType:  Scalar,
		SpecURL: "/openapi",
		Title:   "My API - Scalar",
		Theme:   "dark",
	})

	// Now you can access:
	// http://localhost:8080/docs      - Swagger UI
	// http://localhost:8080/rapidoc   - RapiDoc
	// http://localhost:8080/redoc     - Redoc
	// http://localhost:8080/scalar    - Scalar
	// http://localhost:8080/openapi   - Raw OpenAPI JSON

	_ = router
}

// ExampleCustomization shows advanced customization options
func ExampleCustomization() {
	// Create registry and register models with descriptions using struct tags
	registry := modelregistry.NewModelRegistry()

	// type User struct {
	//     ID    int    `json:"id" gorm:"primaryKey" description:"Unique user identifier"`
	//     Name  string `json:"name" description:"User's full name"`
	//     Email string `json:"email" gorm:"unique" description:"User's email address"`
	// }
	// registry.RegisterModel("public.users", User{})

	// Advanced configuration - create generator function
	generatorFunc := func() (string, error) {
		generator := NewGenerator(GeneratorConfig{
			Title:               "My Advanced API",
			Description:         "Comprehensive API documentation with custom configuration",
			Version:             "2.1.0",
			BaseURL:             "https://api.myapp.com",
			Registry:            registry,
			IncludeRestheadSpec: true,
			IncludeResolveSpec:  true,
			IncludeFuncSpec:     false,
		})

		// Generate the spec
		// spec, err := generator.Generate()
		// if err != nil {
		//     return "", err
		// }

		// Customize the spec further if needed
		// spec.Info.Contact = &Contact{
		//     Name:  "API Support",
		//     Email: "support@myapp.com",
		//     URL:   "https://myapp.com/support",
		// }

		// Add additional servers
		// spec.Servers = append(spec.Servers, Server{
		//     URL:         "https://staging-api.myapp.com",
		//     Description: "Staging Server",
		// })

		// Convert back to JSON - or use GenerateJSON() for simple cases
		return generator.GenerateJSON()
	}

	// Use this generator function with your handlers
	_ = generatorFunc
}
