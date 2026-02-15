import { describe, it, expect } from 'vitest';
import type {
    Options,
    FilterOption,
    SortOption,
    PreloadOption,
    RequestBody,
    APIResponse,
    Metadata,
    APIError,
    Parameter,
    ComputedColumn,
    CustomOperator,
} from '../common/types';

describe('Common Types', () => {
    it('should construct a valid FilterOption with logic_operator', () => {
        const filter: FilterOption = {
            column: 'name',
            operator: 'eq',
            value: 'test',
            logic_operator: 'OR',
        };
        expect(filter.logic_operator).toBe('OR');
        expect(filter.operator).toBe('eq');
    });

    it('should construct Options with all new fields', () => {
        const opts: Options = {
            columns: ['id', 'name'],
            omit_columns: ['secret'],
            filters: [{ column: 'age', operator: 'gte', value: 18 }],
            sort: [{ column: 'name', direction: 'asc' }],
            limit: 10,
            offset: 0,
            cursor_forward: 'abc123',
            cursor_backward: 'xyz789',
            fetch_row_number: '42',
            parameters: [{ name: 'param1', value: 'val1', sequence: 1 }],
            computedColumns: [{ name: 'full_name', expression: "first || ' ' || last" }],
            customOperators: [{ name: 'custom', sql: "status = 'active'" }],
            preload: [{
                relation: 'Items',
                columns: ['id', 'title'],
                omit_columns: ['internal'],
                sort: [{ column: 'id', direction: 'ASC' }],
                recursive: true,
                primary_key: 'id',
                related_key: 'parent_id',
                sql_joins: ['LEFT JOIN other ON other.id = items.other_id'],
                join_aliases: ['other'],
            }],
        };
        expect(opts.omit_columns).toEqual(['secret']);
        expect(opts.cursor_forward).toBe('abc123');
        expect(opts.fetch_row_number).toBe('42');
        expect(opts.parameters![0].sequence).toBe(1);
        expect(opts.preload![0].recursive).toBe(true);
    });

    it('should construct a RequestBody with numeric id', () => {
        const body: RequestBody = {
            operation: 'read',
            id: 42,
            options: { limit: 10 },
        };
        expect(body.id).toBe(42);
    });

    it('should construct a RequestBody with string array id', () => {
        const body: RequestBody = {
            operation: 'delete',
            id: ['1', '2', '3'],
        };
        expect(Array.isArray(body.id)).toBe(true);
    });

    it('should construct Metadata with count and row_number', () => {
        const meta: Metadata = {
            total: 100,
            count: 10,
            filtered: 50,
            limit: 10,
            offset: 0,
            row_number: 5,
        };
        expect(meta.count).toBe(10);
        expect(meta.row_number).toBe(5);
    });

    it('should construct APIError with detail field', () => {
        const err: APIError = {
            code: 'not_found',
            message: 'Record not found',
            detail: 'The record with id 42 does not exist',
        };
        expect(err.detail).toBeDefined();
    });

    it('should construct APIResponse with metadata', () => {
        const resp: APIResponse<string[]> = {
            success: true,
            data: ['a', 'b'],
            metadata: { total: 2, count: 2, filtered: 2, limit: 10, offset: 0 },
        };
        expect(resp.metadata?.count).toBe(2);
    });

    it('should support all operator types', () => {
        const operators: FilterOption['operator'][] = [
            'eq', 'neq', 'gt', 'gte', 'lt', 'lte',
            'like', 'ilike', 'in',
            'contains', 'startswith', 'endswith',
            'between', 'between_inclusive',
            'is_null', 'is_not_null',
        ];
        for (const op of operators) {
            const f: FilterOption = { column: 'x', operator: op, value: 'v' };
            expect(f.operator).toBe(op);
        }
    });

    it('should support PreloadOption with computed_ql and where', () => {
        const preload: PreloadOption = {
            relation: 'Details',
            where: "status = 'active'",
            computed_ql: { cql1: 'SUM(amount)' },
            table_name: 'detail_table',
            updatable: true,
            foreign_key: 'detail_id',
            recursive_child_key: 'parent_detail_id',
        };
        expect(preload.computed_ql?.cql1).toBe('SUM(amount)');
        expect(preload.updatable).toBe(true);
    });

    it('should support Parameter interface', () => {
        const p: Parameter = { name: 'key', value: 'val' };
        expect(p.name).toBe('key');
        const p2: Parameter = { name: 'key2', value: 'val2', sequence: 5 };
        expect(p2.sequence).toBe(5);
    });
});
