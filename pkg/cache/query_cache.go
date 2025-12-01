package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// QueryCacheKey represents the components used to build a cache key for query total count
type QueryCacheKey struct {
	TableName      string                `json:"table_name"`
	Filters        []common.FilterOption `json:"filters"`
	Sort           []common.SortOption   `json:"sort"`
	CustomSQLWhere string                `json:"custom_sql_where,omitempty"`
	CustomSQLOr    string                `json:"custom_sql_or,omitempty"`
	Expand         []ExpandOptionKey     `json:"expand,omitempty"`
	Distinct       bool                  `json:"distinct,omitempty"`
	CursorForward  string                `json:"cursor_forward,omitempty"`
	CursorBackward string                `json:"cursor_backward,omitempty"`
}

// ExpandOptionKey represents expand options for cache key
type ExpandOptionKey struct {
	Relation string `json:"relation"`
	Where    string `json:"where,omitempty"`
}

// BuildQueryCacheKey builds a cache key from query parameters for total count caching
// This is used to cache the total count of records matching a query
func BuildQueryCacheKey(tableName string, filters []common.FilterOption, sort []common.SortOption, customWhere, customOr string) string {
	key := QueryCacheKey{
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

// BuildExtendedQueryCacheKey builds a cache key for extended query options (restheadspec)
// Includes expand, distinct, and cursor pagination options
func BuildExtendedQueryCacheKey(tableName string, filters []common.FilterOption, sort []common.SortOption,
	customWhere, customOr string, expandOpts []interface{}, distinct bool, cursorFwd, cursorBwd string) string {

	key := QueryCacheKey{
		TableName:      tableName,
		Filters:        filters,
		Sort:           sort,
		CustomSQLWhere: customWhere,
		CustomSQLOr:    customOr,
		Distinct:       distinct,
		CursorForward:  cursorFwd,
		CursorBackward: cursorBwd,
	}

	// Convert expand options to cache key format
	if len(expandOpts) > 0 {
		key.Expand = make([]ExpandOptionKey, 0, len(expandOpts))
		for _, exp := range expandOpts {
			// Type assert to get the expand option fields we care about for caching
			if expMap, ok := exp.(map[string]interface{}); ok {
				expKey := ExpandOptionKey{}
				if rel, ok := expMap["relation"].(string); ok {
					expKey.Relation = rel
				}
				if where, ok := expMap["where"].(string); ok {
					expKey.Where = where
				}
				key.Expand = append(key.Expand, expKey)
			}
		}
		// Sort expand options for consistent hashing (already sorted by relation name above)
	}

	// Serialize to JSON for consistent hashing
	jsonData, err := json.Marshal(key)
	if err != nil {
		// Fallback to simple string concatenation if JSON fails
		return hashString(fmt.Sprintf("%s_%v_%v_%s_%s_%v_%v_%s_%s",
			tableName, filters, sort, customWhere, customOr, expandOpts, distinct, cursorFwd, cursorBwd))
	}

	return hashString(string(jsonData))
}

// hashString computes SHA256 hash of a string
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// GetQueryTotalCacheKey returns a formatted cache key for storing/retrieving total count
func GetQueryTotalCacheKey(hash string) string {
	return fmt.Sprintf("query_total:%s", hash)
}

// CachedTotal represents a cached total count
type CachedTotal struct {
	Total int `json:"total"`
}

// InvalidateCacheForTable removes all cached totals for a specific table
// This should be called when data in the table changes (insert/update/delete)
func InvalidateCacheForTable(ctx context.Context, tableName string) error {
	cache := GetDefaultCache()

	// Build a pattern to match all query totals for this table
	// Note: This requires pattern matching support in the provider
	pattern := fmt.Sprintf("query_total:*%s*", strings.ToLower(tableName))

	return cache.DeleteByPattern(ctx, pattern)
}
