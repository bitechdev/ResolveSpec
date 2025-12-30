package mqttspec

import (
	"github.com/bitechdev/ResolveSpec/pkg/websocketspec"
)

// Message types - aliases to websocketspec for protocol consistency
type (
	// Message represents an MQTT message (identical to WebSocket message protocol)
	Message = websocketspec.Message

	// MessageType defines the type of message
	MessageType = websocketspec.MessageType

	// OperationType defines the operation to perform
	OperationType = websocketspec.OperationType

	// ResponseMessage is sent back to clients after processing requests
	ResponseMessage = websocketspec.ResponseMessage

	// NotificationMessage is sent to subscribers when data changes
	NotificationMessage = websocketspec.NotificationMessage

	// ErrorInfo contains error details
	ErrorInfo = websocketspec.ErrorInfo
)

// Message type constants
const (
	MessageTypeRequest      = websocketspec.MessageTypeRequest
	MessageTypeResponse     = websocketspec.MessageTypeResponse
	MessageTypeNotification = websocketspec.MessageTypeNotification
	MessageTypeSubscription = websocketspec.MessageTypeSubscription
	MessageTypeError        = websocketspec.MessageTypeError
	MessageTypePing         = websocketspec.MessageTypePing
	MessageTypePong         = websocketspec.MessageTypePong
)

// Operation type constants
const (
	OperationRead        = websocketspec.OperationRead
	OperationCreate      = websocketspec.OperationCreate
	OperationUpdate      = websocketspec.OperationUpdate
	OperationDelete      = websocketspec.OperationDelete
	OperationSubscribe   = websocketspec.OperationSubscribe
	OperationUnsubscribe = websocketspec.OperationUnsubscribe
	OperationMeta        = websocketspec.OperationMeta
)

// Helper functions from websocketspec
var (
	// NewResponseMessage creates a new response message
	NewResponseMessage = websocketspec.NewResponseMessage

	// NewErrorResponse creates an error response
	NewErrorResponse = websocketspec.NewErrorResponse

	// NewNotificationMessage creates a notification message
	NewNotificationMessage = websocketspec.NewNotificationMessage

	// ParseMessage parses a JSON message into a Message struct
	ParseMessage = websocketspec.ParseMessage
)
