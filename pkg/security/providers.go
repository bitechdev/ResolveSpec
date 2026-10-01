package security

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/dbtrace"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/backends"
)

// Production-Ready Authenticators
// =================================

// maxAuthTokens caps the comma-separated credentials tried per request so one
// request cannot drive unbounded session lookups.
const maxAuthTokens = 4

// sessionActivityTimeout bounds the detached last-activity update.
const sessionActivityTimeout = 5 * time.Second

// sessionActivityInterval is the minimum gap between last-activity writes for
// one session token. Requests inside it skip the write.
const sessionActivityInterval = time.Minute

// activityThrottle remembers when each token's activity was last written.
type activityThrottle struct {
	mu        sync.Mutex
	last      map[string]time.Time
	lastPrune time.Time
}

// allow reports whether token is due an activity write, and if so records it.
func (t *activityThrottle) allow(token string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = make(map[string]time.Time)
	}
	if prev, ok := t.last[token]; ok && now.Sub(prev) < sessionActivityInterval {
		return false
	}
	t.last[token] = now
	if now.Sub(t.lastPrune) > sessionActivityInterval {
		t.lastPrune = now
		for k, v := range t.last {
			if now.Sub(v) >= sessionActivityInterval {
				delete(t.last, k)
			}
		}
	}
	return true
}

// DatabaseAuthenticator provides session-based authentication with database storage
// All database operations go through stored procedures for security and consistency
// Procedure names and modes are configured through lookup.Config (see lookup.DefaultProcNames)
// See lookup/database_schema.sql for procedure definitions
// Also supports multiple OAuth2 providers configured with WithOAuth2()
// Also supports passkey authentication configured with WithPasskey()
type DatabaseAuthenticator struct {
	src      *lookupSource
	cache    *cache.Cache
	cacheTTL time.Duration

	// activityWG tracks in-flight asynchronous session activity updates
	activityWG sync.WaitGroup
	// activityLimit throttles those updates to one per token per interval
	activityLimit activityThrottle
	// sessionLoads collapses concurrent session lookups for the same token
	sessionLoads singleflight.Group

	// Cookie session support (optional, gated by enableCookieSession)
	enableCookieSession bool
	cookieOptions       SessionCookieOptions

	// OAuth2 providers registry (multiple providers supported)
	oauth2Providers      map[string]*OAuth2Provider
	oauth2ProvidersMutex sync.RWMutex

	// Passkey provider (optional)
	passkeyProvider PasskeyProvider

	// Optional fallback called when primary authentication fails
	authenticateCallback func(r *http.Request) (*UserContext, error)
}

// DatabaseAuthenticatorOptions configures the database authenticator
type DatabaseAuthenticatorOptions struct {
	// CacheTTL is the duration to cache user contexts
	// Default: 5 minutes
	CacheTTL time.Duration
	// Cache is an optional cache instance. If nil, uses the default cache
	Cache *cache.Cache
	// PasskeyProvider is an optional passkey provider for WebAuthn/FIDO2 authentication
	PasskeyProvider PasskeyProvider
	// Lookup selects dialect, query mode and procedure/table/column names.
	// The zero value uses stored procedures on Postgres and direct SQL elsewhere.
	Lookup lookup.Config
	// LookupProvider, when set, is used instead of building one from Lookup and the db.
	LookupProvider *lookup.Provider
	// DBFactory is called to obtain a fresh *sql.DB when the existing connection is closed.
	// If nil, reconnection is disabled.
	DBFactory func() (*sql.DB, error)
	// EnableCookieSession enables cookie-based session management.
	// When true, Authenticate reads the session token from the cookie named by
	// CookieOptions.Name (default "session_token") in addition to the Authorization header,
	// and LoginWithCookie / LogoutWithCookie automatically set / clear the cookie.
	EnableCookieSession bool
	// UpgradePasswordHash, when true, rewrites a legacy cleartext password as a
	// bcrypt hash after a successful login. It is off by default and is never
	// enabled automatically: legacy cleartext values are still accepted at login,
	// but stored rows are left untouched unless this is set.
	UpgradePasswordHash bool
	// CookieOptions configures the session cookie written by LoginWithCookie.
	// Only used when EnableCookieSession is true.
	CookieOptions SessionCookieOptions
	// AuthenticateCallback is a fallback called when the primary authentication (database
	// session lookup) fails. If non-nil and the callback returns a non-nil UserContext,
	// that result is used in place of the failure.
	AuthenticateCallback func(r *http.Request) (*UserContext, error)
}

