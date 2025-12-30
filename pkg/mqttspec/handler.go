package mqttspec

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// Handler handles MQTT messages and operations
type Handler struct {
	// Database adapter (GORM/Bun)
	db common.Database

	// Model registry
	registry common.ModelRegistry

	// Hook registry
	hooks *HookRegistry

	// Client manager
	clientManager *ClientManager

	// Subscription manager
	subscriptionManager *SubscriptionManager

	// Broker interface (embedded or external)
	broker BrokerInterface

	// Configuration
	config *Config

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Started flag
	started bool
	mu      sync.RWMutex
}

// NewHandler creates a new MQTT handler
func NewHandler(db common.Database, registry common.ModelRegistry, config *Config) (*Handler, error) {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Handler{
		db:                  db,
		registry:            registry,
		hooks:               NewHookRegistry(),
		clientManager:       NewClientManager(ctx),
		subscriptionManager: NewSubscriptionManager(),
		config:              config,
		ctx:                 ctx,
		cancel:              cancel,
		started:             false,
	}

	// Initialize broker based on mode
	if config.BrokerMode == BrokerModeEmbedded {
		h.broker = NewEmbeddedBroker(config.Broker, h.clientManager)
	} else {
		h.broker = NewExternalBrokerClient(config.ExternalBroker, h.clientManager)
	}

	// Set handler reference in broker
	h.broker.SetHandler(h)

	return h, nil
}

// Start initializes and starts the handler
func (h *Handler) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return fmt.Errorf("handler already started")
	}

	// Start broker
	if err := h.broker.Start(h.ctx); err != nil {
		return fmt.Errorf("failed to start broker: %w", err)
	}

	// Subscribe to all request topics: spec/+/request
	requestTopic := fmt.Sprintf("%s/+/request", h.config.Topics.Prefix)
	if err := h.broker.Subscribe(requestTopic, h.config.QoS.Request, h.handleIncomingMessage); err != nil {
		_ = h.broker.Stop(h.ctx)
		return fmt.Errorf("failed to subscribe to request topic: %w", err)
	}

	h.started = true
	logger.Info("[MQTTSpec] Handler started, listening on topic: %s", requestTopic)

	return nil
}

// Shutdown gracefully shuts down the handler
func (h *Handler) Shutdown() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.started {
		return nil
	}

	logger.Info("[MQTTSpec] Shutting down handler...")

	// Execute disconnect hooks for all clients
	h.clientManager.mu.RLock()
	clients := make([]*Client, 0, len(h.clientManager.clients))
	for _, client := range h.clientManager.clients {
		clients = append(clients, client)
	}
	h.clientManager.mu.RUnlock()

	for _, client := range clients {
		hookCtx := &HookContext{
			Context: h.ctx,
			Handler: nil, // Not used for MQTT
			Metadata: map[string]interface{}{
				"mqtt_client": client,
			},
		}
		_ = h.hooks.Execute(BeforeDisconnect, hookCtx)
		h.clientManager.Unregister(client.ID)
		_ = h.hooks.Execute(AfterDisconnect, hookCtx)
	}

	// Unsubscribe from request topic
	requestTopic := fmt.Sprintf("%s/+/request", h.config.Topics.Prefix)
	_ = h.broker.Unsubscribe(requestTopic)

	// Stop broker
	if err := h.broker.Stop(h.ctx); err != nil {
		logger.Error("[MQTTSpec] Error stopping broker: %v", err)
	}

	// Cancel context
	if h.cancel != nil {
		h.cancel()
	}

	h.started = false
	logger.Info("[MQTTSpec] Handler stopped")

	return nil
}

// Hooks returns the hook registry
func (h *Handler) Hooks() *HookRegistry {
	return h.hooks
}

// Registry returns the model registry
func (h *Handler) Registry() common.ModelRegistry {
	return h.registry
}

// GetDatabase returns the database adapter
func (h *Handler) GetDatabase() common.Database {
	return h.db
}

// GetRelationshipInfo is a placeholder for relationship detection
func (h *Handler) GetRelationshipInfo(modelType reflect.Type, relationName string) *common.RelationshipInfo {
	// TODO: Implement full relationship detection if needed
	return nil
}

