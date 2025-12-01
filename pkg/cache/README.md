# Cache Package

A flexible, provider-based caching library for Go that supports multiple backend storage systems including in-memory, Redis, and Memcache.

## Features

- **Multiple Providers**: Support for in-memory, Redis, and Memcache backends
- **Pluggable Architecture**: Easy to add custom cache providers
- **Type-Safe API**: Automatic JSON serialization/deserialization
- **TTL Support**: Configurable time-to-live for cache entries
- **Context-Aware**: All operations support Go contexts
- **Statistics**: Built-in cache statistics and monitoring
- **Pattern Deletion**: Delete keys by pattern (Redis)
- **Lazy Loading**: GetOrSet pattern for easy cache-aside implementation

## Installation

```bash
go get github.com/bitechdev/ResolveSpec/pkg/cache
```

For Redis support:
```bash
go get github.com/redis/go-redis/v9
```

For Memcache support:
```bash
go get github.com/bradfitz/gomemcache/memcache
```

## Quick Start

### In-Memory Cache

```go
package main

import (
    "context"
    "time"
    "github.com/bitechdev/ResolveSpec/pkg/cache"
)

func main() {
    // Initialize with in-memory provider
    cache.UseMemory(&cache.Options{
        DefaultTTL: 5 * time.Minute,
        MaxSize:    10000,
    })
    defer cache.Close()

    ctx := context.Background()
    c := cache.GetDefaultCache()

    // Store a value
    type User struct {
        ID   int
        Name string
    }
    user := User{ID: 1, Name: "John"}
    c.Set(ctx, "user:1", user, 10*time.Minute)

    // Retrieve a value
    var retrieved User
    c.Get(ctx, "user:1", &retrieved)
}
```

### Redis Cache

```go
cache.UseRedis(&cache.RedisConfig{
    Host:     "localhost",
    Port:     6379,
    Password: "",
    DB:       0,
    Options: &cache.Options{
        DefaultTTL: 5 * time.Minute,
    },
})
defer cache.Close()
```

### Memcache

```go
cache.UseMemcache(&cache.MemcacheConfig{
    Servers: []string{"localhost:11211"},
    Options: &cache.Options{
        DefaultTTL: 5 * time.Minute,
    },
})
defer cache.Close()
```

## API Reference

### Core Methods

#### Set
```go
Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
```
Stores a value in the cache with automatic JSON serialization.

#### Get
```go
Get(ctx context.Context, key string, dest interface{}) error
```
Retrieves and deserializes a value from the cache.

#### SetBytes / GetBytes
```go
SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
GetBytes(ctx context.Context, key string) ([]byte, error)
```
Store and retrieve raw bytes without serialization.

#### Delete
```go
Delete(ctx context.Context, key string) error
```
Removes a key from the cache.

#### DeleteByPattern
```go
DeleteByPattern(ctx context.Context, pattern string) error
```
Removes all keys matching a pattern (Redis only).

#### Clear
```go
Clear(ctx context.Context) error
```
Removes all items from the cache.

#### Exists
```go
Exists(ctx context.Context, key string) bool
```
Checks if a key exists in the cache.

#### GetOrSet
```go
GetOrSet(ctx context.Context, key string, dest interface{}, ttl time.Duration,
         loader func() (interface{}, error)) error
```
Retrieves a value from cache, or loads and caches it if not found (lazy loading).

#### Stats
```go
Stats(ctx context.Context) (*CacheStats, error)
```
Returns cache statistics including hits, misses, and key counts.

## Provider Configuration

### In-Memory Options

```go
&cache.Options{
    DefaultTTL:     5 * time.Minute,  // Default expiration time
    MaxSize:        10000,             // Maximum number of items
    EvictionPolicy: "LRU",             // Eviction strategy (future)
}
```

### Redis Configuration

```go
&cache.RedisConfig{
    Host:     "localhost",
    Port:     6379,
    Password: "",         // Optional authentication
    DB:       0,          // Database number
    PoolSize: 10,         // Connection pool size
    Options:  &cache.Options{
        DefaultTTL: 5 * time.Minute,
    },
}
```

