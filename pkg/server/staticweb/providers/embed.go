package providers

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/server/zipfs"
)

// EmbedFSProvider serves files from an embedded filesystem.
// It supports both direct embedded directories and embedded zip files.
type EmbedFSProvider struct {
	embedFS   *embed.FS
	zipFile   string // Optional: path within embedded FS to zip file
	zipReader *zip.Reader
	fs        fs.FS
	mu        sync.RWMutex
}

// NewEmbedFSProvider creates a new EmbedFSProvider.
// If zipFile is empty, the embedded FS is used directly.
// If zipFile is specified, it's treated as a path to a zip file within the embedded FS.
func NewEmbedFSProvider(embedFS fs.FS, zipFile string) (*EmbedFSProvider, error) {
	if embedFS == nil {
		return nil, fmt.Errorf("embedded filesystem cannot be nil")
	}

	// Try to cast to *embed.FS for tracking purposes
	var embedFSPtr *embed.FS
	if efs, ok := embedFS.(*embed.FS); ok {
		embedFSPtr = efs
	}

	provider := &EmbedFSProvider{
		embedFS: embedFSPtr,
		zipFile: zipFile,
	}

	// If zipFile is specified, open it as a zip archive
	if zipFile != "" {
		// Read the zip file from the embedded FS
		// We need to check if the FS supports ReadFile
		var data []byte
		var err error

		if readFileFS, ok := embedFS.(interface{ ReadFile(string) ([]byte, error) }); ok {
			data, err = readFileFS.ReadFile(zipFile)
		} else {
			// Fall back to Open and reading
			file, openErr := embedFS.Open(zipFile)
			if openErr != nil {
				return nil, fmt.Errorf("failed to open embedded zip file: %w", openErr)
			}
			defer file.Close()
			data, err = io.ReadAll(file)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded zip file: %w", err)
		}

		// Create a zip reader from the data
		reader := bytes.NewReader(data)
		zipReader, err := zip.NewReader(reader, int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("failed to create zip reader: %w", err)
		}

		provider.zipReader = zipReader
		provider.fs = zipfs.NewZipFS(zipReader)
	} else {
		// Use the embedded FS directly
		provider.fs = embedFS
	}

	return provider, nil
}

// Open opens the named file from the embedded filesystem.
func (p *EmbedFSProvider) Open(name string) (fs.File, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.fs == nil {
		return nil, fmt.Errorf("embedded filesystem is closed")
	}

	return p.fs.Open(name)
}

// Close releases any resources held by the provider.
// For embedded filesystems, this is mostly a no-op since Go manages the lifecycle.
func (p *EmbedFSProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear references to allow garbage collection
	p.fs = nil
	p.zipReader = nil

	return nil
}

// Type returns "embed" or "embed-zip" depending on the configuration.
func (p *EmbedFSProvider) Type() string {
	if p.zipFile != "" {
		return "embed-zip"
	}
	return "embed"
}

// ZipFile returns the path to the zip file within the embedded FS, if any.
func (p *EmbedFSProvider) ZipFile() string {
	return p.zipFile
}
