# OAuth2 Authentication Guide

## Overview

The security package provides OAuth2 authentication support for any OAuth2-compliant provider including Google, GitHub, Microsoft, Facebook, and custom providers.

## Features

- **Universal OAuth2 Support**: Works with any OAuth2 provider
- **Pre-configured Providers**: Google, GitHub, Microsoft, Facebook
- **Multi-Provider Support**: Use all OAuth2 providers simultaneously
- **Custom Providers**: Easy configuration for any OAuth2 service
- **Session Management**: Database-backed session storage
- **Token Refresh**: Automatic token refresh support
- **State Validation**: Built-in CSRF protection
- **User Auto-Creation**: Automatically creates users on first login
- **Unified Authentication**: OAuth2 and traditional auth share same session storage

## Quick Start

### 1. Database Setup

```sql
-- Run the schema from database_schema.sql
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255),
    user_level INTEGER DEFAULT 0,
    roles VARCHAR(500),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP,
    remote_id VARCHAR(255),
    auth_provider VARCHAR(50)
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id SERIAL PRIMARY KEY,
    session_token VARCHAR(500) NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45),
    user_agent TEXT,
    access_token TEXT,
    refresh_token TEXT,
    token_type VARCHAR(50) DEFAULT 'Bearer',
    auth_provider VARCHAR(50)
);

-- OAuth2 stored procedures (7 functions)
-- See database_schema.sql for full implementation
```

### 2. Google OAuth2

```go
import "github.com/bitechdev/ResolveSpec/pkg/security"

// Create authenticator
oauth2Auth := security.NewGoogleAuthenticator(
    "your-google-client-id",
    "your-google-client-secret",
    "http://localhost:8080/auth/google/callback",
    db,
)

// Login route - redirects to Google
router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
    state, _ := oauth2Auth.OAuth2GenerateState()
    authURL, _ := oauth2Auth.OAuth2GetAuthURL(state)
    http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
})

// Callback route - handles Google response
router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
    code := r.URL.Query().Get("code")
    state := r.URL.Query().Get("state")

    loginResp, err := oauth2Auth.OAuth2HandleCallback(r.Context(), code, state)
    if err != nil {
        http.Error(w, err.Error(), http.StatusUnauthorized)
        return
    }

    // Set session cookie
    http.SetCookie(w, &http.Cookie{
        Name:     "session_token",
        Value:    loginResp.Token,
        Path:     "/",
        MaxAge:   int(loginResp.ExpiresIn),
        HttpOnly: true,
        Secure:   true,
    })

    http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
})
```

### 3. GitHub OAuth2

```go
oauth2Auth := security.NewGitHubAuthenticator(
    "your-github-client-id",
    "your-github-client-secret",
    "http://localhost:8080/auth/github/callback",
    db,
)

// Same routes pattern as Google
router.HandleFunc("/auth/github/login", ...)
router.HandleFunc("/auth/github/callback", ...)
```

### 4. Microsoft OAuth2

```go
oauth2Auth := security.NewMicrosoftAuthenticator(
    "your-microsoft-client-id",
    "your-microsoft-client-secret",
    "http://localhost:8080/auth/microsoft/callback",
    db,
)
```

### 5. Facebook OAuth2

```go
oauth2Auth := security.NewFacebookAuthenticator(
    "your-facebook-client-id",
    "your-facebook-client-secret",
    "http://localhost:8080/auth/facebook/callback",
    db,
)
```

## Custom OAuth2 Provider

```go
oauth2Auth := security.NewDatabaseAuthenticator(db).WithOAuth2(security.OAuth2Config{
    ClientID:     "your-client-id",
    ClientSecret: "your-client-secret",
    RedirectURL:  "http://localhost:8080/auth/callback",
    Scopes:       []string{"openid", "profile", "email"},
    AuthURL:      "https://your-provider.com/oauth/authorize",
    TokenURL:     "https://your-provider.com/oauth/token",
    UserInfoURL:  "https://your-provider.com/oauth/userinfo",
    DB:           db,
    ProviderName: "custom",

    // Optional: Custom user info parser
    UserInfoParser: func(userInfo map[string]any) (*security.UserContext, error) {
        return &security.UserContext{
            UserName:  userInfo["username"].(string),
            Email:     userInfo["email"].(string),
            RemoteID:  userInfo["id"].(string),
            UserLevel: 1,
            Roles:     []string{"user"},
            Claims:    userInfo,
        }, nil
    },
})
```

