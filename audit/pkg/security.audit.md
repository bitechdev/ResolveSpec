# Audit: `pkg/security`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/security` |
| **Size** | 35 non-test files, **11 256 lines** — the largest package in the repo |
| **Key files** | `oauth_server.go` (1 306), `providers.go` (1 274), `middleware.go` (737), `oauth2_examples.go` (615), `passkey_provider.go` (540), `oauth2_methods.go` (511), `providers_direct.go` (473), `provider.go` (458), `hooks.go` (441), `totp_provider_database.go` (326), `keystore_database.go` (297), `sql_names.go` (267), `query_mode.go` (169), `composite.go` (120), `table_names.go` (101), `chain.go` (57) |
| **Schema** | `database_schema.sql`, `database_schema_sqlite.sql`, `keystore_schema.sql` |
| **Docs** | `README.md`, `SECURITY_FEATURES.md`, `QUICK_REFERENCE.md`, `OAUTH2.md`, `OAUTH2_REFRESH_*.md`, `PASSKEY_QUICK_REFERENCE.md`, `KEYSTORE.md` |
| **Tests** | 6 359 lines across 13 `_test.go` files |
| **Audit date** | 2026-09-29 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names and filter expressions all attacker-controlled |
| **Depth** | deep |

## Summary

This is the package that decides who may read and write what. Three of its four
pillars do not hold.

**Authentication does not verify passwords.** `loginDirect`
(`providers_direct.go:30-87`) never reads `req.Password`: it selects the user by
username, mints a session token, and returns it. The shipped stored procedures do
the same — `resolvespec_login` has the `crypt()` check commented out
(`database_schema.sql:124-129`), as does `resolvespec_jwt_login`
(`database_schema.sql:353-357`). The file comment at `providers_direct.go:20-23`
states this is deliberate ("the stored procedures never verify the password hash
either… Direct mode matches that behavior exactly"). So in **both** query modes,
knowing a username is sufficient to obtain a valid 24-hour session. Registration
stores the password in cleartext (`providers_direct.go:125`,
`database_schema.sql:506-512`) and lets the registrant choose their own
`user_level` and `roles`. `resolvespec_jwt_login` returns the stored password in
its JSON payload (`database_schema.sql:367`). This is finding 1, and it is the
finding that matters: every other control in this package sits behind it.

**Row-level security is silently inert.** `applyRowSecurity` builds the WHERE
clause, logs `"Applying row security filter…"` at `Info`, and then tries to attach
it through a type assertion for `interface{ Where(string, ...interface{})
interface{} }` (`hooks.go:134-136`). No type in this repository has that method:
`common.SelectQuery.Where` returns `SelectQuery`, not `interface{}`
(`pkg/common/interfaces.go:47`). I verified with a compiled program that the
assertion fails and that only the corrected signature matches. The clause is
discarded, the miss is recorded at `Debug` (`hooks.go:139`) while the preceding
`Info` line claims success, and every tenant sees every row. The one part of row
security that does work is the `HasBlock` deny at `hooks.go:92-95` — which is
exactly what a manual test would exercise, so the failure looks like success.
Finding 2.

**Column masking has large holes.** `setColSecValue`
(`provider.go:256-299`) dispatches on `strings.ToLower(fieldval.Kind().String())`.
`float64` and `float32` match no case; `bool` matches no case; `time.Time` has
`Kind() == struct`, so the `"time"`/`"date"` case never fires; `[]byte` has
`Kind() == slice`, and the JSON branch it would need tests `fieldTypeName`, which
the caller passes as the **column name** (`provider.go:355`), not a type. Salary,
balance, date-of-birth and JSONB columns are therefore returned unmasked. And
`ApplyColumnSecurity` carries `defer logger.CatchPanic("ApplyColumnSecurity")()`
(`provider.go:302`) on a function with unnamed results, so any panic inside makes
it return a zero `reflect.Value` **and a nil error** — `applyColumnSecurity` reads
that as success and the response goes out unmasked. `GetRowSecurityTemplate` has
the same construct (`provider.go:443`) and fails open to "no security". Findings
3, 4, 5.

**Performance: every secured request serialises on two global mutexes, each held
across a database round trip.** `LoadColumnSecurity` holds
`ColumnSecurityMutex.Lock()` from `provider.go:375` across the provider call at
`:388`; `LoadRowSecurity` holds `RowSecurityMutex.Lock()` from `:424` across the
provider call at `:433`. Neither honours the `pOverwrite` cache flag that
`hooks.go:46`/`:59` pass — both always call the provider and always overwrite — so
there is no caching at all. Maximum throughput for the whole API becomes
`1 / (2 × provider_latency)`, and a slow database converts into a total stall,
because a mutex wait ignores the request's context deadline. Finding 6.

**Credentials reach the logs and the error tracker.** `providers.go:391` logs the
raw `Authorization` header at `Warn` whenever it contains a comma — attacker
triggerable, and per cross-cutting finding X8 every `Warn` is forwarded to Sentry.
`hooks.go:93` logs `%v` of the user ref at `Warn`; for every spec adapter that ref
is the whole `*UserContext`, whose `SessionID` field is the live session token
(`interfaces.go:13`, set at `providers_direct.go:77`). `hooks.go:129-130` prints
the same struct at `Info` on every secured read. Finding 7.

What is genuinely good, and should be protected in any remediation: the OAuth 2.1
authorization-code flow in `oauth_server.go` is carefully built — S256 PKCE is
mandatory (`:550-555`), `redirect_uri` is matched exactly against the registered
list (`:562`, `:602`), authorization codes are single-use under lock
(`:840-845`), client secrets are hashed and compared with
`subtle.ConstantTimeCompare` (`:1259`). The password-reset flow is textbook: 32
random bytes, SHA-256 at rest, one-hour expiry, a deliberately generic response to
prevent user enumeration, and all sessions deleted on completion
(`providers_direct.go:310-406`). Those two flows show the package knows how to do
this; the gap is that the primary login path never got the same treatment.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **Critical** | security | Password is never verified — in Direct mode *and* in the shipped stored procedures; passwords stored in cleartext; self-registration chooses its own roles |
| 2 | **Critical** | security | Row-level security is silently inert: the `Where` type assertion at `hooks.go:134` can never match (verified) |
| 3 | **Critical** | security / panic handling | `defer logger.CatchPanic(...)()` on unnamed results makes `ApplyColumnSecurity` and `GetRowSecurityTemplate` return "success, no rules" after a panic |
| 4 | **High** | security | `setColSecValue` never masks `float*`, `bool`, `time.Time` or `[]byte`; its JSON branch tests the column *name*, not the type (verified) |
| 5 | **High** | security | `loadSecurityRules` fails open on every error path — a transient provider error yields an unfiltered, unmasked query |
| 6 | **High** | thread locking / slowness | Two package-wide write mutexes each held across a provider DB call, with `pOverwrite` ignored so nothing is ever cached |
| 7 | **High** | security / logging | Session tokens and raw `Authorization` headers written to logs and forwarded to Sentry |
| 8 | **High** | security | `HeaderAuthenticator` trusts `X-User-ID`/`X-User-Roles` from the client and is the authenticator used in the README's complete example |
| 9 | **High** | security / slowness | One request can submit unlimited comma-separated tokens; each is tried against the database in turn |
| 10 | **High** | security | Logout does not invalidate the cached session when the token carries a `Bearer ` prefix — the key used to delete differs from the key used to store |
| 11 | **High** | security | Unauthenticated dynamic client registration: any caller registers a client with self-chosen scopes, and the `clients` map is never pruned |
| 12 | **High** | security | `JWTAuthenticator.Login` issues `token_<userid>_<expiry>` as a bearer token — trivially forgeable — and never verifies the password |
| 34 | **High** | security | `lookupOrFetchClient` rehydrates a persisted client without its secret hash, so after a restart every confidential client is treated as public and client authentication is skipped |
| 13 | **Medium** | security | `refresh_token` grant authenticates no client and binds the token to none |
| 14 | **Medium** | security | Both `SecurityList` maps grow without bound, and their keys embed session tokens and JWT claims |
| 15 | **Medium** | security | Model rules fail open: unregistered models allow update and delete; `CheckModelAuthAllowed` grants every authenticated user every operation |
| 16 | **Medium** | security / logging | `logDataAccess` is a `logger.Info` line with a TODO — there is no audit trail |
| 17 | **Medium** | security | `ValidateSQLNames` / `ValidateTableNames` exist but are never called; names are interpolated into SQL unvalidated |
| 18 | **Medium** | security | Internal error text echoed to unauthenticated clients on every auth failure |
| 19 | **Medium** | security / panic handling | `probeFunctionExists` swallows a panic into `false`, silently downgrading the whole authenticator to Direct mode |
| 20 | **Medium** | thread locking | `go a.updateSessionActivity(r.Context(), …)` — unbounded goroutine per request, no recover, and the context is already cancelled |
| 21 | **Medium** | thread locking | `cleanupStates` and `cleanupExpired` goroutines: one unstoppable and leaked per `WithOAuth2` call |
| 22 | **Medium** | security | `ApplyColumnSecurity` returns an error when a table has *no* rules, which `hooks.go:184` logs at `Warn` — one Sentry event per read |
| 23 | **Medium** | security | Ephemeral RS256 signing key generated per process; `id_token`s break on restart and across replicas |
| 24 | **Medium** | correctness | `contains` is prefix-or-suffix, not substring — primary-key detection misses `bun:"id,pk"` and `extractSQLName` returns `"column:name"` verbatim |
| 25 | **Medium** | correctness | `maskString` masks one character too many at each end and indexes runes by byte offset (verified) |
| 26 | **Low** | thread locking | Unsynchronised nil-map reads outside the lock in `ApplyColumnSecurity` and `GetRowSecurityTemplate` — a real race under `-race` |
| 27 | **Low** | security | `SecurityList`'s maps and mutexes are exported, so any importer can mutate the security cache |
| 28 | **Low** | correctness | `ColumSecurityApplyOnRecord` shadows `i` three times and indexes one slice with another's index |
| 29 | **Low** | correctness | `ClearSecurity`'s filter condition is `&&` where it must be `||` (dead code — zero callers) |
| 30 | **Low** | slowness | `splitTag` builds strings with `+=` inside a rune loop — O(n²) per struct tag, on every secured read |
| 31 | **Low** | correctness | `registerDirect` uses `LastInsertId`, unsupported on Postgres, and checks uniqueness outside a transaction |
| 32 | **Low** | security | `Authenticate` may return `(nil, nil)` through the callback, and the caller dereferences it |
| 33 | **Low** | security | `requestPasswordReset` returns the raw reset token to its caller |

---

## 1. Critical — the password is never verified, in either query mode

`loginDirect` reads exactly one thing from the request: the username.

```go
// providers_direct.go:30-46
func (a *DatabaseAuthenticator) loginDirect(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var userID int
	var email, roles, programUserTable sql.NullString
	var userLevel, programUserID sql.NullInt64

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT id, email, user_level, roles, program_user_id, program_user_table FROM %s WHERE username = ? AND is_active = ?`,
			a.tableNames.Users))
		return db.QueryRowContext(ctx, query, req.Username, true).Scan(&userID, &email, &userLevel, &roles, &programUserID, &programUserTable)
	})
```

`req.Password` appears nowhere in the function. Execution continues straight to
`generateSessionToken()` at `:48` and the session row is inserted at `:60`. Any
request naming an existing active user receives a valid 24-hour session token.

This is not an oversight in one code path. The file header states it as policy:

```go
// providers_direct.go:20-23
// Password verification is intentionally not implemented here: the stored
// procedures never verify the password hash either (see the TODOs in
// database_schema.sql), so Direct mode matches that behavior exactly rather
// than introducing a mismatch between modes.
```

And the stored procedures confirm it:

```sql
-- database_schema.sql:119-129  (resolvespec_login)
    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- TODO: Verify password hash using pgcrypto extension
    -- Enable pgcrypto: CREATE EXTENSION IF NOT EXISTS pgcrypto;
    -- IF NOT (crypt(p_request->>'password', v_password_hash) = v_password_hash) THEN
    --     RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
    --     RETURN;
    -- END IF;
```

`resolvespec_jwt_login` is the same (`database_schema.sql:353-357`), and
`JWTAuthenticator.Login` repeats it a third time in Go
(`providers.go:687-690`).

Three consequences compound it:

**Passwords are stored in cleartext.** The hashing step is commented out in the
procedure:

```sql
-- database_schema.sql:506-512
    -- TODO: Hash password using pgcrypto extension
    -- Enable pgcrypto: CREATE EXTENSION IF NOT EXISTS pgcrypto;
    -- v_password := crypt(v_password, gen_salt('bf'));

    -- Create new user
    INSERT INTO users (username, email, password, user_level, roles, is_active, created_at, updated_at, program_user_id, program_user_table)
    VALUES (v_username, v_email, v_password, v_user_level, v_roles, true, now(), now(), v_program_user_id, v_program_user_table)
```

and Direct mode inserts `req.Password` verbatim (`providers_direct.go:125`), as
does password reset (`providers_direct.go:392`, `UPDATE … SET password = ?` with
`req.NewPassword`). The column comment still says "bcrypt hashed password"
(`database_schema.sql:9`), so a reader of the schema would not notice.

**The stored password is returned to the caller.** `resolvespec_jwt_login`
includes it in its result payload:

```sql
-- database_schema.sql:360-370
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'id', v_user_id,
            'username', v_username,
            'email', v_email,
            'password', v_password,
            ...
```

and `JWTAuthenticator.Login` unmarshals it into a struct field
(`providers.go:678`). It is not placed in `LoginResponse`, so it is not returned
over the wire — but it is now in a live Go value that any `%+v` log line or
error-tracker breadcrumb would capture.

**Self-registration chooses its own privileges.** `registerDirect` writes
`req.UserLevel` and `strings.Join(req.Roles, ",")` straight from the request
(`providers_direct.go:100`, `:125`), and the procedure takes the same fields from
`p_request` (`database_schema.sql:445` documents
`{username, password, email, user_level, roles, …}`). A client that can reach
`Register` becomes `user_level: 99, roles: ["admin"]`.

Finally, note that this reaches the OAuth server: `authorizePost` calls the same
login (`oauth_server.go:605-620`), so `/oauth/authorize` issues authorization
codes to anyone who names a valid username.

### Remediation

The fix is not a patch, it is the missing feature. In order:

1. Add a real verifier. In Go, `golang.org/x/crypto/bcrypt`:
   ```go
   var hash string
   // …SELECT id, password, … FROM users WHERE username = ? AND is_active = ?
   if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
       return nil, fmt.Errorf("invalid credentials")   // same text as the not-found path
   }
   ```
   Keep the error text and the timing identical to the user-not-found branch
   (`providers_direct.go:42-43`) — compare against a dummy hash when the user does
   not exist, so the two paths cost the same.
2. In the procedures, enable `pgcrypto` and uncomment the three `crypt()` blocks.
3. Hash on write: `crypt(v_password, gen_salt('bf'))` in `resolvespec_register`,
   and `bcrypt.GenerateFromPassword` in `registerDirect` and
   `completePasswordResetDirect`.
4. Remove `'password', v_password` from `resolvespec_jwt_login`'s payload and the
   `Password` field from the struct at `providers.go:678`.
5. Do not let a registration request set `user_level` or `roles`. Take them from
   server-side policy; if a deployment genuinely needs client-supplied roles,
   intersect against an allow-list.
6. Ship a migration for existing rows. Cleartext passwords cannot be converted —
   rehash on next successful login, or force a reset.
7. Add a test that asserts login **fails** with a wrong password, for both modes.
   The absence of that test is what let this ship: `providers_test.go` covers the
   success path only.

Until step 1 lands, no other finding in this file can be assessed as mitigated,
because an attacker does not need to bypass column masking or row filters if they
can log in as the data's owner.

---

## 2. Critical — row-level security is silently inert everywhere

`applyRowSecurity` does all the work and then throws it away.

```go
// hooks.go:126-141
		// Generate the WHERE clause from template
		whereClause := rowSec.GetTemplate(pkName, modelType)

		logger.Info("Applying row security filter for user %v on %s.%s: %s",
			userRef, schema, tablename, whereClause)

		// Apply the WHERE clause to the query
		query := secCtx.GetQuery()
		if selectQuery, ok := query.(interface {
			Where(string, ...interface{}) interface{}
		}); ok {
			secCtx.SetQuery(selectQuery.Where(whereClause))
		} else {
			logger.Debug("Query doesn't support Where method, skipping row security")
		}
```

The asserted method must return `interface{}`. The query that
`restheadspec/handler.go:924` places in the hook context is a
`common.SelectQuery`, whose `Where` returns `SelectQuery`:

```go
// pkg/common/interfaces.go:47
	Where(query string, args ...interface{}) SelectQuery
