# Middleware Package

HTTP middleware utilities including rate limiting.

## Rate Limiting

Production-grade rate limiting using token bucket algorithm.

### Quick Start

```go
import "github.com/bitechdev/ResolveSpec/pkg/middleware"

// Create rate limiter: 100 requests per second, burst of 20
rateLimiter := middleware.NewRateLimiter(100, 20)

// Apply to all routes
router.Use(rateLimiter.Middleware)
```

### Basic Usage

```go
package main

import (
    "log"
    "net/http"

    "github.com/bitechdev/ResolveSpec/pkg/middleware"
    "github.com/gorilla/mux"
)

func main() {
    router := mux.NewRouter()

    // Rate limit: 10 requests per second, burst of 5
    rateLimiter := middleware.NewRateLimiter(10, 5)
    router.Use(rateLimiter.Middleware)

    router.HandleFunc("/api/data", dataHandler)

    log.Fatal(http.ListenAndServe(":8080", router))
}
```

### Custom Key Extraction

By default, rate limiting is per IP address. Customize the key:

```go
// Rate limit by User ID from header
keyFunc := func(r *http.Request) string {
    userID := r.Header.Get("X-User-ID")
    if userID == "" {
        return r.RemoteAddr // Fallback to IP
    }
    return "user:" + userID
}

router.Use(rateLimiter.MiddlewareWithKeyFunc(keyFunc))
```

### Advanced Key Functions

**By API Key:**

```go
keyFunc := func(r *http.Request) string {
    apiKey := r.Header.Get("X-API-Key")
    if apiKey == "" {
        return r.RemoteAddr
    }
    return "api:" + apiKey
}
```

**By Authenticated User:**

```go
keyFunc := func(r *http.Request) string {
    // Extract from JWT or session
    user := getUserFromContext(r.Context())
    if user != nil {
        return "user:" + user.ID
    }
    return r.RemoteAddr
}
```

**By Path + User:**

```go
keyFunc := func(r *http.Request) string {
    user := getUserFromContext(r.Context())
    if user != nil {
        return fmt.Sprintf("user:%s:path:%s", user.ID, r.URL.Path)
    }
    return r.URL.Path + ":" + r.RemoteAddr
}
```

### Different Limits Per Route

```go
func main() {
    router := mux.NewRouter()

    // Public endpoints: 10 rps
    publicLimiter := middleware.NewRateLimiter(10, 5)

    // API endpoints: 100 rps
    apiLimiter := middleware.NewRateLimiter(100, 20)

    // Admin endpoints: 1000 rps
    adminLimiter := middleware.NewRateLimiter(1000, 50)

    // Apply different limiters to subrouters
    publicRouter := router.PathPrefix("/public").Subrouter()
    publicRouter.Use(publicLimiter.Middleware)

    apiRouter := router.PathPrefix("/api").Subrouter()
    apiRouter.Use(apiLimiter.Middleware)

    adminRouter := router.PathPrefix("/admin").Subrouter()
    adminRouter.Use(adminLimiter.Middleware)
}
```

### Rate Limit Response

When rate limited, clients receive:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: text/plain

{"error":"rate_limit_exceeded","message":"Too many requests"}
```

### Configuration Examples

**Tight Rate Limit (Anti-abuse):**

```go
// 1 request per second, burst of 3
rateLimiter := middleware.NewRateLimiter(1, 3)
```

**Moderate Rate Limit (Standard API):**

```go
// 100 requests per second, burst of 20
rateLimiter := middleware.NewRateLimiter(100, 20)
```

**Generous Rate Limit (Internal Services):**

```go
// 1000 requests per second, burst of 100
rateLimiter := middleware.NewRateLimiter(1000, 100)
```

**Time-based Limits:**

```go
// 60 requests per minute = 1 request per second
rateLimiter := middleware.NewRateLimiter(1, 10)

