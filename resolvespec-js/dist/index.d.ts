export declare interface APIError {
    code: string;
    message: string;
    details?: any;
    detail?: string;
}

export declare interface APIResponse<T = any> {
    success: boolean;
    data: T;
    metadata?: Metadata;
    error?: APIError;
}

/**
 * Build HTTP headers from Options, matching Go's restheadspec handler conventions.
 *
 * Header mapping:
 *  - X-Select-Fields: comma-separated columns
 *  - X-Not-Select-Fields: comma-separated omit_columns
 *  - X-FieldFilter-{col}: exact match (eq)
 *  - X-SearchOp-{operator}-{col}: AND filter
 *  - X-SearchOr-{operator}-{col}: OR filter
 *  - X-Sort: +col (asc), -col (desc)
 *  - X-Limit, X-Offset: pagination
 *  - X-Cursor-Forward, X-Cursor-Backward: cursor pagination
 *  - X-Preload: RelationName:field1,field2 pipe-separated
 *  - X-Fetch-RowNumber: row number fetch
 *  - X-CQL-SEL-{col}: computed columns
 *  - X-Custom-SQL-W: custom operators (AND)
 */
export declare function buildHeaders(options: Options): Record<string, string>;

export declare interface ClientConfig {
    baseUrl: string;
    token?: string;
}

export declare interface Column {
    name: string;
    type: string;
    is_nullable: boolean;
    is_primary: boolean;
    is_unique: boolean;
    has_index: boolean;
}

export declare interface ComputedColumn {
    name: string;
    expression: string;
}

export declare type ConnectionState = 'connecting' | 'connected' | 'disconnecting' | 'disconnected' | 'reconnecting';

export declare interface CustomOperator {
    name: string;
    sql: string;
}

/**
 * Decode a header value that may be base64 encoded with ZIP_ or __ prefix.
 */
export declare function decodeHeaderValue(value: string): string;

/**
 * Encode a value with base64 and ZIP_ prefix for complex header values.
 */
export declare function encodeHeaderValue(value: string): string;

export declare interface FilterOption {
    column: string;
    operator: Operator | string;
    value: any;
    logic_operator?: 'AND' | 'OR';
}

export declare function getHeaderSpecClient(config: ClientConfig): HeaderSpecClient;

export declare function getResolveSpecClient(config: ClientConfig): ResolveSpecClient;

export declare function getWebSocketClient(config: WebSocketClientConfig): WebSocketClient;

/**
 * HeaderSpec REST client.
 * Sends query options via HTTP headers instead of request body, matching the Go restheadspec handler.
 *
 * HTTP methods: GET=read, POST=create, PUT=update, DELETE=delete
 */
export declare class HeaderSpecClient {
    private config;
    constructor(config: ClientConfig);
    private buildUrl;
    private baseHeaders;
    private fetchWithError;
    read<T = any>(schema: string, entity: string, id?: string, options?: Options): Promise<APIResponse<T>>;
    create<T = any>(schema: string, entity: string, data: any, options?: Options): Promise<APIResponse<T>>;
    update<T = any>(schema: string, entity: string, id: string, data: any, options?: Options): Promise<APIResponse<T>>;
    delete(schema: string, entity: string, id: string): Promise<APIResponse<void>>;
}

export declare type MessageType = 'request' | 'response' | 'notification' | 'subscription' | 'error' | 'ping' | 'pong';

export declare interface Metadata {
    total: number;
    count: number;
    filtered: number;
    limit: number;
    offset: number;
    row_number?: number;
}

export declare type Operation = 'read' | 'create' | 'update' | 'delete';

export declare type Operator = 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte' | 'like' | 'ilike' | 'in' | 'contains' | 'startswith' | 'endswith' | 'between' | 'between_inclusive' | 'is_null' | 'is_not_null';

export declare interface Options {
    preload?: PreloadOption[];
    columns?: string[];
    omit_columns?: string[];
    filters?: FilterOption[];
    sort?: SortOption[];
    limit?: number;
    offset?: number;
    customOperators?: CustomOperator[];
    computedColumns?: ComputedColumn[];
    parameters?: Parameter[];
    cursor_forward?: string;
    cursor_backward?: string;
    fetch_row_number?: string;
}

export declare interface Parameter {
    name: string;
    value: string;
    sequence?: number;
}

export declare interface PreloadOption {
    relation: string;
    table_name?: string;
    columns?: string[];
    omit_columns?: string[];
    sort?: SortOption[];
    filters?: FilterOption[];
    where?: string;
    limit?: number;
    offset?: number;
    updatable?: boolean;
    computed_ql?: Record<string, string>;
    recursive?: boolean;
    primary_key?: string;
    related_key?: string;
    foreign_key?: string;
    recursive_child_key?: string;
    sql_joins?: string[];
    join_aliases?: string[];
}

export declare interface RequestBody {
    operation: Operation;
    id?: number | string | string[];
    data?: any | any[];
    options?: Options;
}

