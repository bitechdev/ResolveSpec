package websocketspec

import (
	"context"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockDatabase is a mock implementation of common.Database for testing
type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) NewSelect() common.SelectQuery {
	args := m.Called()
	return args.Get(0).(common.SelectQuery)
}

func (m *MockDatabase) NewInsert() common.InsertQuery {
	args := m.Called()
	return args.Get(0).(common.InsertQuery)
}

func (m *MockDatabase) NewUpdate() common.UpdateQuery {
	args := m.Called()
	return args.Get(0).(common.UpdateQuery)
}

func (m *MockDatabase) NewDelete() common.DeleteQuery {
	args := m.Called()
	return args.Get(0).(common.DeleteQuery)
}

func (m *MockDatabase) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDatabase) Exec(ctx context.Context, query string, args ...interface{}) (common.Result, error) {
	callArgs := m.Called(ctx, query, args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(common.Result), callArgs.Error(1)
}

func (m *MockDatabase) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	callArgs := m.Called(ctx, dest, query, args)
	return callArgs.Error(0)
}

func (m *MockDatabase) BeginTx(ctx context.Context) (common.Database, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(common.Database), args.Error(1)
}

func (m *MockDatabase) CommitTx(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockDatabase) RollbackTx(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockDatabase) RunInTransaction(ctx context.Context, fn func(common.Database) error) error {
	args := m.Called(ctx, fn)
	return args.Error(0)
}

func (m *MockDatabase) GetUnderlyingDB() interface{} {
	args := m.Called()
	return args.Get(0)
}

// MockSelectQuery is a mock implementation of common.SelectQuery
type MockSelectQuery struct {
	mock.Mock
}

