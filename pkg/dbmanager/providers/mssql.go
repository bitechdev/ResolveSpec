package providers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb" // MSSQL driver
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// MSSQLProvider implements Provider for Microsoft SQL Server databases
type MSSQLProvider struct {
	db     *sql.DB
	config ConnectionConfig
}

// NewMSSQLProvider creates a new MSSQL provider
func NewMSSQLProvider() *MSSQLProvider {
	return &MSSQLProvider{}
}

// Connect establishes a MSSQL connection
func (p *MSSQLProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	// Build DSN
	dsn, err := cfg.BuildDSN()
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	// Connect with retry logic
	var db *sql.DB
	var lastErr error

	retryAttempts := 3 // Default retry attempts
	retryDelay := 1 * time.Second

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, 10*time.Second)
			if cfg.GetEnableLogging() {
				logger.Info("Retrying MSSQL connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Open database connection
		db, err = sql.Open("sqlserver", dsn)
		if err != nil {
			lastErr = err
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to open MSSQL connection", "error", err)
			}
			continue
		}

		// Test the connection with context timeout
		connectCtx, cancel := context.WithTimeout(ctx, cfg.GetConnectTimeout())
		err = db.PingContext(connectCtx)
		cancel()

		if err != nil {
			lastErr = err
			db.Close()
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to ping MSSQL database", "error", err)
			}
			continue
		}

		// Connection successful
		break
	}

	if err != nil {
		return fmt.Errorf("failed to connect after %d attempts: %w", retryAttempts, lastErr)
	}

	// Configure connection pool
	if cfg.GetMaxOpenConns() != nil {
		db.SetMaxOpenConns(*cfg.GetMaxOpenConns())
	}
	if cfg.GetMaxIdleConns() != nil {
		db.SetMaxIdleConns(*cfg.GetMaxIdleConns())
	}
	if cfg.GetConnMaxLifetime() != nil {
		db.SetConnMaxLifetime(*cfg.GetConnMaxLifetime())
	}
	if cfg.GetConnMaxIdleTime() != nil {
		db.SetConnMaxIdleTime(*cfg.GetConnMaxIdleTime())
	}

	p.db = db
	p.config = cfg

	if cfg.GetEnableLogging() {
		logger.Info("MSSQL connection established: name=%s, host=%s, database=%s", cfg.GetName(), cfg.GetHost(), cfg.GetDatabase())
	}

	return nil
}

// Close closes the MSSQL connection
func (p *MSSQLProvider) Close() error {
	if p.db == nil {
		return nil
	}

	err := p.db.Close()
	if err != nil {
		return fmt.Errorf("failed to close MSSQL connection: %w", err)
	}

	if p.config.GetEnableLogging() {
		logger.Info("MSSQL connection closed: name=%s", p.config.GetName())
	}

	p.db = nil
	return nil
}

// HealthCheck verifies the MSSQL connection is alive
func (p *MSSQLProvider) HealthCheck(ctx context.Context) error {
	if p.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Use a short timeout for health checks
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := p.db.PingContext(healthCtx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// GetNative returns the native *sql.DB connection
func (p *MSSQLProvider) GetNative() (*sql.DB, error) {
	if p.db == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}
	return p.db, nil
}

// GetMongo returns an error for MSSQL (not a MongoDB connection)
func (p *MSSQLProvider) GetMongo() (*mongo.Client, error) {
	return nil, ErrNotMongoDB
}

// Stats returns connection pool statistics
func (p *MSSQLProvider) Stats() *ConnectionStats {
	if p.db == nil {
		return &ConnectionStats{
			Name:      p.config.GetName(),
			Type:      "mssql",
			Connected: false,
		}
	}

	stats := p.db.Stats()

	return &ConnectionStats{
		Name:              p.config.GetName(),
		Type:              "mssql",
		Connected:         true,
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}
}
