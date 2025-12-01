package cache

import (
	"context"
	"testing"
	"time"
)

func TestSetDefaultCache(t *testing.T) {
	// Create a custom cache instance
	provider := NewMemoryProvider(&Options{
		DefaultTTL: 1 * time.Minute,
		MaxSize:    50,
	})
	customCache := NewCache(provider)

	// Set it as the default
	SetDefaultCache(customCache)

	// Verify it's now the default
	retrievedCache := GetDefaultCache()
	if retrievedCache != customCache {
		t.Error("SetDefaultCache did not set the cache correctly")
	}

	// Test that we can use it
	ctx := context.Background()
	testKey := "test_key"
	testValue := "test_value"

	err := retrievedCache.Set(ctx, testKey, testValue, time.Minute)
	if err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	var result string
	err = retrievedCache.Get(ctx, testKey, &result)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if result != testValue {
		t.Errorf("Expected %s, got %s", testValue, result)
	}

	// Clean up - reset to default
	SetDefaultCache(nil)
}

func TestGetDefaultCacheInitialization(t *testing.T) {
	// Reset to nil first
	SetDefaultCache(nil)

	// GetDefaultCache should auto-initialize
	cache := GetDefaultCache()
	if cache == nil {
		t.Error("GetDefaultCache should auto-initialize, got nil")
	}

	// Should be usable
	ctx := context.Background()
	err := cache.Set(ctx, "test", "value", time.Minute)
	if err != nil {
		t.Errorf("Failed to use auto-initialized cache: %v", err)
	}

	// Clean up
	SetDefaultCache(nil)
}
