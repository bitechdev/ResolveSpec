# Security Features: Blacklist & Rate Limit Inspection

## IP Blacklist

The IP blacklist middleware allows you to block specific IP addresses or CIDR ranges from accessing your application.

### Basic Usage

```go
import "github.com/bitechdev/ResolveSpec/pkg/middleware"

// Create blacklist (UseProxy=true if behind a proxy)
blacklist := middleware.NewIPBlacklist(middleware.BlacklistConfig{
    UseProxy: true, // Checks X-Forwarded-For and X-Real-IP headers
})

// Block individual IP
blacklist.BlockIP("192.168.1.100", "Suspicious activity detected")

// Block entire CIDR range
blacklist.BlockCIDR("10.0.0.0/8", "Private network blocked")

// Apply middleware
http.Handle("/api/", blacklist.Middleware(yourHandler))
```

### Managing Blacklist

```go
// Unblock an IP
blacklist.UnblockIP("192.168.1.100")

// Unblock a CIDR range
blacklist.UnblockCIDR("10.0.0.0/8")

// Get all blacklisted IPs and CIDRs
ips, cidrs := blacklist.GetBlacklist()
fmt.Printf("Blocked IPs: %v\n", ips)
fmt.Printf("Blocked CIDRs: %v\n", cidrs)

// Check if specific IP is blocked
blocked, reason := blacklist.IsBlocked("192.168.1.100")
if blocked {
    fmt.Printf("IP blocked: %s\n", reason)
}
```

### Blacklist Statistics Endpoint

Expose blacklist statistics via HTTP:

```go
// Add stats endpoint
http.Handle("/admin/blacklist-stats", blacklist.StatsHandler())
```

**Example Response:**
```json
{
  "blocked_ips": ["192.168.1.100", "192.168.1.101"],
  "blocked_cidrs": ["10.0.0.0/8"],
  "total_ips": 2,
  "total_cidrs": 1
}
```

### Integration Example

```go
func main() {
    // Create blacklist
    blacklist := middleware.NewIPBlacklist(middleware.BlacklistConfig{
        UseProxy: true,
    })

    // Block known malicious IPs
    blacklist.BlockIP("203.0.113.1", "Known scanner")
    blacklist.BlockCIDR("198.51.100.0/24", "Spam network")

    // Create your router
    mux := http.NewServeMux()

    // Protected routes
    mux.Handle("/api/", blacklist.Middleware(apiHandler))

    // Admin endpoint to manage blacklist
    mux.HandleFunc("/admin/block-ip", func(w http.ResponseWriter, r *http.Request) {
        ip := r.URL.Query().Get("ip")
        reason := r.URL.Query().Get("reason")

        if err := blacklist.BlockIP(ip, reason); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, "Blocked %s: %s", ip, reason)
    })

    // Stats endpoint
    mux.Handle("/admin/blacklist-stats", blacklist.StatsHandler())

    http.ListenAndServe(":8080", mux)
}
```

---

## Rate Limit Inspection

Monitor and inspect rate limit status per IP address in real-time.

### Basic Usage

```go
import "github.com/bitechdev/ResolveSpec/pkg/middleware"

// Create rate limiter (10 req/sec, burst of 20)
rateLimiter := middleware.NewRateLimiter(10, 20)

// Apply middleware
http.Handle("/api/", rateLimiter.Middleware(yourHandler))
```

### Programmatic Inspection

```go
// Get all tracked IPs
trackedIPs := rateLimiter.GetTrackedIPs()
fmt.Printf("Currently tracking %d IPs\n", len(trackedIPs))

// Get rate limit info for specific IP
info := rateLimiter.GetRateLimitInfo("192.168.1.1")
fmt.Printf("IP: %s\n", info.IP)
fmt.Printf("Tokens Remaining: %.2f\n", info.TokensRemaining)
fmt.Printf("Limit: %.2f req/sec\n", info.Limit)
fmt.Printf("Burst: %d\n", info.Burst)

// Get info for all tracked IPs
allInfo := rateLimiter.GetAllRateLimitInfo()
for _, info := range allInfo {
    fmt.Printf("%s: %.2f tokens remaining\n", info.IP, info.TokensRemaining)
}
```

