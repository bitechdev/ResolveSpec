// WebSocket Message Types
export type MessageType = 'request' | 'response' | 'notification' | 'subscription' | 'error' | 'ping' | 'pong';
export type WSOperation = 'read' | 'create' | 'update' | 'delete' | 'subscribe' | 'unsubscribe' | 'meta';

// Re-export common types
export type { FilterOption, SortOption, PreloadOption, Operator, SortDirection } from './types';

export interface WSOptions {
    filters?: import('./types').FilterOption[];
    columns?: string[];
    preload?: import('./types').PreloadOption[];
    sort?: import('./types').SortOption[];
    limit?: number;
    offset?: number;
}

export interface WSMessage {
    id?: string;
    type: MessageType;
    operation?: WSOperation;
    schema?: string;
    entity?: string;
    record_id?: string;
    data?: any;
    options?: WSOptions;
    subscription_id?: string;
    success?: boolean;
    error?: WSErrorInfo;
    metadata?: Record<string, any>;
    timestamp?: string;
}

export interface WSErrorInfo {
    code: string;
    message: string;
    details?: Record<string, any>;
}

export interface WSRequestMessage {
    id: string;
    type: 'request';
    operation: WSOperation;
    schema?: string;
    entity: string;
    record_id?: string;
    data?: any;
    options?: WSOptions;
}

export interface WSResponseMessage {
    id: string;
    type: 'response';
    success: boolean;
    data?: any;
    error?: WSErrorInfo;
    metadata?: Record<string, any>;
    timestamp: string;
}

export interface WSNotificationMessage {
    type: 'notification';
    operation: WSOperation;
    subscription_id: string;
    schema?: string;
    entity: string;
    data: any;
    timestamp: string;
}

export interface WSSubscriptionMessage {
    id: string;
    type: 'subscription';
    operation: 'subscribe' | 'unsubscribe';
    schema?: string;
    entity: string;
    options?: WSOptions;
    subscription_id?: string;
}

export interface SubscriptionOptions {
    filters?: import('./types').FilterOption[];
    onNotification?: (notification: WSNotificationMessage) => void;
}

export interface WebSocketClientConfig {
    url: string;
    reconnect?: boolean;
    reconnectInterval?: number;
    maxReconnectAttempts?: number;
    heartbeatInterval?: number;
    debug?: boolean;
}

export interface Subscription {
    id: string;
    entity: string;
    schema?: string;
    options?: WSOptions;
    callback?: (notification: WSNotificationMessage) => void;
}

export type ConnectionState = 'connecting' | 'connected' | 'disconnecting' | 'disconnected' | 'reconnecting';

export interface WebSocketClientEvents {
    connect: () => void;
    disconnect: (event: CloseEvent) => void;
    error: (error: Error) => void;
    message: (message: WSMessage) => void;
    stateChange: (state: ConnectionState) => void;
}
