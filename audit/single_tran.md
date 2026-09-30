# Single transaction per request — plan

## Goal
- Every DB statement and every hook that touches the DB in one request runs on **one transaction / one connection**.
- Hooks never receive the raw pool (`h.db`).
- Fixes: RLS GUCs (`set_config(..., true)`) lost on reads/creates/deletes; extra pool connections; select-then-write races.

## Why
- `set_config(..., true)` is transaction-local. A hook on the pool, or a query on another pool connection, never sees it → RLS returns 0 rows / 42501.
- Each un-transacted call takes its own pool connection → bursts with a small pool (see `dbtrace`).
- Already fixed: read/create hooks in `resolvespec` + `restheadspec` (commit `47708fc`, tag >= v1.1.28). Consumers on older tags still show the bug.

## Current state (verified by reading code; not yet by `dbtrace`)
| Spec | Read | Create | Update | Delete |
|---|---|---|---|---|
| restheadspec | tx; `AfterRead` post-commit on pool | tx; `AfterCreate` post-commit on pool | tx; re-fetch + `BeforeScan` post-commit on pool (`:1667-1674`) | **single: no tx, hook + select + delete on pool (`:1945-1994`)**; batch: tx, per-item `BeforeDelete` inside |
| resolvespec | tx | tx | tx; re-fetch on pool (`:1297, 1449, 1602`) | **single: hook + select + delete on pool (`:1654, 1794, 1806`)**; batch: one `BeforeDelete` before tx, none per item |
| websocketspec | **pool** (`:563-672`) | **pool** (`:708`) | **pool** (`:744`) | **pool** (`:757`) |
| mqttspec | **pool** (`:674-789`) | **pool** (`:838`) | **pool** (`:875`) | **pool** (`:889`) |
| resolvemcp | **pool** (`:253`) | single: **pool** (`:445`); batch: tx | tx | tx |
| funcspec | tx; `BeforeResponse` post-commit on pool (`:337, 640`) | — | — | — |

- Correction to earlier note: "no transactions" in mqttspec/websocketspec/resolvemcp-read is a gap for this problem, not a non-issue.
- `BeforeHandle` runs before any tx by design (auth + model checks, `PreloadSecurityRules`). Keep it DB-free except security preload (own connection, cached).

## In-tx hook coverage today (verified)
- Already in tx with `Tx: tx`: `BeforeRead`, `BeforeCreate`, `BeforeUpdate`, `BeforeScan` (read/create/update, both specs); restheadspec batch delete `BeforeDelete` + `AfterDelete`.
- **Not in tx:** single `BeforeDelete`/`AfterDelete` (both specs), resolvespec batch delete (no per-item hook), all `After*` post-commit, all websocketspec/mqttspec hooks, resolvemcp read/single create.
- Gap beyond coverage: no single guaranteed "tx opened" point. User/RLS stamping would have to be repeated in each `Before*` hook and is missed by any path without one (e.g. resolvespec batch delete). `OnTxBegin` closes this: fires once per tx, first, for read/insert/update/delete and for the second short tx.

## Scope
- `OnTxBegin` + `runInTx` apply to **all six**: resolvespec, restheadspec, websocketspec, mqttspec, resolvemcp, funcspec.
- Each spec has its own `HookType` (resolvespec, restheadspec, websocketspec, resolvemcp, funcspec); mqttspec aliases websocketspec, so it inherits the constant but needs its own handler wiring.
- Same semantics everywhere: fires once per tx, first, for read/insert/update/delete and the second short tx; failure aborts + rolls back, nothing leaked.
- Each spec's `security_hooks.go` registers the user/RLS stamping on `OnTxBegin`.
- Shared helper preferred over six copies: one small function in `pkg/common` (begin tx, set `Tx`, call spec-supplied begin callback), each spec passes its own hook executor.

