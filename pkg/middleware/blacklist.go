package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// IPBlacklist provides IP blocking functionality
type IPBlacklist struct {
	mu       sync.RWMutex
	ips      map[string]bool // Individual IPs
	cidrs    []*net.IPNet    // CIDR ranges
	reason   map[string]string
	useProxy bool // Whether to check X-Forwarded-For headers
}

// BlacklistConfig configures the IP blacklist
type BlacklistConfig struct {
	// UseProxy indicates whether to extract IP from X-Forwarded-For/X-Real-IP headers
	UseProxy bool
}

// NewIPBlacklist creates a new IP blacklist
func NewIPBlacklist(config BlacklistConfig) *IPBlacklist {
	return &IPBlacklist{
		ips:      make(map[string]bool),
		cidrs:    make([]*net.IPNet, 0),
		reason:   make(map[string]string),
		useProxy: config.UseProxy,
	}
}

// BlockIP blocks a single IP address
func (bl *IPBlacklist) BlockIP(ip string, reason string) error {
	// Validate IP
	if net.ParseIP(ip) == nil {
		return &net.ParseError{Type: "IP address", Text: ip}
	}

	bl.mu.Lock()
	defer bl.mu.Unlock()

	bl.ips[ip] = true
	if reason != "" {
		bl.reason[ip] = reason
	}
	return nil
}

// BlockCIDR blocks an IP range using CIDR notation
func (bl *IPBlacklist) BlockCIDR(cidr string, reason string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	bl.mu.Lock()
	defer bl.mu.Unlock()

	bl.cidrs = append(bl.cidrs, ipNet)
	if reason != "" {
		bl.reason[cidr] = reason
	}
	return nil
}

// UnblockIP removes an IP from the blacklist
func (bl *IPBlacklist) UnblockIP(ip string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	delete(bl.ips, ip)
	delete(bl.reason, ip)
}

// UnblockCIDR removes a CIDR range from the blacklist
func (bl *IPBlacklist) UnblockCIDR(cidr string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	// Find and remove the CIDR
	for i, ipNet := range bl.cidrs {
		if ipNet.String() == cidr {
			bl.cidrs = append(bl.cidrs[:i], bl.cidrs[i+1:]...)
			break
		}
	}
	delete(bl.reason, cidr)
}

// IsBlocked checks if an IP is blacklisted
func (bl *IPBlacklist) IsBlocked(ip string) (blacklist bool, reason string) {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	// Check individual IPs
	if bl.ips[ip] {
		return true, bl.reason[ip]
	}

	// Check CIDR ranges
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false, ""
	}

	for i, ipNet := range bl.cidrs {
		if ipNet.Contains(parsedIP) {
			cidr := ipNet.String()
			// Try to find reason by CIDR or by index
			if reason, ok := bl.reason[cidr]; ok {
				return true, reason
			}
			// Check if reason was stored by original CIDR string
			for key, reason := range bl.reason {
				if strings.Contains(key, "/") && key == cidr {
					return true, reason
				}
			}
			// Return true even if no reason found
			if i < len(bl.cidrs) {
				return true, ""
			}
		}
	}

	return false, ""
}

// GetBlacklist returns all blacklisted IPs and CIDRs
func (bl *IPBlacklist) GetBlacklist() (ips []string, cidrs []string) {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	ips = make([]string, 0, len(bl.ips))
	for ip := range bl.ips {
		ips = append(ips, ip)
	}

	cidrs = make([]string, 0, len(bl.cidrs))
	for _, ipNet := range bl.cidrs {
		cidrs = append(cidrs, ipNet.String())
	}

	return ips, cidrs
}

// Middleware returns an HTTP middleware that blocks blacklisted IPs
func (bl *IPBlacklist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var clientIP string
		if bl.useProxy {
			clientIP = getClientIP(r)
			// Clean up IPv6 brackets if present
			clientIP = strings.Trim(clientIP, "[]")
		} else {
			// Extract IP from RemoteAddr
			if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
				clientIP = r.RemoteAddr[:idx]
			} else {
				clientIP = r.RemoteAddr
			}
			clientIP = strings.Trim(clientIP, "[]")
		}

		blocked, reason := bl.IsBlocked(clientIP)
		if blocked {
			response := map[string]interface{}{
				"error":   "forbidden",
				"message": "Access denied",
			}
			if reason != "" {
				response["reason"] = reason
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			err := json.NewEncoder(w).Encode(response)
			if err != nil {
				logger.Debug("Failed to write blacklist response: %v", err)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// StatsHandler returns an HTTP handler that shows blacklist statistics
func (bl *IPBlacklist) StatsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ips, cidrs := bl.GetBlacklist()

		stats := map[string]interface{}{
			"blocked_ips":   ips,
			"blocked_cidrs": cidrs,
			"total_ips":     len(ips),
			"total_cidrs":   len(cidrs),
		}

		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(stats)
		if err != nil {
			logger.Debug("Failed to encode stats: %v", err)
		}
	})
}
