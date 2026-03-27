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
//	handler := resolvemcp.NewHandlerWithGORM(db)
//	handler.RegisterModel("public", "users", &User{})
//
//	r := mux.NewRouter()
//	resolvemcp.SetupMuxRoutes(r, handler, "http://localhost:8080")
package resolvemcp

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mark3labs/mcp-go/server"
	"github.com/uptrace/bun"
	bunrouter "github.com/uptrace/bunrouter"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// NewHandlerWithGORM creates a Handler backed by a GORM database connection.
func NewHandlerWithGORM(db *gorm.DB) *Handler {
	return NewHandler(database.NewGormAdapter(db), modelregistry.NewModelRegistry())
}

// NewHandlerWithBun creates a Handler backed by a Bun database connection.
func NewHandlerWithBun(db *bun.DB) *Handler {
	return NewHandler(database.NewBunAdapter(db), modelregistry.NewModelRegistry())
}

// NewHandlerWithDB creates a Handler using an existing common.Database and a new registry.
func NewHandlerWithDB(db common.Database) *Handler {
	return NewHandler(db, modelregistry.NewModelRegistry())
}

// SetupMuxRoutes mounts the MCP HTTP/SSE endpoints on the given Gorilla Mux router.
//
// baseURL is the public-facing base URL of the server (e.g. "http://localhost:8080").
// It is sent to MCP clients during the SSE handshake so they know where to POST messages.
//
// Two routes are registered:
//   - GET  /mcp/sse     — SSE connection endpoint (client subscribes here)
//   - POST /mcp/message — JSON-RPC message endpoint (client sends requests here)
//
// To protect these routes with authentication, wrap the mux router or apply middleware
// before calling SetupMuxRoutes.
func SetupMuxRoutes(muxRouter *mux.Router, handler *Handler, baseURL string) {
	sseServer := server.NewSSEServer(
		handler.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithBasePath("/mcp"),
	)

	muxRouter.Handle("/mcp/sse", sseServer.SSEHandler()).Methods("GET", "OPTIONS")
	muxRouter.Handle("/mcp/message", sseServer.MessageHandler()).Methods("POST", "OPTIONS")

	// Convenience: also expose the full SSE server at /mcp for clients that
	// use ServeHTTP directly (e.g. net/http default mux).
	muxRouter.PathPrefix("/mcp").Handler(http.StripPrefix("/mcp", sseServer))
}

// NewSSEServer creates an *server.SSEServer that can be mounted manually,
// useful when integrating with non-Mux routers or adding extra middleware.
//
//	sseServer := resolvemcp.NewSSEServer(handler, "http://localhost:8080", "/mcp")
//	http.Handle("/mcp/", http.StripPrefix("/mcp", sseServer))
func NewSSEServer(handler *Handler, baseURL, basePath string) *server.SSEServer {
	return server.NewSSEServer(
		handler.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithBasePath(basePath),
	)
}

// SetupBunRouterRoutes mounts the MCP HTTP/SSE endpoints on a bunrouter router.
//
// Two routes are registered under the given basePath prefix:
//   - GET  {basePath}/sse     — SSE connection endpoint
//   - POST {basePath}/message — JSON-RPC message endpoint
func SetupBunRouterRoutes(router *bunrouter.Router, handler *Handler, baseURL, basePath string) {
	sseServer := server.NewSSEServer(
		handler.mcpServer,
		server.WithBaseURL(baseURL),
		server.WithBasePath(basePath),
	)

	router.GET(basePath+"/sse", bunrouter.HTTPHandler(sseServer.SSEHandler()))
	router.POST(basePath+"/message", bunrouter.HTTPHandler(sseServer.MessageHandler()))
}
