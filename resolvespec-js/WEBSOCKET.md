# WebSocketSpec JavaScript Client

A TypeScript/JavaScript client for connecting to WebSocketSpec servers with full support for real-time subscriptions, CRUD operations, and automatic reconnection.

## Installation

```bash
npm install @warkypublic/resolvespec-js
# or
yarn add @warkypublic/resolvespec-js
# or
pnpm add @warkypublic/resolvespec-js
```

## Quick Start

```typescript
import { WebSocketClient } from '@warkypublic/resolvespec-js';

// Create client
const client = new WebSocketClient({
    url: 'ws://localhost:8080/ws',
    reconnect: true,
    debug: true
});

// Connect
await client.connect();

// Read records
const users = await client.read('users', {
    schema: 'public',
    filters: [
        { column: 'status', operator: 'eq', value: 'active' }
    ],
    limit: 10
});

// Subscribe to changes
const subscriptionId = await client.subscribe('users', (notification) => {
    console.log('User changed:', notification.operation, notification.data);
}, { schema: 'public' });

// Clean up
await client.unsubscribe(subscriptionId);
client.disconnect();
```

## Features

- **Real-Time Updates**: Subscribe to entity changes and receive instant notifications
- **Full CRUD Support**: Create, read, update, and delete operations
- **TypeScript Support**: Full type definitions included
- **Auto Reconnection**: Automatic reconnection with configurable retry logic
- **Heartbeat**: Built-in keepalive mechanism
- **Event System**: Listen to connection, error, and message events
- **Promise-based API**: All async operations return promises
- **Filter & Sort**: Advanced querying with filters, sorting, and pagination
- **Preloading**: Load related entities in a single query

## Configuration

```typescript
const client = new WebSocketClient({
    url: 'ws://localhost:8080/ws',           // WebSocket server URL
    reconnect: true,                          // Enable auto-reconnection
    reconnectInterval: 3000,                  // Reconnection delay (ms)
    maxReconnectAttempts: 10,                 // Max reconnection attempts
    heartbeatInterval: 30000,                 // Heartbeat interval (ms)
    debug: false                              // Enable debug logging
});
```

## API Reference

### Connection Management

#### `connect(): Promise<void>`
Connect to the WebSocket server.

```typescript
await client.connect();
```

#### `disconnect(): void`
Disconnect from the server.

```typescript
client.disconnect();
```

#### `isConnected(): boolean`
Check if currently connected.

```typescript
if (client.isConnected()) {
    console.log('Connected!');
}
```

#### `getState(): ConnectionState`
Get current connection state: `'connecting'`, `'connected'`, `'disconnecting'`, `'disconnected'`, or `'reconnecting'`.

```typescript
const state = client.getState();
console.log('State:', state);
```

### CRUD Operations

#### `read<T>(entity: string, options?): Promise<T>`
Read records from an entity.

```typescript
// Read all active users
const users = await client.read('users', {
    schema: 'public',
    filters: [
        { column: 'status', operator: 'eq', value: 'active' }
    ],
    columns: ['id', 'name', 'email'],
    sort: [
        { column: 'name', direction: 'asc' }
    ],
    limit: 10,
    offset: 0
});

// Read single record by ID
const user = await client.read('users', {
    schema: 'public',
    record_id: '123'
});

// Read with preloading
const posts = await client.read('posts', {
    schema: 'public',
    preload: [
        {
            relation: 'user',
            columns: ['id', 'name', 'email']
        },
        {
            relation: 'comments',
            filters: [
                { column: 'status', operator: 'eq', value: 'approved' }
            ]
        }
    ]
});
```

#### `create<T>(entity: string, data: any, options?): Promise<T>`
Create a new record.

```typescript
const newUser = await client.create('users', {
    name: 'John Doe',
    email: 'john@example.com',
    status: 'active'
}, {
    schema: 'public'
});
```

#### `update<T>(entity: string, id: string, data: any, options?): Promise<T>`
Update an existing record.

```typescript
const updatedUser = await client.update('users', '123', {
    name: 'John Updated',
    email: 'john.new@example.com'
}, {
    schema: 'public'
});
```

