# ResolveSpec Security Provider

Type-safe, composable security system for ResolveSpec with support for authentication, column-level security (masking), and row-level security (filtering).

## Features

- ✅ **Interface-Based** - Type-safe providers instead of callbacks
- ✅ **Login/Logout Support** - Built-in authentication lifecycle
- ✅ **Two-Factor Authentication (2FA)** - Optional TOTP support for enhanced security
- ✅ **Composable** - Mix and match different providers
- ✅ **No Global State** - Each handler has its own security configuration
- ✅ **Testable** - Easy to mock and test
- ✅ **Extensible** - Implement custom providers for your needs
- ✅ **Stored Procedures** - All database operations use PostgreSQL stored procedures for security and maintainability

## Stored Procedure Architecture

**All database-backed security providers use PostgreSQL stored procedures exclusively.** No raw SQL queries are executed from Go code.

### Benefits

- **Security**: Database logic is centralized and protected
- **Maintainability**: Update database logic without recompiling Go code
- **Performance**: Stored procedures are pre-compiled and optimized
- **Testability**: Test database logic independently
- **Consistency**: Standardized `resolvespec_*` naming convention

### Available Stored Procedures

| Procedure | Purpose | Used By |
|-----------|---------|---------|
| `resolvespec_login` | Session-based login | DatabaseAuthenticator |
| `resolvespec_logout` | Session invalidation | DatabaseAuthenticator |
| `resolvespec_session` | Session validation | DatabaseAuthenticator |
| `resolvespec_session_update` | Update session activity | DatabaseAuthenticator |
| `resolvespec_refresh_token` | Token refresh | DatabaseAuthenticator |
| `resolvespec_jwt_login` | JWT user validation | JWTAuthenticator |
| `resolvespec_jwt_logout` | JWT token blacklist | JWTAuthenticator |
| `resolvespec_column_security` | Load column rules | DatabaseColumnSecurityProvider |
| `resolvespec_row_security` | Load row templates | DatabaseRowSecurityProvider |

See `database_schema.sql` for complete stored procedure definitions and examples.

## Quick Start

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/security"
    "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

// 1. Create security providers
auth := security.NewJWTAuthenticator("your-secret-key", db)
colSec := security.NewDatabaseColumnSecurityProvider(db)
rowSec := security.NewDatabaseRowSecurityProvider(db)

// 2. Combine providers
provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)

// 3. Create handler and register security hooks
handler := restheadspec.NewHandlerWithGORM(db)
securityList := security.NewSecurityList(provider)
restheadspec.RegisterSecurityHooks(handler, securityList)

// 4. Apply middleware
router := mux.NewRouter()
restheadspec.SetupMuxRoutes(router, handler)
router.Use(security.NewAuthMiddleware(securityList))
router.Use(security.SetSecurityMiddleware(securityList))
```

## Architecture

### Spec-Agnostic Design

The security system is **completely spec-agnostic** - it doesn't depend on any specific spec implementation. Instead, each spec (restheadspec, funcspec, resolvespec) implements its own security integration by adapting to the `SecurityContext` interface.

```
┌─────────────────────────────────────┐
│     Security Package (Generic)      │
│  - SecurityContext interface        │
│  - Security providers                │
│  - Core security logic               │
└─────────────────────────────────────┘
           ▲          ▲          ▲
           │          │          │
    ┌──────┘          │          └──────┐
    │                 │                 │
