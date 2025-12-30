# StaticWeb Package Interface-Driven Refactoring Plan

## Overview
Refactor `pkg/server/staticweb` to be interface-driven, router-agnostic, and maintainable. This is a breaking change that replaces the existing API with a cleaner design.

## User Requirements
- ✅ Work with any server (not just Gorilla mux)
- ✅ Serve static files from zip files or directories
- ✅ Support embedded, local filesystems
- ✅ Interface-driven and maintainable architecture
- ✅ Struct-based configuration
- ✅ Breaking changes acceptable
- 🔮 Remote HTTP/HTTPS and S3 support (future feature)

## Design Principles
1. **Interface-first**: Define clear interfaces for all abstractions
2. **Composition over inheritance**: Combine small, focused components
3. **Router-agnostic**: Return standard `http.Handler` for universal compatibility
4. **Configurable policies**: Extract hardcoded behavior into pluggable strategies
5. **Resource safety**: Proper lifecycle management with `Close()` methods
6. **Testability**: Mock-friendly interfaces with clear boundaries
7. **Extensibility**: Easy to add new providers (HTTP, S3, etc.) in future

## Architecture Overview

### Core Interfaces (pkg/server/staticweb/interfaces.go)

```go
// FileSystemProvider abstracts file sources (local, zip, embedded, future: http, s3)
type FileSystemProvider interface {
    Open(name string) (fs.File, error)
    Close() error
    Type() string
}

// CachePolicy defines caching behavior
type CachePolicy interface {
    GetCacheTime(path string) int
    GetCacheHeaders(path string) map[string]string
}

// MIMETypeResolver determines content types
type MIMETypeResolver interface {
    GetMIMEType(path string) string
    RegisterMIMEType(extension, mimeType string)
}

// FallbackStrategy handles missing files
type FallbackStrategy interface {
    ShouldFallback(path string) bool
    GetFallbackPath(path string) string
}

// StaticFileService manages mount points
type StaticFileService interface {
    Mount(config MountConfig) error
    Unmount(urlPrefix string) error
    ListMounts() []string
    Reload() error
    Close() error
    Handler() http.Handler  // Router-agnostic integration
}
```

### Configuration (pkg/server/staticweb/config.go)

```go
// MountConfig configures a single mount point
type MountConfig struct {
    URLPrefix        string
    Provider         FileSystemProvider
    CachePolicy      CachePolicy      // Optional, uses default if nil
    MIMEResolver     MIMETypeResolver // Optional, uses default if nil
    FallbackStrategy FallbackStrategy // Optional, no fallback if nil
}

// ServiceConfig configures the service
type ServiceConfig struct {
    DefaultCacheTime int                    // Default: 48 hours
    DefaultMIMETypes map[string]string      // Additional MIME types
}
```

## Implementation Plan

### Step 1: Create Core Interfaces
**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/interfaces.go` (NEW)

Define all interfaces listed above. This establishes the contract for all components.

### Step 2: Implement Default Policies
**Directory**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/policies/` (NEW)

**File**: `policies/cache.go`
- `SimpleCachePolicy` - Single TTL for all files
- `ExtensionBasedCachePolicy` - Different TTL per file extension
- `NoCachePolicy` - Disables caching

**File**: `policies/mime.go`
- `DefaultMIMEResolver` - Standard web MIME types + stdlib
- `ConfigurableMIMEResolver` - User-defined mappings
- Migrate hardcoded MIME types from `InitMimeTypes()` (lines 60-76)

**File**: `policies/fallback.go`
- `NoFallback` - Returns 404 for missing files
- `HTMLFallbackStrategy` - SPA routing (serves index.html)
- `ExtensionBasedFallback` - Current behavior (checks extensions)
- Migrate logic from `StaticHTMLFallbackHandler()` (lines 241-285)

### Step 3: Implement FileSystem Providers
**Directory**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/providers/` (NEW)

**File**: `providers/local.go`
```go
type LocalFSProvider struct {
    path string
    fs   fs.FS
}

func NewLocalFSProvider(path string) (*LocalFSProvider, error)
func (l *LocalFSProvider) Open(name string) (fs.File, error)
func (l *LocalFSProvider) Close() error
func (l *LocalFSProvider) Type() string
```

**File**: `providers/zip.go`
```go
type ZipFSProvider struct {
    zipPath   string
    zipReader *zip.ReadCloser
    zipFS     *zipfs.ZipFS
    mu        sync.RWMutex
}