#### `delete(entity: string, id: string, options?): Promise<void>`
Delete a record.

```typescript
await client.delete('users', '123', {
    schema: 'public'
});
```

#### `meta<T>(entity: string, options?): Promise<T>`
Get metadata for an entity.

```typescript
const metadata = await client.meta('users', {
    schema: 'public'
});
console.log('Columns:', metadata.columns);
console.log('Primary key:', metadata.primary_key);
```

### Subscriptions

#### `subscribe(entity: string, callback: Function, options?): Promise<string>`
Subscribe to entity changes.

```typescript
const subscriptionId = await client.subscribe(
    'users',
    (notification) => {
        console.log('Operation:', notification.operation); // 'create', 'update', or 'delete'
        console.log('Data:', notification.data);
        console.log('Timestamp:', notification.timestamp);
    },
    {
        schema: 'public',
        filters: [
            { column: 'status', operator: 'eq', value: 'active' }
        ]
    }
);
```

#### `unsubscribe(subscriptionId: string): Promise<void>`
Unsubscribe from entity changes.

```typescript
await client.unsubscribe(subscriptionId);
```

#### `getSubscriptions(): Subscription[]`
Get list of active subscriptions.

```typescript
const subscriptions = client.getSubscriptions();
console.log('Active subscriptions:', subscriptions.length);
```

### Event Handling

#### `on(event: string, callback: Function): void`
Add event listener.

```typescript
// Connection events
client.on('connect', () => {
    console.log('Connected!');
});

client.on('disconnect', (event) => {
    console.log('Disconnected:', event.code, event.reason);
});

client.on('error', (error) => {
    console.error('Error:', error);
});

// State changes
client.on('stateChange', (state) => {
    console.log('State:', state);
});

// All messages
client.on('message', (message) => {
    console.log('Message:', message);
});
```

#### `off(event: string): void`
Remove event listener.

```typescript
client.off('connect');
```

## Filter Operators

- `eq` - Equal (=)
- `neq` - Not Equal (!=)
- `gt` - Greater Than (>)
- `gte` - Greater Than or Equal (>=)
- `lt` - Less Than (<)
- `lte` - Less Than or Equal (<=)
- `like` - LIKE (case-sensitive)
- `ilike` - ILIKE (case-insensitive)
- `in` - IN (array of values)

## Examples

### Basic CRUD

```typescript
const client = new WebSocketClient({ url: 'ws://localhost:8080/ws' });
await client.connect();

// Create
const user = await client.create('users', {
    name: 'Alice',
    email: 'alice@example.com'
});

// Read
const users = await client.read('users', {
    filters: [{ column: 'status', operator: 'eq', value: 'active' }]
});

// Update
await client.update('users', user.id, { name: 'Alice Updated' });

// Delete
await client.delete('users', user.id);

client.disconnect();
```

### Real-Time Subscriptions

```typescript
const client = new WebSocketClient({ url: 'ws://localhost:8080/ws' });
await client.connect();

// Subscribe to all user changes
const subId = await client.subscribe('users', (notification) => {
    switch (notification.operation) {
        case 'create':
            console.log('New user:', notification.data);
            break;
        case 'update':
            console.log('User updated:', notification.data);
            break;
        case 'delete':
            console.log('User deleted:', notification.data);
            break;
    }
});

// Later: unsubscribe
await client.unsubscribe(subId);
```

### React Integration

