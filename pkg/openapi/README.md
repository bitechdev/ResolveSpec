# OpenAPI Generator for ResolveSpec

This package provides automatic OpenAPI 3.0 specification generation for ResolveSpec, RestheadSpec, and FuncSpec API frameworks.

## Features

- **Automatic Schema Generation**: Generates OpenAPI schemas from Go struct models
- **Multiple Framework Support**: Works with RestheadSpec, ResolveSpec, and FuncSpec
- **Dynamic Endpoint Discovery**: Automatically discovers all registered models and generates paths
- **Query Parameter Access**: Access spec via `?openapi` on any endpoint or via `/openapi`
- **Comprehensive Documentation**: Includes all request/response schemas, parameters, and security schemes

## Quick Start

### RestheadSpec Example

```go
import (
    "github.com/bitechdev/ResolveSpec/pkg/openapi"
    "github.com/bitechdev/ResolveSpec/pkg/restheadspec"
    "github.com/gorilla/mux"
)

func main() {
    // 1. Create handler
    handler := restheadspec.NewHandlerWithGORM(db)

    // 2. Register models
    handler.registry.RegisterModel("public.users", User{})
    handler.registry.RegisterModel("public.products", Product{})

    // 3. Configure OpenAPI generator
    handler.SetOpenAPIGenerator(func() (string, error) {
        generator := openapi.NewGenerator(openapi.GeneratorConfig{
            Title:       "My API",
            Description: "API documentation",
            Version:     "1.0.0",
            BaseURL:     "http://localhost:8080",
            Registry:    handler.registry.(*modelregistry.DefaultModelRegistry),
            IncludeRestheadSpec: true,
            IncludeResolveSpec:  false,
            IncludeFuncSpec:     false,
        })
        return generator.GenerateJSON()
    })

    // 4. Setup routes (automatically includes /openapi endpoint)
    router := mux.NewRouter()
    restheadspec.SetupMuxRoutes(router, handler, nil)

    // Start server
    http.ListenAndServe(":8080", router)
}
```

### ResolveSpec Example

```go
func main() {
    // 1. Create handler
    handler := resolvespec.NewHandlerWithGORM(db)

    // 2. Register models
    handler.RegisterModel("public", "users", User{})
    handler.RegisterModel("public", "products", Product{})

    // 3. Configure OpenAPI generator
    handler.SetOpenAPIGenerator(func() (string, error) {
        generator := openapi.NewGenerator(openapi.GeneratorConfig{
            Title:       "My API",
            Version:     "1.0.0",
            Registry:    handler.registry.(*modelregistry.DefaultModelRegistry),
            IncludeResolveSpec: true,
        })
        return generator.GenerateJSON()
    })

    // 4. Setup routes
    router := mux.NewRouter()
    resolvespec.SetupMuxRoutes(router, handler, nil)

    http.ListenAndServe(":8080", router)
}
```

## Accessing the OpenAPI Specification

Once configured, the OpenAPI spec is available in two ways:

### 1. Global `/openapi` Endpoint

```bash
curl http://localhost:8080/openapi
```

Returns the complete OpenAPI specification for all registered models.

### 2. Query Parameter on Any Endpoint

```bash
# RestheadSpec
curl http://localhost:8080/public/users?openapi

# ResolveSpec
curl http://localhost:8080/resolve/public/users?openapi
```

Returns the same OpenAPI specification as `/openapi`.

## Generated Endpoints

### RestheadSpec

For each registered model (e.g., `public.users`), the following paths are generated:

- `GET /public/users` - List records with header-based filtering
- `POST /public/users` - Create a new record
- `GET /public/users/{id}` - Get a single record
- `PUT /public/users/{id}` - Update a record
- `PATCH /public/users/{id}` - Partially update a record
- `DELETE /public/users/{id}` - Delete a record
- `GET /public/users/metadata` - Get table metadata
- `OPTIONS /public/users` - CORS preflight

### ResolveSpec

For each registered model (e.g., `public.users`), the following paths are generated:

- `POST /resolve/public/users` - Execute operations (read, create, meta)
- `POST /resolve/public/users/{id}` - Execute operations (update, delete)
- `GET /resolve/public/users` - Get metadata
- `OPTIONS /resolve/public/users` - CORS preflight

## Schema Generation

The generator automatically extracts information from your Go struct tags:

```go
type User struct {
    ID        int       `json:"id" gorm:"primaryKey" description:"User ID"`
    Name      string    `json:"name" gorm:"not null" description:"User's full name"`
    Email     string    `json:"email" gorm:"unique" description:"Email address"`
    CreatedAt time.Time `json:"created_at" description:"Creation timestamp"`
    Roles     []string  `json:"roles" description:"User roles"`
}
```

