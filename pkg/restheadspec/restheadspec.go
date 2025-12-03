// Package restheadspec provides the Rest Header Spec API framework.
//
// Rest Header Spec (restheadspec) is a RESTful API framework that reads query options,
// filters, sorting, pagination, and other parameters from HTTP headers instead of
// request bodies or query parameters. This approach provides a clean separation between
// data and metadata in API requests.
//
// # Key Features
//
//   - Header-based API configuration: All query options are passed via HTTP headers
//   - Database-agnostic: Works with both GORM and Bun ORM through adapters
//   - Router-agnostic: Supports multiple HTTP routers (Mux, BunRouter, etc.)
//   - Advanced filtering: Supports complex filter operations (eq, gt, lt, like, between, etc.)
//   - Pagination and sorting: Built-in support for limit, offset, and multi-column sorting
//   - Preloading and expansion: Support for eager loading relationships
//   - Multiple response formats: Default, simple, and Syncfusion formats
//
// # HTTP Headers
//
// The following headers are supported for configuring API requests:
//
//   - X-Filters: JSON array of filter conditions
//   - X-Columns: Comma-separated list of columns to select
//   - X-Sort: JSON array of sort specifications
//   - X-Limit: Maximum number of records to return
//   - X-Offset: Number of records to skip
//   - X-Preload: Comma-separated list of relations to preload
//   - X-Expand: Comma-separated list of relations to expand (LEFT JOIN)
//   - X-Distinct: Boolean to enable DISTINCT queries
//   - X-Skip-Count: Boolean to skip total count query
//   - X-Response-Format: Response format (detail, simple, syncfusion)
//   - X-Clean-JSON: Boolean to remove null/empty fields
//   - X-Custom-SQL-Where: Custom SQL WHERE clause (AND)
//   - X-Custom-SQL-Or: Custom SQL WHERE clause (OR)
//
// # Usage Example
//
//	// Create a handler with GORM
//	handler := restheadspec.NewHandlerWithGORM(db)
//
//	// Register models
//	handler.Registry.RegisterModel("users", User{})
//
//	// Setup routes with Mux
//	muxRouter := mux.NewRouter()
//	restheadspec.SetupMuxRoutes(muxRouter, handler)
//
//	// Make a request with headers
//	// GET /public/users
//	// X-Filters: [{"column":"age","operator":"gt","value":18}]
//	// X-Sort: [{"column":"name","direction":"asc"}]
//	// X-Limit: 10
package restheadspec

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/uptrace/bun"
	"github.com/uptrace/bunrouter"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/router"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// NewHandlerWithGORM creates a new Handler with GORM adapter
func NewHandlerWithGORM(db *gorm.DB) *Handler {
	gormAdapter := database.NewGormAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandler(gormAdapter, registry)
}

// NewHandlerWithBun creates a new Handler with Bun adapter
func NewHandlerWithBun(db *bun.DB) *Handler {
	bunAdapter := database.NewBunAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandler(bunAdapter, registry)
}

// NewStandardMuxRouter creates a router with standard Mux HTTP handlers
func NewStandardMuxRouter() *router.StandardMuxAdapter {
	return router.NewStandardMuxAdapter()
}

// NewStandardBunRouter creates a router with standard BunRouter handlers
func NewStandardBunRouter() *router.StandardBunRouterAdapter {
	return router.NewStandardBunRouterAdapter()
}

// MiddlewareFunc is a function that wraps an http.Handler with additional functionality
type MiddlewareFunc func(http.Handler) http.Handler

