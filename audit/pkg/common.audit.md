# Audit: `pkg/common`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/common` (+ `adapters/database`, `adapters/router`) |
| **Files** | `sql_helpers.go` (1060), `recursive_crud.go` (645), `validation.go` (444), `json_column.go` (402), `interfaces.go` (311), `spatial_helpers.go` (317), `handler_utils.go` (309), `json_condition.go` (219), `types.go` (192), `cors.go` (156), `handler_example.go` (97); `adapters/database/bun.go` (1767), `pgsql.go` (1600), `gorm.go` (1018), `query_metrics.go` (335), `pgsql_preload_example.go` (275), `pgsql_example.go` (176), `test_helpers.go` (132), `utils.go` (117); `adapters/router/mux.go` (238), `bunrouter.go` (214) |
| **Audit date** | 2026-09-30 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |
| **Depth** | deep for `sql_helpers.go`, `validation.go`, `cors.go`, `recursive_crud.go`, `json_column.go`/`json_condition.go` and the reconnect/transaction paths of the three DB adapters; medium for the rest; the `*_example.go` files were skimmed. Findings 1–3 were verified with throw-away probe tests, which were deleted afterwards |

## Summary

`pkg/common` is the shared core behind every spec handler. It contains the
`Database` / `SelectQuery` abstraction and its Bun, GORM and raw-`pgx`
adapters, the request-option types, column validation, JSON-column parsing,
nested (recursive) CRUD, CORS, and a set of SQL string helpers. The spec
packages feed **client-supplied raw SQL fragments** through those helpers:
`x-custom-sql-w`, `x-custom-sql-or`, `x-custom-sql-join`, preload `where`,
sort expressions and cursor filters.

The central problem is that **`SanitizeWhereClause` / `validateWhereClauseSecurity`
is a keyword denylist applied to raw SQL**, and the result is concatenated
straight into the query. A denylist can't make arbitrary client SQL safe, and
this one misses subqueries, functions, comments and parenthesis balancing.
With the helpers exactly as the handlers call them, a client can:

- escape the outer parentheses and OR past every filter the server adds
  afterwards (**row security, tenant filters, the PK filter**). This is
  verified. Row security is inert anyway today (`security.audit.md` finding 2),
  but this bug will defeat it as soon as that is fixed;
- read any table the DB role can see through a subquery;
- stall a connection with `pg_sleep`;
- bypass the keyword list with a comment (`delete/**/from`).

Meanwhile, legitimate filters that merely *contain* a word like `update` are
silently dropped, and the query runs **unfiltered** (fail-open).

Other headline findings:

- `SetCORSHeaders` reflects **any** Origin with `Allow-Credentials: true` and
  ignores `AllowedOrigins`.
- Nested CRUD updates and deletes child rows by primary key alone. A client can
  modify or delete (or re-parent) any row in a related table.
- Sort validation lets arbitrary SQL through whenever a custom join has no
  alias.

On the question that started this audit (idle connections becoming unusable),
the relevant part of `pkg/common` is the adapters' reconnect logic (finding 5).
It is only partly wired into the Bun and pgx adapters, and it's what calls
`dbmanager`'s destructive `Reconnect` (`dbmanager.audit.md` findings 1–2).

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **Critical** | security | Client raw-SQL WHERE (`x-custom-sql-w`/`-or`, preload where, cursor) is protected only by a keyword denylist: parenthesis escape defeats server-added filters; subqueries, `pg_sleep` and comment bypasses all pass (verified) |
| 2 | **Critical** | security | `SetCORSHeaders` reflects any `Origin` and sends `Access-Control-Allow-Credentials: true`; `AllowedOrigins` is never consulted |
| 3 | **High** | security | Sort validation: an empty join alias makes `strings.Contains(col, "")` accept **any** sort string, and `(…)` sort expressions allow arbitrary subqueries (verified) |
| 4 | **High** | security | Nested CRUD (`recursive_crud.go`) updates and deletes child rows by `WHERE pk = ?` only, with no parent/ownership constraint; a client-supplied `_request` switches the operation per object |
| 5 | **High** | locking / availability | Adapter reconnect is inconsistent (Bun and pgx query builders never reconnect) and, where it exists, calls dbmanager's pool-closing `Reconnect`; `BunAdapter.CommitTx`/`RollbackTx` are silent no-ops |
| 6 | **Medium** | security / correctness | `SanitizeWhereClause` fails open: on a denylist hit it returns `""`, so the client's filter is dropped and the query returns unfiltered rows; false positives on ordinary data (`'awaiting update'`, `last_update`) |
| 7 | **Medium** | security | Any column name starting with `cql` passes `ValidateColumn` unconditionally |
| 8 | **Medium** | slowness | Request bodies are read with unbounded `io.ReadAll` in both router adapters |
| 9 | **Medium** | logging | Failed queries log the fully interpolated SQL, and nested CRUD logs full row data, at `Error`, which is forwarded to Sentry (`_CROSS-CUTTING.audit.md` X8) |
| 10 | **Low** | correctness | `stripEmptyComparisonClauses` regexes rewrite SQL without respecting string literals; quote tracking ignores `''`; `qualifyColumnInCondition` compiles a regex per call |
| 11 | **Low** | locking | Adapter fields read without their mutex (`BunAdapter.NewSelect` `db: b.db`, `DriverName`, `PgSQLAdapter.GetUnderlyingDB`) race with `reconnectDB` |
| 12 | **Info** | — | `json_column.go` / `json_condition.go` are well built: allowlisted casts, path bound as a single `text[]` parameter, identifiers validated and quoted |