func NewDatabaseAuthenticator(db *sql.DB) *DatabaseAuthenticator {
	return NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{
		CacheTTL: 5 * time.Minute,
	})
}

func NewDatabaseAuthenticatorWithOptions(db *sql.DB, opts DatabaseAuthenticatorOptions) *DatabaseAuthenticator {
	if opts.CacheTTL == 0 {
		opts.CacheTTL = 5 * time.Minute
	}

	cacheInstance := opts.Cache
	if cacheInstance == nil {
		cacheInstance = cache.GetDefaultCache()
	}

	src := newLookupSource(db)
	src.cfg = opts.Lookup
	src.provider = opts.LookupProvider
	src.opts = backends.Options{DBFactory: opts.DBFactory, UpgradePasswordHash: opts.UpgradePasswordHash}

	return &DatabaseAuthenticator{
		src:                  src,
		cache:                cacheInstance,
		cacheTTL:             opts.CacheTTL,
		passkeyProvider:      opts.PasskeyProvider,
		enableCookieSession:  opts.EnableCookieSession,
		cookieOptions:        opts.CookieOptions,
		authenticateCallback: opts.AuthenticateCallback,
	}
}

func (a *DatabaseAuthenticator) auth() lookup.AuthStore { return a.src.get().Auth }

func (a *DatabaseAuthenticator) SetAuthenticateCallback(fn func(r *http.Request) (*UserContext, error)) {
	a.authenticateCallback = fn
}

func (a *DatabaseAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return a.auth().Login(ctx, req)
}

// LoginWithAPIKey implements APIKeyLoginable. It validates a raw header/generic
// API key and creates a session for the key's user. Unknown, expired and
// inactive keys all return errInvalidAPIKey; the raw key is never logged.
// Procedure-only: the key and user lookup live in resolvespec_login_api_key so
// the underlying schema can differ per database; there is no direct-SQL path.
func (a *DatabaseAuthenticator) LoginWithAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*LoginResponse, error) {
	return a.auth().LoginAPIKey(ctx, rawKey, claims)
}

// Register implements Registrable interface
func (a *DatabaseAuthenticator) Register(ctx context.Context, req RegisterRequest) (*LoginResponse, error) {
	return a.auth().Register(ctx, req)
}

func (a *DatabaseAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	if err := a.auth().Logout(ctx, req); err != nil {
		return err
	}

	// Clear cache for this token
	if req.Token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", req.Token)
		_ = a.cache.Delete(ctx, cacheKey)
	}

	return nil
}

// LoginWithCookie performs a login and, when EnableCookieSession is true, writes the
// session cookie to w using the configured CookieOptions. The LoginResponse is returned
// regardless of whether cookie sessions are enabled.
func (a *DatabaseAuthenticator) LoginWithCookie(ctx context.Context, req LoginRequest, w http.ResponseWriter) (*LoginResponse, error) {
	resp, err := a.Login(ctx, req)
	if err != nil {
		return nil, err
	}
	if a.enableCookieSession {
		SetSessionCookie(w, resp, a.cookieOptions)
	}
	return resp, nil
}

