package providers

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// MongoProvider implements Provider for MongoDB databases
type MongoProvider struct {
	client *mongo.Client
	config ConnectionConfig
}

// NewMongoProvider creates a new MongoDB provider
func NewMongoProvider() *MongoProvider {
	return &MongoProvider{}
}

// Connect establishes a MongoDB connection
func (p *MongoProvider) Connect(ctx context.Context, cfg ConnectionConfig) error {
	// Build DSN
	dsn, err := cfg.BuildDSN()
	if err != nil {
		return fmt.Errorf("failed to build DSN: %w", err)
	}

	// Create client options
	clientOpts := options.Client().ApplyURI(dsn)

	// Set connection pool size
	if cfg.GetMaxOpenConns() != nil {
		maxPoolSize := uint64(*cfg.GetMaxOpenConns())
		clientOpts.SetMaxPoolSize(maxPoolSize)
	}

	if cfg.GetMaxIdleConns() != nil {
		minPoolSize := uint64(*cfg.GetMaxIdleConns())
		clientOpts.SetMinPoolSize(minPoolSize)
	}

	// Set timeouts
	clientOpts.SetConnectTimeout(cfg.GetConnectTimeout())
	if cfg.GetQueryTimeout() > 0 {
		clientOpts.SetTimeout(cfg.GetQueryTimeout())
	}

	// Set read preference if specified
	if cfg.GetReadPreference() != "" {
		rp, err := parseReadPreference(cfg.GetReadPreference())
		if err != nil {
			return fmt.Errorf("invalid read preference: %w", err)
		}
		clientOpts.SetReadPreference(rp)
	}

	// Connect with retry logic
	var client *mongo.Client
	var lastErr error

	retryAttempts := 3
	retryDelay := 1 * time.Second

	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt, retryDelay, 10*time.Second)
			if cfg.GetEnableLogging() {
				logger.Info("Retrying MongoDB connection: attempt=%d/%d, delay=%v", attempt+1, retryAttempts, delay)
			}

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Create MongoDB client
		client, err = mongo.Connect(ctx, clientOpts)
		if err != nil {
			lastErr = err
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to connect to MongoDB", "error", err)
			}
			continue
		}

		// Ping the database to verify connection
		pingCtx, cancel := context.WithTimeout(ctx, cfg.GetConnectTimeout())
		err = client.Ping(pingCtx, readpref.Primary())
		cancel()

		if err != nil {
			lastErr = err
			_ = client.Disconnect(ctx)
			if cfg.GetEnableLogging() {
				logger.Warn("Failed to ping MongoDB", "error", err)
			}
			continue
		}

		// Connection successful
		break
	}

	if err != nil {
		return fmt.Errorf("failed to connect after %d attempts: %w", retryAttempts, lastErr)
	}

	p.client = client
	p.config = cfg

	if cfg.GetEnableLogging() {
		logger.Info("MongoDB connection established: name=%s, host=%s, database=%s", cfg.GetName(), cfg.GetHost(), cfg.GetDatabase())
	}

	return nil
}

// Close closes the MongoDB connection
func (p *MongoProvider) Close() error {
	if p.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := p.client.Disconnect(ctx)
	if err != nil {
		return fmt.Errorf("failed to close MongoDB connection: %w", err)
	}

	if p.config.GetEnableLogging() {
		logger.Info("MongoDB connection closed: name=%s", p.config.GetName())
	}

	p.client = nil
	return nil
}

// HealthCheck verifies the MongoDB connection is alive
func (p *MongoProvider) HealthCheck(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("MongoDB client is nil")
	}

	// Use a short timeout for health checks
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := p.client.Ping(healthCtx, readpref.Primary()); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// GetNative returns an error for MongoDB (not a SQL database)
func (p *MongoProvider) GetNative() (*sql.DB, error) {
	return nil, ErrNotSQLDatabase
}

// GetMongo returns the MongoDB client
func (p *MongoProvider) GetMongo() (*mongo.Client, error) {
	if p.client == nil {
		return nil, fmt.Errorf("MongoDB client is not initialized")
	}
	return p.client, nil
}

// Stats returns connection statistics for MongoDB
func (p *MongoProvider) Stats() *ConnectionStats {
	if p.client == nil {
		return &ConnectionStats{
			Name:      p.config.GetName(),
			Type:      "mongodb",
			Connected: false,
		}
	}

	// MongoDB doesn't expose detailed connection pool stats like sql.DB
	// We return basic stats
	return &ConnectionStats{
		Name:      p.config.GetName(),
		Type:      "mongodb",
		Connected: true,
	}
}

// parseReadPreference parses a read preference string into a readpref.ReadPref
func parseReadPreference(rp string) (*readpref.ReadPref, error) {
	switch rp {
	case "primary":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown read preference: %s", rp)
	}
}
