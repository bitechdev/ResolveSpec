# resolvemcp

Package `resolvemcp` exposes registered database models to AI clients through a **fixed set of Model Context Protocol (MCP) meta tools** over SSE or Streamable HTTP. The tool count does not grow with the number of models. It mirrors the `resolvespec` package — same model registration, filter/sort/pagination/preload options, hook system and security rules.

Every endpoint **requires authentication**; tools run as the authenticated caller.

## Quick Start

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/resolvemcp"
    "github.com/bitechdev/ResolveSpec/pkg/security"
    "github.com/gorilla/mux"
)

handler := resolvemcp.NewHandlerWithGORM(db, resolvemcp.Config{
    BaseURL:  "http://localhost:8080",
    BasePath: "/mcp",
})

securityList, _ := security.NewSecurityList(provider)
resolvemcp.RegisterSecurityHooks(handler, securityList)

handler.RegisterModel("public", "users", &User{})
handler.RegisterModel("public", "orders", &Order{})

r := mux.NewRouter()
resolvemcp.SetupMuxRoutes(r, handler, securityList) // guarded
```

---

## Config

| Field | Default | Purpose |
|---|---|---|
| `BaseURL` | request-detected | Public base URL sent to SSE clients |
| `BasePath` | request-detected | Mount path (e.g. `/mcp`) |
| `DefaultLimit` | 50 | Page size when a read gives no limit |
| `MaxLimit` | 1000 | Larger limits are clamped |
| `MaxOffset` | 100000 | Larger offsets are rejected |
| `MaxBatch` | 100 | Rows in one batch insert |
| `MaxPreloadDepth` | 2 | Depth of a preload path (`a.b.c`) |
| `MaxWriteRows` | 100 | Rows a filter-based update/delete may touch |
| `QueryTimeout` | 30s | One tool call, hooks and queries included |
| `ConfirmTTL` | 5m | Lifetime of a filter-write confirm token |
| `AllowedHosts` | any | Host allowlist for SSE when `BaseURL` is empty (prefer setting `BaseURL`) |
| `EnableAnnotations` | false | Registers `resolvespec_annotate` (opt-in) |

---

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

`RegisterModel` only adds the model to the registry; it creates no tools. All registered models are visible to `list_tables`; per-entity rules (see [Security](#security)) restrict the operations.

### Functions

```go
// Go callback
handler.RegisterFunction(resolvemcp.Function{
    Name:        "recalc_totals",
    Description: "Recalculate order totals",
    Params:      []resolvemcp.FunctionParam{{Name: "order_id", Type: resolvemcp.ParamNumber, Required: true}},
    Handler: func(ctx context.Context, tx common.Database, args map[string]any) (any, error) { return nil, nil },
    Authorize: func(ctx context.Context) error { return nil }, // optional per-caller gate
})

// SQL procedure: SELECT * FROM public.my_proc($1, $2::jsonb)
handler.RegisterFunction(resolvemcp.Function{
    Name: "my_proc", Procedure: "public.my_proc",
    Params: []resolvemcp.FunctionParam{{Name: "a", Type: resolvemcp.ParamString, Required: true}},
})
```

Only registered functions are callable. Arguments are validated against `Params`; calls run in a transaction (`OnTxBegin` fired). A function the caller is not authorized for looks identical to an unknown one.

---

## HTTP Transports

`Config.BasePath` is used for route registration. `Config.BaseURL` is optional — when empty it is detected from each request.

All `Setup*`/`New*` helpers wrap the endpoint in `Guard(securityList)`: a valid OAuth bearer token, session token or API key is required, there is no guest/optional mode, and it fails closed. `handler.SSEServer()` / `handler.StreamableHTTPServer()` and the `*Unauthenticated` variants serve **without** a guard and log a warning; use them only behind your own authentication.

Two transports are supported: **SSE** (legacy, two-endpoint) and **Streamable HTTP** (recommended, single-endpoint).

---

### SSE Transport

Two endpoints: `GET {BasePath}/sse` (subscribe) + `POST {BasePath}/message` (send).

#### Gorilla Mux

```go
resolvemcp.SetupMuxRoutes(r, handler, securityList)
```

| Route | Method | Description |
|---|---|---|
| `{BasePath}/sse` | GET | SSE connection — clients subscribe here |
| `{BasePath}/message` | POST | JSON-RPC — clients send requests here |

#### bunrouter

```go
resolvemcp.SetupBunRouterRoutes(router, handler, securityList)
```

#### Gin / net/http / Echo

```go
sse := resolvemcp.NewSSEServer(handler, securityList) // guarded

