package websocketspec

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageType_Constants(t *testing.T) {
	assert.Equal(t, MessageType("request"), MessageTypeRequest)
	assert.Equal(t, MessageType("response"), MessageTypeResponse)
	assert.Equal(t, MessageType("notification"), MessageTypeNotification)
	assert.Equal(t, MessageType("subscription"), MessageTypeSubscription)
	assert.Equal(t, MessageType("error"), MessageTypeError)
	assert.Equal(t, MessageType("ping"), MessageTypePing)
	assert.Equal(t, MessageType("pong"), MessageTypePong)
}

func TestOperationType_Constants(t *testing.T) {
	assert.Equal(t, OperationType("read"), OperationRead)
	assert.Equal(t, OperationType("create"), OperationCreate)
	assert.Equal(t, OperationType("update"), OperationUpdate)
	assert.Equal(t, OperationType("delete"), OperationDelete)
	assert.Equal(t, OperationType("subscribe"), OperationSubscribe)
	assert.Equal(t, OperationType("unsubscribe"), OperationUnsubscribe)
	assert.Equal(t, OperationType("meta"), OperationMeta)
}

func TestParseMessage_ValidRequestMessage(t *testing.T) {
	jsonData := `{
		"id": "msg-1",
		"type": "request",
		"operation": "read",
		"schema": "public",
		"entity": "users",
		"record_id": "123",
		"options": {
			"filters": [
				{"column": "status", "operator": "eq", "value": "active"}
			],
			"limit": 10
		}
	}`

	msg, err := ParseMessage([]byte(jsonData))
	require.NoError(t, err)
	assert.NotNil(t, msg)

	assert.Equal(t, "msg-1", msg.ID)
	assert.Equal(t, MessageTypeRequest, msg.Type)
	assert.Equal(t, OperationRead, msg.Operation)
	assert.Equal(t, "public", msg.Schema)
	assert.Equal(t, "users", msg.Entity)
	assert.Equal(t, "123", msg.RecordID)
	assert.NotNil(t, msg.Options)
	assert.Equal(t, 10, *msg.Options.Limit)
}

func TestParseMessage_ValidSubscriptionMessage(t *testing.T) {
	jsonData := `{
		"id": "sub-1",
		"type": "subscription",
		"operation": "subscribe",
		"schema": "public",
		"entity": "users"
	}`

	msg, err := ParseMessage([]byte(jsonData))
	require.NoError(t, err)
	assert.NotNil(t, msg)

	assert.Equal(t, "sub-1", msg.ID)
	assert.Equal(t, MessageTypeSubscription, msg.Type)
	assert.Equal(t, OperationSubscribe, msg.Operation)
	assert.Equal(t, "public", msg.Schema)
	assert.Equal(t, "users", msg.Entity)
}

func TestParseMessage_InvalidJSON(t *testing.T) {
	jsonData := `{invalid json}`

	msg, err := ParseMessage([]byte(jsonData))
	assert.Error(t, err)
	assert.Nil(t, msg)
}

func TestParseMessage_EmptyData(t *testing.T) {
	msg, err := ParseMessage([]byte{})
	assert.Error(t, err)
	assert.Nil(t, msg)
}

func TestMessage_IsValid_ValidRequestMessage(t *testing.T) {
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Entity:    "users",
	}

	assert.True(t, msg.IsValid())
}

