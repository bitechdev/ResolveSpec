package security

import (
	"context"
	"net/http"
)

// UserContext holds authenticated user information
type UserContext struct {
	UserID     int            `json:"user_id"`
	UserName   string         `json:"user_name"`
	UserLevel  int            `json:"user_level"`
	SessionID  string         `json:"session_id"`
	SessionRID int64          `json:"session_rid"`
	RemoteID   string         `json:"remote_id"`
	Roles      []string       `json:"roles"`
	Email      string         `json:"email"`
	Claims     map[string]any `json:"claims"`
	Meta       map[string]any `json:"meta"` // Additional metadata that can hold any JSON-serializable values
}

// LoginRequest contains credentials for login
type LoginRequest struct {
	Username string         `json:"username"`
	Password string         `json:"password"`
	Claims   map[string]any `json:"claims"` // Additional login data
	Meta     map[string]any `json:"meta"`   // Additional metadata to be set on user context
}

// RegisterRequest contains information for new user registration
type RegisterRequest struct {
	Username  string         `json:"username"`
	Password  string         `json:"password"`
	Email     string         `json:"email"`
	UserLevel int            `json:"user_level"`
	Roles     []string       `json:"roles"`
	Claims    map[string]any `json:"claims"` // Additional registration data
	Meta      map[string]any `json:"meta"`   // Additional metadata
}

// LoginResponse contains the result of a login attempt
type LoginResponse struct {
	Token        string         `json:"token"`
	RefreshToken string         `json:"refresh_token"`
	User         *UserContext   `json:"user"`
	ExpiresIn    int64          `json:"expires_in"` // Token expiration in seconds
	Meta         map[string]any `json:"meta"`       // Additional metadata to be set on user context
}

// LogoutRequest contains information for logout
type LogoutRequest struct {
	Token  string `json:"token"`
	UserID int    `json:"user_id"`
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
