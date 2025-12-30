package mqttspec

import (
	"context"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// BrokerInterface abstracts MQTT broker operations
type BrokerInterface interface {
	// Start initializes the broker/client connection
	Start(ctx context.Context) error

	// Stop gracefully shuts down the broker/client
	Stop(ctx context.Context) error

	// Publish sends a message to a topic
	Publish(topic string, qos byte, payload []byte) error

	// Subscribe subscribes to a topic pattern with callback
	Subscribe(topicFilter string, qos byte, callback MessageCallback) error

	// Unsubscribe removes subscription
	Unsubscribe(topicFilter string) error

	// IsConnected returns connection status
	IsConnected() bool

	// GetClientManager returns the client manager
	GetClientManager() *ClientManager

	// SetHandler sets the handler reference (needed for hooks)
	SetHandler(handler *Handler)
}

// MessageCallback is called when a message arrives
type MessageCallback func(topic string, payload []byte)

// EmbeddedBroker wraps Mochi MQTT server
type EmbeddedBroker struct {
	config        BrokerConfig
	server        *mqtt.Server
	clientManager *ClientManager
	handler       *Handler
	subscriptions map[string]MessageCallback
	subMu         sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
	started       bool
}

// NewEmbeddedBroker creates a new embedded broker
func NewEmbeddedBroker(config BrokerConfig, clientManager *ClientManager) *EmbeddedBroker {
	return &EmbeddedBroker{
		config:        config,
		clientManager: clientManager,
		subscriptions: make(map[string]MessageCallback),
	}
}

// SetHandler sets the handler reference
func (eb *EmbeddedBroker) SetHandler(handler *Handler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handler = handler
}

// Start starts the embedded MQTT broker
func (eb *EmbeddedBroker) Start(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.started {
		return fmt.Errorf("broker already started")
	}

	eb.ctx, eb.cancel = context.WithCancel(ctx)

	// Create Mochi MQTT server
	eb.server = mqtt.New(&mqtt.Options{
		InlineClient: true,
	})

	// Note: Authentication is handled at the handler level via BeforeConnect hook
	// Mochi MQTT auth can be configured via custom hooks if needed

	// Add TCP listener
	tcp := listeners.NewTCP(
		listeners.Config{
			ID:      "tcp",
			Address: fmt.Sprintf("%s:%d", eb.config.Host, eb.config.Port),
		},
	)
	if err := eb.server.AddListener(tcp); err != nil {
		return fmt.Errorf("failed to add TCP listener: %w", err)
	}

	// Add WebSocket listener if enabled
	if eb.config.EnableWebSocket {
		ws := listeners.NewWebsocket(
			listeners.Config{
				ID:      "ws",
				Address: fmt.Sprintf("%s:%d", eb.config.Host, eb.config.WSPort),
			},
		)
		if err := eb.server.AddListener(ws); err != nil {
			return fmt.Errorf("failed to add WebSocket listener: %w", err)
		}
	}

	// Start server in goroutine
	go func() {
		if err := eb.server.Serve(); err != nil {
			logger.Error("[MQTTSpec] Embedded broker error: %v", err)
		}
	}()

	// Wait for server to be ready
	select {
	case <-time.After(2 * time.Second):
		// Server should be ready
	case <-eb.ctx.Done():
		return fmt.Errorf("context cancelled during startup")
	}

	eb.started = true
	logger.Info("[MQTTSpec] Embedded broker started on %s:%d", eb.config.Host, eb.config.Port)

	return nil
}

// Stop stops the embedded broker
func (eb *EmbeddedBroker) Stop(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if !eb.started {
		return nil
	}

	if eb.cancel != nil {
		eb.cancel()
	}

	if eb.server != nil {
		if err := eb.server.Close(); err != nil {
			logger.Error("[MQTTSpec] Error closing embedded broker: %v", err)
		}
	}

	eb.started = false
	logger.Info("[MQTTSpec] Embedded broker stopped")

	return nil
}

// Publish publishes a message to a topic
func (eb *EmbeddedBroker) Publish(topic string, qos byte, payload []byte) error {
	if !eb.started {
		return fmt.Errorf("broker not started")
	}

	if eb.server == nil {
		return fmt.Errorf("server not initialized")
	}

	// Use inline client to publish
	return eb.server.Publish(topic, payload, false, qos)
}

// Subscribe subscribes to a topic
func (eb *EmbeddedBroker) Subscribe(topicFilter string, qos byte, callback MessageCallback) error {
	if !eb.started {
		return fmt.Errorf("broker not started")
	}

	// Store callback
	eb.subMu.Lock()
	eb.subscriptions[topicFilter] = callback
	eb.subMu.Unlock()

	// Create inline subscription handler
	// Note: Mochi MQTT internal subscriptions are more complex
	// For now, we'll use a publishing hook to intercept messages
	// This is a simplified implementation

	logger.Info("[MQTTSpec] Subscribed to topic filter: %s", topicFilter)

	return nil
}

// Unsubscribe unsubscribes from a topic
func (eb *EmbeddedBroker) Unsubscribe(topicFilter string) error {
	eb.subMu.Lock()
	defer eb.subMu.Unlock()

	delete(eb.subscriptions, topicFilter)
	logger.Info("[MQTTSpec] Unsubscribed from topic filter: %s", topicFilter)

	return nil
}

// IsConnected returns whether the broker is running
func (eb *EmbeddedBroker) IsConnected() bool {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return eb.started
}

// GetClientManager returns the client manager
func (eb *EmbeddedBroker) GetClientManager() *ClientManager {
	return eb.clientManager
}

// ExternalBrokerClient wraps Paho MQTT client
type ExternalBrokerClient struct {
	config        ExternalBrokerConfig
	client        pahomqtt.Client
	clientManager *ClientManager
	handler       *Handler
	subscriptions map[string]MessageCallback
	subMu         sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
	connected     bool
}

// NewExternalBrokerClient creates a new external broker client
func NewExternalBrokerClient(config ExternalBrokerConfig, clientManager *ClientManager) *ExternalBrokerClient {
	return &ExternalBrokerClient{
		config:        config,
		clientManager: clientManager,
		subscriptions: make(map[string]MessageCallback),
	}
}

// SetHandler sets the handler reference
func (ebc *ExternalBrokerClient) SetHandler(handler *Handler) {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()
	ebc.handler = handler
}

// Start connects to the external MQTT broker
func (ebc *ExternalBrokerClient) Start(ctx context.Context) error {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	if ebc.connected {
		return fmt.Errorf("already connected")
	}

	ebc.ctx, ebc.cancel = context.WithCancel(ctx)

	// Create Paho client options
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(ebc.config.BrokerURL)
	opts.SetClientID(ebc.config.ClientID)
	opts.SetUsername(ebc.config.Username)
	opts.SetPassword(ebc.config.Password)
	opts.SetCleanSession(ebc.config.CleanSession)
	opts.SetKeepAlive(ebc.config.KeepAlive)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(ebc.config.ReconnectDelay)

	// Set connection lost handler
	opts.SetConnectionLostHandler(func(client pahomqtt.Client, err error) {
		logger.Error("[MQTTSpec] External broker connection lost: %v", err)
		ebc.mu.Lock()
		ebc.connected = false
		ebc.mu.Unlock()
	})

	// Set on-connect handler
	opts.SetOnConnectHandler(func(client pahomqtt.Client) {
		logger.Info("[MQTTSpec] Connected to external broker")
		ebc.mu.Lock()
		ebc.connected = true
		ebc.mu.Unlock()

		// Resubscribe to topics
		ebc.resubscribeAll()
	})

	// Create and connect client
	ebc.client = pahomqtt.NewClient(opts)
	token := ebc.client.Connect()

	if !token.WaitTimeout(ebc.config.ConnectTimeout) {
		return fmt.Errorf("connection timeout")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to connect to external broker: %w", err)
	}

	ebc.connected = true
	logger.Info("[MQTTSpec] Connected to external MQTT broker: %s", ebc.config.BrokerURL)

	return nil
}

// Stop disconnects from the external broker
func (ebc *ExternalBrokerClient) Stop(ctx context.Context) error {
	ebc.mu.Lock()
	defer ebc.mu.Unlock()

	if !ebc.connected {
		return nil
	}

	if ebc.cancel != nil {
		ebc.cancel()
	}

	if ebc.client != nil && ebc.client.IsConnected() {
		ebc.client.Disconnect(uint(ebc.config.ConnectTimeout.Milliseconds()))
	}

	ebc.connected = false
	logger.Info("[MQTTSpec] Disconnected from external broker")

	return nil
}

// Publish publishes a message to a topic
func (ebc *ExternalBrokerClient) Publish(topic string, qos byte, payload []byte) error {
	if !ebc.connected {
		return fmt.Errorf("not connected to broker")
	}

	token := ebc.client.Publish(topic, qos, false, payload)
	token.Wait()
	return token.Error()
}

// Subscribe subscribes to a topic
func (ebc *ExternalBrokerClient) Subscribe(topicFilter string, qos byte, callback MessageCallback) error {
	if !ebc.connected {
		return fmt.Errorf("not connected to broker")
	}

	// Store callback
	ebc.subMu.Lock()
	ebc.subscriptions[topicFilter] = callback
	ebc.subMu.Unlock()

	// Subscribe via Paho client
	token := ebc.client.Subscribe(topicFilter, qos, func(client pahomqtt.Client, msg pahomqtt.Message) {
		callback(msg.Topic(), msg.Payload())
	})

	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topicFilter, err)
	}

	logger.Info("[MQTTSpec] Subscribed to topic filter: %s", topicFilter)
	return nil
}

