package middleware

import (
	"fmt"
	"net/http"
)

const (
	// DefaultMaxRequestSize is the default maximum request body size (10MB)
	DefaultMaxRequestSize = 10 * 1024 * 1024 // 10MB

	// MaxRequestSizeHeader is the header name for max request size
	MaxRequestSizeHeader = "X-Max-Request-Size"
)

// RequestSizeLimiter limits the size of request bodies
type RequestSizeLimiter struct {
	maxSize int64
}

// NewRequestSizeLimiter creates a new request size limiter
// maxSize is in bytes. If 0, uses DefaultMaxRequestSize (10MB)
func NewRequestSizeLimiter(maxSize int64) *RequestSizeLimiter {
	if maxSize <= 0 {
		maxSize = DefaultMaxRequestSize
	}
	return &RequestSizeLimiter{
		maxSize: maxSize,
	}
}

// Middleware returns an HTTP middleware that enforces request size limits
func (rsl *RequestSizeLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set max bytes reader on the request body
		r.Body = http.MaxBytesReader(w, r.Body, rsl.maxSize)

		// Add informational header
		w.Header().Set(MaxRequestSizeHeader, fmt.Sprintf("%d", rsl.maxSize))

		next.ServeHTTP(w, r)
	})
}

// MiddlewareWithCustomSize returns middleware with a custom size limit function
// This allows different size limits based on the request
func (rsl *RequestSizeLimiter) MiddlewareWithCustomSize(sizeFunc func(*http.Request) int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			maxSize := sizeFunc(r)
			if maxSize <= 0 {
				maxSize = rsl.maxSize
			}

			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			w.Header().Set(MaxRequestSizeHeader, fmt.Sprintf("%d", maxSize))

			next.ServeHTTP(w, r)
		})
	}
}

// Common size limits
const (
	Size1MB   = 1 * 1024 * 1024
	Size5MB   = 5 * 1024 * 1024
	Size10MB  = 10 * 1024 * 1024
	Size50MB  = 50 * 1024 * 1024
	Size100MB = 100 * 1024 * 1024
)
