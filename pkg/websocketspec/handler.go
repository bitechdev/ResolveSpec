package websocketspec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Handler handles WebSocket connections and messages
type Handler struct {
	db                  common.Database
	registry            common.ModelRegistry
	hooks               *HookRegistry
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
	_ = h.hooks.Execute(AfterConnect, hookCtx)

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
		_ = conn.SendJSON(errResp)
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
		_ = conn.SendJSON(errResp)
		return
	}

	// Validate and unwrap model
	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		logger.Error("[WebSocketSpec] Model validation failed for %s.%s: %v", schema, entity, err)
		errResp := NewErrorResponse(msg.ID, "invalid_model", err.Error())
		_ = conn.SendJSON(errResp)
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

	// Execute BeforeHandle hook - auth check fires here, after model resolution
	hookCtx.Operation = string(msg.Operation)
	if err := h.hooks.Execute(BeforeHandle, hookCtx); err != nil {
		if hookCtx.Abort {
			errResp := NewErrorResponse(msg.ID, "unauthorized", hookCtx.AbortMessage)
			_ = conn.SendJSON(errResp)
		}
		return
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
		_ = conn.SendJSON(errResp)
	}
}

// handleRead processes a read operation
func (h *Handler) handleRead(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeRead hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Perform read operation
	var data interface{}
	var metadata map[string]interface{}
	var err error

	// Check if FetchRowNumber is specified (treat as single record read)
	isFetchRowNumber := hookCtx.Options != nil && hookCtx.Options.FetchRowNumber != nil && *hookCtx.Options.FetchRowNumber != ""

	if hookCtx.ID != "" || isFetchRowNumber {
		// Read single record by ID or FetchRowNumber
		data, err = h.readByID(hookCtx)
		metadata = map[string]interface{}{"total": 1}
		// The row number is already set on the record itself via setRowNumbersOnRecords
	} else {
		// Read multiple records
		data, metadata, err = h.readMultiple(hookCtx)
	}

	if err != nil {
		logger.Error("[WebSocketSpec] Read operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "read_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Update hook context with result
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterRead, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterRead hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	resp.Metadata = metadata
	_ = conn.SendJSON(resp)
}

// handleCreate processes a create operation
func (h *Handler) handleCreate(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeCreate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Perform create operation
	data, err := h.create(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Create operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "create_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterCreate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	_ = conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationCreate, data)
}

// handleUpdate processes an update operation
func (h *Handler) handleUpdate(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeUpdate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Perform update operation
	data, err := h.update(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Update operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "update_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterUpdate, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterUpdate hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, hookCtx.Result)
	_ = conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationUpdate, data)
}

// handleDelete processes a delete operation
func (h *Handler) handleDelete(conn *Connection, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] BeforeDelete hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Perform delete operation
	err := h.delete(hookCtx)
	if err != nil {
		logger.Error("[WebSocketSpec] Delete operation failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "delete_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Execute after hook
	if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
		logger.Error("[WebSocketSpec] AfterDelete hook failed: %v", err)
		errResp := NewErrorResponse(msg.ID, "hook_error", err.Error())
		_ = conn.SendJSON(errResp)
		return
	}

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{"deleted": true})
	_ = conn.SendJSON(resp)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationDelete, map[string]interface{}{"id": hookCtx.ID})
}

// handleMeta processes a metadata request
func (h *Handler) handleMeta(conn *Connection, msg *Message, hookCtx *HookContext) {
	metadata := h.getMetadata(hookCtx.Schema, hookCtx.Entity, hookCtx.Model)
	resp := NewResponseMessage(msg.ID, true, metadata)
	_ = conn.SendJSON(resp)
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
		_ = conn.SendJSON(errResp)
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
		_ = conn.SendJSON(errResp)
		return
	}

	// Create subscription
	sub := h.subscriptionManager.Subscribe(subID, conn.ID, msg.Schema, msg.Entity, msg.Options)
	conn.AddSubscription(sub)

	// Update hook context
	hookCtx.Subscription = sub

	// Execute after hook
	_ = h.hooks.Execute(AfterSubscribe, hookCtx)

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{
		"subscription_id": subID,
		"schema":          msg.Schema,
		"entity":          msg.Entity,
	})
	_ = conn.SendJSON(resp)

	logger.Info("[WebSocketSpec] Subscription created: %s for %s.%s (conn: %s)", subID, msg.Schema, msg.Entity, conn.ID)
}

