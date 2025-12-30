package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/middleware"
	"github.com/klauspost/compress/gzhttp"
)

// gracefulServer wraps http.Server with graceful shutdown capabilities (internal type)
type gracefulServer struct {
	server           *http.Server
	shutdownTimeout  time.Duration
	drainTimeout     time.Duration
	inFlightRequests atomic.Int64
	isShuttingDown   atomic.Bool
	shutdownOnce     sync.Once
	shutdownComplete chan struct{}
}

// trackRequestsMiddleware tracks in-flight requests and blocks new requests during shutdown
func (gs *gracefulServer) trackRequestsMiddleware(next http.Handler) http.Handler {
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

// shutdown performs graceful shutdown with request draining
func (gs *gracefulServer) shutdown(ctx context.Context) error {
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
func (gs *gracefulServer) drainRequests(ctx context.Context) error {
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

// inFlightRequests returns the current number of in-flight requests
func (gs *gracefulServer) inFlightRequestsCount() int64 {
	return gs.inFlightRequests.Load()
}

// isShutdown returns true if the server is shutting down
func (gs *gracefulServer) isShutdown() bool {
	return gs.isShuttingDown.Load()
}

// wait blocks until shutdown is complete
func (gs *gracefulServer) wait() {
	<-gs.shutdownComplete
}

// healthCheckHandler returns a handler that responds to health checks
func (gs *gracefulServer) healthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gs.isShutdown() {
			http.Error(w, `{"status":"shutting_down"}`, http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"status":"healthy"}`))
		if err != nil {
			logger.Warn("Failed to write health check response: %v", err)
		}
	}
}

// readinessHandler returns a handler for readiness checks
func (gs *gracefulServer) readinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if gs.isShutdown() {
			http.Error(w, `{"ready":false,"reason":"shutting_down"}`, http.StatusServiceUnavailable)
			return
		}

		inFlight := gs.inFlightRequestsCount()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"ready":true,"in_flight_requests":%d}`, inFlight)
	}
}

// serverManager manages a collection of server instances with graceful shutdown support.
type serverManager struct {
	instances         map[string]Instance
	mu                sync.RWMutex
	shutdownCallbacks []ShutdownCallback
	callbacksMu       sync.Mutex
}

// NewManager creates a new server manager.
func NewManager() Manager {
	return &serverManager{
		instances:         make(map[string]Instance),
		shutdownCallbacks: make([]ShutdownCallback, 0),
	}
}

// Add registers a new server instance.
func (sm *serverManager) Add(cfg Config) (Instance, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if cfg.Name == "" {
		return nil, fmt.Errorf("server name cannot be empty")
	}
	if _, exists := sm.instances[cfg.Name]; exists {
		return nil, fmt.Errorf("server with name '%s' already exists", cfg.Name)
	}

	instance, err := newInstance(cfg)
	if err != nil {
		return nil, err
	}

	sm.instances[cfg.Name] = instance
	return instance, nil
}

// Get returns a server instance by its name.
func (sm *serverManager) Get(name string) (Instance, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	instance, exists := sm.instances[name]
	if !exists {
		return nil, fmt.Errorf("server with name '%s' not found", name)
	}
	return instance, nil
}

// Remove stops and removes a server instance by its name.
func (sm *serverManager) Remove(name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	instance, exists := sm.instances[name]
	if !exists {
		return fmt.Errorf("server with name '%s' not found", name)
	}

	// Stop the server if it's running. Prefer the server's configured shutdownTimeout
	// when available, and fall back to a sensible default.
	timeout := 10 * time.Second
	if gs, ok := instance.(*gracefulServer); ok && gs.shutdownTimeout > 0 {
		timeout = gs.shutdownTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := instance.Stop(ctx); err != nil {
		logger.Warn("Failed to gracefully stop server '%s' on remove: %v", name, err)
	}

	delete(sm.instances, name)
	return nil
}

// StartAll starts all registered server instances.
func (sm *serverManager) StartAll() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var startErrors []error
	for name, instance := range sm.instances {
		if err := instance.Start(); err != nil {
			startErrors = append(startErrors, fmt.Errorf("failed to start server '%s': %w", name, err))
		}
	}

	if len(startErrors) > 0 {
		return fmt.Errorf("encountered errors while starting servers: %v", startErrors)
	}
	return nil
}

// StopAll gracefully shuts down all running server instances.
func (sm *serverManager) StopAll() error {
	return sm.StopAllWithContext(context.Background())
}

// StopAllWithContext gracefully shuts down all running server instances with a context.
func (sm *serverManager) StopAllWithContext(ctx context.Context) error {
	sm.mu.RLock()
	instancesToStop := make([]Instance, 0, len(sm.instances))
	for _, instance := range sm.instances {
		instancesToStop = append(instancesToStop, instance)
	}
	sm.mu.RUnlock()

	logger.Info("Shutting down all servers...")

	// Execute shutdown callbacks first
	sm.callbacksMu.Lock()
	callbacks := make([]ShutdownCallback, len(sm.shutdownCallbacks))
	copy(callbacks, sm.shutdownCallbacks)
	sm.callbacksMu.Unlock()

	if len(callbacks) > 0 {
		logger.Info("Executing %d shutdown callbacks...", len(callbacks))
		for i, cb := range callbacks {
			if err := cb(ctx); err != nil {
				logger.Error("Shutdown callback %d failed: %v", i+1, err)
			}
		}
	}

	// Stop all instances in parallel
	var shutdownErrors []error
	var wg sync.WaitGroup
	var errorsMu sync.Mutex

	for _, instance := range instancesToStop {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()
			shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := inst.Stop(shutdownCtx); err != nil {
				errorsMu.Lock()
				shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop server '%s': %w", inst.Name(), err))
				errorsMu.Unlock()
			}
		}(instance)
	}

	wg.Wait()

	if len(shutdownErrors) > 0 {
		return fmt.Errorf("encountered errors while stopping servers: %v", shutdownErrors)
	}
	logger.Info("All servers stopped gracefully.")
	return nil
}

