# MQTTSpec - MQTT-based Database Query Framework

MQTTSpec is an MQTT-based database query framework that enables real-time database operations and subscriptions via MQTT protocol. It mirrors the functionality of WebSocketSpec but uses MQTT as the transport layer, making it ideal for IoT applications, mobile apps with unreliable networks, and distributed systems requiring QoS guarantees.

## Features

- **Dual Broker Support**: Embedded broker (Mochi MQTT) or external broker connection (Paho MQTT)
- **QoS 1 (At-least-once delivery)**: Reliable message delivery for all operations
- **Full CRUD Operations**: Create, Read, Update, Delete with hooks
- **Real-time Subscriptions**: Subscribe to entity changes with filtering
- **Database Agnostic**: GORM and Bun ORM support
- **Lifecycle Hooks**: 13 hooks for authentication, authorization, validation, and auditing
- **Multi-tenancy Support**: Built-in tenant isolation via hooks
- **Thread-safe**: Proper concurrency handling throughout

## Installation

```bash
go get github.com/bitechdev/ResolveSpec/pkg/mqttspec
```

## Quick Start

### Embedded Broker (Default)

```go
package main

import (
    "github.com/bitechdev/ResolveSpec/pkg/mqttspec"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

type User struct {
    ID     uint   `json:"id" gorm:"primaryKey"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Status string `json:"status"`
}

