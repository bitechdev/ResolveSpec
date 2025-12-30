package mqttspec

import (
	"context"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Client represents an MQTT client connection
type Client struct {
	// ID is the MQTT client ID (unique per connection)
	ID string

	// Username from MQTT CONNECT packet
	Username string

	// ConnectedAt is when the client connected
	ConnectedAt time.Time

	// subscriptions holds active subscriptions for this client
	subscriptions map[string]*Subscription
	subMu         sync.RWMutex

	// metadata stores client-specific data (user_id, roles, tenant_id, etc.)
	// Set by BeforeConnect hook for authentication/authorization
	metadata map[string]interface{}
	metaMu   sync.RWMutex

	// ctx is the client context
	ctx    context.Context
	cancel context.CancelFunc

	// handler reference for callback access
	handler *Handler
}

// ClientManager manages all MQTT client connections
type ClientManager struct {
	// clients maps client_id to Client
	clients map[string]*Client
	mu      sync.RWMutex

	// ctx for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
}

// NewClient creates a new MQTT client
func NewClient(id, username string, handler *Handler) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		ID:            id,
		Username:      username,
		ConnectedAt:   time.Now(),
		subscriptions: make(map[string]*Subscription),
		metadata:      make(map[string]interface{}),
		ctx:           ctx,
		cancel:        cancel,
		handler:       handler,
	}
}

// SetMetadata sets metadata for this client
func (c *Client) SetMetadata(key string, value interface{}) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.metadata[key] = value
}

// GetMetadata retrieves metadata for this client
func (c *Client) GetMetadata(key string) (interface{}, bool) {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	val, ok := c.metadata[key]
	return val, ok
}

// AddSubscription adds a subscription to this client
func (c *Client) AddSubscription(sub *Subscription) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	c.subscriptions[sub.ID] = sub
}

// RemoveSubscription removes a subscription from this client
func (c *Client) RemoveSubscription(subID string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	delete(c.subscriptions, subID)
}

// GetSubscription retrieves a subscription by ID
func (c *Client) GetSubscription(subID string) (*Subscription, bool) {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	sub, ok := c.subscriptions[subID]
	return sub, ok
}

// Close cleans up the client
func (c *Client) Close() {
	if c.cancel != nil {
		c.cancel()
	}

	// Clean up subscriptions
	c.subMu.Lock()
	for subID := range c.subscriptions {
		if c.handler != nil && c.handler.subscriptionManager != nil {
			c.handler.subscriptionManager.Unsubscribe(subID)
		}
	}
	c.subscriptions = make(map[string]*Subscription)
	c.subMu.Unlock()
}

// NewClientManager creates a new client manager
func NewClientManager(ctx context.Context) *ClientManager {
	ctx, cancel := context.WithCancel(ctx)
	return &ClientManager{
		clients: make(map[string]*Client),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Register registers a new MQTT client
func (cm *ClientManager) Register(clientID, username string, handler *Handler) *Client {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	client := NewClient(clientID, username, handler)
	cm.clients[clientID] = client

	count := len(cm.clients)
	logger.Info("[MQTTSpec] Client registered: %s (username: %s, total: %d)", clientID, username, count)

	return client
}

// Unregister removes a client
func (cm *ClientManager) Unregister(clientID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if client, ok := cm.clients[clientID]; ok {
		client.Close()
		delete(cm.clients, clientID)
		count := len(cm.clients)
		logger.Info("[MQTTSpec] Client unregistered: %s (total: %d)", clientID, count)
	}
}

// GetClient retrieves a client by ID
func (cm *ClientManager) GetClient(clientID string) (*Client, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	client, ok := cm.clients[clientID]
	return client, ok
}

// Count returns the number of active clients
func (cm *ClientManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.clients)
}

// Shutdown gracefully shuts down the client manager
func (cm *ClientManager) Shutdown() {
	cm.cancel()

	// Close all clients
	cm.mu.Lock()
	for _, client := range cm.clients {
		client.Close()
	}
	cm.clients = make(map[string]*Client)
	cm.mu.Unlock()

	logger.Info("[MQTTSpec] Client manager shut down")
}
