package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"

	"golang.org/x/oauth2"
)

// OAuth2Config contains configuration for OAuth2 authentication
type OAuth2Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	ProviderName string

	// Optional: Custom user info parser
	// If not provided, will use standard claims (sub, email, name)
	UserInfoParser func(userInfo map[string]any) (*UserContext, error)

	// --- OpenID Connect (see oidc_client.go) ---

	// Issuer turns the provider into an OpenID Connect provider: PKCE and a nonce are used and
	// the id_token returned by the token endpoint is validated (signature, iss, aud, exp, nonce,
	// at_hash). WithOIDC fills the endpoints in by discovery; with WithOAuth2 set JWKSURL
	// as well. UserInfoURL stays optional: the id_token claims are used when it is empty.
	Issuer string
	// JWKSURL is the provider's key set. Only needed with WithOAuth2; WithOIDC discovers it.
	JWKSURL string
	// EndSessionURL is the provider's RP-initiated logout endpoint (discovered by WithOIDC).
	EndSessionURL string
	// UsePKCE sends a PKCE S256 challenge for a provider that is not OIDC. It is always on in OIDC mode.
	UsePKCE bool
	// AllowedAlgs lists the id_token signature algorithms to accept. Default: RS256, PS256, ES256, ES384.
	AllowedAlgs []string
	// AuthStyle selects how the client authenticates at the token endpoint: "basic", "post" or ""
	// (try basic, fall back to post).
	AuthStyle string
	// HTTPClient is used for discovery, JWKS, token and userinfo requests.
	HTTPClient *http.Client
	// ClockSkew tolerates clock differences when validating the id_token. Default 1 minute.
	ClockSkew time.Duration
}

// OAuth2AuthOptions are optional OpenID Connect authentication request parameters.
type OAuth2AuthOptions struct {
	LoginHint string
	Prompt    string // none, login, consent, select_account
	MaxAge    *int
	ACRValues string
	Extra     map[string]string
}

// oauth2State is what the login redirect remembers until the callback.
type oauth2State struct {
	expiry   time.Time
	verifier string // PKCE code_verifier
	nonce    string
}

// OAuth2Provider holds configuration and state for a single OAuth2 provider
type OAuth2Provider struct {
	config         *oauth2.Config
	userInfoURL    string
	userInfoParser func(userInfo map[string]any) (*UserContext, error)
	providerName   string
	states         map[string]*oauth2State
	oidc           *oidcProvider // nil for plain OAuth2
	usePKCE        bool
	httpClient     *http.Client
	statesMutex    sync.RWMutex
	stopCh         chan struct{} // closed to stop cleanupStates
	stopOnce       sync.Once
}

// WithOAuth2 configures OAuth2 support for the DatabaseAuthenticator
// Can be called multiple times to add multiple OAuth2 providers
// Returns the same DatabaseAuthenticator instance for method chaining
func (a *DatabaseAuthenticator) WithOAuth2(cfg OAuth2Config) *DatabaseAuthenticator {
	if cfg.ProviderName == "" {
		cfg.ProviderName = "oauth2"
	}

	if cfg.UserInfoParser == nil {
		cfg.UserInfoParser = defaultOAuth2UserInfoParser
	}

	authStyle := oauth2.AuthStyleAutoDetect
	switch cfg.AuthStyle {
	case "basic":
		authStyle = oauth2.AuthStyleInHeader
	case "post":
		authStyle = oauth2.AuthStyleInParams
	}
	provider := &OAuth2Provider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:   cfg.AuthURL,
				TokenURL:  cfg.TokenURL,
				AuthStyle: authStyle,
			},
		},
		userInfoURL:    cfg.UserInfoURL,
		userInfoParser: cfg.UserInfoParser,
		providerName:   cfg.ProviderName,
		states:         make(map[string]*oauth2State),
		stopCh:         make(chan struct{}),
		usePKCE:        cfg.UsePKCE,
		httpClient:     cfg.HTTPClient,
	}
	if cfg.Issuer != "" {
		provider.oidc = newOIDCProvider(&cfg)
		provider.usePKCE = true
	}

	// Initialize providers map if needed
	a.oauth2ProvidersMutex.Lock()
	if a.oauth2Providers == nil {
		a.oauth2Providers = make(map[string]*OAuth2Provider)
	}

	// Register provider
	if old := a.oauth2Providers[cfg.ProviderName]; old != nil {
		old.stop() // replaced provider: stop its cleanup goroutine
	}
	a.oauth2Providers[cfg.ProviderName] = provider
	a.oauth2ProvidersMutex.Unlock()

	// Start state cleanup goroutine for this provider
	go provider.cleanupStates()

	return a
}

