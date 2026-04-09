# resolvemcp

Package `resolvemcp` exposes registered database models as **Model Context Protocol (MCP) tools and resources** over HTTP/SSE transport. It mirrors the `resolvespec` package patterns — same model registration API, same filter/sort/pagination/preload options, same lifecycle hook system.

## Quick Start

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/resolvemcp"
    "github.com/gorilla/mux"
)

// 1. Create a handler
handler := resolvemcp.NewHandlerWithGORM(db, resolvemcp.Config{
    BaseURL: "http://localhost:8080",
})

// 2. Register models
handler.RegisterModel("public", "users", &User{})
handler.RegisterModel("public", "orders", &Order{})

// 3. Mount routes
r := mux.NewRouter()
resolvemcp.SetupMuxRoutes(r, handler)
```

---

## Config

```go
type Config struct {
    // BaseURL is the public-facing base URL of the server (e.g. "http://localhost:8080").
    // Sent to MCP clients during the SSE handshake so they know where to POST messages.
    // If empty, it is detected from each incoming request using the Host header and
    // TLS state (X-Forwarded-Proto is honoured for reverse-proxy deployments).
    BaseURL string

    // BasePath is the URL path prefix where MCP endpoints are mounted (e.g. "/mcp").
    // Required.
    BasePath string
}
```

## Handler Creation

| Function | Description |
|---|---|
| `NewHandlerWithGORM(db *gorm.DB, cfg Config) *Handler` | Backed by GORM |
| `NewHandlerWithBun(db *bun.DB, cfg Config) *Handler` | Backed by Bun |
| `NewHandlerWithDB(db common.Database, cfg Config) *Handler` | Backed by any `common.Database` |
| `NewHandler(db common.Database, registry common.ModelRegistry, cfg Config) *Handler` | Full control over registry |

---

## Registering Models

```go
handler.RegisterModel(schema, entity string, model interface{}) error
```

- `schema` — database schema name (e.g. `"public"`), or empty string for no schema prefix.
- `entity` — table/entity name (e.g. `"users"`).
- `model` — a pointer to a struct (e.g. `&User{}`).

Each call immediately creates four MCP **tools** and one MCP **resource** for the model.

---

## HTTP Transports

`Config.BasePath` is required and used for all route registration.
`Config.BaseURL` is optional — when empty it is detected from each request.

Two transports are supported: **SSE** (legacy, two-endpoint) and **Streamable HTTP** (recommended, single-endpoint).

---

### SSE Transport

Two endpoints: `GET {BasePath}/sse` (subscribe) + `POST {BasePath}/message` (send).

#### Gorilla Mux

```go
resolvemcp.SetupMuxRoutes(r, handler)
```

| Route | Method | Description |
|---|---|---|
| `{BasePath}/sse` | GET | SSE connection — clients subscribe here |
| `{BasePath}/message` | POST | JSON-RPC — clients send requests here |

#### bunrouter

```go
resolvemcp.SetupBunRouterRoutes(router, handler)
```

#### Gin / net/http / Echo

```go
sse := handler.SSEServer()

engine.Any("/mcp/*path", gin.WrapH(sse))  // Gin
http.Handle("/mcp/", sse)                  // net/http
e.Any("/mcp/*", echo.WrapHandler(sse))     // Echo
```

---

### Streamable HTTP Transport

Single endpoint at `{BasePath}`. Handles POST (client→server) and GET (server→client streaming). Preferred for new integrations.

#### Gorilla Mux

```go
resolvemcp.SetupMuxStreamableHTTPRoutes(r, handler)
```

Mounts the handler at `{BasePath}` (all methods).

#### bunrouter

```go
resolvemcp.SetupBunRouterStreamableHTTPRoutes(router, handler)
```

Registers GET, POST, DELETE on `{BasePath}`.

#### Gin / net/http / Echo

```go
h := handler.StreamableHTTPServer()
// or: h := resolvemcp.NewStreamableHTTPHandler(handler)