┌───▼────┐      ┌────▼─────┐     ┌────▼──────┐
│RestHead│      │ FuncSpec │     │ResolveSpec│
│  Spec  │      │          │     │           │
│        │      │          │     │           │
│Adapts  │      │ Adapts   │     │  Adapts   │
│to      │      │ to       │     │  to       │
│Security│      │ Security │     │  Security │
│Context │      │ Context  │     │  Context  │
└────────┘      └──────────┘     └───────────┘
```

**Benefits:**
- ✅ No circular dependencies
- ✅ Each spec can customize security integration
- ✅ Easy to add new specs
- ✅ Security logic is reusable across all specs

### Core Interfaces

The security system is built on three main interfaces:

#### 1. Authenticator
Handles user authentication lifecycle:

```go
type Authenticator interface {
    Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)
    Logout(ctx context.Context, req LogoutRequest) error
    Authenticate(r *http.Request) (*UserContext, error)
}
```

#### 2. ColumnSecurityProvider
Manages column-level security (masking/hiding):

```go
type ColumnSecurityProvider interface {
    GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error)
}
```

#### 3. RowSecurityProvider
Manages row-level security (WHERE clause filtering):

```go
type RowSecurityProvider interface {
    GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error)
}
```

### SecurityProvider
The main interface that combines all three:

```go
type SecurityProvider interface {
    Authenticator
    ColumnSecurityProvider
    RowSecurityProvider
}
```

#### 4. SecurityContext (Spec Integration Interface)
Each spec implements this interface to integrate with the security system:

```go
type SecurityContext interface {
    GetContext() context.Context
    GetUserID() (int, bool)
    GetSchema() string
    GetEntity() string
    GetModel() interface{}
    GetQuery() interface{}
    SetQuery(interface{})
    GetResult() interface{}
    SetResult(interface{})
}
```

**Implementation Examples:**
- `restheadspec`: Adapts `restheadspec.HookContext` → `SecurityContext`
- `funcspec`: Adapts `funcspec.HookContext` → `SecurityContext`
- `resolvespec`: Adapts `resolvespec.HookContext` → `SecurityContext`

### UserContext
Enhanced user context with complete user information:

```go
type UserContext struct {
    UserID    int            // User's unique ID
    UserName  string         // Username
    UserLevel int            // User privilege level
    SessionID string         // Current session ID
    RemoteID  string         // Remote system ID
    Roles     []string       // User roles
    Email     string         // User email
    Claims    map[string]any // Additional authentication claims
    Meta      map[string]any // Additional metadata (can hold any JSON-serializable values)
}
```

## Available Implementations

### Authenticators

**HeaderAuthenticator** - Simple header-based authentication:
```go
auth := security.NewHeaderAuthenticator()
// Expects: X-User-ID, X-User-Name, X-User-Level, etc.
```

**DatabaseAuthenticator** - Database session-based authentication (Recommended):
```go
auth := security.NewDatabaseAuthenticator(db)
// Supports: Login, Logout, Session management, Token refresh
// All operations use stored procedures: resolvespec_login, resolvespec_logout,
// resolvespec_session, resolvespec_session_update, resolvespec_refresh_token
// Requires: users and user_sessions tables + stored procedures (see database_schema.sql)
```

**JWTAuthenticator** - JWT token authentication with login/logout:
```go
auth := security.NewJWTAuthenticator("secret-key", db)
// Supports: Login, Logout, JWT token validation
// All operations use stored procedures: resolvespec_jwt_login, resolvespec_jwt_logout
// Note: Requires JWT library installation for token signing/verification
```

**TwoFactorAuthenticator** - Wraps any authenticator with TOTP 2FA:
```go
baseAuth := security.NewDatabaseAuthenticator(db)

// Use in-memory provider (for testing)
tfaProvider := security.NewMemoryTwoFactorProvider(nil)

// Or use database provider (for production)
tfaProvider := security.NewDatabaseTwoFactorProvider(db, nil)
// Requires: users table with totp fields, user_totp_backup_codes table
// Requires: resolvespec_totp_* stored procedures (see totp_database_schema.sql)

