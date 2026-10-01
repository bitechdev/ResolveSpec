package security

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// OAuthServerConfig configures the OAuth2 / OpenID Connect authorization server.
//
// Every field except Issuer is optional. The zero value of each new option keeps the
// behaviour of earlier versions; see OAUTH2_SERVER.md for the full guide.
type OAuthServerConfig struct {
	// Issuer is the public base URL of this server (e.g. "https://api.example.com"). It is
	// the "iss" of every token and the base of every endpoint URL. A path is allowed
	// ("https://example.com/auth"); the server then also answers the RFC 8414 path-insertion
	// well-known URLs.
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

	// DefaultScopes lists scopes advertised in server metadata and granted to clients that
	// register without allowed_scopes. Defaults to ["openid","profile","email"].
	DefaultScopes []string

	// AccessTokenTTL is the issued token lifetime. Defaults to 24h.
	AccessTokenTTL time.Duration

	// AuthCodeTTL is the auth code lifetime. Defaults to 2 minutes.
	AuthCodeTTL time.Duration

	// ResourceIdentifier is this server's protected-resource identifier, advertised in
	// RFC 9728 metadata. Defaults to Issuer.
	ResourceIdentifier string

	// SigningKey signs id_tokens (RS256) and is exposed via the JWKS endpoint. If nil and
	// SigningKeys is empty, an RSA-2048 key is generated in memory when the server starts.
	// Supply a persistent key for multi-instance deployments so tokens remain verifiable
	// across restarts and instances.
	SigningKey *rsa.PrivateKey

	// SigningKeys supersedes SigningKey. The first key is the default; all are published in
	// the JWKS, which is how a key is rotated (add the new key first, publish, then remove the
	// old one once its tokens have expired). RSA and ECDSA (P-256/P-384) keys are supported.
	SigningKeys []OAuthSigningKey

	// CookieSecret keys the HMAC that protects the SSO cookie and the state carried through
	// the login and consent forms. Defaults to a value derived from the first signing key, so
	// instances sharing a signing key share sessions.
	CookieSecret []byte

	// SSOCookie configures the browser session cookie that makes prompt=none, max_age,
	// single sign-on and logout work.
	SSOCookie OAuthSSOCookieConfig

	// --- Consent and scopes ---

	// RequireConsent shows a consent screen for every client that is not first-party and has
	// no stored consent covering the requested scopes. A client can also opt in with its
	// require_consent metadata.
	RequireConsent bool

	// ConsentTTL is how long a stored consent is honoured. Defaults to 90 days.
	ConsentTTL time.Duration

	// --- Tokens ---

	// ManagedRefreshTokens makes the server issue and rotate its own refresh tokens (stored
	// hashed, one family per grant, reuse of a rotated token revokes the family). When false,
	// the refresh token of the underlying DatabaseAuthenticator is passed through as before.
	ManagedRefreshTokens bool

	// RefreshTokenTTL is the absolute lifetime of a refresh token family. Defaults to 30 days.
	RefreshTokenTTL time.Duration

	// JWTAccessTokens issues RFC 9068 JWT access tokens instead of opaque session tokens.
	// Resource servers can verify them locally (VerifyAccessToken).
	JWTAccessTokens bool

	// AccessTokenAudience is the "aud" of JWT access tokens that were not requested for a
	// specific resource. Defaults to ResourceIdentifier.
	AccessTokenAudience string

	// EnableDPoP accepts RFC 9449 DPoP proofs at the token and userinfo endpoints and binds
	// the issued tokens to the proof key.
	EnableDPoP bool

	// EnablePAR serves the RFC 9126 pushed authorization request endpoint; RequirePAR makes it
	// mandatory for every client.
	EnablePAR  bool
	RequirePAR bool
	PARTTL     time.Duration // default 90 seconds

	// EnableDeviceFlow serves the RFC 8628 device authorization grant.
	EnableDeviceFlow  bool
	DeviceCodeTTL     time.Duration // default 10 minutes
	DevicePollSeconds int           // minimum poll interval, default 5

	// EnableTokenExchange serves the RFC 8693 token exchange grant.
	EnableTokenExchange bool

	// --- OpenID Connect ---

	// ClaimsProvider supplies the user claims for id_tokens and UserInfo (profile, email,
	// address, phone, custom claims). The default returns sub, preferred_username and email.
	ClaimsProvider OAuthClaimsProvider

	// SupportedACR lists the authentication context class references advertised and accepted.
	SupportedACR []string

	// DisableLogout does not serve the RP-initiated logout endpoint.
	DisableLogout bool

	// --- Hardening ---

	// InitialAccessToken, when set, must be presented as a Bearer token to register a client.
	InitialAccessToken string

	// AllowAnonymousIntrospection lets callers without client credentials use the revocation
	// and introspection endpoints. By default they must authenticate as a client.
	AllowAnonymousIntrospection bool

	// RateLimiter, when set, is called for every request to a token-issuing endpoint
	// ("authorize", "token", "par", "device", "register", "introspect", "revoke", "userinfo",
	// "logout"). Returning false answers 429.
	RateLimiter func(r *http.Request, endpoint string) bool

	// AllowPrivateNetworkFetch lets the server fetch client jwks_uri documents from loopback and
	// private addresses. Leave false in production (SSRF protection).
	AllowPrivateNetworkFetch bool

	// ScopeDescriptions are shown next to each scope on the consent screen. Built-in
	// descriptions exist for openid, profile, email and offline_access.
	ScopeDescriptions map[string]string

	// LoginTemplate and ConsentTemplate replace the built-in pages. They receive
	// OAuthLoginPage and OAuthConsentPage.
	LoginTemplate   *template.Template
	ConsentTemplate *template.Template
}

