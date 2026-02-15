import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ResolveSpecClient, getResolveSpecClient } from '../resolvespec/client';
import type { ClientConfig, APIResponse } from '../common/types';

const config: ClientConfig = { baseUrl: 'http://localhost:3000', token: 'test-token' };

function mockFetchResponse<T>(data: APIResponse<T>, ok = true, status = 200) {
    return vi.fn().mockResolvedValue({
        ok,
        status,
        json: () => Promise.resolve(data),
    });
}

beforeEach(() => {
    vi.restoreAllMocks();
});

describe('ResolveSpecClient', () => {
    it('read() sends POST with operation read', async () => {
        const response: APIResponse = { success: true, data: [{ id: 1 }] };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        const result = await client.read('public', 'users', 1);
        expect(result.success).toBe(true);

        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/1');
        expect(opts.method).toBe('POST');
        expect(opts.headers['Authorization']).toBe('Bearer test-token');

        const body = JSON.parse(opts.body);
        expect(body.operation).toBe('read');
    });

    it('read() with string array id puts id in body', async () => {
        const response: APIResponse = { success: true, data: [] };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        await client.read('public', 'users', ['1', '2']);
        const body = JSON.parse((globalThis.fetch as any).mock.calls[0][1].body);
        expect(body.id).toEqual(['1', '2']);
    });

    it('read() passes options through', async () => {
        const response: APIResponse = { success: true, data: [] };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        await client.read('public', 'users', undefined, {
            columns: ['id', 'name'],
            omit_columns: ['secret'],
            filters: [{ column: 'active', operator: 'eq', value: true }],
            sort: [{ column: 'name', direction: 'asc' }],
            limit: 10,
            offset: 0,
            cursor_forward: 'cursor1',
            fetch_row_number: '5',
        });

        const body = JSON.parse((globalThis.fetch as any).mock.calls[0][1].body);
        expect(body.options.columns).toEqual(['id', 'name']);
        expect(body.options.omit_columns).toEqual(['secret']);
        expect(body.options.cursor_forward).toBe('cursor1');
        expect(body.options.fetch_row_number).toBe('5');
    });

    it('create() sends POST with operation create and data', async () => {
        const response: APIResponse = { success: true, data: { id: 1, name: 'Test' } };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        const result = await client.create('public', 'users', { name: 'Test' });
        expect(result.data.name).toBe('Test');

        const body = JSON.parse((globalThis.fetch as any).mock.calls[0][1].body);
        expect(body.operation).toBe('create');
        expect(body.data.name).toBe('Test');
    });

    it('update() with single id puts id in URL', async () => {
        const response: APIResponse = { success: true, data: { id: 1 } };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        await client.update('public', 'users', { name: 'Updated' }, 1);
        const [url] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/1');
    });

    it('update() with string array id puts id in body', async () => {
        const response: APIResponse = { success: true, data: {} };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        await client.update('public', 'users', { active: false }, ['1', '2']);
        const body = JSON.parse((globalThis.fetch as any).mock.calls[0][1].body);
        expect(body.id).toEqual(['1', '2']);
    });

    it('delete() sends POST with operation delete', async () => {
        const response: APIResponse<void> = { success: true, data: undefined as any };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        await client.delete('public', 'users', 1);
        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/1');

        const body = JSON.parse(opts.body);
        expect(body.operation).toBe('delete');
    });

    it('getMetadata() sends GET request', async () => {
        const response: APIResponse = {
            success: true,
            data: { schema: 'public', table: 'users', columns: [], relations: [] },
        };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(config);
        const result = await client.getMetadata('public', 'users');
        expect(result.data.table).toBe('users');

        const opts = (globalThis.fetch as any).mock.calls[0][1];
        expect(opts.method).toBe('GET');
    });

    it('throws on non-ok response', async () => {
        const errorResp = {
            success: false,
            data: null,
            error: { code: 'not_found', message: 'Not found' },
        };
        globalThis.fetch = mockFetchResponse(errorResp as any, false, 404);

        const client = new ResolveSpecClient(config);
        await expect(client.read('public', 'users', 999)).rejects.toThrow('Not found');
    });

    it('throws generic error when no error message', async () => {
        globalThis.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ success: false, data: null }),
        });

        const client = new ResolveSpecClient(config);
        await expect(client.read('public', 'users')).rejects.toThrow('An error occurred');
    });

    it('config without token omits Authorization header', async () => {
        const noAuthConfig: ClientConfig = { baseUrl: 'http://localhost:3000' };
        const response: APIResponse = { success: true, data: [] };
        globalThis.fetch = mockFetchResponse(response);

        const client = new ResolveSpecClient(noAuthConfig);
        await client.read('public', 'users');
        const opts = (globalThis.fetch as any).mock.calls[0][1];
        expect(opts.headers['Authorization']).toBeUndefined();
    });
});

describe('getResolveSpecClient singleton', () => {
    it('returns same instance for same baseUrl', () => {
        const a = getResolveSpecClient({ baseUrl: 'http://singleton-test:3000' });
        const b = getResolveSpecClient({ baseUrl: 'http://singleton-test:3000' });
        expect(a).toBe(b);
    });

    it('returns different instances for different baseUrls', () => {
        const a = getResolveSpecClient({ baseUrl: 'http://singleton-a:3000' });
        const b = getResolveSpecClient({ baseUrl: 'http://singleton-b:3000' });
        expect(a).not.toBe(b);
    });
});