## funcspec (different)
- Custom SQL handlers (`SqlQuery`, `SqlQueryList`), no CRUD, no model registry; one tx per request already (`:195`, `:561`).
- Already in tx: `BeforeQuery`/`BeforeQueryList`, `BeforeSQLExec`, `AfterSQLExec`, `AfterQuery`/`AfterQueryList`, plus `BeforeOp`.
- `BeforeOp` = generic pre-hook via `ExecuteBeforeOp`, fires before every `Before*` in the tx; but it fires **twice** per tx (query hook + `BeforeSQLExec`), so it is not a once-per-tx point.
- Only gap: `BeforeResponse` runs post-commit with `Tx = h.db` (`:337`, `:640`).
- Applies from this plan: once-per-tx `OnTxBegin` (stamping user/RLS before any SQL, incl. hook-mutated SQL), `BeforeResponse` on a tx, fail-closed abort.
- Does not apply: delete/insert/update phases, second re-fetch tx (no re-fetch; SQL is user-defined), `BeforeHandle` preload.
- Decided: add a real `OnTxBegin`. `BeforeOp` unchanged.

## Common interface (`pkg/common`, new `txhook.go`)
- Precedent: `security.SecurityContext` + per-spec `newSecurityContext(hookCtx)` adapter. Same pattern here.
- `common.TxHookName` = `"on_tx_begin"`: one shared string; each spec declares `OnTxBegin HookType = common.TxHookName` (HookTypes are per-spec types, so the constant value is shared, not the type).
- `common.TxContext` interface, implemented by each spec's `HookContext` via a small adapter: `GetContext()`, `GetTx()`, `SetTx(common.Database)`, plus `Abort` accessors for the abort path.
- `common.RunRequestTx(ctx, db, tc TxContext, onBegin func() error, body func(tx common.Database) error) error`: `RunInTransaction` -> `tc.SetTx(tx)` -> `onBegin()` (spec passes `registry.Execute(OnTxBegin, hookCtx)`) -> `body(tx)`. `onBegin` error or abort = return error = rollback.
- Shared stamping: one function in `pkg/security` taking `SecurityContext` + `common.Database` (sets tx-local user/RLS); each spec's `RegisterSecurityHooks` registers it on `OnTxBegin`. No per-spec copies of the logic.
- Each spec keeps its own registry/`HookContext`; only the tx lifecycle and stamping are shared.
- Out of scope: unifying the six `HookContext` / registry types.

## Design
1. **`OnTxBegin` hook** (new `HookType`, all specs). Runs first inside every tx the handler opens; gets `tx` in `hookCtx.Tx`. RLS stamping is registered once there; reads owner/tenant from request context.
2. **`runInTx` helper per handler**: wraps `RunInTransaction`, sets `hookCtx.Tx = tx`, fires `OnTxBegin`, runs the body. All handler paths use it; no path passes `h.db` to a hook.
3. **Post-commit work** (`After*`, update re-fetch, `BeforeResponse`): run in a second short `runInTx` (so `OnTxBegin` re-applies). Not inside the main tx.
4. **Delete**: hook → select → delete in one tx; 404 on no row; cache invalidation after commit.
5. **Backward compat**: hooks keep the same names/order; only `hookCtx.Tx` changes from pool to tx. `OnTxBegin` is additive.

## Decisions (settled)
- Insert/update: re-fetch + `AfterCreate`/`AfterUpdate`-style post-commit work run in a **second short tx** (must see trigger changes). Only insert/update; read and delete have no second tx.
- Update re-fetch is a plain SELECT in that second tx. No `RETURNING`.
- `OnTxBegin` failure aborts the whole request, rolls back, returns an error with no detail leaked to the client.

## Open
- Consumer's ResolveSpec version: confirm it is >= v1.1.28 (read/create already in tx). Not blocking.

