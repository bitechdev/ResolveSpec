import { afterEach, describe, expect, it, vi } from 'vitest';
import { ResolveSpecClient, getResolveSpecClient } from '../resolvespec/client';
import { HeaderSpecClient, getHeaderSpecClient } from '../headerspec/client';

afterEach(() => vi.unstubAllGlobals());

for (const [name, Client, factory] of [
    ['ResolveSpec', ResolveSpecClient, getResolveSpecClient],
    ['HeaderSpec', HeaderSpecClient, getHeaderSpecClient],
] as const) {
    describe(`${name} custom headers`, () => {
        it('sends tenant headers on every operation and resolves collisions case-insensitively', async () => {
            const fetchMock = vi.fn().mockResolvedValue({
                ok: true, headers: new Headers(), json: async () => ({ success: true, data: [] }),
            });
            vi.stubGlobal('fetch', fetchMock);
            const headers = { 'X-Tenant': 'acme', authorization: 'Basic ignored', 'content-type': 'application/custom+json', 'x-limit': '99' };
            const client = new Client({ baseUrl: 'http://localhost:3000', token: 'tok', headers });
            await client.read('public', 'users', undefined, { limit: 10 });
            await client.create('public', 'users', {});
            if (client instanceof ResolveSpecClient) {
                await client.update('public', 'users', {}, '1');
                await client.getMetadata('public', 'users');
            } else {
                await client.update('public', 'users', '1', {});
            }
            await client.delete('public', 'users', '1');
            for (const [, init] of fetchMock.mock.calls) {
                const sent = new Headers(init.headers);
                expect(sent.get('x-tenant')).toBe('acme');
                expect(sent.get('authorization')).toBe('Bearer tok');
                expect(sent.get('content-type')).toBe('application/custom+json');
            }
            if (client instanceof HeaderSpecClient) {
                expect(new Headers(fetchMock.mock.calls[0][1].headers).get('x-limit')).toBe('10');
            }
            expect(headers.authorization).toBe('Basic ignored');
            expect(headers['x-limit']).toBe('99');
        });

        it('supports custom authentication without a token', async () => {
            const fetchMock = vi.fn().mockResolvedValue({
                ok: true, headers: new Headers(), json: async () => ({ success: true, data: [] }),
            });
            vi.stubGlobal('fetch', fetchMock);
            await new Client({ baseUrl: 'http://localhost:3000', headers: { Authorization: 'Basic custom' } }).read('public', 'users');
            expect(new Headers(fetchMock.mock.calls[0][1].headers).get('authorization')).toBe('Basic custom');
        });

        it('isolates cached clients by headers and token, and snapshots configuration', async () => {
            const config = { baseUrl: 'http://tenant-cache', token: 'one', headers: { 'X-Tenant': 'acme', 'X-App': 'grid' } };
            const first = factory(config);
            expect(factory({ ...config, headers: { 'x-app': 'grid', 'x-tenant': 'acme' } })).toBe(first);
            expect(factory({ ...config, token: 'two' })).not.toBe(first);
            config.headers['X-Tenant'] = 'other';
            expect(factory(config)).not.toBe(first);
            const fetchMock = vi.fn().mockResolvedValue({
                ok: true, headers: new Headers(), json: async () => ({ success: true, data: [] }),
            });
            vi.stubGlobal('fetch', fetchMock);
            await first.read('public', 'users');
            expect(new Headers(fetchMock.mock.calls[0][1].headers).get('x-tenant')).toBe('acme');
        });
    });
}