engine.Any("/mcp", gin.WrapH(h))      // Gin
http.Handle("/mcp", h)                 // net/http
e.Any("/mcp", echo.WrapHandler(h))     // Echo
```

---

## OAuth2 Authentication

`resolvemcp` ships a full **MCP-standard OAuth2 authorization server** (`pkg/security.OAuthServer`) that MCP clients (Claude Desktop, Cursor, etc.) can discover and use automatically.

It can operate as:
- **Its own identity provider** — shows a login form, validates via `DatabaseAuthenticator.Login()`
- **An OAuth2 federation layer** — delegates to external providers (Google, GitHub, Microsoft, etc.)
- **Both simultaneously**

### Standard endpoints served

| Path | Spec | Purpose |
|---|---|---|
| `GET /.well-known/oauth-authorization-server` | RFC 8414 | MCP client auto-discovery |
| `POST /oauth/register` | RFC 7591 | Dynamic client registration |
| `GET /oauth/authorize` | OAuth 2.1 + PKCE | Start login (form or provider redirect) |
| `POST /oauth/authorize` | — | Login form submission |
| `POST /oauth/token` | OAuth 2.1 | Auth code → Bearer token exchange |
| `POST /oauth/token` (refresh) | OAuth 2.1 | Refresh token rotation |
| `GET /oauth/provider/callback` | Internal | External provider redirect target |

MCP clients send `Authorization: Bearer <token>` on all subsequent requests.

---

### Mode 1 — Direct login (server as identity provider)

```go
import "github.com/bitechdev/ResolveSpec/pkg/security"

db, _ := sql.Open("postgres", dsn)
auth := security.NewDatabaseAuthenticator(db)

handler := resolvemcp.NewHandlerWithGORM(gormDB, resolvemcp.Config{
    BaseURL:  "https://api.example.com",
    BasePath: "/mcp",
})

// Enable the OAuth2 server — auth enables the login form
handler.EnableOAuthServer(security.OAuthServerConfig{
    Issuer: "https://api.example.com",
}, auth)

provider, _ := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
securityList, _ := security.NewSecurityList(provider)
security.RegisterSecurityHooks(handler, securityList)

http.ListenAndServe(":8080", handler.HTTPHandler(securityList))
```

MCP client flow:
1. Discovers server at `/.well-known/oauth-authorization-server`
2. Registers itself at `/oauth/register`
3. Redirects user to `/oauth/authorize` → login form appears
4. On submit, exchanges code at `/oauth/token` → receives `Authorization: Bearer` token
5. Uses token on all MCP tool calls

---

### Mode 2 — External provider (Google, GitHub, etc.)

The `RedirectURL` in the provider config must point to `/oauth/provider/callback` on this server.

```go
auth := security.NewDatabaseAuthenticator(db).WithOAuth2(security.OAuth2Config{
    ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
    ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
    RedirectURL:  "https://api.example.com/oauth/provider/callback",
    Scopes:       []string{"openid", "profile", "email"},
    AuthURL:      "https://accounts.google.com/o/oauth2/auth",
    TokenURL:     "https://oauth2.googleapis.com/token",
    UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
    ProviderName: "google",
})

// Pass `auth` so the OAuth server supports persistence, introspection, and revocation.
// Google handles the end-user authentication flow via redirect.
handler.EnableOAuthServer(security.OAuthServerConfig{
    Issuer: "https://api.example.com",
}, auth)
handler.RegisterOAuth2Provider(auth, "google")
```

---

### Mode 3 — Both (login form + external providers)

```go
handler.EnableOAuthServer(security.OAuthServerConfig{
    Issuer:     "https://api.example.com",
    LoginTitle: "My App Login",
}, auth) // auth enables the username/password form

handler.RegisterOAuth2Provider(googleAuth, "google")
handler.RegisterOAuth2Provider(githubAuth, "github")
```

When external providers are registered they take priority; the login form is used as fallback when no providers are configured.

---

### Using `security.OAuthServer` standalone

The authorization server lives in `pkg/security` and can be used with any HTTP framework independently of `resolvemcp`:

```go
oauthSrv := security.NewOAuthServer(security.OAuthServerConfig{
    Issuer: "https://api.example.com",
}, auth)
oauthSrv.RegisterExternalProvider(googleAuth, "google")