---

### 1. Critical — Client raw-SQL WHERE is guarded only by a keyword denylist

`sql_helpers.go:118-161` (`validateWhereClauseSecurity`), `169-305`
(`SanitizeWhereClause`), `375-395` (`EnsureOuterParentheses`).

Call sites that pass **client-controlled** strings:

| Source | Call site |
|---|---|
| `x-custom-sql-w` | `restheadspec/handler.go:692-699` → `query.Where(...)` |
| `x-custom-sql-or` | `restheadspec/handler.go:703-710` → `query.WhereOr(...)` |
| `x-custom-sql-join` | `restheadspec/headers.go:666`, `1330` (sanitized with `tableName ""`) |
| preload `where` | `resolvespec/handler.go:2386, 2458`; `restheadspec/handler.go:618, 1170` |
| cursor filters | `resolvespec/handler.go:458`; `restheadspec/handler.go:916`; `resolvemcp/handler.go:304` |

The pipeline is `AddTablePrefixToColumns` → `SanitizeWhereClause` →
`EnsureOuterParentheses` → `query.Where(s)`, with **no bind arguments**. The
only security check is a substring search for `delete `, `update `, `drop `,
`;delete`, and similar.

A probe reproduced the handler pipeline and then appended a server-side filter
`Where("tenant = ?", 5)`, which is what a row-security or tenant hook does:

| Client `x-custom-sql-w` | Resulting SQL / effect |
|---|---|
| `1=1)) OR ((1=1` | `WHERE ((1=1)) OR ((1=1)) AND (tenant = 5)`: **every tenant's rows**, because `AND` binds tighter than `OR` |
| `id = 1 or (select count(*) from pg_shadow) > 0` | passes unchanged, so boolean-oracle exfiltration from any readable table works |
| `id = 1 and pg_sleep(5) is not null` | passes; each request pins a pool connection for as long as the client likes |
| `id = 1; delete/**/from items` | passes, because the comment defeats `"delete "` (whether it executes depends on the driver's multi-statement handling) |

`EnsureOuterParentheses` only checks whether the string *already* starts and
ends with a matching pair. It never checks that the parentheses inside are
balanced, which is what the escape relies on. `x-custom-sql-or` is worse by
design: `WhereOr` ORs the client clause against **every** condition already on
the query, so it needs no escape at all to widen a server-side filter.

Row security currently has no effect (`security.audit.md` finding 2). Fixing
that type assertion will **not** give tenant isolation while these headers
exist, and the same escape defeats the server's own PK scoping
(`restheadspec/handler.go:759-766`).

**Failure scenario.** An authenticated user of tenant A sends
`X-Custom-SQL-W: 1=1)) OR ((1=1` on a list endpoint and receives tenant B's
rows. Or they send
`X-Custom-SQL-W: (select substr(passwd,1,1) from pg_shadow limit 1) = 'm'` and
extract data one character at a time.

**Recommendation.** Stop accepting raw SQL from clients. Remove the
`x-custom-sql-*` headers from the public surface, or gate them behind an
explicit server-side allowlist per endpoint. Route client filtering through
the structured `FilterOption` path, which validates column names and binds
values. If raw fragments have to stay for trusted internal callers:

- parse them properly (for example with `pg_query_go`) and allow only column
  references, literals and comparison operators;
- reject subqueries and function calls;
- verify that parentheses are balanced outside string literals;
- apply server-side security predicates last, as a wrapper
  `WHERE (server) AND (client)`, and never let `WhereOr` attach at top level.

---

### 2. Critical — CORS reflects every Origin with credentials

`cors.go:117-155`:

```go
origin := r.Header("Origin")
if origin == "" {
	origin = "*"
} else { ... Vary: Origin }
w.SetHeader("Access-Control-Allow-Origin", origin)
...
requestedHeaders := r.Header("Access-Control-Request-Headers")
if requestedHeaders != "" {
	w.SetHeader("Access-Control-Allow-Headers", requestedHeaders)
}
...
if origin != "*" {
	w.SetHeader("Access-Control-Allow-Credentials", "true")
}
```

`DefaultCORSConfig` (`cors.go:19-48`) carefully builds `AllowedOrigins` from the
server config, and `SetCORSHeaders` **never reads it**. Any site the victim
visits can make credentialed cross-origin requests and read the responses.
Allowed request headers are also reflected, so `Authorization` and every
`X-Custom-SQL-*` header pass preflight. `SetCORSHeaders` is called on every
route in `resolvespec/resolvespec.go` (lines 56-347), and `restheadspec` follows
the same pattern.

**Failure scenario.** A user logged in through cookie auth (`SetSessionCookie`/`GetSessionCookie`,
`pkg/security/middleware.go:512-540`) visits `evil.example`. Its script calls
`fetch("https://api/…/users", {credentials:"include"})` and reads every record
the user can see. Combined with finding 1, it can read other tenants' records
too.

**Recommendation.** Send `Allow-Origin: <origin>` and `Allow-Credentials`
only when `origin` is in `config.AllowedOrigins`, matched exactly. Otherwise
omit the CORS headers. Check requested headers against `AllowedHeaders`
instead of echoing them. Build `exposeHeaders` in a fresh slice: `append` onto
`config.AllowedHeaders` can write into a shared backing array.

---

### 3. High — Sort validation bypasses

`validation.go:271-301`:

```go
foundJoin := false
for _, j := range options.JoinAliases {
	if strings.Contains(sort.Column, j) {   // j may be ""
```

`restheadspec/headers.go:674-678` deliberately appends `""` to `JoinAliases`
when `extractJoinAlias` can't find an alias (for example
`LEFT JOIN t ON …` with no alias, or a LATERAL join without one).
`strings.Contains(x, "")` is always `true`, so **any** sort string is
accepted. `restheadspec/handler.go:790-793` then passes anything containing a
`.` or wrapped in `(…)` to `OrderExpr` **verbatim**. Even with a real alias,
the check is a substring test, so a sort like `j.id, (select …)` passes for
alias `j`.

Separately, `(…)` sort expressions are checked by `IsSafeSortExpression`
(`validation.go:381-427`), another denylist. It blocks DML keywords, comments
and `;`, but allows subqueries and functions.

Probe results: with `JoinAliases: [""]`, sort `x.id, (select pg_sleep(10))`
was kept. With no joins, sort `(select passwd from pg_shadow limit 1)` was kept.

**Failure scenario.** A client sends a custom join with no alias plus an
arbitrary ORDER BY expression, which gives injection in ORDER BY: time-based
DoS, or data extraction via `ORDER BY (CASE WHEN (subquery) THEN a ELSE b END)`.

**Recommendation.** Skip empty aliases. Match `alias + "."` as a prefix, then
validate the column after the dot against the joined table. Drop client
supplied sort *expressions*, or restrict them to a server-registered set
(the `cql` computed columns already provide this).

---

### 4. High — Nested CRUD modifies arbitrary related rows

`recursive_crud.go:64-67, 144-196, 344-380, 395-520`.

- Children are updated with `UPDATE <related> SET … WHERE pk = ?`, deleted with
  `DELETE FROM <related> WHERE pk = ?`, and both use the child's PK **from the
  request body**. Nothing checks that the child belongs to the parent being
  written or to the caller's tenant.