```

Go interface satisfaction requires the method signature to match exactly,
including the result type, so the assertion fails for every query type in this
repository. I confirmed there is no other candidate: grepping for
`Where(string, ...interface{}) interface{}` finds only the assertion itself, and
`UpdateQuery.Where` (`pkg/common/interfaces.go:87`) and `DeleteQuery.Where`
(`:98`) return their own concrete interfaces too. I then compiled a minimal
program with a `SelectQuery`-shaped type and verified the assertion fails while
the corrected `Where(string, ...interface{}) SelectQuery` form succeeds.

Two things make this hard to notice:

* The `Info` line at `:129` is emitted **before** the assertion and says
  "Applying row security filter … `<clause>`". The only record of the failure is
  the `Debug` at `:139`, which is off in production
  (`zap.NewProductionConfig()`, `logger/logger.go:24`). The logs assert that
  row security is working.
* The deny path still works. `HasBlock` returns an error at `hooks.go:92-95`
  before any of this, so "block user X from table Y" behaves correctly. A
  reviewer who tests blocking concludes row security is live.

This affects every spec: `restheadspec/security_hooks.go:31-34`,
`resolvespec/security_hooks.go`, `websocketspec/security_hooks.go`,
`mqttspec/security_hooks.go`, `resolvemcp/security_hooks.go` and
`funcspec/security_adapter.go` all register `ApplyRowSecurity` against the same
`hooks.go` implementation.

### Remediation — and why the obvious fix is dangerous

The naive correction is to assert the real interface:

```go
if selectQuery, ok := query.(common.SelectQuery); ok {
    secCtx.SetQuery(selectQuery.Where(whereClause))
}
```

Do not ship that alone. It would turn a dead code path into a SQL injection
sink, because of how the clause is built:

```go
// provider.go:43-50
func (m *RowSecurity) GetTemplate(pPrimaryKeyName string, pModelType reflect.Type) string {
	str := m.Template
	str = strings.ReplaceAll(str, "{PrimaryKeyName}", pPrimaryKeyName)
	str = strings.ReplaceAll(str, "{TableName}", m.Tablename)
	str = strings.ReplaceAll(str, "{SchemaName}", m.Schema)
	str = strings.ReplaceAll(str, "{UserID}", fmt.Sprintf("%v", m.UserID))
	return str
}
```

`m.UserID` is typed `any` (`provider.go:40`) and for every spec adapter it is the
whole `*UserContext` (`restheadspec/security_hooks.go:83-89`), so
`fmt.Sprintf("%v", …)` renders `&{1 alice 0 sess_… [user] alice@example.com
map[…] …}` — including JWT-derived `UserName`, `Email` and `Claims` — directly
into a WHERE clause that `hooks.go:137` passes with **no bind arguments**. A
template of `user_id = {UserID}` plus an attacker-influenced claim yields
arbitrary SQL. The two defects currently mask each other.

Fix both together:

1. Change the template contract to emit a placeholder and a bind value:
   ```go
   func (m *RowSecurity) GetTemplate(pk string, t reflect.Type) (string, []any) {
       str := m.Template
       str = strings.ReplaceAll(str, "{PrimaryKeyName}", quoteIdent(pk))
       str = strings.ReplaceAll(str, "{TableName}",   quoteIdent(m.Tablename))
       str = strings.ReplaceAll(str, "{SchemaName}",  quoteIdent(m.Schema))
       if !strings.Contains(str, "{UserID}") {
           return str, nil
       }
       return strings.ReplaceAll(str, "{UserID}", "?"), []any{m.userIDScalar()}
   }
   ```
   where `userIDScalar()` returns an `int`/`string` — never a struct. Reject a
   `*UserContext` here outright; a provider that needs a claim should put that
   claim in `RowSecurity.UserID` itself.
2. Then assert `common.SelectQuery` and pass the args:
   `selectQuery.Where(clause, args...)`.
3. Make the failure loud. A security filter that cannot be attached must fail the
   request, not log at `Debug`:
   ```go
   selectQuery, ok := query.(common.SelectQuery)
   if !ok {
       return fmt.Errorf("row security: query type %T does not support Where", query)
   }
   ```
4. Add a test that asserts the generated SQL contains the filter. A unit test on
   `applyRowSecurity` with a fake `SelectQuery` recording its calls would have
   caught this on day one.

Note also that `hooks.go:113-124` walks `modelType.NumField()` without checking
`modelType.Kind() == reflect.Struct` first; a non-struct model panics there. The
spec handlers recover (`restheadspec/handler.go:396-400`), so it surfaces as a
500 rather than a crash.

---

## 3. Critical — `CatchPanic` on unnamed results converts a panic into "allowed"

Two functions in `provider.go` defer `logger.CatchPanic`:

```go
// provider.go:301-306
func (m *SecurityList) ApplyColumnSecurity(records reflect.Value, modelType reflect.Type, pUserID int, pSchema, pTablename string) (reflect.Value, error) {
	defer logger.CatchPanic("ApplyColumnSecurity")()

	if m.ColumnSecurity == nil {
		return records, fmt.Errorf("security not initialized")
	}
```

```go
// provider.go:442-447
func (m *SecurityList) GetRowSecurityTemplate(pUserRef any, pSchema, pTablename string) (RowSecurity, error) {
	defer logger.CatchPanic("GetRowSecurityTemplate")()

	if m.RowSecurity == nil {
		return RowSecurity{}, fmt.Errorf("security not initialized")
	}
```

`CatchPanic` returns a closure that calls `recover()`
(`logger/logger.go:153-186`). Recovering in a deferred function on a function
whose results are **unnamed** means the function returns its results' zero
values — here `(reflect.Value{}, nil)` and `(RowSecurity{}, nil)`. The `nil`
error is the problem: both callers treat it as success.

For column masking:

```go
// hooks.go:182-192
	maskedResult, err := securityList.ApplyColumnSecurity(resultValue, modelType, userID, schema, tablename)
	if err != nil {
		logger.Warn("Column security error: %v", err)
		// Don't fail the request, just log the issue
		return nil
	}

	// Update the result with masked data
	if maskedResult.IsValid() && maskedResult.CanInterface() {
		secCtx.SetResult(maskedResult.Interface())
	}
```

`err` is nil, `maskedResult.IsValid()` is false, so nothing happens and
`applyColumnSecurity` returns nil — "masking applied successfully". The handler
then serialises the untouched records (`restheadspec/handler.go:1006` sends
`modelPtr`, the same backing array `ApplyColumnSecurity` was supposed to mutate in
place). The client receives **unmasked** data and the response is a 200.

For row security, `GetRowSecurityTemplate` returning `(RowSecurity{}, nil)` gives
`HasBlock == false` and `Template == ""`, so `applyRowSecurity` returns nil at
`hooks.go:143` — "this table has no row security". A panic while looking up a
blocking rule therefore **unblocks** the user.

This is reachable. `setColSecValue` calls `fieldval.SetString`
(`provider.go:275`, `:277`), `SetZero` (`:271`) and `SetBytes` (`:295`) with no
`CanSet()` guard — only the integer branch checks (`:266`). A rule naming an
unexported field, or a `[]byte` branch reached on a non-byte slice, panics. One
such rule aborts masking for **all** columns and **all** records in the response,
silently.

The panic is logged (`Error` + Sentry via `CatchPanicCallback`), so there is a
trace — but the request still succeeds with unprotected data.

### Remediation

Name the results so the recovered value can be converted into a failure, and make
the caller fail closed:

```go
func (m *SecurityList) ApplyColumnSecurity(records reflect.Value, modelType reflect.Type,
	pUserID int, pSchema, pTablename string) (out reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = reflect.Value{}
			err = logger.HandlePanic("ApplyColumnSecurity", r)   // logs + returns an error
		}
	}()
	...
}
```

`logger.HandlePanic` (`logger/logger.go:197-211`) already does exactly this and is
the right helper here; `CatchPanic` is only appropriate on functions that return
nothing. Same change at `provider.go:443`.

Then close the caller:

```go
// hooks.go:183
	if err != nil {
		return fmt.Errorf("column security failed for %s.%s: %w", schema, tablename, err)
	}
```

— see finding 22 for why that requires fixing the "no rules" error first, and
finding 5 for the same change on the load path. Separately, guard the reflect
writes in `setColSecValue` with `CanSet()` so the panic does not arise at all, and
have it return an error the caller aggregates rather than the ignored
`(int, reflect.Value)` pair.

`_CROSS-CUTTING` finding X8 covers the general `CatchPanic`-on-unnamed-results
hazard; this package is where it has the highest cost.

---

## 4. High — four common column types are never masked

`setColSecValue` decides what to do by string-matching the reflect *kind*:

```go
// provider.go:256-299 (abridged)
	fieldKindLower := strings.ToLower(fieldval.Kind().String())
	switch {
	case strings.Contains(fieldKindLower, "int") && (mask || hide):
		if fieldval.CanInt() && fieldval.CanSet() {
			fieldval.SetInt(0)
		}
	case (strings.Contains(fieldKindLower, "time") || strings.Contains(fieldKindLower, "date")) && (mask || hide):
		fieldval.SetZero()
	case strings.Contains(fieldKindLower, "string"):
		...
	case strings.Contains(fieldTypeName, "json") && (mask || hide):
		...
	}
