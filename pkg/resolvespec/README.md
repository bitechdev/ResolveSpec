# ResolveSpec - Body-Based REST API

ResolveSpec provides a REST API where query options are passed in the JSON request body. This approach offers GraphQL-like flexibility while maintaining RESTful principles, making it ideal for complex queries and operations.

## Features

* **Body-Based Querying**: All query options passed via JSON request body
* **Lifecycle Hooks**: Before/after hooks for create, read, update, delete operations
* **Cursor Pagination**: Efficient cursor-based pagination with complex sorting
* **Offset Pagination**: Traditional limit/offset pagination support
* **Advanced Filtering**: Multiple operators, AND/OR logic, and custom SQL
* **Relationship Preloading**: Load related entities with custom column selection and filters
* **Recursive CRUD**: Automatically handle nested object graphs with foreign key resolution
* **Computed Columns**: Define virtual columns with SQL expressions
* **Database-Agnostic**: Works with GORM, Bun, or custom database adapters
* **Router-Agnostic**: Integrates with any HTTP router through standard interfaces
* **Type-Safe**: Strong type validation and conversion

## Quick Start

### Setup with GORM

```go
import "github.com/bitechdev/ResolveSpec/pkg/resolvespec"
import "github.com/gorilla/mux"

// Create handler
handler := resolvespec.NewHandlerWithGORM(db)

// IMPORTANT: Register models BEFORE setting up routes
handler.registry.RegisterModel("core.users", &User{})
handler.registry.RegisterModel("core.posts", &Post{})

// Setup routes
router := mux.NewRouter()
resolvespec.SetupMuxRoutes(router, handler, nil)

// Start server
http.ListenAndServe(":8080", router)
```

### Setup with Bun ORM

```go
import "github.com/bitechdev/ResolveSpec/pkg/resolvespec"
import "github.com/uptrace/bun"

// Create handler with Bun
handler := resolvespec.NewHandlerWithBun(bunDB)

// Register models
handler.registry.RegisterModel("core.users", &User{})

// Setup routes (same as GORM)
router := mux.NewRouter()
resolvespec.SetupMuxRoutes(router, handler, nil)
```

## Basic Usage

### Simple Read Request

```http
POST /core/users HTTP/1.1
Content-Type: application/json

{
  "operation": "read",
  "options": {
    "columns": ["id", "name", "email"],
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      }
    ],
    "sort": [
      {
        "column": "created_at",
        "direction": "desc"
      }
    ],
    "limit": 10,
    "offset": 0
  }
}
```

### With Preloading

```http
POST /core/users HTTP/1.1
Content-Type: application/json

{
  "operation": "read",
  "options": {
    "columns": ["id", "name", "email"],
    "preload": [
      {
        "relation": "posts",
        "columns": ["id", "title", "created_at"],
        "filters": [
          {
            "column": "status",
            "operator": "eq",
            "value": "published"
          }
        ]
      }
    ],
    "limit": 10
  }
}
```

## Request Structure

### Request Format

```json
{
  "operation": "read|create|update|delete",
  "data": {
    // For create/update operations
  },
  "options": {
    "columns": [...],
    "preload": [...],
    "filters": [...],
    "sort": [...],
    "limit": number,
    "offset": number,
    "cursor_forward": "string",
    "cursor_backward": "string",
    "customOperators": [...],
    "computedColumns": [...]
  }
}
```

### Operations

| Operation | Description | Requires Data | Requires ID |
|-----------|-------------|---------------|-------------|
| `read` | Fetch records | No | Optional (single record) |
| `create` | Create new record(s) | Yes | No |
| `update` | Update existing record(s) | Yes | Yes (in URL) |
| `delete` | Delete record(s) | No | Yes (in URL) |

