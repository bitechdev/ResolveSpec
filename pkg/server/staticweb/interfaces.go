package staticweb

import (
	"io/fs"
	"net/http"
)

// FileSystemProvider abstracts the source of files (local, zip, embedded, future: http, s3)
// Implementations must be safe for concurrent use.
type FileSystemProvider interface {
	// Open opens the named file.
	// The name is always a slash-separated path relative to the filesystem root.
	Open(name string) (fs.File, error)

	// Close releases any resources held by the provider.
	// After Close is called, the provider should not be used.
	Close() error

	// Type returns the provider type (e.g., "local", "zip", "embed", "http", "s3").
	// This is primarily for debugging and logging purposes.
	Type() string
}

// ReloadableProvider is an optional interface that providers can implement
// to support reloading/refreshing their content.
// This is useful for development workflows where the underlying files may change.
type ReloadableProvider interface {
	FileSystemProvider

	// Reload refreshes the provider's content from the underlying source.
	// For zip files, this reopens the zip archive.
	// For local directories, this refreshes the filesystem view.
	// Returns an error if the reload fails.
	Reload() error
}

// CachePolicy defines how files should be cached by browsers and proxies.
// Implementations must be safe for concurrent use.
type CachePolicy interface {
	// GetCacheTime returns the cache duration in seconds for the given path.
	// A value of 0 means no caching.
	// A negative value can be used to indicate browser should revalidate.
	GetCacheTime(path string) int

	// GetCacheHeaders returns additional cache-related HTTP headers for the given path.
	// Common headers include "Cache-Control", "Expires", "ETag", etc.
	// Returns nil if no additional headers are needed.
	GetCacheHeaders(path string) map[string]string
}

// MIMETypeResolver determines the Content-Type for files.
// Implementations must be safe for concurrent use.
type MIMETypeResolver interface {
	// GetMIMEType returns the MIME type for the given file path.
	// Returns empty string if the MIME type cannot be determined.
	GetMIMEType(path string) string

	// RegisterMIMEType registers a custom MIME type for the given file extension.
	// The extension should include the leading dot (e.g., ".webp").
	RegisterMIMEType(extension, mimeType string)
}

// FallbackStrategy handles requests for files that don't exist.
// This is commonly used for Single Page Applications (SPAs) that use client-side routing.
// Implementations must be safe for concurrent use.
type FallbackStrategy interface {
	// ShouldFallback determines if a fallback should be attempted for the given path.
	// Returns true if the request should be handled by fallback logic.
	ShouldFallback(path string) bool

	// GetFallbackPath returns the path to serve instead of the originally requested path.
	// This is only called if ShouldFallback returns true.
	GetFallbackPath(path string) string
}

// MountConfig configures a single mount point.
// A mount point connects a URL prefix to a filesystem provider with optional policies.
type MountConfig struct {
	// URLPrefix is the URL path prefix where the filesystem should be mounted.
	// Must start with "/" (e.g., "/static", "/", "/assets").
	// Requests starting with this prefix will be handled by this mount point.
	URLPrefix string

	// Provider is the filesystem provider that supplies the files.
	// Required.
	Provider FileSystemProvider

	// CachePolicy determines how files should be cached.
	// If nil, the service's default cache policy is used.
	CachePolicy CachePolicy

	// MIMEResolver determines Content-Type headers for files.
	// If nil, the service's default MIME resolver is used.
	MIMEResolver MIMETypeResolver

	// FallbackStrategy handles requests for missing files.
	// If nil, no fallback is performed and 404 responses are returned.
	FallbackStrategy FallbackStrategy
}

// StaticFileService manages multiple mount points and serves static files.
// The service is safe for concurrent use.
type StaticFileService interface {
	// Mount adds a new mount point with the given configuration.
	// Returns an error if the URLPrefix is already mounted or if the config is invalid.
	Mount(config MountConfig) error

	// Unmount removes the mount point at the given URL prefix.
	// Returns an error if no mount point exists at that prefix.
	// Automatically calls Close() on the provider to release resources.
	Unmount(urlPrefix string) error

	// ListMounts returns a sorted list of all mounted URL prefixes.
	ListMounts() []string

	// Reload reinitializes all filesystem providers.
	// This can be used to pick up changes in the underlying filesystems.
	// Not all providers may support reloading.
	Reload() error

	// Close releases all resources held by the service and all mounted providers.
	// After Close is called, the service should not be used.
	Close() error

	// Handler returns an http.Handler that serves static files from all mount points.
	// The handler performs longest-prefix matching to find the appropriate mount point.
	// If no mount point matches, the handler returns without writing a response,
	// allowing other handlers (like API routes) to process the request.
	Handler() http.Handler
}
