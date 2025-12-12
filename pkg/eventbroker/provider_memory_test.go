package eventbroker

import (
	"context"
	"testing"
	"time"
)

func TestNewMemoryProvider(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID:      "test-instance",
		MaxEvents:       100,
		CleanupInterval: 1 * time.Minute,
	})

	if provider == nil {
		t.Fatal("Expected non-nil provider")
	}

	stats, err := provider.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.ProviderType != "memory" {
		t.Errorf("Expected provider type 'memory', got %s", stats.ProviderType)
	}
}

func TestMemoryProviderPublishAndGet(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	event := NewEvent(EventSourceDatabase, "public.users.create")
	event.UserID = 123

	// Publish event
	if err := provider.Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// Get event
	retrieved, err := provider.Get(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != event.ID {
		t.Errorf("Expected event ID %s, got %s", event.ID, retrieved.ID)
	}
	if retrieved.UserID != 123 {
		t.Errorf("Expected user ID 123, got %d", retrieved.UserID)
	}
}

func TestMemoryProviderGetNonExistent(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	_, err := provider.Get(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error when getting non-existent event")
	}
}

func TestMemoryProviderUpdateStatus(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	event := NewEvent(EventSourceDatabase, "public.users.create")
	provider.Publish(context.Background(), event)

	// Update status to processing
	err := provider.UpdateStatus(context.Background(), event.ID, EventStatusProcessing, "")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	retrieved, _ := provider.Get(context.Background(), event.ID)
	if retrieved.Status != EventStatusProcessing {
		t.Errorf("Expected status %s, got %s", EventStatusProcessing, retrieved.Status)
	}

	// Update status to failed with error
	err = provider.UpdateStatus(context.Background(), event.ID, EventStatusFailed, "test error")
	if err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	retrieved, _ = provider.Get(context.Background(), event.ID)
	if retrieved.Status != EventStatusFailed {
		t.Errorf("Expected status %s, got %s", EventStatusFailed, retrieved.Status)
	}
	if retrieved.Error != "test error" {
		t.Errorf("Expected error 'test error', got %s", retrieved.Error)
	}
}

func TestMemoryProviderList(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Publish multiple events
	for i := 0; i < 5; i++ {
		event := NewEvent(EventSourceDatabase, "public.users.create")
		provider.Publish(context.Background(), event)
	}

	// List all events
	events, err := provider.List(context.Background(), &EventFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events))
	}
}

func TestMemoryProviderListWithFilter(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Publish events with different types
	event1 := NewEvent(EventSourceDatabase, "public.users.create")
	provider.Publish(context.Background(), event1)

	event2 := NewEvent(EventSourceDatabase, "public.roles.create")
	provider.Publish(context.Background(), event2)

	event3 := NewEvent(EventSourceWebSocket, "chat.message")
	provider.Publish(context.Background(), event3)

	// Filter by source
	source := EventSourceDatabase
	events, err := provider.List(context.Background(), &EventFilter{
		Source: &source,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events with database source, got %d", len(events))
	}

	// Filter by status
	status := EventStatusPending
	events, err = provider.List(context.Background(), &EventFilter{
		Status: &status,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("Expected 3 events with pending status, got %d", len(events))
	}
}

func TestMemoryProviderListWithLimit(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Publish multiple events
	for i := 0; i < 10; i++ {
		event := NewEvent(EventSourceDatabase, "test.event")
		provider.Publish(context.Background(), event)
	}

	// List with limit
	events, err := provider.List(context.Background(), &EventFilter{
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("Expected 5 events (limited), got %d", len(events))
	}
}

func TestMemoryProviderDelete(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	event := NewEvent(EventSourceDatabase, "public.users.create")
	provider.Publish(context.Background(), event)

	// Delete event
	err := provider.Delete(context.Background(), event.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, err = provider.Get(context.Background(), event.ID)
	if err == nil {
		t.Error("Expected error when getting deleted event")
	}
}

func TestMemoryProviderLRUEviction(t *testing.T) {
	// Create provider with small max events
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
		MaxEvents:  3,
	})

	// Publish 5 events
	events := make([]*Event, 5)
	for i := 0; i < 5; i++ {
		events[i] = NewEvent(EventSourceDatabase, "test.event")
		provider.Publish(context.Background(), events[i])
	}

	// First 2 events should be evicted
	_, err := provider.Get(context.Background(), events[0].ID)
	if err == nil {
		t.Error("Expected first event to be evicted")
	}

	_, err = provider.Get(context.Background(), events[1].ID)
	if err == nil {
		t.Error("Expected second event to be evicted")
	}

	// Last 3 events should still exist
	for i := 2; i < 5; i++ {
		_, err := provider.Get(context.Background(), events[i].ID)
		if err != nil {
			t.Errorf("Expected event %d to still exist", i)
		}
	}
}

func TestMemoryProviderCleanup(t *testing.T) {
	// Create provider with short cleanup interval
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID:      "test-instance",
		CleanupInterval: 100 * time.Millisecond,
		MaxAge:          200 * time.Millisecond,
	})

	// Publish and complete an event
	event := NewEvent(EventSourceDatabase, "test.event")
	provider.Publish(context.Background(), event)
	provider.UpdateStatus(context.Background(), event.ID, EventStatusCompleted, "")

	// Wait for cleanup to run
	time.Sleep(400 * time.Millisecond)

	// Event should be cleaned up
	_, err := provider.Get(context.Background(), event.ID)
	if err == nil {
		t.Error("Expected event to be cleaned up")
	}

	provider.Close()
}