// OAuthSSOCookieConfig configures the SSO cookie.
type OAuthSSOCookieConfig struct {
	Name     string        // default "resolvespec_sso"
	Path     string        // default "/"
	TTL      time.Duration // default 8h
	SameSite http.SameSite // default Lax
	// Insecure sends the cookie over plain HTTP. By default Secure is on whenever Issuer is https.
	Insecure bool
	Disable  bool // never set a cookie: every authorization request authenticates again
}

// OAuthClaimsRequest is the input of an OAuthClaimsProvider.
type OAuthClaimsRequest struct {
	UserID int
	Sub    string
	Scopes []string
	// Requested holds the names the client asked for individually through the OIDC "claims"
	// request parameter for this destination.
	Requested []string
	// Destination is "id_token" or "userinfo".
	Destination string
	// Base are the claims the server already knows (sub, preferred_username, email).
	Base map[string]any
}

// OAuthClaimsProvider returns the claims for a user. The server only includes the standard claims
// the granted scopes entitle the client to (profile, email, address, phone) plus every claim the
// client requested explicitly; claims outside those sets are dropped.
type OAuthClaimsProvider func(ctx context.Context, req OAuthClaimsRequest) (map[string]any, error)

// pendingAuth tracks an authorization request that is waiting for an external provider.
type pendingAuth struct {
	Req       *authzRequest
	Provider  string
	ExpiresAt time.Time
}

type cachedClient struct {
	c  *OAuthServerClient
	at time.Time // zero for clients that only exist in memory
}

// externalProvider pairs a DatabaseAuthenticator with its provider name.
type externalProvider struct {
	auth         *DatabaseAuthenticator
	providerName string
}

