package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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
}

// OAuth2Provider holds configuration and state for a single OAuth2 provider
type OAuth2Provider struct {
	config         *oauth2.Config
	userInfoURL    string
	userInfoParser func(userInfo map[string]any) (*UserContext, error)
	providerName   string
	states         map[string]time.Time // state -> expiry time
	statesMutex    sync.RWMutex
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

	provider := &OAuth2Provider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  cfg.AuthURL,
				TokenURL: cfg.TokenURL,
			},
		},
		userInfoURL:    cfg.UserInfoURL,
		userInfoParser: cfg.UserInfoParser,
		providerName:   cfg.ProviderName,
		states:         make(map[string]time.Time),
	}

	// Initialize providers map if needed
	a.oauth2ProvidersMutex.Lock()
	if a.oauth2Providers == nil {
		a.oauth2Providers = make(map[string]*OAuth2Provider)
	}

	// Register provider
	a.oauth2Providers[cfg.ProviderName] = provider
	a.oauth2ProvidersMutex.Unlock()

	// Start state cleanup goroutine for this provider
	go provider.cleanupStates()

	return a
}

// OAuth2GetAuthURL returns the OAuth2 authorization URL for redirecting users
func (a *DatabaseAuthenticator) OAuth2GetAuthURL(providerName, state string) (string, error) {
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return "", err
	}

	// Store state for validation
	provider.statesMutex.Lock()
	provider.states[state] = time.Now().Add(10 * time.Minute)
	provider.statesMutex.Unlock()

	return provider.config.AuthCodeURL(state), nil
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
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return nil, err
	}

	// Validate state
	if !provider.validateState(state) {
		return nil, fmt.Errorf("invalid state parameter")
	}

	// Exchange code for token
	token, err := provider.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Fetch user info
	client := provider.config.Client(ctx, token)
	resp, err := client.Get(provider.userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read user info: %w", err)
	}

	var userInfo map[string]any
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	// Parse user info
	userCtx, err := provider.userInfoParser(userInfo)
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

	return &LoginResponse{
		Token:        sessionToken,
		RefreshToken: token.RefreshToken,
		User:         userCtx,
		ExpiresIn:    int64(time.Until(expiresAt).Seconds()),
	}, nil
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
	userData := map[string]interface{}{
		"username":      userCtx.UserName,
		"email":         userCtx.Email,
		"remote_id":     userCtx.RemoteID,
		"user_level":    userCtx.UserLevel,
		"roles":         userCtx.Roles,
		"auth_provider": providerName,
	}

	userJSON, err := json.Marshal(userData)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal user data: %w", err)
	}

	var success bool
	var errMsg *string
	var userID *int

	err = a.db.QueryRowContext(ctx, `
		SELECT p_success, p_error, p_user_id 
		FROM resolvespec_oauth_getorcreateuser($1::jsonb)
	`, userJSON).Scan(&success, &errMsg, &userID)

	if err != nil {
		return 0, fmt.Errorf("failed to get or create user: %w", err)
	}

	if !success {
		if errMsg != nil {
			return 0, fmt.Errorf("%s", *errMsg)
		}
		return 0, fmt.Errorf("failed to get or create user")
	}

	if userID == nil {
		return 0, fmt.Errorf("user ID not returned")
	}

	return *userID, nil
}