func (m *MockSelectQuery) Model(model interface{}) common.SelectQuery {
	args := m.Called(model)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Table(table string) common.SelectQuery {
	args := m.Called(table)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Column(columns ...string) common.SelectQuery {
	args := m.Called(columns)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Where(query string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) WhereIn(column string, values interface{}) common.SelectQuery {
	args := m.Called(column, values)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Order(order string) common.SelectQuery {
	args := m.Called(order)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Limit(limit int) common.SelectQuery {
	args := m.Called(limit)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Offset(offset int) common.SelectQuery {
	args := m.Called(offset)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	args := m.Called(relation, apply)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
	args := m.Called(relation, conditions)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) ColumnExpr(query string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) WhereOr(query string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Join(query string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) LeftJoin(query string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) JoinRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	args := m.Called(relation, apply)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) OrderExpr(order string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(order, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Group(group string) common.SelectQuery {
	args := m.Called(group)
	return args.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Having(having string, args ...interface{}) common.SelectQuery {
	callArgs := m.Called(having, args)
	return callArgs.Get(0).(common.SelectQuery)
}

func (m *MockSelectQuery) Scan(ctx context.Context, dest interface{}) error {
	args := m.Called(ctx, dest)
	return args.Error(0)
}

func (m *MockSelectQuery) ScanModel(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSelectQuery) Count(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockSelectQuery) Exists(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

// MockInsertQuery is a mock implementation of common.InsertQuery
type MockInsertQuery struct {
	mock.Mock
}

func (m *MockInsertQuery) Model(model interface{}) common.InsertQuery {
	args := m.Called(model)
	return args.Get(0).(common.InsertQuery)
}

func (m *MockInsertQuery) Table(table string) common.InsertQuery {
	args := m.Called(table)
	return args.Get(0).(common.InsertQuery)
}

func (m *MockInsertQuery) Value(column string, value interface{}) common.InsertQuery {
	args := m.Called(column, value)
	return args.Get(0).(common.InsertQuery)
}

func (m *MockInsertQuery) OnConflict(action string) common.InsertQuery {
	args := m.Called(action)
	return args.Get(0).(common.InsertQuery)
}

func (m *MockInsertQuery) Returning(columns ...string) common.InsertQuery {
	args := m.Called(columns)
	return args.Get(0).(common.InsertQuery)
}

func (m *MockInsertQuery) Exec(ctx context.Context) (common.Result, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(common.Result), args.Error(1)
}

// MockUpdateQuery is a mock implementation of common.UpdateQuery
type MockUpdateQuery struct {
	mock.Mock
}

func (m *MockUpdateQuery) Model(model interface{}) common.UpdateQuery {
	args := m.Called(model)
	return args.Get(0).(common.UpdateQuery)
}

func (m *MockUpdateQuery) Table(table string) common.UpdateQuery {
	args := m.Called(table)
	return args.Get(0).(common.UpdateQuery)
}

func (m *MockUpdateQuery) Set(column string, value interface{}) common.UpdateQuery {
	args := m.Called(column, value)
	return args.Get(0).(common.UpdateQuery)
}

func (m *MockUpdateQuery) SetMap(values map[string]interface{}) common.UpdateQuery {
	args := m.Called(values)
	return args.Get(0).(common.UpdateQuery)
}

func (m *MockUpdateQuery) Where(query string, args ...interface{}) common.UpdateQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.UpdateQuery)
}

func (m *MockUpdateQuery) Exec(ctx context.Context) (common.Result, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(common.Result), args.Error(1)
}

// MockDeleteQuery is a mock implementation of common.DeleteQuery
type MockDeleteQuery struct {
	mock.Mock
}

func (m *MockDeleteQuery) Model(model interface{}) common.DeleteQuery {
	args := m.Called(model)
	return args.Get(0).(common.DeleteQuery)
}

func (m *MockDeleteQuery) Table(table string) common.DeleteQuery {
	args := m.Called(table)
	return args.Get(0).(common.DeleteQuery)
}

func (m *MockDeleteQuery) Where(query string, args ...interface{}) common.DeleteQuery {
	callArgs := m.Called(query, args)
	return callArgs.Get(0).(common.DeleteQuery)
}

func (m *MockDeleteQuery) Exec(ctx context.Context) (common.Result, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(common.Result), args.Error(1)
}

// MockModelRegistry is a mock implementation of common.ModelRegistry
type MockModelRegistry struct {
	mock.Mock
}

func (m *MockModelRegistry) RegisterModel(key string, model interface{}) error {
	args := m.Called(key, model)
	return args.Error(0)
}

func (m *MockModelRegistry) GetModel(key string) (interface{}, error) {
	args := m.Called(key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockModelRegistry) GetAllModels() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockModelRegistry) GetModelByEntity(schema, entity string) (interface{}, error) {
	args := m.Called(schema, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

// Test model
type TestUser struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

func TestNewHandler(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}

	handler := NewHandler(mockDB, mockRegistry)

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.db)
	assert.NotNil(t, handler.registry)
	assert.NotNil(t, handler.hooks)
	assert.NotNil(t, handler.connManager)
	assert.NotNil(t, handler.subscriptionManager)
	assert.NotNil(t, handler.upgrader)
}

func TestHandler_Hooks(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	hooks := handler.Hooks()
	assert.NotNil(t, hooks)
	assert.IsType(t, &HookRegistry{}, hooks)
}

func TestHandler_Registry(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	registry := handler.Registry()
	assert.NotNil(t, registry)
	assert.Equal(t, mockRegistry, registry)
}

func TestHandler_GetDatabase(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	db := handler.GetDatabase()
	assert.NotNil(t, db)
	assert.Equal(t, mockDB, db)
}

func TestHandler_GetConnectionCount(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	count := handler.GetConnectionCount()
	assert.Equal(t, 0, count)
}

func TestHandler_GetSubscriptionCount(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	count := handler.GetSubscriptionCount()
	assert.Equal(t, 0, count)
}

func TestHandler_GetConnection(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Non-existent connection
	_, exists := handler.GetConnection("non-existent")
	assert.False(t, exists)
}

func TestHandler_HandleMessage_InvalidType(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           context.Background(),
	}

	msg := &Message{
		ID:   "msg-1",
		Type: MessageType("invalid"),
	}

	handler.HandleMessage(conn, msg)

	// Should send error response
	select {
	case data := <-conn.send:
		var response map[string]interface{}
		require.NoError(t, ParseMessageBytes(data, &response))
		assert.False(t, response["success"].(bool))
	default:
		t.Fatal("Expected error response")
	}
}

func ParseMessageBytes(data []byte, v interface{}) error {
	return nil // Simplified for testing
}

func TestHandler_GetOperatorSQL(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	tests := []struct {
		operator string
		expected string
	}{
		{"eq", "="},
		{"neq", "!="},
		{"gt", ">"},
		{"gte", ">="},
		{"lt", "<"},
		{"lte", "<="},
		{"like", "LIKE"},
		{"ilike", "ILIKE"},
		{"in", "IN"},
		{"unknown", "="}, // default
	}

	for _, tt := range tests {
		t.Run(tt.operator, func(t *testing.T) {
			result := handler.getOperatorSQL(tt.operator)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandler_GetTableName(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	tests := []struct {
		name     string
		schema   string
		entity   string
		expected string
	}{
		{
			name:     "With schema",
			schema:   "public",
			entity:   "users",
			expected: "public.users",
		},
		{
			name:     "Without schema",
			schema:   "",
			entity:   "users",
			expected: "users",
		},
		{
			name:     "Different schema",
			schema:   "custom",
			entity:   "posts",
			expected: "custom.posts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.getTableName(tt.schema, tt.entity, &TestUser{})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandler_GetMetadata(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	metadata := handler.getMetadata("public", "users", &TestUser{})

	assert.NotNil(t, metadata)
	assert.Equal(t, "public", metadata["schema"])
	assert.Equal(t, "users", metadata["entity"])
	assert.Equal(t, "public.users", metadata["table_name"])
	assert.NotNil(t, metadata["columns"])
	assert.NotNil(t, metadata["primary_key"])
}

func TestHandler_NotifySubscribers(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Create connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		handler:       handler,
	}

	// Register connection
	handler.connManager.connections["conn-1"] = conn

	// Create subscription
	sub := handler.subscriptionManager.Subscribe("sub-1", "conn-1", "public", "users", nil)
	conn.AddSubscription(sub)

	// Notify subscribers
	data := map[string]interface{}{"id": 1, "name": "John"}
	handler.notifySubscribers("public", "users", OperationCreate, data)

	// Verify notification was sent
	select {
	case msg := <-conn.send:
		assert.NotEmpty(t, msg)
	default:
		t.Fatal("Expected notification to be sent")
	}
}

func TestHandler_NotifySubscribers_NoSubscribers(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Notify with no subscribers - should not panic
	data := map[string]interface{}{"id": 1, "name": "John"}
	handler.notifySubscribers("public", "users", OperationCreate, data)

	// No assertions needed - just checking it doesn't panic
}

func TestHandler_NotifySubscribers_ConnectionNotFound(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Create subscription without connection
	handler.subscriptionManager.Subscribe("sub-1", "conn-1", "public", "users", nil)

	// Notify - should handle gracefully when connection not found
	data := map[string]interface{}{"id": 1, "name": "John"}
	handler.notifySubscribers("public", "users", OperationCreate, data)

	// No assertions needed - just checking it doesn't panic
}

func TestHandler_HooksIntegration(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	beforeCalled := false
	afterCalled := false

	// Register hooks
	handler.Hooks().RegisterBefore(OperationCreate, func(ctx *HookContext) error {
		beforeCalled = true
		return nil
	})

	handler.Hooks().RegisterAfter(OperationCreate, func(ctx *HookContext) error {
		afterCalled = true
		return nil
	})

	// Verify hooks are registered
	assert.True(t, handler.Hooks().HasHooks(BeforeCreate))
	assert.True(t, handler.Hooks().HasHooks(AfterCreate))

	// Execute hooks
	ctx := &HookContext{Context: context.Background()}
	handler.Hooks().Execute(BeforeCreate, ctx)
	handler.Hooks().Execute(AfterCreate, ctx)

	assert.True(t, beforeCalled)
	assert.True(t, afterCalled)
}

func TestHandler_Shutdown(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Shutdown should not panic
	handler.Shutdown()

	// Verify context was cancelled
	select {
	case <-handler.connManager.ctx.Done():
		// Expected
	default:
		t.Fatal("Connection manager context not cancelled after shutdown")
	}
}

func TestHandler_SubscriptionLifecycle(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Create connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           context.Background(),
		handler:       handler,
	}

	// Create subscription message
	msg := &Message{
		ID:        "sub-msg-1",
		Type:      MessageTypeSubscription,
		Operation: OperationSubscribe,
		Schema:    "public",
		Entity:    "users",
	}

	// Handle subscribe
	handler.handleSubscribe(conn, msg)

	// Verify subscription was created
	assert.Equal(t, 1, handler.GetSubscriptionCount())
	assert.Equal(t, 1, len(conn.subscriptions))

	// Verify response was sent
	select {
	case data := <-conn.send:
		assert.NotEmpty(t, data)
	default:
		t.Fatal("Expected subscription response")
	}
}

func TestHandler_UnsubscribeLifecycle(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Create connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           context.Background(),
		handler:       handler,
	}

	// Create subscription
	sub := handler.subscriptionManager.Subscribe("sub-1", "conn-1", "public", "users", nil)
	conn.AddSubscription(sub)

	assert.Equal(t, 1, handler.GetSubscriptionCount())

	// Create unsubscribe message
	msg := &Message{
		ID:             "unsub-msg-1",
		Type:           MessageTypeSubscription,
		Operation:      OperationUnsubscribe,
		SubscriptionID: "sub-1",
	}

	// Handle unsubscribe
	handler.handleUnsubscribe(conn, msg)

	// Verify subscription was removed
	assert.Equal(t, 0, handler.GetSubscriptionCount())
	assert.Equal(t, 0, len(conn.subscriptions))

	// Verify response was sent
	select {
	case data := <-conn.send:
		assert.NotEmpty(t, data)
	default:
		t.Fatal("Expected unsubscribe response")
	}
}

func TestHandler_HandlePing(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
	}

	msg := &Message{
		ID:   "ping-1",
		Type: MessageTypePing,
	}

	handler.handlePing(conn, msg)

	// Verify pong was sent
	select {
	case data := <-conn.send:
		assert.NotEmpty(t, data)
	default:
		t.Fatal("Expected pong response")
	}
}

func TestHandler_CompleteWorkflow(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRegistry := &MockModelRegistry{}
	handler := NewHandler(mockDB, mockRegistry)

	// Setup hooks (these are registered but not called in this test workflow)
	handler.Hooks().RegisterBefore(OperationCreate, func(ctx *HookContext) error {
		return nil
	})

	handler.Hooks().RegisterAfter(OperationCreate, func(ctx *HookContext) error {
		return nil
	})

	// Create connection
	conn := &Connection{
		ID:            "conn-1",
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]*Subscription),
		ctx:           context.Background(),
		handler:       handler,
		metadata:      make(map[string]interface{}),
	}

	// Register connection
	handler.connManager.connections["conn-1"] = conn

	// Set user metadata
	conn.SetMetadata("user_id", 123)

	// Create subscription
	subMsg := &Message{
		ID:        "sub-1",
		Type:      MessageTypeSubscription,
		Operation: OperationSubscribe,
		Schema:    "public",
		Entity:    "users",
	}

	handler.handleSubscribe(conn, subMsg)
	assert.Equal(t, 1, handler.GetSubscriptionCount())

	// Clear send channel
	select {
	case <-conn.send:
	default:
	}

	// Send ping
	pingMsg := &Message{
		ID:   "ping-1",
		Type: MessageTypePing,
	}

	handler.handlePing(conn, pingMsg)

	// Verify pong was sent
	select {
	case <-conn.send:
		// Expected
	default:
		t.Fatal("Expected pong response")
	}

	// Verify metadata
	userID, exists := conn.GetMetadata("user_id")
	assert.True(t, exists)
	assert.Equal(t, 123, userID)

	// Verify hooks were registered
	assert.True(t, handler.Hooks().HasHooks(BeforeCreate))
	assert.True(t, handler.Hooks().HasHooks(AfterCreate))
}
