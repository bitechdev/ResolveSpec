package providers

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/server/zipfs"
)

// ZipFSProvider serves files from a zip file.
type ZipFSProvider struct {
	zipPath     string
	stripPrefix string
	zipReader   *zip.ReadCloser
	zipFS       *zipfs.ZipFS
	mu          sync.RWMutex
}

// NewZipFSProvider creates a new ZipFSProvider for the given zip file path.
func NewZipFSProvider(zipPath string) (*ZipFSProvider, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Open the zip file
	zipReader, err := zip.OpenReader(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file: %w", err)
	}

	return &ZipFSProvider{
		zipPath:   absPath,
		zipReader: zipReader,
		zipFS:     zipfs.NewZipFS(&zipReader.Reader),
	}, nil
}

// Open opens the named file from the zip archive.
// If a strip prefix is configured, it prepends the prefix to the requested path.
// For example, with stripPrefix="/dist", requesting "/assets/style.css" will
// open "/dist/assets/style.css" from the zip filesystem.
func (p *ZipFSProvider) Open(name string) (fs.File, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.zipFS == nil {
		return nil, fmt.Errorf("zip filesystem is closed")
	}

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

	return p.zipFS.Open(actualPath)
}

// Close releases resources held by the zip reader.
func (p *ZipFSProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.zipReader != nil {
		err := p.zipReader.Close()
		p.zipReader = nil
		p.zipFS = nil
		return err
	}

	return nil
}

// Type returns "zip".
func (p *ZipFSProvider) Type() string {
	return "zip"
}

// Path returns the absolute path to the zip file being served.
func (p *ZipFSProvider) Path() string {
	return p.zipPath
}

// Reload reopens the zip file to pick up any changes.
// This is useful in development when the zip file is updated.
func (p *ZipFSProvider) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close the existing zip reader if open
	if p.zipReader != nil {
		if err := p.zipReader.Close(); err != nil {
			return fmt.Errorf("failed to close old zip reader: %w", err)
		}
	}

	// Reopen the zip file
	zipReader, err := zip.OpenReader(p.zipPath)
	if err != nil {
		return fmt.Errorf("failed to reopen zip file: %w", err)
	}

	p.zipReader = zipReader
	p.zipFS = zipfs.NewZipFS(&zipReader.Reader)

	return nil
}

// WithStripPrefix sets the prefix to strip from requested paths.
// For example, WithStripPrefix("/dist") will make files at "/dist/assets"
// accessible via "/assets".
func (p *ZipFSProvider) WithStripPrefix(prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stripPrefix = prefix
}

// StripPrefix returns the configured strip prefix.
func (p *ZipFSProvider) StripPrefix() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stripPrefix
}