// LogoutWithCookie performs a logout and, when EnableCookieSession is true, clears the
// session cookie on w. The logout itself is performed regardless of the cookie flag.
func (a *DatabaseAuthenticator) LogoutWithCookie(ctx context.Context, req LogoutRequest, w http.ResponseWriter) error {
	err := a.Logout(ctx, req)
	if err != nil {
		return err
	}
	if a.enableCookieSession {
		ClearSessionCookie(w, a.cookieOptions)
	}
	return nil
}

func (a *DatabaseAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	// Extract session token from header or cookie
	sessionToken := r.Header.Get("Authorization")
	reference := "authenticate"
	var tokens []string

	if sessionToken == "" {
		if a.enableCookieSession {
			if token := GetSessionCookie(r, a.cookieOptions); token != "" {
				tokens = []string{token}
				reference = "cookie"
			}
		}
	} else {
		// Parse Authorization header which may contain multiple comma-separated tokens
		// Format: "Token abc, Token def" or "Bearer abc" or just "abc"
		rawTokens := strings.SplitN(sessionToken, ",", maxAuthTokens+2)
		if len(rawTokens) > maxAuthTokens {
			return nil, fmt.Errorf("too many authorization tokens")
		}
		for _, token := range rawTokens {
			token = strings.TrimSpace(token)
			// Remove "Bearer " prefix if present
			token = strings.TrimPrefix(token, "Bearer ")
			// Remove "Token " prefix if present
			token = strings.TrimPrefix(token, "Token ")
			token = strings.TrimSpace(token)
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}

	if len(tokens) == 0 {
		if a.authenticateCallback != nil {
			return a.authenticateCallback(r)
		}
		return nil, fmt.Errorf("session token required")
	}

	// Log warning if multiple tokens are provided
	if len(tokens) > 1 {
		logger.Warn("Multiple authentication tokens provided in Authorization header (%d tokens). This is unusual and may indicate a misconfigured client. Header: %s", len(tokens), sessionToken)
	}

	// Try each token until one succeeds
	var lastErr error
	for _, token := range tokens {
		// Build cache key
		cacheKey := fmt.Sprintf("auth:session:%s", token)

		// Use cache.GetOrSet to get from cache or load from database
		// Concurrent misses for the same token share one database lookup.
		v, err, _ := a.sessionLoads.Do(cacheKey, func() (any, error) {
			var loaded UserContext
			err := a.cache.GetOrSet(r.Context(), cacheKey, &loaded, a.cacheTTL, func() (any, error) {
				// This function is called only if cache miss
				dbtrace.Raw(r.Context(), "auth.session")

				return a.auth().Session(r.Context(), token, reference)
			})
			if err != nil {
				return nil, err
			}
			return loaded, nil
		})

		if err != nil {
			lastErr = err
			continue // Try next token
		}
		userCtx, _ := v.(UserContext)

		// Authentication succeeded with this token
		// Update last activity timestamp asynchronously
		if a.activityLimit.allow(token, time.Now()) {
			activityCtx := userCtx
			// Detach from the request (it is cancelled when the handler returns) but
			// keep a deadline, and never let a panic here take the process down.
			detached, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), sessionActivityTimeout)
			a.activityWG.Add(1)
			go func(ctx context.Context, token string) {
				defer a.activityWG.Done()
				defer cancel()
				defer logger.CatchPanic("updateSessionActivity")()
				a.updateSessionActivity(ctx, token, &activityCtx)
			}(detached, token)

		}

		return &userCtx, nil
	}

	// All tokens failed — try callback before returning error
	if a.authenticateCallback != nil {
		return a.authenticateCallback(r)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("authentication failed for all provided tokens")
}

// ClearCache removes a specific token from the cache or clears all cache if token is empty
func (a *DatabaseAuthenticator) ClearCache(token string) error {
	ctx := context.Background()
	if token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", token)
		return a.cache.Delete(ctx, cacheKey)
	}
	// Clear all auth cache entries
	return a.cache.DeleteByPattern(ctx, "auth:session:*")
}

