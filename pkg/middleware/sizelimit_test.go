package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestSizeLimiter(t *testing.T) {
	// 1KB limit
	limiter := NewRequestSizeLimiter(1024)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read body
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Small request (should succeed)
	t.Run("SmallRequest", func(t *testing.T) {
		body := bytes.NewReader(make([]byte, 512)) // 512 bytes
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Small request failed: got %d, want %d", w.Code, http.StatusOK)
		}

		// Check header
		if maxSize := w.Header().Get(MaxRequestSizeHeader); maxSize != "1024" {
			t.Errorf("MaxRequestSizeHeader = %q, want %q", maxSize, "1024")
		}
	})

	// Large request (should fail)
	t.Run("LargeRequest", func(t *testing.T) {
		body := bytes.NewReader(make([]byte, 2048)) // 2KB
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Large request should fail: got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
		}
	})
}

func TestRequestSizeLimiterDefault(t *testing.T) {
	// Default limiter (10MB)
	limiter := NewRequestSizeLimiter(0)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", bytes.NewReader(make([]byte, 1024)))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request failed: got %d, want %d", w.Code, http.StatusOK)
	}

	// Check default size
	if maxSize := w.Header().Get(MaxRequestSizeHeader); maxSize != "10485760" {
		t.Errorf("Default MaxRequestSizeHeader = %q, want %q", maxSize, "10485760")
	}
}

func TestRequestSizeLimiterWithCustomSize(t *testing.T) {
	limiter := NewRequestSizeLimiter(1024)

	// Premium users get 10MB, regular users get 1KB
	sizeFunc := func(r *http.Request) int64 {
		if r.Header.Get("X-User-Tier") == "premium" {
			return Size10MB
		}
		return 1024
	}

	handler := limiter.MiddlewareWithCustomSize(sizeFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Regular user with large request (should fail)
	t.Run("RegularUserLargeRequest", func(t *testing.T) {
		body := bytes.NewReader(make([]byte, 2048))
		req := httptest.NewRequest("POST", "/test", body)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("Regular user large request should fail: got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
		}
	})

	// Premium user with large request (should succeed)
	t.Run("PremiumUserLargeRequest", func(t *testing.T) {
		body := bytes.NewReader(make([]byte, 2048))
		req := httptest.NewRequest("POST", "/test", body)
		req.Header.Set("X-User-Tier", "premium")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Premium user large request failed: got %d, want %d", w.Code, http.StatusOK)
		}
	})
}
