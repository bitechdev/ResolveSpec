# resolvespec (Dart)

Dart / Flutter client for ResolveSpec (JSON body) and FunctionSpec. Depends on `package:http`. Dart >= 3.3.

## Clients

| Type | Constructor | Methods |
|---|---|---|
| `ResolveSpecClient` | `(baseUrl, [ClientOptions])` | `getMetadata` `read` `create` `update` `delete` `close` |
| `FuncSpecClient` | `(baseUrl, [ClientOptions])` | `query` `queryList` `close` |

`ClientOptions(token:, headers:, timeout:, httpClient:)`. Precedence: Content-Type < custom headers < bearer token.

## ResolveSpec

- `id`: `int`/`String` → URL, `List<String>` → body. Named args: `id:`, `options:`.
- `Options`, `FilterOption(column, operator, [value, logic])`, `SortOption(column, [direction])`.
- Result: `Response{success, data (decoded JSON), metadata}`.

## FunctionSpec

- Routes are server-defined: pass the `path`.
- `params:` map → query string (list → repeated keys, null skipped).
- `FuncSpecOptions` → `X-*` headers: `filters`, `searchFilters`, `customSqlWhere`, `customSqlOr`, `sort`, `limit`, `offset`, `distinct`, `skipCount`, `skipCache`, `responseFormat`.
- `queryList` fills `metadata` from `Content-Range`; 206 is success.
- Helpers: `buildHeaders`, `buildQuery`, `encodeHeaderValue`, `decodeHeaderValue`.

## Server quirks

- `sort` is raw SQL in ORDER BY (client sends `col ASC|DESC`).
- One search operator per column.
- Values starting `ZIP_` / `__` are base64-decoded by the server.
- Non-ASCII, control chars and edge spaces are auto-encoded (`ZIP_`).

## Errors

`ResolveSpecException{statusCode, message, error: ApiError{code, detail, sql}}`.

## Test

`dart test`
