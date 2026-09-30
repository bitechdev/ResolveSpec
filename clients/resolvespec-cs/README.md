# ResolveSpec.Client (C#)

.NET 8 client for ResolveSpec (JSON body) and FunctionSpec. `System.Text.Json`, no other dependencies.

> Not compiled or tested yet (no .NET SDK was available). Run `dotnet test tests/` first.

## Clients

| Type | Constructor | Methods |
|---|---|---|
| `ResolveSpecClient` | `(baseUrl, ClientOptions?)` | `GetMetadataAsync` `ReadAsync` `CreateAsync` `UpdateAsync` `DeleteAsync` |
| `FuncSpecClient` | `(baseUrl, ClientOptions?)` | `QueryAsync` `QueryListAsync` |

`ClientOptions`: `Token`, `Headers`, `Timeout`, `HttpClient`. Precedence: Content-Type < custom headers < bearer token.

## ResolveSpec

- `id`: int/long/string → URL, `IEnumerable<string>` → body.
- `Options` with nullable properties; wire names via `JsonPropertyName`.
- Result: `Response{Success, Data (JsonElement), Metadata}`; `resp.Decode<T>()`.

## FunctionSpec

- Routes are server-defined: pass the `path`.
- Params (`IDictionary<string, object?>`) → query string (enumerable → repeated keys, bool → `true`/`false`, null skipped).
- `FuncSpecOptions` → `X-*` headers: `Filters`, `SearchFilters`, `CustomSqlWhere`, `CustomSqlOr`, `Sort`, `Limit`, `Offset`, `Distinct`, `SkipCount`, `SkipCache`, `ResponseFormat`.
- `QueryListAsync` fills `Metadata` from `Content-Range`; 206 is success.
- Static helpers: `BuildHeaders`, `BuildQuery`, `EncodeHeaderValue`, `DecodeHeaderValue`.

## Server quirks

- `Sort` is raw SQL in ORDER BY (client sends `col ASC|DESC`).
- One search operator per column.
- Values starting `ZIP_` / `__` are base64-decoded by the server.
- Non-ASCII, control chars and edge spaces are auto-encoded (`ZIP_`).

## Errors

`ResolveSpecException{StatusCode, Message, Error{Code, Detail, Sql}}`.

## Test

`dotnet test tests/`