// ClearUserCache removes all cache entries for a specific user ID
func (a *DatabaseAuthenticator) ClearUserCache(userID int) error {
	ctx := context.Background()
	// Clear all sessions for this user
	pattern := "auth:session:*"
	return a.cache.DeleteByPattern(ctx, pattern)
}

// updateSessionActivity updates the last activity timestamp for the session
func (a *DatabaseAuthenticator) updateSessionActivity(ctx context.Context, sessionToken string, userCtx *UserContext) {
	dbtrace.Raw(ctx, "auth.activity")

	_ = a.auth().TouchSession(ctx, sessionToken, userCtx)
}

// RefreshToken implements Refreshable interface
func (a *DatabaseAuthenticator) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	return a.auth().Refresh(ctx, refreshToken)
}

// JWTAuthenticator provides JWT token-based authentication
// All database operations go through stored procedures
// Procedure names and modes are configured through lookup.Config (see lookup.DefaultProcNames)
// NOTE: JWT signing/verification requires github.com/golang-jwt/jwt/v5 to be installed and imported
type JWTAuthenticator struct {
	secretKey []byte
	src       *lookupSource
}

// WithPasswordHashUpgrade explicitly enables (or disables) upgrading legacy
// cleartext passwords to bcrypt after a successful login. Off by default.
func (a *JWTAuthenticator) WithPasswordHashUpgrade(enabled bool) *JWTAuthenticator {
	a.src.opts.UpgradePasswordHash = enabled
	return a
}

func NewJWTAuthenticator(secretKey string, db *sql.DB) *JWTAuthenticator {
	return &JWTAuthenticator{secretKey: []byte(secretKey), src: newLookupSource(db)}
}

// WithDBFactory configures a factory used to reopen the database connection if it is closed.
func (a *JWTAuthenticator) WithDBFactory(factory func() (*sql.DB, error)) *JWTAuthenticator {
	a.src.opts.DBFactory = factory
	return a
}

// WithLookup configures dialect, query mode and names. Call before first use.
func (a *JWTAuthenticator) WithLookup(cfg lookup.Config) *JWTAuthenticator {
	a.src.cfg = cfg
	return a
}

// WithLookupProvider uses an existing provider instead of building one.
func (a *JWTAuthenticator) WithLookupProvider(p *lookup.Provider) *JWTAuthenticator {
	a.src.provider = p
	return a
}

func (a *JWTAuthenticator) auth() lookup.AuthStore { return a.src.get().Auth }

func (a *JWTAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return a.auth().JWTLogin(ctx, req)
}

func (a *JWTAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	return a.auth().JWTLogout(ctx, req)
}

func (a *JWTAuthenticator) LoginWithCookie(ctx context.Context, req LoginRequest, w http.ResponseWriter) (*LoginResponse, error) {
	return a.Login(ctx, req)
}

func (a *JWTAuthenticator) LogoutWithCookie(ctx context.Context, req LogoutRequest, w http.ResponseWriter) error {
	return a.Logout(ctx, req)
}

func (a *JWTAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("authorization header required")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return nil, fmt.Errorf("bearer token required")
	}

	// TODO: Implement JWT parsing when library is available
	return nil, fmt.Errorf("JWT parsing not implemented - install github.com/golang-jwt/jwt/v5")
}

// Production-Ready Security Providers
// ====================================

// DatabaseColumnSecurityProvider loads column security through the lookup package
// (stored procedure on Postgres by default, direct SQL elsewhere).
type DatabaseColumnSecurityProvider struct {
	src *lookupSource
}

func NewDatabaseColumnSecurityProvider(db *sql.DB) *DatabaseColumnSecurityProvider {
	return &DatabaseColumnSecurityProvider{src: newLookupSource(db)}
}

