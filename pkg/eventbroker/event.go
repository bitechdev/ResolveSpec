package eventbroker

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EventSource represents where an event originated from
type EventSource string

const (
	EventSourceDatabase  EventSource = "database"
	EventSourceWebSocket EventSource = "websocket"
	EventSourceFrontend  EventSource = "frontend"
	EventSourceSystem    EventSource = "system"
	EventSourceInternal  EventSource = "internal"
)

// EventStatus represents the current state of an event
type EventStatus string

const (
	EventStatusPending    EventStatus = "pending"
	EventStatusProcessing EventStatus = "processing"
	EventStatusCompleted  EventStatus = "completed"
	EventStatusFailed     EventStatus = "failed"
)

// Event represents a single event in the system with complete metadata
type Event struct {
	// Identification
	ID string `json:"id" db:"id"`

	// Source & Classification
	Source EventSource `json:"source" db:"source"`
	Type   string      `json:"type" db:"type"` // Pattern: schema.entity.operation

	// Status Tracking
	Status     EventStatus `json:"status" db:"status"`
	RetryCount int         `json:"retry_count" db:"retry_count"`
	Error      string      `json:"error,omitempty" db:"error"`

	// Payload
	Payload json.RawMessage `json:"payload" db:"payload"`

	// Context Information
	UserID     int    `json:"user_id" db:"user_id"`
	SessionID  string `json:"session_id" db:"session_id"`
	InstanceID string `json:"instance_id" db:"instance_id"`

	// Database Context
	Schema    string `json:"schema" db:"schema"`
	Entity    string `json:"entity" db:"entity"`
	Operation string `json:"operation" db:"operation"` // create, update, delete, read

	// Timestamps
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty" db:"processed_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`

	// Extensibility
	Metadata map[string]interface{} `json:"metadata" db:"metadata"`
}

// NewEvent creates a new event with defaults
func NewEvent(source EventSource, eventType string) *Event {
	return &Event{
		ID:         uuid.New().String(),
		Source:     source,
		Type:       eventType,
		Status:     EventStatusPending,
		CreatedAt:  time.Now(),
		Metadata:   make(map[string]interface{}),
		RetryCount: 0,
	}
}

// EventType generates a type string from schema, entity, and operation
// Pattern: schema.entity.operation (e.g., "public.users.create")
func EventType(schema, entity, operation string) string {
	return fmt.Sprintf("%s.%s.%s", schema, entity, operation)
}

// MarkProcessing marks the event as being processed
func (e *Event) MarkProcessing() {
	e.Status = EventStatusProcessing
	now := time.Now()
	e.ProcessedAt = &now
}

// MarkCompleted marks the event as successfully completed
func (e *Event) MarkCompleted() {
	e.Status = EventStatusCompleted
	now := time.Now()
	e.CompletedAt = &now
}

// MarkFailed marks the event as failed with an error message
func (e *Event) MarkFailed(err error) {
	e.Status = EventStatusFailed
	e.Error = err.Error()
	now := time.Now()
	e.CompletedAt = &now
}

// IncrementRetry increments the retry counter
func (e *Event) IncrementRetry() {
	e.RetryCount++
}

// SetPayload sets the event payload from any value by marshaling to JSON
func (e *Event) SetPayload(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	e.Payload = data
	return nil
}

// GetPayload unmarshals the payload into the provided value
func (e *Event) GetPayload(v interface{}) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("payload is empty")
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}
	return nil
}

// Clone creates a deep copy of the event
func (e *Event) Clone() *Event {
	clone := *e

	// Deep copy metadata
	if e.Metadata != nil {
		clone.Metadata = make(map[string]interface{})
		for k, v := range e.Metadata {
			clone.Metadata[k] = v
		}
	}

	// Deep copy timestamps
	if e.ProcessedAt != nil {
		t := *e.ProcessedAt
		clone.ProcessedAt = &t
	}
	if e.CompletedAt != nil {
		t := *e.CompletedAt
		clone.CompletedAt = &t
	}

	return &clone
}

// Validate performs basic validation on the event
func (e *Event) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("event ID is required")
	}
	if e.Source == "" {
		return fmt.Errorf("event source is required")
	}
	if e.Type == "" {
		return fmt.Errorf("event type is required")
	}
	if e.InstanceID == "" {
		return fmt.Errorf("instance ID is required")
	}
	return nil
}
