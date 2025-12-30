package staticweb

import (
	"embed"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/server/staticweb/policies"
	"github.com/bitechdev/ResolveSpec/pkg/server/staticweb/providers"
)

// ServiceConfig configures the static file service.
type ServiceConfig struct {
	// DefaultCacheTime is the default cache duration in seconds.
	// Used when a mount point doesn't specify a custom CachePolicy.
	// Default: 172800 (48 hours)
	DefaultCacheTime int

	// DefaultMIMETypes is a map of file extensions to MIME types.
	// These are added to the default MIME resolver.
	// Extensions should include the leading dot (e.g., ".webp").
	DefaultMIMETypes map[string]string
}

// DefaultServiceConfig returns a ServiceConfig with sensible defaults.
func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		DefaultCacheTime: 172800, // 48 hours
		DefaultMIMETypes: make(map[string]string),
	}
}

// Validate checks if the ServiceConfig is valid.
func (c *ServiceConfig) Validate() error {
	if c.DefaultCacheTime < 0 {
		return fmt.Errorf("DefaultCacheTime cannot be negative")
	}
	return nil
}

// Helper constructor functions for providers

// LocalProvider creates a FileSystemProvider for a local directory.
func LocalProvider(path string) (FileSystemProvider, error) {
	return providers.NewLocalFSProvider(path)
}

// ZipProvider creates a FileSystemProvider for a zip file.
func ZipProvider(zipPath string) (FileSystemProvider, error) {
	return providers.NewZipFSProvider(zipPath)
}

// EmbedProvider creates a FileSystemProvider for an embedded filesystem.
// If zipFile is empty, the embedded FS is used directly.
// If zipFile is specified, it's treated as a path to a zip file within the embedded FS.
// The embedFS parameter can be any fs.FS, but is typically *embed.FS.
func EmbedProvider(embedFS *embed.FS, zipFile string) (FileSystemProvider, error) {
	return providers.NewEmbedFSProvider(embedFS, zipFile)
}

// Policy constructor functions

// SimpleCache creates a simple cache policy with the given TTL in seconds.
func SimpleCache(seconds int) CachePolicy {
	return policies.NewSimpleCachePolicy(seconds)
}

// ExtensionCache creates an extension-based cache policy.
// rules maps file extensions (with leading dot) to cache times in seconds.
// defaultTime is used for files that don't match any rule.
func ExtensionCache(rules map[string]int, defaultTime int) CachePolicy {
	return policies.NewExtensionBasedCachePolicy(rules, defaultTime)
}

// NoCache creates a cache policy that disables all caching.
func NoCache() CachePolicy {
	return policies.NewNoCachePolicy()
}

// HTMLFallback creates a fallback strategy for SPAs that serves the given index file.
func HTMLFallback(indexFile string) FallbackStrategy {
	return policies.NewHTMLFallbackStrategy(indexFile)
}

// ExtensionFallback creates an extension-based fallback strategy.
// staticExtensions is a list of file extensions that should NOT use fallback.
// fallbackPath is the file to serve when fallback is triggered.
func ExtensionFallback(staticExtensions []string, fallbackPath string) FallbackStrategy {
	return policies.NewExtensionBasedFallback(staticExtensions, fallbackPath)
}

// DefaultExtensionFallback creates an extension-based fallback with common web asset extensions.
func DefaultExtensionFallback(fallbackPath string) FallbackStrategy {
	return policies.NewDefaultExtensionBasedFallback(fallbackPath)
}

// DefaultMIMEResolver creates a MIME resolver with common web file types.
func DefaultMIMEResolver() MIMETypeResolver {
	return policies.NewDefaultMIMEResolver()
}
