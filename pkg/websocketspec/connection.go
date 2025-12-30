package websocketspec

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Connection rvepresents a WebSocket connection with its state
type Connection struct {
	// ID is a unique identifier for this connection
	ID string

	// ws is the underlying WebSocket connection
	ws *websocket.Conn

	// send is a channel for outbound messages
	send chan []byte

	// subscriptions holds active subscriptions for this connection
	subscriptions map[string]*Subscription

	// mu protects subscriptions map
	mu sync.RWMutex

	// ctx is the connection context
	ctx context.Context

	// cancel cancels the connection context
	cancel context.CancelFunc

	// handler is the WebSocket handler
	handler *Handler

	// metadata stores connection-specific metadata (e.g., user info, auth state)
	metadata map[string]interface{}

	// metaMu protects metadata map
	metaMu sync.RWMutex

	// closedOnce ensures cleanup happens only once
	closedOnce sync.Once
}

// ConnectionManager manages all active WebSocket connections
type ConnectionManager struct {
	// connections holds all active connections
	connections map[string]*Connection

	// mu protects the connections map
	mu sync.RWMutex

	// register channel for new connections
	register chan *Connection

	// unregister channel for closing connections
	unregister chan *Connection

	// broadcast channel for broadcasting messages
	broadcast chan *BroadcastMessage

	// ctx is the manager context
	ctx context.Context

	// cancel cancels the manager context
	cancel context.CancelFunc
}

// BroadcastMessage represents a message to broadcast to multiple connections
type BroadcastMessage struct {
	// Message is the message to broadcast
	Message []byte

	// Filter is an optional function to filter which connections receive the message
	Filter func(*Connection) bool
}

// NewConnection creates a new WebSocket connection
func NewConnection(id string, ws *websocket.Conn, handler *Handler) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connection{
		ID:            id,
		ws:            ws,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           ctx,
		cancel:        cancel,
		handler:       handler,
		metadata:      make(map[string]interface{}),
	}
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager(ctx context.Context) *ConnectionManager {
	ctx, cancel := context.WithCancel(ctx)
	return &ConnectionManager{
		connections: make(map[string]*Connection),
		register:    make(chan *Connection),
		unregister:  make(chan *Connection),
		broadcast:   make(chan *BroadcastMessage),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Run starts the connection manager event loop
func (cm *ConnectionManager) Run() {
	for {
		select {
		case conn := <-cm.register:
			cm.mu.Lock()
			cm.connections[conn.ID] = conn
			count := len(cm.connections)
			cm.mu.Unlock()
			logger.Info("[WebSocketSpec] Connection registered: %s (total: %d)", conn.ID, count)

		case conn := <-cm.unregister:
			cm.mu.Lock()
			if _, ok := cm.connections[conn.ID]; ok {
				delete(cm.connections, conn.ID)
				close(conn.send)
				count := len(cm.connections)
				cm.mu.Unlock()
				logger.Info("[WebSocketSpec] Connection unregistered: %s (total: %d)", conn.ID, count)
			} else {
				cm.mu.Unlock()
			}

		case msg := <-cm.broadcast:
			cm.mu.RLock()
			for _, conn := range cm.connections {
				if msg.Filter == nil || msg.Filter(conn) {
					select {
					case conn.send <- msg.Message:
					default:
						// Channel full, connection is slow - close it
						logger.Warn("[WebSocketSpec] Connection %s send buffer full, closing", conn.ID)
						cm.mu.RUnlock()
						cm.unregister <- conn
						cm.mu.RLock()
					}
				}
			}
			cm.mu.RUnlock()

		case <-cm.ctx.Done():
			logger.Info("[WebSocketSpec] Connection manager shutting down")
			return
		}
	}
}

// Register registers a new connection
func (cm *ConnectionManager) Register(conn *Connection) {
	cm.register <- conn
}

// Unregister removes a connection
func (cm *ConnectionManager) Unregister(conn *Connection) {
	cm.unregister <- conn
}

// Broadcast sends a message to all connections matching the filter
func (cm *ConnectionManager) Broadcast(message []byte, filter func(*Connection) bool) {
	cm.broadcast <- &BroadcastMessage{
		Message: message,
		Filter:  filter,
	}
}

// Count returns the number of active connections
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.connections)
}

// GetConnection retrieves a connection by ID
func (cm *ConnectionManager) GetConnection(id string) (*Connection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	conn, ok := cm.connections[id]
	return conn, ok
}

// Shutdown gracefully shuts down the connection manager
func (cm *ConnectionManager) Shutdown() {
	cm.cancel()

	// Close all connections
	cm.mu.Lock()
	for _, conn := range cm.connections {
		conn.Close()
	}
	cm.mu.Unlock()
}

// ReadPump reads messages from the WebSocket connection
func (c *Connection) ReadPump() {
	defer func() {
		c.handler.connManager.Unregister(c)
		c.Close()
	}()

	// Configure read parameters
	_ = c.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.ws.SetPongHandler(func(string) error {
		_ = c.ws.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("[WebSocketSpec] Connection %s read error: %v", c.ID, err)
			}
			break
		}

		// Parse and handle the message
		c.handleMessage(message)
	}
}

// WritePump writes messages to the WebSocket connection
func (c *Connection) WritePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Channel closed
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Write any queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// Send sends a message to this connection
func (c *Connection) Send(message []byte) error {
	select {
	case c.send <- message:
		return nil
	case <-c.ctx.Done():
		return fmt.Errorf("connection closed")
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendJSON sends a JSON-encoded message to this connection
func (c *Connection) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	return c.Send(data)
}

// Close closes the connection
func (c *Connection) Close() {
	c.closedOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.ws != nil {
			c.ws.Close()
		}

		// Clean up subscriptions
		c.mu.Lock()
		for subID := range c.subscriptions {
			if c.handler != nil && c.handler.subscriptionManager != nil {
				c.handler.subscriptionManager.Unsubscribe(subID)
			}
		}
		c.subscriptions = make(map[string]*Subscription)
		c.mu.Unlock()

		logger.Info("[WebSocketSpec] Connection %s closed", c.ID)
	})
}

// AddSubscription adds a subscription to this connection
func (c *Connection) AddSubscription(sub *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscriptions[sub.ID] = sub
}

// RemoveSubscription removes a subscription from this connection
func (c *Connection) RemoveSubscription(subID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subscriptions, subID)
}

// GetSubscription retrieves a subscription by ID
func (c *Connection) GetSubscription(subID string) (*Subscription, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sub, ok := c.subscriptions[subID]
	return sub, ok
}

// SetMetadata sets metadata for this connection
func (c *Connection) SetMetadata(key string, value interface{}) {
	c.metaMu.Lock()
	defer c.metaMu.Unlock()
	c.metadata[key] = value
}

// GetMetadata retrieves metadata for this connection
func (c *Connection) GetMetadata(key string) (interface{}, bool) {
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	val, ok := c.metadata[key]
	return val, ok
}

// handleMessage processes an incoming message
func (c *Connection) handleMessage(data []byte) {
	msg, err := ParseMessage(data)
	if err != nil {
		logger.Error("[WebSocketSpec] Failed to parse message: %v", err)
		errResp := NewErrorResponse("", "invalid_message", "Failed to parse message")
		_ = c.SendJSON(errResp)
		return
	}

	if !msg.IsValid() {
		logger.Error("[WebSocketSpec] Invalid message received")
		errResp := NewErrorResponse(msg.ID, "invalid_message", "Message validation failed")
		_ = c.SendJSON(errResp)
		return
	}

	// Route message to appropriate handler
	c.handler.HandleMessage(c, msg)
}