export declare class ResolveSpecClient {
    private config;
    constructor(config: ClientConfig);
    private buildUrl;
    private baseHeaders;
    private fetchWithError;
    getMetadata(schema: string, entity: string): Promise<APIResponse<TableMetadata>>;
    read<T = any>(schema: string, entity: string, id?: number | string | string[], options?: Options): Promise<APIResponse<T>>;
    create<T = any>(schema: string, entity: string, data: any | any[], options?: Options): Promise<APIResponse<T>>;
    update<T = any>(schema: string, entity: string, data: any | any[], id?: number | string | string[], options?: Options): Promise<APIResponse<T>>;
    delete(schema: string, entity: string, id: number | string): Promise<APIResponse<void>>;
}

export declare type SortDirection = 'asc' | 'desc' | 'ASC' | 'DESC';

export declare interface SortOption {
    column: string;
    direction: SortDirection;
}

export declare interface Subscription {
    id: string;
    entity: string;
    schema?: string;
    options?: WSOptions;
    callback?: (notification: WSNotificationMessage) => void;
}

export declare interface SubscriptionOptions {
    filters?: FilterOption[];
    onNotification?: (notification: WSNotificationMessage) => void;
}

export declare interface TableMetadata {
    schema: string;
    table: string;
    columns: Column[];
    relations: string[];
}

export declare class WebSocketClient {
    private ws;
    private config;
    private messageHandlers;
    private subscriptions;
    private eventListeners;
    private state;
    private reconnectAttempts;
    private reconnectTimer;
    private heartbeatTimer;
    private isManualClose;
    constructor(config: WebSocketClientConfig);
    connect(): Promise<void>;
    disconnect(): void;
    request<T = any>(operation: WSOperation, entity: string, options?: {
        schema?: string;
        record_id?: string;
        data?: any;
        options?: WSOptions;
    }): Promise<T>;
    read<T = any>(entity: string, options?: {
        schema?: string;
        record_id?: string;
        filters?: FilterOption[];
        columns?: string[];
        sort?: SortOption[];
        preload?: PreloadOption[];
        limit?: number;
        offset?: number;
    }): Promise<T>;
    create<T = any>(entity: string, data: any, options?: {
        schema?: string;
    }): Promise<T>;
    update<T = any>(entity: string, id: string, data: any, options?: {
        schema?: string;
    }): Promise<T>;
    delete(entity: string, id: string, options?: {
        schema?: string;
    }): Promise<void>;
    meta<T = any>(entity: string, options?: {
        schema?: string;
    }): Promise<T>;
    subscribe(entity: string, callback: (notification: WSNotificationMessage) => void, options?: {
        schema?: string;
        filters?: FilterOption[];
    }): Promise<string>;
    unsubscribe(subscriptionId: string): Promise<void>;
    getSubscriptions(): Subscription[];
    getState(): ConnectionState;
    isConnected(): boolean;
    on<K extends keyof WebSocketClientEvents>(event: K, callback: WebSocketClientEvents[K]): void;
    off<K extends keyof WebSocketClientEvents>(event: K): void;
    private handleMessage;
    private handleResponse;
    private handleNotification;
    private send;
    private startHeartbeat;
    private stopHeartbeat;
    private setState;
    private ensureConnected;
    private emit;
    private log;
}

export declare interface WebSocketClientConfig {
    url: string;
    reconnect?: boolean;
    reconnectInterval?: number;
    maxReconnectAttempts?: number;
    heartbeatInterval?: number;
    debug?: boolean;
}

export declare interface WebSocketClientEvents {
    connect: () => void;
    disconnect: (event: CloseEvent) => void;
    error: (error: Error) => void;
    message: (message: WSMessage) => void;
    stateChange: (state: ConnectionState) => void;
}

export declare interface WSErrorInfo {
    code: string;
    message: string;
    details?: Record<string, any>;
}

export declare interface WSMessage {
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

export declare interface WSNotificationMessage {
    type: 'notification';
    operation: WSOperation;
    subscription_id: string;
    schema?: string;
    entity: string;
    data: any;
    timestamp: string;
}

export declare type WSOperation = 'read' | 'create' | 'update' | 'delete' | 'subscribe' | 'unsubscribe' | 'meta';

export declare interface WSOptions {
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

export declare interface WSRequestMessage {
    id: string;
    type: 'request';
    operation: WSOperation;
    schema?: string;
    entity: string;
    record_id?: string;
    data?: any;
    options?: WSOptions;
}

export declare interface WSResponseMessage {
    id: string;
    type: 'response';
    success: boolean;
    data?: any;
    error?: WSErrorInfo;
    metadata?: Record<string, any>;
    timestamp: string;
}

export declare interface WSSubscriptionMessage {
    id: string;
    type: 'subscription';
    operation: 'subscribe' | 'unsubscribe';
    schema?: string;
    entity: string;
    options?: WSOptions;
    subscription_id?: string;
}

export { }
