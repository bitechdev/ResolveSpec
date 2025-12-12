package websocketspec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Handler handles WebSocket connections and messages
type Handler struct {
	db                  common.Database
	registry            common.ModelRegistry
	hooks               *HookRegistry
	nestedProcessor     *common.NestedCUDProcessor
	connManager         *ConnectionManager
	subscriptionManager *SubscriptionManager
	upgrader            websocket.Upgrader
	ctx                 context.Context
}

// NewHandler creates a new WebSocket handler
func NewHandler(db common.Database, registry common.ModelRegistry) *Handler {
	ctx := context.Background()
	handler := &Handler{
		db:                  db,
		registry:            registry,
		hooks:               NewHookRegistry(),
		connManager:         NewConnectionManager(ctx),
		subscriptionManager: NewSubscriptionManager(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// TODO: Implement proper origin checking
				return true
			},
		},
		ctx: ctx,
	}

	// Initialize nested processor (nil for now, can be added later if needed)
	// handler.nestedProcessor = common.NewNestedCUDProcessor(db, registry, handler)

	// Start connection manager
	go handler.connManager.Run()

	return handler
}

// GetRelationshipInfo implements the RelationshipInfoProvider interface
// This is a placeholder implementation - full relationship support can be added later
func (h *Handler) GetRelationshipInfo(modelType reflect.Type, relationName string) *common.RelationshipInfo {
	// TODO: Implement full relationship detection similar to restheadspec
	return nil
}

// GetDatabase returns the underlying database connection
// Implements common.SpecHandler interface
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

// Hooks returns the hook registry for this handler
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// Registry returns the model registry for this handler
func (h *Handler) Registry() common.ModelRegistry {
	return h.registry
}

// HandleWebSocket upgrades HTTP connection to WebSocket
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("[WebSocketSpec] Failed to upgrade connection: %v", err)
		return
	}

	// Create connection
	connID := uuid.New().String()
	conn := NewConnection(connID, ws, h)

	// Execute before connect hook
	hookCtx := &HookContext{
		Context:    r.Context(),
		Handler:    h,
		Connection: conn,
	}
	if err := h.hooks.Execute(BeforeConnect, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeConnect hook failed: %v", err)
		ws.Close()
		return
	}

	// Register connection
	h.connManager.Register(conn)

	// Execute after connect hook
	h.hooks.Execute(AfterConnect, hookCtx)

	// Start read/write pumps
	go conn.WritePump()
	go conn.ReadPump()

	logger.Info("[WebSocketSpec] WebSocket connection established: %s", connID)
}

// HandleMessage routes incoming messages to appropriate handlers
func (h *Handler) HandleMessage(conn *Connection, msg *Message) {
	switch msg.Type {
	case MessageTypeRequest:
		h.handleRequest(conn, msg)
	case MessageTypeSubscription:
		h.handleSubscription(conn, msg)
	case MessageTypePing:
		h.handlePing(conn, msg)
	default:
		errResp := NewErrorResponse(msg.ID, "invalid_message_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
		conn.SendJSON(errResp)
	}
}

// handleRequest processes a request message
func (h *Handler) handleRequest(conn *Connection, msg *Message) {
	ctx := conn.ctx

	schema := msg.Schema
	entity := msg.Entity
	recordID := msg.RecordID

	// Get model from registry
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		logger.Error("[WebSocketSpec] Model not found for %s.%s: %v", schema, entity, err)
		errResp := NewErrorResponse(msg.ID, "model_not_found", fmt.Sprintf("Model not found: %s.%s", schema, entity))
		conn.SendJSON(errResp)
		return
	}

	// Validate and unwrap model
	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		logger.Error("[WebSocketSpec] Model validation failed for %s.%s: %v", schema, entity, err)
		errResp := NewErrorResponse(msg.ID, "invalid_model", err.Error())
		conn.SendJSON(errResp)
		return
	}

	model = result.Model
	modelPtr := result.ModelPtr
	tableName := h.getTableName(schema, entity, model)

	// Create hook context
	hookCtx := &HookContext{
		Context:    ctx,
		Handler:    h,
		Connection: conn,
		Message:    msg,
		Schema:     schema,
		Entity:     entity,
		TableName:  tableName,
		Model:      model,
		ModelPtr:   modelPtr,
		Options:    msg.Options,
		ID:         recordID,
		Data:       msg.Data,
		Metadata:   make(map[string]interface{}),
	}

	// Route to operation handler
	switch msg.Operation {
	case OperationRead:
		h.handleRead(conn, msg, hookCtx)
	case OperationCreate:
		h.handleCreate(conn, msg, hookCtx)
	case OperationUpdate:
		h.handleUpdate(conn, msg, hookCtx)
	case OperationDelete:
		h.handleDelete(conn, msg, hookCtx)
	case OperationMeta:
		h.handleMeta(conn, msg, hookCtx)
	default:
		errResp := NewErrorResponse(msg.ID, "invalid_operation", fmt.Sprintf("Unknown operation: %s", msg.Operation))
		conn.SendJSON(errResp)
	}
}