func NewZipFSProvider(zipPath string) (*ZipFSProvider, error)
func (z *ZipFSProvider) Open(name string) (fs.File, error)
func (z *ZipFSProvider) Close() error
func (z *ZipFSProvider) Type() string
```
- Integrates with existing `pkg/server/zipfs/zipfs.go`
- Manages zip file lifecycle properly

**File**: `providers/embed.go`
```go
type EmbedFSProvider struct {
    embedFS   *embed.FS
    zipFile   string  // Optional: path within embedded FS to zip file
    zipReader *zip.ReadCloser
    fs        fs.FS
    mu        sync.RWMutex
}

func NewEmbedFSProvider(embedFS *embed.FS, zipFile string) (*EmbedFSProvider, error)
func (e *EmbedFSProvider) Open(name string) (fs.File, error)
func (e *EmbedFSProvider) Close() error
func (e *EmbedFSProvider) Type() string
```

### Step 4: Implement Mount Point
**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/mount.go` (NEW)

```go
type mountPoint struct {
    urlPrefix        string
    provider         FileSystemProvider
    cachePolicy      CachePolicy
    mimeResolver     MIMETypeResolver
    fallbackStrategy FallbackStrategy
}

func newMountPoint(config MountConfig, defaults *ServiceConfig) (*mountPoint, error)
func (m *mountPoint) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (m *mountPoint) Close() error
```

**Key behaviors**:
- Strips URL prefix before passing to provider
- Applies cache headers via `CachePolicy`
- Sets Content-Type via `MIMETypeResolver`
- Falls back via `FallbackStrategy` if file not found
- Integrates with `http.FileServer()` for actual serving

### Step 5: Implement Service
**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/service.go` (NEW)

```go
type service struct {
    mounts  map[string]*mountPoint  // urlPrefix -> mount
    config  *ServiceConfig
    mu      sync.RWMutex
}

func NewService(config *ServiceConfig) StaticFileService
func (s *service) Mount(config MountConfig) error
func (s *service) Unmount(urlPrefix string) error
func (s *service) ListMounts() []string
func (s *service) Reload() error
func (s *service) Close() error
func (s *service) Handler() http.Handler
```

**Handler Implementation**:
- Performs longest-prefix matching to find mount point
- Delegates to mount point's `ServeHTTP()`
- Returns silently if no match (allows API routes to handle)
- Thread-safe with `sync.RWMutex`

### Step 6: Create Configuration Helpers
**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/config.go` (NEW)

```go
// Default configurations
func DefaultServiceConfig() *ServiceConfig
func DefaultCachePolicy() CachePolicy
func DefaultMIMEResolver() MIMETypeResolver

// Helper constructors
func LocalProvider(path string) FileSystemProvider
func ZipProvider(zipPath string) FileSystemProvider
func EmbedProvider(embedFS *embed.FS, zipFile string) FileSystemProvider

// Policy constructors
func SimpleCache(seconds int) CachePolicy
func ExtensionCache(rules map[string]int) CachePolicy
func HTMLFallback(indexFile string) FallbackStrategy
func ExtensionFallback(staticExtensions []string) FallbackStrategy
```

### Step 7: Update/Remove Existing Files

**REMOVE**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/staticweb.go`
- All functionality migrated to new interface-based design
- No backward compatibility needed per user request

**KEEP**: `/home/hein/hein/dev/ResolveSpec/pkg/server/zipfs/zipfs.go`
- Still used by `ZipFSProvider`
- Already implements `fs.FS` interface correctly

### Step 8: Create Examples and Tests

**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/example_test.go` (NEW)

```go
func ExampleService_basic() { /* Serve local directory */ }
func ExampleService_spa() { /* SPA with fallback */ }
func ExampleService_multiple() { /* Multiple mount points */ }
func ExampleService_zip() { /* Serve from zip file */ }
func ExampleService_embedded() { /* Serve from embedded zip */ }
```

**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/service_test.go` (NEW)
- Test mount/unmount operations
- Test longest-prefix matching
- Test concurrent access
- Test resource cleanup

**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/providers/providers_test.go` (NEW)
- Test each provider implementation
- Test resource cleanup
- Test error handling

**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/testing/mocks.go` (NEW)
- `MockFileSystemProvider` - In-memory file storage
- `MockCachePolicy` - Configurable cache behavior
- `MockMIMEResolver` - Custom MIME mappings
- Test helpers for common scenarios

### Step 9: Create Documentation

**File**: `/home/hein/hein/dev/ResolveSpec/pkg/server/staticweb/README.md` (NEW)

Document:
- Quick start examples
- Interface overview
- Provider implementations
- Policy customization
- Router integration patterns
- Migration guide from old API
- Future features roadmap

## Key Improvements Over Current Implementation

### 1. Router-Agnostic Design
**Before**: Coupled to Gorilla mux via `RegisterRoutes(*mux.Router)`
**After**: Returns `http.Handler`, works with any router

```go
// Works with Gorilla Mux
muxRouter.PathPrefix("/").Handler(service.Handler())

