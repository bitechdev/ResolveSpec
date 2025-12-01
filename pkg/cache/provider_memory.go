package cache

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// memoryItem represents a cached item in memory.
type memoryItem struct {
	Value      []byte
	Expiration time.Time
	LastAccess time.Time
	HitCount   int64
}

// isExpired checks if the item has expired.
func (m *memoryItem) isExpired() bool {
	if m.Expiration.IsZero() {
		return false
	}
	return time.Now().After(m.Expiration)
}

// MemoryProvider is an in-memory implementation of the Provider interface.
type MemoryProvider struct {
	mu      sync.RWMutex
	items   map[string]*memoryItem
	options *Options
	hits    int64
	misses  int64
}

// NewMemoryProvider creates a new in-memory cache provider.
func NewMemoryProvider(opts *Options) *MemoryProvider {
	if opts == nil {
		opts = &Options{
			DefaultTTL: 5 * time.Minute,
			MaxSize:    10000,
		}
	}

	return &MemoryProvider{
		items:   make(map[string]*memoryItem),
		options: opts,
	}
}

// Get retrieves a value from the cache by key.
func (m *MemoryProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, exists := m.items[key]
	if !exists {
		m.misses++
		return nil, false
	}

	if item.isExpired() {
		delete(m.items, key)
		m.misses++
		return nil, false
	}

	item.LastAccess = time.Now()
	item.HitCount++
	m.hits++

	return item.Value, true
}

// Set stores a value in the cache with the specified TTL.
func (m *MemoryProvider) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	var expiration time.Time
	if ttl > 0 {
		expiration = time.Now().Add(ttl)
	}

	// Check max size and evict if necessary
	if m.options.MaxSize > 0 && len(m.items) >= m.options.MaxSize {
		if _, exists := m.items[key]; !exists {
			m.evictOne()
		}
	}

	m.items[key] = &memoryItem{
		Value:      value,
		Expiration: expiration,
		LastAccess: time.Now(),
	}

	return nil
}

// Delete removes a key from the cache.
func (m *MemoryProvider) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.items, key)
	return nil
}

// DeleteByPattern removes all keys matching the pattern.
func (m *MemoryProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	for key := range m.items {
		if re.MatchString(key) {
			delete(m.items, key)
		}
	}

	return nil
}

// Clear removes all items from the cache.
func (m *MemoryProvider) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = make(map[string]*memoryItem)
	m.hits = 0
	m.misses = 0
	return nil
}

// Exists checks if a key exists in the cache.
func (m *MemoryProvider) Exists(ctx context.Context, key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	item, exists := m.items[key]
	if !exists {
		return false
	}

	return !item.isExpired()
}

// Close closes the provider and releases any resources.
func (m *MemoryProvider) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = nil
	return nil
}

// Stats returns statistics about the cache provider.
func (m *MemoryProvider) Stats(ctx context.Context) (*CacheStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Clean expired items first
	validKeys := 0
	for _, item := range m.items {
		if !item.isExpired() {
			validKeys++
		}
	}

	return &CacheStats{
		Hits:         m.hits,
		Misses:       m.misses,
		Keys:         int64(validKeys),
		ProviderType: "memory",
		ProviderStats: map[string]any{
			"capacity": m.options.MaxSize,
		},
	}, nil
}

// evictOne removes one item from the cache using LRU strategy.
func (m *MemoryProvider) evictOne() {
	var oldestKey string
	var oldestTime time.Time

	for key, item := range m.items {
		if item.isExpired() {
			delete(m.items, key)
			return
		}

		if oldestKey == "" || item.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.LastAccess
		}
	}

	if oldestKey != "" {
		delete(m.items, oldestKey)
	}
}

// CleanExpired removes all expired items from the cache.
func (m *MemoryProvider) CleanExpired(ctx context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for key, item := range m.items {
		if item.isExpired() {
			delete(m.items, key)
			count++
		}
	}

	return count
}