// WithLookup configures dialect, query mode and names. Call before first use.
func (p *DatabaseColumnSecurityProvider) WithLookup(cfg lookup.Config) *DatabaseColumnSecurityProvider {
	p.src.cfg = cfg
	return p
}

// WithLookupProvider uses an existing provider instead of building one.
func (p *DatabaseColumnSecurityProvider) WithLookupProvider(lp *lookup.Provider) *DatabaseColumnSecurityProvider {
	p.src.provider = lp
	return p
}

// WithNoGroupTables skips group membership when loading rules in direct mode.
func (p *DatabaseColumnSecurityProvider) WithNoGroupTables() *DatabaseColumnSecurityProvider {
	p.src.opts.NoGroupTables = true
	return p
}

func (p *DatabaseColumnSecurityProvider) WithDBFactory(factory func() (*sql.DB, error)) *DatabaseColumnSecurityProvider {
	p.src.opts.DBFactory = factory
	return p
}

func (p *DatabaseColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	dbtrace.Raw(ctx, "security.column")
	return p.src.get().Policy.ColumnSecurity(ctx, userID, schema, table)
}

// DatabaseRowSecurityProvider loads row security through the lookup package
// (stored procedure on Postgres by default, direct SQL elsewhere).
type DatabaseRowSecurityProvider struct {
	src *lookupSource
}

func NewDatabaseRowSecurityProvider(db *sql.DB) *DatabaseRowSecurityProvider {
	return &DatabaseRowSecurityProvider{src: newLookupSource(db)}
}

// WithLookup configures dialect, query mode and names. Call before first use.
func (p *DatabaseRowSecurityProvider) WithLookup(cfg lookup.Config) *DatabaseRowSecurityProvider {
	p.src.cfg = cfg
	return p
}

// WithLookupProvider uses an existing provider instead of building one.
func (p *DatabaseRowSecurityProvider) WithLookupProvider(lp *lookup.Provider) *DatabaseRowSecurityProvider {
	p.src.provider = lp
	return p
}

// WithNoGroupTables skips group membership when loading rules in direct mode.
func (p *DatabaseRowSecurityProvider) WithNoGroupTables() *DatabaseRowSecurityProvider {
	p.src.opts.NoGroupTables = true
	return p
}

func (p *DatabaseRowSecurityProvider) WithDBFactory(factory func() (*sql.DB, error)) *DatabaseRowSecurityProvider {
	p.src.opts.DBFactory = factory
	return p
}

func (p *DatabaseRowSecurityProvider) GetRowSecurity(ctx context.Context, userRef any, schema, table string) (RowSecurity, error) {
	dbtrace.Raw(ctx, "security.row")
	return p.src.get().Policy.RowSecurity(ctx, userRef, schema, table)
}

// Helper functions
// ================

func parseRoles(rolesStr string) []string {
	if rolesStr == "" {
		return []string{}
	}
	return strings.Split(rolesStr, ",")
}

