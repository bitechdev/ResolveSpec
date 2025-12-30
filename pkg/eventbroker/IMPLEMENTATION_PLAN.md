# Event Broker System Implementation Plan

## Overview
Implement a comprehensive event handler/broker system for ResolveSpec that follows existing architectural patterns (Provider interface, Hook system, Config management, Graceful shutdown).

## Requirements Met
- ✅ Events with sources (database, websocket, frontend, system)
- ✅ Event statuses (pending, processing, completed, failed)
- ✅ Timestamps, JSON payloads, user IDs, session IDs
- ✅ Program instance IDs for tracking server instances
- ✅ Both sync and async processing modes
- ✅ Multiple provider backends (in-memory, Redis, NATS, database)
- ✅ Cross-instance pub/sub support

## Architecture

### Core Components

**Event Structure** (with full metadata):
```go
type Event struct {
    ID          string                 // UUID
    Source      EventSource            // database, websocket, system, frontend
    Type        string                 // Pattern: schema.entity.operation
    Status      EventStatus            // pending, processing, completed, failed
    Payload     json.RawMessage        // JSON payload
    UserID      int
    SessionID   string
    InstanceID  string                 // Server instance identifier
    Schema      string
    Entity      string
    Operation   string                 // create, update, delete, read
    CreatedAt   time.Time
    ProcessedAt *time.Time
    CompletedAt *time.Time
    Error       string
    Metadata    map[string]interface{}
    RetryCount  int
}
```

**Provider Pattern** (like cache.Provider):
```go
type Provider interface {
    Store(ctx context.Context, event *Event) error
    Get(ctx context.Context, id string) (*Event, error)
    List(ctx context.Context, filter *EventFilter) ([]*Event, error)
    UpdateStatus(ctx context.Context, id string, status EventStatus, error string) error
    Stream(ctx context.Context, pattern string) (<-chan *Event, error)
    Publish(ctx context.Context, event *Event) error
    Close() error
    Stats(ctx context.Context) (*ProviderStats, error)
}
```

**Broker Interface**:
```go
type Broker interface {
    Publish(ctx context.Context, event *Event) error        // Mode-dependent
    PublishSync(ctx context.Context, event *Event) error    // Blocks
    PublishAsync(ctx context.Context, event *Event) error   // Non-blocking
    Subscribe(pattern string, handler EventHandler) (SubscriptionID, error)
    Unsubscribe(id SubscriptionID) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Stats(ctx context.Context) (*BrokerStats, error)
}
```

## Implementation Steps

### Phase 1: Core Foundation (Files: 1-5)

**1. Create `pkg/eventbroker/event.go`**
- Event struct with all required fields (status, timestamps, user, instance ID, etc.)
- EventSource enum (database, websocket, frontend, system, internal)
- EventStatus enum (pending, processing, completed, failed)
- Helper: `EventType(schema, entity, operation string) string`
- Helper: `NewEvent()` constructor with UUID generation

**2. Create `pkg/eventbroker/provider.go`**
- Provider interface definition
- EventFilter struct for queries
- ProviderStats struct

**3. Create `pkg/eventbroker/handler.go`**
- EventHandler interface
- EventHandlerFunc adapter type

**4. Create `pkg/eventbroker/broker.go`**
- Broker interface definition
- EventBroker struct implementation
- ProcessingMode enum (sync, async)
- Options struct with functional options (WithProvider, WithMode, WithWorkerCount, etc.)
- NewBroker() constructor
- Sync processing implementation

**5. Create `pkg/eventbroker/subscription.go`**
- Pattern matching using glob syntax (e.g., "public.users.*", "*.*.create")
- subscriptionManager struct
- SubscriptionID type
- Subscribe/Unsubscribe logic

### Phase 2: Configuration & Integration (Files: 6-8)

**6. Create `pkg/eventbroker/config.go`**
- EventBrokerConfig struct
- RedisConfig, NATSConfig, DatabaseConfig structs
- RetryPolicyConfig struct

**7. Update `pkg/config/config.go`**
- Add `EventBroker EventBrokerConfig` field to Config struct

