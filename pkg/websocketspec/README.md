# WebSocketSpec - Real-Time WebSocket API Framework

WebSocketSpec provides a WebSocket-based API specification for real-time, bidirectional communication with full CRUD operations, subscriptions, and lifecycle hooks.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Message Protocol](#message-protocol)
- [CRUD Operations](#crud-operations)
- [Subscriptions](#subscriptions)
- [Lifecycle Hooks](#lifecycle-hooks)
- [Client Examples](#client-examples)
- [Authentication](#authentication)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)

## Features

- **Real-Time Bidirectional Communication**: WebSocket-based persistent connections
- **Full CRUD Operations**: Create, Read, Update, Delete with rich query options
- **Real-Time Subscriptions**: Subscribe to entity changes with filter support
- **Automatic Notifications**: Server pushes updates to subscribed clients
- **Lifecycle Hooks**: Before/after hooks for all operations
- **Database Agnostic**: Works with GORM and Bun ORM through adapters
- **Connection Management**: Automatic connection tracking and cleanup
- **Request/Response Correlation**: Message IDs for tracking requests
- **Filter & Sort**: Advanced filtering, sorting, pagination, and preloading

## Installation

```bash
go get github.com/bitechdev/ResolveSpec
```

## Quick Start

### Server Setup

```go
package main

import (
    "net/http"
    "github.com/bitechdev/ResolveSpec/pkg/websocketspec"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

func main() {
    // Connect to database
    db, _ := gorm.Open(postgres.Open("your-connection-string"), &gorm.Config{})

    // Create WebSocket handler
    handler := websocketspec.NewHandlerWithGORM(db)

    // Register models
    handler.Registry.RegisterModel("public.users", &User{})
    handler.Registry.RegisterModel("public.posts", &Post{})

    // Setup WebSocket endpoint
    http.HandleFunc("/ws", handler.HandleWebSocket)

    // Start server
    http.ListenAndServe(":8080", nil)
}

type User struct {
    ID     uint   `json:"id" gorm:"primaryKey"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Status string `json:"status"`
}

type Post struct {
    ID      uint   `json:"id" gorm:"primaryKey"`
    Title   string `json:"title"`
    Content string `json:"content"`
    UserID  uint   `json:"user_id"`
}
```

### Client Setup (JavaScript)

```javascript
const ws = new WebSocket("ws://localhost:8080/ws");

ws.onopen = () => {
    console.log("Connected to WebSocket");
};

ws.onmessage = (event) => {
    const message = JSON.parse(event.data);
    console.log("Received:", message);
};

ws.onerror = (error) => {
    console.error("WebSocket error:", error);
};
```

## Message Protocol

All messages are JSON-encoded with the following structure:

```typescript
interface Message {
    id: string;                  // Unique message ID for correlation
    type: "request" | "response" | "notification" | "subscription";
    operation?: "read" | "create" | "update" | "delete" | "subscribe" | "unsubscribe" | "meta";
    schema?: string;             // Database schema
    entity: string;              // Table/model name
    record_id?: string;          // For single-record operations
    data?: any;                  // Request/response payload
    options?: QueryOptions;      // Filters, sorting, pagination
    subscription_id?: string;    // For subscription messages
    success?: boolean;           // Response success indicator
    error?: ErrorInfo;           // Error details
    metadata?: Record<string, any>; // Additional metadata
    timestamp?: string;          // Message timestamp
}

interface QueryOptions {
    filters?: FilterOption[];
    columns?: string[];
    preload?: PreloadOption[];
    sort?: SortOption[];
    limit?: number;
    offset?: number;
}
```

## CRUD Operations

### CREATE - Create New Records

**Request:**
```json
{
    "id": "msg-1",
    "type": "request",
    "operation": "create",
    "schema": "public",
    "entity": "users",
    "data": {
        "name": "John Doe",
        "email": "john@example.com",
        "status": "active"
    }
}
```

**Response:**
```json
{
    "id": "msg-1",
    "type": "response",
    "success": true,
    "data": {
        "id": 123,
        "name": "John Doe",
        "email": "john@example.com",
        "status": "active"
    },
    "timestamp": "2025-12-12T10:30:00Z"
}
```

### READ - Query Records

**Read Multiple Records:**
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
        "columns": ["id", "name", "email"],
        "sort": [
            {"column": "name", "direction": "asc"}
        ],
        "limit": 10,
        "offset": 0
    }
}
```

**Read Single Record:**
```json
{
    "id": "msg-3",
    "type": "request",
    "operation": "read",
    "schema": "public",
    "entity": "users",
    "record_id": "123"
}
```

**Response:**
```json
{
    "id": "msg-2",
    "type": "response",
    "success": true,
    "data": [
        {"id": 1, "name": "Alice", "email": "alice@example.com"},
        {"id": 2, "name": "Bob", "email": "bob@example.com"}
    ],
    "metadata": {
        "total": 50,
        "count": 2
    },
    "timestamp": "2025-12-12T10:30:00Z"
}
```

### UPDATE - Update Records

```json
{
    "id": "msg-4",
    "type": "request",
    "operation": "update",
    "schema": "public",
    "entity": "users",
    "record_id": "123",
    "data": {
        "name": "John Updated",
        "email": "john.updated@example.com"
    }
}
```

### DELETE - Delete Records

```json
{
    "id": "msg-5",
    "type": "request",
    "operation": "delete",
    "schema": "public",
    "entity": "users",
    "record_id": "123"
}
```

## Subscriptions

Subscriptions allow clients to receive real-time notifications when entities change.

### Subscribe to Changes

```json
{
    "id": "sub-1",
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

**Response:**
```json
{
    "id": "sub-1",
    "type": "response",
    "success": true,
    "data": {
        "subscription_id": "sub-abc123",
        "schema": "public",
        "entity": "users"
    },
    "timestamp": "2025-12-12T10:30:00Z"
}
```

### Receive Notifications

When a subscribed entity changes, clients automatically receive notifications:

```json
{
    "type": "notification",
    "operation": "create",
    "subscription_id": "sub-abc123",
    "schema": "public",
    "entity": "users",
    "data": {
        "id": 124,
        "name": "Jane Smith",
        "email": "jane@example.com",
        "status": "active"
    },
    "timestamp": "2025-12-12T10:35:00Z"
}
```

**Notification Operations:**
- `create` - New record created
- `update` - Record updated
- `delete` - Record deleted

### Unsubscribe

```json
{
    "id": "unsub-1",
    "type": "subscription",
    "operation": "unsubscribe",
    "subscription_id": "sub-abc123"
}
```

## Lifecycle Hooks

Hooks allow you to intercept and modify operations at various points in the lifecycle.

### Available Hook Types

- **BeforeRead** / **AfterRead**
- **BeforeCreate** / **AfterCreate**
- **BeforeUpdate** / **AfterUpdate**
- **BeforeDelete** / **AfterDelete**
- **BeforeSubscribe** / **AfterSubscribe**
- **BeforeConnect** / **AfterConnect**

### Hook Example

```go
handler := websocketspec.NewHandlerWithGORM(db)

// Authorization hook
handler.Hooks().RegisterBefore(websocketspec.OperationRead, func(ctx *websocketspec.HookContext) error {
    // Check permissions
    userID, _ := ctx.Connection.GetMetadata("user_id")
    if userID == nil {
        return fmt.Errorf("unauthorized: user not authenticated")
    }

    // Add filter to only show user's own records
    if ctx.Entity == "posts" {
        ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
            Column:   "user_id",
            Operator: "eq",
            Value:    userID,
        })
    }

    return nil
})

// Logging hook
handler.Hooks().RegisterAfter(websocketspec.OperationCreate, func(ctx *websocketspec.HookContext) error {
    log.Printf("Created %s in %s.%s", ctx.Result, ctx.Schema, ctx.Entity)
    return nil
})

// Validation hook
handler.Hooks().RegisterBefore(websocketspec.OperationCreate, func(ctx *websocketspec.HookContext) error {
    // Validate data before creation
    if data, ok := ctx.Data.(map[string]interface{}); ok {
        if email, exists := data["email"]; !exists || email == "" {
            return fmt.Errorf("email is required")
        }
    }
    return nil
})
```

## Client Examples

### JavaScript/TypeScript Client

```typescript
class WebSocketClient {
    private ws: WebSocket;
    private messageHandlers: Map<string, (data: any) => void> = new Map();
    private subscriptions: Map<string, (data: any) => void> = new Map();

    constructor(url: string) {
        this.ws = new WebSocket(url);
        this.ws.onmessage = (event) => this.handleMessage(event);
    }

    // Send request and wait for response
    async request(operation: string, entity: string, options?: any): Promise<any> {
        const id = this.generateId();

        return new Promise((resolve, reject) => {
            this.messageHandlers.set(id, (data) => {
                if (data.success) {
                    resolve(data.data);
                } else {
                    reject(data.error);
                }
            });

            this.ws.send(JSON.stringify({
                id,
                type: "request",
                operation,
                entity,
                ...options
            }));
        });
    }

    // Subscribe to entity changes
    async subscribe(entity: string, filters?: any[], callback?: (data: any) => void): Promise<string> {
        const id = this.generateId();

        return new Promise((resolve, reject) => {
            this.messageHandlers.set(id, (data) => {
                if (data.success) {
                    const subId = data.data.subscription_id;
                    if (callback) {
                        this.subscriptions.set(subId, callback);
                    }
                    resolve(subId);
                } else {
                    reject(data.error);
                }
            });

            this.ws.send(JSON.stringify({
                id,
                type: "subscription",
                operation: "subscribe",
                entity,
                options: { filters }
            }));
        });
    }

    private handleMessage(event: MessageEvent) {
        const message = JSON.parse(event.data);

        if (message.type === "response") {
            const handler = this.messageHandlers.get(message.id);
            if (handler) {
                handler(message);
                this.messageHandlers.delete(message.id);
            }
        } else if (message.type === "notification") {
            const callback = this.subscriptions.get(message.subscription_id);
            if (callback) {
                callback(message);
            }
        }
    }

    private generateId(): string {
        return `msg-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`;
    }
}

// Usage
const client = new WebSocketClient("ws://localhost:8080/ws");

// Read users
const users = await client.request("read", "users", {
    options: {
        filters: [{ column: "status", operator: "eq", value: "active" }],
        limit: 10
    }
});

// Subscribe to user changes
await client.subscribe("users",
    [{ column: "status", operator: "eq", value: "active" }],
    (notification) => {
        console.log("User changed:", notification.operation, notification.data);
    }
);

// Create user
const newUser = await client.request("create", "users", {
    data: {
        name: "Alice",
        email: "alice@example.com",
        status: "active"
    }
});
```

### Python Client Example

```python
import asyncio
import websockets
import json
import uuid

class WebSocketClient:
    def __init__(self, url):
        self.url = url
        self.ws = None
        self.handlers = {}
        self.subscriptions = {}

    async def connect(self):
        self.ws = await websockets.connect(self.url)
        asyncio.create_task(self.listen())

    async def listen(self):
        async for message in self.ws:
            data = json.loads(message)

            if data["type"] == "response":
                handler = self.handlers.get(data["id"])
                if handler:
                    handler(data)
                    del self.handlers[data["id"]]

            elif data["type"] == "notification":
                callback = self.subscriptions.get(data["subscription_id"])
                if callback:
                    callback(data)

    async def request(self, operation, entity, **kwargs):
        msg_id = str(uuid.uuid4())
        future = asyncio.Future()

        self.handlers[msg_id] = lambda data: future.set_result(data)

        await self.ws.send(json.dumps({
            "id": msg_id,
            "type": "request",
            "operation": operation,
            "entity": entity,
            **kwargs
        }))

        result = await future
        if result["success"]:
            return result["data"]
        else:
            raise Exception(result["error"]["message"])

    async def subscribe(self, entity, callback, filters=None):
        msg_id = str(uuid.uuid4())
        future = asyncio.Future()

        self.handlers[msg_id] = lambda data: future.set_result(data)

        await self.ws.send(json.dumps({
            "id": msg_id,
            "type": "subscription",
            "operation": "subscribe",
            "entity": entity,
            "options": {"filters": filters} if filters else {}
        }))

        result = await future
        if result["success"]:
            sub_id = result["data"]["subscription_id"]
            self.subscriptions[sub_id] = callback
            return sub_id
        else:
            raise Exception(result["error"]["message"])

# Usage
async def main():
    client = WebSocketClient("ws://localhost:8080/ws")
    await client.connect()

    # Read users
    users = await client.request("read", "users",
        options={
            "filters": [{"column": "status", "operator": "eq", "value": "active"}],
            "limit": 10
        }
    )
    print("Users:", users)

    # Subscribe to changes
    def on_user_change(notification):
        print(f"User {notification['operation']}: {notification['data']}")

    await client.subscribe("users", on_user_change,
        filters=[{"column": "status", "operator": "eq", "value": "active"}]
    )

asyncio.run(main())
```

## Authentication

Implement authentication using hooks:

```go
handler := websocketspec.NewHandlerWithGORM(db)

// Authentication on connection
handler.Hooks().Register(websocketspec.BeforeConnect, func(ctx *websocketspec.HookContext) error {
    // Extract token from query params or headers
    r := ctx.Connection.ws.UnderlyingConn().RemoteAddr()

    // Validate token (implement your auth logic)
    token := extractToken(r)
    user, err := validateToken(token)
    if err != nil {
        return fmt.Errorf("authentication failed: %w", err)
    }

    // Store user info in connection metadata
    ctx.Connection.SetMetadata("user", user)
    ctx.Connection.SetMetadata("user_id", user.ID)

    return nil
})

// Check permissions for each operation
handler.Hooks().RegisterBefore(websocketspec.OperationRead, func(ctx *websocketspec.HookContext) error {
    userID, ok := ctx.Connection.GetMetadata("user_id")
    if !ok {
        return fmt.Errorf("unauthorized")
    }

    // Add user-specific filters
    if ctx.Entity == "orders" {
        ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
            Column:   "user_id",
            Operator: "eq",
            Value:    userID,
        })
    }

    return nil
})
```

## Error Handling

Errors are returned in a consistent format:

```json
{
    "id": "msg-1",
    "type": "response",
    "success": false,
    "error": {
        "code": "validation_error",
        "message": "Email is required",
        "details": {
            "field": "email"
        }
    },
    "timestamp": "2025-12-12T10:30:00Z"
}
```

**Common Error Codes:**
- `invalid_message` - Message format is invalid
- `model_not_found` - Entity not registered
- `invalid_model` - Model validation failed
- `read_error` - Read operation failed
- `create_error` - Create operation failed
- `update_error` - Update operation failed
- `delete_error` - Delete operation failed
- `hook_error` - Hook execution failed
- `unauthorized` - Authentication/authorization failed

## Best Practices

1. **Always Use Message IDs**: Correlate requests with responses using unique IDs
2. **Handle Reconnections**: Implement automatic reconnection logic on the client
3. **Validate Data**: Use before-hooks to validate data before operations
4. **Limit Subscriptions**: Implement limits on subscriptions per connection
5. **Use Filters**: Apply filters to subscriptions to reduce unnecessary notifications
6. **Implement Authentication**: Always validate users before processing operations
7. **Handle Errors Gracefully**: Display user-friendly error messages
8. **Clean Up**: Unsubscribe when components unmount or disconnect
9. **Rate Limiting**: Implement rate limiting to prevent abuse
10. **Monitor Connections**: Track active connections and subscriptions

## Filter Operators

Supported filter operators:

- `eq` - Equal (=)
- `neq` - Not Equal (!=)
- `gt` - Greater Than (>)
- `gte` - Greater Than or Equal (>=)
- `lt` - Less Than (<)
- `lte` - Less Than or Equal (<=)
- `like` - LIKE (case-sensitive)
- `ilike` - ILIKE (case-insensitive)
- `in` - IN (array of values)

## Performance Considerations

- **Connection Pooling**: WebSocket connections are reused, reducing overhead
- **Subscription Filtering**: Only matching updates are sent to clients
- **Efficient Queries**: Uses database adapters for optimized queries
- **Message Batching**: Multiple messages can be sent in one write
- **Keepalive**: Automatic ping/pong for connection health

## Comparison with Other Specs

| Feature | WebSocketSpec | RestHeadSpec | ResolveSpec |
|---------|--------------|--------------|-------------|
| Protocol | WebSocket | HTTP/REST | HTTP/REST |
| Real-time | ✅ Yes | ❌ No | ❌ No |
| Subscriptions | ✅ Yes | ❌ No | ❌ No |
| Bidirectional | ✅ Yes | ❌ No | ❌ No |
| Query Options | In Message | In Headers | In Body |
| Overhead | Low | Medium | Medium |
| Use Case | Real-time apps | Traditional APIs | Body-based APIs |

## License

MIT License - See LICENSE file for details
