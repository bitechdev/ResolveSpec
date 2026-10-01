# Breaking changes

Appended as each step of `audit/sec_query_builder.plan.md` lands.

## Step 0: shared types moved to `sectypes` (no action needed)

The plain data types now live in `pkg/security/sectypes` and are aliased in `pkg/security`
(`types.go`), so `security.UserContext` and `sectypes.UserContext` are the same type.
Moved: `UserContext`, `LoginRequest/Response`, `RegisterRequest`, `LogoutRequest`,
`PasswordReset*`, `KeyType` (+ constants), `UserKey`, `CreateKey*`, `OAuthServerClient`,
`OAuthCode`, `OAuthTokenInfo`, `Passkey*` data structs, `TwoFactorSecret`, `ColumnSecurity`,
`RowSecurity` (incl. `GetTemplate`).

## Step 0b (totp): moved to `pkg/security/totp`

Import `github.com/bitechdev/ResolveSpec/pkg/security/totp`. No aliases (import cycle).

| Old | New |
|---|---|
| `security.TwoFactorAuthProvider` | `totp.AuthProvider` |
| `security.TwoFactorConfig` / `DefaultTwoFactorConfig` | `totp.Config` / `totp.DefaultConfig` |
| `security.TOTPGenerator` / `NewTOTPGenerator` | `totp.Generator` / `totp.NewGenerator` |
| `security.GenerateBackupCodes` | `totp.GenerateBackupCodes` |
| `security.MemoryTwoFactorProvider` / `NewMemoryTwoFactorProvider` | `totp.MemoryProvider` / `totp.NewMemoryProvider` |
| `security.TwoFactorAuthenticator` / `NewTwoFactorAuthenticator` | `totp.Authenticator` / `totp.NewAuthenticator` |

`totp.NewAuthenticator` takes a `totp.BaseAuthenticator` (Login, Logout, Authenticate) instead of
`security.Authenticator`; any `security.Authenticator` satisfies it.

`DatabaseTwoFactorProvider` stays in `security` (it now calls the lookup `TOTPStore`). Core imports
`totp`, so `totp` must not import `security`.

## Step 0b (providers, first part): moved to `pkg/security/providers`

Import `github.com/bitechdev/ResolveSpec/pkg/security/providers`. Names unchanged, no aliases (import cycle).

| Old | New |
|---|---|
| `security.HeaderAuthenticator` / `NewHeaderAuthenticator` | `providers.HeaderAuthenticator` / `providers.NewHeaderAuthenticator` |
| `security.ConfigKeyStore` / `NewConfigKeyStore` | `providers.ConfigKeyStore` / `providers.NewConfigKeyStore` |
| `security.KeyStoreAuthenticator` / `NewKeyStoreAuthenticator` | `providers.KeyStoreAuthenticator` / `providers.NewKeyStoreAuthenticator` |
| `security.ConfigColumnSecurityProvider` / `NewConfigColumnSecurityProvider` | `providers.ConfigColumnSecurityProvider` / `providers.NewConfigColumnSecurityProvider` |
| `security.ConfigRowSecurityProvider` / `NewConfigRowSecurityProvider` | `providers.ConfigRowSecurityProvider` / `providers.NewConfigRowSecurityProvider` |

The SHA-256 key hash helper is now `sectypes.HashKey`. The database-backed providers
(`DatabaseAuthenticator`, `JWTAuthenticator`, `DatabaseKeyStore`, `DatabaseColumn/RowSecurityProvider`)
stay in `security`; they call the lookup stores (see step 5).

## Additions (no action needed)

- `common.SQLDBProvider` (`SQLDB() *sql.DB`) is implemented by the bun, gorm and pgsql adapters
  (not their transaction adapters). `lookup.FromDatabase(common.Database)` uses it, plus the
  adapter's `DriverName()`, to get the `*sql.DB` and dialect name.

## Steps 2–3: lookup dialects and procedure backend

- New `lookup/dialect` (postgres, sqlite, mysql, mssql) and `lookup/procedure` packages. No
  existing exported `security` API changed in these steps.
- Procedure-mode code paths in `DatabaseAuthenticator`, `JWTAuthenticator`, the policy providers,
  `DatabaseKeyStore`, `DatabaseTwoFactorProvider` and `DatabasePasskeyProvider` now delegate to
  `lookup/procedure`. Error texts are unchanged.
- Behaviour change (improvement): these procedure paths now reconnect once on a closed `*sql.DB`
  (JWT logout, key create, TOTP, passkey, OAuth previously used the handle directly).

## Step 4: lookup/direct backend

