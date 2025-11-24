package security

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	// Optional: Uncomment if you want to use JWT authentication
	// "github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// Example 1: Simple Header-Based Authenticator
// =============================================

type HeaderAuthenticatorExample struct {
	// Optional: Add any dependencies here (e.g., database, cache)
}

func NewHeaderAuthenticatorExample() *HeaderAuthenticatorExample {
	return &HeaderAuthenticatorExample{}
}

func (a *HeaderAuthenticatorExample) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// For header-based auth, login might not be used
	// Could validate credentials against a database here
	return nil, fmt.Errorf("header authentication does not support login")
}

func (a *HeaderAuthenticatorExample) Logout(ctx context.Context, req LogoutRequest) error {
	// For header-based auth, logout is a no-op
	return nil
}

func (a *HeaderAuthenticatorExample) Authenticate(r *http.Request) (*UserContext, error) {
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

// Example 2: JWT Token Authenticator
// ====================================
// NOTE: To use this, uncomment the jwt import and install: go get github.com/golang-jwt/jwt/v5

type JWTAuthenticatorExample struct {
	secretKey []byte
	db        *gorm.DB
}

func NewJWTAuthenticatorExample(secretKey string, db *gorm.DB) *JWTAuthenticatorExample {
	return &JWTAuthenticatorExample{
		secretKey: []byte(secretKey),
		db:        db,
	}
}

func (a *JWTAuthenticatorExample) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Validate credentials against database
	var user struct {
		ID        int
		Username  string
		Email     string
		Password  string // Should be hashed
		UserLevel int
		Roles     string
	}

	err := a.db.WithContext(ctx).
		Table("users").
		Where("username = ?", req.Username).
		First(&user).Error
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// TODO: Verify password hash
	// if !verifyPassword(user.Password, req.Password) {
	//     return nil, fmt.Errorf("invalid credentials")
	// }

	// Create JWT token
	expiresAt := time.Now().Add(24 * time.Hour)

	// Uncomment when using JWT:
	// token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
	//     "user_id":    user.ID,
	//     "username":   user.Username,
	//     "email":      user.Email,
	//     "user_level": user.UserLevel,
	//     "roles":      user.Roles,
	//     "exp":        expiresAt.Unix(),
	// })
	// tokenString, err := token.SignedString(a.secretKey)
	// if err != nil {
	//     return nil, fmt.Errorf("failed to generate token: %w", err)
	// }

	// Placeholder token for example (replace with actual JWT)
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

func (a *JWTAuthenticatorExample) Logout(ctx context.Context, req LogoutRequest) error {
	// For JWT, logout could involve token blacklisting
	// Add token to blacklist table
	// err := a.db.WithContext(ctx).Table("token_blacklist").Create(map[string]interface{}{
	//     "token":      req.Token,
	//     "expires_at": time.Now().Add(24 * time.Hour),
	// }).Error
	return nil
}

func (a *JWTAuthenticatorExample) Authenticate(r *http.Request) (*UserContext, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("authorization header required")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return nil, fmt.Errorf("bearer token required")
	}

	// Uncomment when using JWT:
	// token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
	//     if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
	//         return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	//     }
	//     return a.secretKey, nil
	// })
	//
	// if err != nil || !token.Valid {
	//     return nil, fmt.Errorf("invalid token: %w", err)
	// }
	//
	// claims, ok := token.Claims.(jwt.MapClaims)
	// if !ok {
	//     return nil, fmt.Errorf("invalid token claims")
	// }
	//
	// return &UserContext{
	//     UserID:    int(claims["user_id"].(float64)),
	//     UserName:  getString(claims, "username"),
	//     Email:     getString(claims, "email"),
	//     UserLevel: getInt(claims, "user_level"),
	//     Roles:     parseRoles(getString(claims, "roles")),
	//     Claims:    claims,
	// }, nil

	// Placeholder implementation (replace with actual JWT parsing)
	return nil, fmt.Errorf("JWT parsing not implemented - uncomment JWT code above")
}

// Example 3: Database Session Authenticator
// ==========================================

type DatabaseAuthenticatorExample struct {
	db *gorm.DB
}

func NewDatabaseAuthenticatorExample(db *gorm.DB) *DatabaseAuthenticatorExample {
	return &DatabaseAuthenticatorExample{db: db}
}

