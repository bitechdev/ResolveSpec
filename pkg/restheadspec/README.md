# RestHeadSpec - Header-Based REST API

RestHeadSpec provides a REST API where all query options are passed via HTTP headers instead of the request body. This provides cleaner separation between data and metadata, making it ideal for GET requests and RESTful architectures.

## Features

* **Header-Based Querying**: All query options via HTTP headers
* **Lifecycle Hooks**: Before/after hooks for create, read, update, delete operations
* **Cursor Pagination**: Efficient cursor-based pagination with complex sorting
* **Advanced Filtering**: Field filters, search operators, AND/OR logic
* **Multiple Response Formats**: Simple, detailed, and Syncfusion-compatible responses
* **Single Record as Object**: Automatically return single-element arrays as objects (default)
* **Base64 Support**: Base64-encoded header values for complex queries
* **Type-Aware Filtering**: Automatic type detection and conversion
* **CORS Support**: Comprehensive CORS headers for cross-origin requests
* **OPTIONS Method**: Full OPTIONS support for CORS preflight

## Quick Start

### Setup with GORM

```go
import "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
import "github.com/gorilla/mux"

// Create handler
handler := restheadspec.NewHandlerWithGORM(db)

// IMPORTANT: Register models BEFORE setting up routes
handler.Registry.RegisterModel("public.users", &User{})
handler.Registry.RegisterModel("public.posts", &Post{})

// Setup routes
router := mux.NewRouter()
restheadspec.SetupMuxRoutes(router, handler, nil)

// Start server
http.ListenAndServe(":8080", router)
```

### Setup with Bun ORM

```go
import "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
import "github.com/uptrace/bun"

// Create handler with Bun
handler := restheadspec.NewHandlerWithBun(bunDB)

// Register models
handler.Registry.RegisterModel("public.users", &User{})

// Setup routes (same as GORM)
router := mux.NewRouter()
restheadspec.SetupMuxRoutes(router, handler, nil)
```

## Basic Usage

### Simple GET Request

```http
GET /public/users HTTP/1.1
Host: api.example.com
X-Select-Fields: id,name,email
X-FieldFilter-Status: active
X-Sort: -created_at
X-Limit: 50
```

### With Preloading

```http
GET /public/users HTTP/1.1
X-Select-Fields: id,name,email,department_id
X-Preload: department:id,name
X-FieldFilter-Status: active
X-Limit: 50
```

## Common Headers

| Header | Description | Example |
|--------|-------------|---------|
| `X-Select-Fields` | Columns to include | `id,name,email` |
| `X-Not-Select-Fields` | Columns to exclude | `password,internal_notes` |
| `X-FieldFilter-{col}` | Exact match filter | `X-FieldFilter-Status: active` |
| `X-SearchFilter-{col}` | Fuzzy search (ILIKE) | `X-SearchFilter-Name: john` |
| `X-SearchOp-{op}-{col}` | Filter with operator | `X-SearchOp-Gte-Age: 18` |
| `X-Preload` | Preload relations | `posts:id,title` |
| `X-Sort` | Sort columns | `-created_at,+name` |
| `X-Limit` | Limit results | `50` |
| `X-Offset` | Offset for pagination | `100` |
| `X-Clean-JSON` | Remove null/empty fields | `true` |
| `X-Single-Record-As-Object` | Return single records as objects | `false` |

**Available Operators**: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `startswith`, `endswith`, `between`, `betweeninclusive`, `in`, `empty`, `notempty`

For complete header documentation, see [HEADERS.md](HEADERS.md).

## Lifecycle Hooks

RestHeadSpec supports lifecycle hooks for all CRUD operations:

