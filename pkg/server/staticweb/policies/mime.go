package policies

import (
	"mime"
	"path"
	"strings"
	"sync"
)

// DefaultMIMEResolver implements a MIME type resolver using Go's standard mime package
// and a set of common web file type mappings.
type DefaultMIMEResolver struct {
	customTypes map[string]string
	mu          sync.RWMutex
}

// NewDefaultMIMEResolver creates a new DefaultMIMEResolver with common web MIME types.
func NewDefaultMIMEResolver() *DefaultMIMEResolver {
	resolver := &DefaultMIMEResolver{
		customTypes: make(map[string]string),
	}

	// JavaScript & TypeScript
	resolver.RegisterMIMEType(".js", "application/javascript")
	resolver.RegisterMIMEType(".mjs", "application/javascript")
	resolver.RegisterMIMEType(".cjs", "application/javascript")
	resolver.RegisterMIMEType(".ts", "text/typescript")
	resolver.RegisterMIMEType(".tsx", "text/tsx")
	resolver.RegisterMIMEType(".jsx", "text/jsx")

	// CSS & Styling
	resolver.RegisterMIMEType(".css", "text/css")
	resolver.RegisterMIMEType(".scss", "text/x-scss")
	resolver.RegisterMIMEType(".sass", "text/x-sass")
	resolver.RegisterMIMEType(".less", "text/x-less")

	// HTML & XML
	resolver.RegisterMIMEType(".html", "text/html")
	resolver.RegisterMIMEType(".htm", "text/html")
	resolver.RegisterMIMEType(".xml", "application/xml")
	resolver.RegisterMIMEType(".xhtml", "application/xhtml+xml")

	// Images - Raster
	resolver.RegisterMIMEType(".png", "image/png")
	resolver.RegisterMIMEType(".jpg", "image/jpeg")
	resolver.RegisterMIMEType(".jpeg", "image/jpeg")
	resolver.RegisterMIMEType(".gif", "image/gif")
	resolver.RegisterMIMEType(".webp", "image/webp")
	resolver.RegisterMIMEType(".avif", "image/avif")
	resolver.RegisterMIMEType(".bmp", "image/bmp")
	resolver.RegisterMIMEType(".tiff", "image/tiff")
	resolver.RegisterMIMEType(".tif", "image/tiff")
	resolver.RegisterMIMEType(".ico", "image/x-icon")
	resolver.RegisterMIMEType(".cur", "image/x-icon")

	// Images - Vector
	resolver.RegisterMIMEType(".svg", "image/svg+xml")
	resolver.RegisterMIMEType(".svgz", "image/svg+xml")

	// Fonts
	resolver.RegisterMIMEType(".woff", "font/woff")
	resolver.RegisterMIMEType(".woff2", "font/woff2")
	resolver.RegisterMIMEType(".ttf", "font/ttf")
	resolver.RegisterMIMEType(".otf", "font/otf")
	resolver.RegisterMIMEType(".eot", "application/vnd.ms-fontobject")

	// Audio
	resolver.RegisterMIMEType(".mp3", "audio/mpeg")
	resolver.RegisterMIMEType(".wav", "audio/wav")
	resolver.RegisterMIMEType(".ogg", "audio/ogg")
	resolver.RegisterMIMEType(".oga", "audio/ogg")
	resolver.RegisterMIMEType(".m4a", "audio/mp4")
	resolver.RegisterMIMEType(".aac", "audio/aac")
	resolver.RegisterMIMEType(".flac", "audio/flac")
	resolver.RegisterMIMEType(".opus", "audio/opus")
	resolver.RegisterMIMEType(".weba", "audio/webm")

	// Video
	resolver.RegisterMIMEType(".mp4", "video/mp4")
	resolver.RegisterMIMEType(".webm", "video/webm")
	resolver.RegisterMIMEType(".ogv", "video/ogg")
	resolver.RegisterMIMEType(".avi", "video/x-msvideo")
	resolver.RegisterMIMEType(".mpeg", "video/mpeg")
	resolver.RegisterMIMEType(".mpg", "video/mpeg")
	resolver.RegisterMIMEType(".mov", "video/quicktime")
	resolver.RegisterMIMEType(".wmv", "video/x-ms-wmv")
	resolver.RegisterMIMEType(".flv", "video/x-flv")
	resolver.RegisterMIMEType(".mkv", "video/x-matroska")
	resolver.RegisterMIMEType(".m4v", "video/mp4")

	// Data & Configuration
	resolver.RegisterMIMEType(".json", "application/json")
	resolver.RegisterMIMEType(".xml", "application/xml")
	resolver.RegisterMIMEType(".yml", "application/yaml")
	resolver.RegisterMIMEType(".yaml", "application/yaml")
	resolver.RegisterMIMEType(".toml", "application/toml")
	resolver.RegisterMIMEType(".ini", "text/plain")
	resolver.RegisterMIMEType(".conf", "text/plain")
	resolver.RegisterMIMEType(".config", "text/plain")

	// Documents
	resolver.RegisterMIMEType(".pdf", "application/pdf")
	resolver.RegisterMIMEType(".doc", "application/msword")
	resolver.RegisterMIMEType(".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	resolver.RegisterMIMEType(".xls", "application/vnd.ms-excel")
	resolver.RegisterMIMEType(".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	resolver.RegisterMIMEType(".ppt", "application/vnd.ms-powerpoint")
	resolver.RegisterMIMEType(".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	resolver.RegisterMIMEType(".odt", "application/vnd.oasis.opendocument.text")
	resolver.RegisterMIMEType(".ods", "application/vnd.oasis.opendocument.spreadsheet")
	resolver.RegisterMIMEType(".odp", "application/vnd.oasis.opendocument.presentation")

	// Archives
	resolver.RegisterMIMEType(".zip", "application/zip")
	resolver.RegisterMIMEType(".tar", "application/x-tar")
	resolver.RegisterMIMEType(".gz", "application/gzip")
	resolver.RegisterMIMEType(".bz2", "application/x-bzip2")
	resolver.RegisterMIMEType(".7z", "application/x-7z-compressed")
	resolver.RegisterMIMEType(".rar", "application/vnd.rar")

	// Text files
	resolver.RegisterMIMEType(".txt", "text/plain")
	resolver.RegisterMIMEType(".md", "text/markdown")
	resolver.RegisterMIMEType(".markdown", "text/markdown")
	resolver.RegisterMIMEType(".csv", "text/csv")
	resolver.RegisterMIMEType(".log", "text/plain")

	// Source code (for syntax highlighting in browsers)
	resolver.RegisterMIMEType(".c", "text/x-c")
	resolver.RegisterMIMEType(".cpp", "text/x-c++")
	resolver.RegisterMIMEType(".h", "text/x-c")
	resolver.RegisterMIMEType(".hpp", "text/x-c++")
	resolver.RegisterMIMEType(".go", "text/x-go")
	resolver.RegisterMIMEType(".py", "text/x-python")
	resolver.RegisterMIMEType(".java", "text/x-java")
	resolver.RegisterMIMEType(".rs", "text/x-rust")
	resolver.RegisterMIMEType(".rb", "text/x-ruby")
	resolver.RegisterMIMEType(".php", "text/x-php")
	resolver.RegisterMIMEType(".sh", "text/x-shellscript")
	resolver.RegisterMIMEType(".bash", "text/x-shellscript")
	resolver.RegisterMIMEType(".sql", "text/x-sql")
	resolver.RegisterMIMEType(".template.sql", "text/plain")
	resolver.RegisterMIMEType(".upg", "text/plain")

	// Web Assembly
	resolver.RegisterMIMEType(".wasm", "application/wasm")

	// Manifest & Service Worker
	resolver.RegisterMIMEType(".webmanifest", "application/manifest+json")
	resolver.RegisterMIMEType(".manifest", "text/cache-manifest")

	// 3D Models
	resolver.RegisterMIMEType(".gltf", "model/gltf+json")
	resolver.RegisterMIMEType(".glb", "model/gltf-binary")
	resolver.RegisterMIMEType(".obj", "model/obj")
	resolver.RegisterMIMEType(".stl", "model/stl")

	// Other common web assets
	resolver.RegisterMIMEType(".map", "application/json")        // Source maps
	resolver.RegisterMIMEType(".swf", "application/x-shockwave-flash")
	resolver.RegisterMIMEType(".apk", "application/vnd.android.package-archive")
	resolver.RegisterMIMEType(".dmg", "application/x-apple-diskimage")
	resolver.RegisterMIMEType(".exe", "application/x-msdownload")
	resolver.RegisterMIMEType(".iso", "application/x-iso9660-image")

	return resolver
}

// GetMIMEType returns the MIME type for the given file path.
// It first checks custom registered types, then falls back to Go's mime.TypeByExtension.
func (r *DefaultMIMEResolver) GetMIMEType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))

	// Check custom types first
	r.mu.RLock()
	if mimeType, ok := r.customTypes[ext]; ok {
		r.mu.RUnlock()
		return mimeType
	}
	r.mu.RUnlock()

	// Fall back to standard library
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}

	// Return empty string if unknown
	return ""
}

