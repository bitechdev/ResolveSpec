package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Production-Ready Authenticators
// =================================

// HeaderAuthenticator provides simple header-based authentication
// Expects: X-User-ID, X-User-Name, X-User-Level, X-Session-ID, X-Remote-ID, X-User-Roles, X-User-Email
type HeaderAuthenticator struct{}

func NewHeaderAuthenticator() *HeaderAuthenticator {
	return &HeaderAuthenticator{}
}

func (a *HeaderAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return nil, fmt.Errorf("header authentication does not support login")
}

func (a *HeaderAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	return nil
}

func (a *HeaderAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return nil, fmt.Errorf("X-User-ID header required")
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	return &UserContext{
		UserID:    userID,
		UserName:  r.Header.Get("X-User-Name"),
		UserLevel: parseIntHeader(r, "X-User-Level", 0),
		SessionID: r.Header.Get("X-Session-ID"),
		RemoteID:  r.Header.Get("X-Remote-ID"),
		Email:     r.Header.Get("X-User-Email"),
		Roles:     parseRoles(r.Header.Get("X-User-Roles")),
	}, nil
}

// DatabaseAuthenticator provides session-based authentication with database storage
// All database operations go through stored procedures for security and consistency
// Requires stored procedures: resolvespec_login, resolvespec_logout, resolvespec_session,
// resolvespec_session_update, resolvespec_refresh_token
// See database_schema.sql for procedure definitions
// Also supports multiple OAuth2 providers configured with WithOAuth2()
type DatabaseAuthenticator struct {
	db       *sql.DB
	cache    *cache.Cache
	cacheTTL time.Duration

	// OAuth2 providers registry (multiple providers supported)
	oauth2Providers      map[string]*OAuth2Provider
	oauth2ProvidersMutex sync.RWMutex
}

// DatabaseAuthenticatorOptions configures the database authenticator
type DatabaseAuthenticatorOptions struct {
	// CacheTTL is the duration to cache user contexts
	// Default: 5 minutes
	CacheTTL time.Duration
	// Cache is an optional cache instance. If nil, uses the default cache
	Cache *cache.Cache
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

	return &DatabaseAuthenticator{
		db:       db,
		cache:    cacheInstance,
		cacheTTL: opts.CacheTTL,
	}
}

func (a *DatabaseAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Convert LoginRequest to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal login request: %w", err)
	}

	// Call resolvespec_login stored procedure
	var success bool
	var errorMsg sql.NullString
	var dataJSON sql.NullString

	query := `SELECT p_success, p_error, p_data::text FROM resolvespec_login($1::jsonb)`
	err = a.db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	if err != nil {
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("login failed")
	}

	// Parse response
	var response LoginResponse
	if err := json.Unmarshal([]byte(dataJSON.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse login response: %w", err)
	}

	return &response, nil
}

// Register implements Registrable interface
func (a *DatabaseAuthenticator) Register(ctx context.Context, req RegisterRequest) (*LoginResponse, error) {
	// Convert RegisterRequest to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal register request: %w", err)
	}

	// Call resolvespec_register stored procedure
	var success bool
	var errorMsg sql.NullString
	var dataJSON sql.NullString

	query := `SELECT p_success, p_error, p_data::text FROM resolvespec_register($1::jsonb)`
	err = a.db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	if err != nil {
		return nil, fmt.Errorf("register query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("registration failed")
	}

	// Parse response
	var response LoginResponse
	if err := json.Unmarshal([]byte(dataJSON.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse register response: %w", err)
	}

	return &response, nil
}

func (a *DatabaseAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	// Convert LogoutRequest to JSON
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal logout request: %w", err)
	}

	// Call resolvespec_logout stored procedure
	var success bool
	var errorMsg sql.NullString
	var dataJSON sql.NullString

	query := `SELECT p_success, p_error, p_data::text FROM resolvespec_logout($1::jsonb)`
	err = a.db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("logout failed")
	}

	// Clear cache for this token
	if req.Token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", req.Token)
		_ = a.cache.Delete(ctx, cacheKey)
	}

	return nil
}

