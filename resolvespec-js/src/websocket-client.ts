import { v4 as uuidv4 } from 'uuid';
import type {
    WebSocketClientConfig,
    WSMessage,
    WSRequestMessage,
    WSResponseMessage,
    WSNotificationMessage,
    WSOperation,
    WSOptions,
    Subscription,
    SubscriptionOptions,
    ConnectionState,
    WebSocketClientEvents
} from './websocket-types';

export class WebSocketClient {
    private ws: WebSocket | null = null;
    private config: Required<WebSocketClientConfig>;
    private messageHandlers: Map<string, (message: WSResponseMessage) => void> = new Map();
    private subscriptions: Map<string, Subscription> = new Map();
    private eventListeners: Partial<WebSocketClientEvents> = {};
    private state: ConnectionState = 'disconnected';
    private reconnectAttempts = 0;
    private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
    private isManualClose = false;

    constructor(config: WebSocketClientConfig) {
        this.config = {
            url: config.url,
            reconnect: config.reconnect ?? true,
            reconnectInterval: config.reconnectInterval ?? 3000,
            maxReconnectAttempts: config.maxReconnectAttempts ?? 10,
            heartbeatInterval: config.heartbeatInterval ?? 30000,
            debug: config.debug ?? false
        };
    }

    /**
     * Connect to WebSocket server
     */
    async connect(): Promise<void> {
        if (this.ws?.readyState === WebSocket.OPEN) {
            this.log('Already connected');
            return;
        }

        this.isManualClose = false;
        this.setState('connecting');

        return new Promise((resolve, reject) => {
            try {
                this.ws = new WebSocket(this.config.url);

                this.ws.onopen = () => {
                    this.log('Connected to WebSocket server');
                    this.setState('connected');
                    this.reconnectAttempts = 0;
                    this.startHeartbeat();
                    this.emit('connect');
                    resolve();
                };

                this.ws.onmessage = (event) => {
                    this.handleMessage(event.data);
                };

                this.ws.onerror = (event) => {
                    this.log('WebSocket error:', event);
                    const error = new Error('WebSocket connection error');
                    this.emit('error', error);
                    reject(error);
                };

                this.ws.onclose = (event) => {
                    this.log('WebSocket closed:', event.code, event.reason);
                    this.stopHeartbeat();
                    this.setState('disconnected');
                    this.emit('disconnect', event);

                    // Attempt reconnection if enabled and not manually closed
                    if (this.config.reconnect && !this.isManualClose && this.reconnectAttempts < this.config.maxReconnectAttempts) {
                        this.reconnectAttempts++;
                        this.log(`Reconnection attempt ${this.reconnectAttempts}/${this.config.maxReconnectAttempts}`);
                        this.setState('reconnecting');

                        this.reconnectTimer = setTimeout(() => {
                            this.connect().catch((err) => {
                                this.log('Reconnection failed:', err);
                            });
                        }, this.config.reconnectInterval);
                    }
                };
            } catch (error) {
                reject(error);
            }
        });
    }

    /**
     * Disconnect from WebSocket server
     */
    disconnect(): void {
        this.isManualClose = true;

        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
        }

        this.stopHeartbeat();

        if (this.ws) {
            this.setState('disconnecting');
            this.ws.close();
            this.ws = null;
        }

