package resolvemcp

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// --------------------------------------------------------------------------
// OAuth2 registration on the Handler
// --------------------------------------------------------------------------

// oauth2Registration stores a configured auth provider and its route config.
type oauth2Registration struct {
	auth *security.DatabaseAuthenticator
	cfg  OAuth2RouteConfig
}

// RegisterOAuth2 attaches an OAuth2 provider to the Handler.
// The login and callback HTTP routes are served by HTTPHandler / StreamableHTTPMux.
// Call this once per provider before serving requests.
//
// Example:
//
//	auth := security.NewGoogleAuthenticator(clientID, secret, redirectURL, db)
//	handler.RegisterOAuth2(auth, resolvemcp.OAuth2RouteConfig{
//	    ProviderName:       "google",
//	    LoginPath:          "/auth/google/login",
//	    CallbackPath:       "/auth/google/callback",
//	    AfterLoginRedirect: "/",
//	})
func (h *Handler) RegisterOAuth2(auth *security.DatabaseAuthenticator, cfg OAuth2RouteConfig) {
	h.oauth2Regs = append(h.oauth2Regs, oauth2Registration{auth: auth, cfg: cfg})
}

// HTTPHandler returns a single http.Handler that serves:
//   - MCP OAuth2 authorization server endpoints (when EnableOAuthServer has been called)
//   - OAuth2 login + callback routes for every registered provider (legacy cookie flow)
//   - The MCP SSE transport wrapped with required authentication middleware
//
// Example:
//
//	auth := security.NewGoogleAuthenticator(...)
//	handler.RegisterOAuth2(auth, cfg)
//	handler.EnableOAuthServer(security.OAuthServerConfig{Issuer: "https://api.example.com"})
//	security.RegisterSecurityHooks(handler, securityList)
//	http.ListenAndServe(":8080", handler.HTTPHandler(securityList))
func (h *Handler) HTTPHandler(securityList *security.SecurityList) http.Handler {
	mux := http.NewServeMux()
	if h.oauthSrv != nil {
		h.mountOAuthServerRoutes(mux)
	}
	h.mountOAuth2Routes(mux)

	mcpHandler := h.AuthedSSEServer(securityList)
	basePath := h.config.BasePath
	if basePath == "" {
		basePath = "/mcp"
	}
	mux.Handle(basePath+"/sse", mcpHandler)
	mux.Handle(basePath+"/message", mcpHandler)
	mux.Handle(basePath+"/", http.StripPrefix(basePath, mcpHandler))

	return mux
}

// StreamableHTTPMux returns a single http.Handler that serves:
//   - MCP OAuth2 authorization server endpoints (when EnableOAuthServer has been called)
//   - OAuth2 login + callback routes for every registered provider (legacy cookie flow)
//   - The MCP streamable HTTP transport wrapped with required authentication middleware
//
// Example:
//
//	http.ListenAndServe(":8080", handler.StreamableHTTPMux(securityList))
func (h *Handler) StreamableHTTPMux(securityList *security.SecurityList) http.Handler {
	mux := http.NewServeMux()
	if h.oauthSrv != nil {
		h.mountOAuthServerRoutes(mux)
	}
	h.mountOAuth2Routes(mux)

	mcpHandler := h.AuthedStreamableHTTPServer(securityList)
	basePath := h.config.BasePath
	if basePath == "" {
		basePath = "/mcp"
	}
	mux.Handle(basePath+"/", http.StripPrefix(basePath, mcpHandler))
	mux.Handle(basePath, mcpHandler)

	return mux
}

// mountOAuth2Routes registers all stored OAuth2 login+callback routes onto mux.
func (h *Handler) mountOAuth2Routes(mux *http.ServeMux) {
	for _, reg := range h.oauth2Regs {
		var cookieOpts []security.SessionCookieOptions
		if reg.cfg.CookieOptions != nil {
			cookieOpts = append(cookieOpts, *reg.cfg.CookieOptions)
		}
		mux.Handle(reg.cfg.LoginPath, OAuth2LoginHandler(reg.auth, reg.cfg.ProviderName))
		mux.Handle(reg.cfg.CallbackPath, OAuth2CallbackHandler(reg.auth, reg.cfg.ProviderName, reg.cfg.AfterLoginRedirect, cookieOpts...))
	}
}

// --------------------------------------------------------------------------
// Auth-wrapped transports
// --------------------------------------------------------------------------

// AuthedSSEServer wraps SSEServer with required authentication middleware from pkg/security.
// The middleware reads the session cookie / Authorization header and populates the user
// context into the request context, making it available to BeforeHandle security hooks.
// Unauthenticated requests receive 401 before reaching any MCP tool.
func (h *Handler) AuthedSSEServer(securityList *security.SecurityList) http.Handler {
	return security.NewAuthMiddleware(securityList)(h.SSEServer())
}

// OptionalAuthSSEServer wraps SSEServer with optional authentication middleware.
// Unauthenticated requests continue as guest rather than returning 401.
// Use together with RegisterSecurityHooks and per-model CanPublicRead/Write rules
// to allow mixed public/private access.
func (h *Handler) OptionalAuthSSEServer(securityList *security.SecurityList) http.Handler {
	return security.NewOptionalAuthMiddleware(securityList)(h.SSEServer())
}

