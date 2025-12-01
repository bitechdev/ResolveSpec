package cache

import (
	"context"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestBuildQueryCacheKey(t *testing.T) {
	filters := []common.FilterOption{
		{Column: "name", Operator: "eq", Value: "test"},
		{Column: "age", Operator: "gt", Value: 25},
	}
	sorts := []common.SortOption{
		{Column: "name", Direction: "asc"},
	}

	// Generate cache key
	key1 := BuildQueryCacheKey("users", filters, sorts, "status = 'active'", "")

	// Same parameters should generate same key
	key2 := BuildQueryCacheKey("users", filters, sorts, "status = 'active'", "")

	if key1 != key2 {
		t.Errorf("Expected same cache keys for identical parameters, got %s and %s", key1, key2)
	}

	// Different parameters should generate different key
	key3 := BuildQueryCacheKey("users", filters, sorts, "status = 'inactive'", "")

	if key1 == key3 {
		t.Errorf("Expected different cache keys for different parameters, got %s and %s", key1, key3)
	}
}

func TestBuildExtendedQueryCacheKey(t *testing.T) {
	filters := []common.FilterOption{
		{Column: "name", Operator: "eq", Value: "test"},
	}
	sorts := []common.SortOption{
		{Column: "name", Direction: "asc"},
	}
	expandOpts := []interface{}{
		map[string]interface{}{
			"relation": "posts",
			"where":    "status = 'published'",
		},
	}

	// Generate cache key
	key1 := BuildExtendedQueryCacheKey("users", filters, sorts, "", "", expandOpts, false, "", "")

	// Same parameters should generate same key
	key2 := BuildExtendedQueryCacheKey("users", filters, sorts, "", "", expandOpts, false, "", "")

	if key1 != key2 {
		t.Errorf("Expected same cache keys for identical parameters")
	}

	// Different distinct value should generate different key
	key3 := BuildExtendedQueryCacheKey("users", filters, sorts, "", "", expandOpts, true, "", "")

	if key1 == key3 {
		t.Errorf("Expected different cache keys for different distinct values")
	}
}

func TestGetQueryTotalCacheKey(t *testing.T) {
	hash := "abc123"
	key := GetQueryTotalCacheKey(hash)

	expected := "query_total:abc123"
	if key != expected {
		t.Errorf("Expected %s, got %s", expected, key)
	}
}

func TestCachedTotalIntegration(t *testing.T) {
	// Initialize cache with memory provider for testing
	UseMemory(&Options{
		DefaultTTL: 1 * time.Minute,
		MaxSize:    100,
	})

	ctx := context.Background()

	// Create test data
	filters := []common.FilterOption{
		{Column: "status", Operator: "eq", Value: "active"},
	}
	sorts := []common.SortOption{
		{Column: "created_at", Direction: "desc"},
	}

	// Build cache key
	cacheKeyHash := BuildQueryCacheKey("test_table", filters, sorts, "", "")
	cacheKey := GetQueryTotalCacheKey(cacheKeyHash)

	// Store a total count in cache
	totalToCache := CachedTotal{Total: 42}
	err := GetDefaultCache().Set(ctx, cacheKey, totalToCache, time.Minute)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	// Retrieve from cache
	var cachedTotal CachedTotal
	err = GetDefaultCache().Get(ctx, cacheKey, &cachedTotal)
	if err != nil {
		t.Fatalf("Failed to get from cache: %v", err)
	}

	if cachedTotal.Total != 42 {
		t.Errorf("Expected total 42, got %d", cachedTotal.Total)
	}

	// Test cache miss
	nonExistentKey := GetQueryTotalCacheKey("nonexistent")
	var missedTotal CachedTotal
	err = GetDefaultCache().Get(ctx, nonExistentKey, &missedTotal)
	if err == nil {
		t.Errorf("Expected error for cache miss, got nil")
	}
}

func TestHashString(t *testing.T) {
	input1 := "test string"
	input2 := "test string"
	input3 := "different string"

	hash1 := hashString(input1)
	hash2 := hashString(input2)
	hash3 := hashString(input3)

	// Same input should produce same hash
	if hash1 != hash2 {
		t.Errorf("Expected same hash for identical inputs")
	}

	// Different input should produce different hash
	if hash1 == hash3 {
		t.Errorf("Expected different hash for different inputs")
	}

	// Hash should be hex encoded SHA256 (64 characters)
	if len(hash1) != 64 {
		t.Errorf("Expected hash length of 64, got %d", len(hash1))
	}
}
