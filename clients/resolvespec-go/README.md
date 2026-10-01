# resolvespec-go

Go client for ResolveSpec (JSON body) and FunctionSpec. Module: `git.warky.dev/wdevs/ResolveSpec/clients/resolvespec-go`. Stdlib only.

## Clients

| Type | Constructor | Methods |
|---|---|---|
| `Client` | `NewClient(baseURL, opts...)` | `GetMetadata` `Read` `Create` `Update` `Delete` |
| `FuncSpecClient` | `NewFuncSpecClient(baseURL, opts...)` | `Query` `QueryList` `Do` |

Client options: `WithToken`, `WithHeader`, `WithHTTPClient`. Precedence: Content-Type < custom headers < bearer token.

## ResolveSpec

- All methods take `ctx`; `Read`/`Update`/`Delete` take `RecordID` (`nil`, int/string → URL, `[]string` → body).
- `Options` fields use pointers for optional ints/bools (`Int(n)`, `Bool(b)`).
- Result: `*Response{Success, Data (raw JSON), Metadata}`; `resp.Decode(&v)`.

## FunctionSpec

- Routes are server-defined: pass the `path`.
- `Params` → query string (slice → repeated keys, bool → `true`/`false`).
- `FuncSpecOptions` → `X-*` headers: `Filters`, `SearchFilters`, `CustomSQLWhere`, `CustomSQLOr`, `Sort`, `Limit`, `Offset`, `Distinct`, `SkipCount`, `SkipCache`, `ResponseFormat`.
- `QueryList` fills `Metadata` from `Content-Range` (`items a-b/total`); 206 is success.

## Server quirks

- `Sort` is raw SQL in ORDER BY (client sends `col ASC|DESC`).
- One search operator per column.
- Values starting `ZIP_` / `__` are base64-decoded by the server.
- Non-ASCII, control chars and edge spaces are auto-encoded (`ZIP_`).

## Errors

`*Error{StatusCode, APIError{Code, Message, Detail, SQL}}`.

## Test

`go test ./...`
