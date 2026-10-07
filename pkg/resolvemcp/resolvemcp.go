package resolvemcp

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gorilla/mux"
	"github.com/uptrace/bun"
	bunrouter "github.com/uptrace/bunrouter"
	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// Config holds configuration for the resolvemcp handler.
type Config struct {
	// BaseURL is the public-facing base URL of the server (e.g. "http://localhost:8080").
	// It is sent to MCP clients during the SSE handshake so they know where to POST messages.
	BaseURL string

	// BasePath is the URL path prefix where the MCP endpoints are mounted (e.g. "/mcp").
	// If empty, the path is detected from each incoming request automatically.
	BasePath string

	// Limits. Zero values take the defaults shown.

	// DefaultLimit is the page size when a read gives no limit (50).
	DefaultLimit int
	// MaxLimit caps a read's limit; larger values are clamped (1000).
	MaxLimit int
	// MaxOffset rejects a read whose offset is larger (100000).
	MaxOffset int
	// MaxBatch caps the items in one batch create (100).
	MaxBatch int
	// MaxPreloadDepth caps the depth of a preload path such as "a.b.c" (2).
	MaxPreloadDepth int
	// MaxWriteRows caps the rows a filter-based update or delete may touch (100).
	MaxWriteRows int
	// QueryTimeout bounds one tool call, hooks and queries included (30s).
	QueryTimeout time.Duration
	// ConfirmTTL is how long a confirmation token for a filter write stays valid (5m).
	ConfirmTTL time.Duration

	// AllowedHosts restricts the Host header accepted by the SSE transport when BaseURL is
	// empty (the message endpoint URL sent to clients is built from it). Empty accepts any
	// host, with at most 32 distinct base URLs cached; prefer setting BaseURL.
	AllowedHosts []string

	// EnableAnnotations registers the resolvespec_annotate tool. Off by default: annotations
	// are free text that agents read back, so enabling the tool opens a write channel into
	// agent-visible text. When on, every call runs the BeforeHandle hooks (operation
	// "annotate_set" / "annotate_get") and the writes run in a transaction with OnTxBegin.
	EnableAnnotations bool
}

// withDefaults fills the zero limit fields.
func (c Config) withDefaults() Config {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.DefaultLimit, 50)
	def(&c.MaxLimit, 1000)
	def(&c.MaxOffset, 100000)
	def(&c.MaxBatch, 100)
	def(&c.MaxPreloadDepth, 2)
	def(&c.MaxWriteRows, 100)
	if c.DefaultLimit > c.MaxLimit {
		c.DefaultLimit = c.MaxLimit
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = 30 * time.Second
	}
	if c.ConfirmTTL <= 0 {
		c.ConfirmTTL = 5 * time.Minute
	}
	return c
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
// using the base path from Config.BasePath, behind Guard(securityList).
//
// Routes registered:
//   - GET  {basePath}/sse     — SSE connection endpoint (client subscribes here)
//   - POST {basePath}/message — JSON-RPC message endpoint (client sends requests here)
//
// Nothing is mounted (and an error is logged) when securityList has no provider.
func SetupMuxRoutes(muxRouter *mux.Router, handler *Handler, securityList *security.SecurityList) {
	if !requireGuard("SetupMuxRoutes", securityList) {
		return
	}
	mountMuxSSE(muxRouter, handler, handler.AuthedSSEServer(securityList))
}

// SetupMuxRoutesUnauthenticated is SetupMuxRoutes without the guard. Every caller reaches every
// registered model, so use it only behind another trusted layer. A warning is logged.
func SetupMuxRoutesUnauthenticated(muxRouter *mux.Router, handler *Handler) {
	warnUnauthenticated("SetupMuxRoutesUnauthenticated")
	mountMuxSSE(muxRouter, handler, handler.SSEServer())
}

func mountMuxSSE(muxRouter *mux.Router, handler *Handler, h http.Handler) {
	basePath := handler.config.BasePath
	muxRouter.Handle(basePath+"/sse", h).Methods("GET", "OPTIONS")
	muxRouter.Handle(basePath+"/message", h).Methods("POST", "OPTIONS")

	// Convenience: also expose the full SSE server at basePath for clients that
	// use ServeHTTP directly (e.g. net/http default mux).
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, h))
}

// SetupBunRouterRoutes mounts the MCP HTTP/SSE endpoints on a bunrouter router
// using the base path from Config.BasePath, behind Guard(securityList).
//
// Routes registered:
//   - GET  {basePath}/sse     — SSE connection endpoint
//   - POST {basePath}/message — JSON-RPC message endpoint
func SetupBunRouterRoutes(router *bunrouter.Router, handler *Handler, securityList *security.SecurityList) {
	if !requireGuard("SetupBunRouterRoutes", securityList) {
		return
	}
	mountBunSSE(router, handler, handler.AuthedSSEServer(securityList))
}

