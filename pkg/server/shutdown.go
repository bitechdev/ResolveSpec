package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// GracefulServer wraps http.Server with graceful shutdown capabilities
type GracefulServer struct {
	server           *http.Server
	shutdownTimeout  time.Duration
	drainTimeout     time.Duration
	inFlightRequests atomic.Int64
	isShuttingDown   atomic.Bool
	shutdownOnce     sync.Once
	shutdownComplete chan struct{}
}

// Config holds configuration for the graceful server
type Config struct {
	// Addr is the server address (e.g., ":8080")
	Addr string

	// Handler is the HTTP handler
	Handler http.Handler

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	// Default: 30 seconds
	ShutdownTimeout time.Duration

	// DrainTimeout is the time to wait for in-flight requests to complete
	// before forcing shutdown. Default: 25 seconds
	DrainTimeout time.Duration

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration
}

// NewGracefulServer creates a new graceful server
func NewGracefulServer(config Config) *GracefulServer {
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}
	if config.DrainTimeout == 0 {
		config.DrainTimeout = 25 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 10 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 120 * time.Second
	}

	gs := &GracefulServer{
		server: &http.Server{
			Addr:         config.Addr,
			Handler:      config.Handler,
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
			IdleTimeout:  config.IdleTimeout,
		},
		shutdownTimeout:  config.ShutdownTimeout,
		drainTimeout:     config.DrainTimeout,
		shutdownComplete: make(chan struct{}),
	}

	return gs
}

// TrackRequestsMiddleware tracks in-flight requests and blocks new requests during shutdown
func (gs *GracefulServer) TrackRequestsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if shutting down
		if gs.isShuttingDown.Load() {
			http.Error(w, `{"error":"service_unavailable","message":"Server is shutting down"}`, http.StatusServiceUnavailable)
			return
		}

		// Increment in-flight counter
		gs.inFlightRequests.Add(1)
		defer gs.inFlightRequests.Add(-1)

		// Serve the request
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the server and handles graceful shutdown
func (gs *GracefulServer) ListenAndServe() error {
	// Wrap handler with request tracking
	gs.server.Handler = gs.TrackRequestsMiddleware(gs.server.Handler)

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("Starting server on %s", gs.server.Addr)
		if err := gs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErr:
		return err
	case sig := <-sigChan:
		logger.Info("Received signal: %v, initiating graceful shutdown", sig)
		return gs.Shutdown(context.Background())
	}
}

// Shutdown performs graceful shutdown with request draining
func (gs *GracefulServer) Shutdown(ctx context.Context) error {
	var shutdownErr error

	gs.shutdownOnce.Do(func() {
		logger.Info("Starting graceful shutdown...")

		// Mark as shutting down (new requests will be rejected)
		gs.isShuttingDown.Store(true)

		// Create context with timeout
		shutdownCtx, cancel := context.WithTimeout(ctx, gs.shutdownTimeout)
		defer cancel()

		// Wait for in-flight requests to complete (with drain timeout)
		drainCtx, drainCancel := context.WithTimeout(shutdownCtx, gs.drainTimeout)
		defer drainCancel()

		shutdownErr = gs.drainRequests(drainCtx)
		if shutdownErr != nil {
			logger.Error("Error draining requests: %v", shutdownErr)
		}

		// Shutdown the server
		logger.Info("Shutting down HTTP server...")
		if err := gs.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Error shutting down server: %v", err)
			if shutdownErr == nil {
				shutdownErr = err
			}
		}

		logger.Info("Graceful shutdown complete")
		close(gs.shutdownComplete)
	})

	return shutdownErr
}

// drainRequests waits for in-flight requests to complete
func (gs *GracefulServer) drainRequests(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		inFlight := gs.inFlightRequests.Load()

		if inFlight == 0 {
			logger.Info("All requests drained in %v", time.Since(startTime))
			return nil
		}

		select {
		case <-ctx.Done():
			logger.Warn("Drain timeout exceeded with %d requests still in flight", inFlight)
			return fmt.Errorf("drain timeout exceeded: %d requests still in flight", inFlight)
		case <-ticker.C:
			logger.Debug("Waiting for %d in-flight requests to complete...", inFlight)
		}
	}
}

// InFlightRequests returns the current number of in-flight requests
func (gs *GracefulServer) InFlightRequests() int64 {
	return gs.inFlightRequests.Load()
}

// IsShuttingDown returns true if the server is shutting down
func (gs *GracefulServer) IsShuttingDown() bool {
	return gs.isShuttingDown.Load()
}

// Wait blocks until shutdown is complete
func (gs *GracefulServer) Wait() {
	<-gs.shutdownComplete
}

// HealthCheckHandler returns a handler that responds to health checks
// Returns 200 OK when healthy, 503 Service Unavailable when shutting down
func (gs *GracefulServer) HealthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gs.IsShuttingDown() {
			http.Error(w, `{"status":"shutting_down"}`, http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"healthy"}`))
		if err != nil {
			logger.Warn("Failed to write. %v", err)
		}
	}
}

// ReadinessHandler returns a handler for readiness checks
// Includes in-flight request count
func (gs *GracefulServer) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gs.IsShuttingDown() {
			http.Error(w, `{"ready":false,"reason":"shutting_down"}`, http.StatusServiceUnavailable)
			return
		}

		inFlight := gs.InFlightRequests()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"ready":true,"in_flight_requests":%d}`, inFlight)
	}
}

// ShutdownCallback is a function called during shutdown
type ShutdownCallback func(context.Context) error

// shutdownCallbacks stores registered shutdown callbacks
var (
	shutdownCallbacks   []ShutdownCallback
	shutdownCallbacksMu sync.Mutex
)

// RegisterShutdownCallback registers a callback to be called during shutdown
// Useful for cleanup tasks like closing database connections, flushing metrics, etc.
func RegisterShutdownCallback(cb ShutdownCallback) {
	shutdownCallbacksMu.Lock()
	defer shutdownCallbacksMu.Unlock()
	shutdownCallbacks = append(shutdownCallbacks, cb)
}

// executeShutdownCallbacks runs all registered shutdown callbacks
func executeShutdownCallbacks(ctx context.Context) error {
	shutdownCallbacksMu.Lock()
	callbacks := make([]ShutdownCallback, len(shutdownCallbacks))
	copy(callbacks, shutdownCallbacks)
	shutdownCallbacksMu.Unlock()

	var errors []error
	for i, cb := range callbacks {
		logger.Debug("Executing shutdown callback %d/%d", i+1, len(callbacks))
		if err := cb(ctx); err != nil {
			logger.Error("Shutdown callback %d failed: %v", i+1, err)
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown callbacks failed: %v", errors)
	}

	return nil
}

// ShutdownWithCallbacks performs shutdown and executes all registered callbacks
func (gs *GracefulServer) ShutdownWithCallbacks(ctx context.Context) error {
	// Execute callbacks first
	if err := executeShutdownCallbacks(ctx); err != nil {
		logger.Error("Error executing shutdown callbacks: %v", err)
	}

	// Then shutdown the server
	return gs.Shutdown(ctx)
}
