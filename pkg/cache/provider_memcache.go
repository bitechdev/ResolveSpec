package cache

import (
	"context"
	"encoding/json"
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

// SetWithTags stores a value in the cache with the specified TTL and tags.
// Note: Tag support in Memcache is limited and less efficient than Redis.
func (m *MemcacheProvider) SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	expiration := int32(ttl.Seconds())

	// Set the main value
	item := &memcache.Item{
		Key:        key,
		Value:      value,
		Expiration: expiration,
	}
	if err := m.client.Set(item); err != nil {
		return err
	}

	// Store tags for this key
	if len(tags) > 0 {
		tagsData, err := json.Marshal(tags)
		if err != nil {
			return fmt.Errorf("failed to marshal tags: %w", err)
		}

		tagsItem := &memcache.Item{
			Key:        fmt.Sprintf("cache:tags:%s", key),
			Value:      tagsData,
			Expiration: expiration,
		}
		if err := m.client.Set(tagsItem); err != nil {
			return err
		}

		// Add key to each tag's key list
		for _, tag := range tags {
			tagKey := fmt.Sprintf("cache:tag:%s", tag)

			// Get existing keys for this tag
			var keys []string
			if item, err := m.client.Get(tagKey); err == nil {
				_ = json.Unmarshal(item.Value, &keys)
			}

			// Add current key if not already present
			found := false
			for _, k := range keys {
				if k == key {
					found = true
					break
				}
			}
			if !found {
				keys = append(keys, key)
			}

			// Store updated key list
			keysData, err := json.Marshal(keys)
			if err != nil {
				continue
			}

			tagItem := &memcache.Item{
				Key:        tagKey,
				Value:      keysData,
				Expiration: expiration + 3600, // Give tag lists longer TTL
			}
			_ = m.client.Set(tagItem)
		}
	}

	return nil
}

// Delete removes a key from the cache.
func (m *MemcacheProvider) Delete(ctx context.Context, key string) error {
	// Get tags for this key
	tagsKey := fmt.Sprintf("cache:tags:%s", key)
	if item, err := m.client.Get(tagsKey); err == nil {
		var tags []string
		if err := json.Unmarshal(item.Value, &tags); err == nil {
			// Remove key from each tag's key list
			for _, tag := range tags {
				tagKey := fmt.Sprintf("cache:tag:%s", tag)
				if tagItem, err := m.client.Get(tagKey); err == nil {
					var keys []string
					if err := json.Unmarshal(tagItem.Value, &keys); err == nil {
						// Remove current key from the list
						newKeys := make([]string, 0, len(keys))
						for _, k := range keys {
							if k != key {
								newKeys = append(newKeys, k)
							}
						}
						// Update the tag's key list
						if keysData, err := json.Marshal(newKeys); err == nil {
							tagItem.Value = keysData
							_ = m.client.Set(tagItem)
						}
					}
				}
			}
		}
		// Delete the tags key
		_ = m.client.Delete(tagsKey)
	}

	// Delete the actual key
	err := m.client.Delete(key)
	if err == memcache.ErrCacheMiss {
		return nil
	}
	return err
}

// DeleteByTag removes all keys associated with the given tag.
func (m *MemcacheProvider) DeleteByTag(ctx context.Context, tag string) error {
	tagKey := fmt.Sprintf("cache:tag:%s", tag)

	// Get all keys associated with this tag
	item, err := m.client.Get(tagKey)
	if err == memcache.ErrCacheMiss {
		return nil
	}
	if err != nil {
		return err
	}

	var keys []string
	if err := json.Unmarshal(item.Value, &keys); err != nil {
		return fmt.Errorf("failed to unmarshal tag keys: %w", err)
	}

	// Delete all keys
	for _, key := range keys {
		_ = m.client.Delete(key)
		// Also delete the tags key for this cache key
		tagsKey := fmt.Sprintf("cache:tags:%s", key)
		_ = m.client.Delete(tagsKey)
	}

	// Delete the tag key itself
	_ = m.client.Delete(tagKey)

	return nil
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