auth := security.NewTwoFactorAuthenticator(baseAuth, tfaProvider, nil)
// Supports: TOTP codes, backup codes, QR code generation
// Compatible with Google Authenticator, Microsoft Authenticator, Authy, etc.
```

### Column Security Providers

**DatabaseColumnSecurityProvider** - Loads rules from database:
```go
colSec := security.NewDatabaseColumnSecurityProvider(db)
// Uses stored procedure: resolvespec_column_security
// Queries core.secaccess and core.hub_link tables
```

**ConfigColumnSecurityProvider** - Static configuration:
```go
rules := map[string][]security.ColumnSecurity{
    "public.employees": {
        {Path: []string{"ssn"}, Accesstype: "mask", MaskStart: 5},
    },
}
colSec := security.NewConfigColumnSecurityProvider(rules)
```

### Row Security Providers

**DatabaseRowSecurityProvider** - Loads filters from database:
```go
rowSec := security.NewDatabaseRowSecurityProvider(db)
// Uses stored procedure: resolvespec_row_security
```

**ConfigRowSecurityProvider** - Static templates:
```go
templates := map[string]string{
    "public.orders": "user_id = {UserID}",
}
blocked := map[string]bool{
    "public.admin_logs": true,
}
rowSec := security.NewConfigRowSecurityProvider(templates, blocked)
```

## Usage Examples

### Example 1: Complete Database-Backed Security with Sessions (restheadspec)

```go
func main() {
    db := setupDatabase()

    // Run migrations (see database_schema.sql)
    // db.Exec("CREATE TABLE users ...")
    // db.Exec("CREATE TABLE user_sessions ...")

    // Create handler
    handler := restheadspec.NewHandlerWithGORM(db)

    // Create security providers
    auth := security.NewDatabaseAuthenticator(db) // Session-based auth
    colSec := security.NewDatabaseColumnSecurityProvider(db)
    rowSec := security.NewDatabaseRowSecurityProvider(db)

    // Combine providers
    provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
    securityList := security.NewSecurityList(provider)

    // Register security hooks for this spec
    restheadspec.RegisterSecurityHooks(handler, securityList)

    // Setup routes
    router := mux.NewRouter()

    // Add auth endpoints
    router.HandleFunc("/auth/login", handleLogin(securityList)).Methods("POST")
    router.HandleFunc("/auth/logout", handleLogout(securityList)).Methods("POST")
    router.HandleFunc("/auth/refresh", handleRefresh(securityList)).Methods("POST")

    // Setup API with security
    apiRouter := router.PathPrefix("/api").Subrouter()
    restheadspec.SetupMuxRoutes(apiRouter, handler)
    apiRouter.Use(security.NewAuthMiddleware(securityList))
    apiRouter.Use(security.SetSecurityMiddleware(securityList))

    http.ListenAndServe(":8080", router)
}

func handleLogin(securityList *security.SecurityList) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req security.LoginRequest
        json.NewDecoder(r.Body).Decode(&req)

        // Add client info to claims
        req.Claims = map[string]any{
            "ip_address": r.RemoteAddr,
            "user_agent": r.UserAgent(),
        }

        resp, err := securityList.Provider().Login(r.Context(), req)
        if err != nil {
            http.Error(w, err.Error(), http.StatusUnauthorized)
            return
        }

        // Set session cookie (optional)
        http.SetCookie(w, &http.Cookie{
            Name:     "session_token",
            Value:    resp.Token,
            Expires:  time.Now().Add(24 * time.Hour),
            HttpOnly: true,
            Secure:   true, // Use in production with HTTPS
            SameSite: http.SameSiteStrictMode,
        })

        json.NewEncoder(w).Encode(resp)
    }
}

func handleRefresh(securityList *security.SecurityList) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("X-Refresh-Token")

        if refreshable, ok := securityList.Provider().(security.Refreshable); ok {
            resp, err := refreshable.RefreshToken(r.Context(), token)
            if err != nil {
                http.Error(w, err.Error(), http.StatusUnauthorized)
                return
}
```

## Two-Factor Authentication (2FA)

### Overview

- **Optional per-user** - Enable/disable 2FA individually
- **TOTP standard** - Compatible with Google Authenticator, Microsoft Authenticator, Authy, 1Password, etc.
- **Configurable** - SHA1/SHA256/SHA512, 6/8 digits, custom time periods
- **Backup codes** - One-time recovery codes with secure hashing
- **Clock skew** - Handles time differences between client/server

### Setup

```go
// 1. Wrap existing authenticator with 2FA support
baseAuth := security.NewDatabaseAuthenticator(db)
tfaProvider := security.NewMemoryTwoFactorProvider(nil) // Use custom DB implementation in production
tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, tfaProvider, nil)

