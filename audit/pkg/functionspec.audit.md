# Audit: `pkg/funcspec`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/funcspec` |
| **Files** | `function_api.go` (1251), `parameters.go` (411), `hooks.go` (179), `hooks_example.go`, `security_adapter.go` (117) |
| **Tests** | `function_api_test.go` (1278), `hooks_test.go` (589), `parameters_test.go` (549) — 2 416 lines; `go test` and `go test -race` pass, 76.1 % statement coverage |
| **Audit date** | 2026-09-30 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; query string, headers and body are attacker-controlled |
| **Depth** | targeted (server-side request path; verified against source) |

## Summary

`funcspec` exposes app-defined SQL templates as endpoints. The template is
trusted; everything the client adds to it is not. The package builds SQL by
string manipulation and has two kinds of client-controlled SQL fragments
(`X-Custom-SQL-W`, `X-Custom-SQL-Or`, `sort`) that are guarded only by a keyword
denylist (`ValidSQL(..., "select")`, `function_api.go:951-980`). That is not an
injection boundary: the fragment lands inside a query that may already carry
tenant or auth predicates, and the OR path produces wrong precedence that
widens results (findings 1-3).

The auth integration is weaker than it looks. `RegisterSecurityHooks` is opt-in,
the anonymous default is `UserID 0`, and the auth hooks return an error *and*
set `Abort`, so `Execute` returns the error first and the handler answers
**400 `hook_error`**, not 401 (finding 6).

Error handling leaks: `sendError` returns the DB error text and the full SQL to
the client, and the panic recovery writes the panic value into the 500 body
(finding 5).

Resource limits are absent: default limit 100 000, no cap on `X-Limit`, no
LIMIT at all when counting is skipped, unbounded `[post_body]` read, a 15-minute
timeout, and a `COUNT(1)` over the full query on every list request (finding 8).

Positives: there are no data races under the existing tests; the `[variable]`
substitution is quote-context aware; `Content-Type` and transaction handling are
consistent; hooks run inside the transaction.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **High** | security | `X-Custom-SQL-W`, `X-Custom-SQL-Or` and `sort` are appended as raw SQL, protected only by a keyword denylist (`ValidSQL "select"`) |
| 2 | **High** | security | `sqlQryWhereOr` emits `a AND b OR (c)`; OR conditions escape the AND-ed predicates (auth/tenant filters) |
| 3 | **Medium** | security | `sqlQryWhere`/`sqlQryWhereOr` locate WHERE/GROUP BY/ORDER BY/LIMIT by substring on the lower-cased query, including inside literals, subqueries and CTEs |
| 4 | **Medium** | security | Unquoted string `X-FieldFilter` value in `ApplyFilters`; `SearchOps` keyed per column (one op per column, random order) |
| 5 | **High** | security / logging | `sendError` returns `err.Error()` and the full SQL; panic recovery writes the panic value to the 500 body |
| 6 | **High** | security / correctness | Auth hooks return an error plus `Abort`; handler replies 400 `hook_error` instead of 401; hooks are opt-in; anonymous = `UserID 0` |
| 7 | **Medium** | security | `DecodeParam` (`ZIP_`/`__`) is applied to every header/param, ignores errors and recurses without a depth limit; headers matched with `HasPrefix` |
| 8 | **High** | slowness | Default limit 100 000, no `X-Limit` cap, no LIMIT with `NoCount`/`skipcount`, unbounded `io.ReadAll` of `[post_body]`, 15-minute timeout, full `COUNT(1)` per list request |
| 9 | **Medium** | locking | `HookRegistry` map and `variablesCallback` are unsynchronized; `Register`/`Clear*` race with `Execute` |
| 10 | **Medium** | security | Dollar-quote substitution (`[post_body]`, `[user]`, `[method]`, …) skips backslash escaping; `[id_session]` substituted unquoted |
| 11 | **Low** | correctness | `Content-Range` offset comes from the `offset` query param only; header offset ignored |
| 12 | **Low** | correctness | `X-Select-Fields`/`X-Not-Select-Fields` accepted but no-ops; `sort` `-col` negates instead of DESC |
| 13 | **Low** | panic / logging | Recovery is handler-level only; `Serving: Records` logged at Info per request; hook/filter strings logged unscrubbed (X8) |
| 14 | **Low** | correctness | `BeforeResponse` runs post-commit on the pool, not the tx (see `audit/single_tran.md`) |
| 15 | **Low** | security | Security adapter hard-codes schema `public` and entity `sql_query`; per-entity rules cannot be applied |
| 16 | **Info** | testing | Regexes compiled per call (`ValidSQL`, `sqlStripStringLiterals`); no `-race` in CI (X1); no hostile-input tests for findings 1-4 |

