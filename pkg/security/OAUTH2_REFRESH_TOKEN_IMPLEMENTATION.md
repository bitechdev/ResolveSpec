# OAuth2 Refresh Token Implementation

## Overview

OAuth2 refresh token functionality is **fully implemented** in the ResolveSpec security package. This allows refreshing expired access tokens without requiring users to re-authenticate.

## Implementation Status: ✅ COMPLETE

### Components Implemented

1. **✅ Database Schema** - Tables and stored procedures
2. **✅ Go Methods** - OAuth2RefreshToken implementation
3. **✅ Thread Safety** - Mutex protection for provider map
4. **✅ Examples** - Working code examples
5. **✅ Documentation** - Complete API reference

---

## 1. Database Schema

### Tables Modified

```sql
-- user_sessions table with OAuth2 token fields
CREATE TABLE IF NOT EXISTS user_sessions (
    id SERIAL PRIMARY KEY,
    session_token VARCHAR(500) NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45),
    user_agent TEXT,
    access_token TEXT,          -- OAuth2 access token
    refresh_token TEXT,         -- OAuth2 refresh token
    token_type VARCHAR(50),     -- "Bearer", etc.
    auth_provider VARCHAR(50)   -- "google", "github", etc.
);
```

### Stored Procedures

**`resolvespec_oauth_getrefreshtoken(p_refresh_token)`**
- Gets OAuth2 session data by refresh token
- Returns: `{user_id, access_token, token_type, expiry}`
- Location: `database_schema.sql:714`

**`resolvespec_oauth_updaterefreshtoken(p_update_data)`**
- Updates session with new tokens after refresh
- Input: `{user_id, old_refresh_token, new_session_token, new_access_token, new_refresh_token, expires_at}`
- Location: `database_schema.sql:752`

**`resolvespec_oauth_getuser(p_user_id)`**
- Gets user data by ID for building UserContext
- Location: `database_schema.sql:791`

---

## 2. Go Implementation

### Method Signature

```go
func (a *DatabaseAuthenticator) OAuth2RefreshToken(
    ctx context.Context,
    refreshToken string,
    providerName string,
) (*LoginResponse, error)
```

**Location:** `pkg/security/oauth2_methods.go:375`

### Implementation Flow

```
1. Validate provider exists
   ├─ getOAuth2Provider(providerName) with RLock
   └─ Return error if provider not configured

2. Get session from database
   ├─ Call resolvespec_oauth_getrefreshtoken(refreshToken)
   └─ Parse session data {user_id, access_token, token_type, expiry}

3. Refresh token with OAuth2 provider
   ├─ Create oauth2.Token from stored data
   ├─ Use provider.config.TokenSource(ctx, oldToken)
   └─ Call tokenSource.Token() to get new token

4. Generate new session token
   └─ Use OAuth2GenerateState() for secure random token

5. Update database
   ├─ Call resolvespec_oauth_updaterefreshtoken()
   └─ Store new session_token, access_token, refresh_token

6. Get user data
   ├─ Call resolvespec_oauth_getuser(user_id)
   └─ Build UserContext

7. Return LoginResponse
   └─ {Token, RefreshToken, User, ExpiresIn}
```

### Thread Safety

**Mutex Protection:** All access to `oauth2Providers` map is protected with `sync.RWMutex`

```go
type DatabaseAuthenticator struct {
    oauth2Providers      map[string]*OAuth2Provider
    oauth2ProvidersMutex sync.RWMutex  // Thread-safe access
}

// Read operations use RLock
func (a *DatabaseAuthenticator) getOAuth2Provider(name string) {
    a.oauth2ProvidersMutex.RLock()
    defer a.oauth2ProvidersMutex.RUnlock()
    // ... access map
}

// Write operations use Lock
func (a *DatabaseAuthenticator) WithOAuth2(cfg OAuth2Config) {
    a.oauth2ProvidersMutex.Lock()
    defer a.oauth2ProvidersMutex.Unlock()
    // ... modify map
}
```

---

## 3. Usage Examples

### Single Provider (Google)