```

Working through the kinds a model actually uses:

| Go type | `Kind().String()` | Matches? | Result |
|---|---|---|---|
| `int`, `int64`, `uint8` | `int`, `int64`, `uint8` | yes (`"uint8"` contains `"int"`) | zeroed |
| `string` | `string` | yes | masked |
| **`float64`, `float32`** | `float64`, `float32` | **no** — `"float64"` does not contain `"int"` | **returned in full** |
| **`bool`** | `bool` | **no** | **returned in full** |
| **`time.Time`** | `struct` | **no** — `"struct"` contains neither `"time"` nor `"date"` | **returned in full** |
| **`[]byte` / JSONB** | `slice` | **no** — see below | **returned in full** |
| `*string` | (dereferenced at `:258-260`) | yes | masked |

So `salary float64`, `account_balance`, `is_vip bool`, `date_of_birth time.Time`
and every JSONB column pass through a "mask" rule untouched. I verified the string
matching: `strings.Contains("float64", "int")` is false, `strings.Contains("uint8",
"int")` is true, and `reflect.TypeOf(time.Time{}).Kind().String()` is `"struct"`.

The JSON branch cannot fire for the intended reason. It tests `fieldTypeName`, and
the caller passes the **column name**:

```go
// provider.go:353-356
					if i == pathLen-1 {
						if nameType == "sql" || nameType == "struct" {
							setColSecValue(field, *colsec, fieldName)
						}
```

where `fieldName` was assigned from `cols.SQLName` or `cols.Name`
(`provider.go:341`, `:347`). The branch therefore fires only when the column
happens to be *named* something containing "json" — and when it does fire on a
non-`[]byte` field, `fieldval.Bytes()` at `:285` panics, which finding 3 turns
into a silent unmasked response. Conversely a JSONB column mapped to `string`
matches the earlier `"string"` case and has its **entire document** masked instead
of the configured path.

Also note the switch has no default: an unmatched kind returns `(0, fieldsrc)`
indistinguishably from success, and both return values are discarded at the call
site.

### Remediation

Dispatch on the type, not on a substring of the kind's name, and report what could
not be handled:

```go
func setColSecValue(fieldsrc reflect.Value, colsec ColumnSecurity, sqlName string) error {
	fieldval := fieldsrc
	for fieldval.Kind() == reflect.Pointer || fieldval.Kind() == reflect.Interface {
		if fieldval.IsNil() {
			return nil
		}
		fieldval = fieldval.Elem()
	}
	if !fieldval.CanSet() {
		return fmt.Errorf("column %s: field not settable", sqlName)
	}
	hide := strings.EqualFold(colsec.Accesstype, "hide")

	// Concrete types first — before any Kind switch.
	switch fieldval.Interface().(type) {
	case time.Time:
		fieldval.SetZero()
		return nil
	case []byte:
		return maskJSONPath(fieldval, colsec)   // the gjson/sjson path
	}

	switch fieldval.Kind() {
	case reflect.String:
		if hide { fieldval.SetString(""); return nil }
		fieldval.SetString(maskString(...))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fieldval.SetInt(0)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fieldval.SetUint(0)
	case reflect.Float32, reflect.Float64:
		fieldval.SetFloat(0)
	case reflect.Bool:
		fieldval.SetBool(false)
	default:
		return fmt.Errorf("column %s: unsupported kind %s for %s",
			sqlName, fieldval.Kind(), colsec.Accesstype)
	}
	return nil
}
```

Decide the JSON case on the field's type (`[]byte`, `json.RawMessage`, or a
driver-specific JSONB wrapper), never on its name. `ApplyColumnSecurity` should
collect the returned errors and propagate them; with finding 3 fixed, an
unmaskable column then fails the request instead of leaking.

Test matrix worth adding: one model with `string`, `*string`, `int64`, `float64`,
`bool`, `time.Time`, `[]byte` and an unexported field, each with `mask` and `hide`,
asserting the value actually changed.

---

## 5. High — `loadSecurityRules` fails open on every path

```go
// hooks.go:32-67
func loadSecurityRules(secCtx SecurityContext, securityList *SecurityList) error {
	userID, ok := secCtx.GetUserID()
	if !ok {
		logger.Warn("No user ID in context for security check")
		return nil
	}
	...
	err := securityList.LoadColumnSecurity(secCtx.GetContext(), userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load column security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}
	...
	_, err = securityList.LoadRowSecurity(secCtx.GetContext(), userRef, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load row security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}

	return nil
}
```

Three fail-open points, with the `return err` commented out at two of them. The
stated rationale — "don't fail the request if no security rules exist" — conflates
*no rules* with *could not determine the rules*. The consequences differ
completely:

* A provider timeout, a closed connection, a missing stored procedure, a
  permissions error on the security tables: `ColumnSecurity[key]` is left empty
  and `RowSecurity[key]` is not written, so the subsequent `ApplyColumnSecurity`
  finds no rules and `GetRowSecurityTemplate` reports "no row security". The
  request proceeds **unmasked and unfiltered**. An attacker who can induce load on
  the security database gets a window of unrestricted reads.
* `GetUserID` is `ctx.Value(UserIDKey).(int)` (`middleware.go:402-405`). Any
  deployment whose user identifier is not an `int` — a UUID subject from an OIDC
  provider, for example — gets `ok == false` and **no rules are loaded at all**,
  for every request. The `Warn` at `:36` is the only signal, and per X8 it floods
  Sentry with one event per request.

Note `GetUserRef` (`:55-58`) was added to handle non-integer identifiers, but
`loadSecurityRules` still returns early on the `GetUserID` check before reaching
it, and `LoadColumnSecurity`'s signature is `int`-only
(`provider.go:370`), so column security cannot be loaded for a non-integer user
at all.

### Remediation

Distinguish "no rules" from "unknown". Have the provider contract say so — e.g.
a sentinel `ErrNoSecurityRules` that the loader treats as success and everything
else as failure:

```go
	if err := securityList.LoadColumnSecurity(ctx, userID, schema, tablename, false); err != nil {
		if !errors.Is(err, ErrNoSecurityRules) {
			return fmt.Errorf("column security unavailable for %s.%s: %w", schema, tablename, err)
		}
	}
```

and register the hook so a returned error aborts the request. For the user-ID
path, widen `LoadColumnSecurity` to take the same `any` ref `LoadRowSecurity`
takes, and drop the `GetUserID` gate in favour of `GetUserRef`:

```go
	userRef, ok := secCtx.GetUserRef()
	if !ok {
		return fmt.Errorf("no user identity in context")   // fail closed
	}
```

If a deployment genuinely wants unauthenticated reads, that is what
`ModelRules.CanPublicRead` is for (`hooks.go:254`) — an explicit opt-in per model,
not an implicit consequence of a missing context value.

---

## 6. High — two global mutexes, each held across a database call, with no caching

```go
// provider.go:370-395
func (m *SecurityList) LoadColumnSecurity(ctx context.Context, pUserID int, pSchema, pTablename string, pOverwrite bool) error {
	if m.provider == nil {
		return fmt.Errorf("security provider not set")
	}

	m.ColumnSecurityMutex.Lock()
	defer m.ColumnSecurityMutex.Unlock()
	...
	if pOverwrite || m.ColumnSecurity[secKey] == nil {
		m.ColumnSecurity[secKey] = make([]ColumnSecurity, 0)
	}

	// Call the provider to load security rules
	colSecList, err := m.provider.GetColumnSecurity(ctx, pUserID, pSchema, pTablename)
	if err != nil {
		return fmt.Errorf("GetColumnSecurity failed: %v", err)
	}

	m.ColumnSecurity[secKey] = colSecList
	return nil
}
```

```go
// provider.go:419-440 (abridged)
	m.RowSecurityMutex.Lock()
	defer m.RowSecurityMutex.Unlock()
	...
	record, err := m.provider.GetRowSecurity(ctx, pUserRef, pSchema, pTablename)
	...
	m.RowSecurity[secKey] = record
```

Three problems, compounding.

**The lock spans the round trip.** `GetColumnSecurity` and `GetRowSecurity` are
the provider interface (`interfaces.go:133-137`); the shipped implementations are
`DatabaseColumnSecurityProvider` / `DatabaseRowSecurityProvider`, which issue SQL.
The write lock is therefore held for the full database latency. Every secured
request calls both loaders (`hooks.go:46`, `:59`), so the ceiling on the whole
API's throughput is `1 / (2 × provider_latency)` — with a 5 ms security query,
about 100 req/s regardless of how many cores or connections are available.

**A slow database becomes a total stall.** `sync.Mutex.Lock` has no timeout and
ignores `ctx`. If the security provider's connection pool saturates, every
in-flight and future request blocks in `Lock()` rather than failing with the
request's deadline. Cancelled clients still hold their place in the queue.

**There is no caching, so this happens on every request.** Both functions take a
`pOverwrite` parameter and both effectively ignore it: `LoadColumnSecurity`
consults it only to reset the slice at `:383` and then unconditionally queries at
`:388` and overwrites at `:393`; `LoadRowSecurity` does not reference `pOverwrite`
at all. `hooks.go:46` and `:59` both pass `false` — expecting a cache — and get a
database call anyway. The two maps are thus write-only caches: they accumulate
entries (finding 14) that are never read as a hit.

`GetRowSecurityTemplate` (`:442-458`) does take only `RLock`, so the read side is
concurrent — but it reads the entry that was just overwritten microseconds
earlier under the write lock, so the read lock buys nothing.

### Remediation

1. **Never call the provider under the lock.** Load first, then publish:
   ```go
   func (m *SecurityList) LoadColumnSecurity(ctx context.Context, ref any, schema, table string, overwrite bool) error {
       key := colSecKey(schema, table, ref)

       if !overwrite {
           m.mu.RLock()
           _, hit := m.columnSecurity[key]
           m.mu.RUnlock()
           if hit {
               return nil
           }
       }

       list, err := m.provider.GetColumnSecurity(ctx, ref, schema, table)   // no lock held
       if err != nil {
           return fmt.Errorf("GetColumnSecurity failed: %w", err)
       }

       m.mu.Lock()
       m.columnSecurity[key] = entry{rules: list, loadedAt: time.Now()}
       m.mu.Unlock()
       return nil
   }
   ```
   Two callers may duplicate one query on a cold key; that is strictly better than
   serialising all of them. If duplicate work matters, collapse it with
   `golang.org/x/sync/singleflight` keyed on `key` — that also keeps a thundering
   herd off the security database.
2. **Honour `pOverwrite` and add a TTL.** Security rules that never expire are
   also a correctness problem: revoking a mask takes effect only on restart. A
   short TTL (30–60 s) plus an explicit invalidate is the usual shape. The
   `Cacheable` interface the composite provider already probes for
   (`composite.go:99-120`) is the natural hook.
3. **Consider the existing cache package.** `pkg/cache` already provides TTL,
   tag-based invalidation and a size cap; `DatabaseAuthenticator` uses it for
   sessions (`providers.go:402`). Reusing it here would fix caching, bounding and
   invalidation in one move — and see `cache.audit.md` findings 1, 2 and 7-9 for
   the caveats to apply when doing so.
4. **Wrap the provider call with a deadline** derived from the request context so a
   hung security query fails the request rather than occupying a slot.

Note that fixing the lock is what *enables* finding 5's fail-closed change to be
safe: failing closed on a provider error is only acceptable once a provider stall
cannot take the whole process down with it.

---

## 7. High — session tokens and `Authorization` headers are written to logs and Sentry

Three places log credentials.

**The raw `Authorization` header, at `Warn`:**

```go
// providers.go:389-392
	// Log warning if multiple tokens are provided
	if len(tokens) > 1 {
		logger.Warn("Multiple authentication tokens provided in Authorization header (%d tokens). This is unusual and may indicate a misconfigured client. Header: %s", len(tokens), sessionToken)
	}
```

`sessionToken` here is the unparsed header value (`:354`). Any client can trigger
this by sending `Authorization: Bearer abc, Bearer def` — so an attacker can
choose what gets written, and a legitimate client with a quirk writes its real
token. Per `_CROSS-CUTTING` X8 every `logger.Warn` is forwarded to the error
tracker (`logger/logger.go:118-122`), so the token leaves the host entirely and
lands in Sentry's retention.

**The whole `UserContext`, including `SessionID`, at `Warn` and `Info`:**

```go
// hooks.go:92-95
	if rowSec.HasBlock {
		logger.Warn("User %v blocked from accessing %s.%s", userRef, schema, tablename)
		return fmt.Errorf("access denied to %s", tablename)
	}
```

```go
// hooks.go:129-130
		logger.Info("Applying row security filter for user %v on %s.%s: %s",
			userRef, schema, tablename, whereClause)
```

`userRef` comes from `GetUserRef`, which returns the full `*UserContext`
(`restheadspec/security_hooks.go:83-89`, and identically in the websocketspec,
mqttspec, resolvespec, resolvemcp and funcspec adapters). `fmt`'s `%v` on a
pointer-to-struct prints every field:

```go
// interfaces.go:9-23
type UserContext struct {
	UserID           int            `json:"user_id"`
	UserName         string         `json:"user_name"`
	UserLevel        int            `json:"user_level"`
	SessionID        string         `json:"session_id"`
	...
	Claims           map[string]any `json:"claims"`
	Meta             map[string]any `json:"meta"`
```

`SessionID` holds the live session token — `loginDirect` assigns
`SessionID: sessionToken` (`providers_direct.go:77`), as does `registerDirect`
(`:169`). So `hooks.go:93` sends a working session token to Sentry, and
`hooks.go:129` writes one to the log file on **every secured read of every table
that has a row-security template**. `Claims` is the full OIDC userinfo map when an
external provider is used (`oauth2_methods.go:356`), so JWT claims go with it.

`hooks.go:87` (`logger.Debug("No row security for %s.%s@%v: %v", …, userRef, err)`)
has the same content but at `Debug`, which production configuration suppresses and
which X8 does not forward — lower risk, same fix.

### Remediation

1. Never format a `UserContext` with `%v`. Give it a redacting `String()`:
   ```go
   func (u *UserContext) String() string {
       if u == nil { return "<nil user>" }
       return fmt.Sprintf("user{id=%d name=%q level=%d roles=%v}", u.UserID, u.UserName, u.UserLevel, u.Roles)
   }
   ```
   `fmt` will use it for both `%v` and `%s`, fixing all three call sites at once
   — and also shrinking the `SecurityList` map keys in finding 14. Pair it with a
   `MarshalJSON` that omits `SessionID`, so the struct cannot leak through a JSON
   log encoder either.
2. At `providers.go:391`, drop the header from the message. The count is the
   actionable part:
   `logger.Warn("Authorization header carried %d tokens; using the first that validates", len(tokens))`.
   Better still, make it `Debug` — a multi-token header is client-triggerable, so
   at `Warn` it is also an attacker-controlled Sentry flood (X8).
3. Lower `hooks.go:129` to `Debug`. "Row security applied" on every read is not a
   `Warn`-worthy event, and at `Info` it doubles the log volume of the read path.
4. Repo-wide, grep for `%v` applied to anything reaching a `UserContext`,
   `LoginRequest` (which has a `Password` field, `interfaces.go:28`) or
   `LoginResponse` before considering this closed.

---

## 8. High — `HeaderAuthenticator` trusts the client, and the README recommends it

```go
// providers.go:18-23
// Production-Ready Authenticators
// =================================

// HeaderAuthenticator provides simple header-based authentication
// Expects: X-User-ID, X-User-Name, X-User-Level, X-Session-ID, X-Remote-ID, X-User-Roles, X-User-Email
type HeaderAuthenticator struct{}
```

```go
// providers.go:45-64
func (a *HeaderAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return nil, fmt.Errorf("X-User-ID header required")
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	return &UserContext{
		UserID:    userID,
		UserName:  r.Header.Get("X-User-Name"),
		UserLevel: parseIntHeader(r, "X-User-Level", 0),
		...
		Roles:     parseRoles(r.Header.Get("X-User-Roles")),
	}, nil
}
```

Every field comes from a request header. `curl -H 'X-User-ID: 1' -H
'X-User-Roles: admin' -H 'X-User-Level: 99'` authenticates as user 1 with
administrative roles. There is no shared secret, no signature, no trusted-proxy
list, and nothing that distinguishes a header injected by a gateway from one sent
by the client.

This pattern is legitimate — but only behind a proxy that strips the headers from
inbound requests and re-adds them after authenticating. Nothing in this package
says so. It sits under a heading that says "Production-Ready", and it is the
authenticator in the README's end-to-end wiring example:

```go
// pkg/security/README.md:655-666
    "public.orders": "user_id = {UserID}",
    }

    // Create providers
    auth := security.NewHeaderAuthenticator()
    colSec := security.NewConfigColumnSecurityProvider(columnRules)
    rowSec := security.NewConfigRowSecurityProvider(rowTemplates, nil)

    // Combine providers and register hooks
    provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
    securityList := security.NewSecurityList(provider)
    restheadspec.RegisterSecurityHooks(handler, securityList)
```

A developer following that example ships an API where authentication is a header.
`README.md:298` does mark `DatabaseAuthenticator` as "(Recommended)", but the
complete example contradicts it.

Reachability: `NewHeaderAuthenticator` has **no non-test callers** in this
repository (only its own constructor at `providers.go:25-26`), so nothing here is
currently exposed. This is a hazard shipped to consumers, not a live
vulnerability — which is why it is High rather than Critical.

### Remediation

Do not delete it; make misuse hard.

1. Require the trust boundary to be declared. Give the constructor a mandatory
   configuration:
   ```go
   type HeaderAuthenticatorConfig struct {
       TrustedProxies []netip.Prefix   // required, no default
       RequiredSecret string           // optional shared secret in X-Auth-Proxy-Secret
   }
   func NewHeaderAuthenticator(cfg HeaderAuthenticatorConfig) (*HeaderAuthenticator, error) {
       if len(cfg.TrustedProxies) == 0 {
           return nil, errors.New("HeaderAuthenticator requires TrustedProxies: these headers are client-controlled")
       }
       ...
   }
   ```
   and have `Authenticate` reject any request whose direct peer
   (`r.RemoteAddr`, **not** `X-Forwarded-For` — see `middleware.audit.md` finding 2)
   is outside the list. Compare the secret with `subtle.ConstantTimeCompare`.
2. Move it out from under "Production-Ready Authenticators", and put the warning in
   the doc comment rather than only in prose: *"trusts unauthenticated request
   headers; safe only behind a reverse proxy that strips `X-User-*` from inbound
   requests."*
3. Change the README's complete example to `NewDatabaseAuthenticator(db)`. If a
   header example is wanted, show it with the trusted-proxy configuration and the
   nginx/envoy `proxy_set_header` lines that make it sound.

---

## 9. High — one request can brute-force unlimited session tokens

```go
// providers.go:365-396 (abridged)
	} else {
		// Parse Authorization header which may contain multiple comma-separated tokens
		// Format: "Token abc, Token def" or "Bearer abc" or just "abc"
		rawTokens := strings.Split(sessionToken, ",")
		for _, token := range rawTokens {
			token = strings.TrimSpace(token)
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimPrefix(token, "Token ")
			token = strings.TrimSpace(token)
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	...
	// Try each token until one succeeds
	var lastErr error
	for _, token := range tokens {
```

`strings.Split` on an attacker-supplied header with no cap on the number of
elements, feeding a loop that performs one cache lookup and, on a miss, one
database round trip per element (`:402-438`).

`Authorization: a,b,c,…` with 10 000 elements is one HTTP request that issues
10 000 session lookups. Two consequences:

* **Rate-limit bypass for credential stuffing.** Any per-request throttle — and
  per `middleware.audit.md` finding 1 there is currently none mounted at all —
  counts this as a single request while it tests 10 000 candidate tokens.
* **Amplified load.** Each miss is a `QueryRowContext` against the session
  procedure. The request body can be empty; the header does the work. Go's default
  `MaxHeaderBytes` is 1 MiB, which at ~4 bytes per element allows on the order of
  200 000 lookups per request.

The loop also stops at the *first* token that validates, so a valid token placed
last still works after thousands of failures — there is no early abort.

### Remediation

1. Cap the count, and reject rather than truncate:
   ```go
   const maxAuthTokens = 4
   if len(rawTokens) > maxAuthTokens {
       return nil, fmt.Errorf("too many authorization tokens")
   }
   ```
   Four is generous; RFC 7235 expects one credential per header. If the
   multi-token behaviour exists for a specific client, one is the right number
   and that client should be fixed.
2. Abort the loop on the first *hard* failure. "Invalid or expired session"
   (`:424`) means this credential is wrong — there is no reason to keep trying
   unless the caller legitimately holds several. Distinguish it from a transport
   error, which should fail the request rather than fall through to the next token.
3. Charge the rate limiter per token, not per request, once a limiter is mounted
   (`middleware.audit.md` finding 1).
4. Drop the `Warn` on the multi-token path (finding 7) — with a cap in place, the
   log line loses its purpose and its Sentry cost.

---

## 10. High — logout does not invalidate a cached `Bearer ` session

`Authenticate` strips the scheme prefix **before** building the cache key:

```go
// providers.go:371-374, 398
			token = strings.TrimPrefix(token, "Bearer ")
			token = strings.TrimPrefix(token, "Token ")
			token = strings.TrimSpace(token)
			...
		cacheKey := fmt.Sprintf("auth:session:%s", token)
```

`Logout` builds the key from the **unstripped** request field:

```go
// providers.go:316-320
	// Clear cache for this token
	if req.Token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", req.Token)
		_ = a.cache.Delete(ctx, cacheKey)
	}
```

`logoutDirect` has the same split — it strips for the SQL but not for the cache:

```go
// providers_direct.go:182-184, 203-206
	token := req.Token
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	...
	if req.Token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", req.Token)
		_ = a.cache.Delete(ctx, cacheKey)
	}
```

So a client that logs out with `{"token": "Bearer sess_abc…"}` — the natural
thing to send back, and exactly what `logoutDirect:183` anticipates — deletes
`auth:session:Bearer sess_abc…`, a key that was never written. The live entry
`auth:session:sess_abc…` survives. The session row is deleted from the database,
but `Authenticate` serves the cached `UserContext` without consulting the
database:

```go
// providers.go:402-403
		err := a.cache.GetOrSet(r.Context(), cacheKey, &userCtx, a.cacheTTL, func() (any, error) {
			// This function is called only if cache miss
```

The token therefore keeps working until `cacheTTL` expires — five minutes by
default (`providers.go:134`, `:140`). On a shared or stolen device, "log out"
does not end the session.

`logoutDirect:184` also strips only `"Bearer "` and `"bearer "`, while
`Authenticate:373` strips `"Token "` as well, so a `Token …`-prefixed logout
fails to match the database row either (`rows == 0` → `"session not found"` at
`:199-201`) while the cache entry also survives.

### Remediation

Normalise once, in one place, and use it everywhere:

```go
// normalizeSessionToken strips any supported scheme prefix. The result is the
// value used both as the cache key suffix and as the stored session_token.
func normalizeSessionToken(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"Bearer ", "bearer ", "Token ", "token "} {
		if rest, ok := cutPrefixFold(s, p); ok {
			return strings.TrimSpace(rest)
		}
	}
	return s
}

func sessionCacheKey(token string) string {
	return "auth:session:" + normalizeSessionToken(token)
}
```

Replace all four sites (`providers.go:398`, `:466`, `:318`;
`providers_direct.go:183`, `:204`) with these helpers.

Two hardening steps beyond the bug:

* Have `Logout` delete the cache entry **before** the database call, and again
  after, so a failed database logout still drops the cached credential.
* Consider whether a 5-minute positive cache on sessions is the right default at
  all. It is a 5-minute window on every revocation, not just logout — password
  reset (`providers_direct.go:395`) deletes session rows but nothing clears their
  cache entries, so a reset password also leaves sessions live for `cacheTTL`.
  `ClearCache(token)` exists (`providers.go:463-471`) but the reset path does not
  call it.

Separately: session tokens are stored in the database in plaintext
(`providers_direct.go:60` inserts `sessionToken` directly, and `sessionDirect:219`
matches on equality), and they are also cache keys — so both the cache keyspace
and a database read expose usable credentials. Storing `sha256(token)` and
matching on the hash would remove both exposures, at the cost of a migration.


---

## 11. High — unauthenticated client registration, with self-chosen scopes and no pruning

`/oauth/register` is mounted unconditionally:

```go
// oauth_server.go:240
	mux.HandleFunc("/oauth/register", s.registerHandler)
```

and the handler requires nothing but a POST:

```go
// oauth_server.go:406-433 (abridged)
func (s *OAuthServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		GrantTypes              []string `json:"grant_types"`
		AllowedScopes           []string `json:"allowed_scopes"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { ... }
	if len(req.RedirectURIs) == 0 { ... }
	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	allowedScopes := req.AllowedScopes
	if len(allowedScopes) == 0 {
		allowedScopes = s.cfg.DefaultScopes
	}
```

RFC 7591 permits open registration, so this is a defensible default for an MCP
server — but three specifics make it exploitable here.

**The client declares its own scopes.** `allowedScopes := req.AllowedScopes`
(`:430`) is stored verbatim into the client record (`:467`) with no intersection
against a server-side permitted set. `s.cfg.DefaultScopes` is used only when the
request omits the field. A registration asking for
`"allowed_scopes": ["admin", "write:all"]` gets them.

**Registered clients are never removed.** The cleanup goroutine prunes only the
two short-lived maps:

```go
// oauth_server.go:252-274 (abridged)
func (s *OAuthServer) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for k, p := range s.pending {
				if now.After(p.ExpiresAt) { delete(s.pending, k) }
			}
			for k, p := range s.codes {
				if now.After(p.ExpiresAt) { delete(s.codes, k) }
			}
			s.mu.Unlock()
		}
	}
}
```

`s.clients` (`:136`, initialised `:189`) has no expiry, no cap and no eviction.
Each POST to `/oauth/register` adds a permanent entry holding the client name, the
`redirect_uris` slice, the grant types and the scopes — all attacker-sized. This
is an unauthenticated, unbounded memory write: the classic shape of a slow
memory-exhaustion DoS, and there is no rate limiter mounted to slow it
(`middleware.audit.md` finding 1).

**The request body is unbounded.** `json.NewDecoder(r.Body)` at `:418` with no
`http.MaxBytesReader`. `RedirectURIs`, `GrantTypes` and `AllowedScopes` are
unbounded slices of unbounded strings, so one request can allocate as much as the
client is willing to send.

Persistence is available but off by default. `PersistClients` (`:37-39`) writes the
record through `OAuthRegisterClient` (`:472-486`) and `lookupOrFetchClient`
(`:1108-1135`) reads it back, so a registration can outlive the process — but with
the default `false`, every registration is lost on restart and invisible to other
replicas, and with it set the rehydrated record is incomplete in a
security-relevant way (finding 34).

### Remediation

1. **Gate registration.** Either require an initial access token (RFC 7591 §3.1) —
   ```go
   if s.cfg.RegistrationAccessToken != "" {
       if subtle.ConstantTimeCompare([]byte(bearerFrom(r)), []byte(s.cfg.RegistrationAccessToken)) != 1 {
           writeOAuthError(w, "invalid_token", "", http.StatusUnauthorized)
           return
       }
   }
   ```
   — or keep it open and bound it hard: a per-IP registration rate limit plus a
   global cap on `len(s.clients)`, rejecting with `503` once reached.
2. **Intersect the requested scopes** against what the server is willing to grant:
   ```go
   allowedScopes = intersectScopes(req.AllowedScopes, s.cfg.GrantableScopes)
   if len(allowedScopes) == 0 { allowedScopes = s.cfg.DefaultScopes }
   ```
   Never store a scope the server did not sanction.
3. **Expire clients.** Give `oauthClient` a `CreatedAt` and a `LastUsedAt`, and
   prune in `cleanupExpired` alongside `pending` and `codes`. For clients that must
   outlive the process, persist them the way codes are persisted.
4. **Bound the body and the slices:**
   `r.Body = http.MaxBytesReader(w, r.Body, 32<<10)`, plus explicit limits on the
   number of redirect URIs (say 8) and their length, and `dec.DisallowUnknownFields()`
   so a typo'd field is an error rather than a silent default.
5. Validate each `redirect_uri` at registration: absolute, no fragment, and
   `https` unless the host is `localhost`. The exact-match check at `:562` makes
   registration the only place this can be enforced.

---

## 12. High — `JWTAuthenticator` issues a forgeable bearer token and verifies nothing

```go
// providers.go:687-706
	// TODO: Verify password
	// if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
	//     return nil, fmt.Errorf("invalid credentials")
	// }

	// Generate token (placeholder - implement JWT signing when library is available)
	expiresAt := time.Now().Add(24 * time.Hour)
	tokenString := fmt.Sprintf("token_%d_%d", user.ID, expiresAt.Unix())

	return &LoginResponse{
		Token: tokenString,
		...
```

and identically in Direct mode:

```go
// providers_direct.go:447-451
	expiresAt := time.Now().Add(24 * time.Hour)
	tokenString := fmt.Sprintf("token_%d_%d", userID, expiresAt.Unix())

	return &LoginResponse{
		Token: tokenString,
```

The "token" is `token_<user_id>_<unix_expiry>`. It is not signed, not random, and
contains no secret — anyone can construct `token_1_1790000000` for user 1. There
is no JWT anywhere in the type despite the name; the doc comment at `:692` calls
it a placeholder.

The counterpart is unimplemented:

```go
// providers.go:741-754
func (a *JWTAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("authorization header required")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return nil, fmt.Errorf("bearer token required")
	}

	// TODO: Implement JWT parsing when library is available
	return nil, fmt.Errorf("JWT parsing not implemented - install github.com/golang-jwt/jwt/v5")
}
```

So this provider **cannot authenticate any request**: it fails closed, always.
That is what keeps this at High rather than Critical — a deployment wiring
`JWTAuthenticator` into `NewAuthMiddleware` returns 401 to everyone and the
problem is discovered immediately.

The exposure is the seam between the two. `Login` mints tokens that are accepted
by nothing in this package, so a deployment that wants JWT login has to pair
`JWTAuthenticator.Login` with a verifier of its own — and the tokens it is handed
carry no signature to verify. `jwtLogoutDirect` compounds the impression that the
flow is complete by inserting the token into a blacklist table
(`providers_direct.go:464-473`) that `Authenticate` never reads.

`Login` also does not verify the password (finding 1) and dereferences `a.getDB()`
at `providers.go:654` without a nil check, panicking rather than erroring when no
database is configured.

### Remediation

Pick one of two honest outcomes.

**Implement it.** Add `github.com/golang-jwt/jwt/v5`, sign with RS256 using a
configured key (the OAuth server already manages one — `oauth_server.go:140`,
`:175-197` — and exposes JWKS at `:343`, so reuse that key and key ID rather than
introducing a second):

```go
claims := jwt.MapClaims{
    "sub": strconv.Itoa(user.ID), "iss": a.issuer, "aud": a.audience,
    "iat": now.Unix(), "exp": now.Add(a.ttl).Unix(), "jti": newJTI(),
    "roles": user.Roles,
}
tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
signed, err := tok.SignedString(a.signingKey)
```

and in `Authenticate` parse with an explicit algorithm allow-list
(`jwt.WithValidMethods([]string{"RS256"})`) — never trust the token's own `alg` —
validating `iss`, `aud` and `exp`, then check `jti` against the blacklist table
`jwtLogoutDirect` already writes.

**Or remove it.** If JWT is not a supported mode, delete `JWTAuthenticator`
rather than shipping a type whose `Login` hands out forgeable credentials. A
constructor that returns `nil, errors.New("JWT authentication is not
implemented")` is safer than one that returns a working-looking token.

Either way, do not leave `Login` issuing `token_<id>_<ts>`. If the type must stay
as a stub, make `Login` fail the same way `Authenticate` does.

---

## 34. High — a persisted client is rehydrated without its secret, so confidential clients become public

The token endpoint gates client authentication on whether the client has a stored
secret:

```go
// oauth_server.go:77-79
func (c *oauthClient) isConfidential() bool {
	return c.ClientSecretHash != ""
}
```

```go
// oauth_server.go:805-812
	// Confidential clients (those registered with a client_secret) must authenticate;
	// public clients keep relying on PKCE alone, unchanged from prior behavior.
	if client, ok := s.lookupOrFetchClient(r.Context(), clientID); ok && client.isConfidential() {
		if _, err := s.authenticateClient(r); err != nil {
			writeOAuthError(w, "invalid_client", err.Error(), http.StatusUnauthorized)
			return
		}
	}
```

Registration stores the hash correctly — `ClientSecretHash: secretHash` at
`:468`, persisted through `OAuthRegisterClient` at `:472-486` — and the in-memory
copy in `s.clients` carries it. But the read-through path does not:

```go
// oauth_server.go:1108-1135
// lookupOrFetchClient checks in-memory first, then DB if PersistClients is enabled.
func (s *OAuthServer) lookupOrFetchClient(ctx context.Context, clientID string) (*oauthClient, bool) {
	s.mu.RLock()
	c, ok := s.clients[clientID]
	s.mu.RUnlock()
	if ok {
		return c, true
	}

	if !s.cfg.PersistClients || s.auth == nil {
		return nil, false
	}

	dbClient, err := s.auth.OAuthGetClient(ctx, clientID)
	if err != nil {
		return nil, false
	}

	c = &oauthClient{
		ClientID:      dbClient.ClientID,
		RedirectURIs:  dbClient.RedirectURIs,
		ClientName:    dbClient.ClientName,
		GrantTypes:    dbClient.GrantTypes,
		AllowedScopes: dbClient.AllowedScopes,
	}
	s.mu.Lock()
	s.clients[clientID] = c
	s.mu.Unlock()
	return c, true
}
```

`ClientSecretHash` and `TokenEndpointAuthMethod` are **not copied**, even though
`OAuthServerClient` carries both (`oauth_server_db.go:11-19`) and the registration
path writes both (`:479-480`). So the rehydrated client has an empty hash, `isConfidential()` returns
false, and the guard at `:807` skips client authentication entirely. The incomplete
record is then written back into `s.clients` (`:1134`), so the wrong answer is
cached for the remaining life of the process.

This triggers whenever the in-memory map does not already hold the client:

* **after any restart or deploy** — every confidential client registered before the
  restart is treated as public from then on;
* **on every replica that did not handle the registration** — so in a
  multi-instance deployment it is the normal case, not the exceptional one;
* **after the first eviction**, if the pruning recommended in finding 11 is added
  without fixing this.

The practical effect is that a confidential client's `client_secret` stops being
required at the token endpoint. Two mitigations keep this at High rather than
Critical. PKCE is mandatory for every client — `code_challenge` is required at the
authorize endpoint (`:549-551`) and the verifier is checked on both the persisted
and in-memory code paths (`:831`, `:859`) — so an attacker still needs the
`code_verifier` that the legitimate client generated. And the code is bound to its
`client_id` and `redirect_uri` (`:851-857`). What is lost is the independent control
RFC 6749 §3.2.1 requires for confidential clients: possession of the secret. An
attacker who obtains a code through any channel that does not also yield the
verifier — a logged query string, a referrer leak, an open redirector on a
registered URI — no longer faces the secret as a second barrier.

There is a matching availability bug in the same defect: a *legitimate* confidential
client that presents the correct secret after a restart is rejected, because
`authenticateClient` refuses a client that is not confidential
(`:1255-1258`). It does not surface, only because `:807` never calls it.

### Remediation

Copy the whole record:

```go
	c = &oauthClient{
		ClientID:                dbClient.ClientID,
		RedirectURIs:            dbClient.RedirectURIs,
		ClientName:              dbClient.ClientName,
		GrantTypes:              dbClient.GrantTypes,
		AllowedScopes:           dbClient.AllowedScopes,
		ClientSecretHash:        dbClient.ClientSecretHash,
		TokenEndpointAuthMethod: dbClient.TokenEndpointAuthMethod,
	}
```

Then make the omission structurally impossible and the gate fail closed:

1. **Convert, don't hand-copy.** Give `OAuthServerClient` a
   `func (c *OAuthServerClient) toOAuthClient() *oauthClient` used by both the
   registration and the fetch path, so a field added to one is added to both. Field
   lists duplicated across a persistence boundary drift; this one already has.
2. **Do not infer confidentiality from a field that can be silently empty.**
   Persist `TokenEndpointAuthMethod` and decide from it:
   ```go
   func (c *oauthClient) isConfidential() bool {
       return c.TokenEndpointAuthMethod != "" && c.TokenEndpointAuthMethod != "none"
   }
   ```
   and refuse to serve a client whose method requires a secret but whose hash is
   empty, rather than downgrading it:
   ```go
   	if c.isConfidential() && c.ClientSecretHash == "" {
   		logger.Error("client %s loaded without a secret hash; refusing", c.ClientID)
   		return nil, false
   	}
   ```
3. **Add the regression test that would have caught it:** register a confidential
   client with `PersistClients` enabled, construct a *new* `OAuthServer` over the
   same authenticator, and assert that a code exchange without
   `client_secret` is rejected with `invalid_client`. The existing OAuth tests all
   run against a single in-process server, which is why the hydration path is
   untested.

Fix this together with finding 11: registration and rehydration are the two halves
of one record's lifecycle, and finding 11's pruning recommendation makes this path
hot rather than restart-only.

---

## 13. Medium — the refresh-token grant authenticates no client

```go
// oauth_server.go:871-900 (abridged)
func (s *OAuthServer) handleRefreshGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	providerName := r.FormValue("provider")
	clientID := r.FormValue("client_id")
	if refreshToken == "" {
		writeOAuthError(w, "invalid_request", "refresh_token required", http.StatusBadRequest)
		return
	}

	// Try external providers first, then fall back to DatabaseAuthenticator
	provider := s.providerByName(providerName)
	if provider != nil {
		loginResp, err := provider.auth.OAuth2RefreshToken(r.Context(), refreshToken, providerName)
		...
		s.writeOAuthToken(w, r, loginResp.Token, loginResp.RefreshToken, clientID, nil, false)
		return
	}

	if s.auth != nil {
		loginResp, err := s.auth.RefreshToken(r.Context(), refreshToken)
		...
```

Compare the authorization-code grant, which does authenticate confidential
clients:

```go
// oauth_server.go:805-812
	// Confidential clients (those registered with a client_secret) must authenticate;
	// public clients keep relying on PKCE alone, unchanged from prior behavior.
	if client, ok := s.lookupOrFetchClient(r.Context(), clientID); ok && client.isConfidential() {
		if _, err := s.authenticateClient(r); err != nil {
			writeOAuthError(w, "invalid_client", err.Error(), http.StatusUnauthorized)
			return
		}
	}
```

The refresh path has no equivalent. `clientID` is read from the form and used only
as a pass-through to `writeOAuthToken`; it is never looked up, never authenticated,
and never compared against the client the refresh token was issued to. RFC 6749
§6 requires client authentication for confidential clients on refresh, and §10.4
requires the refresh token be bound to the client it was issued to.

Consequence: a refresh token that leaks — from a log, a referrer, a compromised
public client — can be redeemed by anyone, including as though it belonged to a
different (confidential) client, yielding a fresh access token and a fresh refresh
token. There is also no rotation check: `writeOAuthToken` is handed whatever the
underlying authenticator returns, so a replayed refresh token is not detected.

Two smaller issues in the same function: `providerName` comes from the form and
selects which external provider handles the token (`:881`), so a caller chooses
the validation path for a credential; and `err.Error()` from the provider is
echoed to the client at `:885` and `:895` (finding 18).

### Remediation

```go
func (s *OAuthServer) handleRefreshGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	if refreshToken == "" || clientID == "" {
		writeOAuthError(w, "invalid_request", "refresh_token and client_id required", http.StatusBadRequest)
		return
	}

	client, ok := s.lookupOrFetchClient(r.Context(), clientID)
	if !ok {
		writeOAuthError(w, "invalid_client", "", http.StatusUnauthorized)
		return
	}
	if client.isConfidential() {
		if _, err := s.authenticateClient(r); err != nil {
			writeOAuthError(w, "invalid_client", "", http.StatusUnauthorized)
			return
		}
	}
	// … and verify the stored refresh token's client_id == clientID before use
```

That last line needs storage support: the refresh token must record its issuing
client. `OAuthExchangeCode` already persists `ClientID` alongside a code
(`:824`), so the same shape applies.

Also rotate on use — issue a new refresh token, invalidate the old one, and treat
reuse of an invalidated token as a compromise signal (RFC 6749 §10.4 / OAuth 2.1
§6.1). And bind the provider to the token rather than to a form field: store
`ProviderName` with the token and ignore `r.FormValue("provider")`.

---

## 14. Medium — both security maps grow without bound, keyed on tokens and claims

```go
// provider.go:54-61
type SecurityList struct {
	provider SecurityProvider

	ColumnSecurityMutex sync.RWMutex
	ColumnSecurity      map[string][]ColumnSecurity
	RowSecurityMutex    sync.RWMutex
	RowSecurity         map[string]RowSecurity
}
```

Entries are inserted at `provider.go:393` and `:438` and **never removed**. There
is no TTL, no size cap and no eviction; `ClearSecurity` (`:397-417`) is the only
delete path and it has zero callers (finding 29) and does not touch `RowSecurity`
at all.

Growth is driven by the key. Column security keys on the integer user ID:

```go
// provider.go:381
	secKey := fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)
```

so that map is bounded by `users × tables` — large but finite. Row security keys
on the opaque ref:

```go
// provider.go:430
	secKey := fmt.Sprintf("%s.%s@%v", pSchema, pTablename, pUserRef)
```

and `pUserRef` is the whole `*UserContext` for every spec adapter
(`restheadspec/security_hooks.go:83-89`). `%v` on a pointer-to-struct renders
every field, so the key is a string like

```
public.orders@&{1 alice 0 sess_9f3c… 42 10.0.0.7 [user] alice@example.com map[email:… exp:1.76e+09 sub:…] map[] false 0 }
```

Three consequences:

* **Unbounded cardinality when claims vary.** Any field that changes per session
  mints a new permanent entry. `SessionID` alone guarantees this: a user who logs
  in twice has two keys; a user who logs in daily for a year has 365. With
  external OIDC providers, `Claims` is the entire userinfo map
  (`oauth2_methods.go:356`), so a refreshed `exp` or `iat` does the same. Map
  iteration order does not matter here — `fmt` prints map keys sorted since Go
  1.12 — but the values themselves vary, and that is enough.
* **Credentials held in memory indefinitely.** Each key string contains a session
  token and the user's email. They persist for the process lifetime, survive
  logout, and appear in any heap dump or core file.
* **Nothing is ever read from these maps anyway** — finding 6 shows both loaders
  query the provider unconditionally, so this is pure accumulation with no
  benefit.

### Remediation

The key and the lifetime both need fixing.

1. **Key on a stable scalar.** Row security needs a *user identity*, not a
   snapshot of the request. Derive it explicitly:
   ```go
   func securityKey(schema, table string, ref any) string {
       switch v := ref.(type) {
       case *UserContext:
           return fmt.Sprintf("%s.%s@%d", schema, table, v.UserID)   // or v.Claims["sub"]
       case int:
           return fmt.Sprintf("%s.%s@%d", schema, table, v)
       case string:
           return fmt.Sprintf("%s.%s@%s", schema, table, v)
       default:
           return fmt.Sprintf("%s.%s@%v", schema, table, v)
       }
   }
   ```
   For deployments with non-integer identities, read the subject claim — that is
   what `GetUserRef`'s doc comment (`hooks.go:17-21`) intends. A redacting
   `String()` on `UserContext` (finding 7) also fixes the default branch.
2. **Bound the maps.** Once finding 6 introduces real caching, the entries need a
   TTL and a cap. `pkg/cache` provides both (`MaxSize` with LRU eviction,
   `cache/provider_memory.go:105-109`, `:307-326`) plus tag-based invalidation, which
   would let a rule change invalidate `table:orders` across all users at once.
3. **Extend `ClearSecurity` to `RowSecurity`** and call it on logout and on rule
   changes, or delete it (finding 29) once a TTL makes it redundant.

---

## 15. Medium — model rules fail open, and any authenticated user may do anything

Two layers of authorisation both default to "allow".

**Unregistered models allow writes.** `checkModelUpdateAllowed` and
`checkModelDeleteAllowed` are the `BeforeUpdate`/`BeforeDelete` hooks
(`restheadspec/security_hooks.go:49-58`):

```go
// hooks.go:274-294
func checkModelUpdateAllowed(secCtx SecurityContext) error {
	rules, ok := GetModelRulesFromContext(secCtx.GetContext())
	if !ok {
		schema := secCtx.GetSchema()
		entity := secCtx.GetEntity()
		var err error
		if schema != "" {
			rules, err = modelregistry.GetModelRulesByName(fmt.Sprintf("%s.%s", schema, entity))
		}
		if err != nil || schema == "" {
			rules, err = modelregistry.GetModelRulesByName(entity)
		}
		if err != nil {
			return nil // model not registered, allow by default
		}
	}
	if !rules.CanUpdate {
		return fmt.Errorf("update not allowed for %s", secCtx.GetEntity())
	}
	return nil
}
```

`checkModelDeleteAllowed` is identical (`:298-318`, fail-open at `:311`). A model
served by the spec handler but absent from the registry — or present under a name
the two lookups do not produce — is fully writable. This is the consuming side of
`modelregistry.audit.md` finding 1 (*registry side fixed 2026-09-30: `checkModelUpdateAllowed`/`checkModelDeleteAllowed` now allow only on `ErrModelNotFound`*): the registry's lookup failure and this
`return nil` combine into "unknown model ⇒ permitted".

**Any authenticated user may perform any operation.** `CheckModelAuthAllowed` is
the `BeforeHandle` hook (`restheadspec/security_hooks.go:14-22`), and its final
step is:

```go
// hooks.go:356-365
	if operation == "delete" && rules.CanPublicDelete {
		return nil
	}

	userID, _ := secCtx.GetUserID()
	if userID == 0 {
		return fmt.Errorf("authentication required")
	}
	return nil
}
```

The only distinction it draws is authenticated vs. guest. `UserContext.Roles` and
`UserLevel` are populated by every authenticator and consulted nowhere in this
function. The doc comment is candid about it — *"8. Authenticated user → allow
(operation-specific checks remain in BeforeUpdate/BeforeDelete)"* (`:332`) — but
`BeforeUpdate`/`BeforeDelete` check only the model's global `CanUpdate`/`CanDelete`
flags, which are per-model, not per-role. So `rules.CanUpdate == true` means
*every* logged-in user may update *every* row, subject only to row security —
which finding 2 shows is inert. Read and create have no per-operation check at all.

Note also that `userID == 0` is the guest marker (`middleware.go:46`), so a real
user whose ID is genuinely 0 is treated as unauthenticated, and conversely any
authenticator that fails to populate `UserIDKey` yields `userID == 0` from the
ignored-error `GetUserID` at `:360` and is correctly denied — the one place the
fail-closed direction holds.

### Remediation

1. **Fail closed on unknown models.** An operation on a model with no rules should
   be refused, not permitted:
   ```go
   	rules, ok := resolveModelRules(secCtx)
   	if !ok {
   		return fmt.Errorf("no security rules registered for %s.%s", secCtx.GetSchema(), secCtx.GetEntity())
   	}
   ```
   If that is too strict for existing deployments, make it configurable and
   default to closed — with a startup log listing every served model that has no
   rules, so the gap is visible at deploy time rather than at exploit time.
   `resolveModelRules` (`:369-390`) already centralises the lookup, so the change
   is one place plus the two write checks.
2. **Add role checks.** `ModelRules` needs per-operation role requirements —
   `ReadRoles`, `CreateRoles`, `UpdateRoles`, `DeleteRoles` — and
   `CheckModelAuthAllowed` should require intersection with `userCtx.Roles`:
   ```go
   	required := rules.RolesFor(operation)
   	if len(required) > 0 && !hasAnyRole(userCtx.Roles, required) {
   		return fmt.Errorf("operation %s on %s requires one of %v", operation, entity, required)
   	}
   ```
   Until that exists, the package's authorisation model is "logged in ⇒ trusted",
   and row security is the only tenant boundary — which makes finding 2 the whole
   of the access-control story.
3. `GetModelRulesFromContext` is populated by `NewModelAuthMiddleware`
   (`middleware.go:225`), which is per-model. Verify that every route that reaches
   these hooks goes through it; where it does not, the fallback registry lookup at
   `:277-288` is the only path and its two name forms must both be correct.

---

## 16. Medium — there is no audit log

`logDataAccess` is registered as an `AfterRead` hook by every spec
(`restheadspec/security_hooks.go:43-46`) and is this:

```go
// hooks.go:198-218
func logDataAccess(secCtx SecurityContext) error {
	userID, _ := secCtx.GetUserID()

	logger.Info("AUDIT: User %d accessed %s.%s",
		userID,
		secCtx.GetSchema(),
		secCtx.GetEntity(),
	)

	// TODO: Write to audit log table or external audit service
	// auditLog := AuditLog{
	//     UserID:    userID,
	//     Schema:    secCtx.GetSchema(),
	//     Entity:    secCtx.GetEntity(),
	//     Action:    "READ",
	//     Timestamp: time.Now(),
	// }
	// db.Create(&auditLog)

	return nil
}
```

What this is: a log line. What it is not, and what a control named `AUDIT` implies:
durable, queryable, tamper-evident, and complete. Specifically —

* It records reads only. There is no equivalent hook on create, update or delete,
  so the operations that change data are not audited at all.
* It records no row identity, no filter, no column list and no result count, so it
  cannot answer "which records did this user see?" — the question an audit log
  exists to answer.
* It goes to the application log, which rotates and is not tamper-evident.
* `Info` is not forwarded to the error tracker (`logger/logger.go:100-106`), so
  it exists in exactly one place, on the host.
* It fires on the read path of every request, so it doubles log volume for no
  retrievable benefit.

For any deployment with a compliance obligation (SOC 2 CC7, PCI DSS 10, HIPAA
§164.312(b)), this does not satisfy it, and the `AUDIT:` prefix makes it look as
though it might.

### Remediation

Either build it or stop calling it an audit log.

To build it: write to a dedicated append-only table through the same
`SecurityProvider` abstraction, and cover writes as well as reads:

```go
type AuditEvent struct {
	At          time.Time
	UserID      int
	SessionID   string     // hash, not the token — see finding 7
	Operation   string     // read | create | update | delete
	Schema      string
	Entity      string
	RecordIDs   []string   // primary keys touched
	Filters     string     // normalised, no values
	RowCount    int
	RequestID   string
	RemoteAddr  string
	Outcome     string     // allowed | denied
}

type AuditSink interface { Record(ctx context.Context, e AuditEvent) error }
```

Register it on `AfterRead`, `AfterCreate`, `AfterUpdate`, `AfterDelete` **and** on
the deny paths (`hooks.go:94`, `:291`, `:315`, `:339`, `:362`) — a denied attempt
is the event most worth recording. Write asynchronously through a bounded queue so
the sink cannot become the throughput ceiling the way finding 6's mutexes are, and
decide explicitly whether a full queue drops events or fails the request; for a
compliance audit log it must be the latter.

If building it is out of scope, rename the function and drop the `AUDIT:` prefix so
nothing reads as a control that is not one, and say plainly in
`SECURITY_FEATURES.md` that audit logging is the integrator's responsibility.

---

## 17. Medium — identifier validation exists but is never called

The package interpolates configurable names into SQL in dozens of places:

```go
// providers.go:413
			query := fmt.Sprintf(`SELECT p_success, p_error, p_user::text FROM %s($1, $2)`, a.sqlNames.Session)
```

```go
// providers_direct.go:216-220
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT s.user_id, u.username, u.email, u.user_level, u.roles, u.program_user_id, u.program_user_table
			 FROM %s s JOIN %s u ON s.user_id = u.id
			 WHERE s.session_token = ? AND s.expires_at > ? AND u.is_active = ?`,
			a.tableNames.UserSessions, a.tableNames.Users))
```

and validators for exactly this exist:

```go
// sql_names.go:9
var validSQLIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
```

```go
// sql_names.go:242-258
// ValidateSQLNames checks that all non-empty fields in names are valid SQL identifiers.
// Returns an error if any field contains invalid characters.
func ValidateSQLNames(names *SQLNames) error {
	v := reflect.ValueOf(names).Elem()
	...
		if val != "" && !validSQLIdentifier.MatchString(val) {
			return fmt.Errorf("SQLNames.%s contains invalid characters: %q", typ.Field(i).Name, val)
		}
```

Grepping the whole repository for `ValidateSQLNames` and `ValidateTableNames`
returns their own definitions and their tests — **no production caller**. The
resolvers that every constructor goes through skip them:

```go
// sql_names.go:262-267
func resolveSQLNames(override ...*SQLNames) *SQLNames {
	if len(override) > 0 && override[0] != nil {
		return MergeSQLNames(DefaultSQLNames(), override[0])
	}
	return DefaultSQLNames()
}
```

```go
// table_names.go:99-101
func resolveTableNames(override *TableNames) *TableNames {
	return MergeTableNames(DefaultTableNames(), override)
}
```

So a `SQLNames`/`TableNames` override flows unvalidated into `fmt.Sprintf`. These
values come from application configuration, not from a request, which caps the
severity — this is not remote SQL injection. But it is injection-by-configuration:
a name assembled from an environment variable or a tenant identifier becomes SQL,
and the guard written to prevent it is inert. This is another instance of
`_CROSS-CUTTING` finding X10 — a control that is implemented, tested, and never
installed.

Note also that the regex rejects schema-qualified names: `public.users` fails
`^[a-zA-Z_][a-zA-Z0-9_]*$`. Since validation never runs, deployments using
`"myschema.users"` work today and would break the moment it is wired in — so the
fix needs the regex widened (or a per-part check) at the same time.

### Remediation

Call the validators where the names enter, and fail at construction rather than at
query time:

```go
func resolveSQLNames(override ...*SQLNames) (*SQLNames, error) {
	n := DefaultSQLNames()
	if len(override) > 0 && override[0] != nil {
		n = MergeSQLNames(n, override[0])
	}
	if err := ValidateSQLNames(n); err != nil {
		return nil, err
	}
	return n, nil
}
```

That changes the signature of the constructors that call it
(`NewDatabaseAuthenticatorWithOptions`, `NewDatabaseColumnSecurityProvider` at
`providers.go:771`, and the row-security and keystore equivalents). Several
already return only a value; those become `(T, error)`. Rejecting a bad
configuration at startup is worth the churn.

Widen the identifier rule to allow an optional schema qualifier, validating each
part:

```go
var validSQLIdentifierPart = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validIdentifier(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) > 2 { return false }
	for _, p := range parts {
		if !validSQLIdentifierPart.MatchString(p) { return false }
	}
	return true
}
```

and quote on interpolation (`pq.QuoteIdentifier` or the dialect equivalent) so the
regex is defence in depth rather than the only line.

---

## 18. Medium — internal error text is returned to unauthenticated clients

```go
// middleware.go:82-91
func authenticateRequest(w http.ResponseWriter, r *http.Request, provider SecurityProvider) (*http.Request, bool) {
	// Call the provider's Authenticate method
	userCtx, err := provider.Authenticate(r)
	if err != nil {
		http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
		return nil, false
	}
```

The same pattern at `middleware.go:203` (`NewAuthMiddleware`) and `:266`
(`NewModelAuthMiddleware`), and in the OAuth server at `oauth_server.go:809`,
`:885` and `:895`.

`err` is whatever the provider produced, and the providers wrap freely:

* `"session query failed: %w"` (`providers.go:417`) — carries the driver's error,
  so a malformed query or a missing procedure returns the PostgreSQL message,
  including the procedure name and sometimes the SQL.
* `"%s", errorMsg.String` (`providers.go:422`) — the stored procedure's own text,
  verbatim, to the client.
* `"failed to parse user context: %w"` (`:434`) — a JSON error quoting the
  offending input, which is the session payload.
* `"invalid user ID: %w"` (`:53`) — `strconv`'s message including the value.

An unauthenticated attacker learns the database dialect, the procedure naming
scheme, whether a given procedure exists, and whether a failure was "no such
session" versus "database unreachable" — a clean oracle for probing
`SQLNames`/`TableNames` configuration and for distinguishing valid from invalid
tokens.

### Remediation

Separate what the client is told from what is recorded:

```go
func authenticateRequest(w http.ResponseWriter, r *http.Request, provider SecurityProvider) (*http.Request, bool) {
	userCtx, err := provider.Authenticate(r)
	if err != nil {
		logger.Debug("authentication failed for %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	if userCtx == nil {
		logger.Error("authenticator returned nil user context with nil error")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}
	return setUserContext(r, userCtx), true
}
```

`Debug` rather than `Warn` on the log side is deliberate: failed authentication is
attacker-triggerable, so at `Warn` it becomes a Sentry flood (X8). Attach a
request ID to the log line and return it in the response if operators need to
correlate a specific 401.

Apply the same shape at `middleware.go:203`, `:266`, and to the OAuth handlers —
`writeOAuthError` should send the RFC 6749 error code (`invalid_client`,
`invalid_grant`) with an empty or fixed `error_description`, never `err.Error()`.
`oauth_server.go:825` and `:852` already do exactly that (`writeOAuthError(w,
"invalid_client", "", …)`), so the correct pattern is already in the file; `:809`,
`:885` and `:895` are the outliers.

The `userCtx == nil` guard also closes finding 32.

---

## 19. Medium — a panicking driver silently downgrades the authenticator to Direct mode

```go
// query_mode.go:53-72
// probeFunctionExists checks, via a Postgres-specific system catalog query,
// whether a function named procName exists. Any error (wrong dialect,
// placeholder syntax rejected, relation missing, etc.) is treated as "does
// not exist" rather than propagated - the probe must never be able to panic
// or block resolution of the query mode.
func probeFunctionExists(ctx context.Context, db *sql.DB, procName string) bool {
	if db == nil {
		return false
	}
	var exists bool
	defer func() {
		// Guard against any unexpected panic from a misbehaving driver.
		_ = recover()
	}()
	row := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = $1 LIMIT 1)`, procName)
	if err := row.Scan(&exists); err != nil {
		return false
	}
	return exists
}
```

The result is unnamed, so the recovered panic makes the function return `false` —
and `false` is not a neutral value here. It means "the stored procedure does not
exist", which makes `ShouldUseProcedure` select **Direct mode**
(`providers.go:404`, `:483`, `:211`, `:250`, and every other call site). Direct
mode is the code path documented as never verifying passwords
(`providers_direct.go:20-23`).

So a transient fault in the driver silently switches the whole authenticator from
the stored-procedure implementation to the one with weaker behaviour, with no log
line at all — `_ = recover()` discards the value. The same applies to the
ordinary error path at `:68`: a permissions error reading `pg_proc`, or a
connection blip during the probe, is indistinguishable from "procedure absent".

The result is cached in a `sync.Map` (`query_mode.go:36`) and only cleared by
`reset()` (`:46-51`), so one bad probe pins the downgraded mode until a reconnect.

The intent — "the probe must never be able to panic or block resolution" — is
sound. The problem is that the fallback direction is the less safe one, and the
event is invisible.

### Remediation

1. **Log it.** A discarded panic in security-relevant code is never acceptable:
   ```go
   	defer func() {
   		if r := recover(); r != nil {
   			logger.Error("probeFunctionExists(%s) panicked: %v", procName, r)
   		}
   	}()
   ```
   Same for the `Scan` error at `:68` — at `Debug` if it is expected on non-Postgres
   dialects, but not silent.
2. **Do not cache a failed probe.** Distinguish the three outcomes rather than
   collapsing them into a bool:
   ```go
   type probeResult int
   const (probeAbsent probeResult = iota; probePresent; probeUnknown)
   ```
   Cache `probeAbsent`/`probePresent`; retry on `probeUnknown`.
3. **Make the mode explicit in production.** `ModeAuto` is convenient for tests and
   ambiguous in deployment. Recommend — and document — setting
   `QueryMode: ModeProcedure` explicitly where the procedures are installed, so a
   probe failure produces an error instead of a silent behaviour change.
4. Log the resolved mode once at startup, per procedure name. "Using Direct mode
   for resolvespec_login" is exactly the line an operator needs and it does not
   exist today.

Most of this becomes moot once finding 1 is fixed and both modes verify passwords
— which is the real answer: the two modes should not differ in their security
properties, so that choosing between them cannot be a security event.

---

## 20. Medium — a fire-and-forget goroutine uses the request's context

```go
// providers.go:445-449
		// Update last activity asynchronously (don't block the request)
		if userCtx != nil {
			go a.updateSessionActivity(r.Context(), token)
		}
		return userCtx, nil
```

Two problems in one line.

**The context dies with the request.** `r.Context()` is cancelled when the handler
returns, which is typically before the goroutine gets scheduled. So
`updateSessionActivity`'s `ExecContext` (`:482-502`) is racing the response: some
fraction of these writes fail with `context canceled`, and which fraction depends
on load. The intent is clearly the opposite of what the code does — the comment
says "don't block the request", but detaching the work from the request also
detaches it from the request's lifetime. `context.WithoutCancel(r.Context())` (Go
1.21+) or `context.Background()` with an explicit timeout is what is wanted:

```go
		if userCtx != nil {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			go func() { defer cancel(); a.updateSessionActivity(ctx, token) }()
		}
```

**The goroutine is unbounded and untracked.** One per authenticated request, with
no semaphore, no wait group, and no panic recovery. `updateSessionActivity`
performs a DB write, so under load this spawns an unbounded number of concurrent
writers against the connection pool — the pool bounds the concurrency, but the
goroutines queue behind it without limit, and each holds a token string alive.
Nothing waits for them at shutdown, so in-flight activity updates are lost on a
restart; and any panic inside the goroutine (for example from a nil `a.getDB()`)
takes the whole process down, because a panic in a goroutine cannot be recovered
by the handler.

The pattern is worth fixing structurally, not per-call-site: session activity is a
write-heavy, low-value, idempotent update — exactly the workload for coalescing.

### Remediation

1. Use a detached context with a timeout, as above.
2. Recover inside the goroutine: `defer logger.CatchPanic("updateSessionActivity")()`
   — this is one of the few places `CatchPanic` is the right helper, because the
   goroutine has no results and no caller to mislead (contrast finding 3).
3. Better: coalesce. Keep a small in-memory map of `token → lastSeen`, updated
   under a mutex by the request path, and have **one** background goroutine flush
   it every 30 seconds in a single batched `UPDATE`. That turns N writes per
   second into one, bounds the goroutine count at 1, and makes the shutdown story
   trivial (flush once in `Close`). It also removes the write from the
   authentication hot path entirely.
4. Whichever route, register the worker with the server's shutdown sequence so the
   final flush happens before the process exits — see `server.audit.md` on the
   shutdown budget.

---

## 21. Medium — `cleanupStates` leaks one goroutine per OAuth2 configuration

```go
// oauth2_methods.go:74-86 (abridged)
func (a *DatabaseAuthenticator) ConfigureOAuth2Provider(name string, cfg OAuth2Config) {
	a.oauth2Mutex.Lock()
	defer a.oauth2Mutex.Unlock()
	if a.oauth2Configs == nil {
		a.oauth2Configs = make(map[string]OAuth2Config)
	}
	a.oauth2Configs[name] = cfg

	if !a.oauth2CleanupStarted {
		a.oauth2CleanupStarted = true
		go a.cleanupStates()
	}
}
```

and the worker:

```go
// oauth2_methods.go:337-351
func (a *DatabaseAuthenticator) cleanupStates() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.oauth2Mutex.Lock()
		for state, entry := range a.oauth2States {
			if time.Now().After(entry.ExpiresAt) {
				delete(a.oauth2States, state)
			}
		}
		a.oauth2Mutex.Unlock()
	}
}
```

`for range ticker.C` with no `ctx.Done()` case and no stop channel: the goroutine
runs until the process exits. The `oauth2CleanupStarted` flag correctly prevents
more than one per `DatabaseAuthenticator`, so this is not a per-request leak — but
there is no `Close` on `DatabaseAuthenticator` at all, so every authenticator ever
constructed keeps a live goroutine and keeps its entire `oauth2States` map, its
configs (including **client secrets**) and its DB handle reachable. Tests that
construct authenticators in a loop accumulate them; a long-running process that
rebuilds its authenticator on configuration reload accumulates them too.

Contrast `OAuthServer`, which does this correctly: a `done` channel created in the
constructor (`oauth_server.go:193`), selected on in the worker (`:256-257`), and
closed by an idempotent `Close` (`:204-211`).

A related overwrite hazard sits two lines up: `a.oauth2Configs[name] = cfg` at
`:80` replaces an existing provider configuration silently. Since `providerName`
is chosen by the client on the refresh path (finding 13), a mis-keyed
reconfiguration silently redirects token validation to a different provider.

### Remediation

Give `DatabaseAuthenticator` a lifecycle, mirroring `OAuthServer`:

```go
type DatabaseAuthenticator struct {
	...
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func (a *DatabaseAuthenticator) startCleanup() {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				a.pruneStates()
			}
		}
	}()
}

func (a *DatabaseAuthenticator) Close() error {
	a.closeOnce.Do(func() { close(a.stopCh) })
	a.wg.Wait()
	return nil
}
```

Wire `Close` into the server's shutdown path, and add `defer logger.CatchPanic(…)()`
inside the worker so a panic in `pruneStates` does not kill the process.

Log an overwrite in `ConfigureOAuth2Provider` (`logger.Info`, not `Warn` —
reconfiguration is an operator action, not an error) so a duplicated provider name
is visible.

---

## 22. Medium — "no rules" is returned as an error, so a normal read is reported to Sentry

`ApplyColumnSecurity` treats an absent bucket as a failure:

```go
// provider.go:311-314
	colsecList, ok := m.ColumnSecurity[fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)]
	if !ok || colsecList == nil {
		return records, fmt.Errorf("nocolumn security data")
	}
```

and the hook logs that failure at `Warn`:

```go
// hooks.go:182-187
	maskedResult, err := securityList.ApplyColumnSecurity(resultValue, modelType, userID, schema, tablename)
	if err != nil {
		logger.Warn("Column security error: %v", err)
		// Don't fail the request, just log the issue
		return nil
	}
```

`logger.Warn` is forwarded to the error tracker (`logger/logger.go:118-122`), so
**every read of every table that has no column-masking rules produces a Sentry
event**. The absence of rules is the normal case — most tables mask nothing — so
this is not an edge case but the steady state. At any real request rate it exhausts
the error-tracking quota and buries the events that matter; the `Warn` also cannot
be tuned away without losing the genuine failures, because both arrive with the
same level and the only distinguishing feature is the error string
`"nocolumn security data"` (note the missing space, which makes it harder to grep
for than it should be).

Two things amplify it.

**In Direct mode, this fires on every read of every table.** Both database
providers refuse to operate without the stored procedures:

```go
// providers.go:810-813
func (p *DatabaseColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.ColumnSecurity) {
		return nil, ErrDirectModeUnsupported
	}
```

```go
// providers.go:926-929
func (p *DatabaseRowSecurityProvider) GetRowSecurity(ctx context.Context, userRef any, schema, table string) (RowSecurity, error) {
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.RowSecurity) {
		return RowSecurity{}, ErrDirectModeUnsupported
	}
```

`ErrDirectModeUnsupported` is explicit about the consequence — *"direct mode does
not support column/row security; requires the
resolvespec_column_security/resolvespec_row_security stored procedures"*
(`table_names.go:15`) — and it is honest, documented behaviour. But it means that
when the procedures are not installed, **all column masking and all row filtering
are unavailable**, and `loadSecurityRules` converts that into two `Warn` lines per
request:

```go
// hooks.go:46-64 (abridged)
	err := securityList.LoadColumnSecurity(secCtx.GetContext(), userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load column security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}
	...
	_, err = securityList.LoadRowSecurity(secCtx.GetContext(), userRef, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load row security: %v", err)
	}
```

So a Direct-mode deployment emits three Sentry events per secured read — two from
the loader, one from `ApplyColumnSecurity` — while silently serving unmasked,
unfiltered data. Finding 19 makes this reachable by accident: a single panicking or
erroring `pg_proc` probe downgrades a procedure-mode deployment into exactly this
state.

**A guest request adds a fourth.** `hooks.go:36` logs `"No user ID in context for
security check"` at `Warn` and returns nil, so every unauthenticated request that
reaches the hook is also a Sentry event — and unauthenticated requests are the ones
an attacker can send without limit (`middleware.audit.md` finding 1).

The row-security path is better behaved: `hooks.go:85-89` logs its "no rules" case
at `Debug` and returns nil. That is the right level — but it is also the reason a
genuine row-security lookup failure is invisible, which is finding 5's fail-open
seen from the logging side.

### Remediation

Return the distinction in the type rather than in the error, so the caller can tell
"no rules" from "lookup failed":

```go
// ApplyColumnSecurity masks the records in place. ok is false when no column rules
// are configured for this user and table, which is not an error.
func (m *SecurityList) ApplyColumnSecurity(...) (result reflect.Value, ok bool, err error)
```

and in the hook:

```go
	masked, ok, err := securityList.ApplyColumnSecurity(resultValue, modelType, userID, schema, tablename)
	switch {
	case err != nil:
		logger.Error("column security failed for %s.%s: %v", schema, tablename, err)
		return fmt.Errorf("unable to apply column security to %s.%s", schema, tablename)  // fail closed
	case !ok:
		return nil    // no rules configured: nothing to apply, nothing to log
	}
	if masked.IsValid() && masked.CanInterface() {
		secCtx.SetResult(masked.Interface())
	}
	return nil
```

Two things happen at once here and both matter: the normal case stops logging, and
a genuine failure stops being ignored. That second half is finding 5 — the change
of return type is what makes failing closed expressible.

For Direct mode specifically:

1. **Decide the policy explicitly at startup, not per request.** If the provider
   cannot supply rules, either refuse to start with security hooks registered, or
   register a no-op provider and log the decision *once*:
   ```go
   	if errors.Is(probeErr, ErrDirectModeUnsupported) {
   		logger.Warn("column/row security unavailable: %v; hooks will not be registered", probeErr)
   	}
   ```
   `Warn` is correct there — it is operator-facing, happens once, and is exactly
   what should reach the error tracker.
2. **Make `errors.Is(err, ErrDirectModeUnsupported)` a distinct branch** in
   `loadSecurityRules` so it is logged at `Debug` (or not at all, given the startup
   line) rather than as a per-request warning.
3. Fix the `"nocolumn security data"` message while there.
---

## 23. Medium — the OAuth signing key is ephemeral, so tokens do not survive a restart

```go
// oauth_server.go:175-196
	signingKey := cfg.SigningKey
	if signingKey == nil {
		var err error
		signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			// Signing keys are only required for id_token issuance (OIDC "openid" scope);
			// leaving signingKey nil degrades gracefully by omitting id_token/JWKS support.
			signingKey = nil
		}
	}
	...
	if signingKey != nil {
		s.signingKeyID = rsaKeyID(&signingKey.PublicKey)
	}
```

The fallback is correct in construction — 2048-bit RSA from `crypto/rand`, key ID
derived from the public key (`rsaKeyID`, `:1271`) — and `cfg.SigningKey` is the
documented way to supply a stable one. Two problems follow from the default path.

First, a key-generation failure is swallowed: `signingKey = nil` with no log line
at all, and `NewOAuthServer` returns a server that silently omits `id_token` and
serves an empty JWKS. `rsa.GenerateKey` failing means `crypto/rand` failed, which is
not a condition to degrade quietly through — and the comment explains the *design*
without acknowledging that the operator is never told.

Second, and the reason this finding exists, the generated key is per process:

* **Every restart invalidates every issued token.** Clients holding valid,
  unexpired access tokens get `invalid_token` after a deploy, with no way to
  distinguish that from a revocation.
* **Replicas disagree.** Two instances behind a load balancer generate different
  keys, so a token minted by one is rejected by the other — an intermittent,
  load-balancer-dependent 401 that is painful to diagnose.
* **JWKS churns.** `/.well-known/jwks.json` (`:343`) publishes the current public
  key only; a relying party that caches JWKS sees the key disappear rather than
  rotate, because there is no overlap period and no second key in the set.

Registered clients (finding 11) and authorization codes share the restart problem,
though codes have `PersistCodes` (`:818`) as an opt-in remedy.

### Remediation

1. **Make the key a required configuration item in production.** Load from a file
   or secret manager, and log loudly at startup when falling back to a generated
   key:
   ```go
   	logger.Warn("OAuth signing key not configured; generated an ephemeral key. " +
   		"Tokens will not survive a restart and will not validate across replicas.")
   ```
   (`Warn` is right here: it is not attacker-triggerable, it happens once, and it
   is exactly what should reach the error tracker.)
2. **Support two keys** — a current signing key and a previous verification key —
   and publish both in JWKS so rotation does not invalidate outstanding tokens.
   The `keyID` derivation already supports this; the JWKS handler needs to emit a
   set rather than a single entry.
3. Document the requirement next to `PersistCodes` in the OAuth section of the
   README, since the two have the same "works in dev, breaks in HA" character.

---

## 24. Medium — `contains` is prefix-or-suffix, not substring, and it decides the primary key

```go
// hooks.go:404-407
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && s[len(s)-len(substr):] == substr
}
```

This is `strings.HasPrefix(s, substr) || strings.HasSuffix(s, substr)`. It does not
search the middle. There are three callers, and each is wrong in a different way.

**Primary-key detection (`hooks.go:117`).** This is the consequential one, because
its output goes into a SQL `WHERE` clause:

```go
// hooks.go:112-125
		pkName := "id" // default
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			if tag := field.Tag.Get("bun"); tag != "" {
				// Check for primary key tag
				if contains(tag, "pk") || contains(tag, "primary_key") {
					if sqlName := extractSQLName(tag); sqlName != "" {
						pkName = sqlName
					}
					break
				}
			}
		}
```

`pkName` is substituted into the row-security template as `{PrimaryKeyName}`
(`provider.go:44`). The tag forms in this repository are:

| bun tag | `contains(tag,"pk")` | result |
| --- | --- | --- |
| `id,pk` | true (suffix) | detected |
| `pk,autoincrement` | true (prefix) | detected |
| `id,pk,autoincrement` | **false** | missed → `pkName` stays `"id"` |
| `column:user_id,pk` | true (suffix) | detected, but `extractSQLName` returns `"column:user_id"` |
| `pkid` | true (prefix) | **false positive** — a column named `pkid` is taken as the primary key |

The two failure rows both produce a wrong `{PrimaryKeyName}` in a row-security
filter. A missed primary key silently falls back to `"id"`, which is right often
enough to hide the bug and wrong exactly where it matters — a table whose key is
`uuid` or `tenant_id` gets a filter referencing a column that may not exist
(a SQL error, surfaced through finding 22's fail-open `Warn`) or, worse, one that
exists but is not the key.

**`extractSQLName`'s two uses (`hooks.go:414`, `:417`).**

```go
// hooks.go:409-422
func extractSQLName(tag string) string {
	parts := splitTag(tag, ',')
	for _, part := range parts {
		if part != "" && !contains(part, ":") {
			return part
		}
		if contains(part, "column:") {
			return part[7:] // Skip "column:"
		}
	}
	return ""
}
```

`contains(part, ":")` asks "does this part begin or end with a colon", not "does it
contain one". For `column:user_id` the colon is in the middle, so `contains` is
false, `!contains` is true, and the function returns `"column:user_id"` whole —
which then becomes `pkName`. The `column:` branch at `:417` is only reachable for a
part that *starts* with `column:`, which the branch above has already consumed, so
it is effectively dead.

This is live today: row security is inert because of finding 2, so a wrong `pkName`
has no effect yet. Fixing finding 2 without fixing this one turns a dormant
correctness bug into a live one, which is why finding 2's remediation and this one
should ship together.

### Remediation

Delete `contains`, `extractSQLName` and `splitTag`, and use the standard library
plus the helper that already parses these tags correctly
(`common/handler_utils.go:53-55`):

```go
func primaryKeyName(t reflect.Type) string {
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("bun")
		if tag == "" {
			continue
		}
		opts := strings.Split(tag, ",")
		isPK := false
		for _, o := range opts[1:] {
			if o == "pk" || o == "primary_key" {
				isPK = true
				break
			}
		}
		if !isPK {
			continue
		}
		if name := common.ExtractTagValue(tag, "column"); name != "" {
			return name
		}
		if opts[0] != "" && !strings.Contains(opts[0], ":") {
			return opts[0]
		}
		return strings.ToLower(t.Field(i).Name)
	}
	return "id"
}
```

Matching options exactly (`o == "pk"`) rather than by substring removes both the
missed-key and false-positive rows from the table above. Better still, take the
primary key from `pkg/reflection`, which already resolves it from the model
metadata rather than re-parsing tags — two implementations of tag parsing in one
repository is how they drift.

Add a test asserting `primaryKeyName` on a struct tagged `bun:"id,pk,autoincrement"`
and on one tagged `bun:"column:tenant_uuid,pk"`; both fail against the current code.

---

## 25. Medium — `maskString`'s offsets are off by one, and it masks at byte positions

```go
// provider.go:85-122 (abridged)
func maskString(pString string, maskStart, maskEnd int, maskChar string, invert bool) string {
	strLen := len(pString)
	middleIndex := (strLen / 2)
	newStr := ""
	if maskStart == 0 && maskEnd == 0 {
		maskStart = strLen
		maskEnd = strLen
	}
	...
	for index, char := range pString {
		if invert && index >= middleIndex-maskStart && index <= middleIndex { newStr += maskChar; continue }
		if invert && index <= middleIndex+maskEnd && index >= middleIndex { newStr += maskChar; continue }
		if !invert && index <= maskStart { newStr += maskChar; continue }
		if !invert && index >= strLen-1-maskEnd { newStr += maskChar; continue }
		newStr += string(char)
	}
	return newStr
}
```

Called from `setColSecValue` for string fields (`provider.go:275`) and for JSON
sub-fields (`:289`), with `colsec.MaskStart`, `colsec.MaskEnd`, `colsec.MaskChar`
and `colsec.MaskInvert` straight from the rule.

Measured behaviour, from a standalone run of this exact function:

| input | maskStart | maskEnd | invert | output |
| --- | --- | --- | --- | --- |
| `4111111111111234` | 4 | 0 | false | `*****1111111123*` |
| `4111111111111234` | 4 | 4 | false | `*****111111*****` |
| `4111111111111234` | 0 | 4 | false | `*1111111111*****` |
| `4111111111111234` | 0 | 0 | false | `****************` |
| `4111111111111234` | 4 | 4 | true | `4111*********234` |
| `alice@example.com` | 2 | 3 | false | `***ce@example****` |
| `Müller` | 2 | 1 | false | `**ll**` |
| `日本語テスト` | 2 | 1 | false | `*本語テスト` |

Three observations.

**The bounds are off by one, in the unsafe direction for `maskEnd` and the safe
direction for `maskStart`.** `index <= maskStart` masks `maskStart+1` characters
from the front, and `index >= strLen-1-maskEnd` masks `maskEnd+1` from the back —
so `MaskStart: 4` masks five characters, not four. Rows 1–3 show it: with
`maskEnd: 4`, five trailing digits are hidden; with `maskEnd: 0`, the final digit
is still masked even though zero was requested. A rule author calibrating "show
the last four digits of a card" gets the last three.

**`maskStart == 0 && maskEnd == 0` masks the entire string** (row 4), because both
are rewritten to `strLen`. That is the right default — an unconfigured rule hides
everything rather than revealing it — but it is undocumented and surprising, and
it means there is no way to express "mask nothing".

**The offsets are byte offsets, so non-ASCII text is masked at the wrong
positions — sometimes revealing data.** `for index, char := range pString` yields
the *byte* index of each rune, and `strLen` is `len()` in bytes. For `Müller` (7
bytes, 6 runes) the requested three-from-the-front only lands on two runes,
because byte index 2 is the continuation byte of `ü` and never appears as a loop
index. For `日本語テスト` (18 bytes, 6 runes, indices 0/3/6/9/12/15) the result is
worse: `index <= 2` matches only index 0, and `index >= 16` matches nothing, so
**one of six characters is masked** and five are returned in clear. A rule written
to hide most of a value hides one sixth of it as soon as the data is not Latin-1.
Any deployment masking names, addresses or free text in a non-English locale is
under-masking today.

The `newStr += maskChar` accumulation is also O(n²) in allocations, which is
irrelevant at field sizes but trivially avoidable.

### Remediation

Operate on runes, define the parameters as counts rather than indices, and fail
safe when they are contradictory:

```go
// maskString replaces characters in s with maskChar. maskStart is the number of
// leading characters to mask and maskEnd the number of trailing characters; when
// invert is set, the middle is masked and the edges are kept. Zero for both masks
// the whole string. If the masked regions meet or overlap, the whole string is
// masked.
func maskString(s string, maskStart, maskEnd int, maskChar string, invert bool) string {
	r := []rune(s)
	n := len(r)
	if maskChar == "" { maskChar = "*" }
	if maskStart <= 0 && maskEnd <= 0 { return strings.Repeat(maskChar, n) }
	if maskStart < 0 { maskStart = 0 }
	if maskEnd < 0 { maskEnd = 0 }

	var b strings.Builder
	b.Grow(len(s))
	for i := range r {
		masked := i < maskStart || i >= n-maskEnd
		if invert { masked = !masked }
		if masked {
			b.WriteString(maskChar)
		} else {
			b.WriteRune(r[i])
		}
	}
	return b.String()
}
```

Note that `invert` here means exactly "keep the edges, mask the middle", which is
what the current invert branches approximate via `middleIndex` — and the
`middleIndex` arithmetic has its own edge cases (`maskStart > middleIndex` makes
the lower bound negative, masking from index 0) that this formulation removes.

Two cautions for the rollout:

* This changes output for every configured rule, because of the off-by-one. Produce
  a before/after table for the rules in each deployment's `column_security` data
  and adjust `MaskStart`/`MaskEnd` by one where the old behaviour was the intended
  one.
* `provider_test.go:130-150` covers ASCII only. Add the `Müller` and `日本語テスト`
  cases above as fixtures so the rune handling cannot regress.

---

## 26. Low — both maps are read once before their mutex is taken

```go
// provider.go:301-311
func (m *SecurityList) ApplyColumnSecurity(records reflect.Value, modelType reflect.Type, pUserID int, pSchema, pTablename string) (reflect.Value, error) {
	defer logger.CatchPanic("ApplyColumnSecurity")()

	if m.ColumnSecurity == nil {
		return records, fmt.Errorf("security not initialized")
	}

	m.ColumnSecurityMutex.RLock()
	defer m.ColumnSecurityMutex.RUnlock()

	colsecList, ok := m.ColumnSecurity[...]
```

The same shape at `provider.go:442-452` (`GetRowSecurityTemplate`, reading
`m.RowSecurity` at `:445` before locking at `:449`) and at `:125-136`
(`ColumSecurityApplyOnRecord`, reading at `:127` before locking at `:136`).

Reading the map header outside the lock is a data race under the Go memory model
whenever another goroutine can assign the field. `LoadColumnSecurity` does exactly
that — it lazily creates the map — so a first-request race between a loader and a
reader is possible in principle. In practice the maps are created in the
constructor and only ever written *into*, not reassigned, which is why this has
not shown up: `go test -race` on the package passes. It is nevertheless
unsynchronised access to a field that another method assigns, and the race
detector will flag it the moment someone adds a `Clear`-style method that does
`m.ColumnSecurity = nil`.

The guard is also nearly useless: a nil map read returns the zero value and
`ok == false`, which the code immediately below already handles. The check exists
only to produce a different error message.

### Remediation

Move the check inside the lock, or delete it:

```go
	m.ColumnSecurityMutex.RLock()
	defer m.ColumnSecurityMutex.RUnlock()

	colsecList, ok := m.ColumnSecurity[securityKey(pSchema, pTablename, pUserID)]
	if !ok || len(colsecList) == 0 {
		return records, nil, nil   // see finding 22 on not returning this as an error
	}
```

Reading a nil map is legal, so dropping the pre-check loses nothing. Do the same
at `:127` and `:445`.

---

## 27. Low — the security maps and their mutexes are exported

```go
// provider.go:54-61
type SecurityList struct {
	provider SecurityProvider

	ColumnSecurityMutex sync.RWMutex
	ColumnSecurity      map[string][]ColumnSecurity
	RowSecurityMutex    sync.RWMutex
	RowSecurity         map[string]RowSecurity
}
```

`provider` is correctly unexported; everything else is public. Any package holding
a `*SecurityList` can read the maps without the lock, write to them without the
lock, take the mutex and forget to release it, or copy the struct — which copies a
`sync.RWMutex` and is a `go vet` error class of its own. The invariant "the map is
only touched while its mutex is held" is unenforceable from outside the package,
and `SecurityList` is reachable through `GetSecurityList()` from every spec
adapter.

There is no reason for the exposure: every legitimate operation already has a
method (`LoadColumnSecurity`, `ApplyColumnSecurity`, `LoadRowSecurity`,
`GetRowSecurityTemplate`, `ClearSecurity`).

### Remediation

```go
type SecurityList struct {
	provider SecurityProvider

	colMu   sync.RWMutex
	columns map[string][]ColumnSecurity
	rowMu   sync.RWMutex
	rows    map[string]RowSecurity
}
```

Add `func (m *SecurityList) Snapshot() (...)` if a consumer genuinely needs to
inspect the state, returning copies. A grep for `ColumnSecurityMutex` and
`RowSecurity` outside `pkg/security` shows no external users today, so this is a
rename with no downstream churn — cheap to do now, and it stops being cheap once
an integrator depends on it.

Also embed `_ noCopy` or keep `SecurityList` pointer-only by convention; `go vet`'s
`copylocks` check catches accidental value copies, but only if the package is
vetted in CI (see `_CROSS-CUTTING.audit.md` on the lint gate).

---

## 28. Low — three nested loops share the index name `i`, and one of them indexes the wrong slice

```go
// provider.go:144-185 (abridged, indentation preserved)
	for i := range colsecList {
		colsec := &colsecList[i]
		...
		for i, path := range colsec.Path {
			...
			for ri := range newRecords {
				...
				columnData := reflection.GetModelColumnDetail(newRecords[ri])
				lastColumnData := reflection.GetModelColumnDetail(lastRecords[ri])
				for i, cols := range columnData {
					if cols.SQLName != "" && strings.EqualFold(cols.SQLName, path) {
						...
						oldField = lastColumnData[i].FieldValue
						break
					}
					if cols.Name != "" && strings.EqualFold(cols.Name, path) {
						...
						oldField = lastColumnData[i].FieldValue
						break
					}
```

`i` is declared three times in nested scopes (`:144`, `:153`, `:170`), each
shadowing the last. The innermost `i` is what `:175` and `:182` use, which is
correct for `columnData` — but `lastColumnData` is a *separate* call to
`GetModelColumnDetail` on a *different* value (`lastRecords[ri]` versus
`newRecords[ri]`), and the code assumes the two slices are index-aligned.

They are aligned as long as both records are the same struct type, which
`ColumSecurityApplyOnRecord` verifies at `:130-133`. So this is not a live
mis-indexing today. It is fragile in two specific ways:

* `GetModelColumnDetail` walks embedded structs and relations; if it ever returns
  a different number of entries for two values of the same type — for example
  because a relation pointer is nil on one and populated on the other — the
  alignment breaks and `oldField` silently becomes a different column's value. In
  `ColumSecurityApplyOnRecord` that value is used to decide whether the field
  changed, so a mis-alignment would mean a modified protected column is treated as
  unmodified: an authorisation decision made on the wrong field.
* `lastColumnData[i]` is an unchecked index. If `lastColumnData` is shorter than
  `columnData`, this panics — and the panic is recovered by
  `logger.CatchPanic("ApplyColumnSecurity")` in the sibling function
  (`provider.go:302`), which returns the *unmasked* records with a nil error
  (finding 3).

The shadowing is what makes this hard to see: a reader checking whether `i` is the
right index has to count three scopes.

### Remediation

Rename the indices to say what they are, and look the old field up by name rather
than by position:

```go
	for ci := range colsecList {
		colsec := &colsecList[ci]
		for pi, path := range colsec.Path {
			for ri := range newRecords {
				columnData := reflection.GetModelColumnDetail(newRecords[ri])
				lastByName := indexByName(reflection.GetModelColumnDetail(lastRecords[ri]))
				for _, col := range columnData {
					if !matchesPath(col, path) { continue }
					oldField, ok := lastByName[strings.ToLower(col.SQLName)]
					if !ok {
						return cols, fmt.Errorf("column %q present in new record but not in previous", col.SQLName)
					}
					...
```

Matching by name removes the positional assumption entirely and turns a silent
mis-alignment into an explicit error. Note the `pi`/`pathLen` interaction at
`:155-159` also depends on the middle `i`; renaming makes that logic legible too.

`go vet -shadow` (or `golangci-lint`'s `govet` with `shadow` enabled) catches this
class; it is not currently enabled.

---

## 29. Low — `ClearSecurity` deletes everything it is asked to keep, and has no callers

```go
// provider.go:397-417
func (m *SecurityList) ClearSecurity(pUserID int, pSchema, pTablename string) error {
	var filtered []ColumnSecurity
	m.ColumnSecurityMutex.Lock()
	defer m.ColumnSecurityMutex.Unlock()

	secKey := fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)
	list, ok := m.ColumnSecurity[secKey]
	if !ok {
		return nil
	}

	for i := range list {
		cs := &list[i]
		if cs.Schema != pSchema && cs.Tablename != pTablename && cs.UserID != pUserID {
			filtered = append(filtered, *cs)
		}
	}

	m.ColumnSecurity[secKey] = filtered
	return nil
}
```

The entries under `secKey` are, by construction, exactly the entries whose schema,
table and user match the key. So `cs.Schema != pSchema && cs.Tablename !=
pTablename && cs.UserID != pUserID` is false for every one of them, `filtered`
stays nil, and the function assigns a nil slice — wiping the whole bucket rather
than clearing a subset of it. Even reading the condition charitably as intended
(`||` rather than `&&`), it would still be a no-op filter for the same reason: the
key already pins all three fields.

The effect of the wipe is not a security hole in the fail-open sense, because
`ApplyColumnSecurity` treats a nil list as "no rules" and returns an error rather
than data — but combined with `hooks.go:182-187`, which logs and continues when
`ApplyColumnSecurity` fails, a wiped bucket means **unmasked data** until the next
`LoadColumnSecurity` with `pOverwrite`.

It is also never called: grepping the repository for `ClearSecurity` finds the
definition and nothing else — no production caller, no test. Another X10 instance.
And it does not touch `RowSecurity` at all, so the one cache that most needs
invalidation (finding 14) has no invalidation path.

### Remediation

Decide which it is.

**If per-key invalidation is wanted,** it is a `delete`, not a filter, and it needs
to cover both maps:

```go
func (m *SecurityList) ClearSecurity(userID any, schema, table string) {
	key := securityKey(schema, table, userID)

	m.colMu.Lock()
	delete(m.columns, key)
	m.colMu.Unlock()

	m.rowMu.Lock()
	delete(m.rows, key)
	m.rowMu.Unlock()
}
```

Then call it — from logout (`providers.go:316-320`, `providers_direct.go:182-206`)
and from whatever administrative path changes a rule.

**If it is not wanted,** delete the function. Dead security code is worse than
absent security code, because a reader assumes it works.

Either way, once finding 6 introduces real caching the invalidation story has to be
designed deliberately, and `pkg/cache`'s tag support (`DeleteByTag`,
`cache/provider_memory.go:194-228`) is the better substrate: tag entries with
`table:orders` and a rule change invalidates every user's copy in one call.

---

## 30. Low — string building by concatenation in per-field loops

`maskString` (`provider.go:102-120`) and `splitTag` (`hooks.go:424-441`) both build
their results with `+=` inside a loop:

```go
// hooks.go:427-436
	for _, ch := range tag {
		if ch == sep {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
```

Each `+=` allocates a new string and copies the accumulated prefix, so both are
O(n²) in bytes copied. `string(ch)` additionally allocates per rune.

At the sizes involved — struct tags, masked field values — this is a few hundred
bytes of garbage per call and will never appear in a profile. It is listed because
`maskString` runs once per masked field per record, so a 10 000-row response with
three masked columns calls it 30 000 times, and the fix is free.

### Remediation

`strings.Builder` in `maskString` (shown in finding 25), and `strings.Split` — or
`strings.FieldsFunc` if empty parts must be dropped, which is what the `current !=
""` check is doing — in place of `splitTag`:

```go
func splitTag(tag string, sep rune) []string {
	return strings.FieldsFunc(tag, func(r rune) bool { return r == sep })
}
```

`FieldsFunc` already omits empty fields, so this is behaviour-preserving. Better
still, delete `splitTag` along with `contains` and `extractSQLName` (finding 24) and
use `common.ExtractTagValue` (`common/handler_utils.go:53-55`).

---

## 31. Low — registration reads the new user's ID with `LastInsertId`, after a non-transactional uniqueness check

```go
// providers_direct.go:105-130 (abridged)
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		var count int
		checkQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE username = ?`, a.tableNames.Users))
		if err := db.QueryRowContext(ctx, checkQuery, req.Username).Scan(&count); err != nil { return err }
		if count > 0 { return errUsernameExists }

		checkQuery2 := ... `SELECT COUNT(*) FROM %s WHERE email = ?` ...
		if count > 0 { return errEmailExists }

		insertQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (username, email, password, ...) VALUES (?, ?, ?, ...)`, a.tableNames.Users))
		res, err := db.ExecContext(ctx, insertQuery, req.Username, req.Email, req.Password, req.UserLevel, rolesStr, true, now, now, 0, "")
		if err != nil { return err }
		userID, err = res.LastInsertId()
		return err
	})
```

Two portability defects in six lines.

**`LastInsertId` is not supported by `lib/pq` or `pgx`.** Those drivers return
`0, errors.New("LastInsertId is not supported by this driver")`, so on PostgreSQL —
the dialect the rest of this package is written against, with `pg_proc` probes and
`$n` placeholders — registration returns an error *after having created the user*.
The user exists, the caller sees a failure, and a retry hits `errUsernameExists`.
The correct form is `INSERT … RETURNING id` with `QueryRowContext`, which is also
what the stored-procedure path effectively does.

**Check-then-insert is a race, not a constraint.** Two concurrent registrations
with the same username both read `count == 0` and both insert. There is no
transaction around the sequence (each `db.ExecContext`/`QueryRowContext` is
autocommitted independently) and no `SELECT … FOR UPDATE`. Whether a duplicate
results depends entirely on whether the `users` table has unique indexes on
`username` and `email` — `database_schema.sql:9` does declare them, so the database
saves this, but then the error surfaced to the caller is the driver's constraint
violation rather than `errUsernameExists`, and the carefully written enumeration
handling at `:133-140` is bypassed.

Both are behind `registerDirect`, whose more serious problems — storing
`req.Password` in plaintext at `:125`, and accepting caller-supplied `UserLevel`
and `Roles` — are covered in findings 1 and 15.

### Remediation

```go
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		q := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (username, email, password, user_level, roles, is_active, created_at, updated_at, program_user_id, program_user_table)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, a.tableNames.Users))
		return db.QueryRowContext(ctx, q, req.Username, req.Email, hashed, level, rolesStr, true, now, now, 0, "").Scan(&userID)
	})
	if err != nil {
		switch {
		case isUniqueViolation(err, "users_username_key"): return nil, errUsernameExists
		case isUniqueViolation(err, "users_email_key"):    return nil, errEmailExists
		}
		return nil, fmt.Errorf("registration failed: %w", err)
	}
```

Let the unique constraint be the check — it is the only one that is actually
atomic — and map the violation back to the existing sentinel errors so the
anti-enumeration behaviour at `:133-140` still applies. For MySQL/SQLite support,
`RETURNING` needs a dialect switch; `rewritePlaceholders` (`query_mode.go:141-157`)
is the existing seam for that.

---

## 32. Low — `authenticateCallback` may return `(nil, nil)`, and the caller does not check

```go
// providers.go:382-387
	if len(tokens) == 0 {
		if a.authenticateCallback != nil {
			return a.authenticateCallback(r)
		}
		return nil, fmt.Errorf("session token required")
	}
```

```go
// providers.go:452-459
	// All tokens failed — try callback before returning error
	if a.authenticateCallback != nil {
		return a.authenticateCallback(r)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("authentication failed for all provided tokens")
```

`authenticateCallback` is an integrator-supplied `func(*http.Request)
(*UserContext, error)` (`providers.go:126-129`). Its result is returned
unexamined, so a callback that returns `(nil, nil)` — a plausible reading of "I
have no opinion about this request" — propagates a nil `*UserContext` with a nil
error to `authenticateRequest`:

```go
// middleware.go:82-91
	userCtx, err := provider.Authenticate(r)
	if err != nil {
		http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	return setUserContext(r, userCtx), true
```

which passes it to `setUserContext`:

```go
// middleware.go:59-78 (abridged)
func setUserContext(r *http.Request, userCtx *UserContext) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, UserContextKey, userCtx)
	ctx = context.WithValue(ctx, UserIDKey, userCtx.UserID)
	...
```

`userCtx.UserID` on a nil pointer panics. The panic happens inside the middleware
chain, where the outermost recovery middleware turns it into a 500 — so the
outcome is a crash-per-request, not a bypass, provided the recovery middleware is
installed. The doc comment at `:126-129` does not state the contract, so a
callback author has no way to know `(nil, nil)` is forbidden.

### Remediation

Validate at the boundary — the callback is external code, so treat its result as
untrusted:

```go
	if a.authenticateCallback != nil {
		userCtx, err := a.authenticateCallback(r)
		if err != nil {
			return nil, err
		}
		if userCtx == nil {
			return nil, fmt.Errorf("authentication callback returned no user context")
		}
		return userCtx, nil
	}
```

and defensively in the middleware, as shown in finding 18:

```go
	if userCtx == nil {
		logger.Error("authenticator %T returned nil user context with nil error", provider)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, false
	}
```

Document the contract on the callback field: *"must return a non-nil UserContext or
a non-nil error; returning (nil, nil) is a programming error."* Same treatment for
`HeaderAuthenticator`'s equivalent hook (finding 8).

---

## 33. Low — `RequestPasswordReset` returns the raw token to its caller

```go
// providers_direct.go:336-359 (abridged)
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to generate reset token: %w", err)
	}
	rawToken := hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	...
	return &PasswordResetResponse{Token: rawToken, ExpiresIn: 3600}, nil
```

The cryptography here is right, and worth saying so: 256 bits from `crypto/rand`,
only the SHA-256 hash is stored (`:352`), a one-hour expiry, prior unused tokens
are deleted before insert, `used`/`used_at` are recorded on completion
(`:399-400`), and all sessions are invalidated when the password changes
(`:395-397`). The enumeration-resistant early return at `:330-332` — generic
success when the user does not exist — is also correct.

The issue is the return value. `PasswordResetResponse.Token` carries the raw token
back to the caller, and the caller is an HTTP handler. Whether that token reaches
the requester's inbox (correct) or the HTTP response body (an account-takeover
primitive: anyone who can POST `/password-reset` for `victim@example.com` receives
the reset token) is entirely up to the integrator, and nothing in the signature,
the doc comment or `SECURITY_FEATURES.md` says which is intended.

