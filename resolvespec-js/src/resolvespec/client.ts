import type { ClientConfig, APIResponse, TableMetadata, Options, RequestBody } from '../common/types';

const instances = new Map<string, ResolveSpecClient>();

export function getResolveSpecClient(config: ClientConfig): ResolveSpecClient {
    const key = config.baseUrl;
    let instance = instances.get(key);
    if (!instance) {
        instance = new ResolveSpecClient(config);
        instances.set(key, instance);
    }
    return instance;
}

export class ResolveSpecClient {
    private config: ClientConfig;

    constructor(config: ClientConfig) {
        this.config = config;
    }

    private buildUrl(schema: string, entity: string, id?: string): string {
        let url = `${this.config.baseUrl}/${schema}/${entity}`;
        if (id) {
            url += `/${id}`;
        }
        return url;
    }

    private baseHeaders(): HeadersInit {
        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
        };

        if (this.config.token) {
            headers['Authorization'] = `Bearer ${this.config.token}`;
        }

        return headers;
    }

    private async fetchWithError<T>(url: string, options: RequestInit): Promise<APIResponse<T>> {
        const response = await fetch(url, options);
        const data = await response.json();

        if (!response.ok) {
            throw new Error(data.error?.message || 'An error occurred');
        }

        return data;
    }

    async getMetadata(schema: string, entity: string): Promise<APIResponse<TableMetadata>> {
        const url = this.buildUrl(schema, entity);
        return this.fetchWithError<TableMetadata>(url, {
            method: 'GET',
            headers: this.baseHeaders(),
        });
    }

    async read<T = any>(
        schema: string,
        entity: string,
        id?: number | string | string[],
        options?: Options
    ): Promise<APIResponse<T>> {
        const urlId = typeof id === 'number' || typeof id === 'string' ? String(id) : undefined;
        const url = this.buildUrl(schema, entity, urlId);
        const body: RequestBody = {
            operation: 'read',
            id: Array.isArray(id) ? id : undefined,
            options,
        };

        return this.fetchWithError<T>(url, {
            method: 'POST',
            headers: this.baseHeaders(),
            body: JSON.stringify(body),
        });
    }

    async create<T = any>(
        schema: string,
        entity: string,
        data: any | any[],
        options?: Options
    ): Promise<APIResponse<T>> {
        const url = this.buildUrl(schema, entity);
        const body: RequestBody = {
            operation: 'create',
            data,
            options,
        };

        return this.fetchWithError<T>(url, {
            method: 'POST',
            headers: this.baseHeaders(),
            body: JSON.stringify(body),
        });
    }

    async update<T = any>(
        schema: string,
        entity: string,
        data: any | any[],
        id?: number | string | string[],
        options?: Options
    ): Promise<APIResponse<T>> {
        const urlId = typeof id === 'number' || typeof id === 'string' ? String(id) : undefined;
        const url = this.buildUrl(schema, entity, urlId);
        const body: RequestBody = {
            operation: 'update',
            id: Array.isArray(id) ? id : undefined,
            data,
            options,
        };

        return this.fetchWithError<T>(url, {
            method: 'POST',
            headers: this.baseHeaders(),
            body: JSON.stringify(body),
        });
    }

    async delete(
        schema: string,
        entity: string,
        id: number | string
    ): Promise<APIResponse<void>> {
        const url = this.buildUrl(schema, entity, String(id));
        const body: RequestBody = {
            operation: 'delete',
        };

        return this.fetchWithError<void>(url, {
            method: 'POST',
            headers: this.baseHeaders(),
            body: JSON.stringify(body),
        });
    }
}
