# PostgreSQL NOTIFY/LISTEN Support

The `dbmanager` package provides built-in support for PostgreSQL's NOTIFY/LISTEN functionality through the `PostgresListener` type.

## Overview

PostgreSQL NOTIFY/LISTEN is a simple pub/sub mechanism that allows database clients to:
- **LISTEN** on named channels to receive notifications
- **NOTIFY** channels to send messages to all listeners
- Receive asynchronous notifications without polling

## Features

- ✅ Subscribe to multiple channels simultaneously
- ✅ Callback-based notification handling
- ✅ Automatic reconnection on connection loss
- ✅ Automatic resubscription after reconnection
- ✅ Thread-safe operations
- ✅ Panic recovery in notification handlers
- ✅ Dedicated connection for listening (doesn't interfere with queries)

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/bitechdev/ResolveSpec/pkg/dbmanager/providers"
)

func main() {
    // Create PostgreSQL provider
    cfg := &providers.Config{
        Name:     "primary",
        Type:     "postgres",
        Host:     "localhost",
        Port:     5432,
        User:     "postgres",
        Password: "password",
        Database: "myapp",
    }

    provider := providers.NewPostgresProvider()
    ctx := context.Background()

    if err := provider.Connect(ctx, cfg); err != nil {
        panic(err)
    }
    defer provider.Close()

    // Get listener
    listener, err := provider.GetListener(ctx)
    if err != nil {
        panic(err)
    }

    // Subscribe to a channel
    err = listener.Listen("events", func(channel, payload string) {
        fmt.Printf("Received on %s: %s\n", channel, payload)
    })
    if err != nil {
        panic(err)
    }

    // Send a notification
    err = listener.Notify(ctx, "events", "Hello, World!")
    if err != nil {
        panic(err)
    }

    // Keep the program running
    time.Sleep(1 * time.Second)
}
```

### Multiple Channels

```go
listener, _ := provider.GetListener(ctx)

// Listen to different channels with different handlers
listener.Listen("user_events", func(channel, payload string) {
    fmt.Printf("User event: %s\n", payload)
})

listener.Listen("order_events", func(channel, payload string) {
    fmt.Printf("Order event: %s\n", payload)
})

listener.Listen("payment_events", func(channel, payload string) {
    fmt.Printf("Payment event: %s\n", payload)
})
```

### Unsubscribing

```go
// Stop listening to a specific channel
err := listener.Unlisten("user_events")
if err != nil {
    fmt.Printf("Failed to unlisten: %v\n", err)
}
```

### Checking Active Channels

```go
// Get list of channels currently being listened to
channels := listener.Channels()
fmt.Printf("Listening to: %v\n", channels)
```

### Checking Connection Status

```go
if listener.IsConnected() {
    fmt.Println("Listener is connected")
} else {
    fmt.Println("Listener is disconnected")
}
```

## Integration with DBManager

When using the DBManager, you can access the listener through the PostgreSQL provider:

```go
// Initialize DBManager
mgr, err := dbmanager.NewManager(dbmanager.FromConfig(cfg.DBManager))
mgr.Connect(ctx)
defer mgr.Close()

// Get PostgreSQL connection
conn, err := mgr.Get("primary")

