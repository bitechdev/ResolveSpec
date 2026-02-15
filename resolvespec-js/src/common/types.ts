// Types aligned with Go pkg/common/types.go

export type Operator =
    | 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte'
    | 'like' | 'ilike' | 'in'
    | 'contains' | 'startswith' | 'endswith'
    | 'between' | 'between_inclusive'
    | 'is_null' | 'is_not_null';

export type Operation = 'read' | 'create' | 'update' | 'delete';
export type SortDirection = 'asc' | 'desc' | 'ASC' | 'DESC';

export interface Parameter {
    name: string;
    value: string;
    sequence?: number;
}

export interface PreloadOption {
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
    // Relationship keys
    primary_key?: string;
    related_key?: string;
    foreign_key?: string;
    recursive_child_key?: string;
    // Custom SQL JOINs
    sql_joins?: string[];
    join_aliases?: string[];
}

export interface FilterOption {
    column: string;
    operator: Operator | string;
    value: any;
    logic_operator?: 'AND' | 'OR';
}

export interface SortOption {
    column: string;
    direction: SortDirection;
}

export interface CustomOperator {
    name: string;
    sql: string;
}

export interface ComputedColumn {
    name: string;
    expression: string;
}

export interface Options {
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

export interface RequestBody {
    operation: Operation;
    id?: number | string | string[];
    data?: any | any[];
    options?: Options;
}

export interface Metadata {
    total: number;
    count: number;
    filtered: number;
    limit: number;
    offset: number;
    row_number?: number;
}

export interface APIError {
    code: string;
    message: string;
    details?: any;
    detail?: string;
}

export interface APIResponse<T = any> {
    success: boolean;
    data: T;
    metadata?: Metadata;
    error?: APIError;
}

export interface Column {
    name: string;
    type: string;
    is_nullable: boolean;
    is_primary: boolean;
    is_unique: boolean;
    has_index: boolean;
}

export interface TableMetadata {
    schema: string;
    table: string;
    columns: Column[];
    relations: string[];
}

export interface ClientConfig {
    baseUrl: string;
    token?: string;
}