There is a second, quieter exposure: the raw token is now in the caller's memory
and in any structure the caller logs. `PasswordResetResponse` has no custom
`String()`, so `logger.Debug("reset response: %+v", resp)` prints it — and if
anyone writes that at `Warn`, it goes to Sentry (X8).

### Remediation

Make the safe path the only path: have the package deliver the token and return
nothing sensitive.

```go
type PasswordResetResponse struct {
	// Requested is always true for a well-formed request, whether or not the
	// address matched an account. The token is never returned; it is delivered
	// through the configured Notifier.
	Requested bool
	ExpiresIn int
}

type ResetNotifier interface {
	SendPasswordReset(ctx context.Context, email, rawToken string, expiresIn time.Duration) error
}
```

If returning the token must stay — for tests, or for integrators who deliver it
themselves — then:

1. Rename the field to `RawTokenDoNotLogOrReturn` or gate it behind an explicit
   `AllowTokenInResponse bool` on the authenticator, defaulting to false.
2. Add `func (r PasswordResetResponse) String() string { return "PasswordResetResponse{redacted}" }`
   so `%v`/`%+v` cannot print it.
3. State the requirement in the doc comment and in `SECURITY_FEATURES.md`: *the
   token must never appear in an HTTP response.*
4. Rate-limit the endpoint per account and per IP — the reset path is otherwise an
   unauthenticated way to generate a row in `user_password_resets` per request
   (`middleware.audit.md` finding 1 again).