## Protected Routes

```go
// Create security provider
colSec := security.NewDatabaseColumnSecurityProvider(db)
rowSec := security.NewDatabaseRowSecurityProvider(db)
provider, _ := security.NewCompositeSecurityProvider(oauth2Auth, colSec, rowSec)
securityList, _ := security.NewSecurityList(provider)

// Apply middleware to protected routes
protectedRouter := router.PathPrefix("/api").Subrouter()
protectedRouter.Use(security.NewAuthMiddleware(securityList))
protectedRouter.Use(security.SetSecurityMiddleware(securityList))

protectedRouter.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
    userCtx, _ := security.GetUserContext(r.Context())
    json.NewEncoder(w).Encode(userCtx)
})
```

## Token Refresh

OAuth2 access tokens expire after a period of time. Use the refresh token to obtain a new access token without requiring the user to log in again.

```go
router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
    var req struct {
        RefreshToken string `json:"refresh_token"`
        Provider     string `json:"provider"` // "google", "github", etc.
    }
    json.NewDecoder(r.Body).Decode(&req)

    // Default to google if not specified
    if req.Provider == "" {
        req.Provider = "google"
    }

    // Use OAuth2-specific refresh method
    loginResp, err := oauth2Auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, req.Provider)
    if err != nil {
        http.Error(w, err.Error(), http.StatusUnauthorized)
        return
    }

    // Set new session cookie
    http.SetCookie(w, &http.Cookie{
        Name:     "session_token",
        Value:    loginResp.Token,
        Path:     "/",
        MaxAge:   int(loginResp.ExpiresIn),
        HttpOnly: true,
        Secure:   true,
    })

    json.NewEncoder(w).Encode(loginResp)
})
```

**Important Notes:**
- The refresh token is returned in the `LoginResponse.RefreshToken` field after successful OAuth2 callback
- Store the refresh token securely on the client side
- Each provider must be configured with the appropriate scopes to receive a refresh token (e.g., `access_type=offline` for Google)
- The `OAuth2RefreshToken` method requires the provider name to identify which OAuth2 provider to use for refreshing

## Logout

```go
router.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
    userCtx, _ := security.GetUserContext(r.Context())
    
    oauth2Auth.Logout(r.Context(), security.LogoutRequest{
        Token:  userCtx.SessionID,
        UserID: userCtx.UserID,
    })

    http.SetCookie(w, &http.Cookie{
        Name:   "session_token",
        Value:  "",
        MaxAge: -1,
    })

    w.WriteHeader(http.StatusOK)
})
```

## Multi-Provider Setup

```go
// Single DatabaseAuthenticator with ALL OAuth2 providers
auth := security.NewDatabaseAuthenticator(db).
    WithOAuth2(security.OAuth2Config{
        ClientID:     "google-client-id",
        ClientSecret: "google-client-secret",
        RedirectURL:  "http://localhost:8080/auth/google/callback",
        Scopes:       []string{"openid", "profile", "email"},
        AuthURL:      "https://accounts.google.com/o/oauth2/auth",
        TokenURL:     "https://oauth2.googleapis.com/token",
        UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
        ProviderName: "google",
    }).
    WithOAuth2(security.OAuth2Config{
        ClientID:     "github-client-id",
        ClientSecret: "github-client-secret",
        RedirectURL:  "http://localhost:8080/auth/github/callback",
        Scopes:       []string{"user:email"},
        AuthURL:      "https://github.com/login/oauth/authorize",
        TokenURL:     "https://github.com/login/oauth/access_token",
        UserInfoURL:  "https://api.github.com/user",
        ProviderName: "github",
    })

// Get list of configured providers
providers := auth.OAuth2GetProviders() // ["google", "github"]

// Google routes
router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
    state, _ := auth.OAuth2GenerateState()
    authURL, _ := auth.OAuth2GetAuthURL("google", state)
    http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
})

router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
    loginResp, err := auth.OAuth2HandleCallback(r.Context(), "google", 
        r.URL.Query().Get("code"), r.URL.Query().Get("state"))
    // ... handle response
})

// GitHub routes
router.HandleFunc("/auth/github/login", func(w http.ResponseWriter, r *http.Request) {
    state, _ := auth.OAuth2GenerateState()
    authURL, _ := auth.OAuth2GetAuthURL("github", state)
    http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
})

router.HandleFunc("/auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
    loginResp, err := auth.OAuth2HandleCallback(r.Context(), "github",
        r.URL.Query().Get("code"), r.URL.Query().Get("state"))
    // ... handle response
})

// Use same authenticator for protected routes - works for ALL providers
provider, _ := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
securityList, _ := security.NewSecurityList(provider)
```

