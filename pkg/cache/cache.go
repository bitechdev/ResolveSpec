package cache

import (
	"context"
	"fmt"
	"time"
)

var (
	defaultCache *Cache
)

// Initialize initializes the cache with a provider.
// If not called, the package will use an in-memory provider by default.
func Initialize(provider Provider) {
	defaultCache = NewCache(provider)
}

// UseMemory configures the cache to use in-memory storage.
func UseMemory(opts *Options) error {
	provider := NewMemoryProvider(opts)
	defaultCache = NewCache(provider)
	return nil
}

// UseRedis configures the cache to use Redis storage.
func UseRedis(config *RedisConfig) error {
	provider, err := NewRedisProvider(config)
	if err != nil {
		return fmt.Errorf("failed to initialize Redis provider: %w", err)
	}
	defaultCache = NewCache(provider)
	return nil
}

// UseMemcache configures the cache to use Memcache storage.
func UseMemcache(config *MemcacheConfig) error {
	provider, err := NewMemcacheProvider(config)
	if err != nil {
		return fmt.Errorf("failed to initialize Memcache provider: %w", err)
	}
	defaultCache = NewCache(provider)
	return nil
}

// GetDefaultCache returns the default cache instance.
// Initializes with in-memory provider if not already initialized.
func GetDefaultCache() *Cache {
	if defaultCache == nil {
		_ = UseMemory(&Options{
			DefaultTTL: 5 * time.Minute,
			MaxSize:    10000,
		})
	}
	return defaultCache
}

// SetDefaultCache sets a custom cache instance as the default cache.
// This is useful for testing or when you want to use a pre-configured cache instance.
func SetDefaultCache(cache *Cache) {
	defaultCache = cache
}

// GetStats returns cache statistics.
func GetStats(ctx context.Context) (*CacheStats, error) {
	cache := GetDefaultCache()
	return cache.Stats(ctx)
}

// Close closes the cache and releases resources.
func Close() error {
	if defaultCache != nil {
		return defaultCache.Close()
	}
	return nil
}