func TestMemoryProviderStats(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
		MaxEvents:  100,
	})

	// Publish events
	for i := 0; i < 5; i++ {
		event := NewEvent(EventSourceDatabase, "test.event")
		provider.Publish(context.Background(), event)
	}

	stats, err := provider.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.ProviderType != "memory" {
		t.Errorf("Expected provider type 'memory', got %s", stats.ProviderType)
	}
	if stats.TotalEvents != 5 {
		t.Errorf("Expected 5 total events, got %d", stats.TotalEvents)
	}
}

func TestMemoryProviderClose(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID:      "test-instance",
		CleanupInterval: 100 * time.Millisecond,
	})

	// Publish event
	event := NewEvent(EventSourceDatabase, "test.event")
	provider.Publish(context.Background(), event)

	// Close provider
	err := provider.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Cleanup goroutine should be stopped
	time.Sleep(200 * time.Millisecond)
}

func TestMemoryProviderConcurrency(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Concurrent publish
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			event := NewEvent(EventSourceDatabase, "test.event")
			provider.Publish(context.Background(), event)
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all events were stored
	events, _ := provider.List(context.Background(), &EventFilter{})
	if len(events) != 10 {
		t.Errorf("Expected 10 events, got %d", len(events))
	}
}

func TestMemoryProviderStream(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Stream is implemented for memory provider (in-process pub/sub)
	ch, err := provider.Stream(context.Background(), "test.*")
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if ch == nil {
		t.Error("Expected non-nil channel")
	}
}

func TestMemoryProviderTimeRangeFilter(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Publish events at different times
	event1 := NewEvent(EventSourceDatabase, "test.event")
	provider.Publish(context.Background(), event1)

	time.Sleep(10 * time.Millisecond)

	event2 := NewEvent(EventSourceDatabase, "test.event")
	provider.Publish(context.Background(), event2)

	time.Sleep(10 * time.Millisecond)

	event3 := NewEvent(EventSourceDatabase, "test.event")
	provider.Publish(context.Background(), event3)

	// Filter by time range
	startTime := event2.CreatedAt.Add(-1 * time.Millisecond)
	events, err := provider.List(context.Background(), &EventFilter{
		StartTime: &startTime,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should get events 2 and 3
	if len(events) != 2 {
		t.Errorf("Expected 2 events after start time, got %d", len(events))
	}
}

func TestMemoryProviderInstanceIDFilter(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	// Publish events with different instance IDs
	event1 := NewEvent(EventSourceDatabase, "test.event")
	event1.InstanceID = "instance-1"
	provider.Publish(context.Background(), event1)

	event2 := NewEvent(EventSourceDatabase, "test.event")
	event2.InstanceID = "instance-2"
	provider.Publish(context.Background(), event2)

	// Filter by instance ID
	events, err := provider.List(context.Background(), &EventFilter{
		InstanceID: "instance-1",
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 event with instance-1, got %d", len(events))
	}
	if events[0].InstanceID != "instance-1" {
		t.Errorf("Expected instance ID 'instance-1', got %s", events[0].InstanceID)
	}
}