// Works with standard http.ServeMux
http.Handle("/", service.Handler())

// Works with any http.Handler-compatible router
```

### 2. Configurable Behaviors
**Before**: Hardcoded MIME types, cache times, file extensions
**After**: Pluggable policies

```go
// Custom cache per file type
cachePolicy := ExtensionCache(map[string]int{
    ".html": 3600,      // 1 hour
    ".js":   86400,     // 1 day
    ".css":  86400,     // 1 day
    ".png":  604800,    // 1 week
})

// Custom fallback logic
fallback := HTMLFallback("index.html")

service.Mount(MountConfig{
    URLPrefix:        "/",
    Provider:         LocalProvider("./dist"),
    CachePolicy:      cachePolicy,
    FallbackStrategy: fallback,
})
```

### 3. Better Resource Management
**Before**: Manual zip file cleanup, easy to leak resources
**After**: Proper lifecycle with `Close()` on all components

```go
defer service.Close()  // Cleans up all providers
```

### 4. Testability
**Before**: Hard to test, coupled to filesystem
**After**: Mock providers for testing

```go
mockProvider := testing.NewInMemoryProvider(map[string][]byte{
    "index.html": []byte("<html>test</html>"),
})

service.Mount(MountConfig{
    URLPrefix: "/",
    Provider:  mockProvider,
})
```

### 5. Extensibility
**Before**: Need to modify code to support new file sources
**After**: Implement `FileSystemProvider` interface

```go
// Future: Add HTTP provider without changing core code
type HTTPFSProvider struct { /* ... */ }
func (h *HTTPFSProvider) Open(name string) (fs.File, error) { /* ... */ }
func (h *HTTPFSProvider) Close() error { /* ... */ }
func (h *HTTPFSProvider) Type() string { return "http" }
```

## Future Features (To Implement Later)

### Remote HTTP/HTTPS Provider
**File**: `providers/http.go` (FUTURE)

Serve static files from remote HTTP servers with local caching:

```go
type HTTPFSProvider struct {
    baseURL    string
    httpClient *http.Client
    cache      LocalCache  // Optional disk/memory cache
    cacheTTL   time.Duration
    mu         sync.RWMutex
}

// Example usage
service.Mount(MountConfig{
    URLPrefix: "/cdn",
    Provider:  HTTPProvider("https://cdn.example.com/assets"),
})
```

**Features**:
- Fetch files from remote URLs on-demand
- Local cache to reduce remote requests
- Configurable TTL and cache eviction
- HEAD request support for metadata
- Retry logic and timeout handling
- Support for authentication headers

### S3-Compatible Provider
**File**: `providers/s3.go` (FUTURE)

Serve static files from S3, MinIO, or S3-compatible storage:

```go
type S3FSProvider struct {
    bucket    string
    prefix    string
    region    string
    client    *s3.Client
    cache     LocalCache
    mu        sync.RWMutex
}

// Example usage
service.Mount(MountConfig{
    URLPrefix: "/media",
    Provider:  S3Provider("my-bucket", "static/", "us-east-1"),
})
```

**Features**:
- List and fetch objects from S3 buckets
- Support for AWS S3, MinIO, DigitalOcean Spaces, etc.
- IAM role or credential-based authentication
- Optional local caching layer
- Efficient metadata retrieval
- Support for presigned URLs

### Other Future Providers
- **GitProvider**: Serve files from Git repositories
- **MemoryProvider**: In-memory filesystem for testing/temporary files
- **ProxyProvider**: Proxy to another static file server
- **CompositeProvider**: Fallback chain across multiple providers

### Advanced Cache Policies (FUTURE)
- **TimedCachePolicy**: Different cache times by time of day
- **UserAgentCachePolicy**: Cache based on client type
- **ConditionalCachePolicy**: Smart cache based on file size/type
- **DistributedCachePolicy**: Shared cache across service instances

### Advanced Fallback Strategies (FUTURE)
- **RegexFallbackStrategy**: Pattern-based routing
- **I18nFallbackStrategy**: Language-based file resolution
- **VersionedFallbackStrategy**: A/B testing support

## Critical Files Summary

### Files to CREATE (in order):
1. `pkg/server/staticweb/interfaces.go` - Core contracts
2. `pkg/server/staticweb/config.go` - Configuration structs and helpers
3. `pkg/server/staticweb/policies/cache.go` - Cache policy implementations
4. `pkg/server/staticweb/policies/mime.go` - MIME resolver implementations
5. `pkg/server/staticweb/policies/fallback.go` - Fallback strategy implementations
6. `pkg/server/staticweb/providers/local.go` - Local directory provider
7. `pkg/server/staticweb/providers/zip.go` - Zip file provider
8. `pkg/server/staticweb/providers/embed.go` - Embedded filesystem provider
9. `pkg/server/staticweb/mount.go` - Mount point implementation
10. `pkg/server/staticweb/service.go` - Main service implementation
11. `pkg/server/staticweb/testing/mocks.go` - Test helpers
12. `pkg/server/staticweb/service_test.go` - Service tests
13. `pkg/server/staticweb/providers/providers_test.go` - Provider tests
14. `pkg/server/staticweb/example_test.go` - Example code
15. `pkg/server/staticweb/README.md` - Documentation

### Files to REMOVE:
1. `pkg/server/staticweb/staticweb.go` - Replaced by new design

### Files to KEEP:
1. `pkg/server/zipfs/zipfs.go` - Used by ZipFSProvider

### Files for FUTURE (not in this refactoring):
1. `pkg/server/staticweb/providers/http.go` - HTTP/HTTPS remote provider
2. `pkg/server/staticweb/providers/s3.go` - S3-compatible storage provider
3. `pkg/server/staticweb/providers/composite.go` - Fallback chain provider

## Example Usage After Refactoring

### Basic Static Site
```go
service := staticweb.NewService(nil)  // Use defaults

