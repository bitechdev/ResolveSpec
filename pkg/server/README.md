# Server Package

Production-ready HTTP server manager with graceful shutdown, request draining, and comprehensive TLS/HTTPS support.

## Features

✅ **Multiple Server Management** - Run multiple HTTP/HTTPS servers concurrently
✅ **Graceful Shutdown** - Handles SIGINT/SIGTERM with request draining
✅ **Automatic Request Rejection** - New requests get 503 during shutdown
✅ **Health & Readiness Endpoints** - Kubernetes-ready health checks
✅ **Shutdown Callbacks** - Register cleanup functions (DB, cache, metrics)
✅ **Comprehensive TLS Support**:
  - Certificate files (production)
  - Self-signed certificates (development/testing)
  - Let's Encrypt / AutoTLS (automatic certificate management)
✅ **GZIP Compression** - Optional response compression
✅ **Panic Recovery** - Automatic panic recovery middleware
✅ **Configurable Timeouts** - Read, write, idle, drain, and shutdown timeouts

## Quick Start

### Single Server

```go
import "github.com/bitechdev/ResolveSpec/pkg/server"

// Create server manager
mgr := server.NewManager()

// Add server
_, err := mgr.Add(server.Config{
    Name:    "api-server",
    Host:    "localhost",
    Port:    8080,
    Handler: myRouter,
    GZIP:    true,
})

// Start and wait for shutdown signal
if err := mgr.ServeWithGracefulShutdown(); err != nil {
    log.Fatal(err)
}
```

### Multiple Servers

```go
mgr := server.NewManager()

// Public API
mgr.Add(server.Config{
    Name:    "public-api",
    Port:    8080,
    Handler: publicRouter,
})

// Admin API
mgr.Add(server.Config{
    Name:    "admin-api",
    Port:    8081,
    Handler: adminRouter,
})

// Start all and wait
mgr.ServeWithGracefulShutdown()
```

## HTTPS/TLS Configuration

### Option 1: Certificate Files (Production)

```go
mgr.Add(server.Config{
    Name:    "https-server",
    Host:    "0.0.0.0",
    Port:    443,
    Handler: handler,
    SSLCert: "/etc/ssl/certs/server.crt",
    SSLKey:  "/etc/ssl/private/server.key",
})
```

### Option 2: Self-Signed Certificate (Development)

```go
mgr.Add(server.Config{
    Name:          "dev-server",
    Host:          "localhost",
    Port:          8443,
    Handler:       handler,
    SelfSignedSSL: true,  // Auto-generates certificate
})
```

### Option 3: Let's Encrypt / AutoTLS (Production)

```go
mgr.Add(server.Config{
    Name:            "prod-server",
    Host:            "0.0.0.0",
    Port:            443,
    Handler:         handler,
    AutoTLS:         true,
    AutoTLSDomains:  []string{"example.com", "www.example.com"},
    AutoTLSEmail:    "admin@example.com",
    AutoTLSCacheDir: "./certs-cache",  // Certificate cache directory
})
```

## Configuration

```go
server.Config{
    // Basic configuration
    Name:        "my-server",        // Server name (required)
    Host:        "0.0.0.0",          // Bind address
    Port:        8080,               // Port (required)
    Handler:     myRouter,           // HTTP handler (required)
    Description: "My API server",    // Optional description

    // Features
    GZIP: true,                      // Enable GZIP compression

    // TLS/HTTPS (choose one option)
    SSLCert:         "/path/to/cert.pem",  // Certificate file
    SSLKey:          "/path/to/key.pem",   // Key file
    SelfSignedSSL:   false,                // Auto-generate self-signed cert
    AutoTLS:         false,                // Let's Encrypt
    AutoTLSDomains:  []string{},           // Domains for AutoTLS
    AutoTLSEmail:    "",                   // Email for Let's Encrypt
    AutoTLSCacheDir: "./certs-cache",      // Cert cache directory

    // Timeouts
    ShutdownTimeout: 30 * time.Second,     // Max shutdown time
    DrainTimeout:    25 * time.Second,     // Request drain timeout
    ReadTimeout:     15 * time.Second,     // Request read timeout
    WriteTimeout:    15 * time.Second,     // Response write timeout
    IdleTimeout:     60 * time.Second,     // Idle connection timeout
}
```

