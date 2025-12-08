package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPBlacklist_BlockIP(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	// Block an IP
	err := bl.BlockIP("192.168.1.100", "Suspicious activity")
	if err != nil {
		t.Fatalf("BlockIP() error = %v", err)
	}

	// Check if IP is blocked
	blocked, reason := bl.IsBlocked("192.168.1.100")
	if !blocked {
		t.Error("IP should be blocked")
	}
	if reason != "Suspicious activity" {
		t.Errorf("Reason = %q, want %q", reason, "Suspicious activity")
	}

	// Check non-blocked IP
	blocked, _ = bl.IsBlocked("192.168.1.1")
	if blocked {
		t.Error("IP should not be blocked")
	}
}

func TestIPBlacklist_BlockCIDR(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	// Block a CIDR range
	err := bl.BlockCIDR("10.0.0.0/24", "Internal network blocked")
	if err != nil {
		t.Fatalf("BlockCIDR() error = %v", err)
	}

	// Check IPs in range
	testIPs := []string{
		"10.0.0.1",
		"10.0.0.100",
		"10.0.0.254",
	}

	for _, ip := range testIPs {
		blocked, _ := bl.IsBlocked(ip)
		if !blocked {
			t.Errorf("IP %s should be blocked by CIDR", ip)
		}
	}

	// Check IP outside range
	blocked, _ := bl.IsBlocked("10.0.1.1")
	if blocked {
		t.Error("IP outside CIDR range should not be blocked")
	}
}

func TestIPBlacklist_UnblockIP(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	// Block and then unblock
	bl.BlockIP("192.168.1.100", "Test")

	blocked, _ := bl.IsBlocked("192.168.1.100")
	if !blocked {
		t.Error("IP should be blocked")
	}

	bl.UnblockIP("192.168.1.100")

	blocked, _ = bl.IsBlocked("192.168.1.100")
	if blocked {
		t.Error("IP should be unblocked")
	}
}

func TestIPBlacklist_UnblockCIDR(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	// Block and then unblock CIDR
	bl.BlockCIDR("10.0.0.0/24", "Test")

	blocked, _ := bl.IsBlocked("10.0.0.1")
	if !blocked {
		t.Error("IP should be blocked by CIDR")
	}

	bl.UnblockCIDR("10.0.0.0/24")

	blocked, _ = bl.IsBlocked("10.0.0.1")
	if blocked {
		t.Error("IP should be unblocked after CIDR removal")
	}
}

func TestIPBlacklist_Middleware(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})
	bl.BlockIP("192.168.1.100", "Banned")

	handler := bl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// Blocked IP should get 403
	t.Run("BlockedIP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
		}

		var response map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if response["error"] != "forbidden" {
			t.Errorf("Error = %v, want %q", response["error"], "forbidden")
		}
	})

	// Allowed IP should succeed
	t.Run("AllowedIP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestIPBlacklist_MiddlewareWithProxy(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: true})
	bl.BlockIP("203.0.113.1", "Blocked via proxy")

	handler := bl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test X-Forwarded-For
	t.Run("X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.1")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})

	// Test X-Real-IP
	t.Run("X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		req.Header.Set("X-Real-IP", "203.0.113.1")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
		}
	})
}

func TestIPBlacklist_StatsHandler(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})
	bl.BlockIP("192.168.1.100", "Test1")
	bl.BlockIP("192.168.1.101", "Test2")
	bl.BlockCIDR("10.0.0.0/24", "Test CIDR")

	handler := bl.StatsHandler()

	req := httptest.NewRequest("GET", "/blacklist-stats", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if int(stats["total_ips"].(float64)) != 2 {
		t.Errorf("total_ips = %v, want 2", stats["total_ips"])
	}

	if int(stats["total_cidrs"].(float64)) != 1 {
		t.Errorf("total_cidrs = %v, want 1", stats["total_cidrs"])
	}
}

func TestIPBlacklist_GetBlacklist(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})
	bl.BlockIP("192.168.1.100", "")
	bl.BlockIP("192.168.1.101", "")
	bl.BlockCIDR("10.0.0.0/24", "")

	ips, cidrs := bl.GetBlacklist()

	if len(ips) != 2 {
		t.Errorf("len(ips) = %d, want 2", len(ips))
	}

	if len(cidrs) != 1 {
		t.Errorf("len(cidrs) = %d, want 1", len(cidrs))
	}

	// Verify CIDR format
	if cidrs[0] != "10.0.0.0/24" {
		t.Errorf("CIDR = %q, want %q", cidrs[0], "10.0.0.0/24")
	}
}

func TestIPBlacklist_InvalidIP(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	err := bl.BlockIP("invalid-ip", "Test")
	if err == nil {
		t.Error("BlockIP() should return error for invalid IP")
	}
}

func TestIPBlacklist_InvalidCIDR(t *testing.T) {
	bl := NewIPBlacklist(BlacklistConfig{UseProxy: false})

	err := bl.BlockCIDR("invalid-cidr", "Test")
	if err == nil {
		t.Error("BlockCIDR() should return error for invalid CIDR")
	}
}