// 2. Use as normal authenticator
provider := security.NewCompositeSecurityProvider(tfaAuth, colSec, rowSec)
securityList := security.NewSecurityList(provider)
```

### Enable 2FA for User

```go
// 1. Initiate 2FA setup
secret, err := tfaAuth.Setup2FA(userID, "MyApp", "user@example.com")
// Returns: secret.Secret, secret.QRCodeURL, secret.BackupCodes

// 2. User scans QR code with authenticator app
// Display secret.QRCodeURL as QR code image

// 3. User enters verification code from app
code := "123456" // From authenticator app
err = tfaAuth.Enable2FA(userID, secret.Secret, code)
// 2FA is now enabled for this user

// 4. Store backup codes securely and show to user once
// Display: secret.BackupCodes (10 codes)
```

### Login Flow with 2FA

```go
// 1. User provides credentials
req := security.LoginRequest{
    Username: "user@example.com",
    Password: "password",
}

resp, err := tfaAuth.Login(ctx, req)

// 2. Check if 2FA required
if resp.Requires2FA {
    // Prompt user for 2FA code
    code := getUserInput() // From authenticator app or backup code
    
    // 3. Login again with 2FA code
    req.TwoFactorCode = code
    resp, err = tfaAuth.Login(ctx, req)
    
    // 4. Success - token is returned
    token := resp.Token
}
```

### Manage 2FA

```go
// Disable 2FA
err := tfaAuth.Disable2FA(userID)

// Regenerate backup codes
newCodes, err := tfaAuth.RegenerateBackupCodes(userID, 10)

// Check status
has2FA, err := tfaProvider.Get2FAStatus(userID)
```

### Custom 2FA Storage

**Option 1: Use DatabaseTwoFactorProvider (Recommended)**

```go
// Uses PostgreSQL stored procedures for all operations
db := setupDatabase()

// Run migrations from totp_database_schema.sql
// - Add totp_secret, totp_enabled, totp_enabled_at to users table
// - Create user_totp_backup_codes table
// - Create resolvespec_totp_* stored procedures

tfaProvider := security.NewDatabaseTwoFactorProvider(db, nil)
tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, tfaProvider, nil)
```

**Option 2: Implement Custom Provider**

Implement `TwoFactorAuthProvider` for custom storage:

```go
type DBTwoFactorProvider struct {
    db *gorm.DB
}

func (p *DBTwoFactorProvider) Enable2FA(userID int, secret string, backupCodes []string) error {
    // Store secret and hashed backup codes in database
    return p.db.Exec("UPDATE users SET totp_secret = ?, backup_codes = ? WHERE id = ?", 
        secret, hashCodes(backupCodes), userID).Error
}

func (p *DBTwoFactorProvider) Get2FASecret(userID int) (string, error) {
    var secret string
    err := p.db.Raw("SELECT totp_secret FROM users WHERE id = ?", userID).Scan(&secret).Error
    return secret, err
}

// Implement remaining methods: Generate2FASecret, Validate2FACode, Disable2FA,
// Get2FAStatus, GenerateBackupCodes, ValidateBackupCode
```

### Configuration

```go
config := &security.TwoFactorConfig{
    Algorithm:  "SHA256",  // SHA1, SHA256, SHA512
    Digits:     8,         // 6 or 8
    Period:     30,        // Seconds per code
    SkewWindow: 2,         // Accept codes ±2 periods
}

totp := security.NewTOTPGenerator(config)
tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, tfaProvider, config)
```

### API Response Structure

```go
// LoginResponse with 2FA
type LoginResponse struct {
    Token              string              `json:"token"`
    Requires2FA        bool                `json:"requires_2fa"`
    TwoFactorSetupData *TwoFactorSecret    `json:"two_factor_setup,omitempty"`
    User               *UserContext        `json:"user"`
}

// TwoFactorSecret for setup
type TwoFactorSecret struct {
    Secret      string   `json:"secret"`         // Base32 encoded
    QRCodeURL   string   `json:"qr_code_url"`    // otpauth://totp/...
    BackupCodes []string `json:"backup_codes"`   // 10 recovery codes
}

