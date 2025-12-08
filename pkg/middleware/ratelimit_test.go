package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	// Create rate limiter: 2 requests per second, burst of 2
	rl := NewRateLimiter(2, 2)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// First request should succeed
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("First request failed: got %d, want %d", w.Code, http.StatusOK)
	}

	// Second request should succeed (within burst)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Second request failed: got %d, want %d", w.Code, http.StatusOK)
	}

	// Third request should be rate limited
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Third request should be rate limited: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	// Wait for rate limiter to refill
	time.Sleep(600 * time.Millisecond)

	// Request should succeed again
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request after wait failed: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, 1)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"

	// Second IP
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:12345"

	// Both should succeed (different IPs)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w1.Code != http.StatusOK {
		t.Errorf("First IP request failed: got %d, want %d", w1.Code, http.StatusOK)
	}

	if w2.Code != http.StatusOK {
		t.Errorf("Second IP request failed: got %d, want %d", w2.Code, http.StatusOK)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		expectedIP     string
	}{
		{
			name:       "RemoteAddr only",
			remoteAddr: "192.168.1.1:12345",
			expectedIP: "192.168.1.1",
		},
		{
			name:          "X-Forwarded-For single IP",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.1",
			expectedIP:    "203.0.113.1",
		},
		{
			name:          "X-Forwarded-For multiple IPs",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.1, 10.0.0.2, 10.0.0.3",
			expectedIP:    "203.0.113.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.1",
			expectedIP: "203.0.113.1",
		},
		{
			name:          "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.1",
			xRealIP:       "203.0.113.2",
			expectedIP:    "203.0.113.1",
		},
		{
			name:       "IPv6 address",
			remoteAddr: "[2001:db8::1]:12345",
			expectedIP: "[2001:db8::1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			ip := getClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("getClientIP() = %q, want %q", ip, tt.expectedIP)
			}
		})
	}
}

func TestRateLimiterWithCustomKeyFunc(t *testing.T) {
	rl := NewRateLimiter(1, 1)

	// Use user ID as key
	keyFunc := func(r *http.Request) string {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			return r.RemoteAddr
		}
		return "user:" + userID
	}

	handler := rl.MiddlewareWithKeyFunc(keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// User 1
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-User-ID", "user1")

	// User 2
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-User-ID", "user2")

	// Both users should succeed (different keys)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w1.Code != http.StatusOK {
		t.Errorf("User 1 request failed: got %d, want %d", w1.Code, http.StatusOK)
	}

	if w2.Code != http.StatusOK {
		t.Errorf("User 2 request failed: got %d, want %d", w2.Code, http.StatusOK)
	}

	// User 1 second request should be rate limited
	w1 = httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("User 1 second request should be rate limited: got %d, want %d", w1.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_GetTrackedIPs(t *testing.T) {
	rl := NewRateLimiter(10, 10)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests from different IPs
	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for _, ip := range ips {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Check tracked IPs
	trackedIPs := rl.GetTrackedIPs()
	if len(trackedIPs) != len(ips) {
		t.Errorf("len(trackedIPs) = %d, want %d", len(trackedIPs), len(ips))
	}

	// Verify all IPs are tracked
	ipMap := make(map[string]bool)
	for _, ip := range trackedIPs {
		ipMap[ip] = true
	}

	for _, ip := range ips {
		if !ipMap[ip] {
			t.Errorf("IP %s should be tracked", ip)
		}
	}
}

func TestRateLimiter_GetRateLimitInfo(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make a request
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Get rate limit info
	info := rl.GetRateLimitInfo("192.168.1.1")

	if info.IP != "192.168.1.1" {
		t.Errorf("IP = %q, want %q", info.IP, "192.168.1.1")
	}

	if info.Limit != 10.0 {
		t.Errorf("Limit = %f, want 10.0", info.Limit)
	}

	if info.Burst != 5 {
		t.Errorf("Burst = %d, want 5", info.Burst)
	}

	// Tokens should be less than burst after one request
	if info.TokensRemaining >= float64(info.Burst) {
		t.Errorf("TokensRemaining = %f, should be less than %d", info.TokensRemaining, info.Burst)
	}
}

func TestRateLimiter_GetRateLimitInfo_UntrackedIP(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	// Get info for untracked IP (should return default)
	info := rl.GetRateLimitInfo("192.168.1.1")

	if info.IP != "192.168.1.1" {
		t.Errorf("IP = %q, want %q", info.IP, "192.168.1.1")
	}

	if info.TokensRemaining != float64(rl.burst) {
		t.Errorf("TokensRemaining = %f, want %d (full burst)", info.TokensRemaining, rl.burst)
	}
}

func TestRateLimiter_GetAllRateLimitInfo(t *testing.T) {
	rl := NewRateLimiter(10, 10)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests from different IPs
	ips := []string{"192.168.1.1", "192.168.1.2"}
	for _, ip := range ips {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Get all rate limit info
	allInfo := rl.GetAllRateLimitInfo()

	if len(allInfo) != len(ips) {
		t.Errorf("len(allInfo) = %d, want %d", len(allInfo), len(ips))
	}

	// Verify each IP has info
	for _, info := range allInfo {
		found := false
		for _, ip := range ips {
			if info.IP == ip {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected IP in info: %s", info.IP)
		}
	}
}

func TestRateLimiter_StatsHandler(t *testing.T) {
	rl := NewRateLimiter(10, 5)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests from different IPs
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.2:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// Test stats handler (all IPs)
	t.Run("AllIPs", func(t *testing.T) {
		statsHandler := rl.StatsHandler()
		req := httptest.NewRequest("GET", "/rate-limit-stats", nil)
		w := httptest.NewRecorder()
		statsHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		var stats map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if int(stats["total_tracked_ips"].(float64)) != 2 {
			t.Errorf("total_tracked_ips = %v, want 2", stats["total_tracked_ips"])
		}

		config := stats["rate_limit_config"].(map[string]interface{})
		if config["requests_per_second"].(float64) != 10.0 {
			t.Errorf("requests_per_second = %v, want 10.0", config["requests_per_second"])
		}
	})

	// Test stats handler (specific IP)
	t.Run("SpecificIP", func(t *testing.T) {
		statsHandler := rl.StatsHandler()
		req := httptest.NewRequest("GET", "/rate-limit-stats?ip=192.168.1.1", nil)
		w := httptest.NewRecorder()
		statsHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}

		var info RateLimitInfo
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if info.IP != "192.168.1.1" {
			t.Errorf("IP = %q, want %q", info.IP, "192.168.1.1")
		}
	})
}
