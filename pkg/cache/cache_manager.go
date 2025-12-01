package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Cache is the main cache manager that wraps a Provider.
type Cache struct {
	provider Provider
}

// NewCache creates a new cache manager with the specified provider.
func NewCache(provider Provider) *Cache {
	return &Cache{
		provider: provider,
	}
}

// Get retrieves and deserializes a value from the cache.
func (c *Cache) Get(ctx context.Context, key string, dest interface{}) error {
	data, exists := c.provider.Get(ctx, key)
	if !exists {
		return fmt.Errorf("key not found: %s", key)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to deserialize: %w", err)
	}

	return nil
}

// GetBytes retrieves raw bytes from the cache.
func (c *Cache) GetBytes(ctx context.Context, key string) ([]byte, error) {
	data, exists := c.provider.Get(ctx, key)
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return data, nil
}

// Set serializes and stores a value in the cache with the specified TTL.
func (c *Cache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}

	return c.provider.Set(ctx, key, data, ttl)
}

// SetBytes stores raw bytes in the cache with the specified TTL.
func (c *Cache) SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.provider.Set(ctx, key, value, ttl)
}

// Delete removes a key from the cache.
func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.provider.Delete(ctx, key)
}

// DeleteByPattern removes all keys matching the pattern.
func (c *Cache) DeleteByPattern(ctx context.Context, pattern string) error {
	return c.provider.DeleteByPattern(ctx, pattern)
}

// Clear removes all items from the cache.
func (c *Cache) Clear(ctx context.Context) error {
	return c.provider.Clear(ctx)
}

// Exists checks if a key exists in the cache.
func (c *Cache) Exists(ctx context.Context, key string) bool {
	return c.provider.Exists(ctx, key)
}

// Stats returns statistics about the cache.
func (c *Cache) Stats(ctx context.Context) (*CacheStats, error) {
	return c.provider.Stats(ctx)
}

// Close closes the cache and releases any resources.
func (c *Cache) Close() error {
	return c.provider.Close()
}

// GetOrSet retrieves a value from cache, or sets it if it doesn't exist.
// The loader function is called only if the key is not found in cache.
func (c *Cache) GetOrSet(ctx context.Context, key string, dest interface{}, ttl time.Duration, loader func() (interface{}, error)) error {
	// Try to get from cache first
	err := c.Get(ctx, key, dest)
	if err == nil {
		return nil
	}

	// Load the value
	value, err := loader()
	if err != nil {
		return fmt.Errorf("loader failed: %w", err)
	}

	// Store in cache
	if err := c.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("failed to cache value: %w", err)
	}

	// Populate dest with the loaded value
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to serialize loaded value: %w", err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to deserialize loaded value: %w", err)
	}

	return nil
}

// Remember is a convenience function that caches the result of a function call.
// It's similar to GetOrSet but returns the value directly.
func (c *Cache) Remember(ctx context.Context, key string, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	// Try to get from cache first as bytes
	data, err := c.GetBytes(ctx, key)
	if err == nil {
		var result interface{}
		if err := json.Unmarshal(data, &result); err == nil {
			return result, nil
		}
	}

	// Load the value
	value, err := loader()
	if err != nil {
		return nil, fmt.Errorf("loader failed: %w", err)
	}

	// Store in cache
	if err := c.Set(ctx, key, value, ttl); err != nil {
		return nil, fmt.Errorf("failed to cache value: %w", err)
	}

	return value, nil
}
