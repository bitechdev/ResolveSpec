# Security Provider - Quick Reference

## 3-Step Setup

```go
// Step 1: Create security providers
auth := security.NewDatabaseAuthenticator(db) // Session-based (recommended)
// OR: auth := security.NewJWTAuthenticator("secret-key", db)
// OR: auth := security.NewHeaderAuthenticator()

colSec := security.NewDatabaseColumnSecurityProvider(db)
rowSec := security.NewDatabaseRowSecurityProvider(db)

// Step 2: Combine providers
provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)

// Step 3: Setup and apply middleware
securityList := security.SetupSecurityProvider(handler, provider)
router.Use(security.NewAuthMiddleware(securityList))
router.Use(security.SetSecurityMiddleware(securityList))
```

---

## Stored Procedures

**All database operations use PostgreSQL stored procedures** with `resolvespec_*` naming:

### Database Authenticators
```go
// DatabaseAuthenticator uses these stored procedures:
resolvespec_login(jsonb)              // Login with credentials
resolvespec_logout(jsonb)             // Invalidate session
resolvespec_session(text, text)       // Validate session token
resolvespec_session_update(text, jsonb) // Update activity timestamp
resolvespec_refresh_token(text, jsonb)  // Generate new session

// JWTAuthenticator uses these stored procedures:
resolvespec_jwt_login(text, text)     // Validate credentials
resolvespec_jwt_logout(text, int)     // Blacklist token
```

### Security Providers
```go
// DatabaseColumnSecurityProvider:
resolvespec_column_security(int, text, text) // Load column rules

// DatabaseRowSecurityProvider:
resolvespec_row_security(text, text, int)    // Load row template
```

All stored procedures return structured results:
- Session/Login: `(p_success bool, p_error text, p_data jsonb)`
- Security: `(p_success bool, p_error text, p_rules jsonb)`

See `database_schema.sql` for complete definitions.

---

## Interface Signatures

```go
// Authenticator interface
type Authenticator interface {
    Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)
    Logout(ctx context.Context, req LogoutRequest) error
    Authenticate(r *http.Request) (*UserContext, error)
}

// ColumnSecurityProvider interface
type ColumnSecurityProvider interface {
    GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error)
}

// RowSecurityProvider interface
type RowSecurityProvider interface {
    GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error)
}
```

---

## UserContext Structure

```go
security.UserContext{
    UserID:    123,                  // User's unique ID
    UserName:  "john_doe",          // Username
    UserLevel: 5,                    // User privilege level
    SessionID: "sess_abc123",       // Current session ID
    RemoteID:  "remote_xyz",        // Remote system ID
    Roles:     []string{"admin"},   // User roles
    Email:     "john@example.com",  // User email
    Claims:    map[string]any{},    // Additional metadata
}
```

---

## ColumnSecurity Structure

```go
security.ColumnSecurity{
    Path:       []string{"column_name"},  // ["ssn"] or ["address", "street"]
    Accesstype: "mask",                   // "mask" or "hide"
    MaskStart:  5,                        // Mask first N chars
    MaskEnd:    0,                        // Mask last N chars
    MaskChar:   "*",                      // Masking character
    MaskInvert: false,                    // true = mask middle
}
```

### Common Examples

```go
// Hide entire field
{Path: []string{"salary"}, Accesstype: "hide"}

// Mask SSN (show last 4)
{Path: []string{"ssn"}, Accesstype: "mask", MaskStart: 5}

// Mask credit card (show last 4)
{Path: []string{"credit_card"}, Accesstype: "mask", MaskStart: 12}

// Mask email (j***@example.com)
{Path: []string{"email"}, Accesstype: "mask", MaskStart: 1, MaskEnd: 0}
```

---

## RowSecurity Structure

```go
security.RowSecurity{
    Schema:    "public",
    Tablename: "orders",
    UserID:    123,
    Template:  "user_id = {UserID}",  // WHERE clause
    HasBlock:  false,                 // true = block all access
}
```

### Template Variables

- `{UserID}` - Current user ID
- `{PrimaryKeyName}` - Primary key column
- `{TableName}` - Table name
- `{SchemaName}` - Schema name

### Common Examples

```go
// Users see only their records
Template: "user_id = {UserID}"

// Users see their records OR public ones
Template: "user_id = {UserID} OR is_public = true"

// Tenant isolation
Template: "tenant_id = 5 AND user_id = {UserID}"

// Complex with subquery
Template: "dept_id IN (SELECT dept_id FROM user_depts WHERE user_id = {UserID})"

// Block all access
HasBlock: true
```

