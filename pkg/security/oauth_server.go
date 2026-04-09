package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuthServerConfig configures the MCP-standard OAuth2 authorization server.
type OAuthServerConfig struct {
	// Issuer is the public base URL of this server (e.g. "https://api.example.com").
	// Used in /.well-known/oauth-authorization-server and to build endpoint URLs.
	Issuer string

	// ProviderCallbackPath is the path on this server that external OAuth2 providers
	// redirect back to. Defaults to "/oauth/provider/callback".
	ProviderCallbackPath string

	// LoginTitle is shown on the built-in login form when the server acts as its own
	// identity provider. Defaults to "Sign in".
	LoginTitle string

	// PersistClients stores registered clients in the database when a DatabaseAuthenticator is provided.
	// Clients registered during a session survive server restarts.
	PersistClients bool

	// PersistCodes stores authorization codes in the database.
	// Useful for multi-instance deployments. Defaults to in-memory.
	PersistCodes bool

	// DefaultScopes lists scopes advertised in server metadata. Defaults to ["openid","profile","email"].
	DefaultScopes []string

	// AccessTokenTTL is the issued token lifetime. Defaults to 24h.
	AccessTokenTTL time.Duration

	// AuthCodeTTL is the auth code lifetime. Defaults to 2 minutes.
	AuthCodeTTL time.Duration
}

// oauthClient is a dynamically registered OAuth2 client (RFC 7591).
type oauthClient struct {
	ClientID      string   `json:"client_id"`
	RedirectURIs  []string `json:"redirect_uris"`
	ClientName    string   `json:"client_name,omitempty"`
	GrantTypes    []string `json:"grant_types"`
	AllowedScopes []string `json:"allowed_scopes,omitempty"`
}

// pendingAuth tracks an in-progress authorization code exchange.
type pendingAuth struct {
	ClientID            string
	RedirectURI         string
	ClientState         string
	CodeChallenge       string
	CodeChallengeMethod string
	ProviderName        string // empty = password login
	ExpiresAt           time.Time
	SessionToken        string   // set after authentication completes
	RefreshToken        string   // set after authentication completes when refresh tokens are issued
	Scopes              []string // requested scopes
}

// externalProvider pairs a DatabaseAuthenticator with its provider name.
type externalProvider struct {
	auth         *DatabaseAuthenticator
	providerName string
}

// OAuthServer implements the MCP-standard OAuth2 authorization server (OAuth 2.1 + PKCE).
//
// It can act as both:
//   - A direct identity provider using DatabaseAuthenticator username/password login
//   - A federation layer that delegates authentication to external OAuth2 providers
//     (Google, GitHub, Microsoft, etc.) registered via RegisterExternalProvider
//
// The server exposes these RFC-compliant endpoints:
//
//	GET  /.well-known/oauth-authorization-server   RFC 8414 — server metadata discovery
//	POST /oauth/register                            RFC 7591 — dynamic client registration
//	GET  /oauth/authorize                           OAuth 2.1 + PKCE — start authorization
//	POST /oauth/authorize                           Direct login form submission
//	POST /oauth/token                               Token exchange and refresh
//	POST /oauth/revoke                              RFC 7009 — token revocation
//	POST /oauth/introspect                          RFC 7662 — token introspection
//	GET  {ProviderCallbackPath}                     Internal — external provider callback
type OAuthServer struct {
	cfg       OAuthServerConfig
	auth      *DatabaseAuthenticator // nil = only external providers
	providers []externalProvider

	mu      sync.RWMutex
	clients map[string]*oauthClient
	pending map[string]*pendingAuth // provider_state → pending (external flow)
	codes   map[string]*pendingAuth // auth_code     → pending (post-auth)

	done chan struct{} // closed by Close() to stop background goroutines
}