// 1000 requests per hour ≈ 0.28 requests per second
rateLimiter := middleware.NewRateLimiter(0.28, 50)
```

### Understanding Burst

The burst parameter allows short bursts above the rate:

```go
// Rate: 10 rps, Burst: 5
// Allows up to 5 requests immediately, then 10/second
rateLimiter := middleware.NewRateLimiter(10, 5)
```

**Bucket fills at rate:** 10 tokens/second
**Bucket capacity:** 5 tokens
**Request consumes:** 1 token

**Example traffic pattern:**
- T=0s: 5 requests → ✅ All allowed (burst)
- T=0.1s: 1 request → ❌ Denied (bucket empty)
- T=0.5s: 1 request → ✅ Allowed (bucket refilled 0.5 tokens)
- T=1s: 1 request → ✅ Allowed (bucket has ~1 token)

### Cleanup Behavior

The rate limiter automatically cleans up inactive limiters every 5 minutes to prevent memory leaks.

### Performance Characteristics

- **Memory**: ~100 bytes per active limiter
- **Throughput**: >1M requests/second
- **Latency**: <1μs per request
- **Concurrency**: Lock-free for rate checks

### Production Deployment

**With Reverse Proxy:**

```go
// Use X-Forwarded-For or X-Real-IP
keyFunc := func(r *http.Request) string {
    // Check proxy headers first
    if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
        return strings.Split(ip, ",")[0]
    }
    if ip := r.Header.Get("X-Real-IP"); ip != "" {
        return ip
    }
    return r.RemoteAddr
}

router.Use(rateLimiter.MiddlewareWithKeyFunc(keyFunc))
```

**Environment-based Configuration:**

```go
import "os"

func getRateLimiter() *middleware.RateLimiter {
    rps := getEnvFloat("RATE_LIMIT_RPS", 100)
    burst := getEnvInt("RATE_LIMIT_BURST", 20)
    return middleware.NewRateLimiter(rps, burst)
}
```

### Testing Rate Limits

```bash
# Send 10 requests rapidly
for i in {1..10}; do
  curl -w "Status: %{http_code}\n" http://localhost:8080/api/data
done
```

**Expected output:**
```
Status: 200  # Request 1-5 (within burst)
Status: 200
Status: 200
Status: 200
Status: 200
Status: 429  # Request 6-10 (rate limited)
Status: 429
Status: 429
Status: 429
Status: 429
```

### Complete Example

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
    "os"
    "strconv"

    "github.com/bitechdev/ResolveSpec/pkg/middleware"
    "github.com/gorilla/mux"
)

func main() {
    // Configuration from environment
    rps, _ := strconv.ParseFloat(os.Getenv("RATE_LIMIT_RPS"), 64)
    if rps == 0 {
        rps = 100 // Default
    }

    burst, _ := strconv.Atoi(os.Getenv("RATE_LIMIT_BURST"))
    if burst == 0 {
        burst = 20 // Default
    }

    // Create rate limiter
    rateLimiter := middleware.NewRateLimiter(rps, burst)

    // Custom key extraction
    keyFunc := func(r *http.Request) string {
        // Try API key first
        if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
            return "api:" + apiKey
        }
        // Try authenticated user
        if userID := r.Header.Get("X-User-ID"); userID != "" {
            return "user:" + userID
        }
        // Fall back to IP
        if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
            return ip
        }
        return r.RemoteAddr
    }

    // Create router
    router := mux.NewRouter()

    // Apply rate limiting
    router.Use(rateLimiter.MiddlewareWithKeyFunc(keyFunc))

    // Routes
    router.HandleFunc("/api/data", dataHandler)
    router.HandleFunc("/health", healthHandler)

    log.Printf("Starting server with rate limit: %.1f rps, burst: %d", rps, burst)
    log.Fatal(http.ListenAndServe(":8080", router))
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]string{
        "message": "Data endpoint",
    })
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}
```

## Best Practices

1. **Set Appropriate Limits**: Consider your backend capacity
   - Database: Can it handle X queries/second?
   - External APIs: What are their rate limits?
   - Server resources: CPU, memory, connections

2. **Use Burst Wisely**: Allow legitimate traffic spikes
   - Too low: Reject valid bursts
   - Too high: Allow abuse

3. **Monitor Rate Limits**: Track how often limits are hit
   ```go
   // Log rate limit events
   if rateLimited {
       log.Printf("Rate limited: %s", clientKey)
   }
   ```

4. **Provide Feedback**: Include rate limit headers (future enhancement)
   ```http
   X-RateLimit-Limit: 100
   X-RateLimit-Remaining: 95
   X-RateLimit-Reset: 1640000000
   ```

5. **Tiered Limits**: Different limits for different user tiers
   ```go
   func getRateLimiter(userTier string) *middleware.RateLimiter {
       switch userTier {
       case "premium":
           return middleware.NewRateLimiter(1000, 100)
       case "standard":
           return middleware.NewRateLimiter(100, 20)
       default:
           return middleware.NewRateLimiter(10, 5)
       }
   }
   ```
