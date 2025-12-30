package staticweb

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// service implements the StaticFileService interface.
type service struct {
	mounts map[string]*mountPoint // urlPrefix -> mount point
	config *ServiceConfig
	mu     sync.RWMutex
}

// NewService creates a new static file service with the given configuration.
// If config is nil, default configuration is used.
func NewService(config *ServiceConfig) StaticFileService {
	if config == nil {
		config = DefaultServiceConfig()
	}

	return &service{
		mounts: make(map[string]*mountPoint),
		config: config,
	}
}

// Mount adds a new mount point with the given configuration.
func (s *service) Mount(config MountConfig) error {
	// Validate the config
	if err := s.validateMountConfig(config); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if the prefix is already mounted
	if _, exists := s.mounts[config.URLPrefix]; exists {
		return fmt.Errorf("mount point already exists at %s", config.URLPrefix)
	}

	// Create the mount point
	mp, err := newMountPoint(config, s.config)
	if err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	// Add to the map
	s.mounts[config.URLPrefix] = mp

	return nil
}

// Unmount removes the mount point at the given URL prefix.
func (s *service) Unmount(urlPrefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mp, exists := s.mounts[urlPrefix]
	if !exists {
		return fmt.Errorf("no mount point exists at %s", urlPrefix)
	}

	// Close the mount point to release resources
	if err := mp.Close(); err != nil {
		return fmt.Errorf("failed to close mount point: %w", err)
	}

	// Remove from the map
	delete(s.mounts, urlPrefix)

	return nil
}

// ListMounts returns a sorted list of all mounted URL prefixes.
func (s *service) ListMounts() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefixes := make([]string, 0, len(s.mounts))
	for prefix := range s.mounts {
		prefixes = append(prefixes, prefix)
	}

	sort.Strings(prefixes)
	return prefixes
}

// Reload reinitializes all filesystem providers that support reloading.
// This is useful when the underlying files have changed (e.g., zip file updated).
// Providers that implement ReloadableProvider will be reloaded.
func (s *service) Reload() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var errors []error

	// Reload all mount points that support it
	for prefix, mp := range s.mounts {
		if reloadable, ok := mp.provider.(ReloadableProvider); ok {
			if err := reloadable.Reload(); err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", prefix, err))
			}
		}
	}

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("errors while reloading providers: %v", errors)
	}

	return nil
}

// Close releases all resources held by the service.
func (s *service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errors []error

	// Close all mount points
	for prefix, mp := range s.mounts {
		if err := mp.Close(); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", prefix, err))
		}
	}

	// Clear the map
	s.mounts = make(map[string]*mountPoint)

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("errors while closing mount points: %v", errors)
	}

	return nil
}

// Handler returns an http.Handler that serves static files from all mount points.
func (s *service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		// Find the best matching mount point using longest-prefix matching
		var bestMatch *mountPoint
		var bestPrefix string

		for prefix, mp := range s.mounts {
			if strings.HasPrefix(r.URL.Path, prefix) {
				if len(prefix) > len(bestPrefix) {
					bestMatch = mp
					bestPrefix = prefix
				}
			}
		}

		// If no mount point matches, return without writing a response
		// This allows other handlers (like API routes) to process the request
		if bestMatch == nil {
			return
		}

		// Serve the file from the matched mount point
		bestMatch.ServeHTTP(w, r)
	})
}

// validateMountConfig validates the mount configuration.
func (s *service) validateMountConfig(config MountConfig) error {
	if config.URLPrefix == "" {
		return fmt.Errorf("URLPrefix cannot be empty")
	}

	if !strings.HasPrefix(config.URLPrefix, "/") {
		return fmt.Errorf("URLPrefix must start with /")
	}

	if config.Provider == nil {
		return fmt.Errorf("Provider cannot be nil")
	}

	return nil
}
