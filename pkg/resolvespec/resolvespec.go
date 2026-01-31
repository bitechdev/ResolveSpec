package resolvespec

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

// SetupMuxRoutes sets up routes for the ResolveSpec API with Mux
// authMiddleware is optional - if provided, routes will be protected with the middleware
// Example: SetupMuxRoutes(router, handler, func(h http.Handler) http.Handler { return security.NewAuthHandler(securityList, h) })
func SetupMuxRoutes(muxRouter *mux.Router, handler *Handler, authMiddleware MiddlewareFunc) {
	// Add global /openapi route
	openAPIHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corsConfig := common.DefaultCORSConfig()
		respAdapter := router.NewHTTPResponseWriter(w)
		reqAdapter := router.NewHTTPRequest(r)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)

		handler.HandleOpenAPI(respAdapter, reqAdapter)
	})
	muxRouter.Handle("/openapi", openAPIHandler).Methods("GET", "OPTIONS")

	// Get all registered models from the registry
	allModels := handler.registry.GetAllModels()

	// Loop through each registered model and create explicit routes
	for fullName := range allModels {
		// Parse the full name (e.g., "public.users" or just "users")
		schema, entity := parseModelName(fullName)

		// Build the route paths
		entityPath := buildRoutePath(schema, entity)
		entityWithIDPath := buildRoutePath(schema, entity) + "/{id}"

		// Create handler functions for this specific entity
		postEntityHandler := createMuxHandler(handler, schema, entity, "")
		postEntityWithIDHandler := createMuxHandler(handler, schema, entity, "id")
		getEntityHandler := createMuxGetHandler(handler, schema, entity, "")
		optionsEntityHandler := createMuxOptionsHandler(handler, schema, entity, []string{"GET", "POST", "OPTIONS"})
		optionsEntityWithIDHandler := createMuxOptionsHandler(handler, schema, entity, []string{"POST", "OPTIONS"})

		// Apply authentication middleware if provided
		if authMiddleware != nil {
			postEntityHandler = authMiddleware(postEntityHandler).(http.HandlerFunc)
			postEntityWithIDHandler = authMiddleware(postEntityWithIDHandler).(http.HandlerFunc)
			getEntityHandler = authMiddleware(getEntityHandler).(http.HandlerFunc)
			// Don't apply auth middleware to OPTIONS - CORS preflight must not require auth
		}

		// Register routes for this entity
		muxRouter.Handle(entityPath, postEntityHandler).Methods("POST")
		muxRouter.Handle(entityWithIDPath, postEntityWithIDHandler).Methods("POST")
		muxRouter.Handle(entityPath, getEntityHandler).Methods("GET")
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
		reqAdapter := router.NewHTTPRequest(r)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)

		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity
		if idParam != "" {
			vars["id"] = mux.Vars(r)[idParam]
		}

		handler.Handle(respAdapter, reqAdapter, vars)
	}
}

// Helper function to create Mux GET handler for a specific entity with CORS support
func createMuxGetHandler(handler *Handler, schema, entity, idParam string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		corsConfig := common.DefaultCORSConfig()
		respAdapter := router.NewHTTPResponseWriter(w)
		reqAdapter := router.NewHTTPRequest(r)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)

		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity
		if idParam != "" {
			vars["id"] = mux.Vars(r)[idParam]
		}

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
		reqAdapter := router.NewHTTPRequest(r)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)

		// Return metadata in the OPTIONS response body
		vars := make(map[string]string)
		vars["schema"] = schema
		vars["entity"] = entity

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

