# pkg/security lookup sub package plan

Status: plan only, no code changed. Related: `audit/mcp_plan.md` (work item 1, API key login).

## Problem

- `pkg/security` mixes two data-access styles: stored procedures (`resolvespec_*`) and ~1,800 lines of hand-written
  "direct" SQL (`*_direct.go`) selected per call by `QueryMode` / `ShouldUseProcedure`.
- Direct SQL is written once with `?` placeholders and only rewritten for Postgres. It assumes one fixed schema
  (table and column names, JSON stored as TEXT, bool/time handling), and is only really exercised on SQLite.
- Table names are configurable (`TableNames`), column names are not. Procedure names are configurable (`SQLNames`).
- Postgres cannot be run "tables only" in a first-class way, and other databases have no defined support.
- Rule going forward: **`pkg/security` itself contains no SQL.** All lookups go through one sub package.

## Goal

New sub package `pkg/security/lookup` that owns every database read/write the security package needs.

| Requirement | Decision |
|---|---|
| Postgres default | Existing stored procedures, existing names, unchanged behaviour out of the box |
| Postgres direct | Optional: work on tables directly with no procs installed |
| SQLite | First-class: configurable tables and columns |
| Other DBs | MySQL/MariaDB and MSSQL via dialects; adding more = adding a dialect |
| Config | Procedure names, table names, column names, mode (procedure / direct / auto), per backend |
| `pkg/security` | Calls lookup interfaces only; no `SELECT`/`INSERT`/`UPDATE`/`DELETE`, no `pg_proc` probing |

## Design

### Package layout

```
pkg/security/            # core: behaviour interfaces, SecurityList, middleware, chain, composite, hooks, write security, tx settings, type aliases
pkg/security/sectypes/   # shared data types (no deps, no SQL, no logic beyond small helpers)
pkg/security/providers/  # concrete authenticators + security providers (see Package split)
pkg/security/oauth/      # OAuth2 client login + OAuth2 authorization server
pkg/security/totp/       # two-factor: generator, providers, TwoFactorAuthenticator
pkg/security/passkey/    # WebAuthn passkey provider + passkey login flow
pkg/security/lookup/
  lookup.go        # store interfaces + record types + Config + New(db, cfg)
  schema.go        # Schema: table + column names per entity, defaults, merge, validate
  mode.go          # Mode (Auto/Procedure/Direct), per-operation resolution, proc probe (pg only)
  dialect/         # Dialect interface + postgres, sqlite, mysql, mssql
  procedure/       # procedure backend (current SQLNames, p_success/p_error/p_data contract)
  direct/          # dialect-driven SQL backend (no fixed SQL strings per dialect)
  ddl/             # reference schemas per dialect (replaces database_schema*.sql variants)
```

### Shared types: `pkg/security/sectypes`

`lookup` cannot import `pkg/security` (cycle: security -> lookup -> security), so the plain data types move to a
dependency-free sub package that both import.

- Moves to `sectypes`: `UserContext`, `LoginRequest`, `LoginResponse`, `RegisterRequest`, `LogoutRequest`,
  `PasswordResetRequest/Response/CompleteRequest`, `KeyType`, `UserKey`, `CreateKeyRequest/Response`,
  `PasskeyCredential` (+ passkey request/option structs the stores return), `TwoFactorSecret`, OAuth server
  client/code/token-info structs (`OAuthServerClient`, `OAuthCode`, `OAuthTokenInfo`), `ColumnSecurity`, `RowSecurity`.
- Stays in `pkg/security`: all behaviour interfaces (`Authenticator`, `SecurityProvider`, `Registrable`, ...),
  authenticators, middleware, `OAuthServer`, TOTP generator, hooks. They reference `sectypes` types.
- Compatibility: `pkg/security` re-exports each moved type as an alias (`type UserContext = sectypes.UserContext`) and
  the `KeyType*` constants, so `security.UserContext` etc. keep compiling and are identical types. In-repo users
  (eventbroker, funcspec, mqttspec, resolvemcp, resolvespec, restheadspec, websocketspec, ~17 files) need no edits.