---

## Cross-cutting references

**X8 — `Warn`/`Error` are forwarded to the error tracker; `Info`/`Debug` are not.**
Confirmed at `logger/logger.go:118-122` and `:135-139` against `:100-106` and
`:142-148`. This package is the largest source of X8 exposure in the repository,
in both directions:

The complete `Warn`/`Error` inventory for this package is eight lines, and six of
them are problems:

| site | level | reached when | problem |
| --- | --- | --- | --- |
| `providers.go:391` | Warn | one comma in an `Authorization` header | logs the **raw header** — finding 7 |
| `hooks.go:36` | Warn | any request with no user ID | fires on every guest request |
| `hooks.go:48` | Warn | column rules cannot be loaded | every request in Direct mode — finding 22 |
| `hooks.go:61` | Warn | row rules cannot be loaded | every request in Direct mode — finding 22 |
| `hooks.go:93` | Warn | a user is blocked | logs the rendered `userRef` — `*UserContext` with session token and claims |
| `hooks.go:184` | Warn | a table has no column rules | every secured read — finding 22 |
| `provider.go:132` | Error | prev/new record type mismatch | appropriate |
| `provider.go:204` | Warn | an unsettable field | once per unsettable field per record — finding 4 |

Five of the six are attacker-triggerable with no rate limiter in front of them
(`middleware.audit.md` finding 1), and three of them fire during entirely normal
operation. Any one will exhaust a Sentry quota from a single client. `hooks.go:93`
and `providers.go:391` additionally put a credential into the error tracker.