// OAuth2GetAuthURL returns the OAuth2 authorization URL for redirecting users
func (a *DatabaseAuthenticator) OAuth2GetAuthURL(providerName, state string) (string, error) {
	return a.OAuth2GetAuthURLWithOptions(providerName, state, OAuth2AuthOptions{})
}

// OAuth2GetAuthURLWithOptions is OAuth2GetAuthURL with OpenID Connect request parameters. For an
// OIDC provider (and with UsePKCE) it also creates the PKCE verifier and the nonce, which are
// kept with the state until the callback.
func (a *DatabaseAuthenticator) OAuth2GetAuthURLWithOptions(providerName, state string, opts OAuth2AuthOptions) (string, error) {
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return "", err
	}
	if provider.oidc != nil {
		dctx, cancel := context.WithTimeout(provider.withHTTPClient(context.Background()), 15*time.Second)
		defer cancel()
		if err := provider.oidc.ensureEndpoints(dctx, provider); err != nil {
			return "", err
		}
	}
	st := &oauth2State{expiry: time.Now().Add(10 * time.Minute)}
	var params []oauth2.AuthCodeOption
	if provider.usePKCE {
		st.verifier = oauth2.GenerateVerifier()
		params = append(params, oauth2.S256ChallengeOption(st.verifier))
	}
	if provider.oidc != nil {
		if st.nonce, err = randomOAuthToken(); err != nil {
			return "", err
		}
		params = append(params, oauth2.SetAuthURLParam("nonce", st.nonce))
	}
	set := func(k, v string) {
		if v != "" {
			params = append(params, oauth2.SetAuthURLParam(k, v))
		}
	}
	set("login_hint", opts.LoginHint)
	set("prompt", opts.Prompt)
	set("acr_values", opts.ACRValues)
	if opts.MaxAge != nil {
		set("max_age", strconv.Itoa(*opts.MaxAge))
	}
	for k, v := range opts.Extra {
		set(k, v)
	}

	provider.statesMutex.Lock()
	provider.states[state] = st
	provider.statesMutex.Unlock()

	return provider.config.AuthCodeURL(state, params...), nil
}

// OAuth2GenerateState generates a random state string for CSRF protection
func (a *DatabaseAuthenticator) OAuth2GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// OAuth2HandleCallback handles the OAuth2 callback and exchanges code for token
func (a *DatabaseAuthenticator) OAuth2HandleCallback(ctx context.Context, providerName, code, state string) (*LoginResponse, error) {
	return a.oauth2Callback(ctx, providerName, code, state, "")
}

// OAuth2HandleCallbackRequest is OAuth2HandleCallback for the redirect request itself. Besides
// code and state it honours the error parameters and the RFC 9207 "iss" parameter, which
// protects against mix-up attacks when several providers are in use.
func (a *DatabaseAuthenticator) OAuth2HandleCallbackRequest(ctx context.Context, providerName string, r *http.Request) (*LoginResponse, error) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		return nil, fmt.Errorf("provider returned an error: %s %s", e, q.Get("error_description"))
	}
	return a.oauth2Callback(ctx, providerName, q.Get("code"), q.Get("state"), q.Get("iss"))
}

