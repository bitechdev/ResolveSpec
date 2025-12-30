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
		return NewRedisProvider(RedisProviderConfig{
			Host:          cfg.Redis.Host,
			Port:          cfg.Redis.Port,
			Password:      cfg.Redis.Password,
			DB:            cfg.Redis.DB,
			StreamName:    cfg.Redis.StreamName,
			ConsumerGroup: cfg.Redis.ConsumerGroup,
			ConsumerName:  getInstanceID(cfg.InstanceID),
			InstanceID:    getInstanceID(cfg.InstanceID),
			MaxLen:        cfg.Redis.MaxLen,
		})

	case "nats":
		// NATS provider initialization
		// Note: Requires github.com/nats-io/nats.go dependency
		return NewNATSProvider(NATSProviderConfig{
			URL:           cfg.NATS.URL,
			StreamName:    cfg.NATS.StreamName,
			SubjectPrefix: "events",
			InstanceID:    getInstanceID(cfg.InstanceID),
			MaxAge:        cfg.NATS.MaxAge,
			Storage:       cfg.NATS.Storage, // "file" or "memory"
		})

	case "database":
		// Database provider requires a database connection
		// This should be provided externally
		return nil, fmt.Errorf("database provider requires a database connection to be configured separately")

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
