package eventbroker

import (
	"context"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern   string
		eventType string
		expected  bool
	}{
		// Exact matches
		{"public.users.create", "public.users.create", true},
		{"public.users.create", "public.users.update", false},

		// Wildcard matches
		{"*", "public.users.create", true},
		{"*", "anything", true},
		{"public.*", "public.users", true},
		{"public.*", "public.users.create", false}, // Different number of parts
		{"public.*", "admin.users", false},
		{"*.users.create", "public.users.create", true},
		{"*.users.create", "admin.users.create", true},
		{"*.users.create", "public.roles.create", false},
		{"public.*.create", "public.users.create", true},
		{"public.*.create", "public.roles.create", true},
		{"public.*.create", "public.users.update", false},

		// Multiple wildcards
		{"*.*", "public.users", true},
		{"*.*", "public.users.create", false}, // Different number of parts
		{"*.*.create", "public.users.create", true},
		{"*.*.create", "admin.roles.create", true},
		{"*.*.create", "public.users.update", false},

		// Edge cases
		{"", "", true},
		{"", "something", false},
		{"something", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.eventType, func(t *testing.T) {
			result := matchPattern(tt.pattern, tt.eventType)
			if result != tt.expected {
				t.Errorf("matchPattern(%q, %q) = %v, expected %v",
					tt.pattern, tt.eventType, result, tt.expected)
			}
		})
	}
}

func TestSubscriptionManager(t *testing.T) {
	manager := newSubscriptionManager()

	// Create test handler
	called := false
	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called = true
		return nil
	})

	// Test Subscribe
	id, err := manager.Subscribe("public.users.*", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if id == "" {
		t.Fatal("Expected non-empty subscription ID")
	}

	// Test GetMatching
	handlers := manager.GetMatching("public.users.create")
	if len(handlers) != 1 {
		t.Fatalf("Expected 1 handler, got %d", len(handlers))
	}

	// Test handler execution
	event := NewEvent(EventSourceDatabase, "public.users.create")
	if err := handlers[0].Handle(context.Background(), event); err != nil {
		t.Fatalf("Handler execution failed: %v", err)
	}
	if !called {
		t.Error("Expected handler to be called")
	}

	// Test Count
	if manager.Count() != 1 {
		t.Errorf("Expected count 1, got %d", manager.Count())
	}

	// Test Unsubscribe
	if err := manager.Unsubscribe(id); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Verify unsubscribed
	handlers = manager.GetMatching("public.users.create")
	if len(handlers) != 0 {
		t.Errorf("Expected 0 handlers after unsubscribe, got %d", len(handlers))
	}
	if manager.Count() != 0 {
		t.Errorf("Expected count 0 after unsubscribe, got %d", manager.Count())
	}
}

func TestSubscriptionManagerMultipleHandlers(t *testing.T) {
	manager := newSubscriptionManager()

	called1 := false
	handler1 := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called1 = true
		return nil
	})

	called2 := false
	handler2 := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called2 = true
		return nil
	})

	// Subscribe multiple handlers
	id1, _ := manager.Subscribe("public.users.*", handler1)
	id2, _ := manager.Subscribe("*.users.*", handler2)

	// Both should match
	handlers := manager.GetMatching("public.users.create")
	if len(handlers) != 2 {
		t.Fatalf("Expected 2 handlers, got %d", len(handlers))
	}

	// Execute all handlers
	event := NewEvent(EventSourceDatabase, "public.users.create")
	for _, h := range handlers {
		h.Handle(context.Background(), event)
	}

	if !called1 || !called2 {
		t.Error("Expected both handlers to be called")
	}

	// Unsubscribe one
	manager.Unsubscribe(id1)
	handlers = manager.GetMatching("public.users.create")
	if len(handlers) != 1 {
		t.Errorf("Expected 1 handler after unsubscribe, got %d", len(handlers))
	}

	// Unsubscribe remaining
	manager.Unsubscribe(id2)
	if manager.Count() != 0 {
		t.Errorf("Expected count 0 after all unsubscribe, got %d", manager.Count())
	}
}

func TestSubscriptionManagerConcurrency(t *testing.T) {
	manager := newSubscriptionManager()

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		return nil
	})

	// Subscribe and unsubscribe concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			id, _ := manager.Subscribe("test.*", handler)
			manager.GetMatching("test.event")
			manager.Unsubscribe(id)
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have no subscriptions left
	if manager.Count() != 0 {
		t.Errorf("Expected count 0 after concurrent operations, got %d", manager.Count())
	}
}

func TestSubscriptionManagerUnsubscribeNonExistent(t *testing.T) {
	manager := newSubscriptionManager()

	// Try to unsubscribe a non-existent ID
	err := manager.Unsubscribe("non-existent-id")
	if err == nil {
		t.Error("Expected error when unsubscribing non-existent ID")
	}
}

func TestSubscriptionIDGeneration(t *testing.T) {
	manager := newSubscriptionManager()

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		return nil
	})

	// Subscribe multiple times and ensure unique IDs
	ids := make(map[SubscriptionID]bool)
	for i := 0; i < 100; i++ {
		id, _ := manager.Subscribe("test.*", handler)
		if ids[id] {
			t.Fatalf("Duplicate subscription ID: %s", id)
		}
		ids[id] = true
	}
}

func TestEventHandlerFunc(t *testing.T) {
	called := false
	var receivedEvent *Event

	handler := EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called = true
		receivedEvent = event
		return nil
	})

	event := NewEvent(EventSourceDatabase, "test.event")
	err := handler.Handle(context.Background(), event)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !called {
		t.Error("Expected handler to be called")
	}
	if receivedEvent != event {
		t.Error("Expected to receive the same event")
	}
}

func TestSubscriptionManagerPatternPriority(t *testing.T) {
	manager := newSubscriptionManager()

	// More specific patterns should still match
	specificCalled := false
	genericCalled := false

	manager.Subscribe("public.users.create", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		specificCalled = true
		return nil
	}))

	manager.Subscribe("*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		genericCalled = true
		return nil
	}))

	handlers := manager.GetMatching("public.users.create")
	if len(handlers) != 2 {
		t.Fatalf("Expected 2 matching handlers, got %d", len(handlers))
	}

	// Execute all handlers
	event := NewEvent(EventSourceDatabase, "public.users.create")
	for _, h := range handlers {
		h.Handle(context.Background(), event)
	}

	if !specificCalled || !genericCalled {
		t.Error("Expected both specific and generic handlers to be called")
	}
}
