package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	// memcacheMaxRelativeTTL is the largest expiry memcached treats as relative seconds;
	// anything larger is interpreted as an absolute Unix timestamp.
	memcacheMaxRelativeTTL = 30 * 24 * 60 * 60

	// memcacheMaxTagKeys bounds the per-tag key list so it stays under memcached's 1MB item limit.
	memcacheMaxTagKeys = 5000

	memcacheCASRetries = 5
)

// MemcacheProvider is a Memcache implementation of the Provider interface.
type MemcacheProvider struct {
	client     *memcache.Client
	options    *Options
	allowFlush bool
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

	// AllowFlush permits Clear() to run flush_all, which wipes every key on every
	// configured server (including data not owned by this cache). Off by default.
	AllowFlush bool
}

// NewMemcacheProvider creates a new Memcache cache provider.
func NewMemcacheProvider(config *MemcacheConfig) (*MemcacheProvider, error) {
	// Work on a copy so the caller's struct is not mutated
	var cfg MemcacheConfig
	if config != nil {
		cfg = *config
		cfg.Servers = append([]string(nil), config.Servers...)
	}
	if cfg.Options != nil {
		o := *cfg.Options
		cfg.Options = &o
	}

	if len(cfg.Servers) == 0 {
		cfg.Servers = []string{"localhost:11211"}
	}

	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 2
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 1 * time.Second
	}

	if cfg.Options == nil {
		cfg.Options = &Options{
			DefaultTTL: 5 * time.Minute,
		}
	}

	client := memcache.New(cfg.Servers...)
	client.MaxIdleConns = cfg.MaxIdleConns
	client.Timeout = cfg.Timeout

	// Test connection
	if err := client.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to Memcache: %w", err)
	}

	return &MemcacheProvider{
		client:     client,
		options:    cfg.Options,
		allowFlush: cfg.AllowFlush,
	}, nil
}

// memcacheKey maps a caller key to a legal memcached key. User keys live under the "k:"
// prefix, so they can never collide with the "cache:tag:" / "cache:tags:" index keys.
// Keys that are too long or contain illegal bytes (whitespace/control characters)
// are replaced with their SHA-256.
func memcacheKey(key string) string {
	k := "k:" + key
	if len(k) > 200 || !legalMemcacheKey(k) {
		sum := sha256.Sum256([]byte(key))
		return "k:h:" + hex.EncodeToString(sum[:])
	}
	return k
}

func legalMemcacheKey(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] <= ' ' || key[i] == 0x7f {
			return false
		}
	}
	return true
}

// memcacheTagKey maps a tag to its index key, hashing if it is not a legal key.
func memcacheTagKey(prefix, name string) string {
	k := prefix + name
	if len(k) > 200 || !legalMemcacheKey(k) {
		sum := sha256.Sum256([]byte(name))
		return prefix + "h:" + hex.EncodeToString(sum[:])
	}
	return k
}

// memcacheExpiry converts a TTL into a memcached expiry value, switching to an
// absolute Unix timestamp above 30 days as the protocol requires.
func memcacheExpiry(ttl time.Duration) int32 {
	if ttl <= 0 {
		return 0 // never expires
	}
	secs := int64(ttl.Seconds())
	if secs > memcacheMaxRelativeTTL {
		return int32(time.Now().Add(ttl).Unix())
	}
	if secs == 0 {
		secs = 1
	}
	return int32(secs)
}

// Get retrieves a value from the cache by key.
func (m *MemcacheProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	item, err := m.client.Get(memcacheKey(key))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, false
	}
	if err != nil {
		// Reported as a miss (the Provider interface cannot express errors), but not silently
		logger.Warn("cache: memcache GET failed: %v", err)
		return nil, false
	}
	return item.Value, true
}

// Set stores a value in the cache with the specified TTL.
func (m *MemcacheProvider) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	return m.client.Set(&memcache.Item{
		Key:        memcacheKey(key),
		Value:      value,
		Expiration: memcacheExpiry(ttl),
	})
}

// SetWithTags stores a value in the cache with the specified TTL and tags.
// Note: Tag support in Memcache is limited and less efficient than Redis. The
// tag index is updated with compare-and-swap; if it cannot be updated the value is
// removed again and an error is returned, so an untracked entry is never left behind.
func (m *MemcacheProvider) SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	expiration := memcacheExpiry(ttl)
	mkey := memcacheKey(key)

	if err := m.client.Set(&memcache.Item{Key: mkey, Value: value, Expiration: expiration}); err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}

	fail := func(err error) error {
		_ = m.client.Delete(mkey) // best-effort rollback; the original error is what matters
		return err
	}

	tagsData, err := json.Marshal(tags)
	if err != nil {
		return fail(fmt.Errorf("failed to marshal tags: %w", err))
	}
	if err := m.client.Set(&memcache.Item{
		Key:        memcacheTagKey("cache:tags:", key),
		Value:      tagsData,
		Expiration: expiration,
	}); err != nil {
		return fail(err)
	}

	// Tag lists live longer than the entries they index
	tagExpiry := memcacheExpiry(ttl + time.Hour)
	for _, tag := range tags {
		if err := m.updateTagKeys(memcacheTagKey("cache:tag:", tag), tagExpiry, func(keys []string) ([]string, error) {
			for _, k := range keys {
				if k == key {
					return keys, nil
				}
			}
			if len(keys) >= memcacheMaxTagKeys {
				return nil, fmt.Errorf("tag index for %q is full (%d keys)", tag, memcacheMaxTagKeys)
			}
			return append(keys, key), nil
		}); err != nil {
			return fail(err)
		}
	}

	return nil
}