func main() {
    // Connect to database
    db, _ := gorm.Open(postgres.Open("postgres://..."), &gorm.Config{})
    db.AutoMigrate(&User{})

    // Create MQTT handler with embedded broker
    handler, err := mqttspec.NewHandlerWithGORM(db)
    if err != nil {
        panic(err)
    }

    // Register models
    handler.Registry().RegisterModel("public.users", &User{})

    // Start handler (starts embedded broker on localhost:1883)
    if err := handler.Start(); err != nil {
        panic(err)
    }

    // Handler is now listening for MQTT messages
    select {} // Keep running
}
```

### External Broker

```go
handler, err := mqttspec.NewHandlerWithGORM(db,
    mqttspec.WithExternalBroker(mqttspec.ExternalBrokerConfig{
        BrokerURL:      "tcp://mqtt.example.com:1883",
        ClientID:       "mqttspec-server",
        Username:       "admin",
        Password:       "secret",
        ConnectTimeout: 10 * time.Second,
    }),
)
```

### Custom Port (Embedded Broker)

```go
handler, err := mqttspec.NewHandlerWithGORM(db,
    mqttspec.WithEmbeddedBroker(mqttspec.BrokerConfig{
        Host: "0.0.0.0",
        Port: 1884,
    }),
)
```

## Topic Structure

MQTTSpec uses a client-based topic hierarchy:

```
spec/{client_id}/request         # Client publishes requests
spec/{client_id}/response        # Server publishes responses
spec/{client_id}/notify/{sub_id} # Server publishes notifications
```

### Wildcard Subscriptions

- **Server**: `spec/+/request` (receives all client requests)
- **Client**: `spec/{client_id}/response` + `spec/{client_id}/notify/+`

## Message Protocol

MQTTSpec uses the same JSON message structure as WebSocketSpec and ResolveSpec for consistency.

### Request Message

```json
{
  "id": "msg-123",
  "type": "request",
  "operation": "read",
  "schema": "public",
  "entity": "users",
  "options": {
    "filters": [
      {"column": "status", "operator": "eq", "value": "active"}
    ],
    "sort": [{"column": "created_at", "direction": "desc"}],
    "limit": 10
  }
}
```

### Response Message

```json
{
  "id": "msg-123",
  "type": "response",
  "success": true,
  "data": [
    {"id": 1, "name": "John Doe", "email": "john@example.com", "status": "active"},
    {"id": 2, "name": "Jane Smith", "email": "jane@example.com", "status": "active"}
  ],
  "metadata": {
    "total": 50,
    "count": 2
  }
}
```

### Notification Message

```json
{
  "type": "notification",
  "operation": "create",
  "subscription_id": "sub-xyz",
  "schema": "public",
  "entity": "users",
  "data": {
    "id": 3,
    "name": "New User",
    "email": "new@example.com",
    "status": "active"
  }
}
```

## CRUD Operations

### Read (Single Record)

**MQTT Client Publishes to**: `spec/{client_id}/request`

```json
{
  "id": "msg-1",
  "type": "request",
  "operation": "read",
  "schema": "public",
  "entity": "users",
  "data": {"id": 1}
}
```

**Server Publishes Response to**: `spec/{client_id}/response`

```json
{
  "id": "msg-1",
  "success": true,
  "data": {"id": 1, "name": "John Doe", "email": "john@example.com"}
}
```

### Read (Multiple Records with Filtering)

```json
{
  "id": "msg-2",
  "type": "request",
  "operation": "read",
  "schema": "public",
  "entity": "users",
  "options": {
    "filters": [
      {"column": "status", "operator": "eq", "value": "active"}
    ],
    "sort": [{"column": "name", "direction": "asc"}],
    "limit": 20,
    "offset": 0
  }
}
```

### Create

```json
{
  "id": "msg-3",
  "type": "request",
  "operation": "create",
  "schema": "public",
  "entity": "users",
  "data": {
    "name": "Alice Brown",
    "email": "alice@example.com",
    "status": "active"
  }
}
```

### Update

```json
{
  "id": "msg-4",
  "type": "request",
  "operation": "update",
  "schema": "public",
  "entity": "users",
  "data": {
    "id": 1,
    "status": "inactive"
  }
}
```

### Delete

```json
{
  "id": "msg-5",
  "type": "request",
  "operation": "delete",
  "schema": "public",
  "entity": "users",
  "data": {"id": 1}
}
```

## Real-time Subscriptions

### Subscribe to Entity Changes

**Client Publishes to**: `spec/{client_id}/request`

```json
{
  "id": "msg-6",
  "type": "subscription",
  "operation": "subscribe",
  "schema": "public",
  "entity": "users",
  "options": {
    "filters": [
      {"column": "status", "operator": "eq", "value": "active"}
    ]
  }
}
```

**Server Response** (published to `spec/{client_id}/response`):

```json
{
  "id": "msg-6",
  "success": true,
  "data": {
    "subscription_id": "sub-abc123",
    "notify_topic": "spec/{client_id}/notify/sub-abc123"
  }
}
```

**Client Then Subscribes** to MQTT topic: `spec/{client_id}/notify/sub-abc123`

### Receiving Notifications

When any client creates/updates/deletes a user matching the subscription filters, the subscriber receives:

```json
{
  "type": "notification",
  "operation": "create",
  "subscription_id": "sub-abc123",
  "schema": "public",
  "entity": "users",
  "data": {
    "id": 10,
    "name": "New User",
    "email": "newuser@example.com",
    "status": "active"
  }
}
```

### Unsubscribe

```json
{
  "id": "msg-7",
  "type": "subscription",
  "operation": "unsubscribe",
  "data": {
    "subscription_id": "sub-abc123"
  }
}
```

## Lifecycle Hooks

MQTTSpec provides 13 lifecycle hooks for implementing cross-cutting concerns:

### Hook Types

- `BeforeHandle` — fires after model resolution, before operation dispatch (auth checks)
- `BeforeConnect` / `AfterConnect` - Connection lifecycle
- `BeforeDisconnect` / `AfterDisconnect` - Disconnection lifecycle
- `BeforeRead` / `AfterRead` - Read operations
- `BeforeCreate` / `AfterCreate` - Create operations
- `BeforeUpdate` / `AfterUpdate` - Update operations
- `BeforeDelete` / `AfterDelete` - Delete operations
- `BeforeSubscribe` / `AfterSubscribe` - Subscription creation
- `BeforeUnsubscribe` / `AfterUnsubscribe` - Subscription removal

### Security Hooks (Recommended)

Use `RegisterSecurityHooks` for integrated auth with model-rule support:

```go
import "github.com/bitechdev/ResolveSpec/pkg/security"

provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
securityList := security.NewSecurityList(provider)
mqttspec.RegisterSecurityHooks(handler, securityList)
// Registers BeforeHandle (model auth), BeforeRead (load rules),
// AfterRead (column security + audit), BeforeUpdate, BeforeDelete
```

### Authentication Example (JWT)

```go
handler.Hooks().Register(mqttspec.BeforeConnect, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)

    // MQTT username contains JWT token
    token := client.Username
    claims, err := jwt.Validate(token)
    if err != nil {
        return fmt.Errorf("invalid token: %w", err)
    }

    // Store user info in client metadata for later use
    client.SetMetadata("user_id", claims.UserID)
    client.SetMetadata("tenant_id", claims.TenantID)
    client.SetMetadata("roles", claims.Roles)

    logger.Info("Client authenticated: user_id=%d, tenant=%s", claims.UserID, claims.TenantID)
    return nil
})
```

### Multi-tenancy Example

```go
// Auto-inject tenant filter for all read operations
handler.Hooks().Register(mqttspec.BeforeRead, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)
    tenantID, _ := client.GetMetadata("tenant_id")

    // Add tenant filter to ensure users only see their own data
    ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
        Column:   "tenant_id",
        Operator: "eq",
        Value:    tenantID,
    })

    return nil
})