### Memcache Configuration

```go
&cache.MemcacheConfig{
    Servers:      []string{"localhost:11211"},
    MaxIdleConns: 2,
    Timeout:      1 * time.Second,
    Options: &cache.Options{
        DefaultTTL: 5 * time.Minute,
    },
}
```

## Advanced Usage

### Custom Provider

```go
// Create a custom provider instance
memProvider := cache.NewMemoryProvider(&cache.Options{
    DefaultTTL: 10 * time.Minute,
    MaxSize:    500,
})

// Initialize with custom provider
cache.Initialize(memProvider)
```

### Lazy Loading Pattern

```go
var data ExpensiveData
err := c.GetOrSet(ctx, "expensive:key", &data, 10*time.Minute, func() (interface{}, error) {
    // This expensive operation only runs if key is not in cache
    return computeExpensiveData(), nil
})
```

### Query API Cache

The package includes specialized functions for caching query results:

```go
// Cache a query result
api := "GetUsers"
query := "SELECT * FROM users WHERE active = true"
tablenames := "users"
total := int64(150)

cache.PutQueryAPICache(ctx, api, query, tablenames, total)

// Retrieve cached query
hash := cache.HashQueryAPICache(api, query)
cachedQuery, err := cache.FetchQueryAPICache(ctx, hash)
```

## Provider Comparison

| Feature | In-Memory | Redis | Memcache |
|---------|-----------|-------|----------|
| Persistence | No | Yes | No |
| Distributed | No | Yes | Yes |
| Pattern Delete | No | Yes | No |
| Statistics | Full | Full | Limited |
| Atomic Operations | Yes | Yes | Yes |
| Max Item Size | Memory | 512MB | 1MB |

## Best Practices

1. **Use contexts**: Always pass context for cancellation and timeout control
2. **Set appropriate TTLs**: Balance between freshness and performance
3. **Handle errors**: Cache misses and errors should be handled gracefully
4. **Monitor statistics**: Use Stats() to monitor cache performance
5. **Clean up**: Always call Close() when shutting down
6. **Pattern consistency**: Use consistent key naming patterns (e.g., "user:id:field")

## Example: Complete Application

```go
package main

import (
    "context"
    "log"
    "time"
    "github.com/bitechdev/ResolveSpec/pkg/cache"
)

type UserService struct {
    cache *cache.Cache
}

func NewUserService() *UserService {
    // Initialize with Redis in production, memory for testing
    cache.UseRedis(&cache.RedisConfig{
        Host: "localhost",
        Port: 6379,
        Options: &cache.Options{
            DefaultTTL: 10 * time.Minute,
        },
    })

    return &UserService{
        cache: cache.GetDefaultCache(),
    }
}

func (s *UserService) GetUser(ctx context.Context, userID int) (*User, error) {
    var user User
    cacheKey := fmt.Sprintf("user:%d", userID)

    // Try to get from cache first
    err := s.cache.GetOrSet(ctx, cacheKey, &user, 15*time.Minute, func() (interface{}, error) {
        // Load from database if not in cache
        return s.loadUserFromDB(userID)
    })

    if err != nil {
        return nil, err
    }

    return &user, nil
}

func (s *UserService) InvalidateUser(ctx context.Context, userID int) error {
    cacheKey := fmt.Sprintf("user:%d", userID)
    return s.cache.Delete(ctx, cacheKey)
}

func main() {
    service := NewUserService()
    defer cache.Close()

    ctx := context.Background()
    user, err := service.GetUser(ctx, 123)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("User: %+v", user)
}
```

## Performance Considerations

- **In-Memory**: Fastest but limited by RAM and not distributed
- **Redis**: Great for distributed systems, persistent, but network overhead
- **Memcache**: Good for distributed caching, simpler than Redis but less features

Choose based on your needs:
- Single instance? Use in-memory
- Need persistence or advanced features? Use Redis
- Simple distributed cache? Use Memcache

## License

See repository license.