---

## Example Implementations

### Database Session Authenticator (Recommended)

```go
// Create authenticator
auth := security.NewDatabaseAuthenticator(db)

// Requires these tables:
// - users (id, username, email, password, user_level, roles, is_active)
// - user_sessions (session_token, user_id, expires_at, created_at, last_activity_at)
// See database_schema.sql for full schema

// Features:
// - Login with username/password
// - Session management in database
// - Token refresh support (implements Refreshable)
// - Automatic session expiration
// - Tracks IP address and user agent
// - Works with Authorization header or cookie
```

### Simple Header Authenticator

```go
type HeaderAuthenticator struct{}

func NewHeaderAuthenticator() *HeaderAuthenticator {
    return &HeaderAuthenticator{}
}

func (a *HeaderAuthenticator) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
    return nil, fmt.Errorf("not supported")
}

func (a *HeaderAuthenticator) Logout(ctx context.Context, req security.LogoutRequest) error {
    return nil
}

func (a *HeaderAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
    userIDStr := r.Header.Get("X-User-ID")
    if userIDStr == "" {
        return nil, fmt.Errorf("X-User-ID required")
    }
    userID, _ := strconv.Atoi(userIDStr)
    return &security.UserContext{
        UserID:   userID,
        UserName: r.Header.Get("X-User-Name"),
    }, nil
}
```

### JWT Authenticator

```go
type JWTAuthenticator struct {
    secretKey []byte
    db        *gorm.DB
}

func NewJWTAuthenticator(secret string, db *gorm.DB) *JWTAuthenticator {
    return &JWTAuthenticator{secretKey: []byte(secret), db: db}
}

func (a *JWTAuthenticator) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
    // Validate credentials against database
    var user User
    err := a.db.WithContext(ctx).Where("username = ?", req.Username).First(&user).Error
    if err != nil {
        return nil, fmt.Errorf("invalid credentials")
    }

    // Generate JWT token
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
        "user_id": user.ID,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
    })
    tokenString, _ := token.SignedString(a.secretKey)

    return &security.LoginResponse{
        Token:     tokenString,
        User:      &security.UserContext{UserID: user.ID},
        ExpiresIn: 86400,
    }, nil
}

func (a *JWTAuthenticator) Logout(ctx context.Context, req security.LogoutRequest) error {
    // Add to blacklist
    return a.db.WithContext(ctx).Table("token_blacklist").Create(map[string]any{
        "token":   req.Token,
        "user_id": req.UserID,
    }).Error
}

func (a *JWTAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
    tokenString := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
    token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
        return a.secretKey, nil
    })
    if err != nil || !token.Valid {
        return nil, fmt.Errorf("invalid token")
    }
    claims := token.Claims.(jwt.MapClaims)
    return &security.UserContext{
        UserID: int(claims["user_id"].(float64)),
    }, nil
}
```

### Static Column Security

```go
type ConfigColumnSecurityProvider struct {
    rules map[string][]security.ColumnSecurity
}

func NewConfigColumnSecurityProvider(rules map[string][]security.ColumnSecurity) *ConfigColumnSecurityProvider {
    return &ConfigColumnSecurityProvider{rules: rules}
}

func (p *ConfigColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    key := fmt.Sprintf("%s.%s", schema, table)
    return p.rules[key], nil
}
```

### Database Column Security

```go
type DatabaseColumnSecurityProvider struct {
    db *gorm.DB
}

func NewDatabaseColumnSecurityProvider(db *gorm.DB) *DatabaseColumnSecurityProvider {
    return &DatabaseColumnSecurityProvider{db: db}
}

func (p *DatabaseColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    var records []struct {
        Control    string
        Accesstype string
        JSONValue  string
    }

    query := `
        SELECT control, accesstype, jsonvalue
        FROM core.secaccess
        WHERE rid_hub IN (
            SELECT rid_hub_parent FROM core.hub_link
            WHERE rid_hub_child = ? AND parent_hubtype = 'secgroup'
        )
        AND control ILIKE ?
    `

    err := p.db.WithContext(ctx).Raw(query, userID, fmt.Sprintf("%s.%s%%", schema, table)).Scan(&records).Error
    if err != nil {
        return nil, err
    }

    var rules []security.ColumnSecurity
    for _, rec := range records {
        parts := strings.Split(rec.Control, ".")
        if len(parts) < 3 {
            continue
        }
        rules = append(rules, security.ColumnSecurity{
            Schema:     schema,
            Tablename:  table,
            Path:       parts[2:],
            Accesstype: rec.Accesstype,
        })
    }
    return rules, nil
}
```

