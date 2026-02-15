import { describe, it, expect, vi, beforeEach } from 'vitest';
import { buildHeaders, encodeHeaderValue, decodeHeaderValue, HeaderSpecClient, getHeaderSpecClient } from '../headerspec/client';
import type { Options, ClientConfig, APIResponse } from '../common/types';

describe('buildHeaders', () => {
    it('should set X-Select-Fields for columns', () => {
        const h = buildHeaders({ columns: ['id', 'name', 'email'] });
        expect(h['X-Select-Fields']).toBe('id,name,email');
    });

    it('should set X-Not-Select-Fields for omit_columns', () => {
        const h = buildHeaders({ omit_columns: ['secret', 'internal'] });
        expect(h['X-Not-Select-Fields']).toBe('secret,internal');
    });

    it('should set X-FieldFilter for eq AND filters', () => {
        const h = buildHeaders({
            filters: [{ column: 'status', operator: 'eq', value: 'active' }],
        });
        expect(h['X-FieldFilter-status']).toBe('active');
    });

    it('should set X-SearchOp for non-eq AND filters', () => {
        const h = buildHeaders({
            filters: [{ column: 'age', operator: 'gte', value: 18 }],
        });
        expect(h['X-SearchOp-greaterthanorequal-age']).toBe('18');
    });

    it('should set X-SearchOr for OR filters', () => {
        const h = buildHeaders({
            filters: [{ column: 'name', operator: 'contains', value: 'test', logic_operator: 'OR' }],
        });
        expect(h['X-SearchOr-contains-name']).toBe('test');
    });

    it('should set X-Sort with direction prefixes', () => {
        const h = buildHeaders({
            sort: [
                { column: 'name', direction: 'asc' },
                { column: 'created_at', direction: 'DESC' },
            ],
        });
        expect(h['X-Sort']).toBe('+name,-created_at');
    });

    it('should set X-Limit and X-Offset', () => {
        const h = buildHeaders({ limit: 25, offset: 50 });
        expect(h['X-Limit']).toBe('25');
        expect(h['X-Offset']).toBe('50');
    });

    it('should set cursor pagination headers', () => {
        const h = buildHeaders({ cursor_forward: 'abc', cursor_backward: 'xyz' });
        expect(h['X-Cursor-Forward']).toBe('abc');
        expect(h['X-Cursor-Backward']).toBe('xyz');
    });

    it('should set X-Preload with pipe-separated relations', () => {
        const h = buildHeaders({
            preload: [
                { relation: 'Items', columns: ['id', 'name'] },
                { relation: 'Category' },
            ],
        });
        expect(h['X-Preload']).toBe('Items:id,name|Category');
    });

    it('should set X-Fetch-RowNumber', () => {
        const h = buildHeaders({ fetch_row_number: '42' });
        expect(h['X-Fetch-RowNumber']).toBe('42');
    });

    it('should set X-CQL-SEL for computed columns', () => {
        const h = buildHeaders({
            computedColumns: [
                { name: 'total', expression: 'price * qty' },
            ],
        });
        expect(h['X-CQL-SEL-total']).toBe('price * qty');
    });

    it('should set X-Custom-SQL-W for custom operators', () => {
        const h = buildHeaders({
            customOperators: [
                { name: 'active', sql: "status = 'active'" },
                { name: 'verified', sql: "verified = true" },
            ],
        });
        expect(h['X-Custom-SQL-W']).toBe("status = 'active' AND verified = true");
    });

    it('should return empty object for empty options', () => {
        const h = buildHeaders({});
        expect(Object.keys(h)).toHaveLength(0);
    });

    it('should handle between filter with array value', () => {
        const h = buildHeaders({
            filters: [{ column: 'price', operator: 'between', value: [10, 100] }],
        });
        expect(h['X-SearchOp-between-price']).toBe('10,100');
    });

    it('should handle is_null filter with null value', () => {
        const h = buildHeaders({
            filters: [{ column: 'deleted_at', operator: 'is_null', value: null }],
        });
        expect(h['X-SearchOp-empty-deleted_at']).toBe('');
    });

    it('should handle in filter with array value', () => {
        const h = buildHeaders({
            filters: [{ column: 'id', operator: 'in', value: [1, 2, 3] }],
        });
        expect(h['X-SearchOp-in-id']).toBe('1,2,3');
    });
});