mux := http.NewServeMux()
mux.Handle("/", oauthSrv.HTTPHandler())   // mounts all OAuth2 routes
mux.Handle("/mcp/", myMCPHandler)
http.ListenAndServe(":8080", mux)
```

---

### Cookie-based flow (legacy)

For simple setups without full MCP OAuth2 compliance, use the legacy helpers that set a session cookie after external provider login:

```go
resolvemcp.SetupMuxOAuth2Routes(r, auth, resolvemcp.OAuth2RouteConfig{
    ProviderName:       "google",
    LoginPath:          "/auth/google/login",
    CallbackPath:       "/auth/google/callback",
    AfterLoginRedirect: "/",
})
resolvemcp.SetupMuxRoutesWithAuth(r, handler, securityList)
```

---

## Security

`resolvemcp` integrates with the `security` package to provide per-entity access control, row-level security, and column-level security — the same system used by `resolvespec` and `restheadspec`.

### Wiring security hooks

```go
import "github.com/bitechdev/ResolveSpec/pkg/security"

securityList := security.NewSecurityList(mySecurityProvider)
resolvemcp.RegisterSecurityHooks(handler, securityList)
```

Call `RegisterSecurityHooks` **once**, after creating the handler and before registering models. It installs these controls automatically:

| Hook | Effect |
|---|---|
| `BeforeHandle` | Enforces per-entity operation rules (see below) |
| `BeforeRead` | Loads RLS/CLS rules, then injects a user-scoped WHERE clause |
| `AfterRead` | Masks/hides columns per column-security rules; writes audit log |
| `BeforeUpdate` | Blocks update if `CanUpdate` is false |
| `BeforeDelete` | Blocks delete if `CanDelete` is false |

### Per-entity operation rules

Use `RegisterModelWithRules` instead of `RegisterModel` to set access rules at registration time:

```go
import "github.com/bitechdev/ResolveSpec/pkg/modelregistry"

// Read-only entity
handler.RegisterModelWithRules("public", "audit_logs", &AuditLog{}, modelregistry.ModelRules{
    CanRead:   true,
    CanCreate: false,
    CanUpdate: false,
    CanDelete: false,
})

