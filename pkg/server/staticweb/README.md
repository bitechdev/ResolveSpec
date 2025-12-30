# StaticWeb - Interface-Driven Static File Server

StaticWeb is a flexible, interface-driven Go package for serving static files over HTTP. It supports multiple filesystem backends (local, zip, embedded) and provides pluggable policies for caching, MIME types, and fallback strategies.

## Features

- **Router-Agnostic**: Works with any HTTP router through standard `http.Handler`
- **Multiple Filesystem Providers**: Local directories, zip files, embedded filesystems
- **Pluggable Policies**: Customizable cache, MIME type, and fallback strategies
- **Thread-Safe**: Safe for concurrent use
- **Resource Management**: Proper lifecycle management with `Close()` methods
- **Extensible**: Easy to add new providers and policies

## Installation

```bash
go get github.com/bitechdev/ResolveSpec/pkg/server/staticweb
```

## Quick Start

### Basic Usage

Serve files from a local directory:

```go
import "github.com/bitechdev/ResolveSpec/pkg/server/staticweb"

// Create service
service := staticweb.NewService(nil)

// Mount a local directory
provider, _ := staticweb.LocalProvider("./public")
service.Mount(staticweb.MountConfig{
    URLPrefix: "/static",
    Provider:  provider,
})

// Use with any router
router.PathPrefix("/").Handler(service.Handler())
```

### Single Page Application (SPA)

Serve an SPA with HTML fallback routing:

```go
service := staticweb.NewService(nil)

provider, _ := staticweb.LocalProvider("./dist")
service.Mount(staticweb.MountConfig{
    URLPrefix:        "/",
    Provider:         provider,
    FallbackStrategy: staticweb.HTMLFallback("index.html"),
})

// API routes take precedence (registered first)
router.HandleFunc("/api/users", usersHandler)
router.HandleFunc("/api/posts", postsHandler)

// Static files handle all other routes
router.PathPrefix("/").Handler(service.Handler())
```

## Filesystem Providers

### Local Directory

Serve files from a local filesystem directory:

```go
provider, err := staticweb.LocalProvider("/var/www/static")
```

### Zip File

Serve files from a zip archive:

```go
provider, err := staticweb.ZipProvider("./static.zip")
```

### Embedded Filesystem

Serve files from Go's embedded filesystem:

```go
//go:embed assets
var assets embed.FS

// Direct embedded FS
provider, err := staticweb.EmbedProvider(&assets, "")

// Or from a zip file within embedded FS
provider, err := staticweb.EmbedProvider(&assets, "assets.zip")
```

## Cache Policies

### Simple Cache

Single TTL for all files:

```go
cachePolicy := staticweb.SimpleCache(3600) // 1 hour
```

### Extension-Based Cache

Different TTLs per file type:

```go
rules := map[string]int{
    ".html": 3600,   // 1 hour
    ".js":   86400,  // 1 day
    ".css":  86400,  // 1 day
    ".png":  604800, // 1 week
}

cachePolicy := staticweb.ExtensionCache(rules, 3600) // default 1 hour
```

### No Cache

Disable caching entirely:

```go
cachePolicy := staticweb.NoCache()
```

## Fallback Strategies

### HTML Fallback

Serve index.html for non-asset requests (SPA routing):

```go
fallback := staticweb.HTMLFallback("index.html")
```

### Extension-Based Fallback

Skip fallback for known static assets:

```go
fallback := staticweb.DefaultExtensionFallback("index.html")
```

Custom extensions:

```go
staticExts := []string{".js", ".css", ".png", ".jpg"}
fallback := staticweb.ExtensionFallback(staticExts, "index.html")
```

## Configuration

### Service Configuration

```go
config := &staticweb.ServiceConfig{
    DefaultCacheTime: 3600,
    DefaultMIMETypes: map[string]string{
        ".webp": "image/webp",
        ".wasm": "application/wasm",
    },
}

service := staticweb.NewService(config)
```

### Mount Configuration

```go
service.Mount(staticweb.MountConfig{
    URLPrefix:        "/static",
    Provider:         provider,
    CachePolicy:      cachePolicy,      // Optional
    MIMEResolver:     mimeResolver,     // Optional
    FallbackStrategy: fallbackStrategy, // Optional
})
```

## Advanced Usage

### Multiple Mount Points

Serve different directories at different URL prefixes with different policies:

```go
service := staticweb.NewService(nil)

// Long-lived assets
assetsProvider, _ := staticweb.LocalProvider("./assets")
service.Mount(staticweb.MountConfig{
    URLPrefix:   "/assets",
    Provider:    assetsProvider,
    CachePolicy: staticweb.SimpleCache(604800), // 1 week
})

// Frequently updated HTML
htmlProvider, _ := staticweb.LocalProvider("./public")
service.Mount(staticweb.MountConfig{
    URLPrefix:   "/",
    Provider:    htmlProvider,
    CachePolicy: staticweb.SimpleCache(300), // 5 minutes
})
```

### Custom MIME Types

