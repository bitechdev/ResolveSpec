import type { FilterOption, SortOption, PreloadOption, Parameter } from '../common/types';

// Re-export common types
export type { FilterOption, SortOption, PreloadOption, Operator, SortDirection } from '../common/types';

// WebSocket Message Types
export type MessageType = 'request' | 'response' | 'notification' | 'subscription' | 'error' | 'ping' | 'pong';
export type WSOperation = 'read' | 'create' | 'update' | 'delete' | 'subscribe' | 'unsubscribe' | 'meta';

export interface WSOptions {
    filters?: FilterOption[];
    columns?: string[];
    omit_columns?: string[];
    preload?: PreloadOption[];
    sort?: SortOption[];
    limit?: number;
    offset?: number;
    parameters?: Parameter[];
    cursor_forward?: string;
    cursor_backward?: string;
    fetch_row_number?: string;
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
    filters?: FilterOption[];
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