// Auto-set tenant_id for all create operations
handler.Hooks().Register(mqttspec.BeforeCreate, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)
    tenantID, _ := client.GetMetadata("tenant_id")

    // Inject tenant_id into new records
    if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
        dataMap["tenant_id"] = tenantID
    }

    return nil
})
```

### Role-based Access Control (RBAC)

```go
handler.Hooks().Register(mqttspec.BeforeDelete, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)
    roles, _ := client.GetMetadata("roles")

    roleList := roles.([]string)
    hasAdminRole := false
    for _, role := range roleList {
        if role == "admin" {
            hasAdminRole = true
            break
        }
    }

    if !hasAdminRole {
        return fmt.Errorf("permission denied: delete requires admin role")
    }

    return nil
})
```

### Audit Logging Example

```go
handler.Hooks().Register(mqttspec.AfterCreate, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)
    userID, _ := client.GetMetadata("user_id")

    logger.Info("Audit: user %d created %s.%s record: %+v",
        userID, ctx.Schema, ctx.Entity, ctx.Result)

    // Could also write to audit log table
    return nil
})
```

## Client Examples

### JavaScript (MQTT.js)

```javascript
const mqtt = require('mqtt');

// Connect to MQTT broker
const client = mqtt.connect('mqtt://localhost:1883', {
  clientId: 'client-abc123',
  username: 'your-jwt-token',
  password: '',  // JWT in username, password can be empty
});

client.on('connect', () => {
  console.log('Connected to MQTT broker');

  // Subscribe to responses
  client.subscribe('spec/client-abc123/response');

  // Read users
  const readMsg = {
    id: 'msg-1',
    type: 'request',
    operation: 'read',
    schema: 'public',
    entity: 'users',
    options: {
      filters: [
        { column: 'status', operator: 'eq', value: 'active' }
      ]
    }
  };

  client.publish('spec/client-abc123/request', JSON.stringify(readMsg));
});

client.on('message', (topic, payload) => {
  const message = JSON.parse(payload.toString());
  console.log('Received:', message);

  if (message.type === 'response') {
    console.log('Response data:', message.data);
  } else if (message.type === 'notification') {
    console.log('Notification:', message.operation, message.data);
  }
});
```

### Python (paho-mqtt)

```python
import paho.mqtt.client as mqtt
import json

client_id = 'client-python-123'

def on_connect(client, userdata, flags, rc):
    print(f"Connected with result code {rc}")

    # Subscribe to responses
    client.subscribe(f"spec/{client_id}/response")

    # Create a user
    create_msg = {
        'id': 'msg-create-1',
        'type': 'request',
        'operation': 'create',
        'schema': 'public',
        'entity': 'users',
        'data': {
            'name': 'Python User',
            'email': 'python@example.com',
            'status': 'active'
        }
    }

    client.publish(f"spec/{client_id}/request", json.dumps(create_msg))

def on_message(client, userdata, msg):
    message = json.loads(msg.payload.decode())
    print(f"Received on {msg.topic}: {message}")

client = mqtt.Client(client_id=client_id)
client.username_pw_set('your-jwt-token', '')
client.on_connect = on_connect
client.on_message = on_message

client.connect('localhost', 1883, 60)
client.loop_forever()
```

### Go (paho.mqtt.golang)

```go
package main

