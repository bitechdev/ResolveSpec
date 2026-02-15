import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { WebSocketClient, getWebSocketClient } from '../websocketspec/client';
import type { WebSocketClientConfig } from '../websocketspec/types';

// Mock uuid
vi.mock('uuid', () => ({
    v4: vi.fn(() => 'mock-uuid-1234'),
}));

// Mock WebSocket
class MockWebSocket {
    static OPEN = 1;
    static CLOSED = 3;

    url: string;
    readyState = MockWebSocket.OPEN;
    onopen: ((ev: any) => void) | null = null;
    onclose: ((ev: any) => void) | null = null;
    onmessage: ((ev: any) => void) | null = null;
    onerror: ((ev: any) => void) | null = null;

    private sentMessages: string[] = [];

    constructor(url: string) {
        this.url = url;
        // Simulate async open
        setTimeout(() => {
            this.onopen?.({});
        }, 0);
    }

    send(data: string) {
        this.sentMessages.push(data);
    }

    close() {
        this.readyState = MockWebSocket.CLOSED;
        this.onclose?.({ code: 1000, reason: 'Normal closure' } as any);
    }

    getSentMessages(): any[] {
        return this.sentMessages.map((m) => JSON.parse(m));
    }

    simulateMessage(data: any) {
        this.onmessage?.({ data: JSON.stringify(data) });
    }
}

let mockWsInstance: MockWebSocket | null = null;

beforeEach(() => {
    mockWsInstance = null;
    (globalThis as any).WebSocket = class extends MockWebSocket {
        constructor(url: string) {
            super(url);
            mockWsInstance = this;
        }
    };
    (globalThis as any).WebSocket.OPEN = MockWebSocket.OPEN;
    (globalThis as any).WebSocket.CLOSED = MockWebSocket.CLOSED;
});

afterEach(() => {
    vi.restoreAllMocks();
});