## Graceful Shutdown

### Automatic (Recommended)

```go
mgr := server.NewManager()

// Add servers...

// This blocks until SIGINT/SIGTERM
mgr.ServeWithGracefulShutdown()
```

### Manual Control

```go
mgr := server.NewManager()

// Add and start servers
mgr.StartAll()

// Later... stop gracefully
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := mgr.StopAllWithContext(ctx); err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

### Shutdown Callbacks

Register cleanup functions to run during shutdown:

```go
// Close database
mgr.RegisterShutdownCallback(func(ctx context.Context) error {
    log.Println("Closing database...")
    return db.Close()
})

// Flush metrics
mgr.RegisterShutdownCallback(func(ctx context.Context) error {
    log.Println("Flushing metrics...")
    return metrics.Flush(ctx)
})

// Close cache
mgr.RegisterShutdownCallback(func(ctx context.Context) error {
    log.Println("Closing cache...")
    return cache.Close()
})
```

## Health Checks

### Adding Health Endpoints

```go
instance, _ := mgr.Add(server.Config{
    Name:    "api-server",
    Port:    8080,
    Handler: router,
})

// Add health endpoints to your router
router.HandleFunc("/health", instance.HealthCheckHandler())
router.HandleFunc("/ready", instance.ReadinessHandler())
```

### Health Endpoint

Returns server health status:

**Healthy (200 OK):**
```json
{"status":"healthy"}
```

**Shutting Down (503 Service Unavailable):**
```json
{"status":"shutting_down"}
```

### Readiness Endpoint

Returns readiness with in-flight request count:

**Ready (200 OK):**
```json
{"ready":true,"in_flight_requests":12}
```

**Not Ready (503 Service Unavailable):**
```json
{"ready":false,"reason":"shutting_down"}
```

## Shutdown Behavior

When a shutdown signal (SIGINT/SIGTERM) is received:

1. **Mark as shutting down** → New requests get 503
2. **Execute callbacks** → Run cleanup functions
3. **Drain requests** → Wait up to `DrainTimeout` for in-flight requests
4. **Shutdown servers** → Close listeners and connections

```
Time   Event
─────────────────────────────────────────
0s     Signal received: SIGTERM
       ├─ Mark servers as shutting down
       ├─ Reject new requests (503)
       └─ Execute shutdown callbacks

1s     Callbacks complete
       └─ Start draining requests...

2s     In-flight: 50 requests
3s     In-flight: 32 requests
4s     In-flight: 12 requests
5s     In-flight: 3 requests
6s     In-flight: 0 requests ✓
       └─ All requests drained

6s     Shutdown servers
7s     All servers stopped ✓
```

## Server Management

### Get Server Instance

```go
instance, err := mgr.Get("api-server")
if err != nil {
    log.Fatal(err)
}

// Check status
fmt.Printf("Address: %s\n", instance.Addr())
fmt.Printf("Name: %s\n", instance.Name())
fmt.Printf("In-flight: %d\n", instance.InFlightRequests())
fmt.Printf("Shutting down: %v\n", instance.IsShuttingDown())
```

### List All Servers

```go
instances := mgr.List()
for _, instance := range instances {
    fmt.Printf("Server: %s at %s\n", instance.Name(), instance.Addr())
}
```

### Remove Server

```go
// Stop and remove a server
if err := mgr.Remove("api-server"); err != nil {
    log.Printf("Error removing server: %v", err)
}
```

### Restart All Servers

```go
// Gracefully restart all servers
if err := mgr.RestartAll(); err != nil {
    log.Printf("Error restarting: %v", err)
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

        # Liveness probe
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10

        # Readiness probe
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5

        # Graceful shutdown
        lifecycle:
          preStop:
            exec:
              command: ["/bin/sh", "-c", "sleep 5"]

        env:
        - name: SHUTDOWN_TIMEOUT
          value: "30"

      # Allow time for graceful shutdown
      terminationGracePeriodSeconds: 35
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
    stop_grace_period: 35s
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/bitechdev/ResolveSpec/pkg/server"
)

