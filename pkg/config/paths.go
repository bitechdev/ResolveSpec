package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Get retrieves a path by name
func (pc PathsConfig) Get(name string) (string, error) {
	if pc == nil {
		return "", fmt.Errorf("paths not initialized")
	}

	path, ok := pc[name]
	if !ok {
		return "", fmt.Errorf("path '%s' not found", name)
	}

	return path, nil
}

// GetOrDefault retrieves a path by name, returning defaultPath if not found
func (pc PathsConfig) GetOrDefault(name, defaultPath string) string {
	if pc == nil {
		return defaultPath
	}

	path, ok := pc[name]
	if !ok {
		return defaultPath
	}

	return path
}

// Set sets a path by name. It takes a pointer so a nil map can be allocated.
// PathsConfig is not safe for concurrent mutation; populate it before sharing.
func (pc *PathsConfig) Set(name, path string) {
	if *pc == nil {
		*pc = make(PathsConfig)
	}
	(*pc)[name] = path
}

// Has checks if a path exists by name
func (pc PathsConfig) Has(name string) bool {
	if pc == nil {
		return false
	}
	_, ok := pc[name]
	return ok
}

// EnsureDir ensures a directory exists at the specified path name
// Creates the directory if it doesn't exist with the given permissions
func (pc PathsConfig) EnsureDir(name string, perm os.FileMode) error {
	path, err := pc.Get(name)
	if err != nil {
		return err
	}

	// Check if directory exists
	info, err := os.Stat(path)
	if err == nil {
		// Path exists, check if it's a directory
		if !info.IsDir() {
			return fmt.Errorf("path '%s' exists but is not a directory: %s", name, path)
		}
		return nil
	}

	// Directory doesn't exist, create it
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, perm); err != nil {
			return fmt.Errorf("failed to create directory for '%s' at %s: %w", name, path, err)
		}
		return nil
	}

	return fmt.Errorf("failed to stat path '%s' at %s: %w", name, path, err)
}

// AbsPath returns the absolute path for a named path
func (pc PathsConfig) AbsPath(name string) (string, error) {
	path, err := pc.Get(name)
	if err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for '%s': %w", name, err)
	}

	return absPath, nil
}

// Join joins path segments with a named base path.
// It returns an error if the result would escape the base path.
func (pc PathsConfig) Join(name string, elem ...string) (string, error) {
	base, err := pc.Get(name)
	if err != nil {
		return "", err
	}

	parts := append([]string{base}, elem...)
	joined := filepath.Join(parts...)

	// filepath.Join resolves ".." rather than rejecting it; ensure the result stays under base
	cleanBase := filepath.Clean(base)
	if joined != cleanBase && !strings.HasPrefix(joined, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes base path '%s'", joined, name)
	}
	return joined, nil
}

// List returns all configured path names
func (pc PathsConfig) List() []string {
	if pc == nil {
		return []string{}
	}

	names := make([]string, 0, len(pc))
	for name := range pc {
		names = append(names, name)
	}
	return names
}
