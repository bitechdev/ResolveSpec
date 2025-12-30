package mqttspec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/common/adapters/database"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Test model
type TestUser struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	TenantID  string `json:"tenant_id"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (TestUser) TableName() string {
	return "users"
}

// setupTestHandler creates a handler with in-memory SQLite database
func setupTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	// Create in-memory SQLite database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate test model
	err = db.AutoMigrate(&TestUser{})
	require.NoError(t, err)

	// Create handler
	config := DefaultConfig()
	config.Broker.Port = 21883 // Use different port for handler tests

	adapter := database.NewGormAdapter(db)
	registry := modelregistry.NewModelRegistry()
	registry.RegisterModel("public.users", &TestUser{})

	handler, err := NewHandlerWithDatabase(adapter, registry, WithEmbeddedBroker(config.Broker))
	require.NoError(t, err)

	return handler, db
}

func TestNewHandler(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.db)
	assert.NotNil(t, handler.registry)
	assert.NotNil(t, handler.hooks)
	assert.NotNil(t, handler.clientManager)
	assert.NotNil(t, handler.subscriptionManager)
	assert.NotNil(t, handler.broker)
	assert.NotNil(t, handler.config)
}

func TestHandler_StartShutdown(t *testing.T) {
	handler, _ := setupTestHandler(t)

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	assert.True(t, handler.started)

	// Shutdown handler
	err = handler.Shutdown()
	require.NoError(t, err)
	assert.False(t, handler.started)
}

func TestHandler_HandleRead_Single(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Insert test data
	user := &TestUser{
		ID:    1,
		Name:  "John Doe",
		Email: "john@example.com",
		Status: "active",
	}
	db.Create(user)

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create read request message
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Schema:    "public",
		Entity:    "users",
		Options:   &common.RequestOptions{},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		ID:       "1",
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle read
	handler.handleRead(client, msg, hookCtx)

	// Note: In a full integration test, we would verify the response was published
	// to the correct MQTT topic. Here we're just testing that the handler doesn't error.
}

func TestHandler_HandleRead_Multiple(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Insert test data
	users := []TestUser{
		{ID: 1, Name: "User 1", Email: "user1@example.com", Status: "active"},
		{ID: 2, Name: "User 2", Email: "user2@example.com", Status: "active"},
		{ID: 3, Name: "User 3", Email: "user3@example.com", Status: "inactive"},
	}
	for _, user := range users {
		db.Create(&user)
	}

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create read request with filter
	msg := &Message{
		ID:        "msg-2",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Schema:    "public",
		Entity:    "users",
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{
				{Column: "status", Operator: "eq", Value: "active"},
			},
		},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle read
	handler.handleRead(client, msg, hookCtx)
}

func TestHandler_HandleCreate(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler to initialize broker
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create request data
	newUser := map[string]interface{}{
		"name":   "New User",
		"email":  "new@example.com",
		"status": "active",
	}

	// Create create request message
	msg := &Message{
		ID:        "msg-3",
		Type:      MessageTypeRequest,
		Operation: OperationCreate,
		Schema:    "public",
		Entity:    "users",
		Data:      newUser,
		Options:   &common.RequestOptions{},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		Data:     newUser,
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle create
	handler.handleCreate(client, msg, hookCtx)

	// Verify user was created in database
	var user TestUser
	result := db.Where("email = ?", "new@example.com").First(&user)
	assert.NoError(t, result.Error)
	assert.Equal(t, "New User", user.Name)
}

func TestHandler_HandleUpdate(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Insert test data
	user := &TestUser{
		ID:     1,
		Name:   "Original Name",
		Email:  "original@example.com",
		Status: "active",
	}
	db.Create(user)

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Update data
	updateData := map[string]interface{}{
		"name": "Updated Name",
	}

	// Create update request message
	msg := &Message{
		ID:        "msg-4",
		Type:      MessageTypeRequest,
		Operation: OperationUpdate,
		Schema:    "public",
		Entity:    "users",
		Data:      updateData,
		Options:   &common.RequestOptions{},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		ID:       "1",
		Data:     updateData,
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle update
	handler.handleUpdate(client, msg, hookCtx)

	// Verify user was updated
	var updatedUser TestUser
	db.First(&updatedUser, 1)
	assert.Equal(t, "Updated Name", updatedUser.Name)
}

func TestHandler_HandleDelete(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Insert test data
	user := &TestUser{
		ID:     1,
		Name:   "To Delete",
		Email:  "delete@example.com",
		Status: "active",
	}
	db.Create(user)

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create delete request message
	msg := &Message{
		ID:        "msg-5",
		Type:      MessageTypeRequest,
		Operation: OperationDelete,
		Schema:    "public",
		Entity:    "users",
		Options:   &common.RequestOptions{},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		ID:       "1",
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle delete
	handler.handleDelete(client, msg, hookCtx)

	// Verify user was deleted
	var deletedUser TestUser
	result := db.First(&deletedUser, 1)
	assert.Error(t, result.Error)
	assert.Equal(t, gorm.ErrRecordNotFound, result.Error)
}

func TestHandler_HandleSubscribe(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create subscribe message
	msg := &Message{
		ID:        "msg-6",
		Type:      MessageTypeSubscription,
		Operation: OperationSubscribe,
		Schema:    "public",
		Entity:    "users",
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{
				{Column: "status", Operator: "eq", Value: "active"},
			},
		},
	}

	// Handle subscribe
	handler.handleSubscribe(client, msg)

	// Verify subscription was created
	subscriptions := handler.subscriptionManager.GetSubscriptionsByEntity("public", "users")
	assert.Len(t, subscriptions, 1)
	assert.Equal(t, client.ID, subscriptions[0].ConnectionID)
}

func TestHandler_HandleUnsubscribe(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create subscription using Subscribe method
	sub := handler.subscriptionManager.Subscribe("sub-1", client.ID, "public", "users", &common.RequestOptions{})
	client.AddSubscription(sub)

	// Create unsubscribe message with subscription ID in Data
	msg := &Message{
		ID:        "msg-7",
		Type:      MessageTypeSubscription,
		Operation: OperationUnsubscribe,
		Data:      map[string]interface{}{"subscription_id": "sub-1"},
		Options:   &common.RequestOptions{},
	}

	// Handle unsubscribe
	handler.handleUnsubscribe(client, msg)

	// Verify subscription was removed
	_, exists := handler.subscriptionManager.GetSubscription("sub-1")
	assert.False(t, exists)
}

func TestHandler_NotifySubscribers(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create mock clients
	client1 := handler.clientManager.Register("client-1", "user1", handler)
	client2 := handler.clientManager.Register("client-2", "user2", handler)

	// Create subscriptions
	opts1 := &common.RequestOptions{
		Filters: []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "active"},
		},
	}
	sub1 := handler.subscriptionManager.Subscribe("sub-1", client1.ID, "public", "users", opts1)
	client1.AddSubscription(sub1)

	opts2 := &common.RequestOptions{
		Filters: []common.FilterOption{
			{Column: "status", Operator: "eq", Value: "inactive"},
		},
	}
	sub2 := handler.subscriptionManager.Subscribe("sub-2", client2.ID, "public", "users", opts2)
	client2.AddSubscription(sub2)

	// Notify subscribers with active user
	activeUser := map[string]interface{}{
		"id":     1,
		"name":   "Active User",
		"status": "active",
	}

	// This should notify sub-1 only
	handler.notifySubscribers("public", "users", OperationCreate, activeUser)

	// Note: In a full integration test, we would verify that the notification
	// was published to the correct MQTT topic. Here we're just testing that
	// the handler doesn't error and finds the correct subscriptions.
}

func TestHandler_Hooks_BeforeRead(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Insert test data with different tenants
	users := []TestUser{
		{ID: 1, Name: "User 1", TenantID: "tenant-a", Status: "active"},
		{ID: 2, Name: "User 2", TenantID: "tenant-b", Status: "active"},
		{ID: 3, Name: "User 3", TenantID: "tenant-a", Status: "active"},
	}
	for _, user := range users {
		db.Create(&user)
	}

	// Register hook to filter by tenant
	handler.Hooks().Register(BeforeRead, func(ctx *HookContext) error {
		// Auto-inject tenant filter
		ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
			Column:   "tenant_id",
			Operator: "eq",
			Value:    "tenant-a",
		})
		return nil
	})

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create read request (no tenant filter)
	msg := &Message{
		ID:        "msg-8",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Schema:    "public",
		Entity:    "users",
		Options:   &common.RequestOptions{},
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle read
	handler.handleRead(client, msg, hookCtx)

	// The hook should have injected the tenant filter
	// In a full test, we would verify only tenant-a users were returned
}

func TestHandler_Hooks_BeforeCreate(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Register hook to set default values
	handler.Hooks().Register(BeforeCreate, func(ctx *HookContext) error {
		// Auto-set tenant_id
		if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
			dataMap["tenant_id"] = "auto-tenant"
		}
		return nil
	})

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create mock client
	client := NewClient("test-client", "test-user", handler)

	// Create user without tenant_id
	newUser := map[string]interface{}{
		"name":   "Test User",
		"email":  "test@example.com",
		"status": "active",
	}

	msg := &Message{
		ID:        "msg-9",
		Type:      MessageTypeRequest,
		Operation: OperationCreate,
		Schema:    "public",
		Entity:    "users",
		Data:      newUser,
		Options:   &common.RequestOptions{},
	}

	hookCtx := &HookContext{
		Context:  context.Background(),
		Handler:  nil,
		Schema:   "public",
		Entity:   "users",
		Data:     newUser,
		Options:  msg.Options,
		Metadata: map[string]interface{}{"mqtt_client": client},
	}

	// Handle create
	handler.handleCreate(client, msg, hookCtx)

	// Verify tenant_id was auto-set
	var user TestUser
	db.Where("email = ?", "test@example.com").First(&user)
	assert.Equal(t, "auto-tenant", user.TenantID)
}

func TestHandler_ConcurrentRequests(t *testing.T) {
	handler, db := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Create multiple clients
	var wg sync.WaitGroup
	numClients := 10

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client := NewClient(fmt.Sprintf("client-%d", id), fmt.Sprintf("user%d", id), handler)

			// Create user
			newUser := map[string]interface{}{
				"name":   fmt.Sprintf("User %d", id),
				"email":  fmt.Sprintf("user%d@example.com", id),
				"status": "active",
			}

			msg := &Message{
				ID:        fmt.Sprintf("msg-%d", id),
				Type:      MessageTypeRequest,
				Operation: OperationCreate,
				Schema:    "public",
				Entity:    "users",
				Data:      newUser,
				Options:   &common.RequestOptions{},
			}

			hookCtx := &HookContext{
				Context:  context.Background(),
				Handler:  nil,
				Schema:   "public",
				Entity:   "users",
				Data:     newUser,
				Options:  msg.Options,
				Metadata: map[string]interface{}{"mqtt_client": client},
			}

			handler.handleCreate(client, msg, hookCtx)
		}(i)
	}

	wg.Wait()

	// Verify all users were created
	var count int64
	db.Model(&TestUser{}).Count(&count)
	assert.Equal(t, int64(numClients), count)
}

func TestHandler_TopicHelpers(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	clientID := "test-client"
	subscriptionID := "sub-123"

	requestTopic := handler.getRequestTopic(clientID)
	assert.Equal(t, "spec/test-client/request", requestTopic)

	responseTopic := handler.getResponseTopic(clientID)
	assert.Equal(t, "spec/test-client/response", responseTopic)

	notifyTopic := handler.getNotifyTopic(clientID, subscriptionID)
	assert.Equal(t, "spec/test-client/notify/sub-123", notifyTopic)
}

func TestHandler_SendResponse(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Test data
	clientID := "test-client"
	msgID := "msg-123"
	data := map[string]interface{}{"id": 1, "name": "Test"}
	metadata := map[string]interface{}{"total": 1}

	// Send response (should not error)
	handler.sendResponse(clientID, msgID, data, metadata)
}

func TestHandler_SendError(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Test error
	clientID := "test-client"
	msgID := "msg-123"
	code := "test_error"
	message := "Test error message"

	// Send error (should not error)
	handler.sendError(clientID, msgID, code, message)
}

// extractClientID extracts the client ID from a topic like spec/{client_id}/request
func extractClientID(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return ""
}

func TestHandler_ExtractClientID(t *testing.T) {
	tests := []struct {
		topic    string
		expected string
	}{
		{"spec/client-123/request", "client-123"},
		{"spec/abc-xyz/request", "abc-xyz"},
		{"spec/test/request", "test"},
	}

	for _, tt := range tests {
		result := extractClientID(tt.topic)
		assert.Equal(t, tt.expected, result, "topic: %s", tt.topic)
	}
}

func TestHandler_HandleIncomingMessage_InvalidJSON(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Invalid JSON payload
	payload := []byte("{invalid json")

	// Should not panic
	handler.handleIncomingMessage("spec/test-client/request", payload)
}

func TestHandler_HandleIncomingMessage_ValidMessage(t *testing.T) {
	handler, _ := setupTestHandler(t)
	defer handler.Shutdown()

	// Start handler
	err := handler.Start()
	require.NoError(t, err)
	defer handler.Shutdown()

	// Valid message
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Schema:    "public",
		Entity:    "users",
		Options:   &common.RequestOptions{},
	}

	payload, _ := json.Marshal(msg)

	// Should not panic or error
	handler.handleIncomingMessage("spec/test-client/request", payload)
}