## 1. Raw SQL fragments behind a keyword denylist — High

`ApplyFilters` (`parameters.go:283-297`) passes `X-Custom-SQL-W` and
`X-Custom-SQL-Or` through `ValidSQL(..., "select")` and splices the result into
the query. `sort` goes the same way into `ORDER BY` (`function_api.go:~226`).
The denylist (`function_api.go:964-979`) removes `;`, `--`, `/*`, `*/`, `xp_`,
`sp_` and a few keywords **followed by a space**. It is not a parser:
- Subqueries, function calls (`pg_sleep`, `pg_read_*` where permitted),
  `SELECT` itself and `)` are not blocked; a `)` can close the
  `COUNT(1) FROM (%s) cnts` wrapper (`function_api.go:~241`).
- Keywords are removed rather than rejected, so input can be shaped so that
  removal assembles a different token.
- Whitespace variants (tab, newline) bypass the `keyword␠` patterns.

Whether the raw fragments are reachable is decided by the handler; they are
parsed whenever `ParseParameters` runs, i.e. always. Fix: drop the two headers
from the wire contract, or accept only a column/operator/value structure built
by the server; validate `sort` against `^[A-Za-z0-9_.]+( (ASC|DESC))?(,…)*$`
and ideally an allowlist of columns.

## 2. OR precedence widens results — High

`sqlQryWhereOr` (`parameters.go:381-411`) rewrites `WHERE a AND b` into
`WHERE a AND b OR (c)`. SQL evaluates `AND` first, so the result is
`(a AND b) OR c`: any row satisfying `c` is returned regardless of `a`/`b`.
Where `a` is a tenant or ownership predicate in the template, a client-supplied
OR condition (`X-SearchOr`, `X-Custom-SQL-Or`, search operator with logic OR)
returns other tenants' rows. Verified with `ParseParameters` + `ApplyFilters`
on generated headers. Fix: wrap the existing WHERE body in parentheses before
appending `OR (...)`, or build a predicate tree.

## 3. Substring-based clause location — Medium

Both helpers use `strings.Index` on `" where "`, `" group by"`, `" order by"`,
`" limit "` over the whole lower-cased query. A match inside a string literal,
a subquery, a CTE or a column alias selects the wrong insertion point, and
`wherePos > 0` decides AND-append vs. new WHERE on the first match anywhere.
`ApplyDistinct` (`parameters.go:363-378`) similarly inserts after the first
`SELECT` substring, and the ORDER BY test (`function_api.go:~224`) compares the
first `order by` to the first `from `. `sqlStripStringLiterals` exists
(`function_api.go:858`) but is not used by these helpers.

## 4. Filter handling inconsistencies — Medium

- `ApplyFilters` builds `col = value` for `X-FieldFilter` without quoting the
  value (`parameters.go:248-250`), so a string value becomes a column reference
  (`status = active`). `mergeHeaderParams` quotes the same filter, so the
  `SqlQuery` path applies it twice with different semantics.
- `RequestParameters.SearchOps` is a map keyed by column; two operators on one
  column overwrite each other and map iteration order makes the generated WHERE
  non-deterministic.

## 5. Information disclosure in errors — High

- `sendError` (`function_api.go:1150-1172`) sets `Detail = err.Error()` and,
  for `*common.SQLError`, `SQL` = the final statement, including the template,
  substituted values and any injected fragment. Used by every failure path
  (`query_failed`, `count_failed`, `hook_error`).
- Panic recovery in `SqlQueryList` (`:80-86`) and `SqlQuery` (`:433-439`) calls
  `http.Error(w, fmt.Sprintf("Internal server error: %v", err), 500)`; the
  panic value reaches the client. Same class as `middleware` finding 4.
Fix: log server-side, return a generic message plus a request id.

## 6. Auth hook abort returns 400, hooks opt-in — High

`RegisterSecurityHooks` (`security_adapter.go:14-55`) sets `Abort`,
`AbortCode=401` **and returns an error**. `HookRegistry.Execute`
(`hooks.go:113-137`) returns the error before it evaluates `Abort`, and the
handler maps that to `sendError(400, "hook_error", …)`
(`function_api.go:~202`). The 401 branch in the handler is only reachable for
hooks that set `Abort` without returning an error. Clients therefore see 400
with `Detail: "hook execution failed: authentication required"`.

Also: without `RegisterSecurityHooks` there is no authentication at all; a
missing user context is replaced with `UserID 0, "anonymous"`
(`function_api.go:~103`) and the request proceeds. Fix: return nil after
setting `Abort` in the auth hooks, or have the handler honour `AbortCode` when
the error wraps an abort; consider fail-closed by default.

