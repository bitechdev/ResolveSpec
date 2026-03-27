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

## HTTP / SSE Transport

The `*server.SSEServer` returned by any of the helpers below implements `http.Handler`, so it works with every Go HTTP framework.

`Config.BasePath` is required and used for all route registration.
`Config.BaseURL` is optional — when empty it is detected from each request.

### Gorilla Mux

```go
resolvemcp.SetupMuxRoutes(r, handler)
```

Registers:

| Route | Method | Description |
|---|---|---|
| `{BasePath}/sse` | GET | SSE connection — clients subscribe here |
| `{BasePath}/message` | POST | JSON-RPC — clients send requests here |
| `{BasePath}/*` | any | Full SSE server (convenience prefix) |

### bunrouter

```go
resolvemcp.SetupBunRouterRoutes(router, handler)
```

Registers `GET {BasePath}/sse` and `POST {BasePath}/message` on the provided `*bunrouter.Router`.

### Gin (or any `http.Handler`-compatible framework)

Use `handler.SSEServer()` to get an `http.Handler` and wrap it with the framework's adapter:

```go
sse := handler.SSEServer()

// Gin
engine.Any("/mcp/*path", gin.WrapH(sse))

// net/http
http.Handle("/mcp/", sse)

// Echo
e.Any("/mcp/*", echo.WrapHandler(sse))
```

### Authentication

Add middleware before the MCP routes. The handler itself has no auth layer.

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
