# Metrics Package

A pluggable metrics collection system with Prometheus implementation.

## Quick Start

```go
import "github.com/bitechdev/ResolveSpec/pkg/metrics"

// Initialize Prometheus provider with default config
provider := metrics.NewPrometheusProvider(nil)
metrics.SetProvider(provider)

// Apply middleware to your router
router.Use(provider.Middleware)

// Expose metrics endpoint
http.Handle("/metrics", provider.Handler())
```

## Configuration

You can customize the metrics provider using a configuration struct:

```go
import "github.com/bitechdev/ResolveSpec/pkg/metrics"

// Create custom configuration
config := &metrics.Config{
    Enabled:  true,
    Provider: "prometheus",
    Namespace: "myapp", // Prefix all metrics with "myapp_"
    HTTPRequestBuckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
    DBQueryBuckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1},
}

// Initialize with custom config
provider := metrics.NewPrometheusProvider(config)
metrics.SetProvider(provider)
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `true` | Enable/disable metrics collection |
| `Provider` | `string` | `"prometheus"` | Metrics provider type |
| `Namespace` | `string` | `""` | Prefix for all metric names |
| `HTTPRequestBuckets` | `[]float64` | See below | Histogram buckets for HTTP duration (seconds) |
| `DBQueryBuckets` | `[]float64` | See below | Histogram buckets for DB query duration (seconds) |

**Default HTTP Request Buckets:** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`

**Default DB Query Buckets:** `[0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5]`

### Pushgateway Configuration (Optional)

For batch jobs, cron tasks, or short-lived processes, you can push metrics to Prometheus Pushgateway:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `PushgatewayURL` | `string` | `""` | URL of Pushgateway (e.g., "http://pushgateway:9091") |
| `PushgatewayJobName` | `string` | `"resolvespec"` | Job name for pushed metrics |
| `PushgatewayInterval` | `int` | `0` | Auto-push interval in seconds (0 = disabled) |

```go
config := &metrics.Config{
    PushgatewayURL:      "http://pushgateway:9091",
    PushgatewayJobName:  "batch-job",
    PushgatewayInterval: 30, // Push every 30 seconds
}
```

## Provider Interface

The package uses a provider interface, allowing you to plug in different metric systems:

```go
type Provider interface {
    RecordHTTPRequest(method, path, status string, duration time.Duration)
    IncRequestsInFlight()
    DecRequestsInFlight()
    RecordDBQuery(operation, table string, duration time.Duration, err error)
    RecordCacheHit(provider string)
    RecordCacheMiss(provider string)
    UpdateCacheSize(provider string, size int64)
    Handler() http.Handler
}
```

## Recording Metrics

### HTTP Metrics (Automatic)

When using the middleware, HTTP metrics are recorded automatically:

```go
router.Use(provider.Middleware)
```

**Collected:**
- Request duration (histogram)
- Request count by method, path, and status
- Requests in flight (gauge)

### Database Metrics

```go
start := time.Now()
rows, err := db.Query("SELECT * FROM users WHERE id = ?", userID)
duration := time.Since(start)

metrics.GetProvider().RecordDBQuery("SELECT", "users", duration, err)
```

### Cache Metrics

```go
// Record cache hit
metrics.GetProvider().RecordCacheHit("memory")

// Record cache miss
metrics.GetProvider().RecordCacheMiss("memory")

// Update cache size
metrics.GetProvider().UpdateCacheSize("memory", 1024)
```

## Prometheus Metrics

When using `PrometheusProvider`, the following metrics are available:

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `http_request_duration_seconds` | Histogram | method, path, status | HTTP request duration |
| `http_requests_total` | Counter | method, path, status | Total HTTP requests |
| `http_requests_in_flight` | Gauge | - | Current in-flight requests |
| `db_query_duration_seconds` | Histogram | operation, table | Database query duration |
| `db_queries_total` | Counter | operation, table, status | Total database queries |
| `cache_hits_total` | Counter | provider | Total cache hits |
| `cache_misses_total` | Counter | provider | Total cache misses |
| `cache_size_items` | Gauge | provider | Current cache size |
| `events_published_total` | Counter | source, event_type | Total events published |
| `events_processed_total` | Counter | source, event_type, status | Total events processed |
| `event_processing_duration_seconds` | Histogram | source, event_type | Event processing duration |
| `event_queue_size` | Gauge | - | Current event queue size |
| `panics_total` | Counter | method | Total panics recovered |

**Note:** If a custom `Namespace` is configured, all metric names will be prefixed with `{namespace}_`.

## Prometheus Queries

### HTTP Request Rate

```promql
rate(http_requests_total[5m])
```

### HTTP Request Duration (95th percentile)

```promql
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
```

### Database Query Error Rate

```promql
rate(db_queries_total{status="error"}[5m])
```

### Cache Hit Rate

```promql
rate(cache_hits_total[5m]) / (rate(cache_hits_total[5m]) + rate(cache_misses_total[5m]))
```

## No-Op Provider

If metrics are disabled:

```go
// No provider set - uses no-op provider automatically
metrics.GetProvider().RecordHTTPRequest(...) // Does nothing
```

## Custom Provider

Implement your own metrics provider:

```go
type CustomProvider struct{}

func (c *CustomProvider) RecordHTTPRequest(method, path, status string, duration time.Duration) {
    // Send to your metrics system
}

// Implement other Provider interface methods...

func (c *CustomProvider) Handler() http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Return your metrics format
    })
}

// Use it
metrics.SetProvider(&CustomProvider{})
```