- `lookup` stores take and return `sectypes` types directly; no separate record types and no conversion layer.
- Rules: `sectypes` imports only the standard library (and `oauth2` types only if unavoidable, else a local struct);
  JSON tags unchanged so wire formats and procedure `p_data` payloads stay identical.

### Package split

Dependency direction (no cycles): `sectypes` <- `lookup` <- `providers`/`oauth`/`totp`/`passkey` -> `security` (core) -> `sectypes`.
Core `security` never imports the sub packages. Sub packages do not import each other (see Dependency rules).

### Dependency rules (how cycles are avoided)

1. **Layers, imports only point down.** L0 `sectypes` (stdlib only) -> L1 `lookup`, core `security` interfaces ->
   L2 `providers`, `oauth`, `totp`, `passkey`. A package may import lower layers, never its own layer or above.
2. **Define interfaces where they are consumed, not where they are implemented** (Go idiom). E.g. `oauth` declares
   the small `SessionCreator` it needs; `providers.DatabaseAuthenticator` satisfies it without `oauth` importing `providers`.
3. **Shared data goes down, not sideways.** If two L2 packages need the same struct, it moves to `sectypes`
   (or a tiny `internal/` package), never "A imports B for one type".
4. **No L2 -> L2 imports.** Composition happens in the application (or an optional top-level `security/setup`
   package that imports everything and is imported by nobody in `pkg/security`).
5. **Dependency injection by constructor**, passing interfaces/stores (`lookup.Provider`, `security.Authenticator`);
   no package-level registries that need a back-import; use functional options for optional collaborators.
6. **Core never imports concrete implementations**; where core needs behaviour it calls an interface it owns
   (hooks, `SecurityContext`, `Authenticator`).
7. **Tests:** external test packages (`package foo_test`) for cross-package integration tests, so test-only
   imports cannot create cycles; shared fixtures in an `internal/testutil` package.
8. **Guard in CI:** `go list -deps` / a small test that asserts the layer rules (e.g. `sectypes` imports only stdlib,
   `lookup` does not import `security`, no L2 package imports another L2 package). `go build` already rejects true cycles.

