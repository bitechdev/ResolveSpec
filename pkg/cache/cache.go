package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

var defaultCache atomic.Pointer[Cache]

// swapOwned installs c as the default and closes the displaced cache, which this
// package created and therefore owns.
func swapOwned(c *Cache) {
	if old := defaultCache.Swap(c); old != nil && old != c {
		_ = old.Close() // best-effort: the displaced provider is being discarded
	}
}

// Initialize initializes the cache with a provider.
// If not called, the package will use an in-memory provider by default.
func Initialize(provider Provider) {
	swapOwned(NewCache(provider))
}

// UseMemory configures the cache to use in-memory storage.
func UseMemory(opts *Options) error {
	swapOwned(NewCache(NewMemoryProvider(opts)))
	return nil
}

// UseRedis configures the cache to use Redis storage.
func UseRedis(config *RedisConfig) error {
	provider, err := NewRedisProvider(config)
	if err != nil {
		return fmt.Errorf("failed to initialize Redis provider: %w", err)
	}
	swapOwned(NewCache(provider))
	return nil
}

// UseMemcache configures the cache to use Memcache storage.
func UseMemcache(config *MemcacheConfig) error {
	provider, err := NewMemcacheProvider(config)
	if err != nil {
		return fmt.Errorf("failed to initialize Memcache provider: %w", err)
	}
	swapOwned(NewCache(provider))
	return nil
}

// GetDefaultCache returns the default cache instance.
// Initializes with in-memory provider if not already initialized.
// Safe for concurrent use.
func GetDefaultCache() *Cache {
	if c := defaultCache.Load(); c != nil {
		return c
	}
	fresh := NewCache(NewMemoryProvider(&Options{
		DefaultTTL: 5 * time.Minute,
		MaxSize:    10000,
	}))
	if defaultCache.CompareAndSwap(nil, fresh) {
		return fresh
	}
	_ = fresh.Close() // lost the race; discard our provider
	return defaultCache.Load()
}

// SetDefaultCache sets a custom cache instance as the default cache.
// This is useful for testing or when you want to use a pre-configured cache instance.
// The caller keeps ownership of both the new and the displaced cache; neither is closed.
func SetDefaultCache(cache *Cache) {
	defaultCache.Store(cache)
}

// GetStats returns cache statistics.
func GetStats(ctx context.Context) (*CacheStats, error) {
	cache := GetDefaultCache()
	return cache.Stats(ctx)
}

// Close closes the cache and releases resources.
func Close() error {
	if c := defaultCache.Load(); c != nil {
		return c.Close()
	}
	return nil
}
