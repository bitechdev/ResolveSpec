package sectypes

// UserContext holds authenticated user information
type UserContext struct {
	UserID           int            `json:"user_id"`
	UserName         string         `json:"user_name"`
	UserLevel        int            `json:"user_level"`
	SessionID        string         `json:"session_id"`
	SessionRID       int64          `json:"session_rid"`
	RemoteID         string         `json:"remote_id"`
	Roles            []string       `json:"roles"`
	Email            string         `json:"email"`
	Claims           map[string]any `json:"claims"`
	Meta             map[string]any `json:"meta"`               // Additional metadata that can hold any JSON-serializable values
	TwoFactorEnabled bool           `json:"two_factor_enabled"` // Indicates if 2FA is enabled for this user
	ProgramUserID    int            `json:"program_user_id"`
	ProgramUserTable string         `json:"program_user_table"`
}

// LoginRequest contains credentials for login
type LoginRequest struct {
	Username      string         `json:"username"`
	Password      string         `json:"password"`
	TwoFactorCode string         `json:"two_factor_code,omitempty"` // TOTP or backup code
	Claims        map[string]any `json:"claims"`                    // Additional login data
	Meta          map[string]any `json:"meta"`                      // Additional metadata to be set on user context
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
	Token              string           `json:"token"`
	RefreshToken       string           `json:"refresh_token"`
	User               *UserContext     `json:"user"`
	ExpiresIn          int64            `json:"expires_in"`                 // Token expiration in seconds
	Requires2FA        bool             `json:"requires_2fa"`               // True if 2FA code is required
	TwoFactorSetupData *TwoFactorSecret `json:"two_factor_setup,omitempty"` // Present when setting up 2FA
	Meta               map[string]any   `json:"meta"`                       // Additional metadata to be set on user context
}

// LogoutRequest contains information for logout
type LogoutRequest struct {
	Token  string `json:"token"`
	UserID int    `json:"user_id"`
}

// PasswordResetRequest initiates a password reset for a user
type PasswordResetRequest struct {
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

// PasswordResetResponse is returned when a reset is initiated
type PasswordResetResponse struct {
	// Token is the reset token to be delivered out-of-band (e.g. email).
	// The stored procedure may return it for delivery or leave it empty
	// if the delivery is handled entirely in the database.
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"` // seconds
}

// PasswordResetCompleteRequest completes a password reset using the token
type PasswordResetCompleteRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}