engine.Any("/mcp/*path", gin.WrapH(sse))  // Gin
http.Handle("/mcp/", sse)                  // net/http
e.Any("/mcp/*", echo.WrapHandler(sse))     // Echo
```

---

### Streamable HTTP Transport

Single endpoint at `{BasePath}`. Handles POST (client→server) and GET (server→client streaming). Preferred for new integrations.

#### Gorilla Mux

```go
resolvemcp.SetupMuxStreamableHTTPRoutes(r, handler, securityList)
```

Mounts the handler at `{BasePath}` (all methods).

#### bunrouter

```go
resolvemcp.SetupBunRouterStreamableHTTPRoutes(router, handler, securityList)
```

Registers GET, POST, DELETE on `{BasePath}`.

#### Gin / net/http / Echo

```go
h := resolvemcp.NewStreamableHTTPHandler(handler, securityList) // guarded

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

> The underlying `security.OAuthServer` also supports consent, OpenID Connect, rotating refresh tokens, JWT access tokens, DPoP, PAR, the device grant and token exchange; they are opt-in `OAuthServerConfig` options described in [pkg/security/OAUTH2_SERVER.md](../security/OAUTH2_SERVER.md). The options of `resolvemcp.OAuth2Config` are unchanged.

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
resolvemcp.RegisterSecurityHooks(handler, securityList)

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

securityList, err := security.NewSecurityList(mySecurityProvider)
if err != nil {
    log.Fatal(err)
}
resolvemcp.RegisterSecurityHooks(handler, securityList)
```

Call `RegisterSecurityHooks` **once**, after creating the handler and before registering models. It installs these controls automatically:

| Hook | Effect |
|---|---|
| `OnTxBegin` | Stamps transaction-local settings (RLS GUCs) set with `SecurityList.SetTxSettings` |
| `BeforeHandle` | Enforces per-entity operation rules (see below); preloads column rules for writes |
| `BeforeRead` | Loads RLS/CLS rules, then injects a user-scoped WHERE clause |
| `BeforeScan` | Applies row security to the row an update or delete targets; a row the user cannot see is "not found" |
| `AfterRead` | Masks/hides columns per column-security rules; writes audit log |
| `BeforeCreate` | Blocks create if `CanCreate` is false; drops hidden/masked columns from the payload |
| `BeforeUpdate` | Blocks update if `CanUpdate` is false; drops hidden/masked columns from the payload |
| `BeforeDelete` | Blocks delete if `CanDelete` is false |

Additional hooks: `BeforeScan` (row pre-read of update/delete/filter writes), `BeforeCall`/`AfterCall` (functions) and `OnTxBegin`. Hooks are mutex-protected and panics in hooks are recovered.

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

## Describing the API for agents

Give agents context about what each table is for:

```go
// 1. Explicitly, in code
handler.SetModelDescription("public", "users", modelregistry.ModelInfo{
    Description: "Application accounts",
    Purpose:     "Look up who a person is",
    Tags:        []string{"identity"},
    Columns:     map[string]string{"email": "Login address, unique"},
})