func main() {
    // Create server manager
    mgr := server.NewManager()

    // Register shutdown callbacks
    mgr.RegisterShutdownCallback(func(ctx context.Context) error {
        log.Println("Cleanup: Closing database...")
        // return db.Close()
        return nil
    })

    // Create router
    router := http.NewServeMux()
    router.HandleFunc("/api/data", dataHandler)

    // Add server
    instance, err := mgr.Add(server.Config{
        Name:            "api-server",
        Host:            "0.0.0.0",
        Port:            8080,
        Handler:         router,
        GZIP:            true,
        ShutdownTimeout: 30 * time.Second,
        DrainTimeout:    25 * time.Second,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Add health endpoints
    router.HandleFunc("/health", instance.HealthCheckHandler())
    router.HandleFunc("/ready", instance.ReadinessHandler())

    // Start and wait for shutdown
    log.Println("Starting server on :8080")
    if err := mgr.ServeWithGracefulShutdown(); err != nil {
        log.Printf("Server stopped: %v", err)
    }

    log.Println("Server shutdown complete")
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
    time.Sleep(100 * time.Millisecond) // Simulate work
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"message":"success"}`))
}
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

# Send requests
for i in {1..10}; do
    curl http://localhost:8080/api/data &
done

# Wait a bit
sleep 1

# Send shutdown signal
kill -TERM $SERVER_PID

# Try more requests (should get 503)
curl -v http://localhost:8080/api/data

# Wait for server to stop
wait $SERVER_PID
echo "Server stopped gracefully"
```

## Best Practices

1. **Set appropriate timeouts**
   - `DrainTimeout` < `ShutdownTimeout`
   - `ShutdownTimeout` < Kubernetes `terminationGracePeriodSeconds`

2. **Use shutdown callbacks** for:
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

5. **HTTPS in production**
   - Use AutoTLS for public-facing services
   - Use certificate files for enterprise PKI
   - Use self-signed only for development/testing

6. **Monitoring**
   - Track in-flight requests in metrics
   - Alert on slow drains
   - Monitor shutdown duration

## Troubleshooting

### Shutdown Takes Too Long

```go
// Increase drain timeout
config.DrainTimeout = 60 * time.Second
config.ShutdownTimeout = 65 * time.Second
```

### Requests Timing Out

```go
// Increase write timeout
config.WriteTimeout = 30 * time.Second
```

### Certificate Issues

```go
// Verify certificate files exist and are readable
if _, err := os.Stat(config.SSLCert); err != nil {
    log.Fatalf("Certificate not found: %v", err)
}

// For AutoTLS, ensure:
// - Port 443 is accessible
// - Domains resolve to server IP
// - Cache directory is writable
```

### Debug Logging

```go
import "github.com/bitechdev/ResolveSpec/pkg/logger"

// Enable debug logging
logger.SetLevel("debug")
```

## API Reference

### Manager Methods

- `NewManager()` - Create new server manager
- `Add(cfg Config)` - Register server instance
- `Get(name string)` - Get server by name
- `Remove(name string)` - Stop and remove server
- `StartAll()` - Start all registered servers
- `StopAll()` - Stop all servers gracefully
- `StopAllWithContext(ctx)` - Stop with timeout
- `RestartAll()` - Restart all servers
- `List()` - Get all server instances
- `ServeWithGracefulShutdown()` - Start and block until shutdown
- `RegisterShutdownCallback(cb)` - Register cleanup function

### Instance Methods

- `Start()` - Start the server
- `Stop(ctx)` - Stop gracefully
- `Addr()` - Get server address
- `Name()` - Get server name
- `HealthCheckHandler()` - Get health handler
- `ReadinessHandler()` - Get readiness handler
- `InFlightRequests()` - Get in-flight count
- `IsShuttingDown()` - Check shutdown status
- `Wait()` - Block until shutdown complete