The fix in each case is to choose the level by *who causes the event*: client
behaviour is `Debug`, operator misconfiguration is `Warn` (once, at startup), and
only genuine server-side faults are `Error`. `hooks.go:93` is the one case where
`Warn` is right — a blocked access attempt is worth an alert — and it needs only the
redacting `String()` from finding 7 to stop leaking.

**X10 — configured, written, and never installed.** Two new instances from this
package:

* `ValidateSQLNames` (`sql_names.go:242-258`) and `ValidateTableNames`
  (`table_names.go:82-96`) have no production callers (finding 17).
* `ClearSecurity` (`provider.go:397-417`) has no callers at all (finding 29).

To which finding 2 adds a third and worst variant: row-level security is *wired*
but inert, because the type assertion at `hooks.go:134-136` can never succeed.

**`pkg/logger`.** `CatchPanic` on a function with unnamed results returns zero
values and a nil error, which is how finding 3 turns a panic into "masking
succeeded". `logger.HandlePanic(name, r)` with a named `err` result is the correct
helper. See `logger.audit.md` finding 2 for the general form; `pkg/security` has
the two most consequential instances (`provider.go:302`, `:443`).

**`pkg/modelregistry`.** `modelregistry.audit.md` finding 1 (lookup by name misses
schema-qualified registrations) is the upstream half of finding 15: the registry
fails to find the model, and `hooks.go:287`/`:311` turn that into `return nil //
model not registered, allow by default`.