// RestartAll gracefully restarts all running server instances.
func (sm *serverManager) RestartAll() error {
	logger.Info("Restarting all servers...")
	if err := sm.StopAll(); err != nil {
		return fmt.Errorf("failed to stop servers during restart: %w", err)
	}

	// Retry starting all servers with exponential backoff instead of a fixed sleep.
	const (
		maxAttempts      = 5
		initialBackoff   = 100 * time.Millisecond
		maxBackoff       = 2 * time.Second
	)

	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := sm.StartAll(); err != nil {
			lastErr = err
			if attempt == maxAttempts {
				break
			}
			logger.Warn("Attempt %d to start servers during restart failed: %v; retrying in %s", attempt, err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		logger.Info("All servers restarted successfully.")
		return nil
	}

	return fmt.Errorf("failed to start servers during restart after %d attempts: %w", maxAttempts, lastErr)
}

// List returns all registered server instances.
func (sm *serverManager) List() []Instance {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	instances := make([]Instance, 0, len(sm.instances))
	for _, instance := range sm.instances {
		instances = append(instances, instance)
	}
	return instances
}

// RegisterShutdownCallback registers a callback to be called during shutdown.
func (sm *serverManager) RegisterShutdownCallback(cb ShutdownCallback) {
	sm.callbacksMu.Lock()
	defer sm.callbacksMu.Unlock()
	sm.shutdownCallbacks = append(sm.shutdownCallbacks, cb)
}

// ServeWithGracefulShutdown starts all servers and blocks until a shutdown signal is received.
func (sm *serverManager) ServeWithGracefulShutdown() error {
	// Start all servers
	if err := sm.StartAll(); err != nil {
		return fmt.Errorf("failed to start servers: %w", err)
	}

	logger.Info("All servers started. Waiting for shutdown signal...")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	sig := <-sigChan
	logger.Info("Received signal: %v, initiating graceful shutdown", sig)

	// Create context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return sm.StopAllWithContext(ctx)
}

// serverInstance is a concrete implementation of the Instance interface.
// It wraps gracefulServer to provide graceful shutdown capabilities.
type serverInstance struct {
	cfg            Config
	gracefulServer *gracefulServer
	certFile       string // Path to certificate file (may be temporary for self-signed)
	keyFile        string // Path to key file (may be temporary for self-signed)
	mu             sync.RWMutex
	running        bool
	serverErr      chan error
}

// newInstance creates a new, unstarted server instance from a config.
func newInstance(cfg Config) (*serverInstance, error) {
	if cfg.Handler == nil {
		return nil, fmt.Errorf("handler cannot be nil")
	}

	// Set default timeouts
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = 25 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var handler http.Handler = cfg.Handler

	// Wrap with GZIP handler if enabled
	if cfg.GZIP {
		gz, err := gzhttp.NewWrapper()
		if err != nil {
			return nil, fmt.Errorf("failed to create GZIP wrapper: %w", err)
		}
		handler = gz(handler)
	}

	// Wrap with the panic recovery middleware
	handler = middleware.PanicRecovery(handler)

	// Configure TLS if any TLS option is enabled
	tlsConfig, certFile, keyFile, err := configureTLS(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to configure TLS: %w", err)
	}

	// Create gracefulServer
	gracefulSrv := &gracefulServer{
		server: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
			TLSConfig:    tlsConfig,
		},
		shutdownTimeout:  cfg.ShutdownTimeout,
		drainTimeout:     cfg.DrainTimeout,
		shutdownComplete: make(chan struct{}),
	}

	return &serverInstance{
		cfg:            cfg,
		gracefulServer: gracefulSrv,
		certFile:       certFile,
		keyFile:        keyFile,
		serverErr:      make(chan error, 1),
	}, nil
}