func (a *DatabaseAuthenticator) oauth2Callback(ctx context.Context, providerName, code, state, iss string) (*LoginResponse, error) {
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return nil, err
	}

	// Validate state
	st, ok := provider.validateState(state)
	if !ok {
		return nil, fmt.Errorf("invalid state parameter")
	}
	if code == "" {
		return nil, fmt.Errorf("missing authorization code")
	}
	if provider.oidc != nil && iss != "" && iss != provider.oidc.issuer {
		return nil, fmt.Errorf("authorization response issuer mismatch")
	}
	if ctx = provider.withHTTPClient(ctx); ctx == nil {
		return nil, fmt.Errorf("no context")
	}

	// Exchange code for token
	var exchange []oauth2.AuthCodeOption
	if st.verifier != "" {
		exchange = append(exchange, oauth2.VerifierOption(st.verifier))
	}
	if provider.oidc != nil {
		if err := provider.oidc.ensureEndpoints(ctx, provider); err != nil {
			return nil, err
		}
	}
	token, err := provider.config.Exchange(ctx, code, exchange...)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// OpenID Connect: validate the id_token.
	var rawIDToken string
	var idClaims map[string]any
	if provider.oidc != nil {
		rawIDToken, _ = token.Extra("id_token").(string)
		if rawIDToken == "" && oauthSliceContains(provider.config.Scopes, "openid") {
			return nil, fmt.Errorf("token response contains no id_token")
		}
		if rawIDToken != "" {
			if idClaims, err = provider.oidc.validateIDToken(ctx, rawIDToken, st.nonce, token.AccessToken); err != nil {
				return nil, fmt.Errorf("invalid id_token: %w", err)
			}
		}
	}

	// Fetch user info
	userInfo := map[string]any{}
	if provider.userInfoURL != "" {
		fetched, err := provider.fetchUserInfo(ctx, token)
		switch {
		case err == nil:
			if sub, _ := idClaims["sub"].(string); sub != "" {
				if us, _ := fetched["sub"].(string); us != "" && us != sub {
					return nil, fmt.Errorf("userinfo subject does not match the id_token")
				}
			}
			userInfo = fetched
		case provider.oidc == nil || idClaims == nil:
			return nil, err
		}
	}
	claims := map[string]any{}
	for k, v := range idClaims {
		claims[k] = v
	}
	for k, v := range userInfo {
		claims[k] = v
	}

	// Parse user info
	userCtx, err := provider.userInfoParser(claims)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}

	// Get or create user in database
	userID, err := a.oauth2GetOrCreateUser(ctx, userCtx, providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create user: %w", err)
	}
	userCtx.UserID = userID

	// Create session token
	sessionToken, err := a.OAuth2GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if token.Expiry.After(time.Now()) {
		expiresAt = token.Expiry
	}

	// Store session in database
	err = a.oauth2CreateSession(ctx, sessionToken, userCtx.UserID, token, expiresAt, providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	userCtx.SessionID = sessionToken

	resp := &LoginResponse{
		Token:        sessionToken,
		RefreshToken: token.RefreshToken,
		User:         userCtx,
		ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
	}
	if rawIDToken != "" {
		// Keep the id_token: it is the id_token_hint of OAuth2LogoutURL.
		resp.Meta = map[string]any{"id_token": rawIDToken}
	}
	return resp, nil
}

// fetchUserInfo calls the provider's userinfo endpoint with the access token.
func (p *OAuth2Provider) fetchUserInfo(ctx context.Context, token *oauth2.Token) (map[string]any, error) {
	resp, err := p.config.Client(ctx, token).Get(p.userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read user info: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed: status %d", resp.StatusCode)
	}
	var userInfo map[string]any
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}
	return userInfo, nil
}

// withHTTPClient makes oauth2 use the provider's HTTP client.
func (p *OAuth2Provider) withHTTPClient(ctx context.Context) context.Context {
	if p.httpClient == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
}

// OAuth2GetProviders returns list of configured OAuth2 provider names
func (a *DatabaseAuthenticator) OAuth2GetProviders() []string {
	a.oauth2ProvidersMutex.RLock()
	defer a.oauth2ProvidersMutex.RUnlock()

	if a.oauth2Providers == nil {
		return nil
	}

	providers := make([]string, 0, len(a.oauth2Providers))
	for name := range a.oauth2Providers {
		providers = append(providers, name)
	}
	return providers
}