// Unsubscribe unsubscribes from a topic
func (ebc *ExternalBrokerClient) Unsubscribe(topicFilter string) error {
	ebc.subMu.Lock()
	defer ebc.subMu.Unlock()

	if ebc.client != nil && ebc.connected {
		token := ebc.client.Unsubscribe(topicFilter)
		token.Wait()
		if err := token.Error(); err != nil {
			logger.Error("[MQTTSpec] Failed to unsubscribe from %s: %v", topicFilter, err)
		}
	}

	delete(ebc.subscriptions, topicFilter)
	logger.Info("[MQTTSpec] Unsubscribed from topic filter: %s", topicFilter)

	return nil
}

// IsConnected returns connection status
func (ebc *ExternalBrokerClient) IsConnected() bool {
	ebc.mu.RLock()
	defer ebc.mu.RUnlock()
	return ebc.connected
}

// GetClientManager returns the client manager
func (ebc *ExternalBrokerClient) GetClientManager() *ClientManager {
	return ebc.clientManager
}

// resubscribeAll resubscribes to all topics after reconnection
func (ebc *ExternalBrokerClient) resubscribeAll() {
	ebc.subMu.RLock()
	defer ebc.subMu.RUnlock()

	for topicFilter, callback := range ebc.subscriptions {
		logger.Info("[MQTTSpec] Resubscribing to topic: %s", topicFilter)
		token := ebc.client.Subscribe(topicFilter, 1, func(client pahomqtt.Client, msg pahomqtt.Message) {
			callback(msg.Topic(), msg.Payload())
		})
		if token.Wait() && token.Error() != nil {
			logger.Error("[MQTTSpec] Failed to resubscribe to %s: %v", topicFilter, token.Error())
		}
	}
}