// RegisterMIMEType registers a custom MIME type for the given file extension.
func (r *DefaultMIMEResolver) RegisterMIMEType(extension, mimeType string) {
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	r.mu.Lock()
	r.customTypes[strings.ToLower(extension)] = mimeType
	r.mu.Unlock()
}

// ConfigurableMIMEResolver implements a MIME type resolver with user-defined mappings only.
// It does not use any default mappings.
type ConfigurableMIMEResolver struct {
	types map[string]string
	mu    sync.RWMutex
}

// NewConfigurableMIMEResolver creates a new ConfigurableMIMEResolver with the given mappings.
func NewConfigurableMIMEResolver(types map[string]string) *ConfigurableMIMEResolver {
	resolver := &ConfigurableMIMEResolver{
		types: make(map[string]string),
	}

	for ext, mimeType := range types {
		resolver.RegisterMIMEType(ext, mimeType)
	}

	return resolver
}

// GetMIMEType returns the MIME type for the given file path.
func (r *ConfigurableMIMEResolver) GetMIMEType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))

	r.mu.RLock()
	defer r.mu.RUnlock()

	if mimeType, ok := r.types[ext]; ok {
		return mimeType
	}

	return ""
}

// RegisterMIMEType registers a MIME type for the given file extension.
func (r *ConfigurableMIMEResolver) RegisterMIMEType(extension, mimeType string) {
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}

	r.mu.Lock()
	r.types[strings.ToLower(extension)] = mimeType
	r.mu.Unlock()
}
