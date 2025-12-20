package server

import (
	"context"
	"net/http"
)

// Config holds the configuration for a single web server instance.
type Config struct {
	Name        string
	Host        string
	Port        int
	Description string
	SSLCert     string
	SSLKey      string
	GZIP        bool
	// Handler is the http.Handler (e.g., a router) to be served.
	Handler http.Handler
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
	StopAll() error
	// RestartAll gracefully restarts all running server instances.
	RestartAll() error
	// List returns all registered server instances.
	List() []Instance
}
