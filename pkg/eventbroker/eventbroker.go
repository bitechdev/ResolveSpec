package eventbroker

import (
	"context"
	"fmt"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/config"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/server"
)

var (
	defaultBroker Broker
	brokerMu      sync.RWMutex
)

// Initialize initializes the global event broker from configuration
func Initialize(cfg config.EventBrokerConfig) error {
	if !cfg.Enabled {
		logger.Info("Event broker is disabled")
		return nil
	}

	// Create provider
	provider, err := NewProviderFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Parse mode
	mode := ProcessingModeAsync
	if cfg.Mode == "sync" {
		mode = ProcessingModeSync
	}

	// Convert retry policy
	retryPolicy := &RetryPolicy{
		MaxRetries:    cfg.RetryPolicy.MaxRetries,
		InitialDelay:  cfg.RetryPolicy.InitialDelay,
		MaxDelay:      cfg.RetryPolicy.MaxDelay,
		BackoffFactor: cfg.RetryPolicy.BackoffFactor,
	}
	if retryPolicy.MaxRetries == 0 {
		retryPolicy = DefaultRetryPolicy()
	}

	// Create broker options
	opts := Options{
		Provider:    provider,
		Mode:        mode,
		WorkerCount: cfg.WorkerCount,
		BufferSize:  cfg.BufferSize,
		RetryPolicy: retryPolicy,
		InstanceID:  getInstanceID(cfg.InstanceID),
	}

	// Create broker
	broker, err := NewBroker(opts)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}

	// Start broker
	if err := broker.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start broker: %w", err)
	}

	// Set as default
	SetDefaultBroker(broker)

	// Register shutdown callback
	RegisterShutdown(broker)

	logger.Info("Event broker initialized successfully (provider: %s, mode: %s, instance: %s)",
		cfg.Provider, cfg.Mode, opts.InstanceID)

	return nil
}

// SetDefaultBroker sets the default global broker
func SetDefaultBroker(broker Broker) {
	brokerMu.Lock()
	defer brokerMu.Unlock()
	defaultBroker = broker
}

// GetDefaultBroker returns the default global broker
func GetDefaultBroker() Broker {
	brokerMu.RLock()
	defer brokerMu.RUnlock()
	return defaultBroker
}

// IsInitialized returns true if the default broker is initialized
func IsInitialized() bool {
	return GetDefaultBroker() != nil
}

// Publish publishes an event using the default broker
func Publish(ctx context.Context, event *Event) error {
	broker := GetDefaultBroker()
	if broker == nil {
		return fmt.Errorf("event broker not initialized")
	}
	return broker.Publish(ctx, event)
}

// PublishSync publishes an event synchronously using the default broker
func PublishSync(ctx context.Context, event *Event) error {
	broker := GetDefaultBroker()
	if broker == nil {
		return fmt.Errorf("event broker not initialized")
	}
	return broker.PublishSync(ctx, event)
}

// PublishAsync publishes an event asynchronously using the default broker
func PublishAsync(ctx context.Context, event *Event) error {
	broker := GetDefaultBroker()
	if broker == nil {
		return fmt.Errorf("event broker not initialized")
	}
	return broker.PublishAsync(ctx, event)
}

// Subscribe subscribes to events using the default broker
func Subscribe(pattern string, handler EventHandler) (SubscriptionID, error) {
	broker := GetDefaultBroker()
	if broker == nil {
		return "", fmt.Errorf("event broker not initialized")
	}
	return broker.Subscribe(pattern, handler)
}

// Unsubscribe unsubscribes from events using the default broker
func Unsubscribe(id SubscriptionID) error {
	broker := GetDefaultBroker()
	if broker == nil {
		return fmt.Errorf("event broker not initialized")
	}
	return broker.Unsubscribe(id)
}

// Stats returns statistics from the default broker
func Stats(ctx context.Context) (*BrokerStats, error) {
	broker := GetDefaultBroker()
	if broker == nil {
		return nil, fmt.Errorf("event broker not initialized")
	}
	return broker.Stats(ctx)
}

// RegisterShutdown registers the broker's shutdown with the server shutdown callbacks
func RegisterShutdown(broker Broker) {
	server.RegisterShutdownCallback(func(ctx context.Context) error {
		logger.Info("Shutting down event broker...")
		return broker.Stop(ctx)
	})
}
