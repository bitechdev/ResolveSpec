package resolvespec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// queryCacheKey represents the components used to build a cache key for query total count
type queryCacheKey struct {
	TableName      string                `json:"table_name"`
	Filters        []common.FilterOption `json:"filters"`
	Sort           []common.SortOption   `json:"sort"`
	CustomSQLWhere string                `json:"custom_sql_where,omitempty"`
	CustomSQLOr    string                `json:"custom_sql_or,omitempty"`
	CursorForward  string                `json:"cursor_forward,omitempty"`
	CursorBackward string                `json:"cursor_backward,omitempty"`
}

// cachedTotal represents a cached total count
type cachedTotal struct {
	Total int `json:"total"`
}

// buildQueryCacheKey builds a cache key from query parameters for total count caching
func buildQueryCacheKey(tableName string, filters []common.FilterOption, sort []common.SortOption, customWhere, customOr string) string {
	key := queryCacheKey{
		TableName:      tableName,
		Filters:        filters,
		Sort:           sort,
		CustomSQLWhere: customWhere,
		CustomSQLOr:    customOr,
	}

	// Serialize to JSON for consistent hashing
	jsonData, err := json.Marshal(key)
	if err != nil {
		// Fallback to simple string concatenation if JSON fails
		return hashString(fmt.Sprintf("%s_%v_%v_%s_%s", tableName, filters, sort, customWhere, customOr))
	}

	return hashString(string(jsonData))
}

// buildExtendedQueryCacheKey builds a cache key for extended query options with cursor pagination
func buildExtendedQueryCacheKey(tableName string, filters []common.FilterOption, sort []common.SortOption,
	customWhere, customOr string, cursorFwd, cursorBwd string) string {

	key := queryCacheKey{
		TableName:      tableName,
		Filters:        filters,
		Sort:           sort,
		CustomSQLWhere: customWhere,
		CustomSQLOr:    customOr,
		CursorForward:  cursorFwd,
		CursorBackward: cursorBwd,
	}

	// Serialize to JSON for consistent hashing
	jsonData, err := json.Marshal(key)
	if err != nil {
		// Fallback to simple string concatenation if JSON fails
		return hashString(fmt.Sprintf("%s_%v_%v_%s_%s_%s_%s",
			tableName, filters, sort, customWhere, customOr, cursorFwd, cursorBwd))
	}

	return hashString(string(jsonData))
}

// hashString computes SHA256 hash of a string
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// getQueryTotalCacheKey returns a formatted cache key for storing/retrieving total count
func getQueryTotalCacheKey(hash string) string {
	return fmt.Sprintf("query_total:%s", hash)
}

// buildCacheTags creates cache tags from schema and table name
func buildCacheTags(schema, tableName string) []string {
	return []string{
		fmt.Sprintf("schema:%s", strings.ToLower(schema)),
		fmt.Sprintf("table:%s", strings.ToLower(tableName)),
	}
}

// setQueryTotalCache stores a query total in the cache with schema and table tags
func setQueryTotalCache(ctx context.Context, cacheKey string, total int, schema, tableName string, ttl time.Duration) error {
	c := cache.GetDefaultCache()
	cacheData := cachedTotal{Total: total}
	tags := buildCacheTags(schema, tableName)

	return c.SetWithTags(ctx, cacheKey, cacheData, ttl, tags)
}

// invalidateCacheForTags removes all cached items matching the specified tags
func invalidateCacheForTags(ctx context.Context, tags []string) error {
	c := cache.GetDefaultCache()

	// Invalidate for each tag
	for _, tag := range tags {
		if err := c.DeleteByTag(ctx, tag); err != nil {
			return err
		}
	}

	return nil
}
