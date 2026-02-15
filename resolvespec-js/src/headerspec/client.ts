import type {
    ClientConfig,
    APIResponse,
    Options,
    FilterOption,
    SortOption,
    PreloadOption,
    CustomOperator,
} from '../common/types';

/**
 * Encode a value with base64 and ZIP_ prefix for complex header values.
 */
export function encodeHeaderValue(value: string): string {
    if (typeof btoa === 'function') {
        return 'ZIP_' + btoa(value);
    }
    return 'ZIP_' + Buffer.from(value, 'utf-8').toString('base64');
}

/**
 * Decode a header value that may be base64 encoded with ZIP_ or __ prefix.
 */
export function decodeHeaderValue(value: string): string {
    let code = value;

    if (code.startsWith('ZIP_')) {
        code = code.slice(4).replace(/[\n\r ]/g, '');
        code = decodeBase64(code);
    } else if (code.startsWith('__')) {
        code = code.slice(2).replace(/[\n\r ]/g, '');
        code = decodeBase64(code);
    }

    // Handle nested encoding
    if (code.startsWith('ZIP_') || code.startsWith('__')) {
        code = decodeHeaderValue(code);
    }

    return code;
}

function decodeBase64(str: string): string {
    if (typeof atob === 'function') {
        return atob(str);
    }
    return Buffer.from(str, 'base64').toString('utf-8');
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
export function buildHeaders(options: Options): Record<string, string> {
    const headers: Record<string, string> = {};

    // Column selection
    if (options.columns?.length) {
        headers['X-Select-Fields'] = options.columns.join(',');
    }

    if (options.omit_columns?.length) {
        headers['X-Not-Select-Fields'] = options.omit_columns.join(',');
    }

    // Filters
    if (options.filters?.length) {
        for (const filter of options.filters) {
            const logicOp = filter.logic_operator ?? 'AND';
            const op = mapOperatorToHeaderOp(filter.operator);
            const valueStr = formatFilterValue(filter);

            if (filter.operator === 'eq' && logicOp === 'AND') {
                // Simple field filter shorthand
                headers[`X-FieldFilter-${filter.column}`] = valueStr;
            } else if (logicOp === 'OR') {
                headers[`X-SearchOr-${op}-${filter.column}`] = valueStr;
            } else {
                headers[`X-SearchOp-${op}-${filter.column}`] = valueStr;
            }
        }
    }

    // Sort
    if (options.sort?.length) {
        const sortParts = options.sort.map((s: SortOption) => {
            const dir = s.direction.toUpperCase();
            return dir === 'DESC' ? `-${s.column}` : `+${s.column}`;
        });
        headers['X-Sort'] = sortParts.join(',');
    }

    // Pagination
    if (options.limit !== undefined) {
        headers['X-Limit'] = String(options.limit);
    }
    if (options.offset !== undefined) {
        headers['X-Offset'] = String(options.offset);
    }

    // Cursor pagination
    if (options.cursor_forward) {
        headers['X-Cursor-Forward'] = options.cursor_forward;
    }
    if (options.cursor_backward) {
        headers['X-Cursor-Backward'] = options.cursor_backward;
    }

    // Preload
    if (options.preload?.length) {
        const parts = options.preload.map((p: PreloadOption) => {
            if (p.columns?.length) {
                return `${p.relation}:${p.columns.join(',')}`;
            }
            return p.relation;
        });
        headers['X-Preload'] = parts.join('|');
    }

    // Fetch row number
    if (options.fetch_row_number) {
        headers['X-Fetch-RowNumber'] = options.fetch_row_number;
    }

    // Computed columns
    if (options.computedColumns?.length) {
        for (const cc of options.computedColumns) {
            headers[`X-CQL-SEL-${cc.name}`] = cc.expression;
        }
    }

    // Custom operators -> X-Custom-SQL-W
    if (options.customOperators?.length) {
        const sqlParts = options.customOperators.map((co: CustomOperator) => co.sql);
        headers['X-Custom-SQL-W'] = sqlParts.join(' AND ');
    }

    return headers;
}

function mapOperatorToHeaderOp(operator: string): string {
    switch (operator) {
        case 'eq': return 'equals';
        case 'neq': return 'notequals';
        case 'gt': return 'greaterthan';
        case 'gte': return 'greaterthanorequal';
        case 'lt': return 'lessthan';
        case 'lte': return 'lessthanorequal';
        case 'like':
        case 'ilike':
        case 'contains': return 'contains';
        case 'startswith': return 'beginswith';
        case 'endswith': return 'endswith';
        case 'in': return 'in';
        case 'between': return 'between';
        case 'between_inclusive': return 'betweeninclusive';
        case 'is_null': return 'empty';
        case 'is_not_null': return 'notempty';
        default: return operator;
    }
}

function formatFilterValue(filter: FilterOption): string {
    if (filter.value === null || filter.value === undefined) {
        return '';
    }
    if (Array.isArray(filter.value)) {
        return filter.value.join(',');
    }
    return String(filter.value);
}

const instances = new Map<string, HeaderSpecClient>();

export function getHeaderSpecClient(config: ClientConfig): HeaderSpecClient {
    const key = config.baseUrl;
    let instance = instances.get(key);
    if (!instance) {
        instance = new HeaderSpecClient(config);
        instances.set(key, instance);
    }
    return instance;
}

/**
 * HeaderSpec REST client.
 * Sends query options via HTTP headers instead of request body, matching the Go restheadspec handler.
 *
 * HTTP methods: GET=read, POST=create, PUT=update, DELETE=delete
 */
export class HeaderSpecClient {
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

    private baseHeaders(): Record<string, string> {
        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
        };
        if (this.config.token) {
            headers['Authorization'] = `Bearer ${this.config.token}`;
        }
        return headers;
    }

    private async fetchWithError<T>(url: string, init: RequestInit): Promise<APIResponse<T>> {
        const response = await fetch(url, init);
        const data = await response.json();

        if (!response.ok) {
            throw new Error(data.error?.message || 'An error occurred');
        }

        return data;
    }

    async read<T = any>(
        schema: string,
        entity: string,
        id?: string,
        options?: Options
    ): Promise<APIResponse<T>> {
        const url = this.buildUrl(schema, entity, id);
        const optHeaders = options ? buildHeaders(options) : {};
        return this.fetchWithError<T>(url, {
            method: 'GET',
            headers: { ...this.baseHeaders(), ...optHeaders },
        });
    }

    async create<T = any>(
        schema: string,
        entity: string,
        data: any,
        options?: Options
    ): Promise<APIResponse<T>> {
        const url = this.buildUrl(schema, entity);
        const optHeaders = options ? buildHeaders(options) : {};
        return this.fetchWithError<T>(url, {
            method: 'POST',
            headers: { ...this.baseHeaders(), ...optHeaders },
            body: JSON.stringify(data),
        });
    }

    async update<T = any>(
        schema: string,
        entity: string,
        id: string,
        data: any,
        options?: Options
    ): Promise<APIResponse<T>> {
        const url = this.buildUrl(schema, entity, id);
        const optHeaders = options ? buildHeaders(options) : {};
        return this.fetchWithError<T>(url, {
            method: 'PUT',
            headers: { ...this.baseHeaders(), ...optHeaders },
            body: JSON.stringify(data),
        });
    }

    async delete(
        schema: string,
        entity: string,
        id: string
    ): Promise<APIResponse<void>> {
        const url = this.buildUrl(schema, entity, id);
        return this.fetchWithError<void>(url, {
            method: 'DELETE',
            headers: this.baseHeaders(),
        });
    }
}
