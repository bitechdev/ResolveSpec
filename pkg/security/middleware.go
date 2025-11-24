package security

import (
	"context"
	"net/http"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// Context keys for user information
	UserIDKey      contextKey = "user_id"
	UserNameKey    contextKey = "user_name"
	UserLevelKey   contextKey = "user_level"
	SessionIDKey   contextKey = "session_id"
	RemoteIDKey    contextKey = "remote_id"
	UserRolesKey   contextKey = "user_roles"
	UserEmailKey   contextKey = "user_email"
	UserContextKey contextKey = "user_context"
)

// NewAuthMiddleware creates an authentication middleware with the given security list
// This middleware extracts user authentication from the request and adds it to context
func NewAuthMiddleware(securityList *SecurityList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get the security provider
			provider := securityList.Provider()
			if provider == nil {
				http.Error(w, "Security provider not configured", http.StatusInternalServerError)
				return
			}

			// Call the provider's Authenticate method
			userCtx, err := provider.Authenticate(r)
			if err != nil {
				http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Add user information to context
			ctx := r.Context()
			ctx = context.WithValue(ctx, UserContextKey, userCtx)
			ctx = context.WithValue(ctx, UserIDKey, userCtx.UserID)
			ctx = context.WithValue(ctx, UserNameKey, userCtx.UserName)
			ctx = context.WithValue(ctx, UserLevelKey, userCtx.UserLevel)
			ctx = context.WithValue(ctx, SessionIDKey, userCtx.SessionID)
			ctx = context.WithValue(ctx, RemoteIDKey, userCtx.RemoteID)

			if len(userCtx.Roles) > 0 {
				ctx = context.WithValue(ctx, UserRolesKey, userCtx.Roles)
			}
			if userCtx.Email != "" {
				ctx = context.WithValue(ctx, UserEmailKey, userCtx.Email)
			}

			// Continue with authenticated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SetSecurityMiddleware adds security context to requests
// This middleware should be applied after AuthMiddleware
func SetSecurityMiddleware(securityList *SecurityList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), SECURITY_CONTEXT_KEY, securityList)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserContext extracts the full user context from request context
func GetUserContext(ctx context.Context) (*UserContext, bool) {
	userCtx, ok := ctx.Value(UserContextKey).(*UserContext)
	return userCtx, ok
}

// GetUserID extracts the user ID from context
func GetUserID(ctx context.Context) (int, bool) {
	userID, ok := ctx.Value(UserIDKey).(int)
	return userID, ok
}

// GetUserName extracts the user name from context
func GetUserName(ctx context.Context) (string, bool) {
	userName, ok := ctx.Value(UserNameKey).(string)
	return userName, ok
}

// GetUserLevel extracts the user level from context
func GetUserLevel(ctx context.Context) (int, bool) {
	userLevel, ok := ctx.Value(UserLevelKey).(int)
	return userLevel, ok
}

// GetSessionID extracts the session ID from context
func GetSessionID(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(SessionIDKey).(string)
	return sessionID, ok
}

// GetRemoteID extracts the remote ID from context
func GetRemoteID(ctx context.Context) (string, bool) {
	remoteID, ok := ctx.Value(RemoteIDKey).(string)
	return remoteID, ok
}

// GetUserRoles extracts user roles from context
func GetUserRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(UserRolesKey).([]string)
	return roles, ok
}

// GetUserEmail extracts user email from context
func GetUserEmail(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(UserEmailKey).(string)
	return email, ok
}