// Note: You'll need to cast to the underlying provider type
// This requires exposing the provider through the Connection interface
// or providing a helper method
```

## Use Cases

### Cache Invalidation

```go
listener.Listen("cache_invalidation", func(channel, payload string) {
    // Parse the payload to determine what to invalidate
    cache.Invalidate(payload)
})
```

### Real-time Updates

```go
listener.Listen("data_updates", func(channel, payload string) {
    // Broadcast update to WebSocket clients
    websocketBroadcast(payload)
})
```

### Configuration Reload

```go
listener.Listen("config_reload", func(channel, payload string) {
    // Reload application configuration
    config.Reload()
})
```

### Distributed Locking

```go
listener.Listen("lock_released", func(channel, payload string) {
    // Attempt to acquire the lock
    tryAcquireLock(payload)
})
```

## Automatic Reconnection

The listener automatically handles connection failures:

1. When a connection error is detected, the listener initiates reconnection
2. Once reconnected, it automatically resubscribes to all previous channels
3. Notification handlers remain active throughout the reconnection process

No manual intervention is required for reconnection.

## Error Handling

### Handler Panics

If a notification handler panics, the panic is recovered and logged. The listener continues to function normally:

```go
listener.Listen("events", func(channel, payload string) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Handler panic: %v", r)
        }
    }()

    // Your event processing logic
    processEvent(payload)
})
```

### Connection Errors

Connection errors trigger automatic reconnection. Check logs for reconnection events when `EnableLogging` is true.

## Thread Safety

All `PostgresListener` methods are thread-safe and can be called concurrently from multiple goroutines.

## Performance Considerations

1. **Dedicated Connection**: The listener uses a dedicated PostgreSQL connection separate from the query connection pool
2. **Asynchronous Handlers**: Notification handlers run in separate goroutines to avoid blocking
3. **Lightweight**: NOTIFY/LISTEN has minimal overhead compared to polling

## Comparison with Polling

| Feature | NOTIFY/LISTEN | Polling |
|---------|---------------|---------|
| Latency | Low (near real-time) | High (depends on poll interval) |
| Database Load | Minimal | High (constant queries) |
| Scalability | Excellent | Poor |
| Complexity | Simple | Moderate |

## Limitations

1. **PostgreSQL Only**: This feature is specific to PostgreSQL and not available for other databases
2. **No Message Persistence**: Notifications are not stored; if no listener is connected, the message is lost
3. **Payload Limit**: Notification payload is limited to 8000 bytes in PostgreSQL
4. **No Guaranteed Delivery**: If a listener disconnects, in-flight notifications may be lost

## Best Practices

1. **Keep Handlers Fast**: Notification handlers should be quick; for heavy processing, send work to a queue
2. **Use JSON Payloads**: Encode structured data as JSON for easy parsing
3. **Handle Errors Gracefully**: Always recover from panics in handlers
4. **Close Properly**: Always close the provider to ensure the listener is properly shut down
5. **Monitor Connection Status**: Use `IsConnected()` for health checks

## Example: Real-World Application

```go
// Subscribe to various application events
listener, _ := provider.GetListener(ctx)

// User registration events
listener.Listen("user_registered", func(channel, payload string) {
    var event UserRegisteredEvent
    json.Unmarshal([]byte(payload), &event)

    // Send welcome email
    sendWelcomeEmail(event.UserID)

    // Invalidate user count cache
    cache.Delete("user_count")
})

// Order placement events
listener.Listen("order_placed", func(channel, payload string) {
    var event OrderPlacedEvent
    json.Unmarshal([]byte(payload), &event)

    // Notify warehouse system
    warehouse.ProcessOrder(event.OrderID)

    // Update inventory cache
    cache.Invalidate("inventory:" + event.ProductID)
})

// Configuration changes
listener.Listen("config_updated", func(channel, payload string) {
    // Reload configuration from database
    appConfig.Reload()
})
```

## Triggering Notifications from SQL

You can trigger notifications directly from PostgreSQL triggers or functions:

```sql
-- Example trigger to notify on new user
CREATE OR REPLACE FUNCTION notify_user_registered()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('user_registered',
        json_build_object(
            'user_id', NEW.id,
            'email', NEW.email,
            'timestamp', NOW()
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER user_registered_trigger
AFTER INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION notify_user_registered();
```

## Additional Resources

- [PostgreSQL NOTIFY Documentation](https://www.postgresql.org/docs/current/sql-notify.html)
- [PostgreSQL LISTEN Documentation](https://www.postgresql.org/docs/current/sql-listen.html)
- [pgx Driver Documentation](https://github.com/jackc/pgx)