// OAuthServer is an OAuth 2.1 authorization server and OpenID Connect provider.
//
// It can act as both:
//   - A direct identity provider using DatabaseAuthenticator username/password login
//   - A federation layer that delegates authentication to external OAuth2 providers
//     (Google, GitHub, Microsoft, etc.) registered via RegisterExternalProvider
//
// Endpoints (see OAUTH2_SERVER.md for parameters and examples):
//
//	GET  /.well-known/oauth-authorization-server   RFC 8414 server metadata
//	GET  /.well-known/openid-configuration         OIDC Discovery
//	GET  /.well-known/oauth-protected-resource     RFC 9728 protected resource metadata
//	POST /oauth/register                            RFC 7591 dynamic client registration
//	GET|PUT|DELETE /oauth/register/{client_id}      RFC 7592 client management
//	GET|POST /oauth/authorize                       authorization endpoint (PKCE S256 required)
//	POST /oauth/token                               authorization_code, refresh_token, client_credentials,
//	                                                 device_code and token-exchange grants
//	POST /oauth/par                                 RFC 9126 pushed authorization requests
//	POST /oauth/device_authorization                RFC 8628 device authorization
//	GET|POST /oauth/device                          device verification page
//	POST /oauth/revoke                              RFC 7009 token revocation
//	POST /oauth/introspect                          RFC 7662 token introspection
//	GET|POST /oauth/userinfo                        OIDC UserInfo
//	GET  /oauth/jwks.json                           JWKS
//	GET|POST /oauth/logout                          OIDC RP-initiated logout
//	GET  {ProviderCallbackPath}                     external provider callback
type OAuthServer struct {
	cfg       OAuthServerConfig
	auth      *DatabaseAuthenticator // nil = only external providers
	providers []externalProvider

	mu      sync.RWMutex
	clients map[string]*cachedClient
	pending map[string]*pendingAuth // provider_state → request waiting for the provider
	codes   map[string]*OAuthCode   // auth code → code (when PersistCodes is false)

	keys      *oauthKeyring
	secret    []byte
	jwks      *jwksCache
	tmpl      oauthTemplates
	issuerURL *url.URL

	bcWG sync.WaitGroup // in-flight back-channel logout notifications

	done chan struct{} // closed by Close() to stop background goroutines
}

// NewOAuthServer creates a new OAuth2 / OIDC authorization server.
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
		if cfg.ManagedRefreshTokens {
			cfg.DefaultScopes = append(cfg.DefaultScopes, "offline_access")
		}
	}
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = 24 * time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 2 * time.Minute
	}
	if cfg.ConsentTTL == 0 {
		cfg.ConsentTTL = 90 * 24 * time.Hour
	}
	if cfg.RefreshTokenTTL == 0 {
		cfg.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if cfg.PARTTL == 0 {
		cfg.PARTTL = 90 * time.Second
	}
	if cfg.DeviceCodeTTL == 0 {
		cfg.DeviceCodeTTL = 10 * time.Minute
	}
	if cfg.DevicePollSeconds == 0 {
		cfg.DevicePollSeconds = 5
	}
	if cfg.SSOCookie.Name == "" {
		cfg.SSOCookie.Name = "resolvespec_sso"
	}
	if cfg.SSOCookie.Path == "" {
		cfg.SSOCookie.Path = "/"
	}
	if cfg.SSOCookie.TTL == 0 {
		cfg.SSOCookie.TTL = 8 * time.Hour
	}
	if cfg.SSOCookie.SameSite == 0 {
		cfg.SSOCookie.SameSite = http.SameSiteLaxMode
	}
	// Normalize issuer: remove trailing slash to ensure consistent endpoint URL construction.
	cfg.Issuer = strings.TrimSuffix(cfg.Issuer, "/")
	if cfg.ResourceIdentifier == "" {
		cfg.ResourceIdentifier = cfg.Issuer
	}
	if cfg.AccessTokenAudience == "" {
		cfg.AccessTokenAudience = cfg.ResourceIdentifier
	}

	keys, err := newOAuthKeyring(&cfg)
	if err != nil {
		// Only a bad explicit key reaches here (generating one cannot realistically fail).
		// Fall back to a fresh key so the server still starts; id_tokens then do not survive a restart.
		cfg.SigningKeys, cfg.SigningKey = nil, nil
		keys, _ = newOAuthKeyring(&cfg)
	}
	issuerURL, _ := url.Parse(cfg.Issuer)
	if issuerURL == nil {
		issuerURL = &url.URL{}
	}

	s := &OAuthServer{
		cfg:       cfg,
		auth:      auth,
		clients:   make(map[string]*cachedClient),
		pending:   make(map[string]*pendingAuth),
		codes:     make(map[string]*OAuthCode),
		keys:      keys,
		jwks:      newJWKSCache(publicHTTPClient(cfg.AllowPrivateNetworkFetch)),
		issuerURL: issuerURL,
		done:      make(chan struct{}),
	}
	s.secret = cfg.CookieSecret
	if len(s.secret) == 0 {
		s.secret = deriveOAuthSecret(keys)
	}
	s.tmpl = newOAuthTemplates(&cfg)
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
	s.bcWG.Wait()
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
	handle := func(pattern, endpoint string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, s.limited(endpoint, h))
	}
	for _, name := range []string{"oauth-authorization-server", "openid-configuration", "oauth-protected-resource"} {
		h := s.metadataHandler(name)
		mux.HandleFunc("/.well-known/"+name, h)
		mux.HandleFunc("/.well-known/"+name+"/{path...}", h)
		if p := strings.TrimSuffix(s.issuerURL.Path, "/"); p != "" {
			mux.HandleFunc(p+"/.well-known/"+name, h)
		}
	}
	handle("/oauth/register", "register", s.registerHandler)
	handle("/oauth/register/{id}", "register", s.registrationManageHandler)
	handle("/oauth/register/{id}/rotate-secret", "register", s.registrationRotateHandler)
	handle("/oauth/authorize", "authorize", s.authorizeHandler)
	handle("/oauth/token", "token", s.tokenHandler)
	handle("/oauth/revoke", "revoke", s.revokeHandler)
	handle("/oauth/introspect", "introspect", s.introspectHandler)
	handle("/oauth/userinfo", "userinfo", s.userinfoHandler)
	mux.HandleFunc("/oauth/jwks.json", s.jwksHandler)
	if s.cfg.EnablePAR || s.cfg.RequirePAR {
		handle("/oauth/par", "par", s.parHandler)
	}
	if s.cfg.EnableDeviceFlow {
		handle("/oauth/device_authorization", "device", s.deviceAuthorizationHandler)
		handle("/oauth/device", "device", s.deviceVerificationHandler)
	}
	if !s.cfg.DisableLogout {
		handle("/oauth/logout", "logout", s.logoutHandler)
	}
	mux.HandleFunc(s.cfg.ProviderCallbackPath, s.providerCallbackHandler)
	return mux
}

