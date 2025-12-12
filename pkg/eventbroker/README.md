# Event Broker System

A comprehensive event handler/broker system for ResolveSpec that provides real-time event publishing, subscription, and cross-instance communication.

## Features

- **Multiple Sources**: Events from database, websockets, frontend, system, and internal sources
- **Event Status Tracking**: Pending, processing, completed, failed states with timestamps
- **Rich Metadata**: User IDs, session IDs, instance IDs, JSON payloads, and custom metadata
- **Sync & Async Modes**: Choose between synchronous or asynchronous event processing
- **Pattern Matching**: Subscribe to events using glob-style patterns
- **Multiple Providers**: In-memory, Redis Streams, NATS JetStream, PostgreSQL with NOTIFY
- **Hook Integration**: Automatic CRUD event capture via restheadspec hooks
- **Retry Logic**: Configurable retry policy with exponential backoff
- **Metrics**: Prometheus-compatible metrics for monitoring
- **Graceful Shutdown**: Proper cleanup and event flushing on shutdown

## Quick Start

### 1. Configuration

Add to your `config.yaml`:

```yaml
event_broker:
  enabled: true
  provider: memory  # memory, redis, nats, database
  mode: async       # sync, async
  worker_count: 10
  buffer_size: 1000
  instance_id: "${HOSTNAME}"
```

### 2. Initialize

```go
import (
	"github.com/bitechdev/ResolveSpec/pkg/config"
	"github.com/bitechdev/ResolveSpec/pkg/eventbroker"
)

func main() {
	// Load configuration
	cfgMgr := config.NewManager()
	cfg, _ := cfgMgr.GetConfig()

	// Initialize event broker
	if err := eventbroker.Initialize(cfg.EventBroker); err != nil {
		log.Fatal(err)
	}
}
```

### 3. Subscribe to Events

```go
// Subscribe to specific events
eventbroker.Subscribe("public.users.create", eventbroker.EventHandlerFunc(
	func(ctx context.Context, event *eventbroker.Event) error {
		log.Printf("New user created: %s", event.Payload)
		// Send welcome email, update cache, etc.
		return nil
	},
))

// Subscribe with patterns
eventbroker.Subscribe("*.*.delete", eventbroker.EventHandlerFunc(
	func(ctx context.Context, event *eventbroker.Event) error {
		log.Printf("Deleted: %s.%s", event.Schema, event.Entity)
		return nil
	},
))
```

### 4. Publish Events

```go
// Create and publish an event
event := eventbroker.NewEvent(eventbroker.EventSourceDatabase, "public.users.update")
event.InstanceID = eventbroker.GetDefaultBroker().InstanceID()
event.UserID = 123
event.SessionID = "session-456"
event.Schema = "public"
event.Entity = "users"
event.Operation = "update"

event.SetPayload(map[string]interface{}{
	"id": 123,
	"name": "John Doe",
})

// Async (non-blocking)
eventbroker.PublishAsync(ctx, event)

// Sync (blocking)
eventbroker.PublishSync(ctx, event)
```

## Automatic CRUD Event Capture

Automatically capture database CRUD operations:

```go
import (
	"github.com/bitechdev/ResolveSpec/pkg/eventbroker"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

func setupHooks(handler *restheadspec.Handler) {
	broker := eventbroker.GetDefaultBroker()

	// Configure which operations to capture
	config := eventbroker.DefaultCRUDHookConfig()
	config.EnableRead = false // Disable read events for performance

	// Register hooks
	eventbroker.RegisterCRUDHooks(broker, handler.Hooks(), config)

	// Now all create/update/delete operations automatically publish events!
}
```

## Event Structure

Every event contains:

```go
type Event struct {
	ID          string                 // UUID
	Source      EventSource            // database, websocket, system, frontend, internal
	Type        string                 // Pattern: schema.entity.operation
	Status      EventStatus            // pending, processing, completed, failed
	Payload     json.RawMessage        // JSON payload
	UserID      int                    // User who triggered the event
	SessionID   string                 // Session identifier
	InstanceID  string                 // Server instance identifier
	Schema      string                 // Database schema
	Entity      string                 // Database entity/table
	Operation   string                 // create, update, delete, read
	CreatedAt   time.Time              // When event was created
	ProcessedAt *time.Time             // When processing started
	CompletedAt *time.Time             // When processing completed
	Error       string                 // Error message if failed
	Metadata    map[string]interface{} // Additional context
	RetryCount  int                    // Number of retry attempts
}
```

