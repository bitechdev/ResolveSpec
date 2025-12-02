package resolvespec

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/uptrace/bun"
	"github.com/uptrace/bunrouter"
	"gorm.io/gorm"

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
	// Create handler functions
	postEntityHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		reqAdapter := router.NewHTTPRequest(r)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.Handle(respAdapter, reqAdapter, vars)
	})

	postEntityWithIDHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		reqAdapter := router.NewHTTPRequest(r)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.Handle(respAdapter, reqAdapter, vars)
	})

	getEntityHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		reqAdapter := router.NewHTTPRequest(r)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.HandleGet(respAdapter, reqAdapter, vars)
	})

	// Apply authentication middleware if provided
	if authMiddleware != nil {
		postEntityHandler = authMiddleware(postEntityHandler).(http.HandlerFunc)
		postEntityWithIDHandler = authMiddleware(postEntityWithIDHandler).(http.HandlerFunc)
		getEntityHandler = authMiddleware(getEntityHandler).(http.HandlerFunc)
	}

	// Register routes
	muxRouter.Handle("/{schema}/{entity}", postEntityHandler).Methods("POST")
	muxRouter.Handle("/{schema}/{entity}/{id}", postEntityWithIDHandler).Methods("POST")
	muxRouter.Handle("/{schema}/{entity}", getEntityHandler).Methods("GET")
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

// SetupBunRouterRoutes sets up bunrouter routes for the ResolveSpec API
func SetupBunRouterRoutes(bunRouter *router.StandardBunRouterAdapter, handler *Handler) {
	r := bunRouter.GetBunRouter()

	r.Handle("POST", "/:schema/:entity", func(w http.ResponseWriter, req bunrouter.Request) error {
		params := map[string]string{
			"schema": req.Param("schema"),
			"entity": req.Param("entity"),
		}
		reqAdapter := router.NewHTTPRequest(req.Request)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.Handle(respAdapter, reqAdapter, params)
		return nil
	})

	r.Handle("POST", "/:schema/:entity/:id", func(w http.ResponseWriter, req bunrouter.Request) error {
		params := map[string]string{
			"schema": req.Param("schema"),
			"entity": req.Param("entity"),
			"id":     req.Param("id"),
		}
		reqAdapter := router.NewHTTPRequest(req.Request)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.Handle(respAdapter, reqAdapter, params)
		return nil
	})

	r.Handle("GET", "/:schema/:entity", func(w http.ResponseWriter, req bunrouter.Request) error {
		params := map[string]string{
			"schema": req.Param("schema"),
			"entity": req.Param("entity"),
		}
		reqAdapter := router.NewHTTPRequest(req.Request)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.HandleGet(respAdapter, reqAdapter, params)
		return nil
	})

	r.Handle("GET", "/:schema/:entity/:id", func(w http.ResponseWriter, req bunrouter.Request) error {
		params := map[string]string{
			"schema": req.Param("schema"),
			"entity": req.Param("entity"),
			"id":     req.Param("id"),
		}
		reqAdapter := router.NewHTTPRequest(req.Request)
		respAdapter := router.NewHTTPResponseWriter(w)
		handler.HandleGet(respAdapter, reqAdapter, params)
		return nil
	})
}

// ExampleWithBunRouter shows how to use bunrouter from uptrace
func ExampleWithBunRouter(bunDB *bun.DB) {
	// Create handler with Bun adapter
	handler := NewHandlerWithBun(bunDB)

	// Create bunrouter
	bunRouter := router.NewStandardBunRouterAdapter()

	// Setup ResolveSpec routes with bunrouter
	SetupBunRouterRoutes(bunRouter, handler)

	// Start server
	// http.ListenAndServe(":8080", bunRouter.GetBunRouter())
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
	bunRouter := router.NewStandardBunRouterAdapter()

	// Setup ResolveSpec routes
	SetupBunRouterRoutes(bunRouter, handler)

	// This gives you the full uptrace stack: bunrouter + Bun ORM
	// http.ListenAndServe(":8080", bunRouter.GetBunRouter())
}