## Phases
| # | Status | Change | Files | Notes |
|---|---|---|---|---|
| 0 | DONE | Baseline: enable `dbtrace` on testserver, record `tx/tx_queries/pooled/raw` per op | `cmd/testserver`, `pkg/dbtrace` | pooled > 0 on write ops = the gaps above |
| 1 | DONE  | Delete in one tx (single + batch, per-item hooks inside tx) | `resolvespec/handler.go`, `restheadspec/handler.go` | fixes 2 pool connections + race + RLS |
| 2 | DONE  | `OnTxBegin` hook type + `runInTx` helper | `common/txhook.go`, `*/hooks.go`, `*/handler.go` | resolvespec + restheadspec; other specs in P4-6 |
| 3 | DONE  | Insert/update post-commit hooks + re-fetch in second short `runInTx` (select only) | restheadspec `:1005, 1467, 1667-1674`; resolvespec `:1297, 1449, 1602` | per decision above |
| 4 | DONE  | websocketspec + mqttspec: wrap read/create/update/delete in `runInTx` | `websocketspec/handler.go`, `mqttspec/handler.go` | mqttspec aliases websocketspec hooks; confirm `OnTxBegin` alias |
| 5 | DONE  | resolvemcp: read + single create in tx | `resolvemcp/handler.go:253, 445` | verify batch/update/delete hooks run inside tx |
| 6 | DONE  | funcspec: `OnTxBegin` (or once-per-tx `BeforeOp`), `BeforeResponse` via `runInTx` | `funcspec/function_api.go:337, 640` | |
| 7 | DONE  | Security hooks: register RLS stamping on `OnTxBegin`; document | `pkg/security/*`, README | |

## Progress
- DONE P0: baseline via `dbtrace` on real Postgres (commit `cd96404`): create/read/delete `pooled=0`; update `pooled=1` (re-fetch) = P3 target. websocketspec/mqttspec/resolvemcp not measured.
- DONE P1: single + batch delete in one tx (resolvespec, restheadspec). Not done: per-item `BeforeDelete` in resolvespec batch (behavior change, deferred).
- DONE infra: `sqlmock` delete tx tests (both specs); compose test server + `scripts/testserver-smoke.sh` (podman first); testmodels ids now serial.
- NOTE: restheadspec single delete still does the lookup before `BeforeDelete`; safe once `OnTxBegin` (P2) exists. An `AfterDelete` failure now rolls the delete back.
- DONE P2 (resolvespec + restheadspec): `common.TxHookName`, `common.TxContext` (`SetTx` only; no abort/context accessors needed since `Execute` already returns an error on abort), `common.RunRequestTx`; per-spec `OnTxBegin`, `HookContext.SetTx`, `Handler.runInTx`. Every `RunInTransaction` in both handlers now goes through it. Tests: `pkg/*/on_tx_begin_test.go` (once, first, on tx, failure rolls back). Not yet: the post-commit second tx (P3) and the security stamping registration (P7).
- DONE P3: restheadspec update re-fetch + `BeforeScan` + `AfterUpdate` and `AfterCreate` run in a second short `runInTx`; resolvespec update re-fetches (single, both batch paths) run in a second short `runInTx`. Fixed the pool reads inside the first tx (resolvespec single/batch update existing-record select, restheadspec update existence select) to use `tx`. Tests: `pkg/*/update_tx_test.go` (restheadspec uses the bun adapter; the pgsql adapter cannot build model-based updates).
- NOTE: resolvespec fires no `AfterCreate`/`AfterRead`/`AfterUpdate`-post-commit hooks other than `AfterUpdate` inside the tx; nothing more to move there.
- OPEN: restheadspec `AfterRead` still runs post-commit with `Tx = h.db` (`:1004`); decision says read has no second tx. Needs a call: run it inside the read tx, or in a short second tx.
- DONE P4: websocketspec + mqttspec. `OnTxBegin` (mqttspec re-exports the websocketspec constant), `HookContext.SetTx`, per-handler `runInTx`/`sendTxError`. Per message: read = 1 tx (Before/After hooks + queries); delete = 1 tx (Before, delete, After); create/update = tx 1 (Before + write) then tx 2 (re-fetch + `BeforeScan` + After). `create()`/`update()` no longer re-fetch; `read*`/`create`/`update`/`delete` use `hookCtx.Tx`. websocketspec `FetchRowNumber` keeps its public signature and delegates to a new tx-aware `fetchRowNumber`. A failure in begin/`OnTxBegin`/commit answers `transaction_error` with no detail. Tests: `pkg/websocketspec/tx_test.go` (sqlmock), `pkg/mqttspec/tx_test.go` (sqlite); mqttspec `update` tests now pass `Tx`.
- DECIDED in P4 (follow `AfterRead` question above): websocketspec/mqttspec run `AfterRead` inside the read tx (keeps "read has no second tx").
- DONE P5: resolvemcp. `OnTxBegin`, `HookContext.SetTx`, `Handler.runInTx`. Read = 1 tx (`BeforeRead`, count, scan, `AfterRead`; `readInTx`). Delete = 1 tx (`BeforeDelete` moved inside, after `OnTxBegin`). Create (single and batch, unified) = tx 1 (`BeforeCreate` + inserts) then tx 2 (re-fetch + `AfterCreate`); the old single-record pool insert/re-fetch is gone. Update = tx 1 (select, `BeforeUpdate`, update, `AfterUpdate`) then tx 2 (re-fetch). `BeforeHandle` still runs before any tx with `Tx = h.db`. Tests: `pkg/resolvemcp/tx_test.go` (sqlmock).
- DONE P6: funcspec. `OnTxBegin`, `HookContext.SetTx`, `Handler.runInTx` for `SqlQuery` and `SqlQueryList`. `BeforeResponse` now runs in a second short tx (`Tx` is no longer the pool). `BeforeOp` is unchanged (still per statement). A begin/`OnTxBegin`/commit failure answers 500 `transaction_error` / "Transaction failed" (before, it returned with no response); body failures still answer via `sendError`. Tests: `pkg/funcspec/tx_test.go`.
- DONE (AfterRead, decided by user): restheadspec `AfterRead` now runs in a second short tx. Test: `pkg/restheadspec/read_tx_test.go`.
- DONE P7: `pkg/security/txsettings.go`: `SecurityList.SetTxSettings(fn)`, `StampTxSettings`, `ApplyTxSettings` (configurable map, decided by user; `set_config(name, value, true)`, value hex-encoded, name validated, Postgres only, fail closed). Every spec's `RegisterSecurityHooks` registers it on `OnTxBegin`. Tests: `pkg/security/txsettings_test.go`, `pkg/resolvespec/tx_settings_test.go`. Docs: `pkg/common/TRANSACTIONS.md`.
- DONE real-Postgres check (resolvespec, testserver via compose): create `tx=1 pooled=0`, read `tx=1 pooled=0`, update `tx=2 pooled=0` (was `pooled=1`), single delete `tx=1 pooled=0`, batch create/delete `tx=1 pooled=0`. Compose now uses host networking (bridge fails here): testserver on 8123, Postgres on 8124 (was 8080/5434); integration test DSNs updated. Smoke script covers read and update. websocketspec/mqttspec/resolvemcp/restheadspec/funcspec not measured on real Postgres.
- NEXT: extra per-spec create/update tests (optional).