### Options Fields

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `columns` | `[]string` | Columns to select | `["id", "name", "email"]` |
| `preload` | `[]PreloadConfig` | Relations to load | See [Preloading](#preloading) |
| `filters` | `[]Filter` | Filter conditions | See [Filtering](#filtering) |
| `sort` | `[]Sort` | Sort criteria | `[{"column": "created_at", "direction": "desc"}]` |
| `limit` | `int` | Max records to return | `50` |
| `offset` | `int` | Number of records to skip | `100` |
| `cursor_forward` | `string` | Cursor for next page | `"12345"` |
| `cursor_backward` | `string` | Cursor for previous page | `"12300"` |
| `customOperators` | `[]CustomOperator` | Custom SQL conditions | See [Custom Operators](#custom-operators) |
| `computedColumns` | `[]ComputedColumn` | Virtual columns | See [Computed Columns](#computed-columns) |

## Filtering

### Available Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `eq` | Equal | `{"column": "status", "operator": "eq", "value": "active"}` |
| `neq` | Not Equal | `{"column": "status", "operator": "neq", "value": "deleted"}` |
| `gt` | Greater Than | `{"column": "age", "operator": "gt", "value": 18}` |
| `gte` | Greater Than or Equal | `{"column": "age", "operator": "gte", "value": 18}` |
| `lt` | Less Than | `{"column": "price", "operator": "lt", "value": 100}` |
| `lte` | Less Than or Equal | `{"column": "price", "operator": "lte", "value": 100}` |
| `like` | LIKE pattern | `{"column": "name", "operator": "like", "value": "%john%"}` |
| `ilike` | Case-insensitive LIKE | `{"column": "email", "operator": "ilike", "value": "%@example.com"}` |
| `in` | IN clause | `{"column": "status", "operator": "in", "value": ["active", "pending"]}` |
| `contains` | Contains string | `{"column": "description", "operator": "contains", "value": "important"}` |
| `startswith` | Starts with string | `{"column": "name", "operator": "startswith", "value": "John"}` |
| `endswith` | Ends with string | `{"column": "email", "operator": "endswith", "value": "@example.com"}` |
| `between` | Between (exclusive) | `{"column": "age", "operator": "between", "value": [18, 65]}` |
| `betweeninclusive` | Between (inclusive) | `{"column": "price", "operator": "betweeninclusive", "value": [10, 100]}` |
| `empty` | IS NULL or empty | `{"column": "deleted_at", "operator": "empty"}` |
| `notempty` | IS NOT NULL | `{"column": "email", "operator": "notempty"}` |

### Complex Filtering Example

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "age",
        "operator": "gte",
        "value": 18
      },
      {
        "column": "email",
        "operator": "ilike",
        "value": "%@company.com"
      }
    ]
  }
}
```

### OR Logic in Filters (SearchOr)

Use the `logic_operator` field to combine filters with OR logic instead of the default AND:

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      },
      {
        "column": "status",
        "operator": "eq",
        "value": "pending",
        "logic_operator": "OR"
      },
      {
        "column": "priority",
        "operator": "eq",
        "value": "high",
        "logic_operator": "OR"
      }
    ]
  }
}
```

This will produce: `WHERE (status = 'active' OR status = 'pending' OR priority = 'high')`

**Important:** Consecutive OR filters are automatically grouped together with parentheses to ensure proper query logic.

#### Mixing AND and OR

Consecutive OR filters are grouped, then combined with AND filters:

```json
{
  "filters": [
    {
      "column": "status",
      "operator": "eq",
      "value": "active"
    },
    {
      "column": "status",
      "operator": "eq",
      "value": "pending",
      "logic_operator": "OR"
    },
    {
      "column": "age",
      "operator": "gte",
      "value": 18
    }
  ]
}
```

Produces: `WHERE (status = 'active' OR status = 'pending') AND age >= 18`

This grouping ensures OR conditions don't interfere with other AND conditions in the query.

### Custom Operators