// getOAuth2Provider retrieves a registered OAuth2 provider by name
func (a *DatabaseAuthenticator) getOAuth2Provider(providerName string) (*OAuth2Provider, error) {
	a.oauth2ProvidersMutex.RLock()
	defer a.oauth2ProvidersMutex.RUnlock()

	if a.oauth2Providers == nil {
		return nil, fmt.Errorf("OAuth2 not configured - call WithOAuth2() first")
	}

	provider, ok := a.oauth2Providers[providerName]
	if !ok {
		// Build provider list without calling OAuth2GetProviders to avoid recursion
		providerNames := make([]string, 0, len(a.oauth2Providers))
		for name := range a.oauth2Providers {
			providerNames = append(providerNames, name)
		}
		return nil, fmt.Errorf("OAuth2 provider '%s' not found - available providers: %v", providerName, providerNames)
	}

	return provider, nil
}

// oauth2GetOrCreateUser finds or creates a user based on OAuth2 info using stored procedure
func (a *DatabaseAuthenticator) oauth2GetOrCreateUser(ctx context.Context, userCtx *UserContext, providerName string) (int, error) {
	return a.src.get().OAuthUser.GetOrCreateUser(ctx, userCtx, providerName)
}

// oauth2CreateSession creates a new OAuth2 session using stored procedure
func (a *DatabaseAuthenticator) oauth2CreateSession(ctx context.Context, sessionToken string, userID int, token *oauth2.Token, expiresAt time.Time, providerName string) error {
	return a.src.get().OAuthUser.CreateSession(ctx, lookup.OAuthSession{
		SessionToken: sessionToken,
		UserID:       userID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresAt:    expiresAt,
		Provider:     providerName,
	})
}

// validateState validates state using in-memory storage and returns what was remembered with it.
func (p *OAuth2Provider) validateState(state string) (*oauth2State, bool) {
	p.statesMutex.Lock()
	defer p.statesMutex.Unlock()

	st, ok := p.states[state]
	if !ok {
		return nil, false
	}
	delete(p.states, state) // One-time use
	if time.Now().After(st.expiry) {
		return nil, false
	}
	return st, true
}

// cleanupStates removes expired states periodically
func (p *OAuth2Provider) cleanupStates() {
	defer logger.CatchPanic("OAuth2Provider.cleanupStates")()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
		}
		p.statesMutex.Lock()
		now := time.Now()
		for state, st := range p.states {
			if now.After(st.expiry) {
				delete(p.states, state)
			}
		}
		p.statesMutex.Unlock()
	}
}

// stop terminates the cleanup goroutine; safe to call more than once.
func (p *OAuth2Provider) stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// Close stops the background OAuth2 state cleanup goroutines and waits for
// in-flight session activity updates. It is safe to call more than once.
func (a *DatabaseAuthenticator) Close() error {
	a.oauth2ProvidersMutex.RLock()
	for _, p := range a.oauth2Providers {
		p.stop()
	}
	a.oauth2ProvidersMutex.RUnlock()
	a.activityWG.Wait()
	return nil
}

// defaultOAuth2UserInfoParser parses standard OAuth2 user info claims
func defaultOAuth2UserInfoParser(userInfo map[string]any) (*UserContext, error) {
	ctx := &UserContext{
		Claims: userInfo,
		Roles:  []string{"user"},
	}

	// Extract standard claims
	if sub, ok := userInfo["sub"].(string); ok {
		ctx.RemoteID = sub
	}
	if email, ok := userInfo["email"].(string); ok {
		ctx.Email = email
		// Use email as username if name not available
		ctx.UserName = strings.Split(email, "@")[0]
	}
	if name, ok := userInfo["name"].(string); ok {
		ctx.UserName = name
	}
	if login, ok := userInfo["login"].(string); ok {
		ctx.UserName = login // GitHub uses "login"
	}

	if ctx.UserName == "" {
		return nil, fmt.Errorf("could not extract username from user info")
	}

	return ctx, nil
}