| Package | Contents (from today's files) |
|---|---|
| `security` (core) | `Authenticator`, `SecurityProvider`, `Registrable`, `Refreshable`, `APIKeyLoginable`, ... interfaces; `SecurityList`; `SecurityContext`; middleware + cookie options; `ChainAuthenticator`; `CompositeSecurityProvider`; hooks; `WriteDataContext`; `TxSettings`; type aliases to `sectypes` |
| `providers` | `DatabaseAuthenticator`, `JWTAuthenticator`, `HeaderAuthenticator`, `KeyStoreAuthenticator`, `ConfigKeyStore`, `DatabaseKeyStore`, `DatabaseColumnSecurityProvider`, `DatabaseRowSecurityProvider`, `Config*SecurityProvider` |
| `oauth` | `OAuth2Config`, `OAuth2Provider`, Google/GitHub/Microsoft/Facebook/multi-provider constructors, OAuth2 refresh, `OAuthServer` + `OAuthServerConfig`, oauth server persistence (via `lookup.OAuthClientStore`) |
| `passkey` | `PasskeyProvider` impl (`DatabasePasskeyProvider`), registration/authentication flows, passkey request/option types that are not shared (shared ones stay in `sectypes`) |
| `totp` | `TwoFactorAuthProvider`, `TwoFactorConfig`, `TOTPGenerator`, `MemoryTwoFactorProvider`, `DatabaseTwoFactorProvider`, `TwoFactorAuthenticator` |

Consequences to design for:
- **Methods cannot span packages.** Today OAuth2 and passkey logic are methods on `DatabaseAuthenticator`
  (`oauth2_methods*.go`, `oauth_server_db*.go`, passkey methods) and `NewOAuthServer` takes `*DatabaseAuthenticator`.
  They become standalone types in `oauth` / `providers` that depend on `lookup` stores and on small interfaces
  (e.g. `oauth.SessionCreator`) instead of the concrete authenticator. `NewGoogleAuthenticator(...)` etc. return a
  `providers.DatabaseAuthenticator` configured with an `oauth.Provider`, or an `oauth.Authenticator` that implements
  `security.Authenticator`; pick one in step 5 (see Open).
- **Constructors cannot be re-exported from core `security`** (it would import the sub packages = cycle). Types that
  move to `sectypes` keep aliases; constructors and concrete types do not. This is a breaking import change.
  In-repo callers affected (outside `pkg/security`): `pkg/resolvemcp` (`oauth2.go`, `oauth2_server.go`, `handler.go`),
  `pkg/middleware/clientqueue.go`, docs and examples. Provide a mechanical migration table
  (`security.NewDatabaseAuthenticator` -> `providers.NewDatabaseAuthenticator`, `security.OAuthServer` -> `oauth.Server`, ...).
- **Interfaces core needs from sub packages** (e.g. 2FA hook points) are defined in core or `sectypes`, implemented in
  `totp`; core never imports `totp`.
- `examples*.go` / `oauth2_examples.go` / `passkey_examples.go` move next to the package they exemplify (or to
  `_example_test.go` files) so core has no dependency on them.
- Tests move with their code; shared helpers (sqlite test DB, `authenticatedRequest`) go to an internal test helper package.

### Store interfaces (one per domain, mirrors current procs)

| Store | Operations (current proc in brackets) |
|---|---|
| `AuthStore` | `Login` [login], `Register` [register], `Logout` [logout], `Session` [session], `TouchSession` [session_update], `Refresh` [refresh_token], `LoginAPIKey` [login_api_key], `JWTLogin`, `JWTLogout`, `ResetRequest`, `ResetComplete` |
| `KeyStore` | `Create`, `List`, `Delete`, `Validate` [keystore_*] |
| `OAuthClientStore` | register client, get client, save code, exchange code, introspect, revoke |
| `OAuthUserStore` | get-or-create user, create session, get/update refresh token, get user |
| `PasskeyStore` | store, get, update counter, list, delete, rename, get by username, login |
| `TOTPStore` | enable, disable, status, secret, regenerate backup codes, validate backup code |
| `PolicyStore` | column security, row security: procedure backend (default) + direct backend over the `sec_*` table layout below |

Each store has a procedure implementation and a direct implementation. A `Provider` bundles them; `security`
constructors take a `lookup.Provider` (or build one from `db` + `lookup.Config`, so existing constructors keep working).

### Config

```go
type Config struct {
    Dialect  string          // "postgres" | "sqlite" | "mysql" | "mssql"; empty = detect from driver
    Mode     Mode            // Auto | Procedure | Direct; default: Procedure for postgres, Direct otherwise
    Overrides map[Op]Mode    // optional per-operation mode, e.g. direct for Session, procedure for Login
    Procs    ProcNames       // = today's SQLNames (+ LoginAPIKey), defaults unchanged
    Schema   Schema          // tables + columns, see below
}
```

- `Schema` = per entity `{Table string; Columns map[Column]string}` with typed column keys covering every column
  (`users.id`, `users.username`, `users.password`, `users.is_active`, ...). Defaults reproduce the current schema,
  so zero config behaves as today. Optional `Schema` name per entity for `schema.table` qualification.
- Merge + validation as today: non-empty override wins; every identifier checked against `^[a-zA-Z_][a-zA-Z0-9_]*$`
  (plus optional single `schema.` prefix). Identifiers are quoted by the dialect, never interpolated raw.
- No back-compat for config: `SQLNames`, `KeyStoreSQLNames`, `TableNames`, `KeyStoreTableNames` and `QueryMode` are
  removed from `pkg/security`; `lookup.Config` replaces them (decision 8).

### Dialect interface (the per-database adaptor)

One adaptor per database type (`dialect/postgres`, `sqlite`, `mysql`, `mssql`), selected by `Config.Dialect` or
detected from the driver, registered through `dialect.Register(name, factory)` so more databases can be added later
without touching core code. Each adaptor supplies only the things that differ; the direct backend builds queries from it.

| Concern | Dialect method |
|---|---|
| Placeholders | `Placeholder(n)` (`$n`, `?`, `@pn`) |
| Identifier quoting | `Quote(ident)` (`"x"`, `` `x` ``, `[x]`) |
| Booleans | `Bool(v)` / scan helper (bool vs 0/1) |
| Time | `Now()` expr or Go-side `time.Now()`; scan helper for drivers returning strings |
| Insert returning id | `InsertReturningID(table, cols, idCol)` returns the SQL + scan strategy: postgres `... RETURNING id` (QueryRow), sqlite/mysql `LastInsertId`, mssql `... OUTPUT INSERTED.id` (QueryRow). The only dialect-specific write construct (decision 12 / confirmed) |
| Get-or-create | none; standard SQL select-then-insert inside a tx (no upsert) |
| Limit/top | only if a query needs it |
| JSON columns (scopes/meta/roles) | `EncodeJSON` / `DecodeJSON` (native jsonb vs TEXT) |
| Random / hashing | done in Go (token generation, SHA-256 key hash, bcrypt) so direct mode needs no `pgcrypto` and no DB functions |
| Driver detection | `Detect(*sql.DB)` from driver type (replaces `driverIsPostgres` / `driverIsPortableOnly`) |

Queries are assembled by a small internal builder (select/insert/update/delete with named columns from `Schema`),
not string-concatenated per dialect and not via an ORM, to keep `pkg/security` free of bun/gorm.

### PolicyStore table layout (column / row security, direct backend)

Approved layout. Both the procedure backend (`resolvespec_column_security` / `resolvespec_row_security`, rewritten
in `database_schema.sql`) and the direct backend read these tables; the former external schema is no longer
referenced anywhere in the repo. All table and column names are configurable via `Schema`, defaults shown.

`sec_group_members` (optional; omit to use direct user rules only)

| Column | Type | Notes |
|---|---|---|
| `group_id` | int, not null | group a user belongs to |
| `user_id` | int, not null | FK users.id; PK (`group_id`, `user_id`) |

`sec_column_rules`

| Column | Type | Notes |
|---|---|---|
| `id` | int PK | |
| `user_id` | int null | rule for one user |
| `group_id` | int null | rule for every member of the group; exactly one of `user_id` / `group_id` set (check constraint) |
| `schema_name` | text, not null | matched case-insensitively |
| `table_name` | text, not null | matched case-insensitively |
| `column_path` | text, not null | dot path under the table (`col` or `col.sub.field`) = `ColumnSecurity.Path` joined by `.` |
| `access_type` | text, not null | `ColumnSecurity.Accesstype` (e.g. `mask`, `hide`, `read`) |
| `mask_start`, `mask_end` | int null | default 0 |
| `mask_invert` | bool null | default false |
| `mask_char` | text null | default `*` |
| `extra_filters` | text/JSON null | `ExtraFilters` map, JSON-encoded via dialect `EncodeJSON` |
| `is_active` | bool, not null | default true |

`sec_row_rules`

| Column | Type | Notes |
|---|---|---|
| `id` | int PK | |
| `user_id` | int null / `group_id` int null | as above, exactly one set |
| `schema_name`, `table_name` | text, not null | case-insensitive match |
| `template` | text null | SQL fragment with the existing placeholders (`RowSecurity.Template`) |
| `has_block` | bool, not null | default false; true = no rows visible (`RowSecurity.HasBlock`) |
| `is_active` | bool, not null | default true |

Resolution rules (direct backend; also the contract the conformance tests assert):
- Applicable rules = active rules where `user_id` = caller, plus rules of every group the caller belongs to.
- Column security: all applicable rules for the exact schema + table, returned as `[]ColumnSecurity` (union).
  Exact table match, not a prefix match (a prefix would match `users_archive` for `users`).
- Row security: any applicable `has_block` wins; otherwise templates of all applicable rules are combined with
  `AND` (each wrapped in parentheses); no rule = `RowSecurity{}` with `ErrNoRowSecurity` semantics unchanged.
- Templates are still validated/substituted by the existing safe-identifier code in core; the store only loads text.
- Both loaders keep the guarantees of today: user reference reduced to a scalar, failures are errors (fail closed),
  no rule is "no rules".

### Mode resolution

- `Procedure`: always call the proc; missing proc = error (no silent fallback).
- `Direct`: always use tables via the dialect builder.
- `Auto`: Postgres probes `pg_proc` once per proc (cached, as today); other dialects resolve to `Direct`.
  Probe lives in `lookup` and is the only place that queries the catalog.
- Postgres default stays `Procedure` so current installs do not change behaviour.
- Roles/user-level safety rules already enforced in direct mode (Register ignores client-supplied level/roles, bcrypt
  hash, opt-in password upgrade) become backend-agnostic tests that both backends must pass.

## Work items

### 0. Extract shared types (prerequisite, no behaviour change)
- Create `pkg/security/sectypes`, move the types listed above, add aliases in `pkg/security`.
- Verify `go build ./...` and `go test ./pkg/...` unchanged; `go vet` for alias/import cycles.
- Do this first and on its own so the diff is a pure move.

### 0b. Package split (after 0, before `lookup` wiring)
- Create `providers`, `oauth`, `totp`; move files per the Package split table, one package per commit:
  `totp` and `passkey` (self-contained) -> `providers` (key stores, authenticators, policy providers) -> `oauth` (needs de-methoding
  from `DatabaseAuthenticator`).
- Break the method-on-`DatabaseAuthenticator` coupling for OAuth2 and passkey first (extract interfaces), then move.
- Update in-repo callers and docs; add migration table to `pkg/security/README.md`.
- Behaviour unchanged; at this point stores are still the old direct/proc code, only relocated.

### 1. Skeleton and contracts
- Create `lookup` package: records, store interfaces, `Config`, `Schema` (defaults, merge, validate), `Mode`.
- No behaviour change yet; compile-only.

### 2. Dialects
- Implement `postgres`, `sqlite`, `mysql`, `mssql` against the dialect interface; driver detection.
- Unit tests per dialect: placeholders, quoting, bool/time round trip, insert-returning-id.

### 3. Procedure backend
- Move existing proc calls out of `pkg/security` into `lookup/procedure` using `ProcNames` (current defaults).
- Keep the `p_success, p_error, p_data` contracts and reconnect-on-closed-DB helper.
- Include `resolvespec_login_api_key` (added in mcp_plan item 1) with the generic error behaviour.

### 4. Direct backend
- Port each `*_direct.go` to `lookup/direct` using `Schema` + dialect builder, one store at a time:
  AuthStore -> KeyStore -> OAuth stores -> Passkey -> TOTP -> PolicyStore (column/row security tables).
- Add direct `LoginAPIKey` here (select by key hash, active, unexpired, key type in header_api/api, user active;
  one generic error), since SQL is now allowed only inside `lookup`.
- Transactions: multi-step writes (login = session insert + last_login; register; reset complete) run in one tx.

### 5. Wire `pkg/security`
- Constructors accept `lookup.Provider` / `lookup.Config`; old options map onto it (deprecated).
- Replace every `*_direct.go` call and `ShouldUseProcedure` branch with a store call.
- Delete `*_direct.go`, `query_mode.go` probe/placeholder code, direct `TableNames` use; keep only aliases.
- Check no non-test code in `pkg/security` contains SQL keywords (CI grep guard).

### 5b. `pkg/security/breaking_changes.md`
- Create at step 0 and append as each step lands: moved types (aliased, no action), moved constructors/types
  with rename table (old -> new import path and symbol), removed config types (`SQLNames`, `TableNames`,
  `KeyStore*Names`, `QueryMode`) with the `lookup.Config` replacement, removed `database_schema_sqlite.sql`,
  `ModeAuto` behaviour change, API key login procedure.

### 6. Schemas and docs
- `lookup/ddl`: reference DDL for postgres (tables only, no procs), sqlite, mysql, mssql; existing proc scripts
  stay beside the procedure backend. Replace `database_schema_sqlite.sql`.
- Document: default (procs), Postgres tables-only, SQLite, custom column mapping, adding a dialect.
- Update `pkg/security` README/QUICK_REFERENCE; note API key procedure in security docs (mcp_plan item 10).

### 7. Tests
- Shared conformance suite run against every backend/dialect: login/register/logout/session/refresh, reset,
  API keys (valid/expired/inactive/unknown/wrong type), keystore, OAuth server, passkey, TOTP, privilege rules.
- Backends covered: sqlite (in-memory, direct), postgres direct and postgres procedure (needs a Postgres instance,
  skipped without `RESOLVESPEC_TEST_PG_DSN`), mysql/mssql behind env DSNs. Dialect unit tests need no DB.
- Procedure backend unit tests with sqlmock for the call/contract shape.
- Check for existing test data before creating any; ask before generating.
- Run with `-race`; migrate current `direct_mode_test.go` / `query_mode_test.go` cases into the suite.

## Order

0. Extract `sectypes` types + aliases (0)
0b. Package split: `totp`, `passkey` -> `providers` -> `oauth` (0b), callers + docs updated
1. Skeleton + Schema/Config (1)
2. Dialects (2)
3. Procedure backend extraction, `pkg/security` wired to it for procs only (3, part of 5) - zero behaviour change
4. Direct backend per store, then remove old `*_direct.go` as each store lands (4, 5)
5. DDL + docs + conformance suite (6, 7)
6. Resume `audit/mcp_plan.md` step 2 (guard) on top of `lookup`

## Breaking changes

- `QueryMode`, `SQLNames`, `TableNames`, `KeyStoreSQLNames`, `KeyStoreTableNames` removed; replaced by `lookup.Config`.
- `ModeAuto` no longer silently falls back from procedure to SQL on non-Postgres drivers without telling: resolution
  is explicit and logged once per op.
- `database_schema_sqlite.sql` replaced by `lookup/ddl`.
- Concrete types and constructors move to `providers`, `oauth`, `totp` (e.g. `security.NewDatabaseAuthenticator`
  -> `providers.NewDatabaseAuthenticator`, `security.NewOAuthServer` -> `oauth.NewServer`,
  `security.NewTOTPGenerator` -> `totp.NewGenerator`). No aliases possible (import cycle); import paths must change.
- Type identity is preserved via aliases; code that used reflection on the package path of these types
  (`security.UserContext` -> `sectypes.UserContext`) would see the new path (none found in-repo; re-check).
- Anything outside `lookup` that relied on SQL living in `pkg/security` (none found in-repo) must use the stores.

## Decisions

| # | Topic | Decision |
|---|---|---|
| 1 | Shared package name | `sectypes` |
| 2 | Type aliases in `security` | Kept permanently (public API) |
| 3 | `ColumnSecurity` / `RowSecurity` | Move to `sectypes` (types and their helper logic that has no outside deps) |
| 4 | Moved type names | Renamed for new paths (`oauth.Server`, `totp.Generator`, ...) |
| 5 | OAuth2 client login | Dedicated `oauth.Authenticator` type; `New{Google,GitHub,Microsoft,Facebook}Authenticator` return it |
| 6 | Migration | Rename table only; recorded in new `pkg/security/breaking_changes.md` |
| 7 | Dialects v1 | postgres, sqlite, mysql/mariadb, mssql; more later via the dialect interface |
| 8 | Deprecated config (`SQLNames`, `TableNames`, `KeyStore*Names`, `QueryMode`) | Removed, no aliases; recorded in `breaking_changes.md` |
| 9 | Column mapping | Every column of every entity configurable |
| 10 | DB handle | `*sql.DB` |
| 11 | Column / row security | Procedures (default) **and** a table layout for direct mode |
| 12 | Postgres direct SQL | Standard SQL only: no `ON CONFLICT` / `MERGE`; get-or-create = select then insert in a tx |
| 13 | Token format | Keep `sess_<hex>_<unix>`, generated in Go for direct mode |

## Open

- None.
