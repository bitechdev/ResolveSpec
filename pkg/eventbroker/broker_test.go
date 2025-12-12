package eventbroker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
		MaxEvents:  1000,
	})

	tests := []struct {
		name      string
		opts      Options
		wantError bool
	}{
		{
			name: "valid options",
			opts: Options{
				Provider:   provider,
				InstanceID: "test-instance",
				Mode:       ProcessingModeSync,
			},
			wantError: false,
		},
		{
			name: "missing provider",
			opts: Options{
				InstanceID: "test-instance",
			},
			wantError: true,
		},
		{
			name: "missing instance ID",
			opts: Options{
				Provider: provider,
			},
			wantError: true,
		},
		{
			name: "async mode with defaults",
			opts: Options{
				Provider:   provider,
				InstanceID: "test-instance",
				Mode:       ProcessingModeAsync,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker, err := NewBroker(tt.opts)
			if (err != nil) != tt.wantError {
				t.Errorf("NewBroker() error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil && broker == nil {
				t.Error("Expected non-nil broker")
			}
		})
	}
}

func TestBrokerStartStop(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, err := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
	})
	if err != nil {
		t.Fatalf("Failed to create broker: %v", err)
	}

	// Test Start
	if err := broker.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}

	// Test double start (should fail)
	if err := broker.Start(context.Background()); err == nil {
		t.Error("Expected error on double start")
	}

	// Test Stop
	if err := broker.Stop(context.Background()); err != nil {
		t.Fatalf("Failed to stop broker: %v", err)
	}

	// Test double stop (should not fail)
	if err := broker.Stop(context.Background()); err != nil {
		t.Error("Double stop should not fail")
	}
}

func TestBrokerPublishSync(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe to events
	called := false
	var receivedEvent *Event
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called = true
		receivedEvent = event
		return nil
	}))

	// Publish event
	event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
	err := broker.PublishSync(context.Background(), event)
	if err != nil {
		t.Fatalf("PublishSync failed: %v", err)
	}

	// Verify handler was called
	if !called {
		t.Error("Expected handler to be called")
	}
	if receivedEvent == nil || receivedEvent.ID != event.ID {
		t.Error("Expected to receive the published event")
	}

	// Verify event status
	if event.Status != EventStatusCompleted {
		t.Errorf("Expected status %s, got %s", EventStatusCompleted, event.Status)
	}
}

func TestBrokerPublishAsync(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:    provider,
		InstanceID:  "test-instance",
		Mode:        ProcessingModeAsync,
		WorkerCount: 2,
		BufferSize:  10,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe to events
	var callCount atomic.Int32
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		callCount.Add(1)
		return nil
	}))

	// Publish multiple events
	for i := 0; i < 5; i++ {
		event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
		if err := broker.PublishAsync(context.Background(), event); err != nil {
			t.Fatalf("PublishAsync failed: %v", err)
		}
	}

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	if callCount.Load() != 5 {
		t.Errorf("Expected 5 handler calls, got %d", callCount.Load())
	}
}

func TestBrokerPublishBeforeStart(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
	})

	event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
	err := broker.Publish(context.Background(), event)
	if err == nil {
		t.Error("Expected error when publishing before start")
	}
}

func TestBrokerHandlerError(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
		RetryPolicy: &RetryPolicy{
			MaxRetries:    2,
			InitialDelay:  10 * time.Millisecond,
			MaxDelay:      100 * time.Millisecond,
			BackoffFactor: 2.0,
		},
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe with failing handler
	var callCount atomic.Int32
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		callCount.Add(1)
		return errors.New("handler error")
	}))

	// Publish event
	event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
	err := broker.PublishSync(context.Background(), event)

	// Should fail after retries
	if err == nil {
		t.Error("Expected error from handler")
	}

	// Should have been called MaxRetries+1 times (initial + retries)
	if callCount.Load() != 3 {
		t.Errorf("Expected 3 calls (1 initial + 2 retries), got %d", callCount.Load())
	}

	// Event should be marked as failed
	if event.Status != EventStatusFailed {
		t.Errorf("Expected status %s, got %s", EventStatusFailed, event.Status)
	}
}

func TestBrokerMultipleHandlers(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe multiple handlers
	var called1, called2, called3 bool
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called1 = true
		return nil
	}))
	broker.Subscribe("test.event", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called2 = true
		return nil
	}))
	broker.Subscribe("*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called3 = true
		return nil
	}))

	// Publish event
	event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
	broker.PublishSync(context.Background(), event)

	// All handlers should be called
	if !called1 || !called2 || !called3 {
		t.Error("Expected all handlers to be called")
	}
}