```typescript
import { useEffect, useState } from 'react';
import { WebSocketClient } from '@warkypublic/resolvespec-js';

function useWebSocket(url: string) {
    const [client] = useState(() => new WebSocketClient({ url }));
    const [isConnected, setIsConnected] = useState(false);

    useEffect(() => {
        client.on('connect', () => setIsConnected(true));
        client.on('disconnect', () => setIsConnected(false));
        client.connect();

        return () => client.disconnect();
    }, [client]);

    return { client, isConnected };
}

function UsersComponent() {
    const { client, isConnected } = useWebSocket('ws://localhost:8080/ws');
    const [users, setUsers] = useState([]);

    useEffect(() => {
        if (!isConnected) return;

        const loadUsers = async () => {
            // Subscribe to changes
            await client.subscribe('users', (notification) => {
                if (notification.operation === 'create') {
                    setUsers(prev => [...prev, notification.data]);
                } else if (notification.operation === 'update') {
                    setUsers(prev => prev.map(u =>
                        u.id === notification.data.id ? notification.data : u
                    ));
                } else if (notification.operation === 'delete') {
                    setUsers(prev => prev.filter(u => u.id !== notification.data.id));
                }
            });

            // Load initial data
            const data = await client.read('users');
            setUsers(data);
        };

        loadUsers();
    }, [client, isConnected]);

    return (
        <div>
            <h2>Users {isConnected ? '🟢' : '🔴'}</h2>
            {users.map(user => (
                <div key={user.id}>{user.name}</div>
            ))}
        </div>
    );
}
```

### TypeScript with Typed Models

```typescript
interface User {
    id: number;
    name: string;
    email: string;
    status: 'active' | 'inactive';
}

interface Post {
    id: number;
    title: string;
    content: string;
    user_id: number;
    user?: User;
}

const client = new WebSocketClient({ url: 'ws://localhost:8080/ws' });
await client.connect();

// Type-safe operations
const users = await client.read<User[]>('users', {
    filters: [{ column: 'status', operator: 'eq', value: 'active' }]
});

const newUser = await client.create<User>('users', {
    name: 'Bob',
    email: 'bob@example.com',
    status: 'active'
});

// Type-safe subscriptions
await client.subscribe(
    'posts',
    (notification) => {
        const post = notification.data as Post;
        console.log('Post:', post.title);
    }
);
```

### Error Handling

```typescript
const client = new WebSocketClient({
    url: 'ws://localhost:8080/ws',
    reconnect: true,
    maxReconnectAttempts: 5
});

client.on('error', (error) => {
    console.error('Connection error:', error);
});

client.on('stateChange', (state) => {
    console.log('State:', state);
    if (state === 'reconnecting') {
        console.log('Attempting to reconnect...');
    }
});

try {
    await client.connect();

    try {
        const user = await client.read('users', { record_id: '999' });
    } catch (error) {
        console.error('Record not found:', error);
    }

    try {
        await client.create('users', { /* invalid data */ });
    } catch (error) {
        console.error('Validation failed:', error);
    }

} catch (error) {
    console.error('Connection failed:', error);
}
```

### Multiple Subscriptions

```typescript
const client = new WebSocketClient({ url: 'ws://localhost:8080/ws' });
await client.connect();

// Subscribe to multiple entities
const userSub = await client.subscribe('users', (n) => {
    console.log('[Users]', n.operation, n.data);
});

const postSub = await client.subscribe('posts', (n) => {
    console.log('[Posts]', n.operation, n.data);
}, {
    filters: [{ column: 'status', operator: 'eq', value: 'published' }]
});

const commentSub = await client.subscribe('comments', (n) => {
    console.log('[Comments]', n.operation, n.data);
});

// Check active subscriptions
console.log('Active:', client.getSubscriptions().length);

// Clean up
await client.unsubscribe(userSub);
await client.unsubscribe(postSub);
await client.unsubscribe(commentSub);
```

## Best Practices

1. **Always Clean Up**: Call `disconnect()` when done to close the connection properly
2. **Use TypeScript**: Leverage type definitions for better type safety
3. **Handle Errors**: Always wrap operations in try-catch blocks
4. **Limit Subscriptions**: Don't create too many subscriptions per connection
5. **Use Filters**: Apply filters to subscriptions to reduce unnecessary notifications
6. **Connection State**: Check `isConnected()` before operations
7. **Event Listeners**: Remove event listeners when no longer needed with `off()`
8. **Reconnection**: Enable auto-reconnection for production apps

## Browser Support

- Chrome/Edge 88+
- Firefox 85+
- Safari 14+
- Node.js 14.16+

## License

MIT
