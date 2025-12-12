package eventbroker

import (
	"fmt"
	"os"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/config"
)

// NewProviderFromConfig creates a provider based on configuration
func NewProviderFromConfig(cfg config.EventBrokerConfig) (Provider, error) {
	switch cfg.Provider {
	case "memory":
		cleanupInterval := 5 * time.Minute
		if cfg.Database.PollInterval > 0 {
			cleanupInterval = cfg.Database.PollInterval
		}

		return NewMemoryProvider(MemoryProviderOptions{
			InstanceID:      getInstanceID(cfg.InstanceID),
			MaxEvents:       10000,
			CleanupInterval: cleanupInterval,
		}), nil

	case "redis":
		// Redis provider will be implemented in Phase 8
		return nil, fmt.Errorf("redis provider not yet implemented")

	case "nats":
		// NATS provider will be implemented in Phase 9
		return nil, fmt.Errorf("nats provider not yet implemented")

	case "database":
		// Database provider will be implemented in Phase 7
		return nil, fmt.Errorf("database provider not yet implemented")

	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
	}
}

// getInstanceID returns the instance ID, defaulting to hostname if not specified
func getInstanceID(configID string) string {
	if configID != "" {
		return configID
	}

	// Try to get hostname
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	// Fallback to a default
	return "resolvespec-instance"
}