// UserContext includes 2FA status
type UserContext struct {
    UserID           int    `json:"user_id"`
    TwoFactorEnabled bool   `json:"two_factor_enabled"`
    // ... other fields
}
```

### Security Best Practices

- **Store secrets encrypted** - Never store TOTP secrets in plain text
- **Hash backup codes** - Use SHA-256 before storing
- **Rate limit** - Limit 2FA verification attempts
- **Require password** - Always verify password before disabling 2FA
- **Show backup codes once** - Display only during setup/regeneration
- **Log 2FA events** - Track enable/disable/failed attempts
- **Mark codes as used** - Backup codes are single-use only


            json.NewEncoder(w).Encode(resp)
        } else {
            http.Error(w, "Refresh not supported", http.StatusNotImplemented)
        }
    }
}
```

### Example 2: Config-Based Security (No Database)

```go
func main() {
    db := setupDatabase()
    handler := restheadspec.NewHandlerWithGORM(db)

    // Static column security rules
    columnRules := map[string][]security.ColumnSecurity{
        "public.employees": {
            {Path: []string{"ssn"}, Accesstype: "mask", MaskStart: 5},
            {Path: []string{"salary"}, Accesstype: "hide"},
        },
    }

    // Static row security templates
    rowTemplates := map[string]string{
        "public.orders": "user_id = {UserID}",
    }

    // Create providers
    auth := security.NewHeaderAuthenticator()
    colSec := security.NewConfigColumnSecurityProvider(columnRules)
    rowSec := security.NewConfigRowSecurityProvider(rowTemplates, nil)

    // Combine providers and register hooks
    provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
    securityList := security.NewSecurityList(provider)
    restheadspec.RegisterSecurityHooks(handler, securityList)

    // Setup routes...
}
```

### Example 3: FuncSpec Security (SQL Query API)

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/funcspec"
    "github.com/bitechdev/ResolveSpec/pkg/security"
)

func main() {
    db := setupDatabase()

    // Create funcspec handler
    handler := funcspec.NewHandler(db)

    // Create security providers
    auth := security.NewJWTAuthenticator("secret-key", db)
    colSec := security.NewDatabaseColumnSecurityProvider(db)
    rowSec := security.NewDatabaseRowSecurityProvider(db)

    // Combine providers
    provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
    securityList := security.NewSecurityList(provider)

    // Register security hooks (audit logging)
    funcspec.RegisterSecurityHooks(handler, securityList)

    // Note: funcspec operates on raw SQL queries, so row/column
    // security is limited. Security should be enforced at the
    // SQL function level or via database policies.

    // Setup routes...
}
```

### Example 4: ResolveSpec Security (REST API)

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/resolvespec"
    "github.com/bitechdev/ResolveSpec/pkg/security"
)

func main() {
    db := setupDatabase()
    registry := common.NewModelRegistry()

    // Register models
    registry.RegisterModel("public.users", &User{})
    registry.RegisterModel("public.orders", &Order{})

    // Create resolvespec handler
    handler := resolvespec.NewHandler(db, registry)

    // Create security providers
    auth := security.NewDatabaseAuthenticator(db)
    colSec := security.NewDatabaseColumnSecurityProvider(db)
    rowSec := security.NewDatabaseRowSecurityProvider(db)

    // Combine providers
    provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
    securityList := security.NewSecurityList(provider)

    // Register security hooks for resolvespec
    resolvespec.RegisterSecurityHooks(handler, securityList)

    // Setup routes...
}
```

### Example 5: Custom Provider

Implement your own provider for complete control:

```go
type MySecurityProvider struct {
    db *gorm.DB
}

func (p *MySecurityProvider) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
    // Your custom login logic
}

func (p *MySecurityProvider) Logout(ctx context.Context, req security.LogoutRequest) error {
    // Your custom logout logic
}

func (p *MySecurityProvider) Authenticate(r *http.Request) (*security.UserContext, error) {
    // Your custom authentication logic
}

func (p *MySecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    // Your custom column security logic
}

func (p *MySecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (security.RowSecurity, error) {
    // Your custom row security logic
}

// Use it with any spec
provider := &MySecurityProvider{db: db}
securityList := security.NewSecurityList(provider)

// Register with restheadspec
restheadspec.RegisterSecurityHooks(restHandler, securityList)

// Or with funcspec
funcspec.RegisterSecurityHooks(funcHandler, securityList)

// Or with resolvespec
resolvespec.RegisterSecurityHooks(resolveHandler, securityList)
```

## Security Features

### Column Security (Masking/Hiding)

**Mask SSN (show last 4 digits):**
```go
{
    Path:       []string{"ssn"},
    Accesstype: "mask",
    MaskStart:  5,
    MaskChar:   "*",
}
// "123-45-6789" → "*****6789"
```

**Hide entire field:**
```go
{
    Path:       []string{"salary"},
    Accesstype: "hide",
}
// Field returns 0 or empty
```

**Nested JSON field masking:**
```go
{
    Path:       []string{"address", "street"},
    Accesstype: "mask",
    MaskStart:  10,
}
```

### Row Security (Filtering)

**User isolation:**
```go
{
    Template: "user_id = {UserID}",
}
// Users only see their own records
```

**Tenant isolation:**
```go
{
    Template: "tenant_id = {TenantID} AND user_id = {UserID}",
}
```

**Block all access:**
```go
{
    HasBlock: true,
}
// Completely blocks access to the table
```

**Template variables:**
- `{UserID}` - Current user's ID
- `{PrimaryKeyName}` - Primary key column
- `{TableName}` - Table name
- `{SchemaName}` - Schema name

## Request Flow

```
HTTP Request
    ↓
NewAuthMiddleware (security package)
    ├─ Calls provider.Authenticate(request)
    └─ Adds UserContext to context
    ↓
SetSecurityMiddleware (security package)
    └─ Adds SecurityList to context
    ↓
Spec Handler (restheadspec/funcspec/resolvespec)
    ↓
BeforeRead Hook (registered by spec)
    ├─ Adapts spec's HookContext → SecurityContext
    ├─ Calls security.LoadSecurityRules(secCtx, securityList)
    │   ├─ Calls provider.GetColumnSecurity()
    │   └─ Calls provider.GetRowSecurity()
    └─ Caches security rules
    ↓
BeforeScan Hook (registered by spec)
    ├─ Adapts spec's HookContext → SecurityContext
    ├─ Calls security.ApplyRowSecurity(secCtx, securityList)
    └─ Applies row security (adds WHERE clause to query)
    ↓
Database Query (with security filters)
    ↓
AfterRead Hook (registered by spec)
    ├─ Adapts spec's HookContext → SecurityContext
    ├─ Calls security.ApplyColumnSecurity(secCtx, securityList)
    ├─ Applies column security (masks/hides fields)
    └─ Calls security.LogDataAccess(secCtx)
    ↓
HTTP Response (secured data)
```

**Key Points:**
- Security package is spec-agnostic and provides core logic
- Each spec registers its own hooks that adapt to SecurityContext
- Security rules are loaded once and cached for the request
- Row security is applied to the query (database level)
- Column security is applied to results (application level)

## Testing

The interface-based design makes testing straightforward:

```go
// Mock authenticator for tests
type MockAuthenticator struct {
    UserToReturn *security.UserContext
    ErrorToReturn error
}

func (m *MockAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
    return m.UserToReturn, m.ErrorToReturn
}

// Use in tests
func TestMyHandler(t *testing.T) {
    mockAuth := &MockAuthenticator{
        UserToReturn: &security.UserContext{UserID: 123},
    }

    provider := security.NewCompositeSecurityProvider(
        mockAuth,
        &MockColumnSecurity{},
        &MockRowSecurity{},
    )

    securityList := security.SetupSecurityProvider(handler, provider)
    // ... test your handler
}
```

## Migration Guide

### From Old Callback System

If you're upgrading from the old callback-based system:

**Old:**
```go
security.GlobalSecurity.AuthenticateCallback = myAuthFunc
security.GlobalSecurity.LoadColumnSecurityCallback = myColSecFunc
security.GlobalSecurity.LoadRowSecurityCallback = myRowSecFunc
security.SetupSecurityProvider(handler, &security.GlobalSecurity)
```

**New:**
```go
// 1. Wrap your functions in a provider
type MyProvider struct{}

func (p *MyProvider) Authenticate(r *http.Request) (*security.UserContext, error) {
    userID, roles, err := myAuthFunc(r)
    return &security.UserContext{UserID: userID, Roles: strings.Split(roles, ",")}, err
}

func (p *MyProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    return myColSecFunc(userID, schema, table)
}

func (p *MyProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (security.RowSecurity, error) {
    return myRowSecFunc(userID, schema, table)
}

func (p *MyProvider) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
    return nil, fmt.Errorf("not implemented")
}

func (p *MyProvider) Logout(ctx context.Context, req security.LogoutRequest) error {
    return nil
}

// 2. Create security list and register hooks
provider := &MyProvider{}
securityList := security.NewSecurityList(provider)

// 3. Register with your spec
restheadspec.RegisterSecurityHooks(handler, securityList)
```

### From Old SetupSecurityProvider API

If you're upgrading from the previous interface-based system:

**Old:**
```go
securityList := security.SetupSecurityProvider(handler, provider)
```

**New:**
```go
securityList := security.NewSecurityList(provider)
restheadspec.RegisterSecurityHooks(handler, securityList) // or funcspec/resolvespec
```

The main changes:
1. Security package no longer knows about specific spec types
2. Each spec registers its own security hooks
3. More flexible - same security provider works with all specs

## Documentation

| File | Description |
|------|-------------|
| **QUICK_REFERENCE.md** | Quick reference guide with examples |
| **INTERFACE_GUIDE.md** | Complete implementation guide |
| **examples.go** | Working provider implementations |
| **setup_example.go** | 6 complete integration examples |

## API Reference

### Context Helpers

Get user information from request context:

```go
userCtx, ok := security.GetUserContext(ctx)
userID, ok := security.GetUserID(ctx)
userName, ok := security.GetUserName(ctx)
userLevel, ok := security.GetUserLevel(ctx)
sessionID, ok := security.GetSessionID(ctx)
remoteID, ok := security.GetRemoteID(ctx)
roles, ok := security.GetUserRoles(ctx)
email, ok := security.GetUserEmail(ctx)
```

### Optional Interfaces

Implement these for additional features:

**Refreshable** - Token refresh support:
```go
type Refreshable interface {
    RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error)
}
```

**Validatable** - Token validation:
```go
type Validatable interface {
    ValidateToken(ctx context.Context, token string) (bool, error)
}
```

**Cacheable** - Cache management:
```go
type Cacheable interface {
    ClearCache(ctx context.Context, userID int, schema, table string) error
}
```

## Benefits Over Callbacks

| Feature | Old (Callbacks) | New (Interfaces) |
|---------|----------------|------------------|
| Type Safety | ❌ Callbacks can be nil | ✅ Compile-time verification |
| Global State | ❌ GlobalSecurity variable | ✅ Dependency injection |
| Testability | ⚠️ Need to set globals | ✅ Easy to mock |
| Composability | ❌ Single provider only | ✅ Mix and match |
| Login/Logout | ❌ Not supported | ✅ Built-in |
| Extensibility | ⚠️ Limited | ✅ Optional interfaces |

## Common Patterns

### Caching Security Rules