// 2. From an external JSON map (keyed by "schema.entity"); entries here win
n, err := handler.LoadModelDescriptions("docs/model-descriptions.json")
```

Example `docs/model-descriptions.json` (every key is optional; unknown keys are rejected):

```json
{
  "public.users": {
    "description": "Application accounts, one row per person who can sign in.",
    "purpose": "Look up who someone is. Use public.orders for what they bought.",
    "tags": ["identity", "pii"],
    "columns": {
      "id": "Internal account id",
      "email": "Login address, unique and lower-cased",
      "created_at": "When the account was created (UTC)"
    }
  },
  "public.orders": {
    "description": "Customer orders.",
    "columns": {
      "status": "One of: pending, paid, shipped, cancelled"
    }
  }
}
```

Keys are `schema.entity` names as registered with `RegisterModel`. Column keys are the JSON column names shown by `describe_table`. An entry replaces any info set earlier for that table, and columns it leaves out still fall back to field tags.

Fallbacks when nothing is set for a table or column, in order: the model's `ModelDescription() string` method (table), then field tags (column): `comment`, `note`, `desc` or `description` tags, then `comment:` inside the `gorm` or `bun` tag.

The text appears in `list_tables` and `describe_table`. The server also sends a short usage guide as MCP `instructions` on connect.

### Catalogue file

`handler.ExportCatalog(path)` writes the usage guide, tools, limits and every table (columns, types, keys, relations, allowed operations, descriptions) to disk, JSON for a `.json` path and Markdown otherwise. The file is replaced atomically. It lists every table with at least one allowed operation, regardless of caller, so keep it out of public directories. Call it after registering models (for example at startup, or from a `go generate` step).

## MCP Tools

Fixed set, independent of the models. `table` is `schema.entity`. Errors return `{"success":false,"error":{"code","message"}}` with codes `invalid_argument`, `not_found`, `forbidden`, `limit_exceeded`, `internal` (internal details are logged, the client gets a reference id).

| Tool | Purpose |
|---|---|
| `list_tables` | Tables the caller may use and the allowed operations |
| `describe_table` | Columns, PK, relations, writable columns, operations, limits |
| `select_table` | Read rows (filters, sort, columns, preloads, paging) |
| `insert_into_table` | Insert one row or a capped batch |
| `update_table` | Update by `id` or `filters` |
| `delete_from_table` | Delete by `id` or `filters` |
| `list_functions` | Registered functions the caller may call, with parameters |
| `call_function` | Call a registered function |
| `resolvespec_annotate` | Only with `EnableAnnotations` |

### `select_table`

| Argument | Type | Description |
|---|---|---|
| `table` | string (required) | `schema.entity` |
| `id` | string | Primary key of one row |
| `filters`, `sort` | array | See [Filtering](#filtering), [Sorting](#sorting) |
| `columns`, `omit_columns` | array | Column selection |
| `preloads` | array | Relations (validated against the model, max depth `MaxPreloadDepth`) |
| `limit`, `offset` | number | Clamped to `MaxLimit` / rejected above `MaxOffset` |
| `cursor_forward`, `cursor_backward` | string | PK cursor, requires `sort` |
| `include_count` | boolean | Also compute totals (slower); otherwise `total`/`filtered` are 0 |

Response: `{"success":true,"data":[...],"metadata":{"total","filtered","count","limit","offset"}}`

### `insert_into_table`

`data` is an object or an array (one transaction, max `MaxBatch`). Unknown, duplicate or read-only keys are rejected; keys are resolved to columns from the model.

### `update_table` / `delete_from_table`

Either `id` or `filters` is required.

| Mode | Behaviour |
|---|---|
| `id` | One row, applied immediately. The row is locked and row security applies; an invisible row is "not found". |
| `filters` | Matching rows are found inside the transaction (row security applied, max `MaxWriteRows`). The first call returns a preview and a `confirm_token`; repeat the identical call with `confirm_token` to apply. |
| `dry_run` | Report match count and preview ids; change nothing. |

The token is single-use, expires after `ConfirmTTL`, and is bound to user, table, operation and a hash of filters, data and matched ids; it is held in memory (lost on restart, single instance). Update changes only the keys in `data`; `null` sets NULL. Filters are strictly parsed (never silently dropped), columns validated, and only the documented operators are accepted.

### `call_function`

`name` and `arguments` (object). See [Functions](#functions).

### `resolvespec_annotate`

Opt-in (`Config.EnableAnnotations`). Stores/retrieves freeform annotations through `resolvespec_set_annotation` / `resolvespec_get_annotation`; runs `BeforeHandle` hooks (`annotate_set` / `annotate_get`) and a transaction.

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
| `BeforeScan` | Row pre-read for update/delete/filter writes |
| `BeforeCall` / `AfterCall` | Around `call_function` |
| `OnTxBegin` | Start of every transaction |

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
| `Operation` | `string` | `"read"`, `"create"`, `"update"`, `"delete"`, `"call"`, `"annotate_set"` or `"annotate_get"` |
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

The caller's `security.UserContext` reaches every tool call through the request context. Request metadata is threaded through `context.Context` during handler execution. Hooks and custom tools can read it:

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

Access the underlying `*server.MCPServer` to register additional tools (they sit behind the same guard). Prefer `RegisterFunction` for database-backed actions:

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

---

## Breaking changes

- Per-model tools (`read_/create_/update_/delete_{schema}_{entity}`) and per-model resources are gone; use the meta tools.
- `Setup*` / `NewSSEServer` / `NewStreamableHTTPHandler` take a `*security.SecurityList` and require authentication. `OptionalAuth*` helpers were removed; `*Unauthenticated` variants exist for explicit opt-out.
- `resolvespec_annotate` is opt-in via `Config.EnableAnnotations`.
- `Handler.Build()` is not needed.
- Update is now a partial update by validated keys; reads are capped by the configured limits.