func (a *DatabaseAuthenticatorExample) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Query user from database
	var user struct {
		ID        int
		Username  string
		Email     string
		Password  string // Should be hashed with bcrypt
		UserLevel int
		Roles     string
		IsActive  bool
	}

	err := a.db.WithContext(ctx).
		Table("users").
		Where("username = ? AND is_active = true", req.Username).
		First(&user).Error
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// TODO: Verify password with bcrypt
	// if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
	//     return nil, fmt.Errorf("invalid credentials")
	// }

	// Generate session token
	sessionToken := fmt.Sprintf("sess_%s_%d", generateRandomString(32), time.Now().Unix())
	expiresAt := time.Now().Add(24 * time.Hour)

	// Create session in database
	err = a.db.WithContext(ctx).Table("user_sessions").Create(map[string]any{
		"session_token": sessionToken,
		"user_id":       user.ID,
		"expires_at":    expiresAt,
		"created_at":    time.Now(),
		"ip_address":    req.Claims["ip_address"],
		"user_agent":    req.Claims["user_agent"],
	}).Error
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &LoginResponse{
		Token: sessionToken,
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

func (a *DatabaseAuthenticatorExample) Logout(ctx context.Context, req LogoutRequest) error {
	// Delete session from database
	err := a.db.WithContext(ctx).
		Table("user_sessions").
		Where("session_token = ? AND user_id = ?", req.Token, req.UserID).
		Delete(nil).Error
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

func (a *DatabaseAuthenticatorExample) Authenticate(r *http.Request) (*UserContext, error) {
	// Extract session token from header or cookie
	sessionToken := r.Header.Get("Authorization")
	if sessionToken == "" {
		// Try cookie
		cookie, err := r.Cookie("session_token")
		if err == nil {
			sessionToken = cookie.Value
		}
	} else {
		// Remove "Bearer " prefix if present
		sessionToken = strings.TrimPrefix(sessionToken, "Bearer ")
	}

	if sessionToken == "" {
		return nil, fmt.Errorf("session token required")
	}

	// Query session and user from database
	var session struct {
		SessionToken string
		UserID       int
		ExpiresAt    time.Time
		Username     string
		Email        string
		UserLevel    int
		Roles        string
	}

	query := `
		SELECT
			s.session_token,
			s.user_id,
			s.expires_at,
			u.username,
			u.email,
			u.user_level,
			u.roles
		FROM user_sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.session_token = ?
		  AND s.expires_at > ?
		  AND u.is_active = true
	`

	err := a.db.Raw(query, sessionToken, time.Now()).Scan(&session).Error
	if err != nil {
		return nil, fmt.Errorf("invalid or expired session")
	}

	// Update last activity timestamp
	go a.updateSessionActivity(sessionToken)

	return &UserContext{
		UserID:    session.UserID,
		UserName:  session.Username,
		Email:     session.Email,
		UserLevel: session.UserLevel,
		SessionID: sessionToken,
		Roles:     parseRoles(session.Roles),
	}, nil
}

// updateSessionActivity updates the last activity timestamp for the session
func (a *DatabaseAuthenticatorExample) updateSessionActivity(sessionToken string) {
	a.db.Table("user_sessions").
		Where("session_token = ?", sessionToken).
		Update("last_activity_at", time.Now())
}

// Optional: Implement Refreshable interface
func (a *DatabaseAuthenticatorExample) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// Query the refresh token
	var session struct {
		UserID   int
		Username string
		Email    string
	}

	err := a.db.WithContext(ctx).Raw(`
		SELECT u.id as user_id, u.username, u.email
		FROM user_sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.session_token = ? AND s.expires_at > ?
	`, refreshToken, time.Now()).Scan(&session).Error
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token")
	}

	// Generate new session token
	newSessionToken := fmt.Sprintf("sess_%s_%d", generateRandomString(32), time.Now().Unix())
	expiresAt := time.Now().Add(24 * time.Hour)

	// Create new session
	err = a.db.WithContext(ctx).Table("user_sessions").Create(map[string]any{
		"session_token": newSessionToken,
		"user_id":       session.UserID,
		"expires_at":    expiresAt,
		"created_at":    time.Now(),
	}).Error
	if err != nil {
		return nil, fmt.Errorf("failed to create new session: %w", err)
	}

	// Delete old session
	a.db.WithContext(ctx).Table("user_sessions").Where("session_token = ?", refreshToken).Delete(nil)

	return &LoginResponse{
		Token: newSessionToken,
		User: &UserContext{
			UserID:   session.UserID,
			UserName: session.Username,
			Email:    session.Email,
		},
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

