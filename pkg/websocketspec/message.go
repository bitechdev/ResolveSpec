package websocketspec

import (
	"encoding/json"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// MessageTypeRequest is a client request message
	MessageTypeRequest MessageType = "request"
	// MessageTypeResponse is a server response message
	MessageTypeResponse MessageType = "response"
	// MessageTypeNotification is a server-initiated notification
	MessageTypeNotification MessageType = "notification"
	// MessageTypeSubscription is a subscription control message
	MessageTypeSubscription MessageType = "subscription"
	// MessageTypeError is an error message
	MessageTypeError MessageType = "error"
	// MessageTypePing is a keepalive ping message
	MessageTypePing MessageType = "ping"
	// MessageTypePong is a keepalive pong response
	MessageTypePong MessageType = "pong"
)

// OperationType represents the operation to perform
type OperationType string

const (
	// OperationRead retrieves records
	OperationRead OperationType = "read"
	// OperationCreate creates a new record
	OperationCreate OperationType = "create"
	// OperationUpdate updates an existing record
	OperationUpdate OperationType = "update"
	// OperationDelete deletes a record
	OperationDelete OperationType = "delete"
	// OperationSubscribe subscribes to entity changes
	OperationSubscribe OperationType = "subscribe"
	// OperationUnsubscribe unsubscribes from entity changes
	OperationUnsubscribe OperationType = "unsubscribe"
	// OperationMeta retrieves metadata about an entity
	OperationMeta OperationType = "meta"
)

// Message represents a WebSocket message
type Message struct {
	// ID is a unique identifier for request/response correlation
	ID string `json:"id,omitempty"`

	// Type is the message type
	Type MessageType `json:"type"`

	// Operation is the operation to perform
	Operation OperationType `json:"operation,omitempty"`

	// Schema is the database schema name
	Schema string `json:"schema,omitempty"`

	// Entity is the table/model name
	Entity string `json:"entity,omitempty"`

	// RecordID is the ID for single-record operations (update, delete, read by ID)
	RecordID string `json:"record_id,omitempty"`

	// Data contains the request/response payload
	Data interface{} `json:"data,omitempty"`

	// Options contains query options (filters, sorting, pagination, etc.)
	Options *common.RequestOptions `json:"options,omitempty"`

	// SubscriptionID is the subscription identifier
	SubscriptionID string `json:"subscription_id,omitempty"`

	// Success indicates if the operation was successful
	Success bool `json:"success,omitempty"`

	// Error contains error information
	Error *ErrorInfo `json:"error,omitempty"`

	// Metadata contains additional response metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Timestamp is when the message was created
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// ErrorInfo contains error details
type ErrorInfo struct {
	// Code is the error code
	Code string `json:"code"`

	// Message is a human-readable error message
	Message string `json:"message"`

	// Details contains additional error context
	Details map[string]interface{} `json:"details,omitempty"`
}

// RequestMessage represents a client request
type RequestMessage struct {
	ID        string             `json:"id"`
	Type      MessageType        `json:"type"`
	Operation OperationType      `json:"operation"`
	Schema    string             `json:"schema,omitempty"`
	Entity    string             `json:"entity"`
	RecordID  string                  `json:"record_id,omitempty"`
	Data      interface{}             `json:"data,omitempty"`
	Options   *common.RequestOptions  `json:"options,omitempty"`
}

// ResponseMessage represents a server response
type ResponseMessage struct {
	ID        string                 `json:"id"`
	Type      MessageType            `json:"type"`
	Success   bool                   `json:"success"`
	Data      interface{}            `json:"data,omitempty"`
	Error     *ErrorInfo             `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// NotificationMessage represents a server-initiated notification
type NotificationMessage struct {
	Type           MessageType        `json:"type"`
	Operation      OperationType      `json:"operation"`
	SubscriptionID string             `json:"subscription_id"`
	Schema         string             `json:"schema"`
	Entity         string             `json:"entity"`
	Data           interface{}        `json:"data"`
	Timestamp      time.Time          `json:"timestamp"`
}

// SubscriptionMessage represents a subscription control message
type SubscriptionMessage struct {
	ID             string          `json:"id"`
	Type           MessageType     `json:"type"`
	Operation      OperationType          `json:"operation"` // subscribe or unsubscribe
	Schema         string                 `json:"schema,omitempty"`
	Entity         string                 `json:"entity"`
	Options        *common.RequestOptions `json:"options,omitempty"` // Filters for subscription
	SubscriptionID string          `json:"subscription_id,omitempty"` // For unsubscribe
}

// NewRequestMessage creates a new request message
func NewRequestMessage(id string, operation OperationType, schema, entity string) *RequestMessage {
	return &RequestMessage{
		ID:        id,
		Type:      MessageTypeRequest,
		Operation: operation,
		Schema:    schema,
		Entity:    entity,
	}
}

// NewResponseMessage creates a new response message
func NewResponseMessage(id string, success bool, data interface{}) *ResponseMessage {
	return &ResponseMessage{
		ID:        id,
		Type:      MessageTypeResponse,
		Success:   success,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// NewErrorResponse creates an error response message
func NewErrorResponse(id string, code, message string) *ResponseMessage {
	return &ResponseMessage{
		ID:      id,
		Type:    MessageTypeResponse,
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now(),
	}
}

// NewNotificationMessage creates a new notification message
func NewNotificationMessage(subscriptionID string, operation OperationType, schema, entity string, data interface{}) *NotificationMessage {
	return &NotificationMessage{
		Type:           MessageTypeNotification,
		Operation:      operation,
		SubscriptionID: subscriptionID,
		Schema:         schema,
		Entity:         entity,
		Data:           data,
		Timestamp:      time.Now(),
	}
}

// ParseMessage parses a JSON message into a Message struct
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ToJSON converts a message to JSON bytes
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// ToJSON converts a response message to JSON bytes
func (r *ResponseMessage) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// ToJSON converts a notification message to JSON bytes
func (n *NotificationMessage) ToJSON() ([]byte, error) {
	return json.Marshal(n)
}

// IsValid checks if a message is valid
func (m *Message) IsValid() bool {
	// Type must be set
	if m.Type == "" {
		return false
	}

	// Request messages must have an ID, operation, and entity
	if m.Type == MessageTypeRequest {
		return m.ID != "" && m.Operation != "" && m.Entity != ""
	}

	// Subscription messages must have an ID and operation
	if m.Type == MessageTypeSubscription {
		return m.ID != "" && m.Operation != ""
	}

	return true
}