// ExampleWithGORM shows how to use ResolveSpec with GORM
func ExampleWithGORM(db *gorm.DB) {
	// Create handler using GORM
	handler := NewHandlerWithGORM(db)

	// Setup router without authentication
	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Register models
	// handler.RegisterModel("public", "users", &User{})

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

// BunRouterHandler is an interface that both bunrouter.Router and bunrouter.Group implement
type BunRouterHandler interface {
	Handle(method, path string, handler bunrouter.HandlerFunc)
}

// SetupBunRouterRoutes sets up bunrouter routes for the ResolveSpec API
// Accepts bunrouter.Router or bunrouter.Group
func SetupBunRouterRoutes(r BunRouterHandler, handler *Handler) {

	// CORS config
	corsConfig := common.DefaultCORSConfig()

	// Add global /openapi route
	r.Handle("GET", "/openapi", func(w http.ResponseWriter, req bunrouter.Request) error {
		respAdapter := router.NewHTTPResponseWriter(w)
		reqAdapter := router.NewHTTPRequest(req.Request)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
		handler.HandleOpenAPI(respAdapter, reqAdapter)
		return nil
	})

	r.Handle("OPTIONS", "/openapi", func(w http.ResponseWriter, req bunrouter.Request) error {
		respAdapter := router.NewHTTPResponseWriter(w)
		reqAdapter := router.NewHTTPRequest(req.Request)
		common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
		return nil
	})

	// Get all registered models from the registry
	allModels := handler.registry.GetAllModels()

	// Loop through each registered model and create explicit routes
	for fullName := range allModels {
		// Parse the full name (e.g., "public.users" or just "users")
		schema, entity := parseModelName(fullName)

		// Build the route paths
		entityPath := buildRoutePath(schema, entity)
		entityWithIDPath := entityPath + "/:id"

		// Create closure variables to capture current schema and entity
		currentSchema := schema
		currentEntity := entity

		// POST route without ID
		r.Handle("POST", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}

			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		// POST route with ID
		r.Handle("POST", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}

			handler.Handle(respAdapter, reqAdapter, params)
			return nil
		})

		// GET route without ID
		r.Handle("GET", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}

			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})

		// GET route with ID
		r.Handle("GET", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			common.SetCORSHeaders(respAdapter, reqAdapter, corsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
				"id":     req.Param("id"),
			}

			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})

		// OPTIONS route without ID (returns metadata)
		r.Handle("OPTIONS", entityPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			optionsCorsConfig := corsConfig
			optionsCorsConfig.AllowedMethods = []string{"GET", "POST", "OPTIONS"}
			common.SetCORSHeaders(respAdapter, reqAdapter, optionsCorsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}

			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})

		// OPTIONS route with ID (returns metadata)
		r.Handle("OPTIONS", entityWithIDPath, func(w http.ResponseWriter, req bunrouter.Request) error {
			respAdapter := router.NewHTTPResponseWriter(w)
			reqAdapter := router.NewHTTPRequest(req.Request)
			optionsCorsConfig := corsConfig
			optionsCorsConfig.AllowedMethods = []string{"POST", "OPTIONS"}
			common.SetCORSHeaders(respAdapter, reqAdapter, optionsCorsConfig)
			params := map[string]string{
				"schema": currentSchema,
				"entity": currentEntity,
			}

			handler.HandleGet(respAdapter, reqAdapter, params)
			return nil
		})
	}
}

// ExampleWithBunRouter shows how to use bunrouter from uptrace
func ExampleWithBunRouter(bunDB *bun.DB) {
	// Create handler with Bun adapter
	handler := NewHandlerWithBun(bunDB)

	// Create bunrouter
	bunRouter := bunrouter.New()

	// Setup ResolveSpec routes with bunrouter
	SetupBunRouterRoutes(bunRouter, handler)

	// Start server
	// http.ListenAndServe(":8080", bunRouter)
}

// ExampleBunRouterWithBunDB shows the full uptrace stack (bunrouter + Bun ORM)
func ExampleBunRouterWithBunDB(bunDB *bun.DB) {
	// Create Bun database adapter
	dbAdapter := database.NewBunAdapter(bunDB)

	// Create model registry
	registry := modelregistry.NewModelRegistry()
	// registry.RegisterModel("public.users", &User{})

	// Create handler with Bun
	handler := NewHandler(dbAdapter, registry)

	// Create bunrouter
	bunRouter := bunrouter.New()

	// Setup ResolveSpec routes
	SetupBunRouterRoutes(bunRouter, handler)

	// This gives you the full uptrace stack: bunrouter + Bun ORM
	// http.ListenAndServe(":8080", bunRouter)
}

// ExampleBunRouterWithGroup shows how to use SetupBunRouterRoutes with a bunrouter.Group
func ExampleBunRouterWithGroup(bunDB *bun.DB) {
	// Create handler with Bun adapter
	handler := NewHandlerWithBun(bunDB)

	// Create bunrouter
	bunRouter := bunrouter.New()

	// Create a route group with a prefix
	apiGroup := bunRouter.NewGroup("/api")

	// Setup ResolveSpec routes on the group - routes will be under /api
	SetupBunRouterRoutes(apiGroup, handler)

	// Start server
	// http.ListenAndServe(":8080", bunRouter)
}