### Static Row Security

```go
type ConfigRowSecurityProvider struct {
    templates map[string]string
    blocked   map[string]bool
}

func NewConfigRowSecurityProvider(templates map[string]string, blocked map[string]bool) *ConfigRowSecurityProvider {
    return &ConfigRowSecurityProvider{templates: templates, blocked: blocked}
}

func (p *ConfigRowSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (security.RowSecurity, error) {
    key := fmt.Sprintf("%s.%s", schema, table)

    if p.blocked[key] {
        return security.RowSecurity{HasBlock: true}, nil
    }

    return security.RowSecurity{
        Schema:    schema,
        Tablename: table,
        UserID:    userID,
        Template:  p.templates[key],
    }, nil
}
```

---

## Testing

```go
// Test Authenticator
auth := security.NewHeaderAuthenticator()
req := httptest.NewRequest("GET", "/", nil)
req.Header.Set("X-User-ID", "123")
userCtx, err := auth.Authenticate(req)
assert.Equal(t, 123, userCtx.UserID)

// Test ColumnSecurityProvider
colSec := security.NewConfigColumnSecurityProvider(rules)
cols, err := colSec.GetColumnSecurity(context.Background(), 123, "public", "employees")
assert.Equal(t, "mask", cols[0].Accesstype)

// Test RowSecurityProvider
rowSec := security.NewConfigRowSecurityProvider(templates, blocked)
row, err := rowSec.GetRowSecurity(context.Background(), 123, "public", "orders")
assert.Equal(t, "user_id = {UserID}", row.Template)
```

---

## Request Flow

```
HTTP Request
    ↓
NewAuthMiddleware → calls provider.Authenticate()
    ↓ (adds UserContext to context)
SetSecurityMiddleware → adds SecurityList to context
    ↓
Handler.Handle()
    ↓
BeforeRead Hook → calls provider.GetColumnSecurity() + GetRowSecurity()
    ↓
BeforeScan Hook → applies row security (WHERE clause)
    ↓
Database Query
    ↓
AfterRead Hook → applies column security (masking)
    ↓
HTTP Response
```

---

## Common Patterns

### Role-Based Security

```go
func (p *MyColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    userCtx, _ := security.GetUserContext(ctx)

    if contains(userCtx.Roles, "admin") {
        return []security.ColumnSecurity{}, nil // No restrictions
    }

    return loadRestrictions(userID, schema, table), nil
}
```

### Tenant Isolation

```go
func (p *MyRowSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (security.RowSecurity, error) {
    tenantID := getUserTenant(userID)
    return security.RowSecurity{
        Template: fmt.Sprintf("tenant_id = %d", tenantID),
    }, nil
}
```

### Caching with Decorator

```go
type CachedColumnSecurityProvider struct {
    inner security.ColumnSecurityProvider
    cache *cache.Cache
}

func (p *CachedColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    key := fmt.Sprintf("%d:%s.%s", userID, schema, table)

    if cached, found := p.cache.Get(key); found {
        return cached.([]security.ColumnSecurity), nil
    }

    rules, err := p.inner.GetColumnSecurity(ctx, userID, schema, table)
    if err == nil {
        p.cache.Set(key, rules, cache.DefaultExpiration)
    }
    return rules, err
}
```

---

## Error Handling

```go
// Panic if provider is nil
provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
// panics if any parameter is nil

// Auth middleware returns 401 if Authenticate fails
func (a *MyAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
    if invalid {
        return nil, fmt.Errorf("invalid credentials") // Returns HTTP 401
    }
    return &security.UserContext{UserID: userID}, nil
}

// Security loading can fail gracefully
func (p *MyProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    rules, err := db.Load(...)
    if err != nil {
        log.Printf("Failed to load security: %v", err)
        return []security.ColumnSecurity{}, nil // No rules = no restrictions
    }
    return rules, nil
}
```

---

## Login/Logout Endpoints

