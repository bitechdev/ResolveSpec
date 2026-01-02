package dbmanager

import (
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/dbmanager/providers"
)

// createConnection creates a database connection based on the configuration
func createConnection(cfg ConnectionConfig) (Connection, error) {
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid connection configuration: %w", err)
	}

	// Create provider based on database type
	provider, err := createProvider(cfg.Type)
	if err != nil {
		return nil, err
	}

	// Create connection wrapper based on database type
	switch cfg.Type {
	case DatabaseTypePostgreSQL, DatabaseTypeSQLite, DatabaseTypeMSSQL:
		return newSQLConnection(cfg.Name, cfg.Type, cfg, provider), nil
	case DatabaseTypeMongoDB:
		return newMongoConnection(cfg.Name, cfg, provider), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDatabase, cfg.Type)
	}
}

// createProvider creates a database provider based on the database type
func createProvider(dbType DatabaseType) (Provider, error) {
	switch dbType {
	case DatabaseTypePostgreSQL:
		return providers.NewPostgresProvider(), nil
	case DatabaseTypeSQLite:
		return providers.NewSQLiteProvider(), nil
	case DatabaseTypeMSSQL:
		return providers.NewMSSQLProvider(), nil
	case DatabaseTypeMongoDB:
		return providers.NewMongoProvider(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDatabase, dbType)
	}
}

// Provider is an alias to the providers.Provider interface
// This allows dbmanager package consumers to use Provider without importing providers
type Provider = providers.Provider