**8. Update `pkg/config/manager.go`**
- Add event broker defaults to `setDefaults()`:
  ```go
  v.SetDefault("event_broker.enabled", false)
  v.SetDefault("event_broker.provider", "memory")
  v.SetDefault("event_broker.mode", "async")
  v.SetDefault("event_broker.worker_count", 10)
  v.SetDefault("event_broker.buffer_size", 1000)
  ```

### Phase 3: Memory Provider (Files: 9)

**9. Create `pkg/eventbroker/provider_memory.go`**
- MemoryProvider struct with mutex-protected map
- In-memory event storage
- Pattern matching for subscriptions
- Channel-based streaming for real-time events
- LRU eviction when max size reached
- Cleanup goroutine for old completed events
- **Note**: Single-instance only (no cross-instance pub/sub)

### Phase 4: Async Processing (Update File: 4)

**10. Update `pkg/eventbroker/broker.go`** (add async support)
- workerPool struct with configurable worker count
- Buffered channel for event queue
- Worker goroutines that process events
- PublishAsync() queues to channel
- Graceful shutdown: stop accepting events, drain queue, wait for workers
- Retry logic with exponential backoff

### Phase 5: Hook Integration (Files: 11)

**11. Create `pkg/eventbroker/hooks.go`**
- `RegisterCRUDHooks(broker Broker, hookRegistry *restheadspec.HookRegistry)`
- Registers AfterCreate, AfterUpdate, AfterDelete, AfterRead hooks
- Extracts UserContext from hook context
- Creates Event with proper metadata
- Calls `broker.PublishAsync()` to not block CRUD operations

### Phase 6: Global Singleton & Factory (Files: 12-13)

**12. Create `pkg/eventbroker/eventbroker.go`**
- Global `defaultBroker` variable
- `Initialize(config *config.Config) error` - creates broker from config
- `SetDefaultBroker(broker Broker)`
- `GetDefaultBroker() Broker`
- Helper functions: `Publish()`, `PublishAsync()`, `PublishSync()`, `Subscribe()`
- `RegisterShutdown(broker Broker)` - registers with server.RegisterShutdownCallback()

**13. Create `pkg/eventbroker/factory.go`**
- `NewProviderFromConfig(config EventBrokerConfig) (Provider, error)`
- Provider selection logic (memory, redis, nats, database)
- Returns appropriate provider based on config

### Phase 7: Redis Provider (Files: 14)

**14. Create `pkg/eventbroker/provider_redis.go`**
- RedisProvider using Redis Streams (XADD, XREAD, XGROUP)
- Consumer group for distributed processing
- Cross-instance pub/sub support
- Stream(pattern) subscribes to consumer group
- Publish() uses XADD to append to stream
- Graceful shutdown: acknowledge pending messages

**Dependencies**: `github.com/redis/go-redis/v9`

### Phase 8: NATS Provider (Files: 15)

**15. Create `pkg/eventbroker/provider_nats.go`**
- NATSProvider using NATS JetStream
- Subject-based routing: `events.{source}.{type}`
- Wildcard subscriptions support
- Durable consumers for replay
- At-least-once delivery semantics

**Dependencies**: `github.com/nats-io/nats.go`

### Phase 9: Database Provider (Files: 16)

**16. Create `pkg/eventbroker/provider_database.go`**
- DatabaseProvider using `common.Database` interface
- Table schema creation (events table with indexes)
- Polling-based event consumption (configurable interval)
- Full SQL query support via List(filter)
- Transaction support for atomic operations
- Good for audit trails and debugging

### Phase 10: Metrics Integration (Files: 17)

**17. Create `pkg/eventbroker/metrics.go`**
- Integrate with existing `metrics.Provider`
- Record metrics:
  - `eventbroker_events_published_total{source, type}`
  - `eventbroker_events_processed_total{source, type, status}`
  - `eventbroker_event_processing_duration_seconds{source, type}`
  - `eventbroker_queue_size`
  - `eventbroker_workers_active`

**18. Update `pkg/metrics/interfaces.go`**
- Add methods to Provider interface:
  ```go
  RecordEventPublished(source, eventType string)
  RecordEventProcessed(source, eventType, status string, duration time.Duration)
  UpdateEventQueueSize(size int64)
  ```

