package websocketspec

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a test connection with proper initialization
func createTestConnection(id string) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connection{
		ID:            id,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		metadata:      make(map[string]interface{}),
		ctx:           ctx,
		cancel:        cancel,
	}
}

func TestNewConnectionManager(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	assert.NotNil(t, cm)
	assert.NotNil(t, cm.connections)
	assert.NotNil(t, cm.register)
	assert.NotNil(t, cm.unregister)
	assert.NotNil(t, cm.broadcast)
	assert.Equal(t, 0, cm.Count())
}

func TestConnectionManager_Count(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer func() {
		// Cancel context without calling Shutdown which tries to close connections
		cm.cancel()
	}()

	// Initially empty
	assert.Equal(t, 0, cm.Count())

	// Add a connection
	conn := createTestConnection("conn-1")

	cm.Register(conn)
	time.Sleep(10 * time.Millisecond) // Give time for registration

	assert.Equal(t, 1, cm.Count())
}

func TestConnectionManager_Register(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	conn := createTestConnection("conn-1")

	cm.Register(conn)
	time.Sleep(10 * time.Millisecond)

	// Verify connection was registered
	retrievedConn, exists := cm.GetConnection("conn-1")
	assert.True(t, exists)
	assert.Equal(t, "conn-1", retrievedConn.ID)
}

func TestConnectionManager_Unregister(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
	}

	cm.Register(conn)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, cm.Count())

	cm.Unregister(conn)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, cm.Count())

	// Verify connection was removed
	_, exists := cm.GetConnection("conn-1")
	assert.False(t, exists)
}

func TestConnectionManager_GetConnection(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	// Non-existent connection
	_, exists := cm.GetConnection("non-existent")
	assert.False(t, exists)

	// Register connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
	}

	cm.Register(conn)
	time.Sleep(10 * time.Millisecond)

	// Get existing connection
	retrievedConn, exists := cm.GetConnection("conn-1")
	assert.True(t, exists)
	assert.Equal(t, "conn-1", retrievedConn.ID)
}

func TestConnectionManager_MultipleConnections(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	// Register multiple connections
	conn1 := &Connection{ID: "conn-1", send: make(chan []byte, 256), subscriptions: make(map[string]*Subscription)}
	conn2 := &Connection{ID: "conn-2", send: make(chan []byte, 256), subscriptions: make(map[string]*Subscription)}
	conn3 := &Connection{ID: "conn-3", send: make(chan []byte, 256), subscriptions: make(map[string]*Subscription)}

	cm.Register(conn1)
	cm.Register(conn2)
	cm.Register(conn3)
	time.Sleep(10 * time.Millisecond)

	assert.Equal(t, 3, cm.Count())

	// Verify all connections exist
	_, exists := cm.GetConnection("conn-1")
	assert.True(t, exists)
	_, exists = cm.GetConnection("conn-2")
	assert.True(t, exists)
	_, exists = cm.GetConnection("conn-3")
	assert.True(t, exists)

	// Unregister one
	cm.Unregister(conn2)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 2, cm.Count())

	// Verify conn-2 is gone but others remain
	_, exists = cm.GetConnection("conn-2")
	assert.False(t, exists)
	_, exists = cm.GetConnection("conn-1")
	assert.True(t, exists)
	_, exists = cm.GetConnection("conn-3")
	assert.True(t, exists)
}

func TestConnectionManager_Shutdown(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()

	// Register connections
	conn1 := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           context.Background(),
	}
	conn1.ctx, conn1.cancel = context.WithCancel(context.Background())

	cm.Register(conn1)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, cm.Count())

	// Shutdown
	cm.Shutdown()
	time.Sleep(10 * time.Millisecond)

	// Verify context was cancelled
	select {
	case <-cm.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context not cancelled after shutdown")
	}
}

func TestConnection_SetMetadata(t *testing.T) {
	conn := &Connection{
		metadata: make(map[string]interface{}),
	}

	conn.SetMetadata("user_id", 123)
	conn.SetMetadata("username", "john")

	// Verify metadata was set
	userID, exists := conn.GetMetadata("user_id")
	assert.True(t, exists)
	assert.Equal(t, 123, userID)

	username, exists := conn.GetMetadata("username")
	assert.True(t, exists)
	assert.Equal(t, "john", username)
}