// Start begins serving requests in a new goroutine.
func (s *serverInstance) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server '%s' is already running", s.cfg.Name)
	}

	// Determine if we're using TLS
	useTLS := s.cfg.SSLCert != "" || s.cfg.SSLKey != "" || s.cfg.SelfSignedSSL || s.cfg.AutoTLS

	// Wrap handler with request tracking
	s.gracefulServer.server.Handler = s.gracefulServer.trackRequestsMiddleware(s.gracefulServer.server.Handler)

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			logger.Info("Server '%s' stopped.", s.cfg.Name)
		}()

		var err error
		protocol := "HTTP"

		if useTLS {
			protocol = "HTTPS"
			logger.Info("Starting %s server '%s' on %s", protocol, s.cfg.Name, s.Addr())

			// For AutoTLS, we need to use a TLS listener
			if s.cfg.AutoTLS {
				// Create listener
				ln, lnErr := net.Listen("tcp", s.gracefulServer.server.Addr)
				if lnErr != nil {
					err = fmt.Errorf("failed to create listener: %w", lnErr)
				} else {
					// Wrap with TLS
					tlsListener := tls.NewListener(ln, s.gracefulServer.server.TLSConfig)
					err = s.gracefulServer.server.Serve(tlsListener)
				}
			} else {
				// Use certificate files (regular SSL or self-signed)
				err = s.gracefulServer.server.ListenAndServeTLS(s.certFile, s.keyFile)
			}
		} else {
			logger.Info("Starting %s server '%s' on %s", protocol, s.cfg.Name, s.Addr())
			err = s.gracefulServer.server.ListenAndServe()
		}

		// If the server stopped for a reason other than a graceful shutdown, log and report the error.
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Server '%s' failed: %v", s.cfg.Name, err)
			select {
			case s.serverErr <- err:
			default:
			}
		}
	}()

	s.running = true
	// A small delay to allow the goroutine to start and potentially fail on binding.
	time.Sleep(50 * time.Millisecond)

	// Check if the server failed to start
	select {
	case err := <-s.serverErr:
		s.running = false
		return err
	default:
	}

	return nil
}

// Stop gracefully shuts down the server.
func (s *serverInstance) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil // Already stopped
	}

	logger.Info("Gracefully shutting down server '%s'...", s.cfg.Name)
	err := s.gracefulServer.shutdown(ctx)
	if err == nil {
		s.running = false
	}
	return err
}

// Addr returns the network address the server is listening on.
func (s *serverInstance) Addr() string {
	return s.gracefulServer.server.Addr
}

// Name returns the server instance name.
func (s *serverInstance) Name() string {
	return s.cfg.Name
}

// HealthCheckHandler returns a handler that responds to health checks.
func (s *serverInstance) HealthCheckHandler() http.HandlerFunc {
	return s.gracefulServer.healthCheckHandler()
}

// ReadinessHandler returns a handler for readiness checks.
func (s *serverInstance) ReadinessHandler() http.HandlerFunc {
	return s.gracefulServer.readinessHandler()
}

// InFlightRequests returns the current number of in-flight requests.
func (s *serverInstance) InFlightRequests() int64 {
	return s.gracefulServer.inFlightRequestsCount()
}

// IsShuttingDown returns true if the server is shutting down.
func (s *serverInstance) IsShuttingDown() bool {
	return s.gracefulServer.isShutdown()
}

// Wait blocks until shutdown is complete.
func (s *serverInstance) Wait() {
	s.gracefulServer.wait()
}