- For updates, the parent's FK is injected into the child data
  (`recursive_crud.go:495-520`), so updating a foreign child **moves it under
  the attacker's parent** as well.
- `_request` (`recursive_crud.go:64-67, 205-212`) lets the client choose
  `insert`/`update`/`delete` for each nested object, independent of the HTTP
  method or the operation the top-level handler authorised.
- These statements go straight to `p.db`, so the spec handlers' Before*/After*
  hooks, and any row-security or audit hooks, don't run for nested rows.

**Failure scenario.** A client sends a `PUT /orders/1` whose body includes
`"lines": [{"id": 9999, "_request": "delete"}]`. Row 9999 of `order_lines` is
deleted even if it belongs to another customer's order.

**Recommendation.** For has-many and has-one children, add
`AND <fk> = <parentID>` to update and delete statements, and treat
`RowsAffected() == 0` as a forbidden or not-found error. Run the same hook
chain (including row security) for nested rows. Allow `_request` only for
operations the top-level request is authorised to perform.

---

### 5. High — Reconnect logic is partial, and it triggers pool destruction

`adapters/database/bun.go:131-143, 167-186, 226-273, 1298-1318`;
`pgsql.go:58-79, 81, 134, 160, 220`; `gorm.go:55, 122-134`.

- **Coverage is uneven.** `BunAdapter` only retries after reconnecting in
  `Exec`, `Query`, `BeginTx` and `RunInTransaction`. `NewSelect`, `NewInsert`,
  `NewUpdate` and `NewDelete` capture `getDB()` once, and `BunSelectQuery.Scan`,
  `ScanModel`, `Count` and `Exists` call bun directly with no retry. Those are
  the paths every read handler uses. `PgSQLAdapter` query builders have no
  reconnect either. Only `GormAdapter` wires `reconnect` into its
  select, insert, update and delete builders.
- **Where it exists, it's harmful.** `reconnectDB` calls the dbmanager factory,
  which runs `sqlConnection.Reconnect` and closes the pool shared by every
  other adapter and handle (`dbmanager.audit.md` findings 1–2). Concurrent
  failures each call the factory.
- **Detection is a substring match.** `isDBClosed` (`pgsql.go:72`) matches
  `"sql: database is closed"`. That only happens *after* someone closed the
  pool, so the reconnect mechanism mainly exists to recover from damage it
  causes itself. It does nothing for the real idle-socket failure (a hang, or
  `driver.ErrBadConn`, which `database/sql` already retries).
- `BunAdapter.CommitTx` / `RollbackTx` (`bun.go:239-249`) return `nil` without
  doing anything. A caller using the `BeginTx`-less path gets
  "committed" when nothing happened. `BunTxAdapter` is correct.

**Recommendation.** Remove adapter-level reconnect entirely and rely on
`database/sql`'s pool (see the fix order in `dbmanager.audit.md`). Make
`BunAdapter.CommitTx`/`RollbackTx` return an explicit
"not in a transaction" error. Add a per-query `context.WithTimeout` in the
adapters as the single place where query deadlines are enforced.

---

### 6. Medium — `SanitizeWhereClause` fails open and has false positives

`sql_helpers.go:176-179`:

```go
if err := validateWhereClauseSecurity(where); err != nil {
	logger.Debug("Security validation failed for WHERE clause: %v", err)
	return ""
}
```

Every caller treats `""` as "no filter" and skips `query.Where`. So a clause
the sanitizer rejects is **removed**, and the request runs unfiltered instead
of failing. The denylist is a substring match on the whole clause, string
literals included, so ordinary filters trip it. The probe showed
`status = 'awaiting update approval'` and `last_update > '2020-01-01'` both
returning `""`, which gives an unfiltered list. For a preload `where` or a
cursor filter, that means returning rows the client asked to exclude, or
breaking pagination.

**Recommendation.** Return an error and make the handler respond `400`. Never
turn a rejected filter into "no filter".

---

### 7. Medium — `cql*` columns bypass column validation

`validation.go:107-110` accepts any column that starts with `cql`
(case-insensitive), with no further checks. The probe showed
`IsValidColumn("cql1); drop")` returning `true`. The computed-column mechanism
only ever generates `cql1…cqlN` (`restheadspec/headers.go:871, 1380`). Whether a
client-supplied `cql…` string reaches SQL unquoted depends on the downstream
handler (`restheadspec/handler.go:490-509`, `cursor.go:189`). The validator
shouldn't be where that decision is made.