// limited applies OAuthServerConfig.RateLimiter.
func (s *OAuthServer) limited(endpoint string, h http.HandlerFunc) http.HandlerFunc {
	if s.cfg.RateLimiter == nil {
		return h
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.RateLimiter(r, endpoint) {
			w.Header().Set("Retry-After", "5")
			writeOAuthError(w, "temporarily_unavailable", "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		h(w, r)
	}
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
			for k, c := range s.codes {
				if now.After(c.ExpiresAt) {
					delete(s.codes, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// --------------------------------------------------------------------------
// Collaborators
// --------------------------------------------------------------------------

// anyAuth returns the authenticator used for sessions: the primary one, or the first provider's.
func (s *OAuthServer) anyAuth() *DatabaseAuthenticator {
	if s.auth != nil {
		return s.auth
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.providers) > 0 {
		return s.providers[0].auth
	}
	return nil
}

// grants returns the grant store, or nil when no authenticator is configured.
func (s *OAuthServer) grants() lookup.OAuthGrantStore {
	if a := s.anyAuth(); a != nil {
		return a.OAuthGrants()
	}
	return nil
}

func (s *OAuthServer) providerByName(name string) *externalProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

func (s *OAuthServer) hasProviders() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.providers) > 0
}

// endpoint returns the absolute URL of a path below the issuer.
func (s *OAuthServer) endpoint(path string) string { return s.cfg.Issuer + path }

// --------------------------------------------------------------------------
// Clients
// --------------------------------------------------------------------------

// needsClientAuth reports whether the client must authenticate at the token endpoint.
func needsClientAuth(c *OAuthServerClient) bool {
	return c.ClientSecretHash != "" || c.TokenEndpointAuthMethod == "private_key_jwt"
}

// clientCacheTTL bounds how long a persisted client is served from memory, so changes made by
// other instances (or directly in the database) become visible.
const clientCacheTTL = 30 * time.Second

// lookupOrFetchClient checks in-memory first, then DB if PersistClients is enabled.
func (s *OAuthServer) lookupOrFetchClient(ctx context.Context, clientID string) (*OAuthServerClient, bool) {
	if clientID == "" {
		return nil, false
	}
	s.mu.RLock()
	c, ok := s.clients[clientID]
	s.mu.RUnlock()
	if ok && (c.at.IsZero() || time.Since(c.at) < clientCacheTTL) {
		return c.c, true
	}

	if !s.cfg.PersistClients || s.auth == nil {
		if ok {
			return c.c, true
		}
		return nil, false
	}

	dbClient, err := s.auth.OAuthGetClient(ctx, clientID)
	if err != nil {
		s.mu.Lock()
		delete(s.clients, clientID)
		s.mu.Unlock()
		return nil, false
	}
	s.mu.Lock()
	s.clients[clientID] = &cachedClient{c: dbClient, at: time.Now()}
	s.mu.Unlock()
	return dbClient, true
}

// saveClient stores a new client in memory and, with PersistClients, in the database.
func (s *OAuthServer) saveClient(ctx context.Context, c *OAuthServerClient, update bool) error {
	cc := &cachedClient{c: c}
	if s.cfg.PersistClients && s.auth != nil {
		var err error
		if update {
			err = s.auth.OAuthUpdateClient(ctx, c)
		} else {
			_, err = s.auth.OAuthRegisterClient(ctx, c)
		}
		if err != nil {
			return err
		}
		cc.at = time.Now()
	}
	s.mu.Lock()
	s.clients[c.ClientID] = cc
	s.mu.Unlock()
	return nil
}

func (s *OAuthServer) removeClient(ctx context.Context, clientID string) error {
	if s.cfg.PersistClients && s.auth != nil {
		if err := s.auth.OAuthDeleteClient(ctx, clientID); err != nil {
			return err
		}
	}
	s.mu.Lock()
	delete(s.clients, clientID)
	s.mu.Unlock()
	return nil
}

// RegisterTrustedClient registers a client programmatically and returns its plaintext secret
// (empty for a public client). Unlike dynamic registration it may set FirstParty (skips the
// consent screen), RequireConsent and RequirePAR, which a remote caller must not control.
// ClientID is generated when empty. Leave ClientSecretHash empty and set
// TokenEndpointAuthMethod to a client_secret_* method to have a secret generated.
func (s *OAuthServer) RegisterTrustedClient(ctx context.Context, c OAuthServerClient) (registered *OAuthServerClient, plainSecret string, err error) {
	if c.ClientID == "" {
		id, err := randomOAuthToken()
		if err != nil {
			return nil, "", err
		}
		c.ClientID = id
	}
	if len(c.GrantTypes) == 0 {
		c.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(c.AllowedScopes) == 0 {
		c.AllowedScopes = s.cfg.DefaultScopes
	}
	if c.TokenEndpointAuthMethod == "" {
		c.TokenEndpointAuthMethod = "none"
	}
	var secret string
	if c.ClientSecretHash == "" && strings.HasPrefix(c.TokenEndpointAuthMethod, "client_secret_") {
		var err error
		if secret, err = randomOAuthToken(); err != nil {
			return nil, "", err
		}
		c.ClientSecretHash = hashClientSecret(secret)
	}
	if c.ClientIDIssuedAt == 0 {
		c.ClientIDIssuedAt = time.Now().Unix()
	}
	if err := s.saveClient(ctx, &c, false); err != nil {
		return nil, "", err
	}
	return &c, secret, nil
}

// --------------------------------------------------------------------------
// Secrets and encoding helpers
// --------------------------------------------------------------------------

func validatePKCESHA256(challenge, verifier string) bool {
	h := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(h[:])), []byte(challenge)) == 1
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

func hashClientSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// hashToken is the stored form of refresh tokens and access-grant keys.
func hashToken(token string) string { return hashClientSecret(token) }

type oauthError struct {
	Code   string
	Desc   string
	Status int
	// WWWAuth is set as the WWW-Authenticate header.
	WWWAuth string
}

func (e *oauthError) write(w http.ResponseWriter) {
	if e.WWWAuth != "" {
		w.Header().Set("WWW-Authenticate", e.WWWAuth)
	}
	writeOAuthError(w, e.Code, e.Desc, e.Status)
}

func oerr(code, desc string, status int) *oauthError {
	return &oauthError{Code: code, Desc: desc, Status: status}
}

func serverErr() *oauthError {
	return oerr("server_error", "internal error", http.StatusInternalServerError)
}

func writeOAuthError(w http.ResponseWriter, errCode, description string, status int) {
	resp := map[string]string{"error": errCode}
	if description != "" {
		resp["error_description"] = description
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp) //nolint:errcheck,gosec // G104: best-effort write, error intentionally ignored
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck,gosec // G104: best-effort write, error intentionally ignored
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