// Public read, authenticated write
handler.RegisterModelWithRules("public", "products", &Product{}, modelregistry.ModelRules{
    CanPublicRead: true,
    CanRead:       true,
    CanCreate:     true,
    CanUpdate:     true,
    CanDelete:     false,
})
```

To update rules for an already-registered model:

```go
handler.SetModelRules("public", "users", modelregistry.ModelRules{
    CanRead:   true,
    CanCreate: true,
    CanUpdate: true,
    CanDelete: false,
})
```

`RegisterModel` (no rules) registers with all-allowed defaults (`CanRead/Create/Update/Delete = true`).

### ModelRules fields

| Field | Default | Description |
|---|---|---|
| `CanPublicRead` | `false` | Allow unauthenticated reads |
| `CanPublicCreate` | `false` | Allow unauthenticated creates |
| `CanPublicUpdate` | `false` | Allow unauthenticated updates |
| `CanPublicDelete` | `false` | Allow unauthenticated deletes |
| `CanRead` | `true` | Allow authenticated reads |
| `CanCreate` | `true` | Allow authenticated creates |
| `CanUpdate` | `true` | Allow authenticated updates |
| `CanDelete` | `true` | Allow authenticated deletes |
| `SecurityDisabled` | `false` | Skip all security checks for this model |

---

## MCP Tools

### Tool Naming

```
{operation}_{schema}_{entity}    // e.g. read_public_users
{operation}_{entity}             // e.g. read_users  (when schema is empty)
```

Operations: `read`, `create`, `update`, `delete`.

### Read Tool — `read_{schema}_{entity}`

Fetch one or many records.

| Argument | Type | Description |
|---|---|---|
| `id` | string | Primary key value. Omit to return multiple records. |
| `limit` | number | Max records per page (recommended: 10–100). |
| `offset` | number | Records to skip (offset-based pagination). |
| `cursor_forward` | string | PK of the **last** record on the current page (next-page cursor). |
| `cursor_backward` | string | PK of the **first** record on the current page (prev-page cursor). |
| `columns` | array | Column names to include. Omit for all columns. |
| `omit_columns` | array | Column names to exclude. |
| `filters` | array | Filter objects (see [Filtering](#filtering)). |
| `sort` | array | Sort objects (see [Sorting](#sorting)). |
| `preloads` | array | Relation preload objects (see [Preloading](#preloading)). |

**Response:**
```json
{
  "success": true,
  "data": [...],
  "metadata": {
    "total": 100,
    "filtered": 100,
    "count": 10,
    "limit": 10,
    "offset": 0
  }
}
```

### Create Tool — `create_{schema}_{entity}`

Insert one or more records.

| Argument | Type | Description |
|---|---|---|
| `data` | object \| array | Single object or array of objects to insert. |

Array input runs inside a single transaction — all succeed or all fail.

**Response:**
```json
{ "success": true, "data": { ... } }
```

### Update Tool — `update_{schema}_{entity}`

Partially update an existing record. Only non-null, non-empty fields in `data` are applied; existing values are preserved for omitted fields.

| Argument | Type | Description |
|---|---|---|
| `id` | string | Primary key of the record. Can also be included inside `data`. |
| `data` | object (required) | Fields to update. |

**Response:**
```json
{ "success": true, "data": { ...merged record... } }
```

### Delete Tool — `delete_{schema}_{entity}`

Delete a record by primary key. **Irreversible.**

| Argument | Type | Description |
|---|---|---|
| `id` | string (required) | Primary key of the record to delete. |

**Response:**
```json
{ "success": true, "data": { ...deleted record... } }
```

### Annotation Tool — `resolvespec_annotate`

Store or retrieve freeform annotation records for any tool, model, or entity. Registered automatically on every handler.

| Argument | Type | Description |
|---|---|---|
| `tool_name` | string (required) | Key to annotate — an MCP tool name (e.g. `read_public_users`), a model name (e.g. `public.users`), or any other identifier. |
| `annotations` | object | Annotation data to persist. Omit to retrieve existing annotations instead. |

**Set annotations** (calls `resolvespec_set_annotation(tool_name, annotations)`):
```json
{ "tool_name": "read_public_users", "annotations": { "description": "Returns active users", "owner": "platform-team" } }
```
**Response:**
```json
{ "success": true, "tool_name": "read_public_users", "action": "set" }
```

**Get annotations** (calls `resolvespec_get_annotation(tool_name)`):
```json
{ "tool_name": "read_public_users" }
```
**Response:**
```json
{ "success": true, "tool_name": "read_public_users", "action": "get", "annotations": { ... } }
```

---

### Resource — `{schema}.{entity}`

Each model is also registered as an MCP resource with URI `schema.entity` (or just `entity` when schema is empty). Reading the resource returns up to 100 records as `application/json`.

---

## Filtering

Pass an array of filter objects to the `filters` argument:

```json
[
  { "column": "status", "operator": "=", "value": "active" },
  { "column": "age", "operator": ">", "value": 18, "logic_operator": "AND" },
  { "column": "role", "operator": "in", "value": ["admin", "editor"], "logic_operator": "OR" }
]
```

### Supported Operators

| Operator | Aliases | Description |
|---|---|---|
| `=` | `eq` | Equal |
| `!=` | `neq`, `<>` | Not equal |
| `>` | `gt` | Greater than |
| `>=` | `gte` | Greater than or equal |
| `<` | `lt` | Less than |
| `<=` | `lte` | Less than or equal |
| `like` | | SQL LIKE (case-sensitive) |
| `ilike` | | SQL ILIKE (case-insensitive) |
| `in` | | Value in list |
| `is_null` | | Column IS NULL |
| `is_not_null` | | Column IS NOT NULL |

### Logic Operators

- `"logic_operator": "AND"` (default) — filter is AND-chained with the previous condition.
- `"logic_operator": "OR"` — filter is OR-grouped with the previous condition.

Consecutive OR filters are grouped into a single `(cond1 OR cond2 OR ...)` clause.

---

## Sorting

```json
[
  { "column": "created_at", "direction": "desc" },
  { "column": "name", "direction": "asc" }
]
```

---

## Pagination

### Offset-Based

```json
{ "limit": 20, "offset": 40 }
```

### Cursor-Based

Cursor pagination uses a SQL `EXISTS` subquery for stable, efficient paging. Always pair with a `sort` argument.

```json
// Next page: pass the PK of the last record on the current page
{ "cursor_forward": "42", "limit": 20, "sort": [{"column": "id", "direction": "asc"}] }