## Tests
- Existing: per-spec `handler_test.go`, `hooks_test.go`, `integration_test.go`; models in `pkg/testmodels/business.go`; `dbtrace` unit tests.
- Done: delete tx tests (`pkg/*/delete_tx_test.go`, sqlmock, 1-conn pool detects pool use). Missing: same for read/create/update, `OnTxBegin`, other specs.
- Add per spec/op: hook `Tx` is not the pool; `OnTxBegin` fires once per tx, before other hooks; single-ID delete = 1 tx; `dbtrace` `pooled == 0` on the request path.
- Test data: reuse `pkg/testmodels`; **ask before generating new data** (per project rule).
- Regression: full `go test -race` for security, dbmanager, common, restheadspec, resolvespec, websocketspec, mqttspec, resolvemcp, funcspec. Known pre-existing failures: mqttspec integration (no DB).

## Risks
- Long tx if a hook does slow work inside it → hold connection longer; keep hooks fast.
- Pool of 1: nothing inside a tx may take a second pool connection (auth/security loads are outside; keep it so).
- Behavior change: After hooks no longer get the pool handle; hooks that relied on an independent connection break.
- websocket/mqtt long-lived connections: tx must be per message, never per connection.

## Done when
- `dbtrace` shows `pooled=0` for every handler op on a hooked model.
- RLS GUC set in `OnTxBegin` is visible to read, create, update, delete queries and hooks.
- No `Tx: h.db` / `hookCtx.Tx = h.db` left in spec handlers.
