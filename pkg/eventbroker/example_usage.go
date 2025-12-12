// nolint
package eventbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Example demonstrates basic usage of the event broker
func Example() {
	// 1. Create a memory provider
	provider := NewMemoryProvider(MemoryProviderOptions{
		InstanceID:      "example-instance",
		MaxEvents:       1000,
		CleanupInterval: 5 * time.Minute,
		MaxAge:          1 * time.Hour,
	})

	// 2. Create a broker
	broker, err := NewBroker(Options{
		Provider:    provider,
		Mode:        ProcessingModeAsync,
		WorkerCount: 5,
		BufferSize:  100,
		RetryPolicy: DefaultRetryPolicy(),
		InstanceID:  "example-instance",
	})
	if err != nil {
		logger.Error("Failed to create broker: %v", err)
		return
	}

	// 3. Start the broker
	if err := broker.Start(context.Background()); err != nil {
		logger.Error("Failed to start broker: %v", err)
		return
	}
	defer func() {
		err := broker.Stop(context.Background())
		if err != nil {
			logger.Error("Failed to stop broker: %v", err)
		}
	}()

	// 4. Subscribe to events
	broker.Subscribe("public.users.*", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			logger.Info("User event: %s (operation: %s)", event.Type, event.Operation)
			return nil
		},
	))

	broker.Subscribe("*.*.create", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			logger.Info("Create event: %s.%s", event.Schema, event.Entity)
			return nil
		},
	))

	// 5. Publish events
	ctx := context.Background()

	// Database event
	dbEvent := NewEvent(EventSourceDatabase, EventType("public", "users", "create"))
	dbEvent.InstanceID = "example-instance"
	dbEvent.UserID = 123
	dbEvent.SessionID = "session-456"
	dbEvent.Schema = "public"
	dbEvent.Entity = "users"
	dbEvent.Operation = "create"
	dbEvent.SetPayload(map[string]interface{}{
		"id":    123,
		"name":  "John Doe",
		"email": "john@example.com",
	})

	if err := broker.PublishAsync(ctx, dbEvent); err != nil {
		logger.Error("Failed to publish event: %v", err)
	}

	// WebSocket event
	wsEvent := NewEvent(EventSourceWebSocket, "chat.message")
	wsEvent.InstanceID = "example-instance"
	wsEvent.UserID = 123
	wsEvent.SessionID = "session-456"
	wsEvent.SetPayload(map[string]interface{}{
		"room":    "general",
		"message": "Hello, World!",
	})

	if err := broker.PublishAsync(ctx, wsEvent); err != nil {
		logger.Error("Failed to publish event: %v", err)
	}

	// 6. Get statistics
	time.Sleep(1 * time.Second) // Wait for processing
	stats, _ := broker.Stats(ctx)
	logger.Info("Broker stats: %d published, %d processed", stats.TotalPublished, stats.TotalProcessed)
}

// ExampleWithHooks demonstrates integration with the hook system
func ExampleWithHooks() {
	// This would typically be called in your main.go or initialization code
	// after setting up your restheadspec.Handler

	// Pseudo-code (actual implementation would use real handler):
	/*
		broker := eventbroker.GetDefaultBroker()
		hookRegistry := handler.Hooks()

		// Register CRUD hooks
		config := eventbroker.DefaultCRUDHookConfig()
		config.EnableRead = false // Disable read events for performance

		if err := eventbroker.RegisterCRUDHooks(broker, hookRegistry, config); err != nil {
			logger.Error("Failed to register CRUD hooks: %v", err)
		}

		// Now all CRUD operations will automatically publish events
	*/
}

// ExampleSubscriptionPatterns demonstrates different subscription patterns
func ExampleSubscriptionPatterns() {
	broker := GetDefaultBroker()
	if broker == nil {
		return
	}

	// Pattern 1: Subscribe to all events from a specific entity
	broker.Subscribe("public.users.*", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			fmt.Printf("User event: %s\n", event.Operation)
			return nil
		},
	))

	// Pattern 2: Subscribe to a specific operation across all entities
	broker.Subscribe("*.*.create", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			fmt.Printf("Create event: %s.%s\n", event.Schema, event.Entity)
			return nil
		},
	))

	// Pattern 3: Subscribe to all events in a schema
	broker.Subscribe("public.*.*", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			fmt.Printf("Public schema event: %s.%s\n", event.Entity, event.Operation)
			return nil
		},
	))

	// Pattern 4: Subscribe to everything (use with caution)
	broker.Subscribe("*", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			fmt.Printf("Any event: %s\n", event.Type)
			return nil
		},
	))
}

// ExampleErrorHandling demonstrates error handling in event handlers
func ExampleErrorHandling() {
	broker := GetDefaultBroker()
	if broker == nil {
		return
	}

	// Handler that may fail
	broker.Subscribe("public.users.create", EventHandlerFunc(
		func(ctx context.Context, event *Event) error {
			// Simulate processing
			var user struct {
				ID    int    `json:"id"`
				Email string `json:"email"`
			}

			if err := event.GetPayload(&user); err != nil {
				return fmt.Errorf("invalid payload: %w", err)
			}

			// Validate
			if user.Email == "" {
				return fmt.Errorf("email is required")
			}

			// Process (e.g., send email)
			logger.Info("Sending welcome email to %s", user.Email)

			return nil
		},
	))
}

// ExampleConfiguration demonstrates initializing from configuration
func ExampleConfiguration() {
	// This would typically be in your main.go

	// Pseudo-code:
	/*
		// Load configuration
		cfgMgr := config.NewManager()
		if err := cfgMgr.Load(); err != nil {
			logger.Fatal("Failed to load config: %v", err)
		}

		cfg, err := cfgMgr.GetConfig()
		if err != nil {
			logger.Fatal("Failed to get config: %v", err)
		}

		// Initialize event broker
		if err := eventbroker.Initialize(cfg.EventBroker); err != nil {
			logger.Fatal("Failed to initialize event broker: %v", err)
		}

		// Use the default broker
		eventbroker.Subscribe("*.*.create", eventbroker.EventHandlerFunc(
			func(ctx context.Context, event *eventbroker.Event) error {
				logger.Info("Created: %s.%s", event.Schema, event.Entity)
				return nil
			},
		))
	*/
}

// ExampleYAMLConfiguration shows example YAML configuration
const ExampleYAMLConfiguration = `
event_broker:
  enabled: true
  provider: memory  # memory, redis, nats, database
  mode: async       # sync, async
  worker_count: 10
  buffer_size: 1000
  instance_id: "${HOSTNAME}"

  # Memory provider is default, no additional config needed

  # Redis provider (when provider: redis)
  redis:
    stream_name: "resolvespec:events"
    consumer_group: "resolvespec-workers"
    host: "localhost"
    port: 6379

  # NATS provider (when provider: nats)
  nats:
    url: "nats://localhost:4222"
    stream_name: "RESOLVESPEC_EVENTS"

  # Database provider (when provider: database)
  database:
    table_name: "events"
    channel: "resolvespec_events"

  # Retry policy
  retry_policy:
    max_retries: 3
    initial_delay: 1s
    max_delay: 30s
    backoff_factor: 2.0
`