func (a *DatabaseAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	// Extract session token from header or cookie
	sessionToken := r.Header.Get("Authorization")
	reference := "authenticate"
	var tokens []string

	if sessionToken == "" {
		// Try cookie
		cookie, err := r.Cookie("session_token")
		if err == nil {
			tokens = []string{cookie.Value}
			reference = "cookie"
		}
	} else {
		// Parse Authorization header which may contain multiple comma-separated tokens
		// Format: "Token abc, Token def" or "Bearer abc" or just "abc"
		rawTokens := strings.Split(sessionToken, ",")
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
		var userCtx UserContext
		err := a.cache.GetOrSet(r.Context(), cacheKey, &userCtx, a.cacheTTL, func() (any, error) {
			// This function is called only if cache miss
			var success bool
			var errorMsg sql.NullString
			var userJSON sql.NullString

			query := `SELECT p_success, p_error, p_user::text FROM resolvespec_session($1, $2)`
			err := a.db.QueryRowContext(r.Context(), query, token, reference).Scan(&success, &errorMsg, &userJSON)
			if err != nil {
				return nil, fmt.Errorf("session query failed: %w", err)
			}

			if !success {
				if errorMsg.Valid {
					return nil, fmt.Errorf("%s", errorMsg.String)
				}
				return nil, fmt.Errorf("invalid or expired session")
			}

			if !userJSON.Valid {
				return nil, fmt.Errorf("no user data in session")
			}

			// Parse UserContext
			var user UserContext
			if err := json.Unmarshal([]byte(userJSON.String), &user); err != nil {
				return nil, fmt.Errorf("failed to parse user context: %w", err)
			}

			return &user, nil
		})

		if err != nil {
			lastErr = err
			continue // Try next token
		}

		// Authentication succeeded with this token
		// Update last activity timestamp asynchronously
		go a.updateSessionActivity(r.Context(), token, &userCtx)

		return &userCtx, nil
	}

	// All tokens failed
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
	// Convert UserContext to JSON
	userJSON, err := json.Marshal(userCtx)
	if err != nil {
		return
	}

	// Call resolvespec_session_update stored procedure
	var success bool
	var errorMsg sql.NullString
	var updatedUserJSON sql.NullString

	query := `SELECT p_success, p_error, p_user::text FROM resolvespec_session_update($1, $2::jsonb)`
	_ = a.db.QueryRowContext(ctx, query, sessionToken, string(userJSON)).Scan(&success, &errorMsg, &updatedUserJSON)
}

