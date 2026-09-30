// Types aligned with Go pkg/common/types.go

export type Operator =
    | 'eq' | 'neq' | 'gt' | 'gte' | 'lt' | 'lte'
    | 'like' | 'ilike' | 'in'
    | 'contains' | 'startswith' | 'endswith'
    | 'between' | 'between_inclusive'
    | 'is_null' | 'is_not_null'
    // PostGIS spatial (sent via X-SpatialFilter-{col})
    | 'st_dwithin' | 'bbox'
    // pgvector similarity (sent via X-VectorFilter-{col})
    | 'l2_within' | 'cosine_within' | 'ip_within';

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

export type VectorMetric = 'l2' | 'cosine' | 'ip';
export type ResponseFormat = 'simple' | 'detail' | 'syncfusion';

/** pgvector KNN search: order by distance between `column` and `vector`. */
export interface VectorSearchOption {
    column: string;
    vector: number[];
    metric?: VectorMetric;
    /** Distance column alias. Default `_distance` */
    as?: string;
    direction?: 'asc' | 'desc';
}

/** LEFT JOIN expansion of a relation (X-Expand). */
export interface ExpandOption {
    relation: string;
    columns?: string[];
}

/** X-Files configuration (Go restheadspec XFiles). Sent as a single JSON header. */
export interface XFiles {
    tablename?: string;
    schema?: string;
    primarykey?: string;
    foreignkey?: string;
    relatedkey?: string;
    sort?: string[];
    prefix?: string;
    editable?: boolean;
    recursive?: boolean;
    expand?: boolean;
    rownumber?: boolean;
    skipcount?: boolean;
    offset?: number;
    limit?: number;
    columns?: string[];
    omit_columns?: string[];
    cql_columns?: string[];
    sql_joins?: string[];
    sql_or?: string[];
    sql_and?: string[];
    parenttables?: XFiles[];
    childtables?: XFiles[];
    filter_fields?: { field: string; value: string; operator: string }[];
    cursor_forward?: string;
    cursor_backward?: string;
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
    vector_search?: VectorSearchOption;
}

/** Options only available to the header-based (restheadspec) protocol. */
export interface HeaderSpecOptions extends Options {
    /** X-Expand: LEFT JOIN relations */
    expand?: ExpandOption[];
    /** X-Custom-SQL-Join: raw JOIN clauses */
    custom_sql_joins?: string[];
    /** X-Custom-SQL-Or: raw SQL, OR-combined */
    custom_sql_or?: string[];
    /** X-SearchCols: columns for multi-column search */
    search_columns?: string[];
    /** X-AdvSQL-{col}: column -> SQL expression */
    advanced_sql?: Record<string, string>;
    /** X-Clean-JSON */
    clean_json?: boolean;
    /** X-Distinct */
    distinct?: boolean;
    /** X-SkipCount: skip total count query */
    skip_count?: boolean;
    /** X-SkipCache */
    skip_cache?: boolean;
    /** X-PKRow: primary key value of a row to fetch */
    pk_row?: string;
    /** X-SimpleApi / X-DetailApi / X-Syncfusion */
    response_format?: ResponseFormat;
    /** X-Single-Record-As-Object (server default true) */
    single_record_as_object?: boolean;
    /** X-Transaction-Atomic */
    atomic_transaction?: boolean;
    /** X-Files: single JSON configuration */
    xfiles?: XFiles;
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
    /** Custom HTTP headers. Token and HeaderSpec query options take precedence. */
    headers?: Record<string, string>;
}
