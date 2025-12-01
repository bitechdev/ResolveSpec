package cache

import (
	"context"
	"fmt"
	"log"
	"time"
)

// ExampleInMemoryCache demonstrates using the in-memory cache provider.
func ExampleInMemoryCache() {
	// Initialize with in-memory provider
	err := UseMemory(&Options{
		DefaultTTL: 5 * time.Minute,
		MaxSize:    1000,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Get the cache instance
	cache := GetDefaultCache()

	// Store a value
	type User struct {
		ID   int
		Name string
	}

	user := User{ID: 1, Name: "John Doe"}
	err = cache.Set(ctx, "user:1", user, 10*time.Minute)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	// Retrieve a value
	var retrieved User
	err = cache.Get(ctx, "user:1", &retrieved)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	fmt.Printf("Retrieved user: %+v\n", retrieved)

	// Check if key exists
	exists := cache.Exists(ctx, "user:1")
	fmt.Printf("Key exists: %v\n", exists)

	// Delete a key
	err = cache.Delete(ctx, "user:1")
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	// Get statistics
	stats, err := cache.Stats(ctx)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}
	fmt.Printf("Cache stats: %+v\n", stats)
	_ = Close()
}

// ExampleRedisCache demonstrates using the Redis cache provider.
func ExampleRedisCache() {
	// Initialize with Redis provider
	err := UseRedis(&RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "", // Set if Redis requires authentication
		DB:       0,
		Options: &Options{
			DefaultTTL: 5 * time.Minute,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Get the cache instance
	cache := GetDefaultCache()

	// Store raw bytes
	data := []byte("Hello, Redis!")
	err = cache.SetBytes(ctx, "greeting", data, 1*time.Hour)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	// Retrieve raw bytes
	retrieved, err := cache.GetBytes(ctx, "greeting")
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	fmt.Printf("Retrieved data: %s\n", string(retrieved))

	// Clear all cache
	err = cache.Clear(ctx)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}
	_ = Close()
}

// ExampleMemcacheCache demonstrates using the Memcache cache provider.
func ExampleMemcacheCache() {
	// Initialize with Memcache provider
	err := UseMemcache(&MemcacheConfig{
		Servers: []string{"localhost:11211"},
		Options: &Options{
			DefaultTTL: 5 * time.Minute,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Get the cache instance
	cache := GetDefaultCache()

	// Store a value
	type Product struct {
		ID    int
		Name  string
		Price float64
	}

	product := Product{ID: 100, Name: "Widget", Price: 29.99}
	err = cache.Set(ctx, "product:100", product, 30*time.Minute)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	// Retrieve a value
	var retrieved Product
	err = cache.Get(ctx, "product:100", &retrieved)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	fmt.Printf("Retrieved product: %+v\n", retrieved)
	_ = Close()
}

// ExampleGetOrSet demonstrates the GetOrSet pattern for lazy loading.
func ExampleGetOrSet() {
	err := UseMemory(&Options{
		DefaultTTL: 5 * time.Minute,
		MaxSize:    1000,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	cache := GetDefaultCache()

	type ExpensiveData struct {
		Result string
	}

	var data ExpensiveData
	err = cache.GetOrSet(ctx, "expensive:computation", &data, 10*time.Minute, func() (interface{}, error) {
		// This expensive operation only runs if the key is not in cache
		fmt.Println("Computing expensive result...")
		time.Sleep(1 * time.Second)
		return ExpensiveData{Result: "computed value"}, nil
	})
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	fmt.Printf("Data: %+v\n", data)

	// Second call will use cached value
	err = cache.GetOrSet(ctx, "expensive:computation", &data, 10*time.Minute, func() (interface{}, error) {
		fmt.Println("This won't be called!")
		return ExpensiveData{Result: "new value"}, nil
	})
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	fmt.Printf("Cached data: %+v\n", data)
	_ = Close()
}

// ExampleCustomProvider demonstrates using a custom provider.
func ExampleCustomProvider() {
	// Create a custom provider
	memProvider := NewMemoryProvider(&Options{
		DefaultTTL: 10 * time.Minute,
		MaxSize:    500,
	})

	// Initialize with custom provider
	Initialize(memProvider)

	ctx := context.Background()
	cache := GetDefaultCache()

	// Use the cache
	err := cache.SetBytes(ctx, "key", []byte("value"), 5*time.Minute)
	if err != nil {
		_ = Close()
		log.Fatal(err)
	}

	// Clean expired items (memory provider specific)
	if mp, ok := cache.provider.(*MemoryProvider); ok {
		count := mp.CleanExpired(ctx)
		fmt.Printf("Cleaned %d expired items\n", count)
	}
	_ = Close()
}

// ExampleDeleteByPattern demonstrates pattern-based deletion (Redis only).
func ExampleDeleteByPattern() {
	err := UseRedis(&RedisConfig{
		Host: "localhost",
		Port: 6379,
		Options: &Options{
			DefaultTTL: 5 * time.Minute,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	cache := GetDefaultCache()

	// Store multiple keys with a pattern
	_ = cache.SetBytes(ctx, "user:1:profile", []byte("profile1"), 10*time.Minute)
	_ = cache.SetBytes(ctx, "user:2:profile", []byte("profile2"), 10*time.Minute)
	_ = cache.SetBytes(ctx, "user:1:settings", []byte("settings1"), 10*time.Minute)

	// Delete all keys matching pattern (Redis glob pattern)
	err = cache.DeleteByPattern(ctx, "user:*:profile")
	if err != nil {
		_ = Close()
		log.Print(err)
		return
	}

	fmt.Println("Deleted all user profile keys")
	_ = Close()
}
