package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisProvider is a Redis implementation of the Provider interface.
type RedisProvider struct {
	client  *redis.Client
	options *Options
}

// RedisConfig contains Redis-specific configuration.
type RedisConfig struct {
	// Host is the Redis server host (default: localhost)
	Host string

	// Port is the Redis server port (default: 6379)
	Port int

	// Password for Redis authentication (optional)
	Password string

	// DB is the Redis database number (default: 0)
	DB int

	// PoolSize is the maximum number of connections (default: 10)
	PoolSize int

	// Options contains general cache options
	Options *Options
}

// NewRedisProvider creates a new Redis cache provider.
func NewRedisProvider(config *RedisConfig) (*RedisProvider, error) {
	if config == nil {
		config = &RedisConfig{
			Host: "localhost",
			Port: 6379,
			DB:   0,
		}
	}

	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 6379
	}
	if config.PoolSize == 0 {
		config.PoolSize = 10
	}

	if config.Options == nil {
		config.Options = &Options{
			DefaultTTL: 5 * time.Minute,
		}
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.DB,
		PoolSize: config.PoolSize,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisProvider{
		client:  client,
		options: config.Options,
	}, nil
}

// Get retrieves a value from the cache by key.
func (r *RedisProvider) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return val, true
}

// Set stores a value in the cache with the specified TTL.
func (r *RedisProvider) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl == 0 {
		ttl = r.options.DefaultTTL
	}

	return r.client.Set(ctx, key, value, ttl).Err()
}

// SetWithTags stores a value in the cache with the specified TTL and tags.
func (r *RedisProvider) SetWithTags(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
	if ttl == 0 {
		ttl = r.options.DefaultTTL
	}

	pipe := r.client.Pipeline()

	// Set the value
	pipe.Set(ctx, key, value, ttl)

	// Add key to each tag's set
	for _, tag := range tags {
		tagKey := fmt.Sprintf("cache:tag:%s", tag)
		pipe.SAdd(ctx, tagKey, key)
		// Set expiration on tag set (longer than cache items to ensure cleanup)
		if ttl > 0 {
			pipe.Expire(ctx, tagKey, ttl+time.Hour)
		}
	}

	// Store tags for this key for later cleanup
	if len(tags) > 0 {
		tagsKey := fmt.Sprintf("cache:tags:%s", key)
		pipe.SAdd(ctx, tagsKey, tags)
		if ttl > 0 {
			pipe.Expire(ctx, tagsKey, ttl)
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}

// Delete removes a key from the cache.
func (r *RedisProvider) Delete(ctx context.Context, key string) error {
	pipe := r.client.Pipeline()

	// Get tags for this key
	tagsKey := fmt.Sprintf("cache:tags:%s", key)
	tags, err := r.client.SMembers(ctx, tagsKey).Result()
	if err == nil && len(tags) > 0 {
		// Remove key from each tag set
		for _, tag := range tags {
			tagKey := fmt.Sprintf("cache:tag:%s", tag)
			pipe.SRem(ctx, tagKey, key)
		}
		// Delete the tags key
		pipe.Del(ctx, tagsKey)
	}

	// Delete the actual key
	pipe.Del(ctx, key)

	_, err = pipe.Exec(ctx)
	return err
}

// DeleteByTag removes all keys associated with the given tag.
func (r *RedisProvider) DeleteByTag(ctx context.Context, tag string) error {
	tagKey := fmt.Sprintf("cache:tag:%s", tag)

	// Get all keys associated with this tag
	keys, err := r.client.SMembers(ctx, tagKey).Result()
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()

	// Delete all keys and their tag associations
	for _, key := range keys {
		pipe.Del(ctx, key)
		// Also delete the tags key for this cache key
		tagsKey := fmt.Sprintf("cache:tags:%s", key)
		pipe.Del(ctx, tagsKey)
	}

	// Delete the tag set itself
	pipe.Del(ctx, tagKey)

	_, err = pipe.Exec(ctx)
	return err
}

// DeleteByPattern removes all keys matching the pattern.
func (r *RedisProvider) DeleteByPattern(ctx context.Context, pattern string) error {
	iter := r.client.Scan(ctx, 0, pattern, 0).Iterator()
	pipe := r.client.Pipeline()

	count := 0
	for iter.Next(ctx) {
		pipe.Del(ctx, iter.Val())
		count++

		// Execute pipeline in batches of 100
		if count%100 == 0 {
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
			pipe = r.client.Pipeline()
		}
	}

	if err := iter.Err(); err != nil {
		return err
	}

	// Execute remaining commands
	if count%100 != 0 {
		_, err := pipe.Exec(ctx)
		return err
	}

	return nil
}

// Clear removes all items from the cache.
func (r *RedisProvider) Clear(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}

// Exists checks if a key exists in the cache.
func (r *RedisProvider) Exists(ctx context.Context, key string) bool {
	result, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false
	}
	return result > 0
}

// Close closes the provider and releases any resources.
func (r *RedisProvider) Close() error {
	return r.client.Close()
}

// Stats returns statistics about the cache provider.
func (r *RedisProvider) Stats(ctx context.Context) (*CacheStats, error) {
	info, err := r.client.Info(ctx, "stats", "keyspace").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis stats: %w", err)
	}

	dbSize, err := r.client.DBSize(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get DB size: %w", err)
	}

	// Parse stats from INFO command
	// This is a simplified version - you may want to parse more detailed stats
	stats := &CacheStats{
		Keys:         dbSize,
		ProviderType: "redis",
		ProviderStats: map[string]any{
			"info": info,
		},
	}

	return stats, nil
}