// oauth2CreateSession creates a new OAuth2 session using stored procedure
func (a *DatabaseAuthenticator) oauth2CreateSession(ctx context.Context, sessionToken string, userID int, token *oauth2.Token, expiresAt time.Time, providerName string) error {
	sessionData := map[string]interface{}{
		"session_token": sessionToken,
		"user_id":       userID,
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_at":    expiresAt,
		"auth_provider": providerName,
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	var success bool
	var errMsg *string

	err = a.db.QueryRowContext(ctx, `
		SELECT p_success, p_error 
		FROM resolvespec_oauth_createsession($1::jsonb)
	`, sessionJSON).Scan(&success, &errMsg)

	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	if !success {
		if errMsg != nil {
			return fmt.Errorf("%s", *errMsg)
		}
		return fmt.Errorf("failed to create session")
	}

	return nil
}

// validateState validates state using in-memory storage
func (p *OAuth2Provider) validateState(state string) bool {
	p.statesMutex.Lock()
	defer p.statesMutex.Unlock()

	expiry, ok := p.states[state]
	if !ok {
		return false
	}

	if time.Now().After(expiry) {
		delete(p.states, state)
		return false
	}

	delete(p.states, state) // One-time use
	return true
}

// cleanupStates removes expired states periodically
func (p *OAuth2Provider) cleanupStates() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.statesMutex.Lock()
		now := time.Now()
		for state, expiry := range p.states {
			if now.After(expiry) {
				delete(p.states, state)
			}
		}
		p.statesMutex.Unlock()
	}
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
	var success bool
	var errMsg *string
	var sessionData []byte

	err = a.db.QueryRowContext(ctx, `
		SELECT p_success, p_error, p_data::text
		FROM resolvespec_oauth_getrefreshtoken($1)
	`, refreshToken).Scan(&success, &errMsg, &sessionData)

	if err != nil {
		return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
	}

	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("invalid or expired refresh token")
	}

	// Parse session data
	var session struct {
		UserID      int       `json:"user_id"`
		AccessToken string    `json:"access_token"`
		TokenType   string    `json:"token_type"`
		Expiry      time.Time `json:"expiry"`
	}
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session data: %w", err)
	}

	// Create oauth2.Token from stored data
	oldToken := &oauth2.Token{
		AccessToken:  session.AccessToken,
		TokenType:    session.TokenType,
		RefreshToken: refreshToken,
		Expiry:       session.Expiry,
	}

	// Use OAuth2 provider to refresh the token
	tokenSource := provider.config.TokenSource(ctx, oldToken)
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
	updateData := map[string]interface{}{
		"user_id":           session.UserID,
		"old_refresh_token": refreshToken,
		"new_session_token": newSessionToken,
		"new_access_token":  newToken.AccessToken,
		"new_refresh_token": newToken.RefreshToken,
		"expires_at":        newToken.Expiry,
	}

	updateJSON, err := json.Marshal(updateData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update data: %w", err)
	}

	var updateSuccess bool
	var updateErrMsg *string

	err = a.db.QueryRowContext(ctx, `
		SELECT p_success, p_error
		FROM resolvespec_oauth_updaterefreshtoken($1::jsonb)
	`, updateJSON).Scan(&updateSuccess, &updateErrMsg)

	if err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	if !updateSuccess {
		if updateErrMsg != nil {
			return nil, fmt.Errorf("%s", *updateErrMsg)
		}
		return nil, fmt.Errorf("failed to update session")
	}

	// Get user data
	var userSuccess bool
	var userErrMsg *string
	var userData []byte

	err = a.db.QueryRowContext(ctx, `
		SELECT p_success, p_error, p_data::text
		FROM resolvespec_oauth_getuser($1)
	`, session.UserID).Scan(&userSuccess, &userErrMsg, &userData)

	if err != nil {
		return nil, fmt.Errorf("failed to get user data: %w", err)
	}

	if !userSuccess {
		if userErrMsg != nil {
			return nil, fmt.Errorf("%s", *userErrMsg)
		}
		return nil, fmt.Errorf("failed to get user data")
	}

	// Parse user context
	var userCtx UserContext
	if err := json.Unmarshal(userData, &userCtx); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}

	userCtx.SessionID = newSessionToken

	return &LoginResponse{
		Token:        newSessionToken,
		RefreshToken: newToken.RefreshToken,
		User:         &userCtx,
		ExpiresIn:    int64(time.Until(newToken.Expiry).Seconds()),
	}, nil
}

// Pre-configured OAuth2 factory methods

// NewGoogleAuthenticator creates a DatabaseAuthenticator configured for Google OAuth2
func NewGoogleAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "profile", "email"},
		AuthURL:      "https://accounts.google.com/o/oauth2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
		ProviderName: "google",
	})
}

// NewGitHubAuthenticator creates a DatabaseAuthenticator configured for GitHub OAuth2
func NewGitHubAuthenticator(clientID, clientSecret, redirectURL string, db *sql.DB) *DatabaseAuthenticator {
	auth := NewDatabaseAuthenticator(db)
	return auth.WithOAuth2(OAuth2Config{
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
	return auth.WithOAuth2(OAuth2Config{
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
	return auth.WithOAuth2(OAuth2Config{
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
