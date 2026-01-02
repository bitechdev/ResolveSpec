package dbmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Manager manages multiple named database connections
type Manager interface {
	// Connection retrieval
	Get(name string) (Connection, error)
	GetDefault() (Connection, error)
	GetAll() map[string]Connection

	// Default database management
	GetDefaultDatabase() (common.Database, error)
	SetDefaultDatabase(name string) error

	// Lifecycle
	Connect(ctx context.Context) error
	Close() error
	HealthCheck(ctx context.Context) error

	// Stats
	Stats() *ManagerStats
}

// ManagerStats contains statistics about the connection manager
type ManagerStats struct {
	TotalConnections int
	HealthyCount     int
	UnhealthyCount   int
	ConnectionStats  map[string]*ConnectionStats
}

// connectionManager implements Manager
type connectionManager struct {
	connections map[string]Connection
	config      ManagerConfig
	mu          sync.RWMutex

	// Background health check
	healthTicker *time.Ticker
	stopChan     chan struct{}
	wg           sync.WaitGroup
}

var (
	// singleton instance of the manager
	instance Manager
	// instanceMu protects the singleton instance
	instanceMu sync.RWMutex
)

// SetupManager initializes the singleton database manager with the provided configuration.
// This function must be called before GetInstance().
// Returns an error if the manager is already initialized or if configuration is invalid.
func SetupManager(cfg ManagerConfig) error {
	instanceMu.Lock()
	defer instanceMu.Unlock()

	if instance != nil {
		return fmt.Errorf("manager already initialized")
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	instance = mgr
	return nil
}

// GetInstance returns the singleton instance of the database manager.
// Returns an error if SetupManager has not been called yet.
func GetInstance() (Manager, error) {
	instanceMu.RLock()
	defer instanceMu.RUnlock()

	if instance == nil {
		return nil, fmt.Errorf("manager not initialized: call SetupManager first")
	}

	return instance, nil
}

// ResetInstance resets the singleton instance (primarily for testing purposes).
// WARNING: This should only be used in tests. Calling this in production code
// while the manager is in use can lead to undefined behavior.
func ResetInstance() {
	instanceMu.Lock()
	defer instanceMu.Unlock()

	if instance != nil {
		_ = instance.Close()
	}
	instance = nil
}

// NewManager creates a new database connection manager
func NewManager(cfg ManagerConfig) (Manager, error) {
	// Apply defaults and validate configuration
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	mgr := &connectionManager{
		connections: make(map[string]Connection),
		config:      cfg,
		stopChan:    make(chan struct{}),
	}

	return mgr, nil
}

// Get retrieves a named connection
func (m *connectionManager) Get(name string) (Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connections[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrConnectionNotFound, name)
	}

	return conn, nil
}

// GetDefault retrieves the default connection
func (m *connectionManager) GetDefault() (Connection, error) {
	m.mu.RLock()
	defaultName := m.config.DefaultConnection
	m.mu.RUnlock()

	if defaultName == "" {
		return nil, ErrNoDefaultConnection
	}

	return m.Get(defaultName)
}

// GetAll returns all connections
func (m *connectionManager) GetAll() map[string]Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy to avoid concurrent access issues
	result := make(map[string]Connection, len(m.connections))
	for name, conn := range m.connections {
		result[name] = conn
	}

	return result
}

// GetDefaultDatabase returns the common.Database interface from the default connection
func (m *connectionManager) GetDefaultDatabase() (common.Database, error) {
	conn, err := m.GetDefault()
	if err != nil {
		return nil, err
	}

	db, err := conn.Database()
	if err != nil {
		return nil, fmt.Errorf("failed to get database from default connection: %w", err)
	}

	return db, nil
}

// SetDefaultDatabase sets the default database connection by name
func (m *connectionManager) SetDefaultDatabase(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify the connection exists
	if _, ok := m.connections[name]; !ok {
		return fmt.Errorf("%w: %s", ErrConnectionNotFound, name)
	}

	m.config.DefaultConnection = name
	logger.Info("Default database connection changed: name=%s", name)

	return nil
}

