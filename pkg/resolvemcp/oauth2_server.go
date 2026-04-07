package resolvemcp

import (
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// EnableOAuthServer activates the MCP-standard OAuth2 authorization server on this Handler.
//
// Pass a DatabaseAuthenticator to enable direct username/password login — the server acts as
// its own identity provider and renders a login form at /oauth/authorize. Pass nil to use
// only external providers registered via RegisterOAuth2Provider.
//
// After calling this, HTTPHandler and StreamableHTTPMux serve the full set of RFC-compliant
// endpoints required by MCP clients alongside the MCP transport:
//
//	GET  /.well-known/oauth-authorization-server   RFC 8414 — auto-discovery
//	POST /oauth/register                            RFC 7591 — dynamic client registration
//	GET  /oauth/authorize                           OAuth 2.1 + PKCE — start login
//	POST /oauth/authorize                           Login form submission (password flow)
//	POST /oauth/token                               Bearer token exchange + refresh
//	GET  /oauth/provider/callback                   External provider redirect target
func (h *Handler) EnableOAuthServer(cfg security.OAuthServerConfig, auth *security.DatabaseAuthenticator) {
	h.oauthSrv = security.NewOAuthServer(cfg, auth)
	// Wire any external providers already registered via RegisterOAuth2
	for _, reg := range h.oauth2Regs {
		h.oauthSrv.RegisterExternalProvider(reg.auth, reg.cfg.ProviderName)
	}
}

// RegisterOAuth2Provider adds an external OAuth2 provider to the MCP OAuth2 authorization server.
// EnableOAuthServer must be called before this. The auth must have been configured with
// WithOAuth2(providerName, ...) for the given provider name.
func (h *Handler) RegisterOAuth2Provider(auth *security.DatabaseAuthenticator, providerName string) {
	if h.oauthSrv != nil {
		h.oauthSrv.RegisterExternalProvider(auth, providerName)
	}
}

// mountOAuthServerRoutes mounts the security.OAuthServer's HTTP handler onto mux.
func (h *Handler) mountOAuthServerRoutes(mux *http.ServeMux) {
	oauthHandler := h.oauthSrv.HTTPHandler()
	// Delegate all /oauth/ and /.well-known/ paths to the OAuth server
	mux.Handle("/.well-known/", oauthHandler)
	mux.Handle("/oauth/", oauthHandler)
	if h.oauthSrv != nil {
		// Also mount the external provider callback path if it differs from /oauth/
		mux.Handle(h.oauthSrv.ProviderCallbackPath(), oauthHandler)
	}
}