// updateTagKeys applies fn to a tag's key list using compare-and-swap.
func (m *MemcacheProvider) updateTagKeys(tagKey string, expiry int32, fn func([]string) ([]string, error)) error {
	for attempt := 0; attempt < memcacheCASRetries; attempt++ {
		item, err := m.client.Get(tagKey)
		var keys []string
		switch {
		case errors.Is(err, memcache.ErrCacheMiss):
			item = nil
		case err != nil:
			return err
		default:
			if err := json.Unmarshal(item.Value, &keys); err != nil {
				return fmt.Errorf("failed to unmarshal tag keys: %w", err)
			}
		}

		keys, err = fn(keys)
		if err != nil {
			return err
		}
		data, err := json.Marshal(keys)
		if err != nil {
			return err
		}

		if item == nil {
			err = m.client.Add(&memcache.Item{Key: tagKey, Value: data, Expiration: expiry})
			if errors.Is(err, memcache.ErrNotStored) {
				continue // someone created it first; retry
			}
			return err
		}
		item.Value = data
		item.Expiration = expiry
		err = m.client.CompareAndSwap(item)
		if errors.Is(err, memcache.ErrCASConflict) || errors.Is(err, memcache.ErrNotStored) || errors.Is(err, memcache.ErrCacheMiss) {
			continue
		}
		return err
	}
	return fmt.Errorf("tag index %q: too much contention, giving up", tagKey)
}

// Delete removes a key from the cache.
func (m *MemcacheProvider) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mkey := memcacheKey(key)

	// Get tags for this key
	tagsKey := memcacheTagKey("cache:tags:", key)
	if item, err := m.client.Get(tagsKey); err == nil {
		var tags []string
		if err := json.Unmarshal(item.Value, &tags); err == nil {
			for _, tag := range tags {
				err := m.updateTagKeys(memcacheTagKey("cache:tag:", tag), memcacheExpiry(m.options.DefaultTTL+time.Hour), func(keys []string) ([]string, error) {
					out := make([]string, 0, len(keys))
					for _, k := range keys {
						if k != key {
							out = append(out, k)
						}
					}
					return out, nil
				})
				if err != nil {
					logger.Warn("cache: failed to update memcache tag index on delete: %v", err)
				}
			}
		}
		if err := m.client.Delete(tagsKey); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
			logger.Warn("cache: failed to delete memcache tags key: %v", err)
		}
	}

	// Delete the actual key
	err := m.client.Delete(mkey)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil
	}
	return err
}

// DeleteByTag removes all keys associated with the given tag.
func (m *MemcacheProvider) DeleteByTag(ctx context.Context, tag string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tagKey := memcacheTagKey("cache:tag:", tag)

	item, err := m.client.Get(tagKey)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil
	}
	if err != nil {
		return err
	}

	var keys []string
	if err := json.Unmarshal(item.Value, &keys); err != nil {
		return fmt.Errorf("failed to unmarshal tag keys: %w", err)
	}

	var firstErr error
	note := func(err error) {
		if err != nil && !errors.Is(err, memcache.ErrCacheMiss) && firstErr == nil {
			firstErr = err
		}
	}
	for _, key := range keys {
		note(m.client.Delete(memcacheKey(key)))
		note(m.client.Delete(memcacheTagKey("cache:tags:", key)))
	}

	if firstErr != nil {
		return firstErr // keep the tag index so the invalidation can be retried
	}
	note(m.client.Delete(tagKey))
	return firstErr
}

// DeleteByPattern is not supported by Memcache; it always returns an error.
// Use tags (SetWithTags / DeleteByTag) for group invalidation instead.
func (m *MemcacheProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	return fmt.Errorf("pattern-based deletion is not supported by Memcache")
}

// Clear removes all items from the cache.
// It runs flush_all on every configured server and therefore requires MemcacheConfig.AllowFlush.
func (m *MemcacheProvider) Clear(ctx context.Context) error {
	if !m.allowFlush {
		return ErrFlushNotAllowed
	}
	return m.client.FlushAll()
}

// Exists checks if a key exists in the cache.
func (m *MemcacheProvider) Exists(ctx context.Context, key string) bool {
	if ctx.Err() != nil {
		return false
	}
	_, err := m.client.Get(memcacheKey(key))
	return err == nil
}

// Close closes the provider and releases idle connections.
func (m *MemcacheProvider) Close() error {
	return m.client.Close()
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