// OAuth2RefreshToken refreshes an expired OAuth2 access token using the refresh token
// Takes the refresh token and returns a new LoginResponse with updated tokens
func (a *DatabaseAuthenticator) OAuth2RefreshToken(ctx context.Context, refreshToken, providerName string) (*LoginResponse, error) {
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return nil, err
	}

	// Get session by refresh token from database
	session, err := a.src.get().OAuthUser.GetByRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	// Create oauth2.Token from stored data
	oldToken := &oauth2.Token{
		AccessToken:  session.AccessToken,
		TokenType:    session.TokenType,
		RefreshToken: refreshToken,
		Expiry:       session.Expiry,
	}

	// Use OAuth2 provider to refresh the token
	tokenSource := provider.config.TokenSource(provider.withHTTPClient(ctx), oldToken)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token with provider: %w", err)
	}

	// Generate new session token
	newSessionToken, err := a.OAuth2GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate new session token: %w", err)
	}

	// Update session in database with new tokens
	if err := a.src.get().OAuthUser.UpdateRefreshToken(ctx, session.UserID, refreshToken, newSessionToken, newToken.AccessToken, newToken.RefreshToken, newToken.Expiry); err != nil {
		return nil, err
	}

	// Get user data
	userCtx, err := a.src.get().OAuthUser.GetUser(ctx, session.UserID)
	if err != nil {
		return nil, err
	}

	userCtx.SessionID = newSessionToken

	resp := &LoginResponse{
		Token:        newSessionToken,
		RefreshToken: newToken.RefreshToken,
		User:         userCtx,
		ExpiresIn:    int64(time.Until(newToken.Expiry).Seconds()),
	}
	if provider.oidc != nil {
		if raw, _ := newToken.Extra("id_token").(string); raw != "" {
			if _, err := provider.oidc.validateIDToken(provider.withHTTPClient(ctx), raw, "", newToken.AccessToken); err != nil {
				return nil, fmt.Errorf("invalid id_token in refresh response: %w", err)
			}
			resp.Meta = map[string]any{"id_token": raw}
		}
	}
	return resp, nil
}

// Pre-configured OAuth2 factory methods

// NewGoogleAuthenticator creates a DatabaseAuthenticator configured for Google OAuth2
func NewGoogleAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{ //nolint:gosec // G101: false positive: identifier/example, not a credential
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "profile", "email"},
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		ProviderName: "google",
		// OpenID Connect: PKCE, nonce and id_token validation against Google's published keys.
		Issuer:        "https://accounts.google.com",
		JWKSURL:       "https://www.googleapis.com/oauth2/v3/certs",
		EndSessionURL: "",
	})
}

// NewGitHubAuthenticator creates a DatabaseAuthenticator configured for GitHub OAuth2
func NewGitHubAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{ //nolint:gosec // G101: false positive: identifier/example, not a credential
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"user:email"},
		AuthURL:      "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserInfoURL:  "https://api.github.com/user",
		ProviderName: "github",
	})
}

// NewMicrosoftAuthenticator creates a DatabaseAuthenticator configured for Microsoft OAuth2
func NewMicrosoftAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{ //nolint:gosec // G101: false positive: identifier/example, not a credential
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "profile", "email"},
		AuthURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL:  "https://graph.microsoft.com/v1.0/me",
		ProviderName: "microsoft",
	})
}

// NewFacebookAuthenticator creates a DatabaseAuthenticator configured for Facebook OAuth2
func NewFacebookAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{ //nolint:gosec // G101: false positive: identifier/example, not a credential
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"email"},
		AuthURL:      "https://www.facebook.com/v12.0/dialog/oauth",
		TokenURL:     "https://graph.facebook.com/v12.0/oauth/access_token",
		UserInfoURL:  "https://graph.facebook.com/me?fields=id,name,email",
		ProviderName: "facebook",
	})
}

// NewMultiProviderAuthenticator creates a DatabaseAuthenticator with all major OAuth2 providers configured
func NewMultiProviderAuthenticator(db *sql.DB, configs map[string]OAuth2Config) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)

	//nolint:gocritic // OAuth2Config is copied but kept for API simplicity
	for _, cfg := range configs {
		auth.WithOAuth2(cfg)
	}

	return auth
}