func TestConnection_GetMetadata(t *testing.T) {
	conn := &Connection{
		metadata: map[string]interface{}{
			"user_id": 123,
			"role":    "admin",
		},
	}

	// Get existing metadata
	userID, exists := conn.GetMetadata("user_id")
	assert.True(t, exists)
	assert.Equal(t, 123, userID)

	// Get non-existent metadata
	_, exists = conn.GetMetadata("non_existent")
	assert.False(t, exists)
}

func TestConnection_AddSubscription(t *testing.T) {
	conn := &Connection{
		subscriptions: make(map[string]*Subscription),
	}

	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "conn-1",
		Entity:       "users",
		Active:       true,
	}

	conn.AddSubscription(sub)

	// Verify subscription was added
	retrievedSub, exists := conn.GetSubscription("sub-1")
	assert.True(t, exists)
	assert.Equal(t, "sub-1", retrievedSub.ID)
}

func TestConnection_RemoveSubscription(t *testing.T) {
	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "conn-1",
		Entity:       "users",
		Active:       true,
	}

	conn := &Connection{
		subscriptions: map[string]*Subscription{
			"sub-1": sub,
		},
	}

	// Verify subscription exists
	_, exists := conn.GetSubscription("sub-1")
	assert.True(t, exists)

	// Remove subscription
	conn.RemoveSubscription("sub-1")

	// Verify subscription was removed
	_, exists = conn.GetSubscription("sub-1")
	assert.False(t, exists)
}

func TestConnection_GetSubscription(t *testing.T) {
	sub1 := &Subscription{ID: "sub-1", Entity: "users"}
	sub2 := &Subscription{ID: "sub-2", Entity: "posts"}

	conn := &Connection{
		subscriptions: map[string]*Subscription{
			"sub-1": sub1,
			"sub-2": sub2,
		},
	}

	// Get existing subscription
	retrievedSub, exists := conn.GetSubscription("sub-1")
	assert.True(t, exists)
	assert.Equal(t, "sub-1", retrievedSub.ID)

	// Get non-existent subscription
	_, exists = conn.GetSubscription("non-existent")
	assert.False(t, exists)
}

func TestConnection_MultipleSubscriptions(t *testing.T) {
	conn := &Connection{
		subscriptions: make(map[string]*Subscription),
	}

	sub1 := &Subscription{ID: "sub-1", Entity: "users"}
	sub2 := &Subscription{ID: "sub-2", Entity: "posts"}
	sub3 := &Subscription{ID: "sub-3", Entity: "comments"}

	conn.AddSubscription(sub1)
	conn.AddSubscription(sub2)
	conn.AddSubscription(sub3)

	// Verify all subscriptions exist
	_, exists := conn.GetSubscription("sub-1")
	assert.True(t, exists)
	_, exists = conn.GetSubscription("sub-2")
	assert.True(t, exists)
	_, exists = conn.GetSubscription("sub-3")
	assert.True(t, exists)

	// Remove one subscription
	conn.RemoveSubscription("sub-2")

	// Verify sub-2 is gone but others remain
	_, exists = conn.GetSubscription("sub-2")
	assert.False(t, exists)
	_, exists = conn.GetSubscription("sub-1")
	assert.True(t, exists)
	_, exists = conn.GetSubscription("sub-3")
	assert.True(t, exists)
}

func TestBroadcastMessage_Structure(t *testing.T) {
	msg := &BroadcastMessage{
		Message: []byte("test message"),
		Filter: func(conn *Connection) bool {
			return true
		},
	}

	assert.NotNil(t, msg.Message)
	assert.NotNil(t, msg.Filter)
	assert.Equal(t, "test message", string(msg.Message))
}

func TestBroadcastMessage_Filter(t *testing.T) {
	// Filter that only allows specific connection
	filter := func(conn *Connection) bool {
		return conn.ID == "conn-1"
	}

	msg := &BroadcastMessage{
		Message: []byte("test"),
		Filter:  filter,
	}

	conn1 := &Connection{ID: "conn-1"}
	conn2 := &Connection{ID: "conn-2"}

	assert.True(t, msg.Filter(conn1))
	assert.False(t, msg.Filter(conn2))
}

func TestConnectionManager_Broadcast(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	// Register connections
	conn1 := &Connection{ID: "conn-1", send: make(chan []byte, 256), subscriptions: make(map[string]*Subscription)}
	conn2 := &Connection{ID: "conn-2", send: make(chan []byte, 256), subscriptions: make(map[string]*Subscription)}

	cm.Register(conn1)
	cm.Register(conn2)
	time.Sleep(10 * time.Millisecond)

	// Broadcast message
	message := []byte("test broadcast")
	cm.Broadcast(message, nil)

	time.Sleep(10 * time.Millisecond)

	// Verify both connections received the message
	select {
	case msg := <-conn1.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("conn1 did not receive message")
	}

	select {
	case msg := <-conn2.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("conn2 did not receive message")
	}
}

