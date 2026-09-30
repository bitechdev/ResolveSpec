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
| # | Change | Files | Notes |
|---|---|---|---|
| 0 | Baseline: enable `dbtrace` on testserver, record `tx/tx_queries/pooled/raw` per op | `cmd/testserver`, `pkg/dbtrace` | pooled > 0 on write ops = the gaps above |
| 1 | Delete in one tx (single + batch, per-item hooks inside tx) | `resolvespec/handler.go`, `restheadspec/handler.go` | fixes 2 pool connections + race + RLS |
| 2 | `OnTxBegin` hook type + `runInTx` helper | `*/hooks.go`, `*/handler.go` | resolvespec + restheadspec first |
| 3 | Insert/update post-commit hooks + re-fetch in second short `runInTx` (select only) | restheadspec `:1005, 1467, 1667-1674`; resolvespec `:1297, 1449, 1602` | per decision above |
| 4 | websocketspec + mqttspec: wrap read/create/update/delete in `runInTx` | `websocketspec/handler.go`, `mqttspec/handler.go` | mqttspec aliases websocketspec hooks; confirm `OnTxBegin` alias |
| 5 | resolvemcp: read + single create in tx | `resolvemcp/handler.go:253, 445` | verify batch/update/delete hooks run inside tx |
| 6 | funcspec: `OnTxBegin` (or once-per-tx `BeforeOp`), `BeforeResponse` via `runInTx` | `funcspec/function_api.go:337, 640` | |
| 7 | Security hooks: register RLS stamping on `OnTxBegin`; document | `pkg/security/*`, README | |

## Tests
- Existing: per-spec `handler_test.go`, `hooks_test.go`, `integration_test.go`; models in `pkg/testmodels/business.go`; `dbtrace` unit tests.
- Missing: any test asserting hook `Tx` is a tx, or counting connections per op.
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