// AuthedStreamableHTTPServer wraps StreamableHTTPServer with required authentication middleware.
func (h *Handler) AuthedStreamableHTTPServer(securityList *security.SecurityList) http.Handler {
	return security.NewAuthMiddleware(securityList)(h.StreamableHTTPServer())
}

// OptionalAuthStreamableHTTPServer wraps StreamableHTTPServer with optional authentication middleware.
func (h *Handler) OptionalAuthStreamableHTTPServer(securityList *security.SecurityList) http.Handler {
	return security.NewOptionalAuthMiddleware(securityList)(h.StreamableHTTPServer())
}

// --------------------------------------------------------------------------
// OAuth2 route config and standalone handlers
// --------------------------------------------------------------------------

// OAuth2RouteConfig configures the OAuth2 HTTP endpoints for a single provider.
type OAuth2RouteConfig struct {
	// ProviderName is the OAuth2 provider name as registered with WithOAuth2()
	// (e.g. "google", "github", "microsoft").
	ProviderName string

	// LoginPath is the HTTP path that redirects the browser to the OAuth2 provider
	// (e.g. "/auth/google/login").
	LoginPath string

	// CallbackPath is the HTTP path that the OAuth2 provider redirects back to
	// (e.g. "/auth/google/callback"). Must match the RedirectURL in OAuth2Config.
	CallbackPath string

	// AfterLoginRedirect is the URL to redirect the browser to after a successful
	// login. When empty the LoginResponse JSON is written directly to the response.
	AfterLoginRedirect string

	// CookieOptions customises the session cookie written on successful login.
	// Defaults to HttpOnly, Secure, SameSite=Lax when nil.
	CookieOptions *security.SessionCookieOptions
}

// OAuth2LoginHandler returns an http.HandlerFunc that redirects the browser to
// the OAuth2 provider's authorization URL.
//
// Register it on any router:
//
//	mux.Handle("/auth/google/login", resolvemcp.OAuth2LoginHandler(auth, "google"))
func OAuth2LoginHandler(auth *security.DatabaseAuthenticator, providerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := auth.OAuth2GenerateState()
		if err != nil {
			http.Error(w, "failed to generate state", http.StatusInternalServerError)
			return
		}
		authURL, err := auth.OAuth2GetAuthURL(providerName, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	}
}

// OAuth2CallbackHandler returns an http.HandlerFunc that handles the OAuth2 provider
// callback: exchanges the authorization code for a session token, writes the session
// cookie, then either redirects to afterLoginRedirect or writes the LoginResponse as JSON.
//
// Register it on any router:
//
//	mux.Handle("/auth/google/callback", resolvemcp.OAuth2CallbackHandler(auth, "google", "/dashboard"))
func OAuth2CallbackHandler(auth *security.DatabaseAuthenticator, providerName, afterLoginRedirect string, cookieOpts ...security.SessionCookieOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}

		loginResp, err := auth.OAuth2HandleCallback(r.Context(), providerName, code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		security.SetSessionCookie(w, loginResp, cookieOpts...)

		if afterLoginRedirect != "" {
			http.Redirect(w, r, afterLoginRedirect, http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(loginResp) //nolint:errcheck
	}
}

// --------------------------------------------------------------------------
// Gorilla Mux convenience helpers
// --------------------------------------------------------------------------

// SetupMuxOAuth2Routes registers the OAuth2 login and callback routes on a Gorilla Mux router.
//
// Example:
//
//	resolvemcp.SetupMuxOAuth2Routes(r, auth, resolvemcp.OAuth2RouteConfig{
//	    ProviderName: "google", LoginPath: "/auth/google/login",
//	    CallbackPath: "/auth/google/callback", AfterLoginRedirect: "/",
//	})
func SetupMuxOAuth2Routes(muxRouter *mux.Router, auth *security.DatabaseAuthenticator, cfg OAuth2RouteConfig) {
	var cookieOpts []security.SessionCookieOptions
	if cfg.CookieOptions != nil {
		cookieOpts = append(cookieOpts, *cfg.CookieOptions)
	}

	muxRouter.Handle(cfg.LoginPath,
		OAuth2LoginHandler(auth, cfg.ProviderName),
	).Methods(http.MethodGet)

	muxRouter.Handle(cfg.CallbackPath,
		OAuth2CallbackHandler(auth, cfg.ProviderName, cfg.AfterLoginRedirect, cookieOpts...),
	).Methods(http.MethodGet)
}

// SetupMuxRoutesWithAuth mounts the MCP SSE endpoints on a Gorilla Mux router
// with required authentication middleware applied.
func SetupMuxRoutesWithAuth(muxRouter *mux.Router, handler *Handler, securityList *security.SecurityList) {
	basePath := handler.config.BasePath
	h := handler.AuthedSSEServer(securityList)

	muxRouter.Handle(basePath+"/sse", h).Methods(http.MethodGet, http.MethodOptions)
	muxRouter.Handle(basePath+"/message", h).Methods(http.MethodPost, http.MethodOptions)
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, h))
}

// SetupMuxStreamableHTTPRoutesWithAuth mounts the MCP streamable HTTP endpoint on a
// Gorilla Mux router with required authentication middleware applied.
func SetupMuxStreamableHTTPRoutesWithAuth(muxRouter *mux.Router, handler *Handler, securityList *security.SecurityList) {
	basePath := handler.config.BasePath
	h := handler.AuthedStreamableHTTPServer(securityList)
	muxRouter.PathPrefix(basePath).Handler(http.StripPrefix(basePath, h))
}
