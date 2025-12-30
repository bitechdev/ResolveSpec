package providers

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalFSProvider(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewLocalFSProvider(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test opening a file
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Read the file
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != "test content" {
		t.Errorf("Expected 'test content', got %q", string(data))
	}

	// Test type
	if provider.Type() != "local" {
		t.Errorf("Expected type 'local', got %q", provider.Type())
	}
}

func TestZipFSProvider(t *testing.T) {
	// Create a temporary zip file
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	// Create zip file with test content
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	zipWriter := zip.NewWriter(zipFile)
	fileWriter, err := zipWriter.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	_, err = fileWriter.Write([]byte("zip content"))
	if err != nil {
		t.Fatal(err)
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	if err := zipFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Test the provider
	provider, err := NewZipFSProvider(zipPath)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test opening a file
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Read the file
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != "zip content" {
		t.Errorf("Expected 'zip content', got %q", string(data))
	}

	// Test type
	if provider.Type() != "zip" {
		t.Errorf("Expected type 'zip', got %q", provider.Type())
	}
}

func TestZipFSProviderReload(t *testing.T) {
	// Create a temporary zip file
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	// Helper to create zip with content
	createZip := func(content string) {
		zipFile, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		defer zipFile.Close()

		zipWriter := zip.NewWriter(zipFile)
		fileWriter, err := zipWriter.Create("test.txt")
		if err != nil {
			t.Fatal(err)
		}

		_, err = fileWriter.Write([]byte(content))
		if err != nil {
			t.Fatal(err)
		}

		if err := zipWriter.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// Create initial zip
	createZip("original content")

	// Test the provider
	provider, err := NewZipFSProvider(zipPath)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Read initial content
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	data, _ := io.ReadAll(file)
	file.Close()

	if string(data) != "original content" {
		t.Errorf("Expected 'original content', got %q", string(data))
	}

	// Update the zip file
	createZip("updated content")

	// Reload the provider
	if err := provider.Reload(); err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	// Read updated content
	file, err = provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file after reload: %v", err)
	}
	data, _ = io.ReadAll(file)
	file.Close()

	if string(data) != "updated content" {
		t.Errorf("Expected 'updated content', got %q", string(data))
	}
}

func TestLocalFSProviderReload(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	provider, err := NewLocalFSProvider(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Read initial content
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	data, _ := io.ReadAll(file)
	file.Close()

	if string(data) != "original" {
		t.Errorf("Expected 'original', got %q", string(data))
	}

	// Update the file
	if err := os.WriteFile(testFile, []byte("updated"), 0644); err != nil {
		t.Fatal(err)
	}

	// Reload the provider
	if err := provider.Reload(); err != nil {
		t.Fatalf("Failed to reload: %v", err)
	}

	// Read updated content
	file, err = provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file after reload: %v", err)
	}
	data, _ = io.ReadAll(file)
	file.Close()

	if string(data) != "updated" {
		t.Errorf("Expected 'updated', got %q", string(data))
	}
}

func TestEmbedFSProvider(t *testing.T) {
	// Test with a mock embed.FS
	mockFS := &mockEmbedFS{
		files: map[string][]byte{
			"test.txt": []byte("test content"),
		},
	}

	provider, err := NewEmbedFSProvider(mockFS, "")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test type
	if provider.Type() != "embed" {
		t.Errorf("Expected type 'embed', got %q", provider.Type())
	}

	// Test opening a file
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Read the file
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != "test content" {
		t.Errorf("Expected 'test content', got %q", string(data))
	}
}

func TestEmbedFSProviderWithZip(t *testing.T) {
	// Create an embedded-like FS with a zip file
	// For simplicity, we'll use a mock embed.FS
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	// Create zip file
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	zipWriter := zip.NewWriter(zipFile)
	fileWriter, err := zipWriter.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	_, err = fileWriter.Write([]byte("embedded zip content"))
	if err != nil {
		t.Fatal(err)
	}

	zipWriter.Close()
	zipFile.Close()

	// Read the zip file
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create a mock embed.FS
	mockFS := &mockEmbedFS{
		files: map[string][]byte{
			"test.zip": zipData,
		},
	}

	provider, err := NewEmbedFSProvider(mockFS, "test.zip")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Test opening a file
	file, err := provider.Open("test.txt")
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Read the file
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(data) != "embedded zip content" {
		t.Errorf("Expected 'embedded zip content', got %q", string(data))
	}

	// Test type
	if provider.Type() != "embed-zip" {
		t.Errorf("Expected type 'embed-zip', got %q", provider.Type())
	}
}

// mockEmbedFS is a mock embed.FS for testing
type mockEmbedFS struct {
	files map[string][]byte
}

func (m *mockEmbedFS) Open(name string) (fs.File, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}

	return &mockFile{
		name:   name,
		reader: bytes.NewReader(data),
		size:   int64(len(data)),
	}, nil
}

func (m *mockEmbedFS) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

type mockFile struct {
	name   string
	reader *bytes.Reader
	size   int64
}

func (f *mockFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{name: f.name, size: f.size}, nil
}

func (f *mockFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *mockFile) Close() error {
	return nil
}

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