// handleIncomingMessage is called when a message arrives on spec/+/request
func (h *Handler) handleIncomingMessage(topic string, payload []byte) {
	// Extract client_id from topic: spec/{client_id}/request
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		logger.Error("[MQTTSpec] Invalid topic format: %s", topic)
		return
	}
	clientID := parts[len(parts)-2] // Second to last part is client_id

	// Parse message
	msg, err := ParseMessage(payload)
	if err != nil {
		logger.Error("[MQTTSpec] Failed to parse message from %s: %v", clientID, err)
		h.sendError(clientID, "", "invalid_message", "Failed to parse message")
		return
	}

	// Validate message
	if !msg.IsValid() {
		logger.Error("[MQTTSpec] Invalid message from %s", clientID)
		h.sendError(clientID, msg.ID, "invalid_message", "Message validation failed")
		return
	}

	// Get or register client
	client, exists := h.clientManager.GetClient(clientID)
	if !exists {
		// First request from this client - register it
		client = h.clientManager.Register(clientID, "", h)

		// Execute connect hooks
		hookCtx := &HookContext{
			Context: h.ctx,
			Handler: nil, // Not used for MQTT, handler ref stored in metadata if needed
			Metadata: map[string]interface{}{
				"mqtt_client": client,
			},
		}

		if err := h.hooks.Execute(BeforeConnect, hookCtx); err != nil {
			logger.Error("[MQTTSpec] BeforeConnect hook failed for %s: %v", clientID, err)
			h.sendError(clientID, msg.ID, "auth_error", err.Error())
			h.clientManager.Unregister(clientID)
			return
		}

		_ = h.hooks.Execute(AfterConnect, hookCtx)
	}

	// Route message by type
	switch msg.Type {
	case MessageTypeRequest:
		h.handleRequest(client, msg)
	case MessageTypeSubscription:
		h.handleSubscription(client, msg)
	case MessageTypePing:
		h.handlePing(client, msg)
	default:
		h.sendError(clientID, msg.ID, "invalid_message_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

// handleRequest processes CRUD requests
func (h *Handler) handleRequest(client *Client, msg *Message) {
	ctx := client.ctx
	schema := msg.Schema
	entity := msg.Entity
	recordID := msg.RecordID

	// Get model from registry
	model, err := h.registry.GetModelByEntity(schema, entity)
	if err != nil {
		logger.Error("[MQTTSpec] Model not found for %s.%s: %v", schema, entity, err)
		h.sendError(client.ID, msg.ID, "model_not_found", fmt.Sprintf("Model not found: %s.%s", schema, entity))
		return
	}

	// Validate and unwrap model
	result, err := common.ValidateAndUnwrapModel(model)
	if err != nil {
		logger.Error("[MQTTSpec] Model validation failed for %s.%s: %v", schema, entity, err)
		h.sendError(client.ID, msg.ID, "invalid_model", err.Error())
		return
	}

	model = result.Model
	modelPtr := result.ModelPtr
	tableName := h.getTableName(schema, entity, model)

	// Create hook context
	hookCtx := &HookContext{
		Context:   ctx,
		Handler:   nil, // Not used for MQTT
		Message:   msg,
		Schema:    schema,
		Entity:    entity,
		TableName: tableName,
		Model:     model,
		ModelPtr:  modelPtr,
		Options:   msg.Options,
		ID:        recordID,
		Data:      msg.Data,
		Metadata: map[string]interface{}{
			"mqtt_client": client,
		},
	}

	// Route to operation handler
	switch msg.Operation {
	case OperationRead:
		h.handleRead(client, msg, hookCtx)
	case OperationCreate:
		h.handleCreate(client, msg, hookCtx)
	case OperationUpdate:
		h.handleUpdate(client, msg, hookCtx)
	case OperationDelete:
		h.handleDelete(client, msg, hookCtx)
	case OperationMeta:
		h.handleMeta(client, msg, hookCtx)
	default:
		h.sendError(client.ID, msg.ID, "invalid_operation", fmt.Sprintf("Unknown operation: %s", msg.Operation))
	}
}

// handleRead processes a read operation
func (h *Handler) handleRead(client *Client, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeRead, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeRead hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
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
		logger.Error("[MQTTSpec] Read operation failed: %v", err)
		h.sendError(client.ID, msg.ID, "read_error", err.Error())
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterRead, hookCtx); err != nil {
		logger.Error("[MQTTSpec] AfterRead hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Send response
	h.sendResponse(client.ID, msg.ID, hookCtx.Result, metadata)
}

// handleCreate processes a create operation
func (h *Handler) handleCreate(client *Client, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeCreate, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeCreate hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Perform create operation
	data, err := h.create(hookCtx)
	if err != nil {
		logger.Error("[MQTTSpec] Create operation failed: %v", err)
		h.sendError(client.ID, msg.ID, "create_error", err.Error())
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterCreate, hookCtx); err != nil {
		logger.Error("[MQTTSpec] AfterCreate hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Send response
	h.sendResponse(client.ID, msg.ID, hookCtx.Result, nil)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationCreate, data)
}

// handleUpdate processes an update operation
func (h *Handler) handleUpdate(client *Client, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeUpdate, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeUpdate hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Perform update operation
	data, err := h.update(hookCtx)
	if err != nil {
		logger.Error("[MQTTSpec] Update operation failed: %v", err)
		h.sendError(client.ID, msg.ID, "update_error", err.Error())
		return
	}

	// Update hook context
	hookCtx.Result = data

	// Execute after hook
	if err := h.hooks.Execute(AfterUpdate, hookCtx); err != nil {
		logger.Error("[MQTTSpec] AfterUpdate hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Send response
	h.sendResponse(client.ID, msg.ID, hookCtx.Result, nil)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationUpdate, data)
}

// handleDelete processes a delete operation
func (h *Handler) handleDelete(client *Client, msg *Message, hookCtx *HookContext) {
	// Execute before hook
	if err := h.hooks.Execute(BeforeDelete, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeDelete hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Perform delete operation
	if err := h.delete(hookCtx); err != nil {
		logger.Error("[MQTTSpec] Delete operation failed: %v", err)
		h.sendError(client.ID, msg.ID, "delete_error", err.Error())
		return
	}

	// Execute after hook
	if err := h.hooks.Execute(AfterDelete, hookCtx); err != nil {
		logger.Error("[MQTTSpec] AfterDelete hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Send response
	h.sendResponse(client.ID, msg.ID, map[string]interface{}{"deleted": true}, nil)

	// Notify subscribers
	h.notifySubscribers(hookCtx.Schema, hookCtx.Entity, OperationDelete, map[string]interface{}{
		"id": hookCtx.ID,
	})
}

// handleMeta processes a metadata request
func (h *Handler) handleMeta(client *Client, msg *Message, hookCtx *HookContext) {
	metadata, err := h.getMetadata(hookCtx)
	if err != nil {
		logger.Error("[MQTTSpec] Meta operation failed: %v", err)
		h.sendError(client.ID, msg.ID, "meta_error", err.Error())
		return
	}

	h.sendResponse(client.ID, msg.ID, metadata, nil)
}

// handleSubscription manages subscriptions
func (h *Handler) handleSubscription(client *Client, msg *Message) {
	switch msg.Operation {
	case OperationSubscribe:
		h.handleSubscribe(client, msg)
	case OperationUnsubscribe:
		h.handleUnsubscribe(client, msg)
	default:
		h.sendError(client.ID, msg.ID, "invalid_subscription_operation", fmt.Sprintf("Unknown subscription operation: %s", msg.Operation))
	}
}

// handleSubscribe creates a subscription
func (h *Handler) handleSubscribe(client *Client, msg *Message) {
	// Generate subscription ID
	subID := uuid.New().String()

	// Create hook context
	hookCtx := &HookContext{
		Context: client.ctx,
		Handler: nil, // Not used for MQTT
		Message: msg,
		Schema:  msg.Schema,
		Entity:  msg.Entity,
		Options: msg.Options,
		Metadata: map[string]interface{}{
			"mqtt_client": client,
		},
	}

	// Execute before hook
	if err := h.hooks.Execute(BeforeSubscribe, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeSubscribe hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Create subscription
	sub := h.subscriptionManager.Subscribe(subID, client.ID, msg.Schema, msg.Entity, msg.Options)
	client.AddSubscription(sub)

	// Execute after hook
	_ = h.hooks.Execute(AfterSubscribe, hookCtx)

	// Send response
	h.sendResponse(client.ID, msg.ID, map[string]interface{}{
		"subscription_id": subID,
		"schema":          msg.Schema,
		"entity":          msg.Entity,
		"notify_topic":    h.getNotifyTopic(client.ID, subID),
	}, nil)

	logger.Info("[MQTTSpec] Subscription created: %s for %s.%s (client: %s)", subID, msg.Schema, msg.Entity, client.ID)
}

// handleUnsubscribe removes a subscription
func (h *Handler) handleUnsubscribe(client *Client, msg *Message) {
	subID := msg.SubscriptionID
	if subID == "" {
		h.sendError(client.ID, msg.ID, "invalid_subscription", "Subscription ID is required")
		return
	}

	// Create hook context
	hookCtx := &HookContext{
		Context: client.ctx,
		Handler: nil, // Not used for MQTT
		Message: msg,
		Metadata: map[string]interface{}{
			"mqtt_client": client,
		},
	}

	// Execute before hook
	if err := h.hooks.Execute(BeforeUnsubscribe, hookCtx); err != nil {
		logger.Error("[MQTTSpec] BeforeUnsubscribe hook failed: %v", err)
		h.sendError(client.ID, msg.ID, "hook_error", err.Error())
		return
	}

	// Remove subscription
	h.subscriptionManager.Unsubscribe(subID)
	client.RemoveSubscription(subID)

	// Execute after hook
	_ = h.hooks.Execute(AfterUnsubscribe, hookCtx)

	// Send response
	h.sendResponse(client.ID, msg.ID, map[string]interface{}{
		"unsubscribed":    true,
		"subscription_id": subID,
	}, nil)

	logger.Info("[MQTTSpec] Subscription removed: %s (client: %s)", subID, client.ID)
}

// handlePing responds to ping messages
func (h *Handler) handlePing(client *Client, msg *Message) {
	pong := &ResponseMessage{
		ID:      msg.ID,
		Type:    MessageTypePong,
		Success: true,
	}

	payload, _ := json.Marshal(pong)
	topic := h.getResponseTopic(client.ID)
	_ = h.broker.Publish(topic, h.config.QoS.Response, payload)
}

// notifySubscribers sends notifications to subscribers
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

		// Get client
		client, exists := h.clientManager.GetClient(sub.ConnectionID)
		if !exists {
			continue
		}

		// Create notification message
		notification := NewNotificationMessage(sub.ID, operation, schema, entity, data)
		payload, err := json.Marshal(notification)
		if err != nil {
			logger.Error("[MQTTSpec] Failed to marshal notification: %v", err)
			continue
		}

		// Publish to client's notify topic
		topic := h.getNotifyTopic(client.ID, sub.ID)
		if err := h.broker.Publish(topic, h.config.QoS.Notification, payload); err != nil {
			logger.Error("[MQTTSpec] Failed to publish notification to %s: %v", topic, err)
		}
	}
}

// Response helpers

// sendResponse publishes a response message
func (h *Handler) sendResponse(clientID, msgID string, data interface{}, metadata map[string]interface{}) {
	resp := NewResponseMessage(msgID, true, data)
	resp.Metadata = metadata

	payload, err := json.Marshal(resp)
	if err != nil {
		logger.Error("[MQTTSpec] Failed to marshal response: %v", err)
		return
	}

	topic := h.getResponseTopic(clientID)
	if err := h.broker.Publish(topic, h.config.QoS.Response, payload); err != nil {
		logger.Error("[MQTTSpec] Failed to publish response to %s: %v", topic, err)
	}
}

// sendError publishes an error response
func (h *Handler) sendError(clientID, msgID, code, message string) {
	errResp := NewErrorResponse(msgID, code, message)

	payload, _ := json.Marshal(errResp)
	topic := h.getResponseTopic(clientID)
	_ = h.broker.Publish(topic, h.config.QoS.Response, payload)
}

// Topic helpers

func (h *Handler) getRequestTopic(clientID string) string {
	return fmt.Sprintf("%s/%s/request", h.config.Topics.Prefix, clientID)
}

func (h *Handler) getResponseTopic(clientID string) string {
	return fmt.Sprintf("%s/%s/response", h.config.Topics.Prefix, clientID)
}

func (h *Handler) getNotifyTopic(clientID, subscriptionID string) string {
	return fmt.Sprintf("%s/%s/notify/%s", h.config.Topics.Prefix, clientID, subscriptionID)
}

// Database operation helpers (adapted from websocketspec)

func (h *Handler) getTableName(schema, entity string, model interface{}) string {
	// Use entity as table name
	tableName := entity

	if schema != "" {
		tableName = schema + "." + tableName
	}
	return tableName
}

// readByID reads a single record by ID
func (h *Handler) readByID(hookCtx *HookContext) (interface{}, error) {
	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Add ID filter
	pkName := reflection.GetPrimaryKeyName(hookCtx.Model)
	query = query.Where(fmt.Sprintf("%s = ?", pkName), hookCtx.ID)

	// Apply columns
	if hookCtx.Options != nil && len(hookCtx.Options.Columns) > 0 {
		query = query.Column(hookCtx.Options.Columns...)
	}

	// Apply preloads (simplified)
	if hookCtx.Options != nil {
		for i := range hookCtx.Options.Preload {
			query = query.PreloadRelation(hookCtx.Options.Preload[i].Relation)
		}
	}

	// Execute query
	if err := query.ScanModel(hookCtx.Context); err != nil {
		return nil, fmt.Errorf("failed to read record: %w", err)
	}

	return hookCtx.ModelPtr, nil
}

// readMultiple reads multiple records
func (h *Handler) readMultiple(hookCtx *HookContext) (data interface{}, metadata map[string]interface{}, err error) {
	query := h.db.NewSelect().Model(hookCtx.ModelPtr).Table(hookCtx.TableName)

	// Apply options
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

// create creates a new record
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

// update updates an existing record
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

// delete deletes a record
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

// getMetadata returns schema metadata for an entity
func (h *Handler) getMetadata(hookCtx *HookContext) (interface{}, error) {
	metadata := make(map[string]interface{})
	metadata["schema"] = hookCtx.Schema
	metadata["entity"] = hookCtx.Entity
	metadata["table_name"] = hookCtx.TableName

	// Get fields from model using reflection
	columns := reflection.GetModelColumns(hookCtx.Model)
	metadata["columns"] = columns
	metadata["primary_key"] = reflection.GetPrimaryKeyName(hookCtx.Model)

	return metadata, nil
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