// handleUnsubscribe removes a subscription
func (h *Handler) handleUnsubscribe(conn *Connection, msg *Message) {
	subID := msg.SubscriptionID
	if subID == "" {
		errResp := NewErrorResponse(msg.ID, "missing_subscription_id", "Subscription ID is required for unsubscribe")
		_ = conn.SendJSON(errResp)
		return
	}

	// Get subscription
	sub, exists := conn.GetSubscription(subID)
	if !exists {
		errResp := NewErrorResponse(msg.ID, "subscription_not_found", fmt.Sprintf("Subscription not found: %s", subID))
		_ = conn.SendJSON(errResp)
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
		_ = conn.SendJSON(errResp)
		return
	}

	// Remove subscription
	h.subscriptionManager.Unsubscribe(subID)
	conn.RemoveSubscription(subID)

	// Execute after hook
	_ = h.hooks.Execute(AfterUnsubscribe, hookCtx)

	// Send response
	resp := NewResponseMessage(msg.ID, true, map[string]interface{}{
		"unsubscribed":    true,
		"subscription_id": subID,
	})
	_ = conn.SendJSON(resp)
}

// handlePing responds to ping messages
func (h *Handler) handlePing(conn *Connection, msg *Message) {
	pong := &Message{
		ID:        msg.ID,
		Type:      MessageTypePong,
		Timestamp: time.Now(),
	}
	_ = conn.SendJSON(pong)
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
	// Handle FetchRowNumber before building query
	var fetchedRowNumber *int64
	pkName := reflection.GetPrimaryKeyName(hookCtx.Model)

	if hookCtx.Options != nil && hookCtx.Options.FetchRowNumber != nil && *hookCtx.Options.FetchRowNumber != "" {
		fetchRowNumberPKValue := *hookCtx.Options.FetchRowNumber
		logger.Debug("[WebSocketSpec] FetchRowNumber: Fetching row number for PK %s = %s", pkName, fetchRowNumberPKValue)

		rowNum, err := h.FetchRowNumber(hookCtx.Context, hookCtx.TableName, pkName, fetchRowNumberPKValue, hookCtx.Options, hookCtx.Model)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch row number: %w", err)
		}

		fetchedRowNumber = &rowNum
		logger.Debug("[WebSocketSpec] FetchRowNumber: Row number %d for PK %s = %s", rowNum, pkName, fetchRowNumberPKValue)

		// Override ID with FetchRowNumber value
		hookCtx.ID = fetchRowNumberPKValue
	}

	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Add ID filter
	query = query.Where(fmt.Sprintf("%s = ?", pkName), hookCtx.ID)

	// Apply columns
	if hookCtx.Options != nil && len(hookCtx.Options.Columns) > 0 {
		query = query.Column(hookCtx.Options.Columns...)
	}

	// Apply preloads (simplified for now)
	if hookCtx.Options != nil {
		for i := range hookCtx.Options.Preload {
			query = query.PreloadRelation(hookCtx.Options.Preload[i].Relation)
		}
	}

	// Execute query
	if err := query.ScanModel(hookCtx.Context); err != nil {
		return nil, fmt.Errorf("failed to read record: %w", err)
	}

	// Set the fetched row number on the record if FetchRowNumber was used
	if fetchedRowNumber != nil {
		logger.Debug("[WebSocketSpec] FetchRowNumber: Setting row number %d on record", *fetchedRowNumber)
		h.setRowNumbersOnRecords(hookCtx.ModelPtr, int(*fetchedRowNumber-1)) // -1 because setRowNumbersOnRecords adds 1
	}

	return hookCtx.ModelPtr, nil
}