```go
package main

import (
    "database/sql"
    "encoding/json"
    "net/http"
    "github.com/bitechdev/ResolveSpec/pkg/security"
    "github.com/gorilla/mux"
)

func main() {
    db, _ := sql.Open("postgres", "connection-string")
    
    // Create Google OAuth2 authenticator
    auth := security.NewGoogleAuthenticator(
        "your-client-id",
        "your-client-secret",
        "http://localhost:8080/auth/google/callback",
        db,
    )
    
    router := mux.NewRouter()
    
    // Token refresh endpoint
    router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            RefreshToken string `json:"refresh_token"`
        }
        json.NewDecoder(r.Body).Decode(&req)
        
        // Refresh token (provider name defaults to "google")
        loginResp, err := auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, "google")
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
    
    http.ListenAndServe(":8080", router)
}
```

### Multi-Provider Setup

```go
// Single authenticator with multiple OAuth2 providers
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

// Refresh endpoint with provider selection
router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
    var req struct {
        RefreshToken string `json:"refresh_token"`
        Provider     string `json:"provider"` // "google" or "github"
    }
    json.NewDecoder(r.Body).Decode(&req)
    
    // Refresh with specific provider
    loginResp, err := auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, req.Provider)
    if err != nil {
        http.Error(w, err.Error(), http.StatusUnauthorized)
        return
    }
    
    json.NewEncoder(w).Encode(loginResp)
})
```

### Client-Side Usage

```javascript
// JavaScript client example
async function refreshAccessToken() {
    const refreshToken = localStorage.getItem('refresh_token');
    const provider = localStorage.getItem('auth_provider'); // "google", "github", etc.
    
    const response = await fetch('/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            refresh_token: refreshToken,
            provider: provider
        })
    });
    
    if (response.ok) {
        const data = await response.json();
        
        // Store new tokens
        localStorage.setItem('access_token', data.token);
        localStorage.setItem('refresh_token', data.refresh_token);
        
        console.log('Token refreshed successfully');
        return data.token;
    } else {
        // Refresh failed - redirect to login
        window.location.href = '/login';
    }
}

// Automatically refresh token when API returns 401
async function apiCall(endpoint) {
    let response = await fetch(endpoint, {
        headers: {
            'Authorization': 'Bearer ' + localStorage.getItem('access_token')
        }
    });
    
    if (response.status === 401) {
        // Token expired - try refresh
        const newToken = await refreshAccessToken();
        
        // Retry with new token
        response = await fetch(endpoint, {
            headers: {
                'Authorization': 'Bearer ' + newToken
            }
        });
    }
    
    return response.json();
}
```

---

## 4. API Reference

### DatabaseAuthenticator Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `OAuth2RefreshToken` | `(ctx, refreshToken, provider) (*LoginResponse, error)` | Refreshes expired OAuth2 access token |
| `WithOAuth2` | `(cfg OAuth2Config) *DatabaseAuthenticator` | Adds OAuth2 provider (chainable) |
| `OAuth2GetAuthURL` | `(provider, state) (string, error)` | Gets authorization URL |
| `OAuth2HandleCallback` | `(ctx, provider, code, state) (*LoginResponse, error)` | Handles OAuth2 callback |
| `OAuth2GenerateState` | `() (string, error)` | Generates CSRF state token |
| `OAuth2GetProviders` | `() []string` | Lists configured providers |

### LoginResponse Structure

```go
type LoginResponse struct {
    Token        string       // New session token
    RefreshToken string       // New refresh token (may be same as input)
    User         *UserContext // User information
    ExpiresIn    int64        // Seconds until expiration
}

type UserContext struct {
    UserID    int      // Database user ID
    UserName  string   // Username
    Email     string   // Email address
    UserLevel int      // Permission level
    SessionID string   // Session token
    RemoteID  string   // OAuth2 provider user ID
    Roles     []string // User roles
    Claims    map[string]any // Additional claims
}
```

---

## 5. Important Notes

### Provider Configuration

**For Google:** Add `access_type=offline` to get refresh token on first login:

```go
auth := security.NewGoogleAuthenticator(clientID, clientSecret, redirectURL, db)
// When generating auth URL, add access_type parameter
authURL, _ := auth.OAuth2GetAuthURL("google", state)
authURL += "&access_type=offline&prompt=consent"
```