// handleRead processes a read operation
func (h *Handler) handleRead(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeRead hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Perform read operation
	var data interface{}
	var metadata map[string]interface{}
	var err error

	if hookCtx.ID != "" {
		// Read single record by ID
		data, err = h.readByID(hookCtx)
		metadata = map[string]interface{}{"total": 1}
	} else {
		// Read multiple records
		data, metadata, err = h.readMultiple(hookCtx)
	}

	if err != nil {
		logger.Error("[WebSocketSpec] Read operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "read_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Update hook context with result
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterRead, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterRead hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	resp.Metadata = metadata
	conn.SendJSON(resp)
}

// handleCreate processes a create operation
func (h *Handler) handleCreate(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeCreate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Perform create operation
	data, err := h.create(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Create operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "create_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterCreate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationCreate, data)
}

// handleUpdate processes an update operation
func (h *Handler) handleUpdate(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeUpdate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Perform update operation
	data, err := h.update(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Update operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "update_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterUpdate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterUpdate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationUpdate, data)
}

// handleDelete processes a delete operation
func (h *Handler) handleDelete(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeDelete hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Perform delete operation
	err := h.delete(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Delete operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "delete_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Execute after hook
	if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterDelete hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{"deleted": true})
	conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationDelete, map[string]interface{}{"id": hookCtx.ID})
}

// handleMeta processes a metadata request
func (h *Handler) handleMeta(conn *Connection, msg *Message, hookCtx *HookContext) {
	metadata := h.getMetadata(hookCtx.Schema, hookCtx.Entity, hookCtx.Model)
	resp := NewResponseMessage(msg.ID, true, metadata)
	conn.SendJSON(resp)
}

// handleSubscription processes subscription messages
func (h *Handler) handleSubscription(conn *Connection, msg *Message) {
	switch msg.Operation {
	case OperationSubscribe:
		h.handleSubscribe(conn, msg)
	case OperationUnsubscribe:
		h.handleUnsubscribe(conn, msg)
	default:
		errResp := NewErrorResponse(msg.ID, "invalid_subscription_operation", fmt.Sprintf("Unknown subscription operation: %s", msg.Operation))
		conn.SendJSON(errResp)
	}
}

// handleSubscribe creates a new subscription
func (h *Handler) handleSubscribe(conn *Connection, msg *Message) {
	// Generate subscription ID
	subID := uuid.New().String()

	// Create hook context
	hookCtx := &HookContext{
		Context:    conn.ctx,
		Handler:    h,
		Connection: conn,
		Message:    msg,
		Schema:     msg.Schema,
		Entity:     msg.Entity,
		Options:    msg.Options,
		Metadata:   make(map[string]interface{}),
	}

	// Execute before hook
	if err := h.hooks.Execute(BeforeSubscribe, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeSubscribe hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Create subscription
	sub := h.subscriptionManager.Subscribe(subID, conn.ID, msg.Schema, msg.Entity, msg.Options)
	conn.AddSubscription(sub)

	// Update hook context
	hookCtx.Subscription = sub

	// Execute after hook
	h.hooks.Execute(AfterSubscribe, hookCtx)

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{
		"subscription_id": subID,
		"schema":          msg.Schema,
		"entity":          msg.Entity,
	})
	conn.SendJSON(resp)

	logger.Info("[WebSocketSpec] Subscription created: %s for %s.%s (conn: %s)", subID, msg.Schema, msg.Entity, conn.ID)
}

// handleUnsubscribe removes a subscription
func (h *Handler) handleUnsubscribe(conn *Connection, msg *Message) {
	subID := msg.SubscriptionID
	if subID == "" {
		errResp := NewErrorResponse(msg.ID, "missing_subscription_id", "Subscription ID is required for unsubscribe")
		conn.SendJSON(errResp)
		return
	}

	// Get subscription
	sub, exists := conn.GetSubscription(subID)
	if !exists {
		errResp := NewErrorResponse(msg.ID, "subscription_not_found", fmt.Sprintf("Subscription not found: %s", subID))
		conn.SendJSON(errResp)
		return
	}

	// Create hook context
	hookCtx := &HookContext{
		Context:      conn.ctx,
		Handler:      h,
		Connection:   conn,
		Message:      msg,
		Subscription: sub,
		Metadata:     make(map[string]interface{}),
	}

	// Execute before hook
	if err := h.hooks.Execute(BeforeUnsubscribe, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeUnsubscribe hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		conn.SendJSON(errResp)
		return
	}

	// Remove subscription
	h.subscriptionManager.Unsubscribe(subID)
	conn.RemoveSubscription(subID)

	// Execute after hook
	h.hooks.Execute(AfterUnsubscribe, hookCtx)

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{
		"unsubscribed": true,
		"subscription_id": subID,
	})
	conn.SendJSON(resp)
}

// handlePing responds to ping messages
func (h *Handler) handlePing(conn *Connection, msg *Message) {
	pong := &Message{
		ID:        msg.ID,
		Type:      MessageTypePong,
		Timestamp: time.Now(),
	}
	conn.SendJSON(pong)
}

// notifySubscribers sends notifications to all subscribers of an entity
func (h *Handler) notifySubscribers(schema, entity string, operation OperationType, data interface{}) {
	subscriptions := h.subscriptionManager.GetSubscriptionsByEntity(schema, entity)
	if len(subscriptions) == 0 {
		return
	}

	for _, sub := range subscriptions {
		// Check if data matches subscription filters
		if !sub.MatchesFilters(data) {
			continue
		}

		// Get connection
		conn, exists := h.connManager.GetConnection(sub.ConnectionID)
		if !exists {
			continue
		}

		// Send notification
		notification := NewNotificationMessage(sub.ID, operation, schema, entity, data)
		if err := conn.SendJSON(notification); err != nil {
			logger.Error("[WebSocketSpec] Failed to send notification to connection %s: %v", conn.ID, err)
		}
	}
}

// CRUD operation implementations

func (h *Handler) readByID(hookCtx *HookContext) (interface{}, error) {
	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Add ID filter
	pkName := reflection.GetPrimaryKeyName(hookCtx.Model)
	query = query.Where(fmt.Sprintf("%s = ?", pkName), hookCtx.ID)

	// Apply columns
	if hookCtx.Options != nil && len(hookCtx.Options.Columns) > 0 {
		query = query.Column(hookCtx.Options.Columns...)
	}

	// Apply preloads (simplified for now)
	if hookCtx.Options != nil {
		for _, preload := range hookCtx.Options.Preload {
			query = query.PreloadRelation(preload.Relation)
		}
	}

	// Execute query
	if err := query.ScanModel(hookCtx.Context); err != nil {
		return nil, fmt.Errorf("failed to read record: %w", err)
	}

	return hookCtx.ModelPtr, nil
}

func (h *Handler) readMultiple(hookCtx *HookContext) (interface{}, map[string]interface{}, error) {
	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Apply options (simplified implementation)
	if hookCtx.Options != nil {
		// Apply filters
		for _, filter := range hookCtx.Options.Filters {
			query = query.Where(fmt.Sprintf("%s %s ?", filter.Column, h.getOperatorSQL(filter.Operator)), filter.Value)
		}

		// Apply sorting
		for _, sort := range hookCtx.Options.Sort {
			direction := "ASC"
			if sort.Direction == "desc" {
				direction = "DESC"
			}
			query = query.Order(fmt.Sprintf("%s %s", sort.Column, direction))
		}

		// Apply limit and offset
		if hookCtx.Options.Limit != nil {
			query = query.Limit(*hookCtx.Options.Limit)
		}
		if hookCtx.Options.Offset != nil {
			query = query.Offset(*hookCtx.Options.Offset)
		}

		// Apply preloads
		for _, preload := range hookCtx.Options.Preload {
			query = query.PreloadRelation(preload.Relation)
		}

		// Apply columns
		if len(hookCtx.Options.Columns) > 0 {
			query = query.Column(hookCtx.Options.Columns...)
		}
	}

	// Execute query
	if err := query.ScanModel(hookCtx.Context); err != nil {
		return nil, nil, fmt.Errorf("failed to read records: %w", err)
	}

	// Get count
	metadata := make(map[string]interface{})
	countQuery := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)
	if hookCtx.Options != nil {
		for _, filter := range hookCtx.Options.Filters {
			countQuery = countQuery.Where(fmt.Sprintf("%s %s ?", filter.Column, h.getOperatorSQL(filter.Operator)), filter.Value)
		}
	}
	count, _ := countQuery.Count(hookCtx.Context)
	metadata["total"] = count
	metadata["count"] = reflection.Len(hookCtx.ModelPtr)

	return hookCtx.ModelPtr, metadata, nil
}

func (h *Handler) create(hookCtx *HookContext) (interface{}, error) {
	// Marshal and unmarshal data into model
	dataBytes, err := json.Marshal(hookCtx.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	if err := json.Unmarshal(dataBytes, hookCtx.ModelPtr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data into model: %w", err)
	}

	// Insert record
	query := h.db.NewInsert().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)
	if _, err := query.Exec(hookCtx.Context); err != nil {
		return nil, fmt.Errorf("failed to create record: %w", err)
	}

	return hookCtx.ModelPtr, nil
}

func (h *Handler) update(hookCtx *HookContext) (interface{}, error) {
	// Marshal and unmarshal data into model
	dataBytes, err := json.Marshal(hookCtx.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	if err := json.Unmarshal(dataBytes, hookCtx.ModelPtr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data into model: %w", err)
	}

	// Update record
	query := h.db.NewUpdate().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Add ID filter
	pkName := reflection.GetPrimaryKeyName(hookCtx.Model)
	query = query.Where(fmt.Sprintf("%s = ?", pkName), hookCtx.ID)

	if _, err := query.Exec(hookCtx.Context); err != nil {
		return nil, fmt.Errorf("failed to update record: %w", err)
	}

	// Fetch updated record
	return h.readByID(hookCtx)
}

func (h *Handler) delete(hookCtx *HookContext) error {
	query := h.db.NewDelete().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Add ID filter
	pkName := reflection.GetPrimaryKeyName(hookCtx.Model)
	query = query.Where(fmt.Sprintf("%s = ?", pkName), hookCtx.ID)

	if _, err := query.Exec(hookCtx.Context); err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}

	return nil
}

// Helper methods

func (h *Handler) getTableName(schema, entity string, model interface{}) string {
	// Use entity as table name
	tableName := entity

	if schema != "" {
		tableName = schema + "." + tableName
	}
	return tableName
}

func (h *Handler) getMetadata(schema, entity string, model interface{}) map[string]interface{} {
	metadata := make(map[string]interface{})
	metadata["schema"] = schema
	metadata["entity"] = entity
	metadata["table_name"] = h.getTableName(schema, entity, model)

	// Get fields from model using reflection
	columns := reflection.GetModelColumns(model)
	metadata["columns"] = columns
	metadata["primary_key"] = reflection.GetPrimaryKeyName(model)

	return metadata
}

// getOperatorSQL converts filter operator to SQL operator
func (h *Handler) getOperatorSQL(operator string) string {
	switch operator {
	case "eq":
		return "="
	case "neq":
		return "!="
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	case "lte":
		return "<="
	case "like":
		return "LIKE"
	case "ilike":
		return "ILIKE"
	case "in":
		return "IN"
	default:
		return "="
	}
}

// Shutdown gracefully shuts down the handler
func (h *Handler) Shutdown() {
	h.connManager.Shutdown()
}

// GetConnectionCount returns the number of active connections
func (h *Handler) GetConnectionCount() int {
	return h.connManager.Count()
}

// GetSubscriptionCount returns the number of active subscriptions
func (h *Handler) GetSubscriptionCount() int {
	return h.subscriptionManager.Count()
}

// BroadcastMessage sends a message to all connections matching the filter
func (h *Handler) BroadcastMessage(message interface{}, filter func(*Connection) bool) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	h.connManager.Broadcast(data, filter)
	return nil
}

// GetConnection retrieves a connection by ID
func (h *Handler) GetConnection(id string) (*Connection, bool) {
	return h.connManager.GetConnection(id)
}

// Helper to convert string ID to int64
func parseID(id string) (int64, error) {
	return strconv.ParseInt(id, 10, 64)
}