### Phase 11: Testing & Examples (Files: 19-20)

**19. Create `pkg/eventbroker/eventbroker_test.go`**
- Unit tests for Event marshaling
- Pattern matching tests
- MemoryProvider tests
- Sync vs async mode tests
- Concurrent publish/subscribe tests
- Graceful shutdown tests

**20. Create `pkg/eventbroker/example_usage.go`**
- Basic publish example
- Subscribe with patterns example
- Hook integration example
- Multiple handlers example
- Error handling example

## Integration Points

### Hook System Integration
```go
// In application initialization (e.g., main.go)
eventbroker.RegisterCRUDHooks(broker, handler.Hooks())
```

This automatically publishes events for all CRUD operations:
- `schema.entity.create` after inserts
- `schema.entity.update` after updates
- `schema.entity.delete` after deletes
- `schema.entity.read` after reads

### Shutdown Integration
```go
// In application initialization
eventbroker.RegisterShutdown(broker)
```

Ensures event broker flushes pending events before shutdown.

### Configuration Example
```yaml
event_broker:
  enabled: true
  provider: redis  # memory, redis, nats, database
  mode: async      # sync, async
  worker_count: 10
  buffer_size: 1000
  instance_id: "${HOSTNAME}"

  redis:
    stream_name: "resolvespec:events"
    consumer_group: "resolvespec-workers"
    host: "localhost"
    port: 6379
```

## Usage Examples

### Publishing Custom Events
```go
// WebSocket event
event := &eventbroker.Event{
    Source:    eventbroker.EventSourceWebSocket,
    Type:      "chat.message",
    Payload:   json.RawMessage(`{"room": "lobby", "msg": "Hello"}`),
    UserID:    userID,
    SessionID: sessionID,
}
eventbroker.PublishAsync(ctx, event)
```

### Subscribing to Events
```go
// Subscribe to all user creation events
eventbroker.Subscribe("public.users.create", eventbroker.EventHandlerFunc(
    func(ctx context.Context, event *eventbroker.Event) error {
        log.Printf("New user created: %s", event.Payload)
        // Send welcome email, update cache, etc.
        return nil
    },
))

// Subscribe to all events from database
eventbroker.Subscribe("*", eventbroker.EventHandlerFunc(
    func(ctx context.Context, event *eventbroker.Event) error {
        if event.Source == eventbroker.EventSourceDatabase {
            // Audit logging
        }
        return nil
    },
))
```

## Critical Files Reference

**Patterns to Follow**:
- `pkg/cache/provider.go` - Provider interface pattern
- `pkg/restheadspec/hooks.go` - Hook system integration
- `pkg/config/manager.go` - Configuration pattern
- `pkg/server/shutdown.go` - Shutdown callbacks

**Files to Modify**:
- `pkg/config/config.go` - Add EventBroker field
- `pkg/config/manager.go` - Add defaults
- `pkg/metrics/interfaces.go` - Add event broker metrics

**New Package**:
- `pkg/eventbroker/` (20 files total)

## Provider Feature Matrix

| Feature | Memory | Redis | NATS | Database |
|---------|--------|-------|------|----------|
| Persistence | ❌ | ✅ | ✅ | ✅ |
| Cross-instance | ❌ | ✅ | ✅ | ✅ |
| Real-time | ✅ | ✅ | ✅ | ⚠️ (polling) |
| Query history | Limited | Limited | ✅ (replay) | ✅ (SQL) |
| External deps | None | Redis | NATS | None |
| Complexity | Low | Medium | Medium | Low |

## Implementation Order Priority

1. **Core + Memory Provider** (Phase 1-3) - Functional in-process event system
2. **Async + Hooks** (Phase 4-5) - Non-blocking event dispatch integrated with CRUD
3. **Config + Singleton** (Phase 6) - Easy initialization and usage
4. **Redis Provider** (Phase 7) - Production-ready distributed events
5. **Metrics** (Phase 10) - Observability
6. **NATS/Database** (Phase 8-9) - Alternative backends
7. **Tests + Examples** (Phase 11) - Documentation and reliability

## Next Steps

After approval, implement in order of phases. Each phase builds on previous phases and can be tested independently.
