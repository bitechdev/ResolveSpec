package mqttspec

import (
	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"

	"gorm.io/gorm"

	"github.com/uptrace/bun"
)

// NewHandlerWithGORM creates an MQTT handler with GORM database adapter
func NewHandlerWithGORM(db *gorm.DB, opts ...Option) (*Handler, error) {
	adapter := database.NewGormAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandlerWithDatabase(adapter, registry, opts...)
}

// NewHandlerWithBun creates an MQTT handler with Bun database adapter
func NewHandlerWithBun(db *bun.DB, opts ...Option) (*Handler, error) {
	adapter := database.NewBunAdapter(db)
	registry := modelregistry.NewModelRegistry()
	return NewHandlerWithDatabase(adapter, registry, opts...)
}

// NewHandlerWithDatabase creates an MQTT handler with a custom database adapter
func NewHandlerWithDatabase(db common.Database, registry common.ModelRegistry, opts ...Option) (*Handler, error) {
	// Start with default configuration
	config := DefaultConfig()

	// Create handler with basic initialization
	// Note: broker and clientManager will be initialized after options are applied
	handler, err := NewHandler(db, registry, config)
	if err != nil {
		return nil, err
	}

	// Apply functional options
	for _, opt := range opts {
		if err := opt(handler); err != nil {
			return nil, err
		}
	}

	// Reinitialize broker based on final config (after options)
	if handler.config.BrokerMode == BrokerModeEmbedded {
		handler.broker = NewEmbeddedBroker(handler.config.Broker, handler.clientManager)
	} else {
		handler.broker = NewExternalBrokerClient(handler.config.ExternalBroker, handler.clientManager)
	}

	// Set handler reference in broker
	handler.broker.SetHandler(handler)

	return handler, nil
}

// Option is a functional option for configuring the handler
type Option func(*Handler) error

// WithEmbeddedBroker configures the handler to use an embedded MQTT broker
func WithEmbeddedBroker(config BrokerConfig) Option {
	return func(h *Handler) error {
		h.config.BrokerMode = BrokerModeEmbedded
		h.config.Broker = config
		return nil
	}
}

// WithExternalBroker configures the handler to connect to an external MQTT broker
func WithExternalBroker(config ExternalBrokerConfig) Option {
	return func(h *Handler) error {
		h.config.BrokerMode = BrokerModeExternal
		h.config.ExternalBroker = config
		return nil
	}
}

// WithHooks sets a pre-configured hook registry
func WithHooks(hooks *HookRegistry) Option {
	return func(h *Handler) error {
		h.hooks = hooks
		return nil
	}
}

// WithTopicPrefix sets a custom topic prefix (default: "spec")
func WithTopicPrefix(prefix string) Option {
	return func(h *Handler) error {
		h.config.Topics.Prefix = prefix
		return nil
	}
}

// WithQoS sets custom QoS levels for different message types
func WithQoS(request, response, notification byte) Option {
	return func(h *Handler) error {
		h.config.QoS.Request = request
		h.config.QoS.Response = response
		h.config.QoS.Notification = notification
		return nil
	}
}