## 7. Header/param decoding — Medium

`decodeValue` (`parameters.go:203`) calls `restheadspec.DecodeParam` and drops
the error. `DecodeParam` replaces all `ZIP_`/`__` occurrences and decodes
recursively with no depth limit, so one value can force repeated base64/gzip
work (decompression amplification, since size is not capped). Header keys are
matched with `HasPrefix`, so `X-SearchOp-<anything>` variants and unrelated
headers with the same prefix are interpreted.

## 8. Unbounded resource use — High

- `parameters.go:54` default `Limit: 100000`; `X-Limit` and `limit` accept any
  positive integer.
- In `SqlQueryList` the `LIMIT`/`OFFSET` clause is added **only inside
  `if !options.NoCount`** (`function_api.go:~232-251`); `NoCount` or
  `X-SkipCount` returns the whole result set.
- `COUNT(1) FROM (<full query>)` runs on every list request (double execution
  cost).
- `[post_body]` uses `io.ReadAll(r.Body)` (`function_api.go:913`) with no
  `http.MaxBytesReader`; the body is also embedded into the SQL text.
- `context.WithTimeout(…, 15*time.Minute)` (`:91`, `:444`) holds a transaction
  and pooled connection for up to 15 minutes per request.
- `ValidSQL` and `sqlStripStringLiterals` compile regexes on each call.
Fix: hard cap on limit, always apply LIMIT, cap body size, configurable timeout.

## 9. Unsynchronized registry — Medium

`HookRegistry.hooks` (`hooks.go`) is a plain map; `Register`, `Clear`,
`ClearAll` mutate it while `Execute` reads it from request goroutines. Safe
only if all registration completes before serving. `Handler.variablesCallback`
(`function_api.go:60-68`) has the same property. Fix: `sync.RWMutex` and copy-on-
read, or document and enforce "register before serve".

## 10. Dollar-quote substitution — Medium

`safeSubstituteVar` returns the raw value when the placeholder is adjacent to
`$` (`function_api.go:1044-1049`), so neither backslash nor quote escaping
applies. The tag is neutralised only for `$M$`, `$PBODY$` and the equivalents
in `replaceMetaVariables`; a caller-supplied value in a template that uses a
different tag (or `$$`) is not. `isInsideDollarQuote` inspects only the first
occurrence of the placeholder. `[id_session]` is replaced without any quoting
(`function_api.go:~900`); its source is the auth layer, but it becomes an
injection point if a session token format allows quotes.

## 11-12. Behavioural defects — Low

- `Content-Range` offset uses only `r.URL.Query().Get("offset")`
  (`function_api.go:~319`) while the applied offset can come from
  `X-Offset`; the reported range is wrong for header-driven paging.
- `ApplyFieldSelection` (`parameters.go:226-241`) only logs; the headers have
  no effect. `sort=-col` is not converted to DESC; it is emitted as `ORDER BY
  -col`, which negates the column value.

## 13. Panic handling and logging — Low

Recovery exists per handler only (no middleware-level recovery for hooks run
outside), and the stack is logged via `logger.Error`, which forwards to Sentry
unscrubbed (X8). `logger.Info("Serving: Records …")` runs on every list request.
`logger.Debug` lines include the generated filter SQL and attacker-supplied
values. Hook failures log `err` with attacker-influenced text.

## 14. `BeforeResponse` outside the transaction — Low

`BeforeResponse` executes after `RunInTransaction` returns, with
`hookCtx.Tx = h.db` (`function_api.go:~336-343`, `:~640`). A hook that writes
cannot be rolled back with the query, and a failure returns 500 after the work
committed. Tracked in `audit/single_tran.md`.

## 15. Security adapter — Low

`funcSpecSecurityContext.GetSchema()` returns `"public"` and `GetEntity()`
returns `"sql_query"` for every endpoint (`security_adapter.go:84-92`), so
column/row security rules keyed by entity cannot distinguish funcspec endpoints.
`GetModel`, `GetQuery`, `SetQuery` are stubs.

## 16. Testing — Info

Tests cover handler flow, hooks and parameter parsing. No test exercises the
hostile inputs of findings 1-4 or the 401-vs-400 outcome. There is no `-race`
job in CI (X1). The earlier note about a failing
`TestReplaceMetaVariables/Replace_[user]` no longer reproduces: the package
passes today.

## Cross-references

X1 (no `-race`), X7 (inconsistent panic handling), X8 (logger forwards to
Sentry unscrubbed), `middleware` finding 4 (panic value in body),
`audit/single_tran.md` (post-commit hooks).