**For GitHub:** Refresh tokens are not always provided. Check provider documentation.

### Token Storage

- Store refresh tokens securely on client (localStorage, secure cookie, etc.)
- Never log refresh tokens
- Refresh tokens are long-lived (days/months depending on provider)
- Access tokens are short-lived (minutes/hours)

### Error Handling

Common errors:
- `"invalid or expired refresh token"` - Token expired or revoked
- `"OAuth2 provider 'xxx' not found"` - Provider not configured
- `"failed to refresh token with provider"` - Provider rejected refresh request

### Security Best Practices

1. **Always use HTTPS** for token transmission
2. **Store refresh tokens securely** on client
3. **Set appropriate cookie flags**: `HttpOnly`, `Secure`, `SameSite`
4. **Implement token rotation** - issue new refresh token on each refresh
5. **Revoke old tokens** after successful refresh
6. **Rate limit** refresh endpoints
7. **Log refresh attempts** for audit trail

---

## 6. Testing

### Manual Test Flow

1. **Initial Login:**
   ```bash
   curl http://localhost:8080/auth/google/login
   # Follow redirect to Google
   # Returns to callback with LoginResponse containing refresh_token
   ```

2. **Wait for Token Expiry (or manually expire in DB)**

3. **Refresh Token:**
   ```bash
   curl -X POST http://localhost:8080/auth/refresh \
     -H "Content-Type: application/json" \
     -d '{
       "refresh_token": "ya29.a0AfH6SMB...",
       "provider": "google"
     }'
   
   # Response:
   {
     "token": "sess_abc123...",
     "refresh_token": "ya29.a0AfH6SMB...",
     "user": {
       "user_id": 1,
       "user_name": "john_doe",
       "email": "john@example.com",
       "session_id": "sess_abc123..."
     },
     "expires_in": 3600
   }
   ```

4. **Use New Token:**
   ```bash
   curl http://localhost:8080/api/protected \
     -H "Authorization: Bearer sess_abc123..."
   ```

### Database Verification

```sql
-- Check session with refresh token
SELECT session_token, user_id, expires_at, refresh_token, auth_provider
FROM user_sessions
WHERE refresh_token = 'ya29.a0AfH6SMB...';

-- Verify token was updated after refresh
SELECT session_token, access_token, refresh_token, 
       expires_at, last_activity_at
FROM user_sessions
WHERE user_id = 1
ORDER BY created_at DESC
LIMIT 1;
```

---

## 7. Troubleshooting

### "Refresh token not found or expired"

**Cause:** Refresh token doesn't exist in database or session expired

**Solution:**
- Check if initial OAuth2 login stored refresh token
- Verify provider returns refresh token (some require `access_type=offline`)
- Check session hasn't been deleted from database

### "Failed to refresh token with provider"

**Cause:** OAuth2 provider rejected the refresh request

**Possible reasons:**
- Refresh token was revoked by user
- OAuth2 app credentials changed
- Network connectivity issues
- Provider rate limiting

**Solution:**
- Re-authenticate user (full OAuth2 flow)
- Check provider dashboard for app status
- Verify client credentials are correct

### "OAuth2 provider 'xxx' not found"

**Cause:** Provider not registered with `WithOAuth2()`

**Solution:**
```go
// Make sure provider is configured
auth := security.NewDatabaseAuthenticator(db).
    WithOAuth2(security.OAuth2Config{
        ProviderName: "google", // This name must match refresh call
        // ... other config
    })

// Then use same name in refresh
auth.OAuth2RefreshToken(ctx, token, "google") // Must match ProviderName
```

---

## 8. Complete Working Example

See `pkg/security/oauth2_examples.go:250` for full working example with token refresh.

---

## Summary

OAuth2 refresh token functionality is **production-ready** with:

- ✅ Complete database schema with stored procedures
- ✅ Thread-safe Go implementation with mutex protection
- ✅ Multi-provider support (Google, GitHub, Microsoft, Facebook, custom)
- ✅ Comprehensive error handling
- ✅ Working code examples
- ✅ Full API documentation
- ✅ Security best practices implemented

**No additional implementation needed - feature is complete and functional.**