func parseIntHeader(r *http.Request, key string, defaultVal int) int {
	val := r.Header.Get(key)
	if val == "" {
		return defaultVal
	}
	intVal, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return intVal
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// func getClaimString(claims map[string]any, key string) string {
// 	if claims == nil {
// 		return ""
// 	}
// 	if val, ok := claims[key]; ok {
// 		if str, ok := val.(string); ok {
// 			return str
// 		}
// 	}
// 	return ""
// }

// Password reset methods
// ======================

// RequestPasswordReset implements PasswordResettable. It calls the stored procedure
// resolvespec_password_reset_request and returns the reset token and expiry.
func (a *DatabaseAuthenticator) RequestPasswordReset(ctx context.Context, req PasswordResetRequest) (*PasswordResetResponse, error) {
	return a.auth().ResetRequest(ctx, req)
}

// CompletePasswordReset implements PasswordResettable. It validates the token and
// updates the user's password via resolvespec_password_reset.
func (a *DatabaseAuthenticator) CompletePasswordReset(ctx context.Context, req PasswordResetCompleteRequest) error {
	return a.auth().ResetComplete(ctx, req)
}

// Passkey authentication methods
// ==============================

// WithPasskey configures the DatabaseAuthenticator with a passkey provider
func (a *DatabaseAuthenticator) WithPasskey(provider PasskeyProvider) *DatabaseAuthenticator {
	a.passkeyProvider = provider
	return a
}

// BeginPasskeyRegistration initiates passkey registration for a user
func (a *DatabaseAuthenticator) BeginPasskeyRegistration(ctx context.Context, req PasskeyBeginRegistrationRequest) (*PasskeyRegistrationOptions, error) {
	if a.passkeyProvider == nil {
		return nil, fmt.Errorf("passkey provider not configured")
	}
	return a.passkeyProvider.BeginRegistration(ctx, req.UserID, req.Username, req.DisplayName)
}

// CompletePasskeyRegistration completes passkey registration
func (a *DatabaseAuthenticator) CompletePasskeyRegistration(ctx context.Context, req PasskeyRegisterRequest) (*PasskeyCredential, error) {
	if a.passkeyProvider == nil {
		return nil, fmt.Errorf("passkey provider not configured")
	}

	cred, err := a.passkeyProvider.CompleteRegistration(ctx, req.UserID, req.Response, req.ExpectedChallenge)
	if err != nil {
		return nil, err
	}

	// Update credential name if provided
	if req.CredentialName != "" && cred.ID != "" {
		_ = a.passkeyProvider.UpdateCredentialName(ctx, req.UserID, cred.ID, req.CredentialName)
	}

	return cred, nil
}

// BeginPasskeyAuthentication initiates passkey authentication
func (a *DatabaseAuthenticator) BeginPasskeyAuthentication(ctx context.Context, req PasskeyBeginAuthenticationRequest) (*PasskeyAuthenticationOptions, error) {
	if a.passkeyProvider == nil {
		return nil, fmt.Errorf("passkey provider not configured")
	}
	return a.passkeyProvider.BeginAuthentication(ctx, req.Username)
}

// LoginWithPasskey authenticates a user using a passkey and creates a session
func (a *DatabaseAuthenticator) LoginWithPasskey(ctx context.Context, req PasskeyLoginRequest) (*LoginResponse, error) {
	if a.passkeyProvider == nil {
		return nil, fmt.Errorf("passkey provider not configured")
	}

	// Verify passkey assertion
	userID, err := a.passkeyProvider.CompleteAuthentication(ctx, req.Response, req.ExpectedChallenge)
	if err != nil {
		return nil, fmt.Errorf("passkey authentication failed: %w", err)
	}

	return a.src.get().Passkey.Login(ctx, userID, req.Claims)
}

// GetPasskeyCredentials returns all passkey credentials for a user
func (a *DatabaseAuthenticator) GetPasskeyCredentials(ctx context.Context, userID int) ([]PasskeyCredential, error) {
	if a.passkeyProvider == nil {
		return nil, fmt.Errorf("passkey provider not configured")
	}
	return a.passkeyProvider.GetCredentials(ctx, userID)
}

// DeletePasskeyCredential removes a passkey credential
func (a *DatabaseAuthenticator) DeletePasskeyCredential(ctx context.Context, userID int, credentialID string) error {
	if a.passkeyProvider == nil {
		return fmt.Errorf("passkey provider not configured")
	}
	return a.passkeyProvider.DeleteCredential(ctx, userID, credentialID)
}

// UpdatePasskeyCredentialName updates the friendly name of a credential
func (a *DatabaseAuthenticator) UpdatePasskeyCredentialName(ctx context.Context, userID int, credentialID string, name string) error {
	if a.passkeyProvider == nil {
		return fmt.Errorf("passkey provider not configured")
	}
	return a.passkeyProvider.UpdateCredentialName(ctx, userID, credentialID, name)
}
