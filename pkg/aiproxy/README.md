# aiproxy

Authenticated reverse proxy for OpenAI-compatible APIs and MCP servers (streamable HTTP).
Upstream URLs and keys stay server-side; clients only use the proxy with normal ResolveSpec auth.

## Setup
```go
p := aiproxy.New(aiproxy.Config{Prefix: "/ai"})
p.RegisterOpenAI(aiproxy.OpenAIUpstream{Upstream: aiproxy.Upstream{Name: "gpt", BaseURL: "https://api.openai.com/v1", APIKey: key}})
p.RegisterMCP(aiproxy.MCPUpstream{Upstream: aiproxy.Upstream{Name: "tools", BaseURL: "http://localhost:3000/mcp", APIKey: key}})
mux.Handle("/ai/", p.Handler(security.NewAuthMiddleware(securityList)))
```

## Routes
| Kind | Client URL | Upstream URL |
|---|---|---|
| OpenAI | `{prefix}/{name}/v1/*` | `BaseURL` + path after `/v1` (BaseURL includes the version) |
| MCP | `{prefix}/{name}` | `BaseURL` as is |

Client SDK base URL: `https://host/ai/gpt/v1` (OpenAI), `https://host/ai/tools` (MCP).
Query string, `Mcp-Session-Id`, SSE streaming all pass through.

## Upstream fields
| Field | Notes |
|---|---|
| `Name` | route segment, `[A-Za-z0-9_.-]`, unique across kinds |
| `BaseURL` | http/https |
| `APIKey` | optional; never logged/serialized (redacted in `String`/JSON) |
| `AuthHeader` | default `Authorization` |
| `AuthFormat` | one `%s`; default `Bearer %s` for `Authorization`, else raw key |
| `Headers` | static extra upstream headers |
| `AllowedRoles` | any-of; empty = any authenticated user |
| `RateLimit` | `{PerSecond, Burst}` per user per upstream; 429 + `Retry-After` |
| `Timeout` | response-header timeout, default 120s (streams not cut) |
| `AllowedModels` (OpenAI) | JSON body `model` must match; non-JSON writes rejected |
| `AllowedTools` (MCP) | `tools/call` name must match (batches checked); `tools/list` not filtered |

## Security behavior
- Auth required; guests rejected (401). Roles checked (403).
- Client `Authorization`, `Cookie`, `Proxy-Authorization`, `X-Api-Key` (+ `Config.StripHeaders`) removed; key injected.
- Response `Set-Cookie`, `Www-Authenticate`, redirect `Location` removed.
- Upstream 401 becomes 502 (proxy credentials problem, not the client's); upstream errors hide URL.
- JSON bodies inspected up to `Config.MaxBodyBytes` (10 MB), else 413.
- Fails closed if `Handler(nil)`.

## Hooks (`p.Hooks()`)
| Hook | When | Context |
|---|---|---|
| `BeforeProxy` | after auth/limit/inspection | `Request` (headers editable), `UserContext`, `Upstream`, `Kind`, `Model`, `Tools`; abort via `Abort`/`AbortMessage`/`AbortCode` (default 403) |
| `AfterProxy` | response finished/failed | `StatusCode`, `Duration`, `Usage`, `Error` |

`Usage` (OpenAI): from `usage` / `response.usage`; streams need `stream_options.include_usage`.

## Audit
`Config.Audit` (`AuditSink`) gets one `AuditRecord` per handled request: user, upstream, method, client path (no query),
model/tools, status, outcome (`ok|upstream_error|denied|rate_limited`), reason, duration, usage. No bodies, no keys.
Default `LogAuditSink` (pkg/logger); `NopAuditSink{}` disables. Sinks run on the request path: keep them fast.

## Metrics
Used automatically when the global `metrics.Provider` implements `metrics.AIProxyRecorder` (`PrometheusProvider` does):
`aiproxy_requests_total{upstream,kind,model,status,outcome}`, `aiproxy_request_duration_seconds{upstream,kind}`,
`aiproxy_tokens_total{upstream,model,type}`. `model` is client-supplied: bounded to 400 distinct pairs, then `other`.

## Dynamic upstreams
| Call | Effect |
|---|---|
| `Register(Definition)` / `Unregister(name)` | runtime add/remove |
| `Reload(ctx, store)` | sync store-managed upstreams: add, replace changed, drop missing; unchanged keep rate-limit state |
| `AutoReload(ctx, store, interval)` | `Reload` now, then periodically |
| `NewProcStore(*sql.DB, proc)` / `NewProcStoreFromDatabase(common.Database, proc)` | calls stored procedure (PostgreSQL), default `resolvespec_ai_proxies` |

- Code-registered upstreams are never replaced/removed by `Reload`; a same-name stored row is skipped and reported.
- Invalid rows are skipped (valid ones applied; last good definition kept); store failure changes nothing.
- Procedure contract (like the security procedures): `SELECT p_success, p_error, p_data FROM resolvespec_ai_proxies()`;
  `p_data` = JSON array of `{name, kind, base_url, api_key, auth_header, auth_format, headers, allowed_roles, allowed, rate_per_second, rate_burst, timeout_seconds, enabled}`.
  Reference table + function: `resolvespec_ai_proxies.sql` (replace the body to source from anywhere).
- Entries with `enabled:false` or undecodable JSON are skipped.
- API keys come back in plaintext from the procedure: restrict execute rights on it.

## Notes
- Legacy MCP SSE transport is not supported.
- `GET /v1/models` is not filtered by `AllowedModels`.