// NewOAuthServer creates a new MCP OAuth2 authorization server.
//
// Pass a DatabaseAuthenticator to enable direct username/password login (the server
// acts as its own identity provider). Pass nil to use only external providers.
// External providers are added separately via RegisterExternalProvider.
//
// Call Close() to stop background goroutines when the server is no longer needed.
func NewOAuthServer(cfg OAuthServerConfig, auth *DatabaseAuthenticator) *OAuthServer {
	if cfg.ProviderCallbackPath == "" {
		cfg.ProviderCallbackPath = "/oauth/provider/callback"
	}
	if cfg.LoginTitle == "" {
		cfg.LoginTitle = "Sign in"
	}
	if len(cfg.DefaultScopes) == 0 {
		cfg.DefaultScopes = []string{"openid", "profile", "email"}
	}
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = 24 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 2 * time.Minute
	}
	// Normalize issuer: trim trailing slash to ensure consistent endpoint URL construction.
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	s := &OAuthServer{
		cfg:     cfg,
		auth:    auth,
		clients: make(map[string]*oauthClient),
		pending: make(map[string]*pendingAuth),
		codes:   make(map[string]*pendingAuth),
		done:    make(chan struct{}),
	}
	go s.cleanupExpired()
	return s
}

// Close stops the background goroutines started by NewOAuthServer.
// It is safe to call Close multiple times.
func (s *OAuthServer) Close() {
	select {
	case <-s.done:
		// already closed
	default:
		close(s.done)
	}
}

// RegisterExternalProvider adds an external OAuth2 provider (Google, GitHub, Microsoft, etc.)
// that handles user authentication via redirect. The DatabaseAuthenticator must have been
// configured with WithOAuth2(providerName, ...) before calling this.
// Multiple providers can be registered; the first is used as the default.
// All providers must be registered before the server starts serving requests.
func (s *OAuthServer) RegisterExternalProvider(auth *DatabaseAuthenticator, providerName string) {
	s.mu.Lock()
	s.providers = append(s.providers, externalProvider{auth: auth, providerName: providerName})
	s.mu.Unlock()
}

// ProviderCallbackPath returns the configured path for external provider callbacks.
func (s *OAuthServer) ProviderCallbackPath() string {
	return s.cfg.ProviderCallbackPath
}

// HTTPHandler returns an http.Handler that serves all RFC-required OAuth2 endpoints.
// Mount it at the root of your HTTP server alongside the MCP transport.
//
//	mux := http.NewServeMux()
//	mux.Handle("/", oauthServer.HTTPHandler())
//	mux.Handle("/mcp/", mcpTransport)
func (s *OAuthServer) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.metadataHandler)
	mux.HandleFunc("/oauth/register", s.registerHandler)
	mux.HandleFunc("/oauth/authorize", s.authorizeHandler)
	mux.HandleFunc("/oauth/token", s.tokenHandler)
	mux.HandleFunc("/oauth/revoke", s.revokeHandler)
	mux.HandleFunc("/oauth/introspect", s.introspectHandler)
	mux.HandleFunc(s.cfg.ProviderCallbackPath, s.providerCallbackHandler)
	return mux
}