**`pkg/cache`.** The natural substrate for finding 6 (no caching at all) and
finding 14 (unbounded maps): `MaxSize` with LRU eviction
(`cache/provider_memory.go:105-109`, `:307-326`) and tag-based invalidation
(`SetWithTags` `:121-170`, `DeleteByTag` `:194-228`). `restheadspec` already uses
the tag pattern for query totals (`restheadspec/cache_helpers.go:99-113`), so
there is a working precedent in-tree.

**`pkg/middleware`.** `middleware.audit.md` finding 1 — no rate limiter is
installed by default — is what makes findings 11, 18, 22 and 33 practical rather
than theoretical. `pkg/security`'s own `middleware.go` is separate from
`pkg/middleware` and composes with it; the recovery middleware from that package
is what keeps finding 32 to a 500 rather than a crash.

**`pkg/config`.** There is no `security` section in `setDefaults`
(`config/manager.go:170-293`), so nothing in this package is configurable through
the standard config path — the OAuth signing key (finding 23), the query mode
(finding 19) and the table/SQL name overrides (finding 17) are all constructor
arguments only. Adding them is the prerequisite for making the safe options the
default in deployment.

**`pkg/restheadspec`.** `security_hooks.go:14-58` is the wiring that registers
every hook discussed here, and `handler.go:396-400`/`:424` is what guarantees
`ApplyColumnSecurity` always receives a slice (which is why the read path does not
have the single-record masking gap it first appears to have). Separately, and
unrelated to security: `handler.go:1006` passes `modelPtr` to the response writer
rather than `hookCtx.Result`, so an `AfterRead` hook that *replaces* the result has
no effect — worth noting here because a hook that redacts by substitution would
silently do nothing.