import (
    "encoding/json"
    "fmt"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
    clientID := "client-go-123"

    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883")
    opts.SetClientID(clientID)
    opts.SetUsername("your-jwt-token")
    opts.SetPassword("")

    opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
        var message map[string]interface{}
        json.Unmarshal(msg.Payload(), &message)
        fmt.Printf("Received on %s: %+v\n", msg.Topic(), message)
    })

    opts.OnConnect = func(client mqtt.Client) {
        fmt.Println("Connected to MQTT broker")

        // Subscribe to responses
        client.Subscribe(fmt.Sprintf("spec/%s/response", clientID), 1, nil)

        // Read users
        readMsg := map[string]interface{}{
            "id":        "msg-1",
            "type":      "request",
            "operation": "read",
            "schema":    "public",
            "entity":    "users",
            "options": map[string]interface{}{
                "filters": []map[string]interface{}{
                    {"column": "status", "operator": "eq", "value": "active"},
                },
            },
        }

        payload, _ := json.Marshal(readMsg)
        client.Publish(fmt.Sprintf("spec/%s/request", clientID), 1, false, payload)
    }

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    // Keep running
    select {}
}
```

## Configuration Options

### BrokerConfig (Embedded Broker)

```go
type BrokerConfig struct {
    Host            string        // Default: "localhost"
    Port            int           // Default: 1883
    EnableWebSocket bool          // Enable WebSocket listener
    WSPort          int           // WebSocket port (default: 1884)
    MaxConnections  int           // Max concurrent connections
    KeepAlive       time.Duration // MQTT keep-alive interval
    EnableAuth      bool          // Enable authentication
}
```

### ExternalBrokerConfig

```go
type ExternalBrokerConfig struct {
    BrokerURL      string         // MQTT broker URL (tcp://host:port)
    ClientID       string         // MQTT client ID
    Username       string         // MQTT username
    Password       string         // MQTT password
    CleanSession   bool           // Clean session flag
    KeepAlive      time.Duration  // Keep-alive interval
    ConnectTimeout time.Duration  // Connection timeout
    ReconnectDelay time.Duration  // Auto-reconnect delay
    MaxReconnect   int            // Max reconnect attempts
    TLSConfig      *tls.Config    // TLS configuration
}
```

### QoS Configuration

```go
handler, err := mqttspec.NewHandlerWithGORM(db,
    mqttspec.WithQoS(1, 1, 1), // Request, Response, Notification
)
```

### Topic Prefix

```go
handler, err := mqttspec.NewHandlerWithGORM(db,
    mqttspec.WithTopicPrefix("myapp"), // Changes topics to myapp/{client_id}/...
)
```

## Documentation References

- **ResolveSpec JSON Protocol**: See `/pkg/resolvespec/README.md` for the full message protocol specification
- **WebSocketSpec Documentation**: See `/pkg/websocketspec/README.md` for similar WebSocket-based implementation
- **Common Interfaces**: See `/pkg/common/types.go` for database adapter interfaces and query options
- **Model Registry**: See `/pkg/modelregistry/README.md` for model registration and reflection
- **Hooks Reference**: See `/pkg/websocketspec/hooks.go` for hook types (same as MQTTSpec)
- **Subscription Management**: See `/pkg/websocketspec/subscription.go` for subscription filtering

## Comparison: MQTTSpec vs WebSocketSpec

| Feature | MQTTSpec | WebSocketSpec |
|---------|----------|---------------|
| **Transport** | MQTT (pub/sub broker) | WebSocket (direct connection) |
| **Connection Model** | Broker-mediated | Direct client-server |
| **QoS Levels** | QoS 0, 1, 2 support | No built-in QoS |
| **Offline Messages** | Yes (with QoS 1+) | No |
| **Auto-reconnect** | Yes (built into MQTT) | Manual implementation needed |
| **Network Efficiency** | Better for unreliable networks | Better for low-latency |
| **Best For** | IoT, mobile apps, distributed systems | Web applications, real-time dashboards |
| **Message Protocol** | Same JSON structure | Same JSON structure |
| **Hooks** | Same 13 hooks | Same 13 hooks |
| **CRUD Operations** | Identical | Identical |
| **Subscriptions** | Identical (via MQTT topics) | Identical (via app-level) |

## Use Cases

### IoT Sensor Data

```go
// Sensors publish data, backend stores and notifies subscribers
handler.Registry().RegisterModel("public.sensor_readings", &SensorReading{})

// Auto-set device_id from client metadata
handler.Hooks().Register(mqttspec.BeforeCreate, func(ctx *mqttspec.HookContext) error {
    client := ctx.Metadata["mqtt_client"].(*mqttspec.Client)
    deviceID, _ := client.GetMetadata("device_id")

    if ctx.Entity == "sensor_readings" {
        if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
            dataMap["device_id"] = deviceID
            dataMap["timestamp"] = time.Now()
        }
    }
    return nil
})
```

### Mobile App with Offline Support

MQTTSpec's QoS 1 ensures messages are delivered even if the client temporarily disconnects.

### Distributed Microservices

Multiple services can subscribe to entity changes and react accordingly.

## Testing

Run unit tests:

```bash
go test -v ./pkg/mqttspec
```

Run with race detection:

```bash
go test -race -v ./pkg/mqttspec
```

## License

This package is part of the ResolveSpec project.

## Contributing

Contributions are welcome! Please ensure:

- All tests pass (`go test ./pkg/mqttspec`)
- No race conditions (`go test -race ./pkg/mqttspec`)
- Documentation is updated
- Examples are provided for new features

## Support

For issues, questions, or feature requests, please open an issue in the ResolveSpec repository.