## Pushgateway Usage

### Automatic Push (Batch Jobs)

For jobs that run periodically, use automatic pushing:

```go
package main

import (
    "time"

    "github.com/bitechdev/ResolveSpec/pkg/metrics"
)

func main() {
    // Configure with automatic pushing every 30 seconds
    config := &metrics.Config{
        Enabled:             true,
        Provider:            "prometheus",
        Namespace:           "batch_job",
        PushgatewayURL:      "http://pushgateway:9091",
        PushgatewayJobName:  "data-processor",
        PushgatewayInterval: 30, // Push every 30 seconds
    }

    provider := metrics.NewPrometheusProvider(config)
    metrics.SetProvider(provider)

    // Ensure cleanup on exit
    defer provider.StopAutoPush()

    // Your batch job logic here
    processBatchData()
}
```

### Manual Push (Short-lived Processes)

For one-time jobs or when you want manual control:

```go
package main

import (
    "log"

    "github.com/bitechdev/ResolveSpec/pkg/metrics"
)

func main() {
    // Configure without automatic pushing
    config := &metrics.Config{
        Enabled:            true,
        Provider:           "prometheus",
        PushgatewayURL:     "http://pushgateway:9091",
        PushgatewayJobName: "migration-job",
        // PushgatewayInterval: 0 (default - no auto-push)
    }

    provider := metrics.NewPrometheusProvider(config)
    metrics.SetProvider(provider)

    // Run your job
    err := runMigration()

    // Push metrics at the end
    if pushErr := provider.Push(); pushErr != nil {
        log.Printf("Failed to push metrics: %v", pushErr)
    }

    if err != nil {
        log.Fatal(err)
    }
}
```

### Docker Compose with Pushgateway

```yaml
version: '3'
services:
  batch-job:
    build: .
    environment:
      PUSHGATEWAY_URL: "http://pushgateway:9091"

  pushgateway:
    image: prom/pushgateway
    ports:
      - "9091:9091"

  prometheus:
    image: prom/prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
```

**prometheus.yml for Pushgateway:**

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  # Scrape the pushgateway
  - job_name: 'pushgateway'
    honor_labels: true  # Important: preserve job labels from pushed metrics
    static_configs:
      - targets: ['pushgateway:9091']
```

## Complete Example

### Basic Usage

```go
package main

import (
    "database/sql"
    "log"
    "net/http"
    "time"

    "github.com/bitechdev/ResolveSpec/pkg/metrics"
    "github.com/gorilla/mux"
)

func main() {
    // Initialize metrics with default config
    provider := metrics.NewPrometheusProvider(nil)
    metrics.SetProvider(provider)

    // Create router
    router := mux.NewRouter()

    // Apply metrics middleware
    router.Use(provider.Middleware)

    // Expose metrics endpoint
    router.Handle("/metrics", provider.Handler())

    // Your API routes
    router.HandleFunc("/api/users", getUsersHandler)

    log.Fatal(http.ListenAndServe(":8080", router))
}

func getUsersHandler(w http.ResponseWriter, r *http.Request) {
    // Record database query
    start := time.Now()
    users, err := fetchUsers()
    duration := time.Since(start)

    metrics.GetProvider().RecordDBQuery("SELECT", "users", duration, err)

    if err != nil {
        http.Error(w, "Internal Server Error", 500)
        return
    }

    // Return users...
}
```

### With Custom Configuration

```go
package main

import (
    "log"
    "net/http"

    "github.com/bitechdev/ResolveSpec/pkg/metrics"
    "github.com/gorilla/mux"
)

func main() {
    // Custom metrics configuration
    metricsConfig := &metrics.Config{
        Enabled:  true,
        Provider: "prometheus",
        Namespace: "myapp",
        // Custom buckets optimized for your application
        HTTPRequestBuckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10},
        DBQueryBuckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
    }

    // Initialize with custom config
    provider := metrics.NewPrometheusProvider(metricsConfig)
    metrics.SetProvider(provider)

    router := mux.NewRouter()
    router.Use(provider.Middleware)
    router.Handle("/metrics", provider.Handler())

    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Docker Compose Example

```yaml
version: '3'
services:
  app:
    build: .
    ports:
      - "8080:8080"

  prometheus:
    image: prom/prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'

  grafana:
    image: grafana/grafana
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
```

**prometheus.yml:**

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'resolvespec'
    static_configs:
      - targets: ['app:8080']
```

## Best Practices

1. **Label Cardinality**: Keep labels low-cardinality
   - ✅ Good: `method`, `status_code`
   - ❌ Bad: `user_id`, `timestamp`

2. **Path Normalization**: Normalize dynamic paths
   ```go
   // Instead of /api/users/123
   // Use /api/users/:id
   ```

3. **Metric Naming**: Follow Prometheus conventions
   - Use `_total` suffix for counters
   - Use `_seconds` suffix for durations
   - Use base units (seconds, not milliseconds)

4. **Performance**: Metrics collection is lock-free and highly performant
   - Safe for high-throughput applications
   - Minimal overhead (<1% in most cases)

5. **Pull vs Push**:
   - **Use Pull (default)**: Long-running services, web servers, microservices
   - **Use Push (Pushgateway)**: Batch jobs, cron tasks, short-lived processes, serverless functions
   - Pull is preferred for most applications as it allows Prometheus to detect if your service is down