// SetupMuxRoutes sets up routes for the RestHeadSpec API with Mux
// authMiddleware is optional - if provided, routes will be protected with the middleware
// Example: SetupMuxRoutes(router, handler, func(h http.Handler) http.Handler { return security.NewAuthHandler(securityList, h) })
func SetupMuxRoutes(muxRouter *mux.Router, handler *Handler, authMiddleware MiddlewareFunc) {
	// Get all registered models from the registry
	allModels := handler.registry.GetAllModels()

	// Loop through each registered model and create explicit routes
	for fullName := range allModels {
		// Parse the full name (e.g., "public.users" or just "users")
		schema, entity := parseModelName(fullName)

		// Build the route paths
		entityPath := buildRoutePath(schema, entity)
		entityWithIDPath := buildRoutePath(schema, entity) + "/{id}"
		metadataPath := buildRoutePath(schema, entity) + "/metadata"

		// Create handler functions for this specific entity
		entityHandler := createMuxHandler(handler, schema, entity, "")
		entityWithIDHandler := createMuxHandler(handler, schema, entity, "id")
		metadataHandler := createMuxGetHandler(handler, schema, entity, "")
		optionsEntityHandler := createMuxOptionsHandler(handler, schema, entity, []string{"GET", "POST", "OPTIONS"})
		optionsEntityWithIDHandler := createMuxOptionsHandler(handler, schema, entity, []string{"GET", "PUT", "PATCH", "DELETE", "POST", "OPTIONS"})

		// Apply authentication middleware if provided
		if authMiddleware != nil {
			entityHandler = authMiddleware(entityHandler).(http.HandlerFunc)
			entityWithIDHandler = authMiddleware(entityWithIDHandler).(http.HandlerFunc)
			metadataHandler = authMiddleware(metadataHandler).(http.HandlerFunc)
			// Don't apply auth middleware to OPTIONS - CORS preflight must not require auth
		}

		// Register routes for this entity
		// GET, POST for /{schema}/{entity}
		muxRouter.Handle(entityPath, entityHandler).Methods("GET", "POST")

		// GET, PUT, PATCH, DELETE, POST for /{schema}/{entity}/{id}
		muxRouter.Handle(entityWithIDPath, entityWithIDHandler).Methods("GET", "PUT", "PATCH", "DELETE", "POST")

		// GET for metadata (using HandleGet)
		muxRouter.Handle(metadataPath, metadataHandler).Methods("GET")

		// OPTIONS for CORS preflight - returns metadata
		muxRouter.Handle(entityPath, optionsEntityHandler).Methods("OPTIONS")
		muxRouter.Handle(entityWithIDPath, optionsEntityWithIDHandler).Methods("OPTIONS")
	}
}

// Helper function to create Mux handler for a specific entity with CORS support
func createMuxHandler(handler *Handler, schema, entity, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		corsConfig := common.DefaultCORSConfig()
		respAdapter := router.NewHTTPResponseWriter(w)
		common.SetCORSHeaders(respAdapter, corsConfig)

		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity
		if idParam != "" {
			vars["id"] = mux.Vars(r)[idParam]
		}
		reqAdapter := router.NewHTTPRequest(r)
		handler.Handle(respAdapter, reqAdapter, vars)
	}
}

// Helper function to create Mux GET handler for a specific entity with CORS support
func createMuxGetHandler(handler *Handler, schema, entity, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		corsConfig := common.DefaultCORSConfig()
		respAdapter := router.NewHTTPResponseWriter(w)
		common.SetCORSHeaders(respAdapter, corsConfig)

		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity
		if idParam != "" {
			vars["id"] = mux.Vars(r)[idParam]
		}
		reqAdapter := router.NewHTTPRequest(r)
		handler.HandleGet(respAdapter, reqAdapter, vars)
	}
}

// Helper function to create Mux OPTIONS handler that returns metadata
func createMuxOptionsHandler(handler *Handler, schema, entity string, allowedMethods []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers with the allowed methods for this route
		corsConfig := common.DefaultCORSConfig()
		corsConfig.AllowedMethods = allowedMethods
		respAdapter := router.NewHTTPResponseWriter(w)
		common.SetCORSHeaders(respAdapter, corsConfig)

		// Return metadata in the OPTIONS response body
		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity
		reqAdapter := router.NewHTTPRequest(r)
		handler.HandleGet(respAdapter, reqAdapter, vars)
	}
}