## Pattern Matching

Subscribe to events using glob-style patterns:

| Pattern | Matches | Example |
|---------|---------|---------|
| `*` | All events | Any event |
| `public.users.*` | All user operations | `public.users.create`, `public.users.update` |
| `*.*.create` | All create operations | `public.users.create`, `auth.sessions.create` |
| `public.*.*` | All events in public schema | `public.users.create`, `public.posts.delete` |
| `public.users.create` | Exact match | Only `public.users.create` |

## Providers

### Memory Provider (Default)

Best for: Development, single-instance deployments

- **Pros**: Fast, no dependencies, simple
- **Cons**: Events lost on restart, single-instance only

```yaml
event_broker:
  provider: memory
```

### Redis Provider (Future)

Best for: Production, multi-instance deployments

- **Pros**: Persistent, cross-instance pub/sub, reliable
- **Cons**: Requires Redis

```yaml
event_broker:
  provider: redis
  redis:
    stream_name: "resolvespec:events"
    consumer_group: "resolvespec-workers"
    host: "localhost"
    port: 6379
```

### NATS Provider (Future)

Best for: High-performance, low-latency requirements

- **Pros**: Very fast, built-in clustering, durable
- **Cons**: Requires NATS server

```yaml
event_broker:
  provider: nats
  nats:
    url: "nats://localhost:4222"
    stream_name: "RESOLVESPEC_EVENTS"
```

### Database Provider (Future)

Best for: Audit trails, event replay, SQL queries

- **Pros**: No additional infrastructure, full SQL query support, PostgreSQL NOTIFY for real-time
- **Cons**: Slower than Redis/NATS

```yaml
event_broker:
  provider: database
  database:
    table_name: "events"
    channel: "resolvespec_events"
```

## Processing Modes

### Async Mode (Recommended)

Events are queued and processed by worker pool:

- Non-blocking event publishing
- Configurable worker count
- Better throughput
- Events may be processed out of order

```yaml
event_broker:
  mode: async
  worker_count: 10
  buffer_size: 1000
```

### Sync Mode

Events are processed immediately:

- Blocking event publishing
- Guaranteed ordering
- Immediate error feedback
- Lower throughput

```yaml
event_broker:
  mode: sync
```

## Retry Policy

Configure automatic retries for failed handlers:

```yaml
event_broker:
  retry_policy:
    max_retries: 3
    initial_delay: 1s
    max_delay: 30s
    backoff_factor: 2.0  # Exponential backoff
```

## Metrics

The event broker exposes Prometheus metrics:

- `eventbroker_events_published_total{source, type}` - Total events published
- `eventbroker_events_processed_total{source, type, status}` - Total events processed
- `eventbroker_event_processing_duration_seconds{source, type}` - Event processing duration
- `eventbroker_queue_size` - Current queue size (async mode)

## Best Practices

1. **Use Async Mode**: For better performance, use async mode in production
2. **Disable Read Events**: Read events can be high volume; disable if not needed
3. **Pattern Matching**: Use specific patterns to avoid processing unnecessary events
4. **Error Handling**: Always handle errors in event handlers; they won't fail the original operation
5. **Idempotency**: Make handlers idempotent as events may be retried
6. **Payload Size**: Keep payloads reasonable; avoid large objects
7. **Monitoring**: Monitor metrics to detect issues early

## Examples

See `example_usage.go` for comprehensive examples including:
- Basic event publishing and subscription
- Hook integration
- Error handling
- Configuration
- Pattern matching

## Architecture

```
┌─────────────────┐
│   Application   │
└────────┬────────┘
         │
         ├─ Publish Events
         │
┌────────▼────────┐      ┌──────────────┐
│  Event Broker   │◄────►│  Subscribers │
└────────┬────────┘      └──────────────┘
         │
         ├─ Store Events
         │
┌────────▼────────┐
│    Provider     │
│  (Memory/Redis  │
│   /NATS/DB)     │
└─────────────────┘
```

## Future Enhancements

- [ ] Database Provider with PostgreSQL NOTIFY
- [ ] Redis Streams Provider
- [ ] NATS JetStream Provider
- [ ] Event replay functionality
- [ ] Dead letter queue
- [ ] Event filtering at provider level
- [ ] Batch publishing
- [ ] Event compression
- [ ] Schema versioning
