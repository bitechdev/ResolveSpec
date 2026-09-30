# resolvespec (Python)

Python client for ResolveSpec REST, HeaderSpec (restheadspec), FunctionSpec and WebSocketSpec. Port of `resolvespec-js`.

- Python >= 3.11, `httpx` (REST, sync + async), `websockets` (WS, async)
- Options/filters/sorts are plain dicts using the wire key names (`TypedDict` hints in `resolvespec.types`)

```
pip install resolvespec
```

## Clients

| Protocol | Sync | Async | Transport |
|---|---|---|---|
| ResolveSpec | `ResolveSpecClient` | `AsyncResolveSpecClient` | POST + JSON body `{operation, id, data, options}` |
| HeaderSpec | `HeaderSpecClient` | `AsyncHeaderSpecClient` | GET/POST/PUT/DELETE, options as `X-*` headers |
| FunctionSpec | `FuncSpecClient` | `AsyncFuncSpecClient` | user-defined SQL endpoints; params via query string + `X-*` headers |
| WebSocketSpec | - | `WebSocketClient` | WebSocket JSON messages |

Constructor (REST): `Client(base_url, token=None, headers=None, timeout=30.0)`

- `token` -> `Authorization: Bearer`; wins over `headers`
- `headers`: custom headers, merged case-insensitively; snapshot at construction
- Sync: context manager / `close()`. Async: `async with` / `await aclose()`
- Cached sync factories: `get_resolvespec_client()`, `get_headerspec_client()` (same args -> same instance)

## ResolveSpec

URL: `{base}/{schema}/{entity}[/{id}]`

| Method | Signature |
|---|---|
| `get_metadata` | `(schema, entity)` (GET) |
| `read` | `(schema, entity, id=None, options=None)` |
| `create` | `(schema, entity, data, options=None)` |
| `update` | `(schema, entity, data, id=None, options=None)` |
| `delete` | `(schema, entity, id)` |

`id`: int/str -> URL path; `list[str]` -> body `id`.
Returns `{"success", "data", "metadata"?, "error"?}`.

## HeaderSpec

| Method | HTTP | Signature |
|---|---|---|
| `read` | GET | `(schema, entity, id=None, options=None)` |
| `create` | POST | `(schema, entity, data, options=None)` |
| `update` | PUT | `(schema, entity, id, data, options=None)` |
| `delete` | DELETE | `(schema, entity, id)` |

Response metadata derived from `Content-Range` (`offset-end/total`) and `X-Limit`.
`build_headers(options)`, `encode_header_value()` / `decode_header_value()` (`ZIP_` / `__` base64) are exported.

### Option -> header

| Option | Header |
|---|---|
| `columns` / `omit_columns` | `X-Select-Fields` / `X-Not-Select-Fields` |
| filter `eq` + AND | `X-FieldFilter-{col}` |
| filter AND / OR | `X-SearchOp-{op}-{col}` / `X-SearchOr-{op}-{col}` |
| spatial (`st_*`, `bbox`) / vector (`*_within`) filter | `X-SpatialFilter-{col}` / `X-VectorFilter-{col}` (JSON) |
| `sort` | `X-Sort` (`+col,-col`) |
| `limit` / `offset` | `X-Limit` / `X-Offset` |
| `cursor_forward` / `cursor_backward` | `X-Cursor-Forward` / `X-Cursor-Backward` |
| `preload` | `X-Preload` (`Rel:c1,c2\|Rel2`), `X-Preload-Where`, `X-Preload-{n}[-Where]` |
| `expand` | `X-Expand` |
| `custom_sql_joins` / `custom_sql_or` | `X-Custom-SQL-Join` / `X-Custom-SQL-Or` |
| `search_columns` | `X-SearchCols` |
| `advanced_sql` | `X-AdvSQL-{col}` |
| `computedColumns` | `X-CQL-SEL-{name}` |
| `customOperators` | `X-Custom-SQL-W` (AND-joined) |
| `vector_search` | `X-Vector-Search-{col}`, `-Vector`, `-As`, `-Dir` |
| `fetch_row_number` | `X-Fetch-RowNumber` |
| `clean_json`, `distinct`, `skip_count`, `skip_cache`, `atomic_transaction`, `single_record_as_object` | `X-Clean-JSON`, `X-Distinct`, `X-SkipCount`, `X-SkipCache`, `X-Transaction-Atomic`, `X-Single-Record-As-Object` |
| `pk_row` | `X-PKRow` |
| `response_format` (`simple`/`detail`/`syncfusion`) | `X-SimpleApi` / `X-DetailApi` / `X-Syncfusion` |
| `xfiles` | `X-Files` (`ZIP_` base64 JSON) |