// RefreshToken implements Refreshable interface
func (a *DatabaseAuthenticator) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// Call api_refresh_token stored procedure
	// First, we need to get the current user context for the refresh token
	var success bool
	var errorMsg sql.NullString
	var userJSON sql.NullString
	// Get current session to pass to refresh
	query := `SELECT p_success, p_error, p_user::text FROM resolvespec_session($1, $2)`
	err := a.db.QueryRowContext(ctx, query, refreshToken, "refresh").Scan(&success, &errorMsg, &userJSON)
	if err != nil {
		return nil, fmt.Errorf("refresh token query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("invalid refresh token")
	}

	// Call resolvespec_refresh_token to generate new token
	var newSuccess bool
	var newErrorMsg sql.NullString
	var newUserJSON sql.NullString

	refreshQuery := `SELECT p_success, p_error, p_user::text FROM resolvespec_refresh_token($1, $2::jsonb)`
	err = a.db.QueryRowContext(ctx, refreshQuery, refreshToken, userJSON).Scan(&newSuccess, &newErrorMsg, &newUserJSON)
	if err != nil {
		return nil, fmt.Errorf("refresh token generation failed: %w", err)
	}

	if !newSuccess {
		if newErrorMsg.Valid {
			return nil, fmt.Errorf("%s", newErrorMsg.String)
		}
		return nil, fmt.Errorf("failed to refresh token")
	}

	// Parse refreshed user context
	var userCtx UserContext
	if err := json.Unmarshal([]byte(newUserJSON.String), &userCtx); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}

	return &LoginResponse{
		Token:     userCtx.SessionID, // New session token from stored procedure
		User:      &userCtx,
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

// JWTAuthenticator provides JWT token-based authentication
// All database operations go through stored procedures
// Requires stored procedures: resolvespec_jwt_login, resolvespec_jwt_logout
// NOTE: JWT signing/verification requires github.com/golang-jwt/jwt/v5 to be installed and imported
type JWTAuthenticator struct {
	secretKey []byte
	db        *sql.DB
}

func NewJWTAuthenticator(secretKey string, db *sql.DB) *JWTAuthenticator {
	return &JWTAuthenticator{
		secretKey: []byte(secretKey),
		db:        db,
	}
}

func (a *JWTAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Call resolvespec_jwt_login stored procedure
	var success bool
	var errorMsg sql.NullString
	var userJSON []byte

	query := `SELECT p_success, p_error, p_user FROM resolvespec_jwt_login($1, $2)`
	err := a.db.QueryRowContext(ctx, query, req.Username, req.Password).Scan(&success, &errorMsg, &userJSON)
	if err != nil {
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Parse user data
	var user struct {
		ID        int    `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		UserLevel int    `json:"user_level"`
		Roles     string `json:"roles"`
	}

	if err := json.Unmarshal(userJSON, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user data: %w", err)
	}

	// TODO: Verify password
	// if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
	//     return nil, fmt.Errorf("invalid credentials")
	// }

	// Generate token (placeholder - implement JWT signing when library is available)
	expiresAt := time.Now().Add(24 * time.Hour)
	tokenString := fmt.Sprintf("token_%d_%d", user.ID, expiresAt.Unix())

	return &LoginResponse{
		Token: tokenString,
		User: &UserContext{
			UserID:    user.ID,
			UserName:  user.Username,
			Email:     user.Email,
			UserLevel: user.UserLevel,
			Roles:     parseRoles(user.Roles),
		},
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

func (a *JWTAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	// Call resolvespec_jwt_logout stored procedure
	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_jwt_logout($1, $2)`
	err := a.db.QueryRowContext(ctx, query, req.Token, req.UserID).Scan(&success, &errorMsg)
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("logout failed")
	}

	return nil
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

// DatabaseColumnSecurityProvider loads column security from database
// All database operations go through stored procedures
// Requires stored procedure: resolvespec_column_security
type DatabaseColumnSecurityProvider struct {
	db *sql.DB
}

func NewDatabaseColumnSecurityProvider(db *sql.DB) *DatabaseColumnSecurityProvider {
	return &DatabaseColumnSecurityProvider{db: db}
}

func (p *DatabaseColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	var rules []ColumnSecurity

	// Call resolvespec_column_security stored procedure
	var success bool
	var errorMsg sql.NullString
	var rulesJSON []byte

	query := `SELECT p_success, p_error, p_rules FROM resolvespec_column_security($1, $2, $3)`
	err := p.db.QueryRowContext(ctx, query, userID, schema, table).Scan(&success, &errorMsg, &rulesJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to load column security: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("failed to load column security")
	}

	// Parse the JSON array of security records
	type SecurityRecord struct {
		Control    string `json:"control"`
		Accesstype string `json:"accesstype"`
		JSONValue  string `json:"jsonvalue"`
	}

	var records []SecurityRecord
	if err := json.Unmarshal(rulesJSON, &records); err != nil {
		return nil, fmt.Errorf("failed to parse security rules: %w", err)
	}

	// Convert records to ColumnSecurity rules
	for _, rec := range records {
		parts := strings.Split(rec.Control, ".")
		if len(parts) < 3 {
			continue
		}

		rule := ColumnSecurity{
			Schema:     schema,
			Tablename:  table,
			Path:       parts[2:],
			Accesstype: rec.Accesstype,
			UserID:     userID,
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// DatabaseRowSecurityProvider loads row security from database
// All database operations go through stored procedures
// Requires stored procedure: resolvespec_row_security
type DatabaseRowSecurityProvider struct {
	db *sql.DB
}

func NewDatabaseRowSecurityProvider(db *sql.DB) *DatabaseRowSecurityProvider {
	return &DatabaseRowSecurityProvider{db: db}
}

func (p *DatabaseRowSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error) {
	var template string
	var hasBlock bool

	// Call resolvespec_row_security stored procedure
	query := `SELECT p_template, p_block FROM resolvespec_row_security($1, $2, $3)`

	err := p.db.QueryRowContext(ctx, query, schema, table, userID).Scan(&template, &hasBlock)
	if err != nil {
		return RowSecurity{}, fmt.Errorf("failed to load row security: %w", err)
	}

	return RowSecurity{
		Schema:    schema,
		Tablename: table,
		UserID:    userID,
		Template:  template,
		HasBlock:  hasBlock,
	}, nil
}

// ConfigColumnSecurityProvider provides static column security configuration
type ConfigColumnSecurityProvider struct {
	rules map[string][]ColumnSecurity
}

func NewConfigColumnSecurityProvider(rules map[string][]ColumnSecurity) *ConfigColumnSecurityProvider {
	return &ConfigColumnSecurityProvider{rules: rules}
}

func (p *ConfigColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	key := fmt.Sprintf("%s.%s", schema, table)
	rules, ok := p.rules[key]
	if !ok {
		return []ColumnSecurity{}, nil
	}
	return rules, nil
}

// ConfigRowSecurityProvider provides static row security configuration
type ConfigRowSecurityProvider struct {
	templates map[string]string
	blocked   map[string]bool
}

func NewConfigRowSecurityProvider(templates map[string]string, blocked map[string]bool) *ConfigRowSecurityProvider {
	return &ConfigRowSecurityProvider{
		templates: templates,
		blocked:   blocked,
	}
}

func (p *ConfigRowSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error) {
	key := fmt.Sprintf("%s.%s", schema, table)

	if p.blocked[key] {
		return RowSecurity{
			Schema:    schema,
			Tablename: table,
			UserID:    userID,
			HasBlock:  true,
		}, nil
	}

	template := p.templates[key]
	return RowSecurity{
		Schema:    schema,
		Tablename: table,
		UserID:    userID,
		Template:  template,
		HasBlock:  false,
	}, nil
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
