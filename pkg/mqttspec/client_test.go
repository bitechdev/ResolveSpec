package mqttspec

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	client := NewClient("client-123", "user@example.com", nil)

	assert.Equal(t, "client-123", client.ID)
	assert.Equal(t, "user@example.com", client.Username)
	assert.NotNil(t, client.subscriptions)
	assert.NotNil(t, client.metadata)
	assert.NotNil(t, client.ctx)
	assert.NotNil(t, client.cancel)
}

func TestClient_Metadata(t *testing.T) {
	client := NewClient("client-123", "user", nil)

	// Set metadata
	client.SetMetadata("user_id", 456)
	client.SetMetadata("tenant_id", "tenant-abc")
	client.SetMetadata("roles", []string{"admin", "user"})

	// Get metadata
	userID, exists := client.GetMetadata("user_id")
	assert.True(t, exists)
	assert.Equal(t, 456, userID)

	tenantID, exists := client.GetMetadata("tenant_id")
	assert.True(t, exists)
	assert.Equal(t, "tenant-abc", tenantID)

	roles, exists := client.GetMetadata("roles")
	assert.True(t, exists)
	assert.Equal(t, []string{"admin", "user"}, roles)

	// Non-existent key
	_, exists = client.GetMetadata("nonexistent")
	assert.False(t, exists)
}

func TestClient_Subscriptions(t *testing.T) {
	client := NewClient("client-123", "user", nil)

	// Create mock subscription
	sub := &Subscription{
		ID:           "sub-1",
		ConnectionID: "client-123",
		Schema:       "public",
		Entity:       "users",
		Active:       true,
	}

	// Add subscription
	client.AddSubscription(sub)

	// Get subscription
	retrieved, exists := client.GetSubscription("sub-1")
	assert.True(t, exists)
	assert.Equal(t, "sub-1", retrieved.ID)

	// Remove subscription
	client.RemoveSubscription("sub-1")

	// Verify removed
	_, exists = client.GetSubscription("sub-1")
	assert.False(t, exists)
}

func TestClient_Close(t *testing.T) {
	client := NewClient("client-123", "user", nil)

	// Add some subscriptions
	client.AddSubscription(&Subscription{ID: "sub-1"})
	client.AddSubscription(&Subscription{ID: "sub-2"})

	// Close client
	client.Close()

	// Verify subscriptions cleared
	client.subMu.RLock()
	assert.Empty(t, client.subscriptions)
	client.subMu.RUnlock()

	// Verify context cancelled
	select {
	case <-client.ctx.Done():
		// Context was cancelled
	default:
		t.Fatal("Context should be cancelled after Close()")
	}
}

func TestNewClientManager(t *testing.T) {
	cm := NewClientManager(context.Background())

	assert.NotNil(t, cm)
	assert.NotNil(t, cm.clients)
	assert.Equal(t, 0, cm.Count())
}

func TestClientManager_Register(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	client := cm.Register("client-1", "user@example.com", nil)

	assert.NotNil(t, client)
	assert.Equal(t, "client-1", client.ID)
	assert.Equal(t, "user@example.com", client.Username)
	assert.Equal(t, 1, cm.Count())
}

func TestClientManager_Unregister(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	cm.Register("client-1", "user1", nil)
	assert.Equal(t, 1, cm.Count())

	cm.Unregister("client-1")
	assert.Equal(t, 0, cm.Count())
}

func TestClientManager_GetClient(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	cm.Register("client-1", "user1", nil)

	// Get existing client
	client, exists := cm.GetClient("client-1")
	assert.True(t, exists)
	assert.NotNil(t, client)
	assert.Equal(t, "client-1", client.ID)

	// Get non-existent client
	_, exists = cm.GetClient("nonexistent")
	assert.False(t, exists)
}

func TestClientManager_MultipleClients(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	cm.Register("client-1", "user1", nil)
	cm.Register("client-2", "user2", nil)
	cm.Register("client-3", "user3", nil)

	assert.Equal(t, 3, cm.Count())

	cm.Unregister("client-2")
	assert.Equal(t, 2, cm.Count())

	// Verify correct client was removed
	_, exists := cm.GetClient("client-2")
	assert.False(t, exists)

	_, exists = cm.GetClient("client-1")
	assert.True(t, exists)

	_, exists = cm.GetClient("client-3")
	assert.True(t, exists)
}

func TestClientManager_Shutdown(t *testing.T) {
	cm := NewClientManager(context.Background())

	cm.Register("client-1", "user1", nil)
	cm.Register("client-2", "user2", nil)
	assert.Equal(t, 2, cm.Count())

	cm.Shutdown()

	// All clients should be removed
	assert.Equal(t, 0, cm.Count())

	// Context should be cancelled
	select {
	case <-cm.ctx.Done():
		// Context was cancelled
	default:
		t.Fatal("Context should be cancelled after Shutdown()")
	}
}

func TestClientManager_ConcurrentOperations(t *testing.T) {
	cm := NewClientManager(context.Background())
	defer cm.Shutdown()

	// This test verifies that concurrent operations don't cause race conditions
	// Run with: go test -race

	var wg sync.WaitGroup

	// Goroutine 1: Register clients
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cm.Register("client-"+string(rune(i)), "user", nil)
		}
	}()

	// Goroutine 2: Get clients
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cm.GetClient("client-" + string(rune(i)))
		}
	}()

	// Goroutine 3: Count
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cm.Count()
		}
	}()

	wg.Wait()
}

func TestClient_ConcurrentMetadata(t *testing.T) {
	client := NewClient("client-123", "user", nil)

	var wg sync.WaitGroup

	// Concurrent writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			client.SetMetadata("key1", i)
		}
	}()

	// Concurrent reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			client.GetMetadata("key1")
		}
	}()

	wg.Wait()
}