### Rate Limit Stats Endpoint

Expose rate limit statistics via HTTP:

```go
// Add stats endpoint
http.Handle("/admin/rate-limit-stats", rateLimiter.StatsHandler())
```

**Example Response (all IPs):**
```json
{
  "total_tracked_ips": 3,
  "rate_limit_config": {
    "requests_per_second": 10,
    "burst": 20
  },
  "tracked_ips": [
    {
      "ip": "192.168.1.1",
      "tokens_remaining": 15.5,
      "limit": 10,
      "burst": 20
    },
    {
      "ip": "192.168.1.2",
      "tokens_remaining": 18.2,
      "limit": 10,
      "burst": 20
    }
  ]
}
```

**Example Response (specific IP):**
```bash
GET /admin/rate-limit-stats?ip=192.168.1.1
```
```json
{
  "ip": "192.168.1.1",
  "tokens_remaining": 15.5,
  "limit": 10,
  "burst": 20
}
```

### Complete Integration Example

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"

    "github.com/bitechdev/ResolveSpec/pkg/middleware"
)

func main() {
    // Create rate limiter
    rateLimiter := middleware.NewRateLimiter(10, 20)

    // Create blacklist
    blacklist := middleware.NewIPBlacklist(middleware.BlacklistConfig{
        UseProxy: true,
    })

    mux := http.NewServeMux()

    // API handler with both middlewares (blacklist first, then rate limit)
    apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{
            "message": "Success",
        })
    })

    // Apply middleware chain: blacklist -> rate limit -> handler
    mux.Handle("/api/", blacklist.Middleware(rateLimiter.Middleware(apiHandler)))

    // Admin endpoints
    mux.Handle("/admin/rate-limit-stats", rateLimiter.StatsHandler())
    mux.Handle("/admin/blacklist-stats", blacklist.StatsHandler())

    // Custom monitoring endpoint
    mux.HandleFunc("/admin/monitor", func(w http.ResponseWriter, r *http.Request) {
        // Get rate limit stats
        rateLimitInfo := rateLimiter.GetAllRateLimitInfo()

        // Get blacklist stats
        blockedIPs, blockedCIDRs := blacklist.GetBlacklist()

        response := map[string]interface{}{
            "rate_limits": rateLimitInfo,
            "blacklist": map[string]interface{}{
                "ips":   blockedIPs,
                "cidrs": blockedCIDRs,
            },
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    })

    // Dynamic blacklist management
    mux.HandleFunc("/admin/block", func(w http.ResponseWriter, r *http.Request) {
        ip := r.URL.Query().Get("ip")
        reason := r.URL.Query().Get("reason")

        if ip == "" {
            http.Error(w, "IP required", http.StatusBadRequest)
            return
        }

        if err := blacklist.BlockIP(ip, reason); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }

        fmt.Fprintf(w, "Blocked %s: %s", ip, reason)
    })

    mux.HandleFunc("/admin/unblock", func(w http.ResponseWriter, r *http.Request) {
        ip := r.URL.Query().Get("ip")
        if ip == "" {
            http.Error(w, "IP required", http.StatusBadRequest)
            return
        }

        blacklist.UnblockIP(ip)
        fmt.Fprintf(w, "Unblocked %s", ip)
    })

    // Auto-block IPs that exceed rate limit
    mux.HandleFunc("/admin/auto-block-heavy-users", func(w http.ResponseWriter, r *http.Request) {
        blocked := 0

        for _, info := range rateLimiter.GetAllRateLimitInfo() {
            // If tokens are very low, IP is making many requests
            if info.TokensRemaining < 1.0 {
                blacklist.BlockIP(info.IP, "Exceeded rate limit")
                blocked++
            }
        }

        fmt.Fprintf(w, "Blocked %d IPs exceeding rate limits", blocked)
    })

    fmt.Println("Server starting on :8080")
    fmt.Println("Rate limit stats: http://localhost:8080/admin/rate-limit-stats")
    fmt.Println("Blacklist stats: http://localhost:8080/admin/blacklist-stats")
    http.ListenAndServe(":8080", mux)
}
```

---

## Monitoring Dashboard Example

Create a simple monitoring page:

```go
mux.HandleFunc("/admin/dashboard", func(w http.ResponseWriter, r *http.Request) {
    html := `
    <html>
    <head>
        <title>Security Dashboard</title>
        <script>
            async function loadStats() {
                const rateLimitRes = await fetch('/admin/rate-limit-stats');
                const rateLimitData = await rateLimitRes.json();

                const blacklistRes = await fetch('/admin/blacklist-stats');
                const blacklistData = await blacklistRes.json();

                document.getElementById('rate-limit').innerHTML =
                    JSON.stringify(rateLimitData, null, 2);
                document.getElementById('blacklist').innerHTML =
                    JSON.stringify(blacklistData, null, 2);
            }

            setInterval(loadStats, 5000); // Refresh every 5 seconds
            loadStats();
        </script>
    </head>
    <body>
        <h1>Security Dashboard</h1>

        <h2>Rate Limits</h2>
        <pre id="rate-limit">Loading...</pre>

        <h2>Blacklist</h2>
        <pre id="blacklist">Loading...</pre>
    </body>
    </html>
    `

    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(html))
})
```

---

## Best Practices

### 1. Proxy Configuration
Always set `UseProxy: true` when running behind a reverse proxy (nginx, Cloudflare, etc.):
```go
blacklist := middleware.NewIPBlacklist(middleware.BlacklistConfig{
    UseProxy: true, // Checks X-Forwarded-For headers
})
```

### 2. Middleware Order
Apply blacklist before rate limiting to save resources:
```go
// Correct order: blacklist -> rate limit -> handler
handler := blacklist.Middleware(
    rateLimiter.Middleware(yourHandler)
)
```

### 3. Secure Admin Endpoints
Protect admin endpoints with authentication:
```go
mux.Handle("/admin/", authMiddleware(adminHandler))
```

### 4. Monitoring
Set up alerts when:
- Many IPs are being rate limited
- Blacklist grows too large
- Specific IPs are repeatedly blocked

### 5. Dynamic Response
Automatically block IPs that consistently exceed rate limits:
```go
// Check every minute
ticker := time.NewTicker(1 * time.Minute)
go func() {
    for range ticker.C {
        for _, info := range rateLimiter.GetAllRateLimitInfo() {
            if info.TokensRemaining < 0.5 {
                blacklist.BlockIP(info.IP, "Automated block: rate limit exceeded")
            }
        }
    }
}()
```

### 6. CIDR for Network Blocks
Use CIDR ranges to block entire networks efficiently:
```go
// Block entire subnets
blacklist.BlockCIDR("10.0.0.0/8", "Private network")
blacklist.BlockCIDR("192.168.0.0/16", "Local network")
```

---

## API Reference

### IPBlacklist

#### Methods
- `BlockIP(ip, reason string) error` - Block a single IP address
- `BlockCIDR(cidr, reason string) error` - Block a CIDR range
- `UnblockIP(ip string)` - Remove IP from blacklist
- `UnblockCIDR(cidr string)` - Remove CIDR from blacklist
- `IsBlocked(ip string) (blocked bool, reason string)` - Check if IP is blocked
- `GetBlacklist() (ips, cidrs []string)` - Get all blocked IPs and CIDRs
- `Middleware(next http.Handler) http.Handler` - HTTP middleware
- `StatsHandler() http.Handler` - HTTP handler for statistics

### RateLimiter

#### Methods
- `GetTrackedIPs() []string` - Get all tracked IP addresses
- `GetRateLimitInfo(ip string) *RateLimitInfo` - Get info for specific IP
- `GetAllRateLimitInfo() []*RateLimitInfo` - Get info for all tracked IPs
- `Middleware(next http.Handler) http.Handler` - HTTP middleware
- `StatsHandler() http.Handler` - HTTP handler for statistics

#### RateLimitInfo Structure
```go
type RateLimitInfo struct {
    IP              string  `json:"ip"`
    TokensRemaining float64 `json:"tokens_remaining"`
    Limit           float64 `json:"limit"`
    Burst           int     `json:"burst"`
}
```
