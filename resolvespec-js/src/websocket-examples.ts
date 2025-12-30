import { WebSocketClient } from './websocket-client';
import type { WSNotificationMessage } from './websocket-types';

/**
 * Example 1: Basic Usage
 */
export async function basicUsageExample() {
    // Create client
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws',
        reconnect: true,
        debug: true
    });

    // Connect
    await client.connect();

    // Read users
    const users = await client.read('users', {
        schema: 'public',
        filters: [
            { column: 'status', operator: 'eq', value: 'active' }
        ],
        limit: 10,
        sort: [
            { column: 'name', direction: 'asc' }
        ]
    });

    console.log('Users:', users);

    // Create a user
    const newUser = await client.create('users', {
        name: 'John Doe',
        email: 'john@example.com',
        status: 'active'
    }, { schema: 'public' });

    console.log('Created user:', newUser);

    // Update user
    const updatedUser = await client.update('users', '123', {
        name: 'John Updated'
    }, { schema: 'public' });

    console.log('Updated user:', updatedUser);

    // Delete user
    await client.delete('users', '123', { schema: 'public' });

    // Disconnect
    client.disconnect();
}

/**
 * Example 2: Real-time Subscriptions
 */
export async function subscriptionExample() {
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws',
        debug: true
    });

    await client.connect();

    // Subscribe to user changes
    const subscriptionId = await client.subscribe(
        'users',
        (notification: WSNotificationMessage) => {
            console.log('User changed:', notification.operation, notification.data);

            switch (notification.operation) {
                case 'create':
                    console.log('New user created:', notification.data);
                    break;
                case 'update':
                    console.log('User updated:', notification.data);
                    break;
                case 'delete':
                    console.log('User deleted:', notification.data);
                    break;
            }
        },
        {
            schema: 'public',
            filters: [
                { column: 'status', operator: 'eq', value: 'active' }
            ]
        }
    );

    console.log('Subscribed with ID:', subscriptionId);

    // Later: unsubscribe
    setTimeout(async () => {
        await client.unsubscribe(subscriptionId);
        console.log('Unsubscribed');
        client.disconnect();
    }, 60000);
}

/**
 * Example 3: Event Handling
 */
export async function eventHandlingExample() {
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws'
    });

    // Listen to connection events
    client.on('connect', () => {
        console.log('Connected!');
    });

    client.on('disconnect', (event) => {
        console.log('Disconnected:', event.code, event.reason);
    });

    client.on('error', (error) => {
        console.error('WebSocket error:', error);
    });

    client.on('stateChange', (state) => {
        console.log('State changed to:', state);
    });

    client.on('message', (message) => {
        console.log('Received message:', message);
    });

    await client.connect();

    // Your operations here...
}

/**
 * Example 4: Multiple Subscriptions
 */
export async function multipleSubscriptionsExample() {
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws',
        debug: true
    });

    await client.connect();

    // Subscribe to users
    const userSubId = await client.subscribe(
        'users',
        (notification) => {
            console.log('[Users]', notification.operation, notification.data);
        },
        { schema: 'public' }
    );

    // Subscribe to posts
    const postSubId = await client.subscribe(
        'posts',
        (notification) => {
            console.log('[Posts]', notification.operation, notification.data);
        },
        {
            schema: 'public',
            filters: [
                { column: 'status', operator: 'eq', value: 'published' }
            ]
        }
    );

    // Subscribe to comments
    const commentSubId = await client.subscribe(
        'comments',
        (notification) => {
            console.log('[Comments]', notification.operation, notification.data);
        },
        { schema: 'public' }
    );

    console.log('Active subscriptions:', client.getSubscriptions());

    // Clean up after 60 seconds
    setTimeout(async () => {
        await client.unsubscribe(userSubId);
        await client.unsubscribe(postSubId);
        await client.unsubscribe(commentSubId);
        client.disconnect();
    }, 60000);
}

/**
 * Example 5: Advanced Queries
 */
export async function advancedQueriesExample() {
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws'
    });

    await client.connect();

    // Complex query with filters, sorting, pagination, and preloading
    const posts = await client.read('posts', {
        schema: 'public',
        filters: [
            { column: 'status', operator: 'eq', value: 'published' },
            { column: 'views', operator: 'gte', value: 100 }
        ],
        columns: ['id', 'title', 'content', 'user_id', 'created_at'],
        sort: [
            { column: 'created_at', direction: 'desc' },
            { column: 'views', direction: 'desc' }
        ],
        preload: [
            {
                relation: 'user',
                columns: ['id', 'name', 'email']
            },
            {
                relation: 'comments',
                columns: ['id', 'content', 'user_id'],
                filters: [
                    { column: 'status', operator: 'eq', value: 'approved' }
                ]
            }
        ],
        limit: 20,
        offset: 0
    });

    console.log('Posts:', posts);

    // Get single record by ID
    const post = await client.read('posts', {
        schema: 'public',
        record_id: '123'
    });

    console.log('Single post:', post);

    client.disconnect();
}