// SetupBunRouterRoutesUnauthenticated is SetupBunRouterRoutes without the guard. A warning is logged.
func SetupBunRouterRoutesUnauthenticated(router *bunrouter.Router, handler *Handler) {
	warnUnauthenticated("SetupBunRouterRoutesUnauthenticated")
	mountBunSSE(router, handler, handler.SSEServer())
}

func mountBunSSE(router *bunrouter.Router, handler *Handler, h http.Handler) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("panic mounting resolvemcp bunrouter routes: %v\n%s", rec, debug.Stack())
		}
	}()

	basePath := handler.config.BasePath
	router.GET(basePath+"/sse", bunrouter.HTTPHandler(h))
	logger.Info("Registered resolvemcp bunrouter route GET %s/sse", basePath)

	router.POST(basePath+"/message", bunrouter.HTTPHandler(h))
	logger.Info("Registered resolvemcp bunrouter route POST %s/message", basePath)
}

// NewSSEServer returns an http.Handler that serves MCP over SSE behind Guard(securityList).
// If Config.BasePath is set it is used directly; otherwise the base path is
// detected from each incoming request (by stripping the "/sse" or "/message" suffix).
//
//	h := resolvemcp.NewSSEServer(handler, securityList)
//	http.Handle("/api/mcp/", h)
func NewSSEServer(handler *Handler, securityList *security.SecurityList) http.Handler {
	return handler.AuthedSSEServer(securityList)
}

// SetupMuxStreamableHTTPRoutes mounts the MCP streamable HTTP endpoint on the given Gorilla Mux
// router, behind Guard(securityList). The streamable HTTP transport uses a single endpoint
// (Config.BasePath) for all communication: POST for client→server messages, GET for
// server→client streaming.
//
// Nothing is mounted (and an error is logged) when securityList has no provider.
func SetupMuxStreamableHTTPRoutes(muxRouter *mux.Router, handler *Handler, securityList *security.SecurityList) {
	if !requireGuard("SetupMuxStreamableHTTPRoutes", securityList) {
		return
	}
	basePath := handler.config.BasePath
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, handler.AuthedStreamableHTTPServer(securityList)))
}

// SetupMuxStreamableHTTPRoutesUnauthenticated is SetupMuxStreamableHTTPRoutes without the guard.
// A warning is logged.
func SetupMuxStreamableHTTPRoutesUnauthenticated(muxRouter *mux.Router, handler *Handler) {
	warnUnauthenticated("SetupMuxStreamableHTTPRoutesUnauthenticated")
	basePath := handler.config.BasePath
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, handler.StreamableHTTPServer()))
}

// SetupBunRouterStreamableHTTPRoutes mounts the MCP streamable HTTP endpoint on a bunrouter
// router, behind Guard(securityList). The transport uses a single endpoint (Config.BasePath).
func SetupBunRouterStreamableHTTPRoutes(router *bunrouter.Router, handler *Handler, securityList *security.SecurityList) {
	if !requireGuard("SetupBunRouterStreamableHTTPRoutes", securityList) {
		return
	}
	mountBunStreamable(router, handler, handler.AuthedStreamableHTTPServer(securityList))
}

// SetupBunRouterStreamableHTTPRoutesUnauthenticated is SetupBunRouterStreamableHTTPRoutes
// without the guard. A warning is logged.
func SetupBunRouterStreamableHTTPRoutesUnauthenticated(router *bunrouter.Router, handler *Handler) {
	warnUnauthenticated("SetupBunRouterStreamableHTTPRoutesUnauthenticated")
	mountBunStreamable(router, handler, handler.StreamableHTTPServer())
}

func mountBunStreamable(router *bunrouter.Router, handler *Handler, h http.Handler) {
	basePath := handler.config.BasePath
	router.GET(basePath, bunrouter.HTTPHandler(h))
	router.POST(basePath, bunrouter.HTTPHandler(h))
	router.DELETE(basePath, bunrouter.HTTPHandler(h))
}

// NewStreamableHTTPHandler returns an http.Handler that serves MCP over the streamable HTTP
// transport behind Guard(securityList). Mount it at the desired path; that path becomes the
// MCP endpoint.
//
//	h := resolvemcp.NewStreamableHTTPHandler(handler, securityList)
//	http.Handle("/mcp", h)
//	engine.Any("/mcp", gin.WrapH(h))
func NewStreamableHTTPHandler(handler *Handler, securityList *security.SecurityList) http.Handler {
	return handler.AuthedStreamableHTTPServer(securityList)
}
