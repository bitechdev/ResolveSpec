package cache

import (
	"context"
	"errors"
	"time"
)

// ErrFlushNotAllowed is returned by Clear on shared-server providers (Redis, Memcache)
// unless AllowFlush is set in their config, because Clear flushes the whole server/DB.
var ErrFlushNotAllowed = errors.New("cache: Clear flushes the entire server; set AllowFlush in the provider config to permit it")

// Provider defines the interface that all cache providers must implement.
type Provider interface {
	// Get retrieves a value from the cache by key.
	// Returns nil, false if key doesn't exist or is expired.
	Get(ctx context.Context, key string) ([]byte, bool)

	// Set stores a value in the cache with the specified TTL.
	// If ttl is 0, the item never expires.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// SetWithTags stores a value in the cache with the specified TTL and tags.
	// Tags can be used to invalidate groups of related keys.
	// If ttl is 0, the item never expires.
	SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// DeleteByTag removes all keys associated with the given tag.
	DeleteByTag(ctx context.Context, tag string) error

	// DeleteByPattern removes all keys matching the pattern.
	// Pattern syntax depends on the provider implementation.
	DeleteByPattern(ctx context.Context, pattern string) error

	// Clear removes all items from the cache.
	Clear(ctx context.Context) error

	// Exists checks if a key exists in the cache.
	Exists(ctx context.Context, key string) bool

	// Close closes the provider and releases any resources.
	Close() error

	// Stats returns statistics about the cache provider.
	Stats(ctx context.Context) (*CacheStats, error)
}

// CacheStats contains cache statistics.
type CacheStats struct {
	Hits          int64          `json:"hits"`
	Misses        int64          `json:"misses"`
	Keys          int64          `json:"keys"`
	ProviderType  string         `json:"provider_type"`
	ProviderStats map[string]any `json:"provider_stats,omitempty"`
}

// Options contains configuration options for cache providers.
type Options struct {
	// DefaultTTL is the default time-to-live for cache items.
	DefaultTTL time.Duration

	// MaxSize is the maximum number of items (for in-memory provider).
	// 0 selects the default (10000); a negative value means unbounded.
	MaxSize int

	// CleanupInterval is how often the in-memory provider removes expired items
	// (default: 1 minute).
	CleanupInterval time.Duration

	// EvictionPolicy determines how items are evicted (LRU, LFU, etc).
	EvictionPolicy string
}