```go
mimeResolver := staticweb.DefaultMIMEResolver()
mimeResolver.RegisterMIMEType(".webp", "image/webp")
mimeResolver.RegisterMIMEType(".wasm", "application/wasm")

service.Mount(staticweb.MountConfig{
    URLPrefix:    "/static",
    Provider:     provider,
    MIMEResolver: mimeResolver,
})
```

### Resource Cleanup

Always close the service when done:

```go
service := staticweb.NewService(nil)
defer service.Close()

// ... mount and use service ...
```

Or unmount individual mount points:

```go
service.Unmount("/static")
```

### Reloading/Refreshing Content

Reload providers to pick up changes from the underlying filesystem. This is particularly useful for zip files in development:

```go
// When zip file or directory contents change
err := service.Reload()
if err != nil {
    log.Printf("Failed to reload: %v", err)
}
```

Providers that support reloading:
- **ZipFSProvider**: Reopens the zip file to pick up changes
- **LocalFSProvider**: Refreshes the directory view (automatically picks up changes)
- **EmbedFSProvider**: Not reloadable (embedded at compile time)

You can also reload individual providers:

```go
if reloadable, ok := provider.(staticweb.ReloadableProvider); ok {
    err := reloadable.Reload()
    if err != nil {
        log.Printf("Failed to reload: %v", err)
    }
}
```

**Development Workflow Example:**

```go
service := staticweb.NewService(nil)

provider, _ := staticweb.ZipProvider("./dist.zip")
service.Mount(staticweb.MountConfig{
    URLPrefix: "/app",
    Provider:  provider,
})

// In development, reload when dist.zip is rebuilt
go func() {
    watcher := fsnotify.NewWatcher()
    watcher.Add("./dist.zip")

    for range watcher.Events {
        log.Println("Reloading static files...")
        if err := service.Reload(); err != nil {
            log.Printf("Reload failed: %v", err)
        }
    }
}()
```

## Router Integration

### Gorilla Mux

```go
router := mux.NewRouter()
router.HandleFunc("/api/users", usersHandler)
router.PathPrefix("/").Handler(service.Handler())
```

### Standard http.ServeMux

```go
http.Handle("/api/", apiHandler)
http.Handle("/", service.Handler())
```

### BunRouter

```go
router.GET("/api/users", usersHandler)
router.GET("/*path", bunrouter.HTTPHandlerFunc(service.Handler()))
```

## Architecture

### Core Interfaces

#### FileSystemProvider

Abstracts the source of files:

```go
type FileSystemProvider interface {
    Open(name string) (fs.File, error)
    Close() error
    Type() string
}
```

Implementations:
- `LocalFSProvider` - Local directories
- `ZipFSProvider` - Zip archives
- `EmbedFSProvider` - Embedded filesystems

#### CachePolicy

Defines caching behavior:

```go
type CachePolicy interface {
    GetCacheTime(path string) int
    GetCacheHeaders(path string) map[string]string
}
```

Implementations:
- `SimpleCachePolicy` - Single TTL
- `ExtensionBasedCachePolicy` - Per-extension TTL
- `NoCachePolicy` - Disable caching

#### FallbackStrategy

Handles missing files:

```go
type FallbackStrategy interface {
    ShouldFallback(path string) bool
    GetFallbackPath(path string) string
}
```

Implementations:
- `NoFallback` - Return 404
- `HTMLFallbackStrategy` - SPA routing
- `ExtensionBasedFallback` - Skip known assets

#### MIMETypeResolver

Determines Content-Type:

```go
type MIMETypeResolver interface {
    GetMIMEType(path string) string
    RegisterMIMEType(extension, mimeType string)
}
```

Implementations:
- `DefaultMIMEResolver` - Common web types
- `ConfigurableMIMEResolver` - Custom mappings

## Testing

### Mock Providers

```go
import staticwebtesting "github.com/bitechdev/ResolveSpec/pkg/server/staticweb/testing"

provider := staticwebtesting.NewMockProvider(map[string][]byte{
    "index.html": []byte("<html>test</html>"),
    "app.js":     []byte("console.log('test')"),
})

service.Mount(staticweb.MountConfig{
    URLPrefix: "/",
    Provider:  provider,
})
```

### Test Helpers

```go
req := httptest.NewRequest("GET", "/index.html", nil)
rec := httptest.NewRecorder()

service.Handler().ServeHTTP(rec, req)

// Assert response
assert.Equal(t, 200, rec.Code)
```

## Future Features

The interface-driven design allows for easy extensibility:

### Planned Providers

- **HTTPFSProvider**: Fetch files from remote HTTP servers with local caching
- **S3FSProvider**: Serve files from S3-compatible storage
- **CompositeProvider**: Fallback chain across multiple providers
- **MemoryProvider**: In-memory filesystem for testing

### Planned Policies

- **TimedCachePolicy**: Different cache times by time of day
- **ConditionalCachePolicy**: Smart cache based on file size/type
- **RegexFallbackStrategy**: Pattern-based routing

## License

See the main repository for license information.

## Contributing

Contributions are welcome! The interface-driven design makes it easy to add new providers and policies without modifying existing code.