**Recommendation.** Accept only `^cql[0-9]+$`, and only when that computed
column was actually registered for the request.

---

### 8. Medium — Unbounded request body reads

`adapters/router/mux.go:101-115` uses `io.ReadAll(h.req.Body)` with no
`http.MaxBytesReader`, and `bunrouter.go:102-115` delegates to it. The
request-size middleware exists but isn't mounted (`_CROSS-CUTTING.audit.md`
X10), so a single request can make the process buffer gigabytes.

**Recommendation.** Wrap the body in `http.MaxBytesReader` inside the adapter,
with a configurable limit (for example 10 MB) and a sensible default.

---

### 9. Medium — Sensitive data in error logs

- `bun.go:1311-1315` (and the equivalent in `ScanModel`/`Count`, and in
  `pgsql.go` / `gorm.go`) logs `b.query.String()`, the SQL with **all argument
  values interpolated**, at `Error` on every failed query. That includes
  filter values, emails, and tokens used as lookup keys.
- `recursive_crud.go` logs `data=%+v` (whole rows, including password or secret
  columns) at `Error` on every failed nested write (lines 121, 153, 312, 342,
  352, 509, 528, 550).
- `logger.Error` is forwarded to Sentry unscrubbed (`_CROSS-CUTTING.audit.md`
  X8), and a hostile client can trigger failing queries at will.

**Recommendation.** Log the query with placeholders, not interpolated. Log
column names, not values. Put full dumps behind a debug flag.

---

### 10. Low — Fragile SQL string rewriting

- `reEmptyCompMid` / `reEmptyCompEnd` (`sql_helpers.go:66-80`) run over the
  whole SQL string, including string literals and subqueries, and silently
  delete text that matches `col = and`. That can change a query's meaning.
- The quote tracking in `splitByAND` / `findOperatorOutsideParentheses` /
  `stripWrappingParens` toggles on every `'`, so an escaped `''` inside a
  literal flips the state.
- `qualifyColumnInCondition` (`sql_helpers.go:751-760`) compiles a regex on
  every call in the per-request path.

---

### 11. Low — Unsynchronised adapter field reads

`BunAdapter` protects `db` with `dbMu` in `getDB`/`reconnectDB`. But
`NewSelect` stores `db: b.db` (`bun.go:170`, used for count queries), and
`DriverName` reads `b.db` (`bun.go:280`), both without the lock.
`PgSQLAdapter.GetUnderlyingDB` (`pgsql.go:220`) does the same. These are data
races with `reconnectDB`, and `-race` would flag them
(`_CROSS-CUTTING.audit.md` X1). In practice the count query can run against
the old, closed pool.

---

### 12. Info — JSON column parsing is sound

`json_column.go` / `json_condition.go` are a good model for how the rest of
this package should handle client input:

- The base column must match `^[A-Za-z_][A-Za-z0-9_]*$` and is quoted with
  `QuoteIdent`.
- Casts go through an allowlist.
- The JSON path is always bound as one `?::text[]` parameter, with depth and
  segment-size limits.
- The dotted shorthand counts as JSON only when reflection confirms that the
  base is a JSON column.
- The alias is validated and quoted.

No findings.

---

## Panic handling

The adapter methods (`Scan`, `ScanModel`, `Count`, `Exec`, `Query`,
`RunInTransaction`) recover and convert panics with `logger.HandlePanic`.
`PgSQLAdapter.RunInTransaction` rolls back on panic before re-raising or
converting it. `BunAdapter.RunInTransaction` relies on bun's `RunInTx`, which
also rolls back. No panic paths were found in `sql_helpers.go`,
`validation.go` or the JSON parser that are reachable from client input.
Slicing is length-guarded. `recursive_crud.go` recurses over the model's
relation graph. For self-referential models, depth is bounded only by the
JSON decoder's nesting limit, which makes it a slowness issue rather than a
crash.

## Test coverage

`sql_helpers_test.go` and `validation_test.go` test the *intended* behaviour
of the sanitizer and validator. None of them test hostile inputs. Each probe
case in findings 1, 3, 6 and 7 is a one-line table entry and should be added
as a regression test that asserts rejection. `cors.go` and `recursive_crud.go`
have no security-focused tests.