This generates an OpenAPI schema with:
- Property names from `json` tags
- Required fields from `gorm:"not null"` and non-pointer types
- Descriptions from `description` tags
- Proper type mappings (int → integer, time.Time → string with format: date-time, etc.)

## RestheadSpec Headers

The generator documents all RestheadSpec HTTP headers:

- `X-Filters` - JSON array of filter conditions
- `X-Columns` - Comma-separated columns to select
- `X-Sort` - JSON array of sort specifications
- `X-Limit` - Maximum records to return
- `X-Offset` - Records to skip
- `X-Preload` - Relations to eager load
- `X-Expand` - Relations to expand (LEFT JOIN)
- `X-Distinct` - Enable DISTINCT queries
- `X-Response-Format` - Response format (detail, simple, syncfusion)
- `X-Clean-JSON` - Remove null/empty fields
- `X-Custom-SQL-Where` - Custom WHERE clause (AND)
- `X-Custom-SQL-Or` - Custom WHERE clause (OR)

## ResolveSpec Request Body

The generator documents the ResolveSpec request body structure:

```json
{
  "operation": "read",
  "data": {},
  "id": 123,
  "options": {
    "limit": 10,
    "offset": 0,
    "filters": [
      {"column": "status", "operator": "eq", "value": "active"}
    ],
    "sort": [
      {"column": "created_at", "direction": "desc"}
    ]
  }
}
```

## Security Schemes

The generator automatically includes common security schemes:

- **BearerAuth**: JWT Bearer token authentication
- **SessionToken**: Session token in Authorization header
- **CookieAuth**: Cookie-based session authentication
- **HeaderAuth**: Header-based user authentication (X-User-ID)

## FuncSpec Custom Endpoints

For FuncSpec, you can manually register custom SQL endpoints:

```go
funcSpecEndpoints := map[string]openapi.FuncSpecEndpoint{
    "/api/reports/sales": {
        Path:        "/api/reports/sales",
        Method:      "GET",
        Summary:     "Get sales report",
        Description: "Returns sales data for specified date range",
        SQLQuery:    "SELECT * FROM sales WHERE date BETWEEN [start_date] AND [end_date]",
        Parameters:  []string{"start_date", "end_date"},
    },
}

generator := openapi.NewGenerator(openapi.GeneratorConfig{
    // ... other config
    IncludeFuncSpec:   true,
    FuncSpecEndpoints: funcSpecEndpoints,
})
```

## Combining Multiple Frameworks

You can generate a unified OpenAPI spec that includes multiple frameworks:

```go
generator := openapi.NewGenerator(openapi.GeneratorConfig{
    Title:       "Unified API",
    Version:     "1.0.0",
    Registry:    sharedRegistry,
    IncludeRestheadSpec: true,
    IncludeResolveSpec:  true,
    IncludeFuncSpec:     true,
    FuncSpecEndpoints:   funcSpecEndpoints,
})
```

This will generate a complete spec with all endpoints from all frameworks.

## Advanced Customization

You can customize the generated spec further:

```go
handler.SetOpenAPIGenerator(func() (string, error) {
    generator := openapi.NewGenerator(config)

    // Generate initial spec
    spec, err := generator.Generate()
    if err != nil {
        return "", err
    }

    // Add contact information
    spec.Info.Contact = &openapi.Contact{
        Name:  "API Support",
        Email: "support@example.com",
        URL:   "https://example.com/support",
    }

    // Add additional servers
    spec.Servers = append(spec.Servers, openapi.Server{
        URL:         "https://staging.example.com",
        Description: "Staging Server",
    })

    // Convert back to JSON
    data, _ := json.MarshalIndent(spec, "", "  ")
    return string(data), nil
})
```

## Using with Swagger UI

You can serve the generated OpenAPI spec with Swagger UI:

1. Get the spec from `/openapi`
2. Load it in Swagger UI at `https://petstore.swagger.io/`
3. Or self-host Swagger UI and point it to your `/openapi` endpoint

Example with self-hosted Swagger UI:

```go
// Serve Swagger UI static files
router.PathPrefix("/swagger/").Handler(
    http.StripPrefix("/swagger/", http.FileServer(http.Dir("./swagger-ui"))),
)

// Configure Swagger UI to use /openapi
```

## Testing

You can test the OpenAPI endpoint:

```bash
# Get the full spec
curl http://localhost:8080/openapi | jq

# Validate with openapi-generator
openapi-generator validate -i http://localhost:8080/openapi

# Generate client SDKs
openapi-generator generate -i http://localhost:8080/openapi -g typescript-fetch -o ./client
```

## Complete Example

See `example.go` in this package for complete, runnable examples including:
- Basic RestheadSpec setup
- Basic ResolveSpec setup
- Combining both frameworks
- Adding FuncSpec endpoints
- Advanced customization

## License

Part of the ResolveSpec project.
