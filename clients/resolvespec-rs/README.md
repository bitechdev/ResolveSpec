# resolvespec (Rust)

Rust client for ResolveSpec (JSON body) and FunctionSpec. Async (`reqwest` + `tokio`). MSRV 1.80.

## Clients

| Type | Constructor | Methods |
|---|---|---|
| `ResolveSpecClient` | `new(base_url)` / `from_builder(ClientBuilder)` | `get_metadata` `read` `create` `update` `delete` |
| `FuncSpecClient` | `new(base_url)` / `from_builder(ClientBuilder)` | `query` `query_list` `request` |

`ClientBuilder::new(url).token().header().timeout().http_client()`. Precedence: Content-Type < custom headers < bearer token.

## ResolveSpec

- `RecordId`: `Int`/`Str` → URL, `Many(Vec<String>)` → body (`From` impls provided).
- `Options` (`Default` + struct update), optional fields are `Option`/empty `Vec`.
- Result: `Response{success, data: serde_json::Value, metadata}`; `resp.decode::<T>()`.

## FunctionSpec

- Routes are server-defined: pass the `path`.
- `Params = BTreeMap<String, Param>` → query string (`Param::List` → repeated keys).
- `FuncSpecOptions` → `X-*` headers: `filters`, `search_filters`, `custom_sql_where`, `custom_sql_or`, `sort`, `limit`, `offset`, `distinct`, `skip_count`, `skip_cache`, `response_format`.
- `query_list` fills `metadata` from `Content-Range`; 206 is success.

## Server quirks

- `sort` is raw SQL in ORDER BY (client sends `col ASC|DESC`).
- One search operator per column.
- Values starting `ZIP_` / `__` are base64-decoded by the server.
- Non-ASCII, control chars and edge spaces are auto-encoded (`ZIP_`).

## Errors

`Error::Api { status, message, error: ApiError{code, message, detail, sql} }`, `Error::Http`, `Error::Json`.

## Test

`cargo test`
