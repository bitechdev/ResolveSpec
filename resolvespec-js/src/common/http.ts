import type { ClientConfig } from './types';

/** Merge HTTP headers case-insensitively, preserving the winning spelling. */
export function mergeHeaders(...sources: Record<string, string>[]): Record<string, string> {
    const result: Record<string, string> = {};
    for (const source of sources) {
        for (const [name, value] of Object.entries(source)) {
            for (const existing of Object.keys(result)) {
                if (existing.toLowerCase() === name.toLowerCase()) delete result[existing];
            }
            Object.defineProperty(result, name, { value, enumerable: true, configurable: true, writable: true });
        }
    }
    return result;
}

export function clientHeaders(config: ClientConfig): Record<string, string> {
    return mergeHeaders(
        { 'Content-Type': 'application/json' },
        config.headers ?? {},
        config.token ? { Authorization: `Bearer ${config.token}` } : {},
    );
}

export function clientCacheKey(config: ClientConfig): string {
    const headers = Object.entries(clientHeaders(config))
        .map(([name, value]) => [name.toLowerCase(), value])
        .sort(([a], [b]) => a.localeCompare(b));
    return JSON.stringify([config.baseUrl, headers]);
}