## Configuration Options

### OAuth2Config Fields

| Field | Type | Description |
|-------|------|-------------|
| ClientID | string | OAuth2 client ID from provider |
| ClientSecret | string | OAuth2 client secret |
| RedirectURL | string | Callback URL registered with provider |
| Scopes | []string | OAuth2 scopes to request |
| AuthURL | string | Provider's authorization endpoint |
| TokenURL | string | Provider's token endpoint |
| UserInfoURL | string | Provider's user info endpoint |
| DB | *sql.DB | Database connection for sessions |
| UserInfoParser | func | Custom parser for user info (optional) |
| StateValidator | func | Custom state validator (optional) |
| ProviderName | string | Provider name for logging (optional) |

## User Info Parsing

The default parser extracts these standard fields:
- `sub` → RemoteID
- `email` → Email, UserName
- `name` → UserName
- `login` → UserName (GitHub)

Custom parser example:

```go
UserInfoParser: func(userInfo map[string]any) (*security.UserContext, error) {
    // Extract custom fields
    ctx := &security.UserContext{
        UserName:  userInfo["preferred_username"].(string),
        Email:     userInfo["email"].(string),
        RemoteID:  userInfo["sub"].(string),
        UserLevel: 1,
        Roles:     []string{"user"},
        Claims:    userInfo, // Store all claims
    }

    // Add custom roles based on provider data
    if groups, ok := userInfo["groups"].([]interface{}); ok {
        for _, g := range groups {
            ctx.Roles = append(ctx.Roles, g.(string))
        }
    }

    return ctx, nil
}
```

## Security Best Practices

1. **Always use HTTPS in production**
   ```go
   http.SetCookie(w, &http.Cookie{
       Secure:   true,  // Only send over HTTPS
       HttpOnly: true,  // Prevent XSS access
       SameSite: http.SameSiteLaxMode, // CSRF protection
   })
   ```

2. **Store secrets securely**
   ```go
   clientID := os.Getenv("GOOGLE_CLIENT_ID")
   clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
   ```

3. **Validate redirect URLs**
   - Only register trusted redirect URLs with OAuth2 providers
   - Never accept redirect URL from request parameters

5. **Session expiration**
   - OAuth2 sessions automatically expire based on token expiry
   - Clean up expired sessions periodically:
   ```sql
   DELETE FROM user_sessions WHERE expires_at < NOW();
   ```

4. **State parameter**
   - Automatically generated with cryptographic randomness
   - One-time use and expires after 10 minutes
   - Prevents CSRF attacks

## Implementation Details

All database operations use stored procedures for consistency and security:
- `resolvespec_oauth_getorcreateuser` - Find or create OAuth2 user
- `resolvespec_oauth_createsession` - Create OAuth2 session
- `resolvespec_oauth_getsession` - Validate and retrieve session
- `resolvespec_oauth_deletesession` - Logout/delete session
- `resolvespec_oauth_getrefreshtoken` - Get session by refresh token
- `resolvespec_oauth_updaterefreshtoken` - Update tokens after refresh
- `resolvespec_oauth_getuser` - Get user data by ID

## Provider Setup Guides