```go
import "github.com/bitechdev/ResolveSpec/pkg/restheadspec"

// Create handler
handler := restheadspec.NewHandlerWithGORM(db)

// Register a before-read hook (e.g., for authorization)
handler.Hooks.Register(restheadspec.BeforeRead, func(ctx *restheadspec.HookContext) error {
    // Check permissions
    if !userHasPermission(ctx.Context, ctx.Entity) {
        return fmt.Errorf("unauthorized access to %s", ctx.Entity)
    }

    // Modify query options
    ctx.Options.Limit = ptr(100) // Enforce max limit

    return nil
})

// Register an after-read hook (e.g., for data transformation)
handler.Hooks.Register(restheadspec.AfterRead, func(ctx *restheadspec.HookContext) error {
    // Transform or filter results
    if users, ok := ctx.Result.([]User); ok {
        for i := range users {
            users[i].Email = maskEmail(users[i].Email)
        }
    }
    return nil
})

// Register a before-create hook (e.g., for validation)
handler.Hooks.Register(restheadspec.BeforeCreate, func(ctx *restheadspec.HookContext) error {
    // Validate data
    if user, ok := ctx.Data.(*User); ok {
        if user.Email == "" {
            return fmt.Errorf("email is required")
        }
        // Add timestamps
        user.CreatedAt = time.Now()
    }
    return nil
})
```

**Available Hook Types**:
* `BeforeRead`, `AfterRead`
* `BeforeCreate`, `AfterCreate`
* `BeforeUpdate`, `AfterUpdate`
* `BeforeDelete`, `AfterDelete`

**HookContext** provides:
* `Context`: Request context
* `Handler`: Access to handler, database, and registry
* `Schema`, `Entity`, `TableName`: Request info
* `Model`: The registered model type
* `Options`: Parsed request options (filters, sorting, etc.)
* `ID`: Record ID (for single-record operations)
* `Data`: Request data (for create/update)
* `Result`: Operation result (for after hooks)
* `Writer`: Response writer (allows hooks to modify response)

## Cursor Pagination

RestHeadSpec supports efficient cursor-based pagination for large datasets:

```http
GET /public/posts HTTP/1.1
X-Sort: -created_at,+id
X-Limit: 50
X-Cursor-Forward: <cursor_token>
```

**How it works**:
1. First request returns results + cursor token in response
2. Subsequent requests use `X-Cursor-Forward` or `X-Cursor-Backward`
3. Cursor maintains consistent ordering even with data changes
4. Supports complex multi-column sorting

**Benefits over offset pagination**:
* Consistent results when data changes
* Better performance for large offsets
* Prevents "skipped" or duplicate records
* Works with complex sort expressions

**Example with hooks**:

```go
// Enable cursor pagination in a hook
handler.Hooks.Register(restheadspec.BeforeRead, func(ctx *restheadspec.HookContext) error {
    // For large tables, enforce cursor pagination
    if ctx.Entity == "posts" && ctx.Options.Offset != nil && *ctx.Options.Offset > 1000 {
        return fmt.Errorf("use cursor pagination for large offsets")
    }
    return nil
})
```

## Response Formats

RestHeadSpec supports multiple response formats:

**1. Simple Format** (`X-SimpleApi: true`):

```json
[
  { "id": 1, "name": "John" },
  { "id": 2, "name": "Jane" }
]
```

**2. Detail Format** (`X-DetailApi: true`, default):

```json
{
  "success": true,
  "data": [...],
  "metadata": {
    "total": 100,
    "filtered": 100,
    "limit": 50,
    "offset": 0
  }
}
```

**3. Syncfusion Format** (`X-Syncfusion: true`):

```json
{
  "result": [...],
  "count": 100
}
```

## Single Record as Object (Default Behavior)

By default, RestHeadSpec automatically converts single-element arrays into objects for cleaner API responses.

**Default behavior (enabled)**:

```http
GET /public/users/123
```

```json
{
  "success": true,
  "data": { "id": 123, "name": "John", "email": "john@example.com" }
}
```

**To disable** (force arrays):

```http
GET /public/users/123
X-Single-Record-As-Object: false
```

```json
{
  "success": true,
  "data": [{ "id": 123, "name": "John", "email": "john@example.com" }]
}
```