func TestBrokerUnsubscribe(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe
	called := false
	id, _ := broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		called = true
		return nil
	}))

	// Unsubscribe
	if err := broker.Unsubscribe(id); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Publish event
	event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
	broker.PublishSync(context.Background(), event)

	// Handler should not be called
	if called {
		t.Error("Expected handler not to be called after unsubscribe")
	}
}

func TestBrokerStats(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "test-instance",
		Mode:       ProcessingModeSync,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	// Subscribe
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		return nil
	}))

	// Publish events
	for i := 0; i < 3; i++ {
		event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
		broker.PublishSync(context.Background(), event)
	}

	// Get stats
	stats, err := broker.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.InstanceID != "test-instance" {
		t.Errorf("Expected instance ID 'test-instance', got %s", stats.InstanceID)
	}
	if stats.TotalPublished != 3 {
		t.Errorf("Expected 3 published events, got %d", stats.TotalPublished)
	}
	if stats.TotalProcessed != 3 {
		t.Errorf("Expected 3 processed events, got %d", stats.TotalProcessed)
	}
	if stats.ActiveSubscribers != 1 {
		t.Errorf("Expected 1 active subscriber, got %d", stats.ActiveSubscribers)
	}
}

func TestBrokerInstanceID(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:   provider,
		InstanceID: "my-instance",
	})

	if broker.InstanceID() != "my-instance" {
		t.Errorf("Expected instance ID 'my-instance', got %s", broker.InstanceID())
	}
}

func TestBrokerConcurrentPublish(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:    provider,
		InstanceID:  "test-instance",
		Mode:        ProcessingModeAsync,
		WorkerCount: 5,
		BufferSize:  100,
	})
	broker.Start(context.Background())
	defer broker.Stop(context.Background())

	var callCount atomic.Int32
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		callCount.Add(1)
		return nil
	}))

	// Publish concurrently
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
			broker.PublishAsync(context.Background(), event)
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Wait for async processing

	if callCount.Load() != 50 {
		t.Errorf("Expected 50 handler calls, got %d", callCount.Load())
	}
}

func TestBrokerGracefulShutdown(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	broker, _ := NewBroker(Options{
		Provider:    provider,
		InstanceID:  "test-instance",
		Mode:        ProcessingModeAsync,
		WorkerCount: 2,
		BufferSize:  10,
	})
	broker.Start(context.Background())

	var processedCount atomic.Int32
	broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
		time.Sleep(50 * time.Millisecond) // Simulate work
		processedCount.Add(1)
		return nil
	}))

	// Publish events
	for i := 0; i < 5; i++ {
		event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
		broker.PublishAsync(context.Background(), event)
	}

	// Stop broker (should wait for events to be processed)
	if err := broker.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// All events should be processed
	if processedCount.Load() != 5 {
		t.Errorf("Expected 5 processed events, got %d", processedCount.Load())
	}
}

func TestBrokerDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", policy.MaxRetries)
	}
	if policy.InitialDelay != 1*time.Second {
		t.Errorf("Expected InitialDelay 1s, got %v", policy.InitialDelay)
	}
	if policy.BackoffFactor != 2.0 {
		t.Errorf("Expected BackoffFactor 2.0, got %f", policy.BackoffFactor)
	}
}

func TestBrokerProcessingModes(t *testing.T) {
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID: "test-instance",
	})

	tests := []struct {
		name string
		mode ProcessingMode
	}{
		{"sync mode", ProcessingModeSync},
		{"async mode", ProcessingModeAsync},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker, _ := NewBroker(Options{
				Provider:   provider,
				InstanceID: "test-instance",
				Mode:       tt.mode,
			})
			broker.Start(context.Background())
			defer broker.Stop(context.Background())

			called := false
			broker.Subscribe("test.*", EventHandlerFunc(func(ctx context.Context, event *Event) error {
				called = true
				return nil
			}))

			event := NewEvent(EventSourceSystem, "test.event")
	event.InstanceID = "test-instance"
			broker.Publish(context.Background(), event)

			if tt.mode == ProcessingModeAsync {
				time.Sleep(50 * time.Millisecond)
			}

			if !called {
				t.Error("Expected handler to be called")
			}
		})
	}
}
