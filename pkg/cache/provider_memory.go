package cache

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

const (
	defaultMemoryMaxSize         = 10000
	defaultMemoryCleanupInterval = time.Minute
)

// ErrClosed is returned by a provider that has been closed.
var ErrClosed = errors.New("cache: provider closed")

// memoryItem represents a cached item in memory.
type memoryItem struct {
	Value      []byte
	Expiration time.Time
	Tags       []string
	lastAccess atomic.Int64 // unix nanos
	hitCount   atomic.Int64
}

func newMemoryItem(value []byte, expiration time.Time, tags []string) *memoryItem {
	buf := make([]byte, len(value))
	copy(buf, value)
	item := &memoryItem{Value: buf, Expiration: expiration, Tags: tags}
	item.lastAccess.Store(time.Now().UnixNano())
	return item
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
	closed    bool
	done      chan struct{}
	closeOnce sync.Once
}

// NewMemoryProvider creates a new in-memory cache provider.
// A MaxSize <= 0 selects the default (10000); use MaxSize -1 for an unbounded cache.
// A background goroutine removes expired items until Close is called.
func NewMemoryProvider(opts *Options) *MemoryProvider {
	var o Options
	if opts != nil {
		o = *opts // do not mutate the caller's struct
	} else {
		o = Options{DefaultTTL: 5 * time.Minute}
	}
	if o.MaxSize == 0 {
		o.MaxSize = defaultMemoryMaxSize
	}
	if o.CleanupInterval <= 0 {
		o.CleanupInterval = defaultMemoryCleanupInterval
	}

	m := &MemoryProvider{
		items:     make(map[string]*memoryItem),
		tagToKeys: make(map[string]map[string]struct{}),
		options:   &o,
		done:      make(chan struct{}),
	}
	go m.janitor(o.CleanupInterval)
	return m
}

func (m *MemoryProvider) janitor(interval time.Duration) {
	defer logger.CatchPanic("cache.MemoryProvider.janitor")()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			m.CleanExpired(context.Background())
		}
	}
}

// removeLocked deletes a key and its tag associations. Caller must hold m.mu for writing.
func (m *MemoryProvider) removeLocked(key string) {
	if item, ok := m.items[key]; ok {
		for _, tag := range item.Tags {
			if ks := m.tagToKeys[tag]; ks != nil {
				delete(ks, key)
				if len(ks) == 0 {
					delete(m.tagToKeys, tag)
				}
			}
		}
	}
	delete(m.items, key)
}

// Get retrieves a value from the cache by key.
func (m *MemoryProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	m.mu.RLock()
	item, exists := m.items[key]
	if !exists || m.closed {
		m.mu.RUnlock()
		m.misses.Add(1)
		return nil, false
	}

	if item.isExpired() {
		m.mu.RUnlock()
		// Delete only if the entry is still the same expired one
		m.mu.Lock()
		if cur, ok := m.items[key]; ok && cur == item {
			m.removeLocked(key)
		}
		m.mu.Unlock()
		m.misses.Add(1)
		return nil, false
	}

	item.lastAccess.Store(time.Now().UnixNano())
	item.hitCount.Add(1)
	out := make([]byte, len(item.Value))
	copy(out, item.Value)
	m.mu.RUnlock()

	m.hits.Add(1)
	return out, true
}

// Set stores a value in the cache with the specified TTL.
func (m *MemoryProvider) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return m.SetWithTags(ctx, key, value, ttl, nil)
}

// SetWithTags stores a value in the cache with the specified TTL and tags.
func (m *MemoryProvider) SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	if ttl == 0 {
		ttl = m.options.DefaultTTL
	}

	var expiration time.Time
	if ttl > 0 {
		expiration = time.Now().Add(ttl)
	}

	if _, exists := m.items[key]; exists {
		m.removeLocked(key) // drops old tag associations
	} else if m.options.MaxSize > 0 && len(m.items) >= m.options.MaxSize {
		m.evictOne()
	}

	m.items[key] = newMemoryItem(value, expiration, tags)

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

	m.removeLocked(key)
	return nil
}

// DeleteByTag removes all keys associated with the given tag.
func (m *MemoryProvider) DeleteByTag(ctx context.Context, tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	keySet, exists := m.tagToKeys[tag]
	if !exists {
		return nil // No keys with this tag
	}

	for key := range keySet {
		if item, ok := m.items[key]; ok {
			newTags := make([]string, 0, len(item.Tags))
			for _, t := range item.Tags {
				if t != tag {
					newTags = append(newTags, t)
				}
			}

			// If item has no more tags, delete it; otherwise update its tags
			if len(newTags) == 0 {
				delete(m.items, key)
			} else {
				item.Tags = newTags
			}
		}
	}

	delete(m.tagToKeys, tag)
	return nil
}

// DeleteByPattern removes all keys matching the pattern.
// The pattern is a Go regular expression (unanchored); it is compiled before the lock is taken.
func (m *MemoryProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for key := range m.items {
		if re.MatchString(key) {
			m.removeLocked(key)
		}
	}

	return nil
}

// Clear removes all items from the cache.
func (m *MemoryProvider) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = make(map[string]*memoryItem)
	m.tagToKeys = make(map[string]map[string]struct{})
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

// Close closes the provider, stops the janitor and releases stored items.
// Later writes return ErrClosed and reads report a miss.
func (m *MemoryProvider) Close() error {
	m.closeOnce.Do(func() { close(m.done) })

	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.items = make(map[string]*memoryItem)
	m.tagToKeys = make(map[string]map[string]struct{})
	return nil
}

// Stats returns statistics about the cache provider.
func (m *MemoryProvider) Stats(ctx context.Context) (*CacheStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Count non-expired items (read-only)
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
// Note: this is an O(n) scan. Caller must hold m.mu for writing.
func (m *MemoryProvider) evictOne() {
	var oldestKey string
	var oldest int64

	for key, item := range m.items {
		if item.isExpired() {
			m.removeLocked(key)
			return
		}

		if la := item.lastAccess.Load(); oldestKey == "" || la < oldest {
			oldestKey = key
			oldest = la
		}
	}

	if oldestKey != "" {
		m.removeLocked(oldestKey)
	}
}

// CleanExpired removes all expired items from the cache.
func (m *MemoryProvider) CleanExpired(ctx context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for key, item := range m.items {
		if item.isExpired() {
			m.removeLocked(key)
			count++
		}
	}

	return count
}