**How it works**:
* When a query returns exactly **one record**, it's returned as an object
* When a query returns **multiple records**, they're returned as an array
* Set `X-Single-Record-As-Object: false` to always receive arrays
* Works with all response formats (simple, detail, syncfusion)
* Applies to both read operations and create/update returning clauses

## CORS & OPTIONS Support

RestHeadSpec includes comprehensive CORS support for cross-origin requests:

**OPTIONS Method**:

```http
OPTIONS /public/users HTTP/1.1
```

Returns metadata with appropriate CORS headers:

```http
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, X-Select-Fields, X-FieldFilter-*, ...
Access-Control-Max-Age: 86400
Access-Control-Allow-Credentials: true
```

**Key Features**:
* OPTIONS returns model metadata (same as GET metadata endpoint)
* All HTTP methods include CORS headers automatically
* OPTIONS requests don't require authentication (CORS preflight)
* Supports all HeadSpec custom headers (`X-Select-Fields`, `X-FieldFilter-*`, etc.)
* 24-hour max age to reduce preflight requests

**Configuration**:

```go
import "github.com/bitechdev/ResolveSpec/pkg/common"

// Get default CORS config
corsConfig := common.DefaultCORSConfig()

// Customize if needed
corsConfig.AllowedOrigins = []string{"https://example.com"}
corsConfig.AllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
```

## Advanced Features

### Base64 Encoding

For complex header values, use base64 encoding:

```http
GET /public/users HTTP/1.1
X-Select-Fields-Base64: aWQsbmFtZSxlbWFpbA==
```

### AND/OR Logic

Combine multiple filters with AND/OR logic:

```http
GET /public/users HTTP/1.1
X-FieldFilter-Status: active
X-SearchOp-Gte-Age: 18
X-Filter-Logic: AND
```

### Complex Preloading

Load nested relationships:

```http
GET /public/users HTTP/1.1
X-Preload: posts:id,title,comments:id,text,author:name
```

## Model Registration

```go
type User struct {
    ID    uint   `json:"id" gorm:"primaryKey"`
    Name  string `json:"name"`
    Email string `json:"email"`
    Posts []Post `json:"posts,omitempty" gorm:"foreignKey:UserID"`
}

// Schema.Table format
handler.Registry.RegisterModel("public.users", &User{})
```

## Complete Example

```go
package main

import (
    "log"
    "net/http"

    "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
    "github.com/gorilla/mux"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

type User struct {
    ID     uint   `json:"id" gorm:"primaryKey"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Status string `json:"status"`
}

func main() {
    // Connect to database
    db, err := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})
    if err != nil {
        log.Fatal(err)
    }

    // Create handler
    handler := restheadspec.NewHandlerWithGORM(db)

    // Register models
    handler.Registry.RegisterModel("public.users", &User{})

    // Add hooks
    handler.Hooks.Register(restheadspec.BeforeRead, func(ctx *restheadspec.HookContext) error {
        log.Printf("Reading %s", ctx.Entity)
        return nil
    })

    // Setup routes
    router := mux.NewRouter()
    restheadspec.SetupMuxRoutes(router, handler, nil)

    // Start server
    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Testing

RestHeadSpec is designed for testability:

```go
import (
    "net/http/httptest"
    "testing"
)

func TestUserRead(t *testing.T) {
    handler := restheadspec.NewHandlerWithGORM(testDB)
    handler.Registry.RegisterModel("public.users", &User{})

    req := httptest.NewRequest("GET", "/public/users", nil)
    req.Header.Set("X-Select-Fields", "id,name")
    req.Header.Set("X-Limit", "10")

    rec := httptest.NewRecorder()
    // Test your handler...
}
```

## See Also

* [HEADERS.md](HEADERS.md) - Complete header reference
* [Main README](../../README.md) - ResolveSpec overview
* [ResolveSpec Package](../resolvespec/README.md) - Body-based API
* [StaticWeb Package](../server/staticweb/README.md) - Static file server

## License

This package is part of ResolveSpec and is licensed under the MIT License.