// Previous page: pass the PK of the first record on the current page
{ "cursor_backward": "23", "limit": 20, "sort": [{"column": "id", "direction": "asc"}] }
```

---

## Preloading Relations

```json
[
  { "relation": "Profile" },
  { "relation": "Orders" }
]
```

Available relations are listed in each tool's description. Only relations defined on the model struct are valid.

---

## Hook System

Hooks let you intercept and modify CRUD operations at well-defined lifecycle points.

### Hook Types

| Constant | Fires |
|---|---|
| `BeforeHandle` | After model resolution, before operation dispatch (all CRUD) |
| `BeforeRead` / `AfterRead` | Around read queries |
| `BeforeCreate` / `AfterCreate` | Around insert |
| `BeforeUpdate` / `AfterUpdate` | Around update |
| `BeforeDelete` / `AfterDelete` | Around delete |

### Registering Hooks

```go
handler.Hooks().Register(resolvemcp.BeforeCreate, func(ctx *resolvemcp.HookContext) error {
    // Inject a timestamp before insert
    if data, ok := ctx.Data.(map[string]interface{}); ok {
        data["created_at"] = time.Now()
    }
    return nil
})

// Register the same hook for multiple events
handler.Hooks().RegisterMultiple(
    []resolvemcp.HookType{resolvemcp.BeforeCreate, resolvemcp.BeforeUpdate},
    auditHook,
)
```

### HookContext Fields

| Field | Type | Description |
|---|---|---|
| `Context` | `context.Context` | Request context |
| `Handler` | `*Handler` | The resolvemcp handler |
| `Schema` | `string` | Database schema name |
| `Entity` | `string` | Entity/table name |
| `Model` | `interface{}` | Registered model instance |
| `Options` | `common.RequestOptions` | Parsed request options (read operations) |
| `Operation` | `string` | `"read"`, `"create"`, `"update"`, or `"delete"` |
| `ID` | `string` | Primary key from request (read/update/delete) |
| `Data` | `interface{}` | Input data (create/update — modifiable) |
| `Result` | `interface{}` | Output data (set by After hooks) |
| `Error` | `error` | Operation error, if any |
| `Query` | `common.SelectQuery` | Live query object (available in `BeforeRead`) |
| `Tx` | `common.Database` | Database/transaction handle |
| `Abort` | `bool` | Set to `true` to abort the operation |
| `AbortMessage` | `string` | Error message returned when aborting |
| `AbortCode` | `int` | Optional status code for the abort |

### Aborting an Operation

```go
handler.Hooks().Register(resolvemcp.BeforeDelete, func(ctx *resolvemcp.HookContext) error {
    ctx.Abort = true
    ctx.AbortMessage = "deletion is disabled"
    return nil
})
```

### Managing Hooks

```go
registry := handler.Hooks()
registry.HasHooks(resolvemcp.BeforeCreate)   // bool
registry.Clear(resolvemcp.BeforeCreate)      // remove hooks for one type
registry.ClearAll()                          // remove all hooks
```

---

## Context Helpers

Request metadata is threaded through `context.Context` during handler execution. Hooks and custom tools can read it:

```go
schema    := resolvemcp.GetSchema(ctx)
entity    := resolvemcp.GetEntity(ctx)
tableName := resolvemcp.GetTableName(ctx)
model     := resolvemcp.GetModel(ctx)
modelPtr  := resolvemcp.GetModelPtr(ctx)
```

You can also set values manually (e.g. in middleware):

```go
ctx = resolvemcp.WithSchema(ctx, "tenant_a")
```

---

## Adding Custom MCP Tools

Access the underlying `*server.MCPServer` to register additional tools:

```go
mcpServer := handler.MCPServer()
mcpServer.AddTool(myTool, myHandler)
```

---

## Table Name Resolution

The handler resolves table names in priority order:

1. `TableNameProvider` interface — `TableName() string` (can return `"schema.table"`)
2. `SchemaProvider` interface — `SchemaName() string` (combined with entity name)
3. Fallback: `schema.entity` (or `schema_entity` for SQLite)