- New `lookup/direct` package: table-backed stores for auth, keys, OAuth (client + user), passkey,
  TOTP and policy, built from `lookup.Schema` and the dialect. Nothing in `pkg/security` calls it
  yet (wiring happens in step 5), so no existing API changes here.
- Direct `LoginAPIKey` is new: `header_api` / `api` keys only; unknown, expired, inactive and
  wrong-type keys (and inactive users) all return `lookup.ErrInvalidAPIKey`.
- Policy tables (`sec_group_members`, `sec_column_rules`, `sec_row_rules`) are required for the
  direct policy store; `PolicyOptions.NoGroups` skips the membership table.
- Direct behaviour that changes when step 5 switches over: login, register, refresh, API-key login,
  password reset and passkey login now write in one transaction; `Keys.Create` stores NULL (not the
  text `null`) for empty scopes/meta; OAuth code exchange consumes the code atomically; a
  non-numeric row-security user reference is an error instead of loading no rules.

## Step 5: pkg/security uses lookup

`pkg/security` no longer contains SQL (guarded by `TestCoreContainsNoSQL`). Every database call goes
through a `lookup.Provider` built by `lookup/backends.New`.

Removed (replaced by `lookup.Config`: `Dialect`, `Mode`, `Overrides`, `Procs`, `Schema`):
- Types and functions `SQLNames`, `DefaultSQLNames`, `MergeSQLNames`, `ValidateSQLNames`,
  `TableNames` (+ Default/Merge/Validate), `KeyStoreSQLNames`, `KeyStoreTableNames` (+ same),
  `QueryMode`, `ModeAuto`/`ModeProcedure`/`ModeDirect`, `ErrDirectModeUnsupported`.
- Options fields `SQLNames`, `TableNames`, `QueryMode` on `DatabaseAuthenticatorOptions`,
  `DatabaseKeyStoreOptions`, `DatabasePasskeyProviderOptions`; replaced by `Lookup lookup.Config`
  and `LookupProvider *lookup.Provider`.
- Builders `WithQueryMode`, `WithTableNames` on `JWTAuthenticator`, the column/row providers and
  `DatabaseTwoFactorProvider`; replaced by `WithLookup(cfg)` and `WithLookupProvider(p)`.
- The variadic `names ...*SQLNames` argument of `NewJWTAuthenticator`,
  `NewDatabaseColumnSecurityProvider`, `NewDatabaseRowSecurityProvider` and
  `NewDatabaseTwoFactorProvider`.

Behaviour changes:
- Default mode is per dialect: stored procedures on Postgres, direct SQL elsewhere. `ModeAuto`
  (probe `pg_proc` once per procedure) is now opt-in via `lookup.ModeAuto`; it used to be the
  default everywhere. Procedure mode on a non-Postgres dialect is a configuration error.
- A bad lookup configuration no longer panics or is silently ignored: the component logs it and
  every call returns the error.
- Dialect is detected from the driver; if detection fails the postgres dialect is assumed.
- Column and row security now work in direct mode (tables `sec_group_members`, `sec_column_rules`,
  `sec_row_rules`); they used to return `ErrDirectModeUnsupported`. `WithNoGroupTables()` skips the
  group membership table.
- `LoginWithAPIKey` works in direct mode; `DatabaseAuthenticator.Logout` now clears the session
  cache in every mode (direct mode used to skip it).
- `Authenticate` now holds the session lookup in the configured backend only; the activity update no
  longer silently falls back to a direct write when the procedure is missing.
- Direct-mode behaviour changes listed under step 4 take effect here.

## Step 6: schemas and docs

- SQL files moved from `pkg/security/` to `pkg/security/lookup/` (`database_schema.sql`,
  `keystore_schema.sql`). `database_schema_sqlite.sql` is superseded by `lookup/ddl/sqlite.sql`
  (now also includes `sec_group_members`, `sec_column_rules`, `sec_row_rules`).
- New `lookup/ddl` package: embedded reference table schemas for `postgres`, `sqlite`, `mysql`,
  `mssql` (`ddl.SQL(dialect)`, `ddl.Statements(dialect)`). `ddl/postgres.sql` is tables only and uses
  base64 / JSON text columns, so it cannot be combined with the procedure schema
  (`database_schema.sql`, `bytea` / `text[]` columns) on the same tables.
- `security.ApplyTxSettings` is unchanged; its SQL moved to `lookup.ApplyTxSettings(ctx, tx, settings)`.
- Removed the unexported `password.go` from `pkg/security` (bcrypt helpers live in `lookup/direct`).
- `README.md`, `KEYSTORE.md` and the root README describe `lookup.Config` instead of `QueryMode`,
  `SQLNames` and `TableNames`.