```go
func SetupAuthRoutes(router *mux.Router, securityList *security.SecurityList) {
    // Login
    router.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
        var req security.LoginRequest
        json.NewDecoder(r.Body).Decode(&req)

        resp, err := securityList.Provider().Login(r.Context(), req)
        if err != nil {
            http.Error(w, err.Error(), http.StatusUnauthorized)
            return
        }

        json.NewEncoder(w).Encode(resp)
    }).Methods("POST")

    // Logout
    router.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        userID, _ := security.GetUserID(r.Context())

        err := securityList.Provider().Logout(r.Context(), security.LogoutRequest{
            Token:  token,
            UserID: userID,
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        w.WriteHeader(http.StatusOK)
    }).Methods("POST")
}
```

---

## Debugging

```go
// Enable debug logging
import "github.com/bitechdev/GoCore/pkg/cfg"
cfg.SetLogLevel("DEBUG")

// Log in provider methods
func (a *MyAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
    token := r.Header.Get("Authorization")
    log.Printf("Auth: token=%s", token)
    // ...
}

// Check if methods are called
func (p *MyColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    log.Printf("Loading column security: user=%d, schema=%s, table=%s", userID, schema, table)
    // ...
}
```

---

## Complete Minimal Example

```go
package main

import (
    "context"
    "fmt"
    "net/http"
    "strconv"
    "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
    "github.com/bitechdev/ResolveSpec/pkg/security"
    "github.com/gorilla/mux"
)

// Simple all-in-one provider
type SimpleProvider struct{}

func (p *SimpleProvider) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
    return nil, fmt.Errorf("not implemented")
}

func (p *SimpleProvider) Logout(ctx context.Context, req security.LogoutRequest) error {
    return nil
}

func (p *SimpleProvider) Authenticate(r *http.Request) (*security.UserContext, error) {
    id, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
    return &security.UserContext{UserID: id}, nil
}

func (p *SimpleProvider) GetColumnSecurity(ctx context.Context, u int, s, t string) ([]security.ColumnSecurity, error) {
    return []security.ColumnSecurity{}, nil
}

func (p *SimpleProvider) GetRowSecurity(ctx context.Context, u int, s, t string) (security.RowSecurity, error) {
    return security.RowSecurity{Template: fmt.Sprintf("user_id = %d", u)}, nil
}

func main() {
    handler := restheadspec.NewHandlerWithGORM(db)

    // Setup security
    provider := &SimpleProvider{}
    securityList := security.SetupSecurityProvider(handler, provider)

    // Apply middleware
    router := mux.NewRouter()
    restheadspec.SetupMuxRoutes(router, handler)
    router.Use(security.NewAuthMiddleware(securityList))
    router.Use(security.SetSecurityMiddleware(securityList))

    http.ListenAndServe(":8080", router)
}
```

---

## Context Helpers

```go
// Get full user context
userCtx, ok := security.GetUserContext(ctx)

// Get individual fields
userID, ok := security.GetUserID(ctx)
userName, ok := security.GetUserName(ctx)
userLevel, ok := security.GetUserLevel(ctx)
sessionID, ok := security.GetSessionID(ctx)
remoteID, ok := security.GetRemoteID(ctx)
roles, ok := security.GetUserRoles(ctx)
email, ok := security.GetUserEmail(ctx)
```

---

## Resources

| File | Description |
|------|-------------|
| `INTERFACE_GUIDE.md` | **Start here** - Complete implementation guide |
| `examples.go` | Working provider implementations to copy |
| `setup_example.go` | 6 complete integration examples |
| `README.md` | Architecture overview and migration guide |

---

## Cheat Sheet

```go
// ===== REQUIRED SETUP =====
auth := security.NewJWTAuthenticator("secret", db)
colSec := security.NewDatabaseColumnSecurityProvider(db)
rowSec := security.NewDatabaseRowSecurityProvider(db)
provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
securityList := security.SetupSecurityProvider(handler, provider)

// ===== INTERFACE METHODS =====
Authenticate(r *http.Request) (*UserContext, error)
Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)
Logout(ctx context.Context, req LogoutRequest) error
GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error)
GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error)

// ===== QUICK EXAMPLES =====
// Header auth
&UserContext{UserID: 123, UserName: "john"}

// Mask SSN
{Path: []string{"ssn"}, Accesstype: "mask", MaskStart: 5}

// User isolation
{Template: "user_id = {UserID}"}
```
