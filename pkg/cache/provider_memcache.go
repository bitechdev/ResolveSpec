package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

// MemcacheProvider is a Memcache implementation of the Provider interface.
type MemcacheProvider struct {
	client  *memcache.Client
	options *Options
}

// MemcacheConfig contains Memcache-specific configuration.
type MemcacheConfig struct {
	// Servers is a list of memcache server addresses (e.g., "localhost:11211")
	Servers []string

	// MaxIdleConns is the maximum number of idle connections (default: 2)
	MaxIdleConns int

	// Timeout for connection operations (default: 1 second)
	Timeout time.Duration

	// Options contains general cache options
	Options *Options
}

// NewMemcacheProvider creates a new Memcache cache provider.
func NewMemcacheProvider(config *MemcacheConfig) (*MemcacheProvider, error) {
	if config == nil {
		config = &MemcacheConfig{
			Servers: []string{"localhost:11211"},
		}
	}

	if len(config.Servers) == 0 {
		config.Servers = []string{"localhost:11211"}
	}

	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 2
	}

	if config.Timeout == 0 {
		config.Timeout = 1 * time.Second
	}

	if config.Options == nil {
		config.Options = &Options{
			DefaultTTL: 5 * time.Minute,
		}
	}

	client := memcache.New(config.Servers...)
	client.MaxIdleConns = config.MaxIdleConns
	client.Timeout = config.Timeout

	// Test connection
	if err := client.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to Memcache: %w", err)
	}

	return &MemcacheProvider{
		client:  client,
		options: config.Options,
	}, nil
}

// Get retrieves a value from the cache by key.
func (m *MemcacheProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	item, err := m.client.Get(key)
	if err == memcache.ErrCacheMiss {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return item.Value, true
}

// Set stores a value in the cache with the specified TTL.
func (m *MemcacheProvider) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	item := &memcache.Item{
		Key:        key,
		Value:      value,
		Expiration: int32(ttl.Seconds()),
	}

	return m.client.Set(item)
}

// Delete removes a key from the cache.
func (m *MemcacheProvider) Delete(ctx context.Context, key string) error {
	err := m.client.Delete(key)
	if err == memcache.ErrCacheMiss {
		return nil
	}
	return err
}

// DeleteByPattern removes all keys matching the pattern.
// Note: Memcache does not support pattern-based deletion natively.
// This is a no-op for memcache and returns an error.
func (m *MemcacheProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	return fmt.Errorf("pattern-based deletion is not supported by Memcache")
}

// Clear removes all items from the cache.
func (m *MemcacheProvider) Clear(ctx context.Context) error {
	return m.client.FlushAll()
}

// Exists checks if a key exists in the cache.
func (m *MemcacheProvider) Exists(ctx context.Context, key string) bool {
	_, err := m.client.Get(key)
	return err == nil
}

// Close closes the provider and releases any resources.
func (m *MemcacheProvider) Close() error {
	// Memcache client doesn't have a close method
	return nil
}

// Stats returns statistics about the cache provider.
// Note: Memcache provider returns limited statistics.
func (m *MemcacheProvider) Stats(ctx context.Context) (*CacheStats, error) {
	stats := &CacheStats{
		ProviderType: "memcache",
		ProviderStats: map[string]any{
			"note": "Memcache does not provide detailed statistics through the standard client",
		},
	}

	return stats, nil
}