describe('encodeHeaderValue / decodeHeaderValue', () => {
    it('should round-trip encode/decode', () => {
        const original = 'some complex value with spaces & symbols!';
        const encoded = encodeHeaderValue(original);
        expect(encoded.startsWith('ZIP_')).toBe(true);
        const decoded = decodeHeaderValue(encoded);
        expect(decoded).toBe(original);
    });

    it('should decode __ prefixed values', () => {
        const encoded = '__' + btoa('hello');
        expect(decodeHeaderValue(encoded)).toBe('hello');
    });

    it('should return plain values as-is', () => {
        expect(decodeHeaderValue('plain')).toBe('plain');
    });
});

describe('HeaderSpecClient', () => {
    const config: ClientConfig = { baseUrl: 'http://localhost:3000', token: 'tok' };

    function mockFetch<T>(data: APIResponse<T>, ok = true) {
        return vi.fn().mockResolvedValue({
            ok,
            json: () => Promise.resolve(data),
        });
    }

    beforeEach(() => {
        vi.restoreAllMocks();
    });

    it('read() sends GET with headers from options', async () => {
        globalThis.fetch = mockFetch({ success: true, data: [{ id: 1 }] });
        const client = new HeaderSpecClient(config);

        await client.read('public', 'users', undefined, {
            columns: ['id', 'name'],
            limit: 10,
        });

        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users');
        expect(opts.method).toBe('GET');
        expect(opts.headers['X-Select-Fields']).toBe('id,name');
        expect(opts.headers['X-Limit']).toBe('10');
        expect(opts.headers['Authorization']).toBe('Bearer tok');
    });

    it('read() with id appends to URL', async () => {
        globalThis.fetch = mockFetch({ success: true, data: {} });
        const client = new HeaderSpecClient(config);

        await client.read('public', 'users', '42');

        const [url] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/42');
    });

    it('create() sends POST with body and headers', async () => {
        globalThis.fetch = mockFetch({ success: true, data: { id: 1 } });
        const client = new HeaderSpecClient(config);

        await client.create('public', 'users', { name: 'Test' });

        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(opts.method).toBe('POST');
        expect(JSON.parse(opts.body)).toEqual({ name: 'Test' });
    });

    it('update() sends PUT with id in URL', async () => {
        globalThis.fetch = mockFetch({ success: true, data: {} });
        const client = new HeaderSpecClient(config);

        await client.update('public', 'users', '1', { name: 'Updated' }, {
            filters: [{ column: 'active', operator: 'eq', value: true }],
        });

        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/1');
        expect(opts.method).toBe('PUT');
        expect(opts.headers['X-FieldFilter-active']).toBe('true');
    });

    it('delete() sends DELETE', async () => {
        globalThis.fetch = mockFetch({ success: true, data: undefined as any });
        const client = new HeaderSpecClient(config);

        await client.delete('public', 'users', '1');

        const [url, opts] = (globalThis.fetch as any).mock.calls[0];
        expect(url).toBe('http://localhost:3000/public/users/1');
        expect(opts.method).toBe('DELETE');
    });

    it('throws on non-ok response', async () => {
        globalThis.fetch = mockFetch(
            { success: false, data: null as any, error: { code: 'err', message: 'fail' } },
            false
        );
        const client = new HeaderSpecClient(config);

        await expect(client.read('public', 'users')).rejects.toThrow('fail');
    });
});

describe('getHeaderSpecClient singleton', () => {
    it('returns same instance for same baseUrl', () => {
        const a = getHeaderSpecClient({ baseUrl: 'http://hs-singleton:3000' });
        const b = getHeaderSpecClient({ baseUrl: 'http://hs-singleton:3000' });
        expect(a).toBe(b);
    });

    it('returns different instances for different baseUrls', () => {
        const a = getHeaderSpecClient({ baseUrl: 'http://hs-singleton-a:3000' });
        const b = getHeaderSpecClient({ baseUrl: 'http://hs-singleton-b:3000' });
        expect(a).not.toBe(b);
    });
});