// Connect establishes all configured database connections
func (m *connectionManager) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create connections from configuration
	for name := range m.config.Connections {
		// Get a copy of the connection config
		connCfg := m.config.Connections[name]
		// Apply global defaults to connection config
		connCfg.ApplyDefaults(&m.config)
		connCfg.Name = name

		// Create connection using factory
		conn, err := createConnection(connCfg)
		if err != nil {
			return fmt.Errorf("failed to create connection '%s': %w", name, err)
		}

		// Connect
		if err := conn.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect '%s': %w", name, err)
		}

		m.connections[name] = conn
		logger.Info("Database connection established: name=%s, type=%s", name, connCfg.Type)
	}

	// Start background health checks if enabled
	if m.config.EnableAutoReconnect && m.config.HealthCheckInterval > 0 {
		m.startHealthChecker()
	}

	logger.Info("Database manager initialized: connections=%d", len(m.connections))
	return nil
}

// Close closes all database connections
func (m *connectionManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop health checker
	m.stopHealthChecker()

	// Close all connections
	var errors []error
	for name, conn := range m.connections {
		if err := conn.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close connection '%s': %w", name, err))
			logger.Error("Failed to close connection", "name", name, "error", err)
		} else {
			logger.Info("Connection closed: name=%s", name)
		}
	}

	m.connections = make(map[string]Connection)

	if len(errors) > 0 {
		return fmt.Errorf("errors closing connections: %v", errors)
	}

	logger.Info("Database manager closed")
	return nil
}

// HealthCheck performs health checks on all connections
func (m *connectionManager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	connections := make(map[string]Connection, len(m.connections))
	for name, conn := range m.connections {
		connections[name] = conn
	}
	m.mu.RUnlock()

	var errors []error
	for name, conn := range connections {
		if err := conn.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("connection '%s': %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("health check failed for %d connections: %v", len(errors), errors)
	}

	return nil
}

// Stats returns statistics for all connections
func (m *connectionManager) Stats() *ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &ManagerStats{
		TotalConnections: len(m.connections),
		ConnectionStats:  make(map[string]*ConnectionStats),
	}

	for name, conn := range m.connections {
		connStats := conn.Stats()
		stats.ConnectionStats[name] = connStats

		if connStats.Connected && connStats.HealthCheckStatus == "healthy" {
			stats.HealthyCount++
		} else {
			stats.UnhealthyCount++
		}
	}

	return stats
}

// startHealthChecker starts background health checking
func (m *connectionManager) startHealthChecker() {
	if m.healthTicker != nil {
		return // Already running
	}

	m.healthTicker = time.NewTicker(m.config.HealthCheckInterval)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		logger.Info("Health checker started: interval=%v", m.config.HealthCheckInterval)

		for {
			select {
			case <-m.healthTicker.C:
				m.performHealthCheck()
			case <-m.stopChan:
				logger.Info("Health checker stopped")
				return
			}
		}
	}()
}

// stopHealthChecker stops background health checking
func (m *connectionManager) stopHealthChecker() {
	if m.healthTicker != nil {
		m.healthTicker.Stop()
		close(m.stopChan)
		m.wg.Wait()
		m.healthTicker = nil
	}
}

// performHealthCheck performs a health check on all connections
func (m *connectionManager) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m.mu.RLock()
	connections := make([]struct {
		name string
		conn Connection
	}, 0, len(m.connections))
	for name, conn := range m.connections {
		connections = append(connections, struct {
			name string
			conn Connection
		}{name, conn})
	}
	m.mu.RUnlock()

	for _, item := range connections {
		if err := item.conn.HealthCheck(ctx); err != nil {
			logger.Warn("Health check failed",
				"connection", item.name,
				"error", err)

			// Attempt reconnection if enabled
			if m.config.EnableAutoReconnect {
				logger.Info("Attempting reconnection: connection=%s", item.name)
				if err := item.conn.Reconnect(ctx); err != nil {
					logger.Error("Reconnection failed",
						"connection", item.name,
						"error", err)
				} else {
					logger.Info("Reconnection successful: connection=%s", item.name)
				}
			}
		}
	}
}