func TestMessage_IsValid_InvalidRequestMessage_NoID(t *testing.T) {
	msg := &Message{
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Entity:    "users",
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_InvalidRequestMessage_NoOperation(t *testing.T) {
	msg := &Message{
		ID:     "msg-1",
		Type:   MessageTypeRequest,
		Entity: "users",
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_InvalidRequestMessage_NoEntity(t *testing.T) {
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_ValidSubscriptionMessage(t *testing.T) {
	msg := &Message{
		ID:        "sub-1",
		Type:      MessageTypeSubscription,
		Operation: OperationSubscribe,
	}

	assert.True(t, msg.IsValid())
}

func TestMessage_IsValid_InvalidSubscriptionMessage_NoID(t *testing.T) {
	msg := &Message{
		Type:      MessageTypeSubscription,
		Operation: OperationSubscribe,
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_InvalidSubscriptionMessage_NoOperation(t *testing.T) {
	msg := &Message{
		ID:   "sub-1",
		Type: MessageTypeSubscription,
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_NoType(t *testing.T) {
	msg := &Message{
		ID: "msg-1",
	}

	assert.False(t, msg.IsValid())
}

func TestMessage_IsValid_ResponseMessage(t *testing.T) {
	msg := &Message{
		Type: MessageTypeResponse,
	}

	// Response messages don't require specific fields
	assert.True(t, msg.IsValid())
}

func TestMessage_IsValid_NotificationMessage(t *testing.T) {
	msg := &Message{
		Type: MessageTypeNotification,
	}

	// Notification messages don't require specific fields
	assert.True(t, msg.IsValid())
}

func TestMessage_ToJSON(t *testing.T) {
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Entity:    "users",
	}

	jsonData, err := msg.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Parse back to verify
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "msg-1", parsed["id"])
	assert.Equal(t, "request", parsed["type"])
	assert.Equal(t, "read", parsed["operation"])
	assert.Equal(t, "users", parsed["entity"])
}

func TestNewRequestMessage(t *testing.T) {
	msg := NewRequestMessage("msg-1", OperationRead, "public", "users")

	assert.Equal(t, "msg-1", msg.ID)
	assert.Equal(t, MessageTypeRequest, msg.Type)
	assert.Equal(t, OperationRead, msg.Operation)
	assert.Equal(t, "public", msg.Schema)
	assert.Equal(t, "users", msg.Entity)
}

func TestNewResponseMessage(t *testing.T) {
	data := map[string]interface{}{"id": 1, "name": "John"}
	msg := NewResponseMessage("msg-1", true, data)

	assert.Equal(t, "msg-1", msg.ID)
	assert.Equal(t, MessageTypeResponse, msg.Type)
	assert.True(t, msg.Success)
	assert.Equal(t, data, msg.Data)
	assert.False(t, msg.Timestamp.IsZero())
}

func TestNewErrorResponse(t *testing.T) {
	msg := NewErrorResponse("msg-1", "validation_error", "Email is required")

	assert.Equal(t, "msg-1", msg.ID)
	assert.Equal(t, MessageTypeResponse, msg.Type)
	assert.False(t, msg.Success)
	assert.Nil(t, msg.Data)
	assert.NotNil(t, msg.Error)
	assert.Equal(t, "validation_error", msg.Error.Code)
	assert.Equal(t, "Email is required", msg.Error.Message)
	assert.False(t, msg.Timestamp.IsZero())
}

func TestNewNotificationMessage(t *testing.T) {
	data := map[string]interface{}{"id": 1, "name": "John"}
	msg := NewNotificationMessage("sub-123", OperationCreate, "public", "users", data)

	assert.Equal(t, MessageTypeNotification, msg.Type)
	assert.Equal(t, OperationCreate, msg.Operation)
	assert.Equal(t, "sub-123", msg.SubscriptionID)
	assert.Equal(t, "public", msg.Schema)
	assert.Equal(t, "users", msg.Entity)
	assert.Equal(t, data, msg.Data)
	assert.False(t, msg.Timestamp.IsZero())
}

func TestResponseMessage_ToJSON(t *testing.T) {
	resp := NewResponseMessage("msg-1", true, map[string]interface{}{"test": "data"})

	jsonData, err := resp.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Verify JSON structure
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "msg-1", parsed["id"])
	assert.Equal(t, "response", parsed["type"])
	assert.True(t, parsed["success"].(bool))
}

func TestNotificationMessage_ToJSON(t *testing.T) {
	notif := NewNotificationMessage("sub-123", OperationUpdate, "public", "users", map[string]interface{}{"id": 1})

	jsonData, err := notif.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Verify JSON structure
	var parsed map[string]interface{}
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "notification", parsed["type"])
	assert.Equal(t, "update", parsed["operation"])
	assert.Equal(t, "sub-123", parsed["subscription_id"])
}

func TestErrorInfo_Structure(t *testing.T) {
	err := &ErrorInfo{
		Code:    "validation_error",
		Message: "Invalid input",
		Details: map[string]interface{}{
			"field": "email",
			"value": "invalid",
		},
	}

	assert.Equal(t, "validation_error", err.Code)
	assert.Equal(t, "Invalid input", err.Message)
	assert.NotNil(t, err.Details)
	assert.Equal(t, "email", err.Details["field"])
}

func TestMessage_WithOptions(t *testing.T) {
	limit := 10
	offset := 0

	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeRequest,
		Operation: OperationRead,
		Entity:    "users",
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{
				{Column: "status", Operator: "eq", Value: "active"},
			},
			Columns: []string{"id", "name", "email"},
			Sort: []common.SortOption{
				{Column: "name", Direction: "asc"},
			},
			Limit:  &limit,
			Offset: &offset,
		},
	}

	assert.True(t, msg.IsValid())
	assert.NotNil(t, msg.Options)
	assert.Len(t, msg.Options.Filters, 1)
	assert.Equal(t, "status", msg.Options.Filters[0].Column)
	assert.Len(t, msg.Options.Columns, 3)
	assert.Len(t, msg.Options.Sort, 1)
	assert.Equal(t, 10, *msg.Options.Limit)
}

func TestMessage_CompleteRequestFlow(t *testing.T) {
	// Create a request message
	req := NewRequestMessage("msg-123", OperationCreate, "public", "users")
	req.Data = map[string]interface{}{
		"name":   "John Doe",
		"email":  "john@example.com",
		"status": "active",
	}

	// Convert to JSON
	reqJSON, err := json.Marshal(req)
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseMessage(reqJSON)
	require.NoError(t, err)
	assert.True(t, parsed.IsValid())
	assert.Equal(t, "msg-123", parsed.ID)
	assert.Equal(t, MessageTypeRequest, parsed.Type)
	assert.Equal(t, OperationCreate, parsed.Operation)

	// Create success response
	resp := NewResponseMessage("msg-123", true, map[string]interface{}{
		"id":     1,
		"name":   "John Doe",
		"email":  "john@example.com",
		"status": "active",
	})

	respJSON, err := resp.ToJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, respJSON)
}

func TestMessage_TimestampSerialization(t *testing.T) {
	now := time.Now()
	msg := &Message{
		ID:        "msg-1",
		Type:      MessageTypeResponse,
		Timestamp: now,
	}

	jsonData, err := msg.ToJSON()
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseMessage(jsonData)
	require.NoError(t, err)

	// Timestamps should be approximately equal (within a second due to serialization)
	assert.WithinDuration(t, now, parsed.Timestamp, time.Second)
}

func TestMessage_WithMetadata(t *testing.T) {
	msg := &Message{
		ID:       "msg-1",
		Type:     MessageTypeResponse,
		Success:  true,
		Data:     []interface{}{},
		Metadata: map[string]interface{}{
			"total": 100,
			"count": 10,
			"page":  1,
		},
	}

	jsonData, err := msg.ToJSON()
	require.NoError(t, err)

	parsed, err := ParseMessage(jsonData)
	require.NoError(t, err)
	assert.NotNil(t, parsed.Metadata)
	assert.Equal(t, float64(100), parsed.Metadata["total"]) // JSON numbers are float64
	assert.Equal(t, float64(10), parsed.Metadata["count"])
}
