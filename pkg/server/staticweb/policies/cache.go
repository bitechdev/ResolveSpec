package policies

import (
	"fmt"
	"path"
	"strings"
)

// SimpleCachePolicy implements a basic cache policy with a single TTL for all files.
type SimpleCachePolicy struct {
	cacheTime int // Cache duration in seconds
}

// NewSimpleCachePolicy creates a new SimpleCachePolicy with the given cache time in seconds.
func NewSimpleCachePolicy(cacheTimeSeconds int) *SimpleCachePolicy {
	return &SimpleCachePolicy{
		cacheTime: cacheTimeSeconds,
	}
}

// GetCacheTime returns the cache duration for any file.
func (p *SimpleCachePolicy) GetCacheTime(filePath string) int {
	return p.cacheTime
}

// GetCacheHeaders returns the Cache-Control header for the given file.
func (p *SimpleCachePolicy) GetCacheHeaders(filePath string) map[string]string {
	if p.cacheTime <= 0 {
		return map[string]string{
			"Cache-Control": "no-cache, no-store, must-revalidate",
			"Pragma":        "no-cache",
			"Expires":       "0",
		}
	}

	return map[string]string{
		"Cache-Control": fmt.Sprintf("public, max-age=%d", p.cacheTime),
	}
}

// ExtensionBasedCachePolicy implements a cache policy that varies by file extension.
type ExtensionBasedCachePolicy struct {
	rules       map[string]int // Extension -> cache time in seconds
	defaultTime int            // Default cache time for unmatched extensions
}

// NewExtensionBasedCachePolicy creates a new ExtensionBasedCachePolicy.
// rules maps file extensions (with leading dot, e.g., ".js") to cache times in seconds.
// defaultTime is used for files that don't match any rule.
func NewExtensionBasedCachePolicy(rules map[string]int, defaultTime int) *ExtensionBasedCachePolicy {
	return &ExtensionBasedCachePolicy{
		rules:       rules,
		defaultTime: defaultTime,
	}
}

// GetCacheTime returns the cache duration based on the file extension.
func (p *ExtensionBasedCachePolicy) GetCacheTime(filePath string) int {
	ext := strings.ToLower(path.Ext(filePath))
	if cacheTime, ok := p.rules[ext]; ok {
		return cacheTime
	}
	return p.defaultTime
}

// GetCacheHeaders returns cache headers based on the file extension.
func (p *ExtensionBasedCachePolicy) GetCacheHeaders(filePath string) map[string]string {
	cacheTime := p.GetCacheTime(filePath)

	if cacheTime <= 0 {
		return map[string]string{
			"Cache-Control": "no-cache, no-store, must-revalidate",
			"Pragma":        "no-cache",
			"Expires":       "0",
		}
	}

	return map[string]string{
		"Cache-Control": fmt.Sprintf("public, max-age=%d", cacheTime),
	}
}

// NoCachePolicy implements a cache policy that disables all caching.
type NoCachePolicy struct{}

// NewNoCachePolicy creates a new NoCachePolicy.
func NewNoCachePolicy() *NoCachePolicy {
	return &NoCachePolicy{}
}

// GetCacheTime always returns 0 (no caching).
func (p *NoCachePolicy) GetCacheTime(filePath string) int {
	return 0
}

// GetCacheHeaders returns headers that disable caching.
func (p *NoCachePolicy) GetCacheHeaders(filePath string) map[string]string {
	return map[string]string{
		"Cache-Control": "no-cache, no-store, must-revalidate",
		"Pragma":        "no-cache",
		"Expires":       "0",
	}
}
