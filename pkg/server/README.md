# Server Package

Graceful HTTP server with request draining and shutdown coordination.

## Quick Start

```go
import "github.com/bitechdev/ResolveSpec/pkg/server"

// Create server
srv := server.NewGracefulServer(server.Config{
    Addr:    ":8080",
    Handler: router,
})

// Start server (blocks until shutdown signal)
if err := srv.ListenAndServe(); err != nil {
    log.Fatal(err)
}
```

## Features

✅ Graceful shutdown on SIGINT/SIGTERM
✅ Request draining (waits for in-flight requests)
✅ Automatic request rejection during shutdown
✅ Health and readiness endpoints
✅ Shutdown callbacks for cleanup
✅ Configurable timeouts

## Configuration

```go
config := server.Config{
    // Server address
    Addr: ":8080",

    // HTTP handler
    Handler: myRouter,

    // Maximum time for graceful shutdown (default: 30s)
    ShutdownTimeout: 30 * time.Second,

    // Time to wait for in-flight requests (default: 25s)
    DrainTimeout: 25 * time.Second,

    // Request read timeout (default: 10s)
    ReadTimeout: 10 * time.Second,

    // Response write timeout (default: 10s)
    WriteTimeout: 10 * time.Second,

    // Idle connection timeout (default: 120s)
    IdleTimeout: 120 * time.Second,
}

srv := server.NewGracefulServer(config)
```

## Shutdown Behavior

**Signal received (SIGINT/SIGTERM):**

1. **Mark as shutting down** - New requests get 503
2. **Drain requests** - Wait up to `DrainTimeout` for in-flight requests
3. **Shutdown server** - Close listeners and connections
4. **Execute callbacks** - Run registered cleanup functions

```
Time   Event
─────────────────────────────────────────
0s     Signal received: SIGTERM
       ├─ Mark as shutting down
       ├─ Reject new requests (503)
       └─ Start draining...

1s     In-flight: 50 requests
2s     In-flight: 32 requests
3s     In-flight: 12 requests
4s     In-flight: 3 requests
5s     In-flight: 0 requests ✓
       └─ All requests drained

5s     Execute shutdown callbacks
6s     Shutdown complete
```

## Health Checks

### Health Endpoint

Returns 200 when healthy, 503 when shutting down:

```go
router.HandleFunc("/health", srv.HealthCheckHandler())
```

**Response (healthy):**
```json
{"status":"healthy"}
```

**Response (shutting down):**
```json
{"status":"shutting_down"}
```

### Readiness Endpoint

Includes in-flight request count:

```go
router.HandleFunc("/ready", srv.ReadinessHandler())
```

**Response:**
```json
{"ready":true,"in_flight_requests":12}
```

**During shutdown:**
```json
{"ready":false,"reason":"shutting_down"}
```

## Shutdown Callbacks

Register cleanup functions to run during shutdown:

```go
// Close database
server.RegisterShutdownCallback(func(ctx context.Context) error {
    logger.Info("Closing database connection...")
    return db.Close()
})

// Flush metrics
server.RegisterShutdownCallback(func(ctx context.Context) error {
    logger.Info("Flushing metrics...")
    return metricsProvider.Flush(ctx)
})

// Close cache
server.RegisterShutdownCallback(func(ctx context.Context) error {
    logger.Info("Closing cache...")
    return cache.Close()
})
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/bitechdev/ResolveSpec/pkg/middleware"
    "github.com/bitechdev/ResolveSpec/pkg/metrics"
    "github.com/bitechdev/ResolveSpec/pkg/server"
    "github.com/gorilla/mux"
)

func main() {
    // Initialize metrics
    metricsProvider := metrics.NewPrometheusProvider()
    metrics.SetProvider(metricsProvider)

    // Create router
    router := mux.NewRouter()

    // Apply middleware
    rateLimiter := middleware.NewRateLimiter(100, 20)
    sizeLimiter := middleware.NewRequestSizeLimiter(middleware.Size10MB)
    sanitizer := middleware.DefaultSanitizer()

    router.Use(rateLimiter.Middleware)
    router.Use(sizeLimiter.Middleware)
    router.Use(sanitizer.Middleware)
    router.Use(metricsProvider.Middleware)

    // API routes
    router.HandleFunc("/api/data", dataHandler)

    // Create graceful server
    srv := server.NewGracefulServer(server.Config{
        Addr:            ":8080",
        Handler:         router,
        ShutdownTimeout: 30 * time.Second,
        DrainTimeout:    25 * time.Second,
    })

    // Health checks
    router.HandleFunc("/health", srv.HealthCheckHandler())
    router.HandleFunc("/ready", srv.ReadinessHandler())

    // Metrics endpoint
    router.Handle("/metrics", metricsProvider.Handler())

    // Register shutdown callbacks
    server.RegisterShutdownCallback(func(ctx context.Context) error {
        log.Println("Cleanup: Flushing metrics...")
        return nil
    })

    server.RegisterShutdownCallback(func(ctx context.Context) error {
        log.Println("Cleanup: Closing database...")
        // return db.Close()
        return nil
    })

    // Start server (blocks until shutdown)
    log.Printf("Starting server on :8080")
    if err := srv.ListenAndServe(); err != nil {
        log.Fatal(err)
    }

    // Wait for shutdown to complete
    srv.Wait()
    log.Println("Server stopped")
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
    // Your handler logic
    time.Sleep(100 * time.Millisecond) // Simulate work
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"message":"success"}`))
}
```

## Kubernetes Integration

### Deployment with Probes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: myapp:latest
        ports:
        - containerPort: 8080

        # Liveness probe - is app running?
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
          timeoutSeconds: 5

        # Readiness probe - can app handle traffic?
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
          timeoutSeconds: 3

        # Graceful shutdown
        lifecycle:
          preStop:
            exec:
              command: ["/bin/sh", "-c", "sleep 5"]

        # Environment
        env:
        - name: SHUTDOWN_TIMEOUT
          value: "30"
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp
spec:
  selector:
    app: myapp
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
```

