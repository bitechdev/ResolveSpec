package security

import (
	"context"
	"net/http"
)

// Authenticator handles user authentication operations
type Authenticator interface {
	// Login authenticates credentials and returns a token
	Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)

	// LoginWithCookie authenticates credentials and, when cookie sessions are enabled,
	// writes the session cookie to w. Implementations that do not support cookies
	// should delegate to Login and ignore w.
	LoginWithCookie(ctx context.Context, req LoginRequest, w http.ResponseWriter) (*LoginResponse, error)

	// Logout invalidates a user's session/token
	Logout(ctx context.Context, req LogoutRequest) error

	// LogoutWithCookie invalidates a user's session/token and, when cookie sessions are
	// enabled, clears the session cookie on w. Implementations that do not support cookies
	// should delegate to Logout and ignore w.
	LogoutWithCookie(ctx context.Context, req LogoutRequest, w http.ResponseWriter) error

	// Authenticate extracts and validates user from HTTP request
	// Returns UserContext or error if authentication fails
	Authenticate(r *http.Request) (*UserContext, error)

	// SetAuthenticateCallback registers a fallback called when primary authentication fails.
	// If the callback returns a non-nil UserContext, that result is used instead of the error.
	SetAuthenticateCallback(fn func(r *http.Request) (*UserContext, error))
}

// Registrable allows providers to support user registration
type Registrable interface {
	// Register creates a new user account
	Register(ctx context.Context, req RegisterRequest) (*LoginResponse, error)
}

// ColumnSecurityProvider handles column-level security (masking/hiding)
type ColumnSecurityProvider interface {
	// GetColumnSecurity loads column security rules for a user and entity
	GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error)
}

// RowSecurityProvider handles row-level security (filtering)
type RowSecurityProvider interface {
	// GetRowSecurity loads row security rules for a user and entity.
	// userRef identifies the user and is opaque to the caller: it may be an int ID,
	// a string/UUID, or the full *security.UserContext (see SecurityContext.GetUserRef),
	// so providers backed by non-integer user identifiers (e.g. UUIDs) or that need
	// access to JWT claims can implement row security without relying on a numeric ID.
	GetRowSecurity(ctx context.Context, userRef any, schema, table string) (RowSecurity, error)
}

// SecurityProvider is the main interface combining all security concerns
type SecurityProvider interface {
	Authenticator
	ColumnSecurityProvider
	RowSecurityProvider
}

// Optional interfaces for advanced functionality

// Refreshable allows providers to support token refresh
type Refreshable interface {
	// RefreshToken exchanges a refresh token for a new access token
	RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error)
}

// APIKeyLoginable allows providers to exchange a raw API key for a session.
type APIKeyLoginable interface {
	// LoginWithAPIKey validates the raw API key and creates a session for its user.
	// Unknown, expired and inactive keys all yield the same generic error.
	LoginWithAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*LoginResponse, error)
}

// Validatable allows providers to validate tokens without full authentication
type Validatable interface {
	// ValidateToken checks if a token is valid without extracting full user context
	ValidateToken(ctx context.Context, token string) (bool, error)
}

// Cacheable allows providers to support caching of security rules
type Cacheable interface {
	// ClearCache clears cached security rules for a user/entity
	ClearCache(ctx context.Context, userID int, schema, table string) error
}

// PasswordResettable allows providers to support self-service password reset
type PasswordResettable interface {
	// RequestPasswordReset creates a reset token for the given email/username
	RequestPasswordReset(ctx context.Context, req PasswordResetRequest) (*PasswordResetResponse, error)

	// CompletePasswordReset validates the token and sets the new password
	CompletePasswordReset(ctx context.Context, req PasswordResetCompleteRequest) error
}