func TestConnectionManager_BroadcastWithFilter(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	// Register connections with metadata
	conn1 := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		metadata:      map[string]interface{}{"role": "admin"},
	}
	conn2 := &Connection{
		ID:            "conn-2",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		metadata:      map[string]interface{}{"role": "user"},
	}

	cm.Register(conn1)
	cm.Register(conn2)
	time.Sleep(10 * time.Millisecond)

	// Broadcast only to admins
	filter := func(conn *Connection) bool {
		role, _ := conn.GetMetadata("role")
		return role == "admin"
	}

	message := []byte("admin message")
	cm.Broadcast(message, filter)
	time.Sleep(10 * time.Millisecond)

	// Verify only conn1 received the message
	select {
	case msg := <-conn1.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("conn1 (admin) did not receive message")
	}

	// Verify conn2 did not receive the message
	select {
	case <-conn2.send:
		t.Fatal("conn2 (user) should not have received admin message")
	case <-time.After(50 * time.Millisecond):
		// Expected - no message
	}
}

func TestConnection_ConcurrentMetadataAccess(t *testing.T) {
	// This test verifies that concurrent metadata access doesn't cause race conditions
	// Run with: go test -race

	conn := &Connection{
		metadata: make(map[string]interface{}),
	}

	done := make(chan bool)

	// Goroutine 1: Write metadata
	go func() {
		for i := 0; i < 100; i++ {
			conn.SetMetadata("key", i)
		}
		done <- true
	}()

	// Goroutine 2: Read metadata
	go func() {
		for i := 0; i < 100; i++ {
			conn.GetMetadata("key")
		}
		done <- true
	}()

	// Wait for completion
	<-done
	<-done
}

func TestConnection_ConcurrentSubscriptionAccess(t *testing.T) {
	// This test verifies that concurrent subscription access doesn't cause race conditions
	// Run with: go test -race

	conn := &Connection{
		subscriptions: make(map[string]*Subscription),
	}

	done := make(chan bool)

	// Goroutine 1: Add subscriptions
	go func() {
		for i := 0; i < 100; i++ {
			sub := &Subscription{ID: "sub-" + string(rune(i)), Entity: "users"}
			conn.AddSubscription(sub)
		}
		done <- true
	}()

	// Goroutine 2: Get subscriptions
	go func() {
		for i := 0; i < 100; i++ {
			conn.GetSubscription("sub-" + string(rune(i)))
		}
		done <- true
	}()

	// Wait for completion
	<-done
	<-done
}

func TestConnectionManager_CompleteLifecycle(t *testing.T) {
	ctx := context.Background()
	cm := NewConnectionManager(ctx)

	// Start manager
	go cm.Run()
	defer cm.cancel()

	// Create and register connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		metadata:      make(map[string]interface{}),
	}

	// Set metadata
	conn.SetMetadata("user_id", 123)

	// Add subscriptions
	sub1 := &Subscription{ID: "sub-1", Entity: "users"}
	sub2 := &Subscription{ID: "sub-2", Entity: "posts"}
	conn.AddSubscription(sub1)
	conn.AddSubscription(sub2)

	// Register connection
	cm.Register(conn)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 1, cm.Count())

	// Verify connection exists
	retrievedConn, exists := cm.GetConnection("conn-1")
	require.True(t, exists)
	assert.Equal(t, "conn-1", retrievedConn.ID)

	// Verify metadata
	userID, exists := retrievedConn.GetMetadata("user_id")
	assert.True(t, exists)
	assert.Equal(t, 123, userID)

	// Verify subscriptions
	_, exists = retrievedConn.GetSubscription("sub-1")
	assert.True(t, exists)
	_, exists = retrievedConn.GetSubscription("sub-2")
	assert.True(t, exists)

	// Broadcast message
	message := []byte("test message")
	cm.Broadcast(message, nil)
	time.Sleep(10 * time.Millisecond)

	select {
	case msg := <-retrievedConn.send:
		assert.Equal(t, message, msg)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Connection did not receive broadcast")
	}

	// Unregister connection
	cm.Unregister(conn)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, cm.Count())

	// Verify connection is gone
	_, exists = cm.GetConnection("conn-1")
	assert.False(t, exists)
}
