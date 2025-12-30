package providers

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LocalFSProvider serves files from a local directory.
type LocalFSProvider struct {
	path string
	fs   fs.FS
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
func (p *LocalFSProvider) Open(name string) (fs.File, error) {
	return p.fs.Open(name)
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
