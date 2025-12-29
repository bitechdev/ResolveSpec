package server

import (
	"context"
	"net/http"
	"time"
)

// Config holds the configuration for a single web server instance.
type Config struct {
	Name        string
	Host        string
	Port        int
	Description string

	// Handler is the http.Handler (e.g., a router) to be served.
	Handler http.Handler

	// GZIP compression support
	GZIP bool

	// TLS/HTTPS configuration options (mutually exclusive)
	// Option 1: Provide certificate and key files directly
	SSLCert string
	SSLKey  string

	// Option 2: Use self-signed certificate (for development/testing)
	// Generates a self-signed certificate automatically if no SSLCert/SSLKey provided
	SelfSignedSSL bool

	// Option 3: Use Let's Encrypt / Certbot for automatic TLS
	// AutoTLS enables automatic certificate management via Let's Encrypt
	AutoTLS bool
	// AutoTLSDomains specifies the domains for Let's Encrypt certificates
	AutoTLSDomains []string
	// AutoTLSCacheDir specifies where to cache certificates (default: "./certs-cache")
	AutoTLSCacheDir string
	// AutoTLSEmail is the email for Let's Encrypt registration (optional but recommended)
	AutoTLSEmail string

	// Graceful shutdown configuration
	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// Default: 30 seconds
	ShutdownTimeout time.Duration

	// DrainTimeout is the time to wait for in-flight requests to complete
	// before forcing shutdown. Default: 25 seconds
	DrainTimeout time.Duration

	// ReadTimeout is the maximum duration for reading the entire request
	// Default: 15 seconds
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	// Default: 15 seconds
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	// Default: 60 seconds
	IdleTimeout time.Duration
}

// Instance defines the interface for a single server instance.
// It abstracts the underlying http.Server, allowing for easier management and testing.
type Instance interface {
	// Start begins serving requests. This method should be non-blocking and
	// run the server in a separate goroutine.
	Start() error

	// Stop gracefully shuts down the server without interrupting any active connections.
	// It accepts a context to allow for a timeout.
	Stop(ctx context.Context) error

	// Addr returns the network address the server is listening on.
	Addr() string

	// Name returns the server instance name.
	Name() string

	// HealthCheckHandler returns a handler that responds to health checks.
	// Returns 200 OK when healthy, 503 Service Unavailable when shutting down.
	HealthCheckHandler() http.HandlerFunc

	// ReadinessHandler returns a handler for readiness checks.
	// Includes in-flight request count.
	ReadinessHandler() http.HandlerFunc

	// InFlightRequests returns the current number of in-flight requests.
	InFlightRequests() int64

	// IsShuttingDown returns true if the server is shutting down.
	IsShuttingDown() bool

	// Wait blocks until shutdown is complete.
	Wait()
}

// Manager defines the interface for a server manager.
// It is responsible for managing the lifecycle of multiple server instances.
type Manager interface {
	// Add registers a new server instance based on the provided configuration.
	// The server is not started until StartAll or Start is called on the instance.
	Add(cfg Config) (Instance, error)

	// Get returns a server instance by its name.
	Get(name string) (Instance, error)

	// Remove stops and removes a server instance by its name.
	Remove(name string) error

	// StartAll starts all registered server instances that are not already running.
	StartAll() error

	// StopAll gracefully shuts down all running server instances.
	// Executes shutdown callbacks and drains in-flight requests.
	StopAll() error

	// StopAllWithContext gracefully shuts down all running server instances with a context.
	StopAllWithContext(ctx context.Context) error

	// RestartAll gracefully restarts all running server instances.
	RestartAll() error

	// List returns all registered server instances.
	List() []Instance

	// ServeWithGracefulShutdown starts all servers and blocks until a shutdown signal is received.
	// It handles SIGINT and SIGTERM signals and performs graceful shutdown with callbacks.
	ServeWithGracefulShutdown() error

	// RegisterShutdownCallback registers a callback to be called during shutdown.
	// Useful for cleanup tasks like closing database connections, flushing metrics, etc.
	RegisterShutdownCallback(cb ShutdownCallback)
}

// ShutdownCallback is a function called during graceful shutdown.
type ShutdownCallback func(context.Context) error