        this.setState('disconnected');
        this.messageHandlers.clear();
    }

    /**
     * Send a CRUD request and wait for response
     */
    async request<T = any>(
        operation: WSOperation,
        entity: string,
        options?: {
            schema?: string;
            record_id?: string;
            data?: any;
            options?: WSOptions;
        }
    ): Promise<T> {
        this.ensureConnected();

        const id = uuidv4();
        const message: WSRequestMessage = {
            id,
            type: 'request',
            operation,
            entity,
            schema: options?.schema,
            record_id: options?.record_id,
            data: options?.data,
            options: options?.options
        };

        return new Promise((resolve, reject) => {
            // Set up response handler
            this.messageHandlers.set(id, (response: WSResponseMessage) => {
                if (response.success) {
                    resolve(response.data);
                } else {
                    reject(new Error(response.error?.message || 'Request failed'));
                }
            });

            // Send message
            this.send(message);

            // Timeout after 30 seconds
            setTimeout(() => {
                if (this.messageHandlers.has(id)) {
                    this.messageHandlers.delete(id);
                    reject(new Error('Request timeout'));
                }
            }, 30000);
        });
    }

    /**
     * Read records
     */
    async read<T = any>(entity: string, options?: {
        schema?: string;
        record_id?: string;
        filters?: import('./types').FilterOption[];
        columns?: string[];
        sort?: import('./types').SortOption[];
        preload?: import('./types').PreloadOption[];
        limit?: number;
        offset?: number;
    }): Promise<T> {
        return this.request<T>('read', entity, {
            schema: options?.schema,
            record_id: options?.record_id,
            options: {
                filters: options?.filters,
                columns: options?.columns,
                sort: options?.sort,
                preload: options?.preload,
                limit: options?.limit,
                offset: options?.offset
            }
        });
    }

    /**
     * Create a record
     */
    async create<T = any>(entity: string, data: any, options?: {
        schema?: string;
    }): Promise<T> {
        return this.request<T>('create', entity, {
            schema: options?.schema,
            data
        });
    }

    /**
     * Update a record
     */
    async update<T = any>(entity: string, id: string, data: any, options?: {
        schema?: string;
    }): Promise<T> {
        return this.request<T>('update', entity, {
            schema: options?.schema,
            record_id: id,
            data
        });
    }

    /**
     * Delete a record
     */
    async delete(entity: string, id: string, options?: {
        schema?: string;
    }): Promise<void> {
        await this.request('delete', entity, {
            schema: options?.schema,
            record_id: id
        });
    }

    /**
     * Get metadata for an entity
     */
    async meta<T = any>(entity: string, options?: {
        schema?: string;
    }): Promise<T> {
        return this.request<T>('meta', entity, {
            schema: options?.schema
        });
    }

    /**
     * Subscribe to entity changes
     */
    async subscribe(
        entity: string,
        callback: (notification: WSNotificationMessage) => void,
        options?: {
            schema?: string;
            filters?: import('./types').FilterOption[];
        }
    ): Promise<string> {
        this.ensureConnected();

        const id = uuidv4();
        const message: WSMessage = {
            id,
            type: 'subscription',
            operation: 'subscribe',
            entity,
            schema: options?.schema,
            options: {
                filters: options?.filters
            }
        };

        return new Promise((resolve, reject) => {
            this.messageHandlers.set(id, (response: WSResponseMessage) => {
                if (response.success && response.data?.subscription_id) {
                    const subscriptionId = response.data.subscription_id;

                    // Store subscription
                    this.subscriptions.set(subscriptionId, {
                        id: subscriptionId,
                        entity,
                        schema: options?.schema,
                        options: { filters: options?.filters },
                        callback
                    });

                    this.log(`Subscribed to ${entity} with ID: ${subscriptionId}`);
                    resolve(subscriptionId);
                } else {
                    reject(new Error(response.error?.message || 'Subscription failed'));
                }
            });

            this.send(message);

            // Timeout
            setTimeout(() => {
                if (this.messageHandlers.has(id)) {
                    this.messageHandlers.delete(id);
                    reject(new Error('Subscription timeout'));
                }
            }, 10000);
        });
    }

    /**
     * Unsubscribe from entity changes
     */
    async unsubscribe(subscriptionId: string): Promise<void> {
        this.ensureConnected();

        const id = uuidv4();
        const message: WSMessage = {
            id,
            type: 'subscription',
            operation: 'unsubscribe',
            subscription_id: subscriptionId
        };

        return new Promise((resolve, reject) => {
            this.messageHandlers.set(id, (response: WSResponseMessage) => {
                if (response.success) {
                    this.subscriptions.delete(subscriptionId);
                    this.log(`Unsubscribed from ${subscriptionId}`);
                    resolve();
                } else {
                    reject(new Error(response.error?.message || 'Unsubscribe failed'));
                }
            });

            this.send(message);

            // Timeout
            setTimeout(() => {
                if (this.messageHandlers.has(id)) {
                    this.messageHandlers.delete(id);
                    reject(new Error('Unsubscribe timeout'));
                }
            }, 10000);
        });
    }

    /**
     * Get list of active subscriptions
     */
    getSubscriptions(): Subscription[] {
        return Array.from(this.subscriptions.values());
    }

    /**
     * Get connection state
     */
    getState(): ConnectionState {
        return this.state;
    }

    /**
     * Check if connected
     */
    isConnected(): boolean {
        return this.ws?.readyState === WebSocket.OPEN;
    }

    /**
     * Add event listener
     */
    on<K extends keyof WebSocketClientEvents>(event: K, callback: WebSocketClientEvents[K]): void {
        this.eventListeners[event] = callback as any;
    }

    /**
     * Remove event listener
     */
    off<K extends keyof WebSocketClientEvents>(event: K): void {
        delete this.eventListeners[event];
    }

    // Private methods

    private handleMessage(data: string): void {
        try {
            const message: WSMessage = JSON.parse(data);
            this.log('Received message:', message);

            this.emit('message', message);

            // Handle different message types
            switch (message.type) {
                case 'response':
                    this.handleResponse(message as WSResponseMessage);
                    break;

                case 'notification':
                    this.handleNotification(message as WSNotificationMessage);
                    break;

                case 'pong':
                    // Heartbeat response
                    break;

                default:
                    this.log('Unknown message type:', message.type);
            }
        } catch (error) {
            this.log('Error parsing message:', error);
        }
    }

    private handleResponse(message: WSResponseMessage): void {
        const handler = this.messageHandlers.get(message.id);
        if (handler) {
            handler(message);
            this.messageHandlers.delete(message.id);
        }
    }

    private handleNotification(message: WSNotificationMessage): void {
        const subscription = this.subscriptions.get(message.subscription_id);
        if (subscription?.callback) {
            subscription.callback(message);
        }
    }

    private send(message: WSMessage): void {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            throw new Error('WebSocket is not connected');
        }

        const data = JSON.stringify(message);
        this.log('Sending message:', message);
        this.ws.send(data);
    }

    private startHeartbeat(): void {
        if (this.heartbeatTimer) {
            return;
        }

        this.heartbeatTimer = setInterval(() => {
            if (this.isConnected()) {
                const pingMessage: WSMessage = {
                    id: uuidv4(),
                    type: 'ping'
                };
                this.send(pingMessage);
            }
        }, this.config.heartbeatInterval);
    }

    private stopHeartbeat(): void {
        if (this.heartbeatTimer) {
            clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = null;
        }
    }

    private setState(state: ConnectionState): void {
        if (this.state !== state) {
            this.state = state;
            this.emit('stateChange', state);
        }
    }

    private ensureConnected(): void {
        if (!this.isConnected()) {
            throw new Error('WebSocket is not connected. Call connect() first.');
        }
    }

    private emit<K extends keyof WebSocketClientEvents>(
        event: K,
        ...args: Parameters<WebSocketClientEvents[K]>
    ): void {
        const listener = this.eventListeners[event];
        if (listener) {
            (listener as any)(...args);
        }
    }

    private log(...args: any[]): void {
        if (this.config.debug) {
            console.log('[WebSocketClient]', ...args);
        }
    }
}

export default WebSocketClient;
