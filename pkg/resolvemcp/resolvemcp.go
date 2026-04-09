// Package resolvemcp exposes registered database models as Model Context Protocol (MCP) tools
// and resources over HTTP/SSE transport.
//
// It mirrors the resolvespec package patterns:
//   - Same model registration API
//   - Same filter, sort, cursor pagination, preload options
//   - Same lifecycle hook system
//
// Usage:
//
//	handler := resolvemcp.NewHandlerWithGORM(db, resolvemcp.Config{BaseURL: "http://localhost:8080"})
//	handler.RegisterModel("public", "users", &User{})
//
//	r := mux.NewRouter()
//	resolvemcp.SetupMuxRoutes(r, handler)
package resolvemcp

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/uptrace/bun"
	bunrouter "github.com/uptrace/bunrouter"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// Config holds configuration for the resolvemcp handler.
type Config struct {
	// BaseURL is the public-facing base URL of the server (e.g. "http://localhost:8080").
	// It is sent to MCP clients during the SSE handshake so they know where to POST messages.
	BaseURL string

	// BasePath is the URL path prefix where the MCP endpoints are mounted (e.g. "/mcp").
	// If empty, the path is detected from each incoming request automatically.
	BasePath string
}

// NewHandlerWithGORM creates a Handler backed by a GORM database connection.
func NewHandlerWithGORM(db *gorm.DB, cfg Config) *Handler {
	return NewHandler(database.NewGormAdapter(db), modelregistry.NewModelRegistry(), cfg)
}

// NewHandlerWithBun creates a Handler backed by a Bun database connection.
func NewHandlerWithBun(db *bun.DB, cfg Config) *Handler {
	return NewHandler(database.NewBunAdapter(db), modelregistry.NewModelRegistry(), cfg)
}

// NewHandlerWithDB creates a Handler using an existing common.Database and a new registry.
func NewHandlerWithDB(db common.Database, cfg Config) *Handler {
	return NewHandler(db, modelregistry.NewModelRegistry(), cfg)
}

// SetupMuxRoutes mounts the MCP HTTP/SSE endpoints on the given Gorilla Mux router
// using the base path from Config.BasePath (falls back to "/mcp" if empty).
//
// Two routes are registered:
//   - GET  {basePath}/sse     — SSE connection endpoint (client subscribes here)
//   - POST {basePath}/message — JSON-RPC message endpoint (client sends requests here)
//
// To protect these routes with authentication, wrap the mux router or apply middleware
// before calling SetupMuxRoutes.
func SetupMuxRoutes(muxRouter *mux.Router, handler *Handler) {
	basePath := handler.config.BasePath
	h := handler.SSEServer()

	muxRouter.Handle(basePath+"/sse", h).Methods("GET", "OPTIONS")
	muxRouter.Handle(basePath+"/message", h).Methods("POST", "OPTIONS")

	// Convenience: also expose the full SSE server at basePath for clients that
	// use ServeHTTP directly (e.g. net/http default mux).
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, h))
}

// SetupBunRouterRoutes mounts the MCP HTTP/SSE endpoints on a bunrouter router
// using the base path from Config.BasePath.
//
// Two routes are registered:
//   - GET  {basePath}/sse     — SSE connection endpoint
//   - POST {basePath}/message — JSON-RPC message endpoint
func SetupBunRouterRoutes(router *bunrouter.Router, handler *Handler) {
	basePath := handler.config.BasePath
	h := handler.SSEServer()

	router.GET(basePath+"/sse", bunrouter.HTTPHandler(h))
	router.POST(basePath+"/message", bunrouter.HTTPHandler(h))
}

// NewSSEServer returns an http.Handler that serves MCP over SSE.
// If Config.BasePath is set it is used directly; otherwise the base path is
// detected from each incoming request (by stripping the "/sse" or "/message" suffix).
//
//	h := resolvemcp.NewSSEServer(handler)
//	http.Handle("/api/mcp/", h)
func NewSSEServer(handler *Handler) http.Handler {
	return handler.SSEServer()
}

// SetupMuxStreamableHTTPRoutes mounts the MCP streamable HTTP endpoint on the given Gorilla Mux router.
// The streamable HTTP transport uses a single endpoint (Config.BasePath) for all communication:
// POST for client→server messages, GET for server→client streaming.
//
// Example:
//
//	resolvemcp.SetupMuxStreamableHTTPRoutes(r, handler) // mounts at Config.BasePath
func SetupMuxStreamableHTTPRoutes(muxRouter *mux.Router, handler *Handler) {
	basePath := handler.config.BasePath
	h := handler.StreamableHTTPServer()
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, h))
}

// SetupBunRouterStreamableHTTPRoutes mounts the MCP streamable HTTP endpoint on a bunrouter router.
// The streamable HTTP transport uses a single endpoint (Config.BasePath).
func SetupBunRouterStreamableHTTPRoutes(router *bunrouter.Router, handler *Handler) {
	basePath := handler.config.BasePath
	h := handler.StreamableHTTPServer()
	router.GET(basePath, bunrouter.HTTPHandler(h))
	router.POST(basePath, bunrouter.HTTPHandler(h))
	router.DELETE(basePath, bunrouter.HTTPHandler(h))
}

// NewStreamableHTTPHandler returns an http.Handler that serves MCP over the streamable HTTP transport.
// Mount it at the desired path; that path becomes the MCP endpoint.
//
//	h := resolvemcp.NewStreamableHTTPHandler(handler)
//	http.Handle("/mcp", h)
//	engine.Any("/mcp", gin.WrapH(h))
func NewStreamableHTTPHandler(handler *Handler) http.Handler {
	return handler.StreamableHTTPServer()
}