Filter operator -> header op: `eq equals`, `neq notequals`, `gt greaterthan`, `gte greaterthanorequal`, `lt lessthan`, `lte lessthanorequal`, `like/ilike/contains contains`, `startswith beginswith`, `endswith`, `in`, `between`, `between_inclusive betweeninclusive`, `is_null empty`, `is_not_null notempty`.

## FunctionSpec

Routes are defined by the server app, so calls take a `path`. The server never reads a request body.

| Method | Server handler | Result |
|---|---|---|
| `query(path, params=None, options=None, *, method="GET")` | `SqlQuery` (single record) | `{success, data}` |
| `query_list(path, params=None, options=None, *, method="GET")` | `SqlQueryList` | `{success, data, metadata}` (from `Content-Range: items a-b/total`) |

- `params` -> query string. `bool` -> `true/false`, `None` skipped, `list` -> repeated key (server: `IN` filter). `p-` prefixed names are substituted into the SQL.
- `options` -> `X-*` headers. Query values override headers of the same name.
- 206 Partial Content (more rows than returned) is treated as success.

| Option | Header |
|---|---|
| `filters` (`eq`+AND) | `X-FieldFilter-{col}` |
| `filters` (other) | `X-SearchOp-{op}-{col}` / `X-SearchOr-{op}-{col}` |
| `search_filters` `{col: text}` | `X-SearchFilter-{col}` (ILIKE) |
| `custom_sql_where` / `custom_sql_or` | `X-Custom-SQL-W` / `X-Custom-SQL-Or` |
| `sort` | `X-Sort` as SQL terms: `col ASC,col DESC` |
| `limit` / `offset` | `X-Limit` / `X-Offset` |
| `distinct`, `skip_count`, `skip_cache` | `X-Distinct`, `X-SkipCount`, `X-SkipCache` |
| `response_format` | `X-SimpleApi` / `X-DetailApi` / `X-Syncfusion` (`data` shape changes: array / `{items,...}` / `{result,count}`) |

Server limits:
- `sort` goes verbatim into `ORDER BY`; `-col` (restheadspec style) does **not** mean DESC.
- `X-Select-Fields` / `X-Not-Select-Fields` are no-ops server-side, so not exposed.
- One search operator per column; same column twice keeps the last.
- Values starting with `ZIP_` / `__` are base64-decoded by the server; such plaintext cannot be sent.
- Non-ASCII / control-char values are sent `ZIP_`-encoded automatically.

## WebSocketSpec

`WebSocketClient(url, *, reconnect=True, reconnect_interval=3.0, max_reconnect_attempts=10, heartbeat_interval=30.0, request_timeout=30.0, subscribe_timeout=10.0, headers=None)`

| Method | Notes |
|---|---|
| `connect()` / `close()` | also `async with` |
| `request(operation, entity, *, schema, record_id, data, options)` | returns response `data` |
| `read(entity, *, schema, record_id, filters, columns, sort, preload, limit, offset)` | |
| `create(entity, data, *, schema)` | |
| `update(entity, id, data, *, schema)` | |
| `delete(entity, id, *, schema)` | |
| `meta(entity, *, schema)` | |
| `subscribe(entity, callback, *, schema, filters)` | returns subscription id; callback gets notification dict (sync or async) |
| `unsubscribe(subscription_id)` | |
| `on(event, cb)` / `off(event)` | events: `connect`, `disconnect`, `error`, `message`, `state_change` |
| `state`, `is_connected()`, `get_subscriptions()` | |

Auto-reconnect does not restore subscriptions; re-subscribe on `connect`.

## Errors

`ResolveSpecError(message, status_code, code, details)` on non-2xx (REST) or failed response / timeout / not connected (WS).

## Dev

```
pip install -e '.[dev]'
pytest
```