---

## Recommended order of work

The list is ordered by risk reduced per unit of effort, not by finding number.

1. **Verify passwords (finding 1).** Everything else is secondary to the fact that
   `loginDirect` never reads `req.Password` and the stored procedure compares it to
   nothing. Add bcrypt (or argon2id) hashing on registration and comparison on
   login, in both query modes, with a migration path for existing plaintext rows.
   Until this is done the package has no authentication.
2. **Make row-level security actually run, and fix the injection it enables at the
   same time (findings 2 and the `GetTemplate` interpolation).** Replace the
   impossible assertion at `hooks.go:134-136` with the real `common.SelectQuery`
   interface, and make `GetTemplate` bind `{UserID}` as a parameter instead of
   `fmt.Sprintf("%v", …)` (`provider.go:48`). These must ship together: turning the
   filter on while it still interpolates its user ID into SQL would introduce a
   live injection where today there is only dead code. Add an integration test that
   asserts a second tenant's rows are absent from the result.
3. **Replace `CatchPanic` with `HandlePanic` on the two masking functions
   (finding 3).** Two lines, and it converts "panic ⇒ unmasked data, nil error"
   into "panic ⇒ error".
4. **Stop holding the global write locks across provider calls (finding 6).** Call
   the provider outside the lock; take the lock only to publish the result. This is
   the whole of the package's throughput problem and it is a mechanical change.
5. **Fail closed, now that failures are distinguishable (findings 5, 15 and 22).**
   Change `GetRowSecurityTemplate`/`ApplyColumnSecurity` to return `(value, ok,
   err)`, then deny on `err` and skip silently on `!ok`; make an unregistered model
   a denial rather than a permission. Step 22's return-type change is what makes
   this possible, so do it here rather than earlier.
6. **Add a redacting `String()` to `UserContext` and drop the credential log lines
   (finding 7).** One method plus four log-line edits removes session tokens and
   `Authorization` headers from logs, from Sentry, and from the cache keys in
   finding 14.
7. **Close the four token-handling gaps (findings 9, 10, 11, 34).** Reject
   multi-token `Authorization` headers instead of trying each; strip the scheme
   prefix before building the logout cache key; gate or bound `/oauth/register`;
   and copy `ClientSecretHash`/`TokenEndpointAuthMethod` in
   `lookupOrFetchClient` so a restart stops turning confidential clients into
   public ones. Finding 34 is a three-line fix for a High, so it can go first.
8. **Fix the masking primitives (findings 4 and 25).** Type-switch instead of
   kind-string matching so `float*`, `bool`, `time.Time` and `[]byte` are actually
   masked, guard every setter with `CanSet()`, and rewrite `maskString` on runes
   with corrected bounds. Ship with a before/after table, because output changes.
9. **Build the audit log, or stop calling it one (finding 16).**
10. **The remainder** — findings 12, 13, 17, 19, 20, 21, 23, 24, 26–33 — in any
    order. Findings 12 (delete or implement `JWTAuthenticator`) and 17 (call the
    identifier validators) are the two that remove the most latent risk for the
    least work.

Two things to preserve while doing all of this. The **OAuth 2.1 authorization-code
grant** (`oauth_server.go:794-869`) is correct: mandatory PKCE with S256 only,
exact `redirect_uri` matching, single-use codes, confidential-client
authentication, `subtle.ConstantTimeCompare` for secrets. The **password-reset
flow** (`providers_direct.go:310-407`) is correct: 256 bits from `crypto/rand`,
hash-at-rest, one-hour expiry, single use, session invalidation on completion, and
enumeration-resistant responses. Both are the standard the rest of the package
should be brought up to, and neither should be refactored casually in the course of
fixing the items above.