## Docker Compose

```yaml
version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SHUTDOWN_TIMEOUT=30
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 10s
    stop_grace_period: 35s  # Slightly longer than shutdown timeout
```

## Testing Graceful Shutdown

### Test Script

```bash
#!/bin/bash

# Start server in background
./myapp &
SERVER_PID=$!

# Wait for server to start
sleep 2

# Send some requests
for i in {1..10}; do
    curl http://localhost:8080/api/data &
done

# Wait a bit
sleep 1

# Send shutdown signal
kill -TERM $SERVER_PID

# Try to send more requests (should get 503)
curl -v http://localhost:8080/api/data

# Wait for server to stop
wait $SERVER_PID
echo "Server stopped gracefully"
```

### Expected Output

```
Starting server on :8080
Received signal: terminated, initiating graceful shutdown
Starting graceful shutdown...
Waiting for 8 in-flight requests to complete...
Waiting for 4 in-flight requests to complete...
Waiting for 1 in-flight requests to complete...
All requests drained in 2.3s
Cleanup: Flushing metrics...
Cleanup: Closing database...
Shutting down HTTP server...
Graceful shutdown complete
Server stopped
```

## Monitoring In-Flight Requests

```go
// Get current in-flight count
count := srv.InFlightRequests()
fmt.Printf("In-flight requests: %d\n", count)

// Check if shutting down
if srv.IsShuttingDown() {
    fmt.Println("Server is shutting down")
}
```

## Advanced Usage

### Custom Shutdown Logic

```go
// Implement custom shutdown
go func() {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

    <-sigChan
    log.Println("Shutdown signal received")

    // Custom pre-shutdown logic
    log.Println("Running custom cleanup...")

    // Shutdown with callbacks
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := srv.ShutdownWithCallbacks(ctx); err != nil {
        log.Printf("Shutdown error: %v", err)
    }
}()

// Start server
srv.server.ListenAndServe()
```

### Multiple Servers

```go
// HTTP server
httpSrv := server.NewGracefulServer(server.Config{
    Addr:    ":8080",
    Handler: httpRouter,
})

// HTTPS server
httpsSrv := server.NewGracefulServer(server.Config{
    Addr:    ":8443",
    Handler: httpsRouter,
})

// Start both
go httpSrv.ListenAndServe()
go httpsSrv.ListenAndServe()

// Shutdown both on signal
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt)
<-sigChan

ctx := context.Background()
httpSrv.Shutdown(ctx)
httpsSrv.Shutdown(ctx)
```

## Best Practices

1. **Set appropriate timeouts**
   - `DrainTimeout` < `ShutdownTimeout`
   - `ShutdownTimeout` < Kubernetes `terminationGracePeriodSeconds`

2. **Register cleanup callbacks** for:
   - Database connections
   - Message queues
   - Metrics flushing
   - Cache shutdown
   - Background workers

3. **Health checks**
   - Use `/health` for liveness (is app alive?)
   - Use `/ready` for readiness (can app serve traffic?)

4. **Load balancer considerations**
   - Set `preStop` hook in Kubernetes (5-10s delay)
   - Allows load balancer to deregister before shutdown

5. **Monitoring**
   - Track in-flight requests in metrics
   - Alert on slow drains
   - Monitor shutdown duration

## Troubleshooting

### Shutdown Takes Too Long

```go
// Increase drain timeout
config.DrainTimeout = 60 * time.Second
```

### Requests Still Timing Out

```go
// Increase write timeout
config.WriteTimeout = 30 * time.Second
```

### Force Shutdown Not Working

The server will force shutdown after `ShutdownTimeout` even if requests are still in-flight. Adjust timeouts as needed.

### Debugging Shutdown

```go
// Enable debug logging
import "github.com/bitechdev/ResolveSpec/pkg/logger"

logger.SetLevel("debug")
```
