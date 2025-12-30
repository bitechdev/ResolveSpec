package policies

import (
	"path"
	"strings"
)

// NoFallback implements a fallback strategy that never falls back.
// All requests for missing files will result in 404 responses.
type NoFallback struct{}

// NewNoFallback creates a new NoFallback strategy.
func NewNoFallback() *NoFallback {
	return &NoFallback{}
}

// ShouldFallback always returns false.
func (f *NoFallback) ShouldFallback(filePath string) bool {
	return false
}

// GetFallbackPath returns an empty string (never called since ShouldFallback returns false).
func (f *NoFallback) GetFallbackPath(filePath string) string {
	return ""
}

// HTMLFallbackStrategy implements a fallback strategy for Single Page Applications (SPAs).
// It serves a specified HTML file (typically index.html) for non-file requests.
type HTMLFallbackStrategy struct {
	indexFile string
}

// NewHTMLFallbackStrategy creates a new HTMLFallbackStrategy.
// indexFile is the path to the HTML file to serve (e.g., "index.html", "/index.html").
func NewHTMLFallbackStrategy(indexFile string) *HTMLFallbackStrategy {
	return &HTMLFallbackStrategy{
		indexFile: indexFile,
	}
}

// ShouldFallback returns true for requests that don't look like static assets.
func (f *HTMLFallbackStrategy) ShouldFallback(filePath string) bool {
	// Always fall back unless it looks like a static asset
	return !f.isStaticAsset(filePath)
}

// GetFallbackPath returns the index file path.
func (f *HTMLFallbackStrategy) GetFallbackPath(filePath string) string {
	return f.indexFile
}

// isStaticAsset checks if the path looks like a static asset (has a file extension).
func (f *HTMLFallbackStrategy) isStaticAsset(filePath string) bool {
	return path.Ext(filePath) != ""
}

// ExtensionBasedFallback implements a fallback strategy that skips fallback for known static file extensions.
// This is the behavior from the original StaticHTMLFallbackHandler.
type ExtensionBasedFallback struct {
	staticExtensions map[string]bool
	fallbackPath     string
}

// NewExtensionBasedFallback creates a new ExtensionBasedFallback strategy.
// staticExtensions is a list of file extensions (with leading dot) that should NOT use fallback.
// fallbackPath is the file to serve when fallback is triggered.
func NewExtensionBasedFallback(staticExtensions []string, fallbackPath string) *ExtensionBasedFallback {
	extMap := make(map[string]bool)
	for _, ext := range staticExtensions {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extMap[strings.ToLower(ext)] = true
	}

	return &ExtensionBasedFallback{
		staticExtensions: extMap,
		fallbackPath:     fallbackPath,
	}
}

// NewDefaultExtensionBasedFallback creates an ExtensionBasedFallback with common web asset extensions.
// This matches the behavior of the original StaticHTMLFallbackHandler.
func NewDefaultExtensionBasedFallback(fallbackPath string) *ExtensionBasedFallback {
	return NewExtensionBasedFallback([]string{
		".js", ".css", ".png", ".svg", ".ico", ".json",
		".jpg", ".jpeg", ".gif", ".woff", ".woff2", ".ttf", ".eot",
	}, fallbackPath)
}

// ShouldFallback returns true if the file path doesn't have a static asset extension.
func (f *ExtensionBasedFallback) ShouldFallback(filePath string) bool {
	ext := strings.ToLower(path.Ext(filePath))

	// If it's a known static extension, don't fallback
	if f.staticExtensions[ext] {
		return false
	}

	// Otherwise, try fallback
	return true
}

// GetFallbackPath returns the configured fallback path.
func (f *ExtensionBasedFallback) GetFallbackPath(filePath string) string {
	return f.fallbackPath
}

// HTMLExtensionFallback implements a fallback strategy that appends .html to paths.
// This tries to serve {path}.html for missing files.
type HTMLExtensionFallback struct {
	staticExtensions map[string]bool
}

// NewHTMLExtensionFallback creates a new HTMLExtensionFallback strategy.
func NewHTMLExtensionFallback(staticExtensions []string) *HTMLExtensionFallback {
	extMap := make(map[string]bool)
	for _, ext := range staticExtensions {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extMap[strings.ToLower(ext)] = true
	}

	return &HTMLExtensionFallback{
		staticExtensions: extMap,
	}
}

// ShouldFallback returns true if the path doesn't have a static extension or .html.
func (f *HTMLExtensionFallback) ShouldFallback(filePath string) bool {
	ext := strings.ToLower(path.Ext(filePath))

	// If it's a known static extension, don't fallback
	if f.staticExtensions[ext] {
		return false
	}

	// If it already has .html, don't fallback
	if ext == ".html" || ext == ".htm" {
		return false
	}

	return true
}

// GetFallbackPath returns the path with .html appended.
func (f *HTMLExtensionFallback) GetFallbackPath(filePath string) string {
	cleanPath := path.Clean(filePath)
	if !strings.HasSuffix(filePath, "/") {
		cleanPath = strings.TrimRight(cleanPath, "/")
	}

	if !strings.HasSuffix(strings.ToLower(cleanPath), ".html") {
		return cleanPath + ".html"
	}

	return cleanPath
}
