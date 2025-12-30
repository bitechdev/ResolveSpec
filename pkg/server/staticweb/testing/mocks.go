package testing

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// MockFileSystemProvider is an in-memory filesystem provider for testing.
type MockFileSystemProvider struct {
	files  map[string][]byte
	closed bool
	mu     sync.RWMutex
}

// NewMockProvider creates a new in-memory provider with the given files.
// Keys should be slash-separated paths (e.g., "index.html", "assets/app.js").
func NewMockProvider(files map[string][]byte) *MockFileSystemProvider {
	return &MockFileSystemProvider{
		files: files,
	}
}

// Open opens a file from the in-memory filesystem.
func (m *MockFileSystemProvider) Open(name string) (fs.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	// Remove leading slash if present
	name = strings.TrimPrefix(name, "/")

	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	return &mockFile{
		name: path.Base(name),
		data: data,
	}, nil
}

// Close marks the provider as closed.
func (m *MockFileSystemProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

// Type returns "mock".
func (m *MockFileSystemProvider) Type() string {
	return "mock"
}

// AddFile adds a file to the in-memory filesystem.
func (m *MockFileSystemProvider) AddFile(name string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimPrefix(name, "/")
	m.files[name] = data
}

// RemoveFile removes a file from the in-memory filesystem.
func (m *MockFileSystemProvider) RemoveFile(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimPrefix(name, "/")
	delete(m.files, name)
}

// mockFile implements fs.File for in-memory files.
type mockFile struct {
	name   string
	data   []byte
	reader *bytes.Reader
	offset int64
}

func (f *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{
		name: f.name,
		size: int64(len(f.data)),
	}, nil
}

func (f *mockFile) Read(p []byte) (int, error) {
	if f.reader == nil {
		f.reader = bytes.NewReader(f.data)
		if f.offset > 0 {
			f.reader.Seek(f.offset, io.SeekStart)
		}
	}
	n, err := f.reader.Read(p)
	f.offset += int64(n)
	return n, err
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	if f.reader == nil {
		f.reader = bytes.NewReader(f.data)
	}
	pos, err := f.reader.Seek(offset, whence)
	f.offset = pos
	return pos, err
}

func (f *mockFile) Close() error {
	return nil
}

// mockFileInfo implements fs.FileInfo.
type mockFileInfo struct {
	name string
	size int64
}

func (fi *mockFileInfo) Name() string       { return fi.name }
func (fi *mockFileInfo) Size() int64        { return fi.size }
func (fi *mockFileInfo) Mode() fs.FileMode  { return 0644 }
func (fi *mockFileInfo) ModTime() time.Time { return time.Now() }
func (fi *mockFileInfo) IsDir() bool        { return false }
func (fi *mockFileInfo) Sys() interface{}   { return nil }

// MockCachePolicy is a configurable cache policy for testing.
type MockCachePolicy struct {
	CacheTime int
	Headers   map[string]string
}

// NewMockCachePolicy creates a new mock cache policy.
func NewMockCachePolicy(cacheTime int) *MockCachePolicy {
	return &MockCachePolicy{
		CacheTime: cacheTime,
		Headers:   make(map[string]string),
	}
}

// GetCacheTime returns the configured cache time.
func (p *MockCachePolicy) GetCacheTime(path string) int {
	return p.CacheTime
}

// GetCacheHeaders returns the configured headers.
func (p *MockCachePolicy) GetCacheHeaders(path string) map[string]string {
	if p.Headers != nil {
		return p.Headers
	}
	return map[string]string{
		"Cache-Control": fmt.Sprintf("public, max-age=%d", p.CacheTime),
	}
}

// MockMIMEResolver is a configurable MIME resolver for testing.
type MockMIMEResolver struct {
	types map[string]string
	mu    sync.RWMutex
}

// NewMockMIMEResolver creates a new mock MIME resolver.
func NewMockMIMEResolver() *MockMIMEResolver {
	return &MockMIMEResolver{
		types: make(map[string]string),
	}
}

// GetMIMEType returns the MIME type for the given path.
func (r *MockMIMEResolver) GetMIMEType(path string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ext := strings.ToLower(path[strings.LastIndex(path, "."):])
	if mimeType, ok := r.types[ext]; ok {
		return mimeType
	}
	return "application/octet-stream"
}

// RegisterMIMEType registers a MIME type.
func (r *MockMIMEResolver) RegisterMIMEType(extension, mimeType string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	r.types[strings.ToLower(extension)] = mimeType
}

// MockFallbackStrategy is a configurable fallback strategy for testing.
type MockFallbackStrategy struct {
	ShouldFallbackFunc func(path string) bool
	FallbackPathFunc   func(path string) string
}

// NewMockFallbackStrategy creates a new mock fallback strategy.
func NewMockFallbackStrategy(shouldFallback func(string) bool, fallbackPath func(string) string) *MockFallbackStrategy {
	return &MockFallbackStrategy{
		ShouldFallbackFunc: shouldFallback,
		FallbackPathFunc:   fallbackPath,
	}
}

// ShouldFallback returns whether fallback should be used.
func (s *MockFallbackStrategy) ShouldFallback(path string) bool {
	if s.ShouldFallbackFunc != nil {
		return s.ShouldFallbackFunc(path)
	}
	return false
}

// GetFallbackPath returns the fallback path.
func (s *MockFallbackStrategy) GetFallbackPath(path string) string {
	if s.FallbackPathFunc != nil {
		return s.FallbackPathFunc(path)
	}
	return "index.html"
}
