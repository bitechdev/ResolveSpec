package security

import (
	"context"
	"net/http"
)

// UserContext holds authenticated user information
type UserContext struct {
	UserID    int
	UserName  string
	UserLevel int
	SessionID string
	RemoteID  string
	Roles     []string
	Email     string
	Claims    map[string]any
}

// LoginRequest contains credentials for login
type LoginRequest struct {
	Username string
	Password string
	Claims   map[string]any // Additional login data
}

// LoginResponse contains the result of a login attempt
type LoginResponse struct {
	Token        string
	RefreshToken string
	User         *UserContext
	ExpiresIn    int64 // Token expiration in seconds
}

// LogoutRequest contains information for logout
type LogoutRequest struct {
	Token  string
	UserID int
}

// Authenticator handles user authentication operations
type Authenticator interface {
	// Login authenticates credentials and returns a token
	Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)

	// Logout invalidates a user's session/token
	Logout(ctx context.Context, req LogoutRequest) error

	// Authenticate extracts and validates user from HTTP request
	// Returns UserContext or error if authentication fails
	Authenticate(r *http.Request) (*UserContext, error)
}

// ColumnSecurityProvider handles column-level security (masking/hiding)
type ColumnSecurityProvider interface {
	// GetColumnSecurity loads column security rules for a user and entity
	GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error)
}

// RowSecurityProvider handles row-level security (filtering)
type RowSecurityProvider interface {
	// GetRowSecurity loads row security rules for a user and entity
	GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error)
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
