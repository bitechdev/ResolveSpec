package eventbroker

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")

	if event.ID == "" {
		t.Error("Expected event ID to be generated")
	}
	if event.Source != EventSourceDatabase {
		t.Errorf("Expected source %s, got %s", EventSourceDatabase, event.Source)
	}
	if event.Type != "public.users.create" {
		t.Errorf("Expected type 'public.users.create', got %s", event.Type)
	}
	if event.Status != EventStatusPending {
		t.Errorf("Expected status %s, got %s", EventStatusPending, event.Status)
	}
	if event.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if event.Metadata == nil {
		t.Error("Expected Metadata to be initialized")
	}
}

func TestEventType(t *testing.T) {
	tests := []struct {
		schema    string
		entity    string
		operation string
		expected  string
	}{
		{"public", "users", "create", "public.users.create"},
		{"admin", "roles", "update", "admin.roles.update"},
		{"", "system", "start", ".system.start"}, // Empty schema results in leading dot
	}

	for _, tt := range tests {
		result := EventType(tt.schema, tt.entity, tt.operation)
		if result != tt.expected {
			t.Errorf("EventType(%q, %q, %q) = %q, expected %q",
				tt.schema, tt.entity, tt.operation, result, tt.expected)
		}
	}
}

func TestEventValidate(t *testing.T) {
	tests := []struct {
		name      string
		event     *Event
		wantError bool
	}{
		{
			name: "valid event",
			event: func() *Event {
				e := NewEvent(EventSourceDatabase, "public.users.create")
				e.InstanceID = "test-instance"
				return e
			}(),
			wantError: false,
		},
		{
			name: "missing ID",
			event: &Event{
				Source: EventSourceDatabase,
				Type:   "public.users.create",
				Status: EventStatusPending,
			},
			wantError: true,
		},
		{
			name: "missing source",
			event: &Event{
				ID:     "test-id",
				Type:   "public.users.create",
				Status: EventStatusPending,
			},
			wantError: true,
		},
		{
			name: "missing type",
			event: &Event{
				ID:     "test-id",
				Source: EventSourceDatabase,
				Status: EventStatusPending,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Event.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestEventSetPayload(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")

	payload := map[string]interface{}{
		"id":   1,
		"name": "John Doe",
		"email": "john@example.com",
	}

	err := event.SetPayload(payload)
	if err != nil {
		t.Fatalf("SetPayload failed: %v", err)
	}

	if event.Payload == nil {
		t.Fatal("Expected payload to be set")
	}

	// Verify payload can be unmarshaled
	var result map[string]interface{}
	if err := json.Unmarshal(event.Payload, &result); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	if result["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", result["name"])
	}
}

func TestEventGetPayload(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")

	payload := map[string]interface{}{
		"id":   float64(1), // JSON unmarshals numbers as float64
		"name": "John Doe",
	}

	if err := event.SetPayload(payload); err != nil {
		t.Fatalf("SetPayload failed: %v", err)
	}

	var result map[string]interface{}
	if err := event.GetPayload(&result); err != nil {
		t.Fatalf("GetPayload failed: %v", err)
	}

	if result["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", result["name"])
	}
}

func TestEventMarkProcessing(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")
	event.MarkProcessing()

	if event.Status != EventStatusProcessing {
		t.Errorf("Expected status %s, got %s", EventStatusProcessing, event.Status)
	}
	if event.ProcessedAt == nil {
		t.Error("Expected ProcessedAt to be set")
	}
}

func TestEventMarkCompleted(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")
	event.MarkCompleted()

	if event.Status != EventStatusCompleted {
		t.Errorf("Expected status %s, got %s", EventStatusCompleted, event.Status)
	}
	if event.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

func TestEventMarkFailed(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")
	testErr := errors.New("test error")
	event.MarkFailed(testErr)

	if event.Status != EventStatusFailed {
		t.Errorf("Expected status %s, got %s", EventStatusFailed, event.Status)
	}
	if event.Error != "test error" {
		t.Errorf("Expected error %q, got %q", "test error", event.Error)
	}
	if event.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

func TestEventIncrementRetry(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")

	initialCount := event.RetryCount
	event.IncrementRetry()

	if event.RetryCount != initialCount+1 {
		t.Errorf("Expected retry count %d, got %d", initialCount+1, event.RetryCount)
	}
}

func TestEventJSONMarshaling(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")
	event.UserID = 123
	event.SessionID = "session-123"
	event.InstanceID = "instance-1"
	event.Schema = "public"
	event.Entity = "users"
	event.Operation = "create"
	event.SetPayload(map[string]interface{}{"name": "Test"})

	// Marshal to JSON
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Unmarshal back
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	// Verify fields
	if decoded.ID != event.ID {
		t.Errorf("Expected ID %s, got %s", event.ID, decoded.ID)
	}
	if decoded.Source != event.Source {
		t.Errorf("Expected source %s, got %s", event.Source, decoded.Source)
	}
	if decoded.UserID != event.UserID {
		t.Errorf("Expected UserID %d, got %d", event.UserID, decoded.UserID)
	}
}

func TestEventStatusString(t *testing.T) {
	statuses := []EventStatus{
		EventStatusPending,
		EventStatusProcessing,
		EventStatusCompleted,
		EventStatusFailed,
	}

	for _, status := range statuses {
		if string(status) == "" {
			t.Errorf("EventStatus %v has empty string representation", status)
		}
	}
}

func TestEventSourceString(t *testing.T) {
	sources := []EventSource{
		EventSourceDatabase,
		EventSourceWebSocket,
		EventSourceFrontend,
		EventSourceSystem,
		EventSourceInternal,
	}

	for _, source := range sources {
		if string(source) == "" {
			t.Errorf("EventSource %v has empty string representation", source)
		}
	}
}

func TestEventMetadata(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")

	// Test setting metadata
	event.Metadata["key1"] = "value1"
	event.Metadata["key2"] = 123

	if event.Metadata["key1"] != "value1" {
		t.Errorf("Expected metadata key1 to be 'value1', got %v", event.Metadata["key1"])
	}
	if event.Metadata["key2"] != 123 {
		t.Errorf("Expected metadata key2 to be 123, got %v", event.Metadata["key2"])
	}
}

func TestEventTimestamps(t *testing.T) {
	event := NewEvent(EventSourceDatabase, "public.users.create")
	createdAt := event.CreatedAt

	// Wait a tiny bit to ensure timestamps differ
	time.Sleep(time.Millisecond)

	event.MarkProcessing()
	if event.ProcessedAt == nil {
		t.Fatal("ProcessedAt should be set")
	}
	if !event.ProcessedAt.After(createdAt) {
		t.Error("ProcessedAt should be after CreatedAt")
	}

	time.Sleep(time.Millisecond)

	event.MarkCompleted()
	if event.CompletedAt == nil {
		t.Fatal("CompletedAt should be set")
	}
	if !event.CompletedAt.After(*event.ProcessedAt) {
		t.Error("CompletedAt should be after ProcessedAt")
	}
}