// parseModelName parses a model name like "public.users" into schema and entity
// If no schema is present, returns empty string for schema
func parseModelName(fullName string) (schema, entity string) {
	parts := strings.Split(fullName, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fullName
}

// buildRoutePath builds a route path from schema and entity
// If schema is empty, returns just "/entity", otherwise "/{schema}/{entity}"
func buildRoutePath(schema, entity string) string {
	if schema == "" {
		return "/" + entity
	}
	return "/" + schema + "/" + entity
}

// Example usage functions for documentation:

// ExampleWithGORM shows how to use RestHeadSpec with GORM
func ExampleWithGORM(db *gorm.DB) {
	// Create handler using GORM
	handler := NewHandlerWithGORM(db)

	// Setup router without authentication
	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Register models
	// handler.registry.RegisterModel("public.users", &User{})

	// To add authentication, pass a middleware function:
	// import "github.com/bitechdev/ResolveSpec/pkg/security"
	// secList := security.NewSecurityList(myProvider)
	// authMiddleware := func(h http.Handler) http.Handler {
	//     return security.NewAuthHandler(secList, h)
	// }
	// SetupMuxRoutes(muxRouter, handler, authMiddleware)
}

// ExampleWithBun shows how to switch to Bun ORM
func ExampleWithBun(bunDB *bun.DB) {
	// Create Bun adapter
	dbAdapter := database.NewBunAdapter(bunDB)

	// Create model registry
	registry := modelregistry.NewModelRegistry()
	// registry.RegisterModel("public.users", &User{})

	// Create handler
	handler := NewHandler(dbAdapter, registry)

	// Setup routes without authentication
	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)
}

// SetupBunRouterRoutes sets up bunrouter routes for the RestHeadSpec API
func SetupBunRouterRoutes(bunRouter *router.StandardBunRouterAdapter, handler *Handler) {
	r := bunRouter.GetBunRouter()

	// Get all registered models from the registry
	allModels := handler.registry.GetAllModels()

	// CORS config
	corsConfig := common.DefaultCORSConfig()

	// Loop through each registered model and create explicit routes
	for fullName := range allModels {
		// Parse the full name (e.g., "public.users" or just "users")
		schema, entity := parseModelName(fullName)

		// Build the route paths
		entityPath := buildRoutePath(schema, entity)
		entityWithIDPath := entityPath + "/:id"
		metadataPath := entityPath + "/metadata"

		// Create closure variables to capture current schema and entity
		currentSchema := schema
		currentEntity := entity

		// GET and POST for /{schema}/{entity}
		r.Handle("GET", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		r.Handle("POST", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		// GET, POST, PUT, PATCH, DELETE for /{schema}/{entity}/:id
		r.Handle("GET", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		r.Handle("POST", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		r.Handle("PUT", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		r.Handle("PATCH", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		r.Handle("DELETE", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		// Metadata endpoint
		r.Handle("GET", metadataPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			common.SetCORSHeaders(respAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})

		// OPTIONS route without ID (returns metadata)
		r.Handle("OPTIONS", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			optionsCorsConfig := corsConfig
			optionsCorsConfig.AllowedMethods = []string{"GET", "POST", "OPTIONS"}
			common.SetCORSHeaders(respAdapter, optionsCorsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})

		// OPTIONS route with ID (returns metadata)
		r.Handle("OPTIONS", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			optionsCorsConfig := corsConfig
			optionsCorsConfig.AllowedMethods = []string{"GET", "PUT", "PATCH", "DELETE", "POST", "OPTIONS"}
			common.SetCORSHeaders(respAdapter, optionsCorsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}
			reqAdapter := router.NewBunRouterRequest(req)
			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})
	}
}

// ExampleBunRouterWithBunDB shows usage with both BunRouter and Bun DB
func ExampleBunRouterWithBunDB(bunDB *bun.DB) {
	// Create handler
	handler := NewHandlerWithBun(bunDB)

	// Create BunRouter adapter
	routerAdapter := NewStandardBunRouter()

	// Setup routes
	SetupBunRouterRoutes(routerAdapter, handler)

	// Get the underlying router for server setup
	r := routerAdapter.GetBunRouter()

	// Start server
	if err := http.ListenAndServe(":8080", r); err != nil {
		logger.Error("Server failed to start: %v", err)
	}
}
