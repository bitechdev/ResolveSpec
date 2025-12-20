package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/middleware"
	"github.com/klauspost/compress/gzhttp"
	"golang.org/x/net/http2"
)

// serverManager manages a collection of server instances.
type serverManager struct {
	instances map[string]Instance
	mu        sync.RWMutex
}

// NewManager creates a new server manager.
func NewManager() Manager {
	return &serverManager{
		instances: make(map[string]Instance),
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

	// Stop the server if it's running
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := instance.Stop(ctx); err != nil {
		logger.Warn("Failed to gracefully stop server '%s' on remove: %v", name, err, context.Background())
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
		// In a real-world scenario, you might want a more sophisticated error handling strategy
		return fmt.Errorf("encountered errors while starting servers: %v", startErrors)
	}
	return nil
}

// StopAll gracefully shuts down all running server instances.
func (sm *serverManager) StopAll() error {
	sm.mu.RLock()
	instancesToStop := make([]Instance, 0, len(sm.instances))
	for _, instance := range sm.instances {
		instancesToStop = append(instancesToStop, instance)
	}
	sm.mu.RUnlock()

	logger.Info("Shutting down all servers...", context.Background())

	var shutdownErrors []error
	var wg sync.WaitGroup

	for _, instance := range instancesToStop {
		wg.Add(1)
		go func(inst Instance) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := inst.Stop(ctx); err != nil {
				shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop server '%s': %w", inst.Addr(), err))
			}
		}(instance)
	}

	wg.Wait()

	if len(shutdownErrors) > 0 {
		return fmt.Errorf("encountered errors while stopping servers: %v", shutdownErrors)
	}
	logger.Info("All servers stopped gracefully.", context.Background())
	return nil
}

// RestartAll gracefully restarts all running server instances.
func (sm *serverManager) RestartAll() error {
	logger.Info("Restarting all servers...", context.Background())
	if err := sm.StopAll(); err != nil {
		return fmt.Errorf("failed to stop servers during restart: %w", err)
	}

	// Give ports time to be released
	time.Sleep(200 * time.Millisecond)

	if err := sm.StartAll(); err != nil {
		return fmt.Errorf("failed to start servers during restart: %w", err)
	}
	logger.Info("All servers restarted successfully.", context.Background())
	return nil
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

// serverInstance is a concrete implementation of the Instance interface.
type serverInstance struct {
	cfg        Config
	httpServer *http.Server
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
}

// newInstance creates a new, unstarted server instance from a config.
func newInstance(cfg Config) (*serverInstance, error) {
	if cfg.Handler == nil {
		return nil, fmt.Errorf("handler cannot be nil")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	var handler http.Handler = cfg.Handler

	// Wrap with GZIP handler if enabled
	if cfg.GZIP {
		gz, err := gzhttp.NewWrapper(gzhttp.BestSpeed)
		if err != nil {
			return nil, fmt.Errorf("failed to create GZIP wrapper: %w", err)
		}
		handler = gz(handler)
	}

	// Wrap with the panic recovery middleware
	handler = middleware.PanicRecovery(handler)

	// Here you could add other default middleware like request logging, metrics, etc.

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &serverInstance{
		cfg:        cfg,
		httpServer: httpServer,
		stopCh:     make(chan struct{}),
	}, nil
}

// Start begins serving requests in a new goroutine.
func (s *serverInstance) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server '%s' is already running", s.cfg.Name)
	}

	hasSSL := s.cfg.SSLCert != "" && s.cfg.SSLKey != ""

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			logger.Info("Server '%s' stopped.", s.cfg.Name, context.Background())
		}()

		var err error
		protocol := "HTTP"

		if hasSSL {
			protocol = "HTTPS"
			// Configure TLS + HTTP/2
			s.httpServer.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
			            logger.Info("Starting %s server '%s' on %s", protocol, s.cfg.Name, s.Addr(), context.Background())			err = s.httpServer.ListenAndServeTLS(s.cfg.SSLCert, s.cfg.SSLKey)
		} else {
			logger.Info("Starting %s server '%s' on %s", protocol, s.cfg.Name, s.Addr(), context.Background())
			err = s.httpServer.ListenAndServe()
		}

		// If the server stopped for a reason other than a graceful shutdown, log the error.
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Server '%s' failed: %v", s.cfg.Name, err, context.Background())
		}
	}()

	s.running = true
	// A small delay to allow the goroutine to start and potentially fail on binding.
	// A more robust solution might involve a channel signal.
	time.Sleep(50 * time.Millisecond)

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
	err := s.httpServer.Shutdown(ctx)
	if err == nil {
		s.running = false
	}
	return err
}

// Addr returns the network address the server is listening on.
func (s *serverInstance) Addr() string {
	return s.httpServer.Addr
}
