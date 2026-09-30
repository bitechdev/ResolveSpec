import { clientCacheKey, clientHeaders, mergeHeaders } from '../common/http';
import { b64DecodeUnicode, b64EncodeUnicode } from '@warkypublic/artemis-kit/base64';
import type {
  APIResponse,
  ClientConfig,
  CustomOperator,
  FilterOption,
  HeaderSpecOptions,
  PreloadOption,
  SortOption,
} from "../common/types";

/**
 * Encode a value with base64 and ZIP_ prefix for complex header values.
 */
export function encodeHeaderValue(value: string): string {
  return "ZIP_" + b64EncodeUnicode(value);
}

/**
 * Decode a header value that may be base64 encoded with ZIP_ or __ prefix.
 */
export function decodeHeaderValue(value: string): string {
  let code = value;

  if (code.startsWith("ZIP_")) {
    code = code.slice(4).replace(/[\n\r ]/g, "");
    code = decodeBase64(code);
  } else if (code.startsWith("__")) {
    code = code.slice(2).replace(/[\n\r ]/g, "");
    code = decodeBase64(code);
  }

  // Handle nested encoding
  if (code.startsWith("ZIP_") || code.startsWith("__")) {
    code = decodeHeaderValue(code);
  }

  return code;
}

function decodeBase64(str: string): string {
  return b64DecodeUnicode(str);
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
 *  - X-Preload-Where: where for X-Preload (extra where groups use X-Preload-{n}[-Where])
 *  - X-SpatialFilter-{col} / X-VectorFilter-{col}: JSON {op,value,logic}
 *  - X-Vector-Search-{col|vector|as|dir}: pgvector KNN
 *  - X-Expand, X-Custom-SQL-Join, X-Custom-SQL-Or, X-SearchCols, X-AdvSQL-{col}
 *  - X-Clean-JSON, X-Distinct, X-SkipCount, X-SkipCache, X-PKRow
 *  - X-SimpleApi / X-DetailApi / X-Syncfusion, X-Single-Record-As-Object
 *  - X-Transaction-Atomic, X-Files
 */
export function buildHeaders(options: HeaderSpecOptions): Record<string, string> {
  const headers: Record<string, string> = {};

  // Column selection
  if (options.columns?.length) {
    headers["X-Select-Fields"] = options.columns.join(",");
  }

  if (options.omit_columns?.length) {
    headers["X-Not-Select-Fields"] = options.omit_columns.join(",");
  }

  // Filters
  if (options.filters?.length) {
    for (const filter of options.filters) {
      const logicOp = filter.logic_operator ?? "AND";
      const op = mapOperatorToHeaderOp(filter.operator);
      const valueStr = formatFilterValue(filter);

      const geoPrefix = geoFilterHeader(filter.operator);
      if (geoPrefix) {
        const payload: Record<string, unknown> = {
          op: filter.operator,
          value: filter.value,
        };
        if (logicOp === "OR") payload.logic = "or";
        headers[`${geoPrefix}${filter.column}`] = JSON.stringify(payload);
        continue;
      }

      if (filter.operator === "eq" && logicOp === "AND") {
        // Simple field filter shorthand
        headers[`X-FieldFilter-${filter.column}`] = valueStr;
      } else if (logicOp === "OR") {
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
      return dir === "DESC" ? `-${s.column}` : `+${s.column}`;
    });
    headers["X-Sort"] = sortParts.join(",");
  }

  // Pagination
  if (options.limit !== undefined) {
    headers["X-Limit"] = String(options.limit);
  }
  if (options.offset !== undefined) {
    headers["X-Offset"] = String(options.offset);
  }

  // Cursor pagination
  if (options.cursor_forward) {
    headers["X-Cursor-Forward"] = options.cursor_forward;
  }
  if (options.cursor_backward) {
    headers["X-Cursor-Backward"] = options.cursor_backward;
  }

  // Preload
  if (options.preload?.length) {
    // Go applies X-Preload-Where to every preload in the matching X-Preload header,
    // so preloads are grouped by where clause.
    const groups = new Map<string, string[]>();
    for (const p of options.preload) {
      const spec = p.columns?.length
        ? `${p.relation}:${p.columns.join(",")}`
        : p.relation;
      const where = p.where ?? "";
      groups.set(where, [...(groups.get(where) ?? []), spec]);
    }
    let n = 0;
    for (const [where, specs] of groups) {
      if (!where) {
        headers["X-Preload"] = specs.join("|");
      } else if (!groups.has("") && n === 0) {
        // X-Preload-Where would also apply to a where-less X-Preload, so only use it alone
        headers["X-Preload"] = specs.join("|");
        headers["X-Preload-Where"] = where;
        n++;
      } else {
        n++;
        headers[`X-Preload-${n}`] = specs.join("|");
        headers[`X-Preload-${n}-Where`] = where;
      }
    }
  }

  // Expand (LEFT JOIN)
  if (options.expand?.length) {
    headers["X-Expand"] = options.expand
      .map((e) =>
        e.columns?.length ? `${e.relation}:${e.columns.join(",")}` : e.relation,
      )
      .join("|");
  }

  if (options.custom_sql_joins?.length) {
    headers["X-Custom-SQL-Join"] = options.custom_sql_joins.join("|");
  }
  if (options.custom_sql_or?.length) {
    headers["X-Custom-SQL-Or"] = options.custom_sql_or.join(" OR ");
  }
  if (options.search_columns?.length) {
    headers["X-SearchCols"] = options.search_columns.join(",");
  }
  if (options.advanced_sql) {
    for (const [col, sql] of Object.entries(options.advanced_sql)) {
      headers[`X-AdvSQL-${col}`] = sql;
    }
  }

  // pgvector KNN search
  if (options.vector_search) {
    const vs = options.vector_search;
    headers[`X-Vector-Search-${vs.column}`] = vs.metric ?? "l2";
    headers["X-Vector-Search-Vector"] = JSON.stringify(vs.vector);
    if (vs.as) headers["X-Vector-Search-As"] = vs.as;
    if (vs.direction) headers["X-Vector-Search-Dir"] = vs.direction;
  }

  // Flags
  const flags: [string, boolean | undefined][] = [
    ["X-Clean-JSON", options.clean_json],
    ["X-Distinct", options.distinct],
    ["X-SkipCount", options.skip_count],
    ["X-SkipCache", options.skip_cache],
    ["X-Transaction-Atomic", options.atomic_transaction],
    ["X-Single-Record-As-Object", options.single_record_as_object],
  ];
  for (const [name, val] of flags) {
    if (val !== undefined) headers[name] = String(val);
  }

  if (options.pk_row) {
    headers["X-PKRow"] = options.pk_row;
  }

  if (options.response_format) {
    const formatHeaders = {
      simple: "X-SimpleApi",
      detail: "X-DetailApi",
      syncfusion: "X-Syncfusion",
    } as const;
    headers[formatHeaders[options.response_format]] = "true";
  }

  if (options.xfiles) {
    headers["X-Files"] = encodeHeaderValue(JSON.stringify(options.xfiles));
  }

  // Fetch row number
  if (options.fetch_row_number) {
    headers["X-Fetch-RowNumber"] = options.fetch_row_number;
  }

  // Computed columns
  if (options.computedColumns?.length) {
    for (const cc of options.computedColumns) {
      headers[`X-CQL-SEL-${cc.name}`] = cc.expression;
    }
  }

  // Custom operators -> X-Custom-SQL-W
  if (options.customOperators?.length) {
    const sqlParts = options.customOperators.map(
      (co: CustomOperator) => co.sql,
    );
    headers["X-Custom-SQL-W"] = sqlParts.join(" AND ");
  }

  return headers;
}

const VECTOR_OPS = new Set(["l2_within", "cosine_within", "ip_within"]);

function geoFilterHeader(operator: string): string | null {
  const op = operator.toLowerCase();
  if (VECTOR_OPS.has(op) || op.endsWith("_within")) return "X-VectorFilter-";
  if (op.startsWith("st_") || op === "bbox" || op === "&&") {
    return "X-SpatialFilter-";
  }
  return null;
}

function mapOperatorToHeaderOp(operator: string): string {
  switch (operator) {
    case "eq":
      return "equals";
    case "neq":
      return "notequals";
    case "gt":
      return "greaterthan";
    case "gte":
      return "greaterthanorequal";
    case "lt":
      return "lessthan";
    case "lte":
      return "lessthanorequal";
    case "like":
    case "ilike":
    case "contains":
      return "contains";
    case "startswith":
      return "beginswith";
    case "endswith":
      return "endswith";
    case "in":
      return "in";
    case "between":
      return "between";
    case "between_inclusive":
      return "betweeninclusive";
    case "is_null":
      return "empty";
    case "is_not_null":
      return "notempty";
    default:
      return operator;
  }
}

function formatFilterValue(filter: FilterOption): string {
  if (filter.value === null || filter.value === undefined) {
    return "";
  }
  if (Array.isArray(filter.value)) {
    return filter.value.join(",");
  }
  return String(filter.value);
}

const instances = new Map<string, HeaderSpecClient>();

export function getHeaderSpecClient(config: ClientConfig): HeaderSpecClient {
  const key = clientCacheKey(config);
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
    this.config = { ...config, headers: { ...config.headers } };
  }

  private buildUrl(schema: string, entity: string, id?: string): string {
    let url = `${this.config.baseUrl}/${schema}/${entity}`;
    if (id) {
      url += `/${id}`;
    }
    return url;
  }

  private baseHeaders(): Record<string, string> {
    return clientHeaders(this.config);
  }

  private async fetchWithError<T>(
    url: string,
    init: RequestInit,
  ): Promise<APIResponse<T>> {
    const response = await fetch(url, init);
    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error?.message ||
          `${response.statusText} ` + `(${response.status})`,
      );
    }

    return {
      data: data,
      success: true,
      error: data.error ? data.error : undefined,
      metadata: {
        count: response.headers.get("content-range")
          ? Number(response.headers.get("content-range")?.split("/")[1])
          : 0,
        total: response.headers.get("content-range")
          ? Number(response.headers.get("content-range")?.split("/")[1])
          : 0,
        filtered: response.headers.get("content-range")
          ? Number(response.headers.get("content-range")?.split("/")[1])
          : 0,
        offset: response.headers.get("content-range")
          ? Number(
              response.headers
                .get("content-range")
                ?.split("/")[0]
                .split("-")[0],
            )
          : 0,
        limit: response.headers.get("x-limit")
          ? Number(response.headers.get("x-limit"))
          : 0,
      },
    };
  }

  async read<T = any>(
    schema: string,
    entity: string,
    id?: string,
    options?: HeaderSpecOptions,
  ): Promise<APIResponse<T>> {
    const url = this.buildUrl(schema, entity, id);
    const optHeaders = options ? buildHeaders(options) : {};
    return this.fetchWithError<T>(url, {
      method: "GET",
      headers: mergeHeaders(this.baseHeaders(), optHeaders),
    });
  }

  async create<T = any>(
    schema: string,
    entity: string,
    data: any,
    options?: HeaderSpecOptions,
  ): Promise<APIResponse<T>> {
    const url = this.buildUrl(schema, entity);
    const optHeaders = options ? buildHeaders(options) : {};
    return this.fetchWithError<T>(url, {
      method: "POST",
      headers: mergeHeaders(this.baseHeaders(), optHeaders),
      body: JSON.stringify(data),
    });
  }

  async update<T = any>(
    schema: string,
    entity: string,
    id: string,
    data: any,
    options?: HeaderSpecOptions,
  ): Promise<APIResponse<T>> {
    const url = this.buildUrl(schema, entity, id);
    const optHeaders = options ? buildHeaders(options) : {};
    return this.fetchWithError<T>(url, {
      method: "PUT",
      headers: mergeHeaders(this.baseHeaders(), optHeaders),
      body: JSON.stringify(data),
    });
  }

  async delete(
    schema: string,
    entity: string,
    id: string,
  ): Promise<APIResponse<void>> {
    const url = this.buildUrl(schema, entity, id);
    return this.fetchWithError<void>(url, {
      method: "DELETE",
      headers: this.baseHeaders(),
    });
  }
}