/**
 * Example 6: Error Handling
 */
export async function errorHandlingExample() {
    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws',
        reconnect: true,
        maxReconnectAttempts: 5
    });

    client.on('error', (error) => {
        console.error('Connection error:', error);
    });

    client.on('stateChange', (state) => {
        console.log('Connection state:', state);
    });

    try {
        await client.connect();

        try {
            // Try to read non-existent entity
            await client.read('nonexistent', { schema: 'public' });
        } catch (error) {
            console.error('Read error:', error);
        }

        try {
            // Try to create invalid record
            await client.create('users', {
                // Missing required fields
            }, { schema: 'public' });
        } catch (error) {
            console.error('Create error:', error);
        }

    } catch (error) {
        console.error('Connection failed:', error);
    } finally {
        client.disconnect();
    }
}

/**
 * Example 7: React Integration
 */
export function reactIntegrationExample() {
    const exampleCode = `
import { useEffect, useState } from 'react';
import { WebSocketClient } from '@warkypublic/resolvespec-js';

export function useWebSocket(url: string) {
    const [client] = useState(() => new WebSocketClient({ url }));
    const [isConnected, setIsConnected] = useState(false);

    useEffect(() => {
        client.on('connect', () => setIsConnected(true));
        client.on('disconnect', () => setIsConnected(false));

        client.connect();

        return () => {
            client.disconnect();
        };
    }, [client]);

    return { client, isConnected };
}

export function UsersComponent() {
    const { client, isConnected } = useWebSocket('ws://localhost:8080/ws');
    const [users, setUsers] = useState([]);

    useEffect(() => {
        if (!isConnected) return;

        // Subscribe to user changes
        const subscribeToUsers = async () => {
            const subId = await client.subscribe('users', (notification) => {
                if (notification.operation === 'create') {
                    setUsers(prev => [...prev, notification.data]);
                } else if (notification.operation === 'update') {
                    setUsers(prev => prev.map(u =>
                        u.id === notification.data.id ? notification.data : u
                    ));
                } else if (notification.operation === 'delete') {
                    setUsers(prev => prev.filter(u => u.id !== notification.data.id));
                }
            }, { schema: 'public' });

            // Load initial users
            const initialUsers = await client.read('users', {
                schema: 'public',
                filters: [{ column: 'status', operator: 'eq', value: 'active' }]
            });
            setUsers(initialUsers);

            return () => client.unsubscribe(subId);
        };

        subscribeToUsers();
    }, [client, isConnected]);

    const createUser = async (name: string, email: string) => {
        await client.create('users', { name, email, status: 'active' }, {
            schema: 'public'
        });
    };

    return (
        <div>
            <h2>Users ({users.length})</h2>
            {isConnected ? '🟢 Connected' : '🔴 Disconnected'}
            {/* Render users... */}
        </div>
    );
}
`;

    console.log(exampleCode);
}

/**
 * Example 8: TypeScript with Typed Models
 */
export async function typedModelsExample() {
    // Define your models
    interface User {
        id: number;
        name: string;
        email: string;
        status: 'active' | 'inactive';
        created_at: string;
    }

    interface Post {
        id: number;
        title: string;
        content: string;
        user_id: number;
        status: 'draft' | 'published';
        views: number;
        user?: User;
    }

    const client = new WebSocketClient({
        url: 'ws://localhost:8080/ws'
    });

    await client.connect();

    // Type-safe operations
    const users = await client.read<User[]>('users', {
        schema: 'public',
        filters: [{ column: 'status', operator: 'eq', value: 'active' }]
    });

    const newUser = await client.create<User>('users', {
        name: 'Alice',
        email: 'alice@example.com',
        status: 'active'
    }, { schema: 'public' });

    const posts = await client.read<Post[]>('posts', {
        schema: 'public',
        preload: [
            {
                relation: 'user',
                columns: ['id', 'name', 'email']
            }
        ]
    });

    // Type-safe subscriptions
    await client.subscribe(
        'users',
        (notification) => {
            const user = notification.data as User;
            console.log('User changed:', user.name, user.email);
        },
        { schema: 'public' }
    );

    client.disconnect();
}
