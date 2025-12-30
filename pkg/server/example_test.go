package server_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/server"
)

// ExampleManager_basic demonstrates basic server manager usage
func ExampleManager_basic() {
	// Create a server manager
	mgr := server.NewManager()

	// Define a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Hello from server!")
	})

	// Add an HTTP server
	_, err := mgr.Add(server.Config{
		Name:    "api-server",
		Host:    "localhost",
		Port:    8080,
		Handler: handler,
		GZIP:    true, // Enable GZIP compression
	})
	if err != nil {
		panic(err)
	}

	// Start all servers
	if err := mgr.StartAll(); err != nil {
		panic(err)
	}

	// Server is now running...
	// When done, stop gracefully
	if err := mgr.StopAll(); err != nil {
		panic(err)
	}
}

// ExampleManager_https demonstrates HTTPS configurations
func ExampleManager_https() {
	mgr := server.NewManager()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Secure connection!")
	})

	// Option 1: Use certificate files
	_, err := mgr.Add(server.Config{
		Name:    "https-server-files",
		Host:    "localhost",
		Port:    8443,
		Handler: handler,
		SSLCert: "/path/to/cert.pem",
		SSLKey:  "/path/to/key.pem",
	})
	if err != nil {
		panic(err)
	}

	// Option 2: Self-signed certificate (for development)
	_, err = mgr.Add(server.Config{
		Name:          "https-server-self-signed",
		Host:          "localhost",
		Port:          8444,
		Handler:       handler,
		SelfSignedSSL: true,
	})
	if err != nil {
		panic(err)
	}

	// Option 3: Let's Encrypt / AutoTLS (for production)
	_, err = mgr.Add(server.Config{
		Name:           "https-server-letsencrypt",
		Host:           "0.0.0.0",
		Port:           443,
		Handler:        handler,
		AutoTLS:        true,
		AutoTLSDomains: []string{"example.com", "www.example.com"},
		AutoTLSEmail:   "admin@example.com",
		AutoTLSCacheDir: "./certs-cache",
	})
	if err != nil {
		panic(err)
	}

	// Start all servers
	if err := mgr.StartAll(); err != nil {
		panic(err)
	}

	// Cleanup
	mgr.StopAll()
}

// ExampleManager_gracefulShutdown demonstrates graceful shutdown with callbacks
func ExampleManager_gracefulShutdown() {
	mgr := server.NewManager()

	// Register shutdown callbacks for cleanup tasks
	mgr.RegisterShutdownCallback(func(ctx context.Context) error {
		fmt.Println("Closing database connections...")
		// Close your database here
		return nil
	})

	mgr.RegisterShutdownCallback(func(ctx context.Context) error {
		fmt.Println("Flushing metrics...")
		// Flush metrics here
		return nil
	})

	// Add server with custom timeouts
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintln(w, "Done!")
	})

	_, err := mgr.Add(server.Config{
		Name:            "api-server",
		Host:            "localhost",
		Port:            8080,
		Handler:         handler,
		ShutdownTimeout: 30 * time.Second, // Max time for shutdown
		DrainTimeout:    25 * time.Second, // Time to wait for in-flight requests
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	// Start servers and block until shutdown signal (SIGINT/SIGTERM)
	// This will automatically handle graceful shutdown with callbacks
	if err := mgr.ServeWithGracefulShutdown(); err != nil {
		fmt.Printf("Shutdown completed: %v\n", err)
	}
}

// ExampleManager_healthChecks demonstrates health and readiness endpoints
func ExampleManager_healthChecks() {
	mgr := server.NewManager()

	// Create a router with health endpoints
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Data endpoint")
	})

	// Add server
	instance, err := mgr.Add(server.Config{
		Name:    "api-server",
		Host:    "localhost",
		Port:    8080,
		Handler: mux,
	})
	if err != nil {
		panic(err)
	}

	// Add health and readiness endpoints
	mux.HandleFunc("/health", instance.HealthCheckHandler())
	mux.HandleFunc("/ready", instance.ReadinessHandler())

	// Start the server
	if err := mgr.StartAll(); err != nil {
		panic(err)
	}

	// Health check returns:
	// - 200 OK with {"status":"healthy"} when healthy
	// - 503 Service Unavailable with {"status":"shutting_down"} when shutting down

	// Readiness check returns:
	// - 200 OK with {"ready":true,"in_flight_requests":N} when ready
	// - 503 Service Unavailable with {"ready":false,"reason":"shutting_down"} when shutting down

	// Cleanup
	mgr.StopAll()
}

// ExampleManager_multipleServers demonstrates running multiple servers
func ExampleManager_multipleServers() {
	mgr := server.NewManager()

	// Public API server
	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Public API")
	})
	_, err := mgr.Add(server.Config{
		Name:    "public-api",
		Host:    "0.0.0.0",
		Port:    8080,
		Handler: publicHandler,
		GZIP:    true,
	})
	if err != nil {
		panic(err)
	}

	// Admin API server (different port)
	adminHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Admin API")
	})
	_, err = mgr.Add(server.Config{
		Name:    "admin-api",
		Host:    "localhost",
		Port:    8081,
		Handler: adminHandler,
	})
	if err != nil {
		panic(err)
	}

	// Metrics server (internal only)
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Metrics data")
	})
	_, err = mgr.Add(server.Config{
		Name:    "metrics",
		Host:    "127.0.0.1",
		Port:    9090,
		Handler: metricsHandler,
	})
	if err != nil {
		panic(err)
	}

	// Start all servers at once
	if err := mgr.StartAll(); err != nil {
		panic(err)
	}

	// Get specific server instance
	publicInstance, err := mgr.Get("public-api")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Public API running on: %s\n", publicInstance.Addr())

	// List all servers
	instances := mgr.List()
	fmt.Printf("Running %d servers\n", len(instances))

	// Stop all servers gracefully (in parallel)
	if err := mgr.StopAll(); err != nil {
		panic(err)
	}
}

// ExampleManager_monitoring demonstrates monitoring server state
func ExampleManager_monitoring() {
	mgr := server.NewManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond) // Simulate work
		fmt.Fprintln(w, "Done")
	})

	instance, err := mgr.Add(server.Config{
		Name:    "api-server",
		Host:    "localhost",
		Port:    8080,
		Handler: handler,
	})
	if err != nil {
		panic(err)
	}

	if err := mgr.StartAll(); err != nil {
		panic(err)
	}

	// Check server status
	fmt.Printf("Server address: %s\n", instance.Addr())
	fmt.Printf("Server name: %s\n", instance.Name())
	fmt.Printf("Is shutting down: %v\n", instance.IsShuttingDown())
	fmt.Printf("In-flight requests: %d\n", instance.InFlightRequests())

	// Cleanup
	mgr.StopAll()

	// Wait for complete shutdown
	instance.Wait()
}