func (h *Handler) readMultiple(hookCtx *HookContext) (data interface{}, metadata map[string]interface{}, err error) {
	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Apply options (simplified implementation)
	if hookCtx.Options != nil {
		// Apply filters with OR grouping support
		query = h.applyFilters(query, hookCtx.Options.Filters)

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
		for i := range hookCtx.Options.Preload {
			query = query.PreloadRelation(hookCtx.Options.Preload[i].Relation)
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

	// Set row numbers on records if RowNumber field exists
	offset := 0
	if hookCtx.Options != nil && hookCtx.Options.Offset != nil {
		offset = *hookCtx.Options.Offset
	}
	h.setRowNumbersOnRecords(hookCtx.ModelPtr, offset)

	// Get count
	metadata = make(map[string]interface{})
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
	tableName := entity

	if schema != "" {
		if h.db.DriverName() == "sqlite" {
			tableName = schema + "_" + tableName
		} else {
			tableName = schema + "." + tableName
		}
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
// applyFilters applies all filters with proper grouping for OR logic
// Groups consecutive OR filters together to ensure proper query precedence
func (h *Handler) applyFilters(query common.SelectQuery, filters []common.FilterOption) common.SelectQuery {
	if len(filters) == 0 {
		return query
	}

	i := 0
	for i < len(filters) {
		// Check if this starts an OR group (next filter has OR logic)
		startORGroup := i+1 < len(filters) && strings.EqualFold(filters[i+1].LogicOperator, "OR")

		if startORGroup {
			// Collect all consecutive filters that are OR'd together
			orGroup := []common.FilterOption{filters[i]}
			j := i + 1
			for j < len(filters) && strings.EqualFold(filters[j].LogicOperator, "OR") {
				orGroup = append(orGroup, filters[j])
				j++
			}

			// Apply the OR group as a single grouped WHERE clause
			query = h.applyFilterGroup(query, orGroup)
			i = j
		} else {
			// Single filter with AND logic (or first filter)
			condition, args := h.buildFilterCondition(filters[i])
			if condition != "" {
				query = query.Where(condition, args...)
			}
			i++
		}
	}

	return query
}

// applyFilterGroup applies a group of filters that should be OR'd together
// Always wraps them in parentheses and applies as a single WHERE clause
func (h *Handler) applyFilterGroup(query common.SelectQuery, filters []common.FilterOption) common.SelectQuery {
	if len(filters) == 0 {
		return query
	}

	// Build all conditions and collect args
	var conditions []string
	var args []interface{}

	for _, filter := range filters {
		condition, filterArgs := h.buildFilterCondition(filter)
		if condition != "" {
			conditions = append(conditions, condition)
			args = append(args, filterArgs...)
		}
	}

	if len(conditions) == 0 {
		return query
	}

	// Single filter - no need for grouping
	if len(conditions) == 1 {
		return query.Where(conditions[0], args...)
	}

	// Multiple conditions - group with parentheses and OR
	groupedCondition := "(" + strings.Join(conditions, " OR ") + ")"
	return query.Where(groupedCondition, args...)
}

// buildFilterCondition builds a filter condition and returns it with args
func (h *Handler) buildFilterCondition(filter common.FilterOption) (conditionString string, conditionArgs []interface{}) {
	var condition string
	var args []interface{}

	operatorSQL := h.getOperatorSQL(filter.Operator)
	condition = fmt.Sprintf("%s %s ?", filter.Column, operatorSQL)
	args = []interface{}{filter.Value}

	return condition, args
}

// setRowNumbersOnRecords sets the RowNumber field on each record if it exists
// The row number is calculated as offset + index + 1 (1-based)
func (h *Handler) setRowNumbersOnRecords(records interface{}, offset int) {
	// Get the reflect value of the records
	recordsValue := reflect.ValueOf(records)
	if recordsValue.Kind() == reflect.Ptr {
		recordsValue = recordsValue.Elem()
	}

	// Ensure it's a slice
	if recordsValue.Kind() != reflect.Slice {
		logger.Debug("[WebSocketSpec] setRowNumbersOnRecords: records is not a slice, skipping")
		return
	}

	// Iterate through each record
	for i := 0; i < recordsValue.Len(); i++ {
		record := recordsValue.Index(i)

		// Dereference if it's a pointer
		if record.Kind() == reflect.Ptr {
			if record.IsNil() {
				continue
			}
			record = record.Elem()
		}

		// Ensure it's a struct
		if record.Kind() != reflect.Struct {
			continue
		}

		// Try to find and set the RowNumber field
		rowNumberField := record.FieldByName("RowNumber")
		if rowNumberField.IsValid() && rowNumberField.CanSet() {
			// Check if the field is of type int64
			if rowNumberField.Kind() == reflect.Int64 {
				rowNum := int64(offset + i + 1)
				rowNumberField.SetInt(rowNum)
				logger.Debug("[WebSocketSpec] Set RowNumber=%d for record index %d", rowNum, i)
			}
		}
	}
}

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

// FetchRowNumber calculates the row number of a specific record based on sorting and filtering
// Returns the 1-based row number of the record with the given primary key value
func (h *Handler) FetchRowNumber(ctx context.Context, tableName string, pkName string, pkValue string, options *common.RequestOptions, model interface{}) (int64, error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[WebSocketSpec] Panic during FetchRowNumber: %v", r)
		}
	}()

	// Build the sort order SQL
	sortSQL := ""
	if options != nil && len(options.Sort) > 0 {
		sortParts := make([]string, 0, len(options.Sort))
		for _, sort := range options.Sort {
			if sort.Column == "" {
				continue
			}
			direction := "ASC"
			if strings.EqualFold(sort.Direction, "desc") {
				direction = "DESC"
			}
			sortParts = append(sortParts, fmt.Sprintf("%s %s", sort.Column, direction))
		}
		sortSQL = strings.Join(sortParts, ", ")
	} else {
		// Default sort by primary key
		sortSQL = fmt.Sprintf("%s ASC", pkName)
	}

	// Build WHERE clause from filters
	whereSQL := ""
	var whereArgs []interface{}
	if options != nil && len(options.Filters) > 0 {
		var conditions []string
		for _, filter := range options.Filters {
			operatorSQL := h.getOperatorSQL(filter.Operator)
			conditions = append(conditions, fmt.Sprintf("%s.%s %s ?", tableName, filter.Column, operatorSQL))
			whereArgs = append(whereArgs, filter.Value)
		}
		if len(conditions) > 0 {
			whereSQL = "WHERE " + strings.Join(conditions, " AND ")
		}
	}

	// Build the final query with parameterized PK value
	queryStr := fmt.Sprintf(`
		SELECT search.rn
		FROM (
			SELECT %[1]s.%[2]s,
				ROW_NUMBER() OVER(ORDER BY %[3]s) AS rn
			FROM %[1]s
			%[4]s
		) search
		WHERE search.%[2]s = ?
	`,
		tableName, // [1] - table name
		pkName,    // [2] - primary key column name
		sortSQL,   // [3] - sort order SQL
		whereSQL,  // [4] - WHERE clause
	)

	logger.Debug("[WebSocketSpec] FetchRowNumber query: %s, pkValue: %s", queryStr, pkValue)

	// Append PK value to whereArgs
	whereArgs = append(whereArgs, pkValue)

	// Execute the raw query with parameterized PK value
	var result []struct {
		RN int64 `bun:"rn"`
	}
	err := h.db.Query(ctx, &result, queryStr, whereArgs...)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch row number: %w", err)
	}

	if len(result) == 0 {
		whereInfo := "none"
		if whereSQL != "" {
			whereInfo = whereSQL
		}
		return 0, fmt.Errorf("no row found for primary key %s=%s with active filters: %s", pkName, pkValue, whereInfo)
	}

	return result[0].RN, nil
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
