package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
	cleanup  time.Duration
}

// NewRateLimiter creates a new rate limiter
// rps is requests per second, burst is the maximum burst size
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(rps),
		burst:    burst,
		cleanup:  5 * time.Minute, // Clean up stale limiters every 5 minutes
	}

	// Start cleanup goroutine
	go rl.cleanupRoutine()

	return rl
}

// getLimiter returns the rate limiter for a given key (e.g., IP address)
func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if exists {
		return limiter
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := rl.limiters[key]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(rl.rate, rl.burst)
	rl.limiters[key] = limiter
	return limiter
}

// cleanupRoutine periodically removes inactive limiters
func (rl *RateLimiter) cleanupRoutine() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		// Simple cleanup: remove all limiters
		// In production, you might want to track last access time
		rl.limiters = make(map[string]*rate.Limiter)
		rl.mu.Unlock()
	}
}

// Middleware returns an HTTP middleware that applies rate limiting
// Automatically handles X-Forwarded-For headers when behind a proxy
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP, handling proxy headers
		key := getClientIP(r)

		limiter := rl.getLimiter(key)

		if !limiter.Allow() {
			http.Error(w, `{"error":"rate_limit_exceeded","message":"Too many requests"}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MiddlewareWithKeyFunc returns an HTTP middleware with a custom key extraction function
func (rl *RateLimiter) MiddlewareWithKeyFunc(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				key = r.RemoteAddr
			}

			limiter := rl.getLimiter(key)

			if !limiter.Allow() {
				http.Error(w, `{"error":"rate_limit_exceeded","message":"Too many requests"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitInfo contains information about a specific IP's rate limit status
type RateLimitInfo struct {
	IP             string    `json:"ip"`
	TokensRemaining float64   `json:"tokens_remaining"`
	Limit          float64   `json:"limit"`
	Burst          int       `json:"burst"`
}

// GetTrackedIPs returns all IPs currently being tracked by the rate limiter
func (rl *RateLimiter) GetTrackedIPs() []string {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	ips := make([]string, 0, len(rl.limiters))
	for ip := range rl.limiters {
		ips = append(ips, ip)
	}
	return ips
}

// GetRateLimitInfo returns rate limit information for a specific IP
func (rl *RateLimiter) GetRateLimitInfo(ip string) *RateLimitInfo {
	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if !exists {
		// Return default info for untracked IP
		return &RateLimitInfo{
			IP:             ip,
			TokensRemaining: float64(rl.burst),
			Limit:          float64(rl.rate),
			Burst:          rl.burst,
		}
	}

	return &RateLimitInfo{
		IP:             ip,
		TokensRemaining: limiter.Tokens(),
		Limit:          float64(rl.rate),
		Burst:          rl.burst,
	}
}

// GetAllRateLimitInfo returns rate limit information for all tracked IPs
func (rl *RateLimiter) GetAllRateLimitInfo() []*RateLimitInfo {
	ips := rl.GetTrackedIPs()
	info := make([]*RateLimitInfo, 0, len(ips))

	for _, ip := range ips {
		info = append(info, rl.GetRateLimitInfo(ip))
	}

	return info
}

// StatsHandler returns an HTTP handler that exposes rate limit statistics
// Example: GET /rate-limit-stats
func (rl *RateLimiter) StatsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Support querying specific IP via ?ip=x.x.x.x
		if ip := r.URL.Query().Get("ip"); ip != "" {
			info := rl.GetRateLimitInfo(ip)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(info)
			return
		}

		// Return all tracked IPs
		allInfo := rl.GetAllRateLimitInfo()

		stats := map[string]interface{}{
			"total_tracked_ips": len(allInfo),
			"rate_limit_config": map[string]interface{}{
				"requests_per_second": float64(rl.rate),
				"burst":              rl.burst,
			},
			"tracked_ips": allInfo,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})
}

// getClientIP extracts the real client IP from the request
// Handles X-Forwarded-For, X-Real-IP, and falls back to RemoteAddr
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (most common in production)
	// Format: X-Forwarded-For: client, proxy1, proxy2
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (the original client)
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header (used by some proxies like nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	// Remove port if present (format: "ip:port")
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}

	return r.RemoteAddr
}