### Google

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing
3. Enable Google+ API
4. Create OAuth 2.0 credentials
5. Add authorized redirect URI: `http://localhost:8080/auth/google/callback`
6. Copy Client ID and Client Secret

### GitHub

1. Go to [GitHub Developer Settings](https://github.com/settings/developers)
2. Click "New OAuth App"
3. Set Homepage URL: `http://localhost:8080`
4. Set Authorization callback URL: `http://localhost:8080/auth/github/callback`
5. Copy Client ID and Client Secret

### Microsoft

1. Go to [Azure Portal](https://portal.azure.com/)
2. Register new application in Azure AD
3. Add redirect URI: `http://localhost:8080/auth/microsoft/callback`
4. Create client secret
5. Copy Application (client) ID and secret value

### Facebook

1. Go to [Facebook Developers](https://developers.facebook.com/)
2. Create new app
3. Add Facebook Login product
4. Set Valid OAuth Redirect URIs: `http://localhost:8080/auth/facebook/callback`
5. Copy App ID and App Secret

## Troubleshooting

### "redirect_uri_mismatch" error
- Ensure the redirect URL in code matches exactly with provider configuration
- Include protocol (http/https), domain, port, and path

### "invalid_client" error
- Verify Client ID and Client Secret are correct
- Check if credentials are for the correct environment (dev/prod)

### "invalid_grant" error during token exchange
- State parameter validation failed
- Token might have expired
- Check server time synchronization

### User not created after successful OAuth2 login
- Check database constraints (username/email unique)
- Verify UserInfoParser is extracting required fields
- Check database logs for constraint violations

## Testing

```go
func TestOAuth2Flow(t *testing.T) {
    // Mock database
    db, mock, _ := sqlmock.New()
    
    oauth2Auth := security.NewGoogleAuthenticator(
        "test-client-id",
        "test-client-secret",
        "http://localhost/callback",
        db,
    )

    // Test state generation
    state, err := oauth2Auth.GenerateState()
    assert.NoError(t, err)
    assert.NotEmpty(t, state)

    // Test auth URL generation
    authURL := oauth2Auth.GetAuthURL(state)
    assert.Contains(t, authURL, "accounts.google.com")
    assert.Contains(t, authURL, state)
}
```

## API Reference

### DatabaseAuthenticator with OAuth2

| Method | Description |
|--------|-------------|
| WithOAuth2(cfg) | Adds OAuth2 provider (can be called multiple times, returns *DatabaseAuthenticator) |
| OAuth2GetAuthURL(provider, state) | Returns OAuth2 authorization URL for specified provider |
| OAuth2GenerateState() | Generates random state for CSRF protection |
| OAuth2HandleCallback(ctx, provider, code, state) | Exchanges code for token and creates session |
| OAuth2RefreshToken(ctx, refreshToken, provider) | Refreshes expired access token using refresh token |
| OAuth2GetProviders() | Returns list of configured OAuth2 provider names |
| Login(ctx, req) | Standard username/password login |
| Logout(ctx, req) | Invalidates session (works for both OAuth2 and regular sessions) |
| Authenticate(r) | Validates session token from request (works for both OAuth2 and regular sessions) |

### Pre-configured Constructors

- `NewGoogleAuthenticator(clientID, secret, redirectURL, db)` - Single provider
- `NewGitHubAuthenticator(clientID, secret, redirectURL, db)` - Single provider
- `NewMicrosoftAuthenticator(clientID, secret, redirectURL, db)` - Single provider
- `NewFacebookAuthenticator(clientID, secret, redirectURL, db)` - Single provider
- `NewMultiProviderAuthenticator(db, configs)` - Multiple providers at once

All return `*DatabaseAuthenticator` with OAuth2 pre-configured.

For multiple providers, use `WithOAuth2()` multiple times or `NewMultiProviderAuthenticator()`.

## Examples

Complete working examples available in `oauth2_examples.go`:
- Basic Google OAuth2
- GitHub OAuth2
- Custom provider
- Multi-provider setup
- Token refresh
- Logout flow
- Complete integration with security middleware