Add custom SQL conditions when needed:

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "email_domain_filter",
        "sql": "LOWER(email) LIKE '%@example.com'"
      },
      {
        "name": "recent_records",
        "sql": "created_at > NOW() - INTERVAL '7 days'"
      }
    ]
  }
}
```

Custom operators are applied as additional WHERE conditions to your query.

### Fetch Row Number

Get the row number (position) of a specific record in the filtered and sorted result set. **When `fetch_row_number` is specified, only that specific record is returned** (not all records).

```json
{
  "operation": "read",
  "options": {
    "filters": [
      {
        "column": "status",
        "operator": "eq",
        "value": "active"
      }
    ],
    "sort": [
      {
        "column": "score",
        "direction": "desc"
      }
    ],
    "fetch_row_number": "12345"
  }
}
```

**Response - Returns ONLY the specified record with its position:**

```json
{
  "success": true,
  "data": {
    "id": 12345,
    "name": "John Doe",
    "score": 850,
    "status": "active"
  },
  "metadata": {
    "total": 1000,
    "count": 1,
    "filtered": 1000,
    "row_number": 42
  }
}
```

**Use Case:** Perfect for "Show me this user and their ranking" - you get just that one user with their position in the leaderboard.

**Note:** This is different from the `RowNumber` field feature, which automatically numbers all records in a paginated response based on offset. That feature uses simple math (`offset + index + 1`), while `fetch_row_number` uses SQL window functions to calculate the actual position in a sorted/filtered set. To use the `RowNumber` field feature, simply add a `RowNumber int64` field to your model - it will be automatically populated with the row position based on pagination.

## Preloading

Load related entities with custom configuration:

```json
{
  "operation": "read",
  "options": {
    "columns": ["id", "name", "email"],
    "preload": [
      {
        "relation": "posts",
        "columns": ["id", "title", "created_at"],
        "filters": [
          {
            "column": "status",
            "operator": "eq",
            "value": "published"
          }
        ],
        "sort": [
          {
            "column": "created_at",
            "direction": "desc"
          }
        ],
        "limit": 5
      },
      {
        "relation": "profile",
        "columns": ["bio", "website"]
      }
    ]
  }
}
```

## Cursor Pagination

Efficient pagination for large datasets:

### First Request (No Cursor)

```json
{
  "operation": "read",
  "options": {
    "sort": [
      {
        "column": "created_at",
        "direction": "desc"
      },
      {
        "column": "id",
        "direction": "asc"
      }
    ],
    "limit": 50
  }
}
```

### Next Page (Forward Cursor)

```json
{
  "operation": "read",
  "options": {
    "sort": [
      {
        "column": "created_at",
        "direction": "desc"
      },
      {
        "column": "id",
        "direction": "asc"
      }
    ],
    "limit": 50,
    "cursor_forward": "12345"
  }
}
```

### Previous Page (Backward Cursor)

```json
{
  "operation": "read",
  "options": {
    "sort": [
      {
        "column": "created_at",
        "direction": "desc"
      },
      {
        "column": "id",
        "direction": "asc"
      }
    ],
    "limit": 50,
    "cursor_backward": "12300"
  }
}
```

**Benefits over offset pagination**:
* Consistent results when data changes
* Better performance for large offsets
* Prevents "skipped" or duplicate records
* Works with complex sort expressions

## Recursive CRUD Operations

Automatically handle nested object graphs with intelligent foreign key resolution.

### Creating Nested Objects

```json
{
  "operation": "create",
  "data": {
    "name": "John Doe",
    "email": "john@example.com",
    "posts": [
      {
        "title": "My First Post",
        "content": "Hello World",
        "tags": [
          {"name": "tech"},
          {"name": "programming"}
        ]
      },
      {
        "title": "Second Post",
        "content": "More content"
      }
    ],
    "profile": {
      "bio": "Software Developer",
      "website": "https://example.com"
    }
  }
}
```

### Per-Record Operation Control with `_request`

Control individual operations for each nested record:

```json
{
  "operation": "update",
  "data": {
    "name": "John Updated",
    "posts": [
      {
        "_request": "insert",
        "title": "New Post",
        "content": "Fresh content"
      },
      {
        "_request": "update",
        "id": 456,
        "title": "Updated Post Title"
      },
      {
        "_request": "delete",
        "id": 789
      }
    ]
  }
}
```

**Supported `_request` values**:
* `insert` - Create a new related record
* `update` - Update an existing related record
* `delete` - Delete a related record
* `upsert` - Create if doesn't exist, update if exists

**How It Works**:
1. Automatic foreign key resolution - parent IDs propagate to children
2. Recursive processing - handles nested relationships at any depth
3. Transaction safety - all operations execute atomically
4. Relationship detection - automatically detects belongsTo, hasMany, hasOne, many2many
5. Flexible operations - mix create, update, and delete in one request

## Computed Columns

Define virtual columns using SQL expressions:

```json
{
  "operation": "read",
  "options": {
    "columns": ["id", "first_name", "last_name"],
    "computedColumns": [
      {
        "name": "full_name",
        "expression": "CONCAT(first_name, ' ', last_name)"
      },
      {
        "name": "age_years",
        "expression": "EXTRACT(YEAR FROM AGE(birth_date))"
      }
    ]
  }
}
```

## Custom Operators

Add custom SQL conditions when standard filters aren't sufficient:

```json
{
  "operation": "read",
  "options": {
    "customOperators": [
      {
        "name": "email_domain_filter",
        "sql": "LOWER(email) LIKE '%@example.com'"
      },
      {
        "name": "recent_records",
        "sql": "created_at > NOW() - INTERVAL '7 days'"
      },
      {
        "name": "complex_condition",
        "sql": "(status = 'active' AND score > 100) OR (status = 'pending' AND priority = 'high')"
      }
    ]
  }
}
```

**Note:** Custom operators are applied as WHERE conditions. Make sure to properly escape and sanitize any user input to prevent SQL injection.

## Lifecycle Hooks

Register hooks for all CRUD operations:

```go
import "github.com/bitechdev/ResolveSpec/pkg/resolvespec"

// Create handler
handler := resolvespec.NewHandlerWithGORM(db)

// Register a before-read hook (e.g., for authorization)
handler.Hooks().Register(resolvespec.BeforeRead, func(ctx *resolvespec.HookContext) error {
    // Check permissions
    if !userHasPermission(ctx.Context, ctx.Entity) {
        return fmt.Errorf("unauthorized access to %s", ctx.Entity)
    }

    // Modify query options
    if ctx.Options.Limit == nil || *ctx.Options.Limit > 100 {
        ctx.Options.Limit = ptr(100) // Enforce max limit
    }

    return nil
})