```go
type CachedProvider struct {
    inner security.ColumnSecurityProvider
    cache *cache.Cache
}

func (p *CachedProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
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

### Role-Based Security

```go
func (p *MyProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]security.ColumnSecurity, error) {
    userCtx, _ := security.GetUserContext(ctx)

    if contains(userCtx.Roles, "admin") {
        return []security.ColumnSecurity{}, nil // No restrictions
    }

    return loadRestrictionsForUser(userID, schema, table), nil
}
```

### Multi-Tenant Isolation

```go
func (p *MyProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (security.RowSecurity, error) {
    tenantID := getUserTenant(userID)

    return security.RowSecurity{
        Template: fmt.Sprintf("tenant_id = %d AND user_id = {UserID}", tenantID),
    }, nil
}
```

## Middleware and Handler API

### NewAuthMiddleware
Standard middleware that authenticates all requests:

```go
router.Use(security.NewAuthMiddleware(securityList))
```

Routes can skip authentication using the `SkipAuth` helper:

```go
func PublicHandler(w http.ResponseWriter, r *http.Request) {
    ctx := security.SkipAuth(r.Context())
    // This route will bypass authentication
    // A guest user context will be set instead
}

router.Handle("/public", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    ctx := security.SkipAuth(r.Context())
    PublicHandler(w, r.WithContext(ctx))
}))
```

When authentication is skipped, a guest user context is automatically set:
- UserID: 0
- UserName: "guest"
- Roles: ["guest"]
- RemoteID: Request's remote address

Routes can use optional authentication with the `OptionalAuth` helper:

```go
func OptionalAuthHandler(w http.ResponseWriter, r *http.Request) {
    ctx := security.OptionalAuth(r.Context())
    r = r.WithContext(ctx)

    // This route will try to authenticate
    // If authentication succeeds, authenticated user context is set
    // If authentication fails, guest user context is set instead

    userCtx, _ := security.GetUserContext(r.Context())
    if userCtx.UserID == 0 {
        // Guest user
        fmt.Fprintf(w, "Welcome, guest!")
    } else {
        // Authenticated user
        fmt.Fprintf(w, "Welcome back, %s!", userCtx.UserName)
    }
}

router.Handle("/home", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    ctx := security.OptionalAuth(r.Context())
    OptionalAuthHandler(w, r.WithContext(ctx))
}))
```

**Authentication Modes Summary:**
- **Required (default)**: Authentication must succeed or returns 401
- **SkipAuth**: Bypasses authentication entirely, always sets guest context
- **OptionalAuth**: Tries authentication, falls back to guest context if it fails

### NewAuthHandler

Standalone authentication handler (without middleware wrapping):

```go
// Use when you need authentication logic without middleware
authHandler := security.NewAuthHandler(securityList, myHandler)
http.Handle("/api/protected", authHandler)
```

### NewOptionalAuthHandler

Standalone optional authentication handler that tries to authenticate but falls back to guest:

```go
// Use for routes that should work for both authenticated and guest users
optionalHandler := security.NewOptionalAuthHandler(securityList, myHandler)
http.Handle("/home", optionalHandler)

// Example handler that checks user context
func myHandler(w http.ResponseWriter, r *http.Request) {
    userCtx, _ := security.GetUserContext(r.Context())
    if userCtx.UserID == 0 {
        fmt.Fprintf(w, "Welcome, guest!")
    } else {
        fmt.Fprintf(w, "Welcome back, %s!", userCtx.UserName)
    }
}
```

### Helper Functions

Extract user information from context:

```go
// Get full user context
userCtx, ok := security.GetUserContext(ctx)

// Get specific fields
userID, ok := security.GetUserID(ctx)
userName, ok := security.GetUserName(ctx)
userLevel, ok := security.GetUserLevel(ctx)
sessionID, ok := security.GetSessionID(ctx)
remoteID, ok := security.GetRemoteID(ctx)
roles, ok := security.GetUserRoles(ctx)
email, ok := security.GetUserEmail(ctx)
meta, ok := security.GetUserMeta(ctx)
```

### Metadata Support

The `Meta` field in `UserContext` can hold any JSON-serializable values:

```go
// Set metadata during login
loginReq := security.LoginRequest{
    Username: "user@example.com",
    Password: "password",
    Meta: map[string]any{
        "department": "engineering",
        "location": "US",
        "preferences": map[string]any{
            "theme": "dark",
        },
    },
}

// Access metadata in handlers
meta, ok := security.GetUserMeta(ctx)
if ok {
    department := meta["department"].(string)
}
```

## License

Part of the ResolveSpec project.
