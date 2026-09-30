# resolvemcp rewrite plan

Source: `audit/pkg/resolvemcp.audit.md`. Status: plan only, no code changed.

## Goal

Replace 4 tools + 1 resource per model with a fixed set of meta tools.
Endpoint guarded by OAuth / session token / API key; tools run as the authenticated caller.
Same rules as resolvespec CRUD, plus guardrails.

## Decisions

| Topic | Decision |
|---|---|
| Tools | Fixed meta tools; per-model tools/resources removed (breaking) |
| Functions | Explicit registry `Handler.RegisterFunction`; two kinds: Go callback (`func(ctx, tx, args)` + JSON-schema params) and SQL procedure by name (declared params); both behind `call_function`, run in tx with hooks |
| Create | `insert_into_table` included |
| Writes | update/delete by id **or** filters |
| Guardrails | require id or filters, max rows, `dry_run`, confirm token |
| Token scope | filter writes only; id writes = single row, no token |
| Confirm token store | in-memory, TTL, bound to user/table/filter hash; lost on restart, single instance |
| Read limits | server caps: limit, offset, batch, preload depth, timeout |
| Model exposure | all registered models visible; rules only restrict operations |
| Visibility | list tools show only what the caller may do (rules) |
| Identity | the authenticated caller's `UserContext`; **no fixed MCP user, no `SetUsername`, no service session, no background refresh** |
| Guard | endpoint always requires one of: OAuth bearer, session token, API key; no guest/optional mode |
| API key login | new `DatabaseAuthenticator.LoginWithAPIKey(ctx, rawKey)` + procedure `resolvespec_login_api_key`; validates key via keystore, creates session, returns `LoginResponse` |
| Session SQL | procedure mode + direct-SQL fallback (`ShouldUseProcedure`), same as `Login` |
| OAuth routes | `oauth2.go`/`oauth2_server.go` kept, part of the guard |
| Annotations | opt-in `Config.EnableAnnotations`, via `BeforeHandle` |

## Open

- None.

## Tools

| Tool | Purpose |
|---|---|
| `list_tables` | visible `schema.entity` + allowed ops |
| `describe_table` | columns, PK, relations, writable columns, rules, limits |
| `select_table` | filters, sort, columns, preloads, cursor; capped |
| `insert_into_table` | one or batch (capped); column allowlist |
| `update_table` | validated keys; id or filters; guardrails |
| `delete_from_table` | id or filters; guardrails |
| `list_functions` | registered functions + parameter schemas |
| `call_function` | validated args; tx + hooks + rules |

## Config additions

| Field | Purpose |
|---|---|
| `DefaultLimit`, `MaxLimit`, `MaxOffset` | read paging caps |
| `MaxBatch` | insert batch cap |
| `MaxPreloadDepth` | preload cap |
| `MaxWriteRows` | filter-write row cap |
| `QueryTimeout` | per-call context timeout |
| `ConfirmTTL` | confirm token lifetime |
| `EnableAnnotations` | opt-in annotate tool |

## Guardrail rules

| Rule | Behaviour |
|---|---|
| Target required | update/delete with neither id nor filters rejected |
| Max rows | count matches inside tx; abort above `MaxWriteRows` |
| `dry_run` | returns match count + preview, no write |
| Confirm token | filter write: first call returns token + preview; second call with token executes; bound to user, table, filter hash; expires at `ConfirmTTL` |
| Id write | single row, no token |

## Work items

### 1. API key login (`pkg/security`)
- Existing: keystore has `ValidateKey` and `KeyStoreAuthenticator`; `Login` needs a password; no key-to-session path.
- Add `resolvespec_login_api_key` to `SQLNames` (default + override) and a SQL script beside the existing procedures. Contract: `p_success, p_error, p_data`, input raw key; hashes, validates active/non-expired key, creates session for the key's user.
- Add `DatabaseAuthenticator.LoginWithAPIKey(ctx, rawKey)`; procedure first, direct-SQL fallback via `ShouldUseProcedure`.
- Hashed lookup; same generic error for unknown, expired or inactive key; no key material in logs.
- Expose through the chain/composite authenticators so the middleware can accept it.

### 2. Endpoint guard
- Wire `security.NewAuthMiddleware` with a chain of OAuth bearer, session token (header/cookie) and API key.
- `SetupMux*`/`SetupBunRouter*` helpers require the guard; unauthenticated serving only when explicitly constructed without it, logged loudly. Remove `OptionalAuth*` from the MCP path.
- Caller `UserContext` flows to every tool call context; rules, RLS and `OnTxBegin` apply to that user.

### 3. Security fixes (audit #1-6)
- Put model rules in request context (`withRequestData`) and/or `AddRegistry` on construction.
- Add `BeforeCreate` -> `CheckModelCreateAllowed` (new in `pkg/security`).
- Call `BeforeHandle` first in `executeUpdate`.
- Validate create/update keys against `ColumnValidator`; reject unknown; resolve column names from model, not json tags.
- Apply row security to update/delete pre-read; fail if row not visible.
- Annotate tool: opt-in + `BeforeHandle`.

### 4. Limits (audit #7, #13)
- Apply default/max limit, max offset, batch cap, preload depth cap, timeout.
- Validate preload names against model relations.
- Count only when requested.

### 5. Error and panic surface (audit #9, #17)
- Stable error codes + short message to client.
- Details and stack logged server-side.
- Recover hook panics.

### 6. Update/create semantics (audit #10-12)
- `SET` from validated incoming keys only; explicit null supported.
- Lock row (`FOR UPDATE`) on update.
- Refetch and `After*` hooks inside the same tx, or report committed write with warning if not possible (see `audit/single_tran.md`).

### 7. Smaller fixes (audit #8, #14, #15)
- SSE pool: require `BaseURL` or cap/evict; allowlist Host.
- Uniform not-found vs hook error text.
- Add mutex to `HookRegistry`.

### 8. Meta tools
- New file for meta tools; reuse parse helpers and `buildModelInfo` for `describe_table`.
- Remove per-model register functions and resources.
- `RegisterModel` only registers to registry.
- Function registry (Go callback kind + SQL procedure kind) + validation of args against declared schema.

### 9. Tests
- Update `tx_test.go` (calls `executeRead/Create/Update/Delete`) and `tools_test.go`.
- New: rule enforcement, unknown keys, limits, guardrails (cap, dry_run, token expiry/binding), guard rejects unauthenticated, API key login (valid, expired, inactive, unknown), visibility filtering, `-race`.
- Check for existing test data first; ask before generating any.

### 10. Docs
- Rewrite `pkg/resolvemcp/README.md` cheatsheet style.
- Document `resolvespec_login_api_key` in `pkg/security` docs.
- Update root README references.
- Update audit file when findings are closed.

## Order

1. `LoginWithAPIKey` + procedure in `pkg/security` (1)
2. Guard + security fixes (2-3)
3. Limits, errors, update/create semantics (4-6)
4. Meta tools + function registry (8)
5. Smaller fixes (7)
6. Tests (9), docs (10)

## Breaking changes

- Per-model tools and resources gone.
- MCP endpoint requires authentication.
- Annotate tool off by default.
- Update/create reject unknown keys.
- Reads capped by default.
