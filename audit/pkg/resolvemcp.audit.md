# Audit: `pkg/resolvemcp`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/resolvemcp` |
| **Files** | `handler.go` (901), `tools.go` (720), `cursor.go`, `oauth2.go`, `oauth2_server.go`, `annotation.go`, `hooks.go`, `security_hooks.go`, `context.go`, `resolvemcp.go` |
| **Tests** | `tools_test.go` (34), `tx_test.go` (207); `go test` passes. No hostile-input tests, no `-race` |
| **Audit date** | 2026-09-30 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging, agent usability |
| **Threat model** | hostile or confused MCP client (LLM agent, possibly prompt-injected); tool arguments are attacker-controlled |
| **Depth** | targeted (request path, security wiring; verified against source) |

## Summary

Every model registers 4 tools + 1 resource (`read_/create_/update_/delete_<schema>_<entity>`), each with an
inlined column list, relation list and schema doc. Tool list grows 4N; context cost is
paid on every session whether or not the table is used. Replace with fixed meta tools
(see Rewrite).

Security wiring fails open in several places: model rules never reach the hooks,
`create` has no rule check, `update` skips `BeforeHandle`, update/delete skip row-level
security, and create/update write client-chosen column names. Reads have no size cap.
`resolvespec_annotate` is an unauthenticated write channel into agent-visible text.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **High** | security | Model rules set via `RegisterModelWithRules` never reach `security.Check*`: handler uses a private registry that is not `modelregistry.AddRegistry`'d; hooks look up the global list |
| 2 | **High** | security | `create` has no rule check: `CheckModelAuthAllowed` only tests `CanPublicCreate`/auth; no `BeforeCreate` hook registered, `CanCreate` never read |
| 3 | **High** | security | `executeUpdate` never fires `BeforeHandle` (create/read/delete do); auth + public-rule check skipped, only `BeforeUpdate` (`CanUpdate`) runs |
| 4 | **High** | security | Create/update data keys are not validated against model columns (`q.Value(key,…)`, `SetMap(existingMap)`): mass assignment of any column, arbitrary identifiers |
| 5 | **High** | security | Row-level security (`ApplyRowSecurity`) is wired to `BeforeRead` only; update/delete by id bypass app-level RLS (DB-level RLS via `OnTxBegin` still applies) |
| 6 | **High** | security | `resolvespec_annotate` has no auth/rule check, writes through `h.db` (outside tx, no `OnTxBegin`), any `tool_name` key; annotations are agent-facing text, so it is a prompt-injection store |
| 7 | **High** | slowness | No default/max `limit`, no max `offset`, `COUNT(*)` on every read, `[]` batch create unbounded, no statement timeout |
| 8 | **Medium** | security | `dynamicSSEHandler.pool` keyed by `Host` + `X-Forwarded-Proto` (attacker-controlled): unbounded map growth and poisoned `message` endpoint URL sent to the client |
| 9 | **Medium** | security / logging | Raw `err.Error()` (DB errors, hook errors, panic value `"internal error: %s"`) returned as tool text; `logger.Error` of the same forwards to Sentry (X8) |
| 10 | **Medium** | correctness | Update reads row, merges **json-tag keys** into `SetMap` as column names, writes every column back; breaks when json tag ≠ db column, clobbers concurrent edits (no `FOR UPDATE`) |
| 11 | **Medium** | correctness | Update ignores `nil` and `""` values: a column cannot be set to NULL or empty |
| 12 | **Medium** | correctness | Create/update commit tx 1, then run tx 2 (refetch + `AfterCreate`). Tx 2 failure returns an error for a committed write; an agent retry duplicates the insert |
| 13 | **Medium** | security | Preload relation names are passed straight to `PreloadRelation` without checking the model's relations; no depth/breadth cap |
| 14 | **Low** | security | Update/delete distinguish `record not found` from hook errors, so ids can be enumerated by error text |
| 15 | **Medium** | locking | `HookRegistry.hooks` map unsynchronized; `Register`/`Clear*` race with `Execute` (same as funcspec #9) |
| 16 | **Low** | security | Filter columns are validated by `ColumnValidator` for reads only; sort/column values are interpolated unquoted after validation (relies on validator being exact); `CustomOperators`/`ComputedColumns` unreachable from tools today, keep it that way |
| 17 | **Low** | panic | `recoverPanic` returns the panic value to the client and loses the stack; hook panics in `Execute` are not recovered before the handler-level recover |
| 18 | **Low** | agent usability | Tool names embed schema+entity (`read_public_users`); no discovery tool, so clients cannot list tables without loading every tool schema |
| 19 | **Info** | testing | No tests for auth/rule enforcement, hostile filters, key validation, limits, or `-race` |

## Details

### 1. Rules invisible to hooks (High)
`NewHandlerWithGORM/Bun/DB` call `modelregistry.NewModelRegistry()`. `security` resolves rules
via `GetModelRulesFromContext` then `modelregistry.GetModelRulesByName`, which walks the
**global** list (`registries`). The handler registry is never added, so
`ErrModelNotFound` → `CheckModelUpdate/DeleteAllowed` return `nil` (allow) and
`CheckModelAuthAllowed` falls back to "auth required, public flags ignored".
`CanUpdate=false`, `CanDelete=false` are not enforced. Fix: put rules into the
request context in `withRequestData` (`security.ModelRulesKey`) and/or `AddRegistry` on
construction.

### 2-3. Create/update gating (High)
`CheckModelAuthAllowed(op)` handles public flags only. Add `BeforeCreate` →
`CheckModelCreateAllowed` (new, mirrors update/delete), and call `BeforeHandle` at the
top of `executeUpdate`.

### 4. Column allowlist (High)
Validate every key in create/update `data` against `common.NewColumnValidator(model)`;
reject unknown keys with an error (do not silently drop on writes). Also consider a
per-model writable-column set (excluding PK, `CanPublic*`-guarded columns) for agents.

### 5. RLS on writes (High)
Run `LoadSecurityRules` + a row predicate on the update/delete pre-read query; fail the
write when the row is not visible to the user.

### 6. Annotation tool (High)
Remove from default registration or gate behind `BeforeHandle` + explicit rule. Values
returned to the agent must be treated as data, not instructions.

### 7. Limits (High)
Server config: `DefaultLimit` (e.g. 50), `MaxLimit`, `MaxOffset`, `MaxBatch`, `MaxPreloadDepth`,
per-call `context.WithTimeout`. Skip `COUNT(*)` unless requested (`with_count`).

### 8. SSE pool (Medium)
Require `Config.BaseURL` for SSE, or cap/evict `pool`, and validate `Host` against an
allowlist.

### 9/17. Error surface (Medium/Low)
Map errors to stable codes + short message; log details server-side with stack
(`logger.HandlePanic`).

### 10-12. Update/create semantics (Medium)
Build the `SET` only from validated incoming keys (column names resolved from model,
not json tags); use `NULL` for explicit null; single tx including refetch and `After*`
hooks (see `audit/single_tran.md`), or return success + warning when tx 2 fails.

## Rewrite (agreed design)

Replace per-model tools with fixed meta tools. Decisions recorded 2026-09-30:

| Decision | Choice |
|---|---|
| Functions source | Explicit registry: `Handler.RegisterFunction(name, meta, fn)`; only registered functions visible/callable |
| Old tools/resources | Removed (breaking) |
| Discovery | `list_tables`, `describe_table`, `list_functions` |
| Create | `insert_into_table` added |
| Write scope | update/delete by id **or** filters; max-rows cap, `dry_run` and confirm token apply to filter writes; id writes are single-row, no token |
| Guardrails | require id/filter, max rows affected, `dry_run`, confirm token |
| Read limits / ACL | server caps (limit, offset, preload depth); list tools filtered per caller rules |
| Identity | Authenticated caller's `UserContext`; no fixed MCP user. Endpoint guarded by OAuth / session token / API key (new `resolvespec_login_api_key`); no guest mode |
| Annotations | `resolvespec_annotate` becomes opt-in (`Config.EnableAnnotations`) and goes through `BeforeHandle` |

| Tool | Purpose |
|---|---|
| `list_tables` | registered `schema.entity` visible to caller, with allowed ops |
| `describe_table` | columns, PK, relations, writable columns, rules, limits for one table |
| `select_table` | filters/sort/columns/preloads/cursor; capped |
| `insert_into_table` | one or batch (capped); column allowlist |
| `update_table` | validated keys; guardrails |
| `delete_from_table` | guardrails |
| `list_functions` | registered functions + parameter schemas |
| `call_function` | validated args, tx + hooks |