// cleanupExpired removes stale pending auths and codes every 5 minutes.
func (s *OAuthServer) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for k, p := range s.pending {
				if now.After(p.ExpiresAt) {
					delete(s.pending, k)
				}
			}
			for k, p := range s.codes {
				if now.After(p.ExpiresAt) {
					delete(s.codes, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// --------------------------------------------------------------------------
// RFC 8414 — Server metadata
// --------------------------------------------------------------------------

func (s *OAuthServer) metadataHandler(w http.ResponseWriter, r *http.Request) {
	issuer := s.cfg.Issuer
	meta := map[string]interface{}{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/oauth/register",
		"revocation_endpoint":                   issuer + "/oauth/revoke",
		"introspection_endpoint":                issuer + "/oauth/introspect",
		"scopes_supported":                      s.cfg.DefaultScopes,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta) //nolint:errcheck
}

// --------------------------------------------------------------------------
// RFC 7591 — Dynamic client registration
// --------------------------------------------------------------------------

func (s *OAuthServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RedirectURIs  []string `json:"redirect_uris"`
		ClientName    string   `json:"client_name"`
		GrantTypes    []string `json:"grant_types"`
		AllowedScopes []string `json:"allowed_scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, "invalid_request", "malformed JSON", http.StatusBadRequest)
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, "invalid_request", "redirect_uris required", http.StatusBadRequest)
		return
	}
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	allowedScopes := req.AllowedScopes
	if len(allowedScopes) == 0 {
		allowedScopes = s.cfg.DefaultScopes
	}
	clientID, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	client := &oauthClient{
		ClientID:      clientID,
		RedirectURIs:  req.RedirectURIs,
		ClientName:    req.ClientName,
		GrantTypes:    grantTypes,
		AllowedScopes: allowedScopes,
	}

	if s.cfg.PersistClients && s.auth != nil {
		dbClient := &OAuthServerClient{
			ClientID:      client.ClientID,
			RedirectURIs:  client.RedirectURIs,
			ClientName:    client.ClientName,
			GrantTypes:    client.GrantTypes,
			AllowedScopes: client.AllowedScopes,
		}
		if _, err := s.auth.OAuthRegisterClient(r.Context(), dbClient); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
	}

	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(client) //nolint:errcheck
}

// --------------------------------------------------------------------------
// Authorization endpoint — GET + POST /oauth/authorize
// --------------------------------------------------------------------------

func (s *OAuthServer) authorizeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.authorizeGet(w, r)
	case http.MethodPost:
		s.authorizePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// authorizeGet validates the request and either:
//   - Redirects to an external provider (if providers are registered)
//   - Renders a login form (if the server is its own identity provider)
func (s *OAuthServer) authorizeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	clientState := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	providerName := q.Get("provider")
	scopeStr := q.Get("scope")
	var scopes []string
	if scopeStr != "" {
		scopes = strings.Fields(scopeStr)
	}

	if q.Get("response_type") != "code" {
		writeOAuthError(w, "unsupported_response_type", "only 'code' is supported", http.StatusBadRequest)
		return
	}
	if codeChallenge == "" {
		writeOAuthError(w, "invalid_request", "code_challenge required (PKCE S256)", http.StatusBadRequest)
		return
	}
	if codeChallengeMethod != "" && codeChallengeMethod != "S256" {
		writeOAuthError(w, "invalid_request", "only S256 code_challenge_method is supported", http.StatusBadRequest)
		return
	}
	client, ok := s.lookupOrFetchClient(r.Context(), clientID)
	if !ok {
		writeOAuthError(w, "invalid_client", "unknown client_id", http.StatusBadRequest)
		return
	}
	if !oauthSliceContains(client.RedirectURIs, redirectURI) {
		writeOAuthError(w, "invalid_request", "redirect_uri not registered", http.StatusBadRequest)
		return
	}

	// External provider path
	if len(s.providers) > 0 {
		s.redirectToExternalProvider(w, r, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, providerName, scopes)
		return
	}

	// Direct login form path (server is its own identity provider)
	if s.auth == nil {
		http.Error(w, "no authentication provider configured", http.StatusInternalServerError)
		return
	}
	s.renderLoginForm(w, r, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, scopeStr, "")
}

// authorizePost handles login form submission for the direct login flow.
func (s *OAuthServer) authorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	clientState := r.FormValue("client_state")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	username := r.FormValue("username")
	password := r.FormValue("password")
	scopeStr := r.FormValue("scope")
	var scopes []string
	if scopeStr != "" {
		scopes = strings.Fields(scopeStr)
	}

	client, ok := s.lookupOrFetchClient(r.Context(), clientID)
	if !ok || !oauthSliceContains(client.RedirectURIs, redirectURI) {
		http.Error(w, "invalid client or redirect_uri", http.StatusBadRequest)
		return
	}
	if s.auth == nil {
		http.Error(w, "no authentication provider configured", http.StatusInternalServerError)
		return
	}

	loginResp, err := s.auth.Login(r.Context(), LoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		s.renderLoginForm(w, r, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, scopeStr, "Invalid username or password")
		return
	}

	s.issueCodeAndRedirect(w, r, loginResp.Token, loginResp.RefreshToken, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, "", scopes)
}

// redirectToExternalProvider stores the pending auth and redirects to the configured provider.
func (s *OAuthServer) redirectToExternalProvider(w http.ResponseWriter, r *http.Request, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, providerName string, scopes []string) {
	var provider *externalProvider
	if providerName != "" {
		for i := range s.providers {
			if s.providers[i].providerName == providerName {
				provider = &s.providers[i]
				break
			}
		}
		if provider == nil {
			http.Error(w, fmt.Sprintf("provider %q not found", providerName), http.StatusBadRequest)
			return
		}
	} else {
		provider = &s.providers[0]
	}

	providerState, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	pending := &pendingAuth{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		ClientState:         clientState,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ProviderName:        provider.providerName,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		Scopes:              scopes,
	}
	s.mu.Lock()
	s.pending[providerState] = pending
	s.mu.Unlock()

	authURL, err := provider.auth.OAuth2GetAuthURL(provider.providerName, providerState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// --------------------------------------------------------------------------
// External provider callback — GET {ProviderCallbackPath}
// --------------------------------------------------------------------------

func (s *OAuthServer) providerCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	providerState := r.URL.Query().Get("state")

	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	pending, ok := s.pending[providerState]
	if ok {
		delete(s.pending, providerState)
	}
	s.mu.Unlock()

	if !ok || time.Now().After(pending.ExpiresAt) {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	provider := s.providerByName(pending.ProviderName)
	if provider == nil {
		http.Error(w, fmt.Sprintf("provider %q not found", pending.ProviderName), http.StatusInternalServerError)
		return
	}

	loginResp, err := provider.auth.OAuth2HandleCallback(r.Context(), pending.ProviderName, code, providerState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	s.issueCodeAndRedirect(w, r, loginResp.Token, loginResp.RefreshToken,
		pending.ClientID, pending.RedirectURI, pending.ClientState,
		pending.CodeChallenge, pending.CodeChallengeMethod, pending.ProviderName, pending.Scopes)
}

// issueCodeAndRedirect generates a short-lived auth code and redirects to the MCP client.
func (s *OAuthServer) issueCodeAndRedirect(w http.ResponseWriter, r *http.Request, sessionToken, refreshToken, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, providerName string, scopes []string) {
	authCode, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	pending := &pendingAuth{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		ClientState:         clientState,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ProviderName:        providerName,
		SessionToken:        sessionToken,
		RefreshToken:        refreshToken,
		ExpiresAt:           time.Now().Add(s.cfg.AuthCodeTTL),
		Scopes:              scopes,
	}

	if s.cfg.PersistCodes && s.auth != nil {
		oauthCode := &OAuthCode{
			Code:                authCode,
			ClientID:            clientID,
			RedirectURI:         redirectURI,
			ClientState:         clientState,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			SessionToken:        sessionToken,
			RefreshToken:        refreshToken,
			Scopes:              scopes,
			ExpiresAt:           pending.ExpiresAt,
		}
		if err := s.auth.OAuthSaveCode(r.Context(), oauthCode); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
	} else {
		s.mu.Lock()
		s.codes[authCode] = pending
		s.mu.Unlock()
	}

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusInternalServerError)
		return
	}
	qp := redirectURL.Query()
	qp.Set("code", authCode)
	if clientState != "" {
		qp.Set("state", clientState)
	}
	redirectURL.RawQuery = qp.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// --------------------------------------------------------------------------
// Token endpoint — POST /oauth/token
// --------------------------------------------------------------------------

func (s *OAuthServer) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	switch r.FormValue("grant_type") {
	case "authorization_code":
		s.handleAuthCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshGrant(w, r)
	default:
		writeOAuthError(w, "unsupported_grant_type", "", http.StatusBadRequest)
	}
}

func (s *OAuthServer) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	codeVerifier := r.FormValue("code_verifier")

	if code == "" || codeVerifier == "" {
		writeOAuthError(w, "invalid_request", "code and code_verifier required", http.StatusBadRequest)
		return
	}

	var sessionToken string
	var refreshToken string
	var scopes []string

	if s.cfg.PersistCodes && s.auth != nil {
		oauthCode, err := s.auth.OAuthExchangeCode(r.Context(), code)
		if err != nil {
			writeOAuthError(w, "invalid_grant", "code expired or invalid", http.StatusBadRequest)
			return
		}
		if oauthCode.ClientID != clientID {
			writeOAuthError(w, "invalid_client", "", http.StatusBadRequest)
			return
		}
		if oauthCode.RedirectURI != redirectURI {
			writeOAuthError(w, "invalid_grant", "redirect_uri mismatch", http.StatusBadRequest)
			return
		}
		if !validatePKCESHA256(oauthCode.CodeChallenge, codeVerifier) {
			writeOAuthError(w, "invalid_grant", "code_verifier invalid", http.StatusBadRequest)
			return
		}
		sessionToken = oauthCode.SessionToken
		refreshToken = oauthCode.RefreshToken
		scopes = oauthCode.Scopes
	} else {
		s.mu.Lock()
		pending, ok := s.codes[code]
		if ok {
			delete(s.codes, code)
		}
		s.mu.Unlock()

		if !ok || time.Now().After(pending.ExpiresAt) {
			writeOAuthError(w, "invalid_grant", "code expired or invalid", http.StatusBadRequest)
			return
		}
		if pending.ClientID != clientID {
			writeOAuthError(w, "invalid_client", "", http.StatusBadRequest)
			return
		}
		if pending.RedirectURI != redirectURI {
			writeOAuthError(w, "invalid_grant", "redirect_uri mismatch", http.StatusBadRequest)
			return
		}
		if !validatePKCESHA256(pending.CodeChallenge, codeVerifier) {
			writeOAuthError(w, "invalid_grant", "code_verifier invalid", http.StatusBadRequest)
			return
		}
		sessionToken = pending.SessionToken
		refreshToken = pending.RefreshToken
		scopes = pending.Scopes
	}

	s.writeOAuthToken(w, sessionToken, refreshToken, scopes)
}

func (s *OAuthServer) handleRefreshGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	providerName := r.FormValue("provider")
	if refreshToken == "" {
		writeOAuthError(w, "invalid_request", "refresh_token required", http.StatusBadRequest)
		return
	}

	// Try external providers first, then fall back to DatabaseAuthenticator
	provider := s.providerByName(providerName)
	if provider != nil {
		loginResp, err := provider.auth.OAuth2RefreshToken(r.Context(), refreshToken, providerName)
		if err != nil {
			writeOAuthError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
			return
		}
		s.writeOAuthToken(w, loginResp.Token, loginResp.RefreshToken, nil)
		return
	}

	if s.auth != nil {
		loginResp, err := s.auth.RefreshToken(r.Context(), refreshToken)
		if err != nil {
			writeOAuthError(w, "invalid_grant", err.Error(), http.StatusBadRequest)
			return
		}
		s.writeOAuthToken(w, loginResp.Token, loginResp.RefreshToken, nil)
		return
	}

	writeOAuthError(w, "invalid_grant", "no provider available for refresh", http.StatusBadRequest)
}

// --------------------------------------------------------------------------
// RFC 7009 — Token revocation
// --------------------------------------------------------------------------

func (s *OAuthServer) revokeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	token := r.FormValue("token")
	if token == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.auth != nil {
		s.auth.OAuthRevokeToken(r.Context(), token) //nolint:errcheck
	} else if len(s.providers) > 0 {
		// In external-provider-only mode, attempt revocation via the first provider's auth.
		s.providers[0].auth.OAuthRevokeToken(r.Context(), token) //nolint:errcheck
	}
	w.WriteHeader(http.StatusOK)
}

// --------------------------------------------------------------------------
// RFC 7662 — Token introspection
// --------------------------------------------------------------------------

func (s *OAuthServer) introspectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"active":false}`)) //nolint:errcheck
		return
	}
	token := r.FormValue("token")
	w.Header().Set("Content-Type", "application/json")

	if token == "" {
		w.Write([]byte(`{"active":false}`)) //nolint:errcheck
		return
	}

	// Resolve the authenticator to use: prefer the primary auth, then the first provider's auth.
	authToUse := s.auth
	if authToUse == nil && len(s.providers) > 0 {
		authToUse = s.providers[0].auth
	}
	if authToUse == nil {
		w.Write([]byte(`{"active":false}`)) //nolint:errcheck
		return
	}

	info, err := authToUse.OAuthIntrospectToken(r.Context(), token)
	if err != nil {
		w.Write([]byte(`{"active":false}`)) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(info) //nolint:errcheck
}

// --------------------------------------------------------------------------
// Login form (direct identity provider mode)
// --------------------------------------------------------------------------

func (s *OAuthServer) renderLoginForm(w http.ResponseWriter, r *http.Request, clientID, redirectURI, clientState, codeChallenge, codeChallengeMethod, scope, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	errHTML := ""
	if errMsg != "" {
		errHTML = `<p style="color:red">` + errMsg + `</p>`
	}
	fmt.Fprintf(w, loginFormHTML,
		s.cfg.LoginTitle,
		s.cfg.LoginTitle,
		errHTML,
		clientID,
		htmlEscape(redirectURI),
		htmlEscape(clientState),
		htmlEscape(codeChallenge),
		htmlEscape(codeChallengeMethod),
		htmlEscape(scope),
	)
}

const loginFormHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.15);width:320px}
h2{margin:0 0 1.5rem;font-size:1.25rem}
label{display:block;margin-bottom:.25rem;font-size:.875rem;color:#555}
input[type=text],input[type=password]{width:100%%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;margin-bottom:1rem;font-size:1rem}
button{width:100%%;padding:.6rem;background:#0070f3;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
button:hover{background:#005fd4}.err{color:#d32f2f;margin-bottom:1rem;font-size:.875rem}</style>
</head><body><div class="card">
<h2>%s</h2>%s
<form method="POST" action="authorize">
<input type="hidden" name="client_id" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="client_state" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="%s">
<input type="hidden" name="scope" value="%s">
<label>Username</label><input type="text" name="username" autofocus autocomplete="username">
<label>Password</label><input type="password" name="password" autocomplete="current-password">
<button type="submit">Sign in</button>
</form></div></body></html>`

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// lookupOrFetchClient checks in-memory first, then DB if PersistClients is enabled.
func (s *OAuthServer) lookupOrFetchClient(ctx context.Context, clientID string) (*oauthClient, bool) {
	s.mu.RLock()
	c, ok := s.clients[clientID]
	s.mu.RUnlock()
	if ok {
		return c, true
	}

	if !s.cfg.PersistClients || s.auth == nil {
		return nil, false
	}

	dbClient, err := s.auth.OAuthGetClient(ctx, clientID)
	if err != nil {
		return nil, false
	}

	c = &oauthClient{
		ClientID:      dbClient.ClientID,
		RedirectURIs:  dbClient.RedirectURIs,
		ClientName:    dbClient.ClientName,
		GrantTypes:    dbClient.GrantTypes,
		AllowedScopes: dbClient.AllowedScopes,
	}
	s.mu.Lock()
	s.clients[clientID] = c
	s.mu.Unlock()
	return c, true
}

func (s *OAuthServer) providerByName(name string) *externalProvider {
	for i := range s.providers {
		if s.providers[i].providerName == name {
			return &s.providers[i]
		}
	}
	// If name is empty and only one provider exists, return it
	if name == "" && len(s.providers) == 1 {
		return &s.providers[0]
	}
	return nil
}

func validatePKCESHA256(challenge, verifier string) bool {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:]) == challenge
}

func randomOAuthToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func oauthSliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func (s *OAuthServer) writeOAuthToken(w http.ResponseWriter, accessToken, refreshToken string, scopes []string) {
	expiresIn := int64(s.cfg.AccessTokenTTL.Seconds())
	resp := map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	}
	if refreshToken != "" {
		resp["refresh_token"] = refreshToken
	}
	if len(scopes) > 0 {
		resp["scope"] = strings.Join(scopes, " ")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func writeOAuthError(w http.ResponseWriter, errCode, description string, status int) {
	resp := map[string]string{"error": errCode}
	if description != "" {
		resp["error_description"] = description
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
