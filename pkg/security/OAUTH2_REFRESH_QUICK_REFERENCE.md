# OAuth2 Refresh Token - Quick Reference

## Quick Setup (3 Steps)

### 1. Initialize Authenticator
```go
auth := security.NewGoogleAuthenticator(
    "client-id",
    "client-secret", 
    "http://localhost:8080/auth/google/callback",
    db,
)
```

### 2. OAuth2 Login Flow
```go
// Login - Redirect to Google
router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
    state, _ := auth.OAuth2GenerateState()
    authURL, _ := auth.OAuth2GetAuthURL("google", state)
    http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
})

// Callback - Store tokens
router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
    loginResp, _ := auth.OAuth2HandleCallback(
        r.Context(),
        "google",
        r.URL.Query().Get("code"),
        r.URL.Query().Get("state"),
    )
    
    // Save refresh_token on client
    // loginResp.RefreshToken - Store this securely!
    // loginResp.Token - Session token for API calls
})
```

### 3. Refresh Endpoint
```go
router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
    var req struct {
        RefreshToken string `json:"refresh_token"`
    }
    json.NewDecoder(r.Body).Decode(&req)
    
    // Refresh token
    loginResp, err := auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, "google")
    if err != nil {
        http.Error(w, err.Error(), 401)
        return
    }
    
    json.NewEncoder(w).Encode(loginResp)
})
```

---

## Multi-Provider Example

```go
// Configure multiple providers
auth := security.NewDatabaseAuthenticator(db).
    WithOAuth2(security.OAuth2Config{
        ProviderName: "google",
        ClientID:     "google-client-id",
        ClientSecret: "google-secret",
        RedirectURL:  "http://localhost:8080/auth/google/callback",
        Scopes:       []string{"openid", "profile", "email"},
        AuthURL:      "https://accounts.google.com/o/oauth2/auth",
        TokenURL:     "https://oauth2.googleapis.com/token",
        UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
    }).
    WithOAuth2(security.OAuth2Config{
        ProviderName: "github",
        ClientID:     "github-client-id",
        ClientSecret: "github-secret",
        RedirectURL:  "http://localhost:8080/auth/github/callback",
        Scopes:       []string{"user:email"},
        AuthURL:      "https://github.com/login/oauth/authorize",
        TokenURL:     "https://github.com/login/oauth/access_token",
        UserInfoURL:  "https://api.github.com/user",
    })

// Refresh with provider selection
router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
    var req struct {
        RefreshToken string `json:"refresh_token"`
        Provider     string `json:"provider"` // "google" or "github"
    }
    json.NewDecoder(r.Body).Decode(&req)
    
    loginResp, err := auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, req.Provider)
    if err != nil {
        http.Error(w, err.Error(), 401)
        return
    }
    
    json.NewEncoder(w).Encode(loginResp)
})
```

---

## Client-Side JavaScript

```javascript
// Automatic token refresh on 401
async function apiCall(url) {
    let response = await fetch(url, {
        headers: {
            'Authorization': 'Bearer ' + localStorage.getItem('access_token')
        }
    });
    
    // Token expired - refresh it
    if (response.status === 401) {
        await refreshToken();
        
        // Retry request with new token
        response = await fetch(url, {
            headers: {
                'Authorization': 'Bearer ' + localStorage.getItem('access_token')
            }
        });
    }
    
    return response.json();
}

async function refreshToken() {
    const response = await fetch('/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            refresh_token: localStorage.getItem('refresh_token'),
            provider: localStorage.getItem('provider')
        })
    });
    
    if (response.ok) {
        const data = await response.json();
        localStorage.setItem('access_token', data.token);
        localStorage.setItem('refresh_token', data.refresh_token);
    } else {
        // Refresh failed - redirect to login
        window.location.href = '/login';
    }
}
```

---

## API Methods

| Method | Parameters | Returns |
|--------|-----------|---------|
| `OAuth2RefreshToken` | `ctx, refreshToken, provider` | `*LoginResponse, error` |
| `OAuth2HandleCallback` | `ctx, provider, code, state` | `*LoginResponse, error` |
| `OAuth2GetAuthURL` | `provider, state` | `string, error` |
| `OAuth2GenerateState` | none | `string, error` |
| `OAuth2GetProviders` | none | `[]string` |

---

## LoginResponse Structure

```go
type LoginResponse struct {
    Token        string       // New session token for API calls
    RefreshToken string       // Refresh token (store securely)
    User         *UserContext // User information
    ExpiresIn    int64        // Seconds until token expires
}
```

---

## Database Stored Procedures

- `resolvespec_oauth_getrefreshtoken(refresh_token)` - Get session by refresh token
- `resolvespec_oauth_updaterefreshtoken(update_data)` - Update tokens after refresh
- `resolvespec_oauth_getuser(user_id)` - Get user data

All procedures return: `{p_success bool, p_error text, p_data jsonb}`

---

## Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid or expired refresh token` | Token revoked/expired | Re-authenticate user |
| `OAuth2 provider 'xxx' not found` | Provider not configured | Add with `WithOAuth2()` |
| `failed to refresh token with provider` | Provider rejected request | Check credentials, re-auth user |

---

## Security Checklist

- [ ] Use HTTPS for all OAuth2 endpoints
- [ ] Store refresh tokens securely (HttpOnly cookies or encrypted storage)
- [ ] Set cookie flags: `HttpOnly`, `Secure`, `SameSite=Strict`
- [ ] Implement rate limiting on refresh endpoint
- [ ] Log refresh attempts for audit
- [ ] Rotate tokens on refresh
- [ ] Revoke old sessions after successful refresh

---

## Testing

```bash
# 1. Login and get refresh token
curl http://localhost:8080/auth/google/login
# Follow OAuth2 flow, get refresh_token from callback response

# 2. Refresh token
curl -X POST http://localhost:8080/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"ya29.xxx","provider":"google"}'

# 3. Use new token
curl http://localhost:8080/api/protected \
  -H "Authorization: Bearer sess_abc123..."
```

---

## Pre-configured Providers

```go
// Google
auth := security.NewGoogleAuthenticator(clientID, secret, redirectURL, db)

// GitHub  
auth := security.NewGitHubAuthenticator(clientID, secret, redirectURL, db)

// Microsoft
auth := security.NewMicrosoftAuthenticator(clientID, secret, redirectURL, db)

// Facebook
auth := security.NewFacebookAuthenticator(clientID, secret, redirectURL, db)

// All providers at once
auth := security.NewMultiProviderAuthenticator(db, map[string]security.OAuth2Config{
    "google": {...},
    "github": {...},
})
```

---

## Provider-Specific Notes

### Google
- Add `access_type=offline` to get refresh token
- Add `prompt=consent` to force consent screen
```go
authURL += "&access_type=offline&prompt=consent"
```

### GitHub
- Refresh tokens not always provided
- May need to request `offline_access` scope

### Microsoft
- Use `offline_access` scope for refresh token

### Facebook
- Tokens expire after 60 days by default
- Check app settings for token expiration policy

---

## Complete Example

See `/pkg/security/oauth2_examples.go` line 250 for full working example.

For detailed documentation see `/pkg/security/OAUTH2_REFRESH_TOKEN_IMPLEMENTATION.md`.