describe('WebSocketClient', () => {
    const wsConfig: WebSocketClientConfig = {
        url: 'ws://localhost:8080',
        reconnect: false,
        heartbeatInterval: 60000,
    };

    it('should connect and set state to connected', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();
        expect(client.getState()).toBe('connected');
        expect(client.isConnected()).toBe(true);
        client.disconnect();
    });

    it('should disconnect and set state to disconnected', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();
        client.disconnect();
        expect(client.getState()).toBe('disconnected');
        expect(client.isConnected()).toBe(false);
    });

    it('should send read request', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const readPromise = client.read('users', {
            schema: 'public',
            filters: [{ column: 'active', operator: 'eq', value: true }],
            limit: 10,
        });

        // Simulate server response
        const sent = mockWsInstance!.getSentMessages();
        expect(sent.length).toBe(1);
        expect(sent[0].operation).toBe('read');
        expect(sent[0].entity).toBe('users');
        expect(sent[0].options.filters[0].column).toBe('active');

        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            data: [{ id: 1 }],
            timestamp: new Date().toISOString(),
        });

        const result = await readPromise;
        expect(result).toEqual([{ id: 1 }]);

        client.disconnect();
    });

    it('should send create request', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const createPromise = client.create('users', { name: 'Test' }, { schema: 'public' });

        const sent = mockWsInstance!.getSentMessages();
        expect(sent[0].operation).toBe('create');
        expect(sent[0].data.name).toBe('Test');

        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            data: { id: 1, name: 'Test' },
            timestamp: new Date().toISOString(),
        });

        const result = await createPromise;
        expect(result.name).toBe('Test');

        client.disconnect();
    });

    it('should send update request with record_id', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const updatePromise = client.update('users', '1', { name: 'Updated' });

        const sent = mockWsInstance!.getSentMessages();
        expect(sent[0].operation).toBe('update');
        expect(sent[0].record_id).toBe('1');

        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            data: { id: 1, name: 'Updated' },
            timestamp: new Date().toISOString(),
        });

        await updatePromise;
        client.disconnect();
    });

    it('should send delete request', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const deletePromise = client.delete('users', '1');

        const sent = mockWsInstance!.getSentMessages();
        expect(sent[0].operation).toBe('delete');
        expect(sent[0].record_id).toBe('1');

        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            timestamp: new Date().toISOString(),
        });

        await deletePromise;
        client.disconnect();
    });

    it('should reject on failed request', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const readPromise = client.read('users');

        const sent = mockWsInstance!.getSentMessages();
        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: false,
            error: { code: 'not_found', message: 'Not found' },
            timestamp: new Date().toISOString(),
        });

        await expect(readPromise).rejects.toThrow('Not found');
        client.disconnect();
    });

    it('should handle subscriptions', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        const callback = vi.fn();
        const subPromise = client.subscribe('users', callback, {
            schema: 'public',
        });

        const sent = mockWsInstance!.getSentMessages();
        expect(sent[0].type).toBe('subscription');
        expect(sent[0].operation).toBe('subscribe');

        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            data: { subscription_id: 'sub-1' },
            timestamp: new Date().toISOString(),
        });

        const subId = await subPromise;
        expect(subId).toBe('sub-1');
        expect(client.getSubscriptions()).toHaveLength(1);

        // Simulate notification
        mockWsInstance!.simulateMessage({
            type: 'notification',
            operation: 'create',
            subscription_id: 'sub-1',
            entity: 'users',
            data: { id: 2, name: 'New' },
            timestamp: new Date().toISOString(),
        });

        expect(callback).toHaveBeenCalledTimes(1);
        expect(callback.mock.calls[0][0].data.id).toBe(2);

        client.disconnect();
    });

    it('should handle unsubscribe', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        // Subscribe first
        const subPromise = client.subscribe('users', vi.fn());
        let sent = mockWsInstance!.getSentMessages();
        mockWsInstance!.simulateMessage({
            id: sent[0].id,
            type: 'response',
            success: true,
            data: { subscription_id: 'sub-1' },
            timestamp: new Date().toISOString(),
        });
        await subPromise;

        // Unsubscribe
        const unsubPromise = client.unsubscribe('sub-1');
        sent = mockWsInstance!.getSentMessages();
        mockWsInstance!.simulateMessage({
            id: sent[sent.length - 1].id,
            type: 'response',
            success: true,
            timestamp: new Date().toISOString(),
        });

        await unsubPromise;
        expect(client.getSubscriptions()).toHaveLength(0);

        client.disconnect();
    });

    it('should emit events', async () => {
        const client = new WebSocketClient(wsConfig);
        const connectCb = vi.fn();
        const stateChangeCb = vi.fn();

        client.on('connect', connectCb);
        client.on('stateChange', stateChangeCb);

        await client.connect();

        expect(connectCb).toHaveBeenCalledTimes(1);
        expect(stateChangeCb).toHaveBeenCalled();

        client.off('connect');
        client.disconnect();
    });

    it('should reject when sending without connection', async () => {
        const client = new WebSocketClient(wsConfig);
        await expect(client.read('users')).rejects.toThrow('WebSocket is not connected');
    });

    it('should handle pong messages without error', async () => {
        const client = new WebSocketClient(wsConfig);
        await client.connect();

        // Should not throw
        mockWsInstance!.simulateMessage({ type: 'pong' });

        client.disconnect();
    });

    it('should handle malformed messages gracefully', async () => {
        const client = new WebSocketClient({ ...wsConfig, debug: false });
        await client.connect();

        // Simulate non-JSON message
        mockWsInstance!.onmessage?.({ data: 'not-json' } as any);

        client.disconnect();
    });
});

describe('getWebSocketClient singleton', () => {
    it('returns same instance for same url', () => {
        const a = getWebSocketClient({ url: 'ws://ws-singleton:8080' });
        const b = getWebSocketClient({ url: 'ws://ws-singleton:8080' });
        expect(a).toBe(b);
    });

    it('returns different instances for different urls', () => {
        const a = getWebSocketClient({ url: 'ws://ws-singleton-a:8080' });
        const b = getWebSocketClient({ url: 'ws://ws-singleton-b:8080' });
        expect(a).not.toBe(b);
    });
});
