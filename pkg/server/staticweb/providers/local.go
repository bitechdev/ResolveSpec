package providers

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// LocalFSProvider serves files from a local directory.
type LocalFSProvider struct {
	path        string
	stripPrefix string
	fs          fs.FS
	mu          sync.RWMutex
}

// NewLocalFSProvider creates a new LocalFSProvider for the given directory path.
// The path must be an absolute path to an existing directory.
func NewLocalFSProvider(path string) (*LocalFSProvider, error) {
	// Validate that the path exists and is a directory
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &LocalFSProvider{
		path: absPath,
		fs:   os.DirFS(absPath),
	}, nil
}

// Open opens the named file from the local directory.
// If a strip prefix is configured, it prepends the prefix to the requested path.
// For example, with stripPrefix="/dist", requesting "/assets/style.css" will
// open "/dist/assets/style.css" from the local filesystem.
func (p *LocalFSProvider) Open(name string) (fs.File, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Apply prefix stripping by prepending the prefix to the requested path
	actualPath := name
	if p.stripPrefix != "" {
		// Clean the paths to handle leading/trailing slashes
		prefix := strings.Trim(p.stripPrefix, "/")
		cleanName := strings.TrimPrefix(name, "/")

		if prefix != "" {
			actualPath = path.Join(prefix, cleanName)
		} else {
			actualPath = cleanName
		}
	}

	return p.fs.Open(actualPath)
}

// Close releases any resources held by the provider.
// For local filesystem, this is a no-op since os.DirFS doesn't hold resources.
func (p *LocalFSProvider) Close() error {
	return nil
}

// Type returns "local".
func (p *LocalFSProvider) Type() string {
	return "local"
}

// Path returns the absolute path to the directory being served.
func (p *LocalFSProvider) Path() string {
	return p.path
}

// Reload refreshes the filesystem view.
// For local directories, os.DirFS automatically picks up changes,
// so this recreates the DirFS to ensure a fresh view.
func (p *LocalFSProvider) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Verify the directory still exists
	info, err := os.Stat(p.path)
	if err != nil {
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is no longer a directory: %s", p.path)
	}

	// Recreate the DirFS
	p.fs = os.DirFS(p.path)

	return nil
}

// WithStripPrefix sets the prefix to strip from requested paths.
// For example, WithStripPrefix("/dist") will make files at "/dist/assets"
// accessible via "/assets".
func (p *LocalFSProvider) WithStripPrefix(prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stripPrefix = prefix
}

// StripPrefix returns the configured strip prefix.
func (p *LocalFSProvider) StripPrefix() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stripPrefix
}