// Register an after-read hook (e.g., for data transformation)
handler.Hooks().Register(resolvespec.AfterRead, func(ctx *resolvespec.HookContext) error {
    // Transform or filter results
    if users, ok := ctx.Result.([]User); ok {
        for i := range users {
            users[i].Email = maskEmail(users[i].Email)
        }
    }
    return nil
})

// Register a before-create hook (e.g., for validation)
handler.Hooks().Register(resolvespec.BeforeCreate, func(ctx *resolvespec.HookContext) error {
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

## Model Registration

```go
type User struct {
    ID        uint      `json:"id" gorm:"primaryKey"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    Status    string    `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    Posts     []Post    `json:"posts,omitempty" gorm:"foreignKey:UserID"`
    Profile   *Profile  `json:"profile,omitempty" gorm:"foreignKey:UserID"`
}

type Post struct {
    ID        uint      `json:"id" gorm:"primaryKey"`
    UserID    uint      `json:"user_id"`
    Title     string    `json:"title"`
    Content   string    `json:"content"`
    Status    string    `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    Tags      []Tag     `json:"tags,omitempty" gorm:"many2many:post_tags"`
}

// Schema.Table format
handler.registry.RegisterModel("core.users", &User{})
handler.registry.RegisterModel("core.posts", &Post{})
```

## Complete Example

```go
package main

import (
    "log"
    "net/http"

    "github.com/bitechdev/ResolveSpec/pkg/resolvespec"
    "github.com/gorilla/mux"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

type User struct {
    ID     uint   `json:"id" gorm:"primaryKey"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Status string `json:"status"`
    Posts  []Post `json:"posts,omitempty" gorm:"foreignKey:UserID"`
}

type Post struct {
    ID      uint   `json:"id" gorm:"primaryKey"`
    UserID  uint   `json:"user_id"`
    Title   string `json:"title"`
    Content string `json:"content"`
    Status  string `json:"status"`
}

func main() {
    // Connect to database
    db, err := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})
    if err != nil {
        log.Fatal(err)
    }

    // Create handler
    handler := resolvespec.NewHandlerWithGORM(db)

    // Register models
    handler.registry.RegisterModel("core.users", &User{})
    handler.registry.RegisterModel("core.posts", &Post{})

    // Add hooks
    handler.Hooks().Register(resolvespec.BeforeRead, func(ctx *resolvespec.HookContext) error {
        log.Printf("Reading %s", ctx.Entity)
        return nil
    })

    // Setup routes
    router := mux.NewRouter()
    resolvespec.SetupMuxRoutes(router, handler, nil)

    // Start server
    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Testing

ResolveSpec is designed for testability:

```go
import (
    "bytes"
    "encoding/json"
    "net/http/httptest"
    "testing"
)

func TestUserRead(t *testing.T) {
    handler := resolvespec.NewHandlerWithGORM(testDB)
    handler.registry.RegisterModel("core.users", &User{})

    reqBody := map[string]interface{}{
        "operation": "read",
        "options": map[string]interface{}{
            "columns": []string{"id", "name"},
            "limit":   10,
        },
    }

    body, _ := json.Marshal(reqBody)
    req := httptest.NewRequest("POST", "/core/users", bytes.NewReader(body))
    rec := httptest.NewRecorder()

    // Test your handler...
}
```

## Router Integration

### Gorilla Mux

```go
router := mux.NewRouter()
resolvespec.SetupMuxRoutes(router, handler, nil)
```

### BunRouter

```go
router := bunrouter.New()
resolvespec.SetupBunRouterWithResolveSpec(router, handler)
```

### Custom Routers

```go
// Implement custom integration using common.Request and common.ResponseWriter
router.POST("/:schema/:entity", func(w http.ResponseWriter, r *http.Request) {
    params := extractParams(r) // Your param extraction logic
    reqAdapter := router.NewHTTPRequest(r)
    respAdapter := router.NewHTTPResponseWriter(w)
    handler.Handle(respAdapter, reqAdapter, params)
})
```

## Response Format

### Success Response

```json
{
  "success": true,
  "data": [...],
  "metadata": {
    "total": 100,
    "filtered": 50,
    "limit": 10,
    "offset": 0
  }
}
```

### Error Response

```json
{
  "success": false,
  "error": {
    "code": "validation_error",
    "message": "Invalid request",
    "details": "..."
  }
}
```

## See Also

* [Main README](../../README.md) - ResolveSpec overview
* [RestHeadSpec Package](../restheadspec/README.md) - Header-based API
* [StaticWeb Package](../server/staticweb/README.md) - Static file server

## License

This package is part of ResolveSpec and is licensed under the MIT License.
