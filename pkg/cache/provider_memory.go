package cache

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// memoryItem represents a cached item in memory.
type memoryItem struct {
	Value      []byte
	Expiration time.Time
	LastAccess time.Time
	HitCount   int64
	Tags       []string
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
	mu        sync.RWMutex
	items     map[string]*memoryItem
	tagToKeys map[string]map[string]struct{} // tag -> set of keys
	options   *Options
	hits      atomic.Int64
	misses    atomic.Int64
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
		items:     make(map[string]*memoryItem),
		tagToKeys: make(map[string]map[string]struct{}),
		options:   opts,
	}
}

// Get retrieves a value from the cache by key.
func (m *MemoryProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	// First try with read lock for fast path
	m.mu.RLock()
	item, exists := m.items[key]
	if !exists {
		m.mu.RUnlock()
		m.misses.Add(1)
		return nil, false
	}

	if item.isExpired() {
		m.mu.RUnlock()
		// Upgrade to write lock to delete expired item
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		m.misses.Add(1)
		return nil, false
	}

	// Update stats and access time with write lock
	value := item.Value
	m.mu.RUnlock()

	// Update access tracking with write lock
	m.mu.Lock()
	item.LastAccess = time.Now()
	item.HitCount++
	m.mu.Unlock()

	m.hits.Add(1)
	return value, true
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

// SetWithTags stores a value in the cache with the specified TTL and tags.
func (m *MemoryProvider) SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
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

	// Remove old tag associations if key exists
	if oldItem, exists := m.items[key]; exists {
		for _, tag := range oldItem.Tags {
			if keySet, ok := m.tagToKeys[tag]; ok {
				delete(keySet, key)
				if len(keySet) == 0 {
					delete(m.tagToKeys, tag)
				}
			}
		}
	}

	// Store the item
	m.items[key] = &memoryItem{
		Value:      value,
		Expiration: expiration,
		LastAccess: time.Now(),
		Tags:       tags,
	}

	// Add new tag associations
	for _, tag := range tags {
		if m.tagToKeys[tag] == nil {
			m.tagToKeys[tag] = make(map[string]struct{})
		}
		m.tagToKeys[tag][key] = struct{}{}
	}

	return nil
}

// Delete removes a key from the cache.
func (m *MemoryProvider) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove tag associations
	if item, exists := m.items[key]; exists {
		for _, tag := range item.Tags {
			if keySet, ok := m.tagToKeys[tag]; ok {
				delete(keySet, key)
				if len(keySet) == 0 {
					delete(m.tagToKeys, tag)
				}
			}
		}
	}

	delete(m.items, key)
	return nil
}

// DeleteByTag removes all keys associated with the given tag.
func (m *MemoryProvider) DeleteByTag(ctx context.Context, tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get all keys associated with this tag
	keySet, exists := m.tagToKeys[tag]
	if !exists {
		return nil // No keys with this tag
	}

	// Delete all items with this tag
	for key := range keySet {
		if item, ok := m.items[key]; ok {
			// Remove this tag from the item's tag list
			newTags := make([]string, 0, len(item.Tags))
			for _, t := range item.Tags {
				if t != tag {
					newTags = append(newTags, t)
				}
			}

			// If item has no more tags, delete it
			// Otherwise update its tags
			if len(newTags) == 0 {
				delete(m.items, key)
			} else {
				item.Tags = newTags
			}
		}
	}

	// Remove the tag mapping
	delete(m.tagToKeys, tag)
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
	m.hits.Store(0)
	m.misses.Store(0)
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
		Hits:         m.hits.Load(),
		Misses:       m.misses.Load(),
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