err := service.Mount(staticweb.MountConfig{
    URLPrefix: "/static",
    Provider:  staticweb.LocalProvider("./public"),
})

muxRouter.PathPrefix("/").Handler(service.Handler())
```

### SPA with API Routes
```go
service := staticweb.NewService(nil)

service.Mount(staticweb.MountConfig{
    URLPrefix:        "/",
    Provider:         staticweb.LocalProvider("./dist"),
    FallbackStrategy: staticweb.HTMLFallback("index.html"),
})

// API routes take precedence (registered first)
muxRouter.HandleFunc("/api/users", usersHandler)
muxRouter.HandleFunc("/api/posts", postsHandler)

// Static files handle all other routes
muxRouter.PathPrefix("/").Handler(service.Handler())
```

### Multiple Mount Points with Different Policies
```go
service := staticweb.NewService(&staticweb.ServiceConfig{
    DefaultCacheTime: 3600,
})

// Assets with long cache
service.Mount(staticweb.MountConfig{
    URLPrefix:   "/assets",
    Provider:    staticweb.LocalProvider("./assets"),
    CachePolicy: staticweb.SimpleCache(604800), // 1 week
})

// HTML with short cache
service.Mount(staticweb.MountConfig{
    URLPrefix:   "/",
    Provider:    staticweb.LocalProvider("./public"),
    CachePolicy: staticweb.SimpleCache(300), // 5 minutes
})

router.PathPrefix("/").Handler(service.Handler())
```

### Embedded Files from Zip
```go
//go:embed assets.zip
var assetsZip embed.FS

service := staticweb.NewService(nil)

service.Mount(staticweb.MountConfig{
    URLPrefix: "/static",
    Provider:  staticweb.EmbedProvider(&assetsZip, "assets.zip"),
})

router.PathPrefix("/").Handler(service.Handler())
```

### Future: CDN Fallback (when HTTP provider is implemented)
```go
// Primary CDN with local fallback
service.Mount(staticweb.MountConfig{
    URLPrefix: "/static",
    Provider:  staticweb.CompositeProvider(
        staticweb.HTTPProvider("https://cdn.example.com/assets"),
        staticweb.LocalProvider("./public/assets"),
    ),
})
```

## Testing Strategy

### Unit Tests
- Each provider implementation independently
- Each policy implementation independently
- Mount point request handling
- Service mount/unmount operations

### Integration Tests
- Full request flow through service
- Multiple mount points
- Longest-prefix matching
- Resource cleanup

### Example Tests
- Executable examples in `example_test.go`
- Demonstrate common usage patterns

## Migration Impact

### Breaking Changes
- Complete API redesign (acceptable per user)
- Package not currently used in codebase (no migration needed)
- New consumers will use new API from start

### Future Extensibility
The interface-driven design allows future additions without breaking changes:
- Add HTTPFSProvider by implementing `FileSystemProvider`
- Add S3FSProvider by implementing `FileSystemProvider`
- Add custom cache policies by implementing `CachePolicy`
- Add custom fallback strategies by implementing `FallbackStrategy`

## Implementation Order
1. Interfaces (foundation)
2. Configuration (API surface)
3. Policies (pluggable behavior)
4. Providers (filesystem abstraction)
5. Mount Point (request handling)
6. Service (orchestration)
7. Tests (validation)
8. Documentation (usage)
9. Remove old code (cleanup)

This order ensures each layer builds on tested, working components.

---

