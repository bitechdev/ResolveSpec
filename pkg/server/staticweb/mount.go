package staticweb

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// mountPoint represents a mounted filesystem at a specific URL prefix.
type mountPoint struct {
	urlPrefix        string
	provider         FileSystemProvider
	cachePolicy      CachePolicy
	mimeResolver     MIMETypeResolver
	fallbackStrategy FallbackStrategy
	fileServer       http.Handler
}

// newMountPoint creates a new mount point with the given configuration.
func newMountPoint(config MountConfig, defaults *ServiceConfig) (*mountPoint, error) {
	if config.URLPrefix == "" {
		return nil, fmt.Errorf("URLPrefix cannot be empty")
	}

	if !strings.HasPrefix(config.URLPrefix, "/") {
		return nil, fmt.Errorf("URLPrefix must start with /")
	}

	if config.Provider == nil {
		return nil, fmt.Errorf("Provider cannot be nil")
	}

	mp := &mountPoint{
		urlPrefix:        config.URLPrefix,
		provider:         config.Provider,
		cachePolicy:      config.CachePolicy,
		mimeResolver:     config.MIMEResolver,
		fallbackStrategy: config.FallbackStrategy,
	}

	// Apply defaults if policies are not specified
	if mp.cachePolicy == nil && defaults != nil {
		mp.cachePolicy = defaultCachePolicy(defaults.DefaultCacheTime)
	}

	if mp.mimeResolver == nil {
		mp.mimeResolver = defaultMIMEResolver()
	}

	// Create an http.FileServer for serving files
	mp.fileServer = http.FileServer(http.FS(config.Provider))

	return mp, nil
}

// ServeHTTP handles HTTP requests for files in this mount point.
func (m *mountPoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the URL prefix to get the file path
	filePath := strings.TrimPrefix(r.URL.Path, m.urlPrefix)
	if filePath == "" {
		filePath = "/"
	}

	// Clean the path
	filePath = path.Clean(filePath)

	// Try to open the file
	file, err := m.provider.Open(strings.TrimPrefix(filePath, "/"))
	if err != nil {
		// File doesn't exist - check if we should use fallback
		if m.fallbackStrategy != nil && m.fallbackStrategy.ShouldFallback(filePath) {
			fallbackPath := m.fallbackStrategy.GetFallbackPath(filePath)
			file, err = m.provider.Open(strings.TrimPrefix(fallbackPath, "/"))
			if err == nil {
				// Successfully opened fallback file
				defer file.Close()
				m.serveFile(w, r, fallbackPath, file)
				return
			}
		}

		// No fallback or fallback failed - return 404
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	// Serve the file
	m.serveFile(w, r, filePath, file)
}

// serveFile serves a single file with appropriate headers.
func (m *mountPoint) serveFile(w http.ResponseWriter, r *http.Request, filePath string, file fs.File) {
	// Get file info
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If it's a directory, try to serve index.html
	if stat.IsDir() {
		indexPath := path.Join(filePath, "index.html")
		indexFile, err := m.provider.Open(strings.TrimPrefix(indexPath, "/"))
		if err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		defer indexFile.Close()

		indexStat, err := indexFile.Stat()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		filePath = indexPath
		stat = indexStat
		file = indexFile
	}

	// Set Content-Type header using MIME resolver
	if m.mimeResolver != nil {
		if mimeType := m.mimeResolver.GetMIMEType(filePath); mimeType != "" {
			w.Header().Set("Content-Type", mimeType)
		}
	}

	// Apply cache policy
	if m.cachePolicy != nil {
		headers := m.cachePolicy.GetCacheHeaders(filePath)
		for key, value := range headers {
			w.Header().Set(key, value)
		}
	}

	// Serve the content
	if seeker, ok := file.(interface {
		io.ReadSeeker
	}); ok {
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), seeker)
	} else {
		// If the file doesn't support seeking, we need to read it all into memory
		data, err := fs.ReadFile(m.provider, strings.TrimPrefix(filePath, "/"))
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), strings.NewReader(string(data)))
	}
}

// Close releases resources held by the mount point.
func (m *mountPoint) Close() error {
	if m.provider != nil {
		return m.provider.Close()
	}
	return nil
}

// defaultCachePolicy creates a default simple cache policy.
func defaultCachePolicy(cacheTime int) CachePolicy {
	// Import the policies package type - we'll need to use the concrete type
	// For now, create a simple inline implementation
	return &simpleCachePolicy{cacheTime: cacheTime}
}

// simpleCachePolicy is a simple inline implementation of CachePolicy
type simpleCachePolicy struct {
	cacheTime int
}

func (p *simpleCachePolicy) GetCacheTime(path string) int {
	return p.cacheTime
}

func (p *simpleCachePolicy) GetCacheHeaders(path string) map[string]string {
	if p.cacheTime <= 0 {
		return map[string]string{
			"Cache-Control": "no-cache, no-store, must-revalidate",
			"Pragma":        "no-cache",
			"Expires":       "0",
		}
	}
	return map[string]string{
		"Cache-Control": fmt.Sprintf("public, max-age=%d", p.cacheTime),
	}
}

// defaultMIMEResolver creates a default MIME resolver.
func defaultMIMEResolver() MIMETypeResolver {
	// Import the policies package type - we'll need to use the concrete type
	// For now, create a simple inline implementation
	return &simpleMIMEResolver{
		types: map[string]string{
			".js":   "application/javascript",
			".mjs":  "application/javascript",
			".cjs":  "application/javascript",
			".css":  "text/css",
			".html": "text/html",
			".htm":  "text/html",
			".json": "application/json",
			".png":  "image/png",
			".jpg":  "image/jpeg",
			".jpeg": "image/jpeg",
			".gif":  "image/gif",
			".svg":  "image/svg+xml",
			".ico":  "image/x-icon",
			".txt":  "text/plain",
		},
	}
}

// simpleMIMEResolver is a simple inline implementation of MIMETypeResolver
type simpleMIMEResolver struct {
	types map[string]string
}

func (r *simpleMIMEResolver) GetMIMEType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	if mimeType, ok := r.types[ext]; ok {
		return mimeType
	}
	return ""
}

func (r *simpleMIMEResolver) RegisterMIMEType(extension, mimeType string) {
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	r.types[strings.ToLower(extension)] = mimeType
}
