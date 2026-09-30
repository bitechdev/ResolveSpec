# Audit: cross-cutting findings across `pkg/*`

| | |
|---|---|
| **Scope** | all 23 packages under `pkg/` (64 065 non-test lines) |
| **Audit date** | 2026-09-29 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |

This file records findings that are **not specific to one package** — they are
properties of the repository or patterns repeated across many packages. The
per-package audits reference this file rather than restating them.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| X1 | **High** | locking | `-race` is never run anywhere; no package is ever race-checked |
| X2 | **High** | testing | `go test` runs against 2 of 23 packages; the other 21 are only compiled and vetted |
| X3 | **High** | testing | Every integration-test step is `continue-on-error: true` — integration failures cannot fail CI |
| X10 | **High** | security | Whole subsystems are declared, configured, documented and tested but never installed — including every protective middleware and the metrics provider |
| X4 | **Medium** | security | `gosec` is not enabled in `.golangci.json`; no SAST runs on a package set full of dynamic SQL |
| X5 | **Medium** | locking | Unsynchronized package-level mutable globals are the dominant concurrency pattern |
| X6 | **Medium** | security | Insecure-by-default transport across the board: `sslmode: disable`, `WithInsecure()`, no TLS in cache configs |
| X7 | **Medium** | panic handling | Panic handling is inconsistent and, where it exists, tends to fail open |
| X8 | **Medium** | security | `logger.Warn`/`Error` forward every message to Sentry unscrubbed, and error strings routinely embed attacker data *(partly fixed 2026-09-30: redaction and rate limiting added in `pkg/logger`; call sites still embed attacker data)* |
| X9 | **Low** | testing | Test coverage is extremely uneven: 5 packages have no test file at all |

The table is ordered by severity; the sections below are in ID order, since other
audit files reference these findings by number.

---

### X1. High — `-race` is never run

Verified by grep: the string `-race` does not appear in `Makefile`,
`.github/workflows/tests.yml`, `.github/workflows/maint.yml` or
`.github/workflows/make_tag.yml`.

Every test invocation in the repository:

```makefile
# Makefile:8
	@go test ./pkg/resolvespec ./pkg/restheadspec -v -cover
# Makefile:13
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
# Makefile:97
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -v
# Makefile:103
	@go test ./pkg/resolvespec ./pkg/restheadspec -coverprofile=coverage.out
# Makefile:110
	@go test -tags=integration ./pkg/resolvespec ./pkg/restheadspec -coverprofile=coverage-integration.out
```

```yaml
# .github/workflows/tests.yml — unit-tests job
      - name: Run unit tests
        run: go test ./pkg/resolvespec ./pkg/restheadspec -v -cover
```

**Why this matters.** This audit found unsynchronized concurrent access to
mutable state in **six** packages, and the Go race detector would have flagged
every one of them on the first run:

| Package | Racing state | Reference |
|---|---|---|
| `pkg/cache` | `defaultCache` read/written by concurrent request handlers | `cache.audit.md` finding 3 |
| `pkg/config` | `*viper.Viper` has no internal lock; `configInstance` singleton | `config.audit.md` findings 1, 2 |
| `pkg/logger` | `Logger`, `errorTracker` globals | `logger.audit.md` finding 1 |
| `pkg/modelregistry` | `defaultRegistry` read by 6 functions without the lock | `modelregistry.audit.md` findings 2, 8 *(fixed 2026-09-30)* |
| `pkg/tracing` | `tracer` global | `tracing.audit.md` finding 5 *(fixed 2026-09-30)* |
| `pkg/errortracking` | `sentry.Init` mutates process globals | `errortracking.audit.md` finding 2 |

**Failure scenario.** `pkg/config` finding 1 is the sharpest illustration. A
concurrent `Manager.Set`/`Manager.Get` pair reaches viper's internal maps, which
have no mutex. A concurrent map read and write in Go is not a panic — it is
`fatal error: concurrent map read and map write`, which **`recover()` cannot
catch**. The process dies instantly, mid-request, with no graceful shutdown and
no error-tracker report. That is a remotely-triggerable hard crash, and it
cannot be found by inspection at scale — it is precisely what `-race` exists to
find. The detector has been in Go since 1.1 and costs one flag.

**Recommendation.** Add a race job that covers everything, and keep it separate
from the coverage run (race builds are ~2–10× slower):

```makefile
test-race:
	@go test -race -count=1 ./pkg/...
```

```yaml
  race-tests:
    name: Race Detector
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with: { go-version: "1.24" }
      - run: go test -race -count=1 ./pkg/...
```

Expect it to fail on the first run — that is the point. Fix `pkg/logger`,
`pkg/config` and `pkg/cache` first, since they are the shared dependencies. Note
that the race detector only reports races that **actually execute**, so X1 and X2
have to be fixed together: a race detector pointed at packages with no tests
finds nothing.

**Status (2026-09-30) — resolved for the packages with tests.** `make test-race` now exists
(`go test -race -count=1 ./pkg/...`), `test-unit` covers `./pkg/...`, and `test`
depends on both. The CI workflow (`.github/workflows/tests.yml`) now has a `race-tests`
job running the same command. The first full run was not clean:

| Package | Race | Kind |
|---|---|---|
| `pkg/logger` | `Logger` / `errorTracker` reassigned while other goroutines log (hit via `pkg/server` tests) | **production** — now guarded by an `RWMutex` (`getLogger`, `setLogger`, `getErrorTracker`); the exported `Logger` var is kept for compatibility |
| `pkg/security` | `DatabaseAuthenticator.Authenticate` passed `&userCtx` to the async session-activity goroutine while also returning it to the caller | **production** — the goroutine now gets a copy and is tracked by a `WaitGroup` so tests can wait for it |
| `pkg/security` tests | async activity update used sqlmock concurrently with the test adding expectations | test — tests wait via `authenticateSync` |
| `pkg/eventbroker`, `pkg/websocketspec` tests | handler/hook closures mutated a plain `bool`/`int` from worker goroutines | test — now `atomic` |
| `pkg/mqttspec` tests | not a race: hand-built `HookContext` lacked `TableName`/`Model`/`ModelPtr`, the unsubscribe test set `Data` instead of `SubscriptionID`, and `:memory:` SQLite gave each pooled connection its own empty database | test — fixed; these were failing without `-race` too |

`pkg/cache`, `pkg/config`, `pkg/modelregistry`, `pkg/tracing` and
`pkg/errortracking` are listed above but did **not** trip the detector: their
racing paths are not exercised by the current tests, which is the point made in
the paragraph above about X1 and X2 needing to be fixed together. Adding
concurrent tests for those globals is still outstanding.

Known limitation: `pkg/security` tests are not repeatable with `-count>1` (a
package-level capability cache carries over between runs), so the race target
keeps `-count=1`.

---

### X2. High — `go test` runs against 2 of 23 packages

Every `go test` invocation in the repository names exactly
`./pkg/resolvespec ./pkg/restheadspec`. No invocation uses `./...` or
`./pkg/...`.

The test bodies that exist but are never executed by CI:

| Package | Test files | Test lines | Run by CI? |
|---|---|---|---|
| `restheadspec` | 19 | 5 123 | **yes** |
| `resolvespec` | 8 | 2 379 | **yes** |
| `security` | 15 | 6 359 | no |
| `common` | 10 | 3 644 | no |
| `reflection` | 8 | 3 404 | no |
| `websocketspec` | 6 | 3 092 | no |
| `funcspec` | 3 | 2 416 | no |
| `spectypes` | 7 | 2 367 | no |
| `eventbroker` | 4 | 1 527 | no |
| `mqttspec` | 3 | 1 408 | no |
| `middleware` | 5 | 1 127 | no |
| `openapi` | 2 | 1 022 | no |
| `server` | 2 | 694 | no |
| `dbmanager` | 2 | 659 | no |
| `config` | 1 | 608 | no |
| `cache` | 1 | 69 | no |
| `errortracking` | 1 | 67 | no |
| `metrics` | 1 | 64 | no |
| `resolvemcp` | 1 | 34 | no |
| `logger` | 0 | 0 | — |
| `modelregistry` | 1 | ~150 | yes (`-race`) *(added 2026-09-30)* |
| `testmodels` | 0 | 0 | — |
| `tracing` | 1 | ~90 | yes *(added 2026-09-30)* |

**Failure scenario.** `pkg/security` has 6 359 lines of tests — the largest test
body in the repository — and **not one of them runs in CI**. A change that breaks
authentication, column-level security or row-security templates merges green.
The `maint.yml` job named "Run Vet Tests" is misleading: it runs `go mod
download`, `go mod verify` and `go vet ./...` and contains **no `go test` step at
all** (verified by grep). So the only signal on 21 of 23 packages is "it
compiles and vet is happy".

This directly explains the density of findings in this audit. The
`pkg/modelregistry` authorization fail-open (`modelregistry.audit.md` finding 1)
and the `pkg/cache`/`pkg/security` auth-outage-on-cache-failure
(`cache.audit.md` finding 1) are both the kind of defect a single unit test would
have caught, in packages that have never been tested.

**Recommendation.** Change every invocation to `./pkg/...`:

```makefile
test-unit:
	@go test ./pkg/... -v -cover
```

```yaml
      - name: Run unit tests
        run: go test ./pkg/... -v -cover
```

If some currently-unrun package fails immediately, that is a bug report, not a
reason to keep the narrow list. Quarantine individual failing tests with
`t.Skip` and a `TODO` referencing an issue, so the *package* stays in the set.

---

### X3. High — integration failures cannot fail CI

`.github/workflows/tests.yml`, `integration-tests` job — every meaningful step
carries `continue-on-error: true`:

```yaml
      - name: Run resolvespec integration tests
        continue-on-error: true
        env:
          TEST_DATABASE_URL: "host=localhost user=postgres password=postgres dbname=resolvespec_test port=5432 sslmode=disable"
        run: go test -tags=integration ./pkg/resolvespec -v -coverprofile=coverage-resolvespec-integration.out
      - name: Run restheadspec integration tests
        continue-on-error: true
        ...
```

**Failure scenario.** The integration suites are the only tests that exercise
real SQL generation against a real PostgreSQL — i.e. the only automated check on
the identifier-quoting and filter-construction paths that this audit's threat
model cares most about. Because both steps are `continue-on-error`, a SQL
injection regression, a broken join, or a total suite failure (wrong DSN, missing
migration) shows as a green check mark with a collapsed red step that nobody
opens. The job has no step that fails, so the job always passes. This is
strictly worse than not having the tests, because it creates the appearance of
coverage.

Note the integration DSN itself uses `sslmode=disable`, consistent with X6.

**Recommendation.** Remove `continue-on-error` from the two `go test` steps.
Keep it only on the coverage-report generation and artifact-upload steps, which
genuinely should not fail a build. If the suites are currently flaky, fix or
skip the flaky tests individually — `continue-on-error` on the whole step
disables the signal entirely.

---

### X4. Medium — `gosec` is not enabled — **RESOLVED**

> **Status (2026-09-30):** `gosec` is now in `linters.enable` and the repository lints clean
> (0 issues). The initial run produced 115 findings. Real fixes: login-form values in
> `security/oauth_server.go` are now HTML-escaped (G705), and `SqlSparseVector` index
> parsing uses `ParseInt(..., 10, 32)` (G109). The remaining ~110 sites carry
> `//nolint:gosec // Gxxx: <reason>` comments. The G201/G701 reasons (identifiers from
> trusted config or internal/validated names) and the G115 range claims were not
> individually audited and still need review. The text below describes the state before the change.

`.golangci.json` (`version: 2`) enables exactly three linters beyond the v2
standard set:

```json
  "linters": {
    "enable": [
      "gocritic",
      "misspell",
      "revive"
    ],
```

golangci-lint v2's standard set (`errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`) is on by default, so those do run. **`gosec` does not** —
it appears in the file only inside an exclusion rule for `_test.go`:

```json
        {
          "linters": [
            "dupl",
            "errcheck",
            "gocritic",
            "gosec"
          ],
          "path": "_test\\.go"
        },
```

Listing a linter in `exclusions.rules` does not enable it. The `lint` job in
`.github/workflows/maint.yml:40-57` does run golangci-lint over the whole
repository with `version: latest`, so the config is applied — it simply never
asks for the security checks.

**Failure scenario.** This repository builds SQL by string construction from
attacker-controlled schema, table, column and filter names (see
`restheadspec.audit.md` and `common.audit.md`). `gosec`'s `G201`/`G202`
(SQL string formatting/concatenation) are exactly the rules that would flag a
new `fmt.Sprintf` into a query, which is the single most likely way a SQL
injection enters this codebase. Also unenabled and relevant: `G104` (unhandled
errors — this audit found ~20 discarded errors in `pkg/cache` alone), `G304`
(file path from variable — relevant to `PathsConfig.Join`, `config.audit.md`
finding 14), `G402` (bad TLS settings — X6), `G404` (weak random).

**Recommendation.** Add `gosec` to `linters.enable` and triage the initial
findings. Expect noise on the SQL rules given the architecture; suppress
individual verified-safe sites with `//nolint:gosec // G201: identifier is
validated by X` comments that name the invariant, rather than disabling the rule
globally. That converts each suppression into a reviewable claim.

Consider also `bodyclose`, `rowserrcheck` and `sqlclosecheck` for a
database-heavy codebase, and `contextcheck` given how many methods here accept a
`ctx` and ignore it.

---

### X5. Medium — unsynchronized mutable package globals are the dominant pattern

Nine of the twenty-three packages expose mutable process-wide state through
package-level variables, and most guard it with nothing:

| Package | Global | Guarded? |
|---|---|---|
| `pkg/logger` | `Logger *zap.SugaredLogger` (`logger.go:15`), `errorTracker` (`:16`) | **no** — and `Logger` is exported |
| `pkg/cache` | `defaultCache *Cache` (`cache.go:10`) | **no** |
| `pkg/config` | `configInstance *Manager` (`manager.go:15`) | **no** |
| `pkg/tracing` | `tracer` | **yes** *(fixed 2026-09-30)* — `atomic.Pointer` |
| `pkg/modelregistry` | `defaultRegistry` | **yes** *(fixed 2026-09-30)* — guarded by `registriesMutex`; all access via `GetDefaultRegistry()` |
| `pkg/metrics` | `globalProvider` (`interfaces.go:50-51`) | **yes** — `globalProviderMu sync.RWMutex` |

`pkg/metrics` is the model the others should follow:

```go
// pkg/metrics/interfaces.go:50-72
var (
	globalProviderMu sync.RWMutex
	globalProvider   Provider
)

func SetProvider(p Provider) {
	globalProviderMu.Lock()
	globalProvider = p
	globalProviderMu.Unlock()
}

func GetProvider() Provider {
	globalProviderMu.RLock()
	p := globalProvider
	globalProviderMu.RUnlock()
	if p == nil {
		return &NoOpProvider{}
	}
	return p
}
```

Note that it also returns a working `NoOpProvider` rather than `nil`, so callers
need no nil check — the pattern `pkg/logger` and `pkg/cache` should copy.

**Failure scenario.** Beyond the data races in X1, the shared failure mode is
**lazy initialization on the request path**. `cache.GetDefaultCache()`
(`cache.go:48`) and `config.GetConfigManager()` (`manager.go:18`) both
`if x == nil { x = construct() }` with no `sync.Once`. Under concurrent first
traffic, several instances are constructed and all but one are silently
discarded, so writes go to an orphaned object — a cache that is permanently 100%
miss, or two `Manager`s disagreeing about configuration. It presents as "the
cache doesn't work" with no error anywhere.

`pkg/logger.Logger` being **exported** and mutable is its own hazard: any
package, or any consumer of this library, can reassign the process logger
mid-flight while other goroutines are calling `Logger.Infow`.

**Recommendation.** For each global: `atomic.Pointer[T]` for
single-pointer swaps, `sync.Once` for lazy defaults, `sync.RWMutex` for
multi-field state. Unexport `logger.Logger` behind accessors. Where a nil global
is possible, return a no-op implementation instead of `nil`, as
`pkg/metrics.GetProvider` does.

---

### X6. Medium — insecure transport is the default everywhere

Every network dependency defaults to cleartext, and in two cases there is no way
to configure otherwise:

| Component | Default | Configurable? | Reference |
|---|---|---|---|
| PostgreSQL | `sslmode: disable` (`config/manager.go:242`) | yes, via config | `config.audit.md` finding 3 |
| OTLP traces | `otlptracegrpc.WithInsecure()` hardcoded (`tracing/tracing.go:41`) *(fixed 2026-09-30: TLS default, `Insecure` opt-in)* | **yes** | `tracing.audit.md` finding 1 |
| Redis (cache) | no `TLSConfig` set | **no** — `RedisConfig` has no TLS field | `cache.audit.md` finding 15 |
| Memcache | no TLS | **no** | `cache.audit.md` finding 15 |
| CORS | `allowed_origins: ["*"]`, `allowed_headers: ["*"]` (`config/manager.go:214-216`) | yes | `config.audit.md` finding 3 |
| DB user | `user: postgres` with blank password (`config/manager.go:239-240`) | yes | `config.audit.md` finding 3 |

The `tracing.go:41` case is the most pointed, because the code knows better:

```go
otlptracegrpc.WithInsecure(), // Use WithTLSCredentials in production
```

The comment names the fix and the config struct provides no way to apply it.

**Failure scenario.** The cache holds `UserContext` — identity and authorization
data — keyed by the raw bearer token (`security/providers.go:398`). With no TLS,
anything on the path between the service and Redis can read session contents and
the `AUTH` password, then **write** a forged `auth:session:<token>` entry.
`GetOrSet` returns a cache hit without consulting the database, so a forged entry
is a complete authentication bypass. Meanwhile the trace exporter ships full
request URLs including query strings (`tracing.audit.md` finding 2) in cleartext
to the collector.

**Recommendation.** Invert every default: TLS on unless explicitly disabled.
Concretely — add `TLS`/`TLSSkipVerify`/`TLSCACertFile` to `cache.RedisConfig`
and `tracing.Config`; change the `sslmode` default to `require`; change
`cors.allowed_origins` to `[]` and require an explicit list; remove the default
`postgres`/blank-password credentials so a misconfigured deployment fails to
start rather than connecting to a local database as a superuser. Add a startup
validation pass that logs a prominent warning for each insecure setting actually
in effect.

---

### X7. Medium — panic handling is inconsistent, and where it exists it fails open

Three different conventions coexist:

1. **`logger.CatchPanic(location)`** (`logger/logger.go:184`) — recovers, logs,
   reports, and **swallows**. Both call sites are security enforcement:
   `security/provider.go:302` (`ApplyColumnSecurity`) and `:443`
   (`GetRowSecurityTemplate`). See `logger.audit.md` finding 4.
2. **`logger.HandlePanic(method, r)`** (`logger/logger.go:197`) — converts the
   panic to an `error` the caller must handle. This is the correct shape.
3. **Nothing at all.** `pkg/cache` has zero `recover()` calls in 1 538 lines;
   so do several other packages.

**Failure scenario (fail-open).** `ApplyColumnSecurity` panics — a nil map, a
bad type assertion on a rule, a reflection edge case. `CatchPanic` recovers and
the function returns normally, so the caller believes column security was
applied. It was not. The response contains the columns the security layer was
supposed to strip. The panic is logged, but the request succeeds with elevated
data exposure. A security control whose failure mode is "allow" is the wrong
default; it must be "deny".

**Failure scenario (panic under a lock).** `pkg/cache` holds `m.mu` across
`m.items[key] = ...` (`provider_memory.go:111`). After `Close()` sets
`items = nil` that assignment panics. With no recover in the package the panic
propagates to whatever handler exists upstream; if that handler recovers, `m.mu`
is **never unlocked** and every subsequent cache operation blocks forever. The
process stays alive and wedged — worse than a crash, because health checks that
do not touch the cache keep passing.

**Recommendation.** Establish one convention and apply it:

- **Request boundaries** (HTTP handlers, event consumers, goroutines): recover,
  log with stack, report to the error tracker, return 500 / nack. A `go`
  statement without a deferred recover is a process-kill waiting to happen —
  `security/providers.go:447` (`go a.updateSessionActivity(...)`) is one.
- **Security enforcement**: recover, log, and **fail closed** — return an error
  that the caller must propagate as a denial. Never `CatchPanic`.
- **Internal helpers**: do not recover. Let the boundary handle it.
- **Anything holding a lock**: prefer `defer mu.Unlock()` (already the pattern in
  `pkg/cache`) so a panic cannot leak the lock, and keep panicking code out of
  critical sections.

Add a `CatchPanicFailClosed(location string, err *error)` helper so the
fail-closed variant is as easy to reach for as `CatchPanic`.

---

### X8. Medium — attacker data reaches Sentry unscrubbed

Two facts compose badly:

`pkg/logger/logger.go:125-140` — every `Error` (and every `Warn`, `:108-123`)
forwards the fully-formatted message to the error tracker:

```go
func Error(template string, args ...interface{}) {
	ctx, remainingArgs := extractContext(args...)
	message := fmt.Sprintf(template, remainingArgs...)
	...
	if errorTracker != nil {
		errorTracker.CaptureMessage(ctx, message, errortracking.SeverityError, map[string]interface{}{
			"process_id": os.Getpid(),
		})
	}
}
```

And `pkg/errortracking` installs **no `BeforeSend` scrubber**
(`errortracking.audit.md` finding 1), so the message goes to Sentry verbatim.

Meanwhile error strings across the codebase interpolate attacker-controlled
values, sometimes secrets:

| Site | Interpolated value |
|---|---|
| `cache/cache_manager.go:26`, `:40` | the full cache key — for the session cache, **the raw bearer token** |
| `security/providers.go:391` | the raw `Authorization` header, logged at `Warn` when multiple tokens are present |
| `config/manager.go:164` | config file paths |
| throughout `restheadspec` | schema, table, column and filter values from the request |

**Failure scenario.** `security/providers.go:391` is live today:

```go
logger.Warn("Multiple authentication tokens provided in Authorization header (%d tokens). This is unusual and may indicate a misconfigured client. Header: %s", len(tokens), sessionToken)
```

A client sends two bearer tokens. `logger.Warn` formats the full header value
into the message and forwards it to Sentry, where a **valid session credential**
is now stored by a third party, visible to everyone with Sentry access, retained
per Sentry's policy, and replayable for the token's lifetime. No attacker
sophistication is required — the trigger is a single extra header, and the
codebase invites it by logging the header contents as the diagnostic.

**Recommendation.**

1. Add a `BeforeSend` hook in `pkg/errortracking` that redacts
   `Authorization`, `Cookie`, `Set-Cookie`, anything matching
   `(?i)(token|password|secret|apikey|api_key|bearer)\s*[:=]\s*\S+`, and
   long high-entropy strings. This is the one change that bounds the whole class.
2. Never log a credential, even truncated. Change `providers.go:391` to log
   `len(tokens)` only.
3. Replace `fmt.Errorf("key not found: %s", key)` with a sentinel
   `cache.ErrNotFound` (`cache.audit.md` finding 7).
4. Key the session cache on `sha256(token)`, as
   `security/keystore_database.go:287` already does for API keys.
5. Add sampling / rate limiting to the tracker fan-out
   (`logger.audit.md` finding 3) so an error storm is not also a cost and
   availability event.

---

### X9. Low — five packages have no tests at all

`pkg/logger`, `pkg/modelregistry`, `pkg/testmodels`, `pkg/tracing` have zero
`*_test.go` files. `pkg/resolvemcp` has 34 lines, `pkg/metrics` 64,
`pkg/errortracking` 67, `pkg/cache` 69.

**Failure scenario.** `pkg/modelregistry` is untested and contains this audit's
only **Critical** authorization finding: `GetModel` returns a "registry locked"
error under write-lock contention, which `security/hooks.go:274-294` converts
into `return nil // model not registered, allow by default`
(`modelregistry.audit.md` finding 1; *fixed 2026-09-30, regression tests added*). A twenty-line test that registers a model
from one goroutine while reading it from another would demonstrate the fail-open
immediately. The package guards a security boundary and has never been tested.

`pkg/logger` being untested matters for a different reason: it is imported by
almost every other package, so a defect there (the format-string sink in `Info`
and `Debug`, `logger.audit.md` finding 6) is repo-wide.

**Recommendation.** Prioritize by blast radius, not by size:

1. `pkg/modelregistry` — concurrent register/read; assert `GetModelRulesByName`
   never returns a "locked" error that a caller could read as "not registered".
2. `pkg/logger` — nil-`Logger` fallback paths, format-string handling, and that
   `Warn`/`Error` do not forward secrets once a scrubber exists.
3. `pkg/cache` — concurrent `GetDefaultCache`, the expired-item TOCTOU, and that
   `tagToKeys` does not grow after eviction.
4. `pkg/tracing`, `pkg/metrics`, `pkg/errortracking` — construction and no-op
   paths; these are mostly configuration surfaces.

Combine with X1 and X2: tests that are not run, and tests run without `-race`,
do not close these gaps.

---

### X10. High — configured subsystems that are never installed

Three separate subsystems are fully built — typed config, defaults, tests,
documentation — and then never connected to anything that runs.

**1. Every protective middleware.** `pkg/middleware` provides rate limiting, IP
blacklisting, request-size limiting and input sanitization. Non-test callers:

| Constructor | Non-test callers |
|---|---|
| `middleware.NewRateLimiter` | **0** |
| `middleware.NewIPBlacklist` | **0** |
| `middleware.NewRequestSizeLimiter` | **0** |
| `middleware.DefaultSanitizer` | **0** outside the package |
| `middleware.StrictSanitizer` | **0** |
| `middleware.PanicRecovery` | 1 — `pkg/server/manager.go:466` |

`pkg/server/manager.go` is the only file outside the package that imports it, and
only for `PanicRecovery`. The config that exists to drive the rest —
`MiddlewareConfig.RateLimitRPS`, `.RateLimitBurst`, `.MaxRequestSize`
(`pkg/config/config.go:123-125`), defaulted at `pkg/config/manager.go:209-211` —
has **no reader anywhere in the module**.

**2. The metrics provider.** `metrics.SetProvider` and
`metrics.NewPrometheusProvider` have **0 non-test callers**, so
`metrics.GetProvider()` returns `&NoOpProvider{}`
(`pkg/metrics/interfaces.go:63-72`) for the process lifetime. Every instrumented
call site in the repository — 39 DB-query sites in
`pkg/common/adapters/database`, the HTTP middleware, the event-broker counters,
and the sole `RecordPanic` call at `pkg/middleware/panic.go:19` — writes to a
no-op. `MetricsConfig.Enabled` and `.Provider` are likewise never read, and
`pkg/config` has no `metrics` section at all.

**3. The configured CORS policy.** `config.CORSConfig`
(`pkg/config/config.go:128-134`), defaulted at `pkg/config/manager.go:214-217`,
is never read. The policy that actually applies comes from a **different type of
the same name**, `common.CORSConfig`, built by `common.DefaultCORSConfig()`
(`pkg/common/cors.go:19-48`), which derives allowed origins from the configured
server instances and the host's local IPs and ignores `cors.allowed_origins`
entirely. It is called from ten sites across `pkg/resolvespec` and
`pkg/restheadspec`.

**Failure scenario.** Each of these is a silent, config-shaped lie, and they fail
in the same way: the operator's mental model of the deployment is wrong in the
direction of believing a control exists.

- **Under the hostile-client threat model there is no rate limit and no
  request-body limit in the serving path.** `max_request_size: 10485760` is
  configured and unenforced, so a single unauthenticated `POST` with a
  multi-gigabyte body is read into memory and OOM-kills the process; unlimited
  request rate exhausts the 25-connection default pool
  (`pkg/config/manager.go:224`) just as cheaply. Both are one-line attacks
  against controls the configuration says are active. An operator lowering
  `rate_limit_rps` during an incident observes no change and will reasonably
  conclude the attack exceeds the limit rather than that no limit exists.
- **There is no telemetry with which to notice any of it.** No request counts, no
  latency histograms, no `panics_total`, no DB-query metrics — the one signal
  that would show an attack in progress is wired end to end and discarded at the
  last step. This is also why the metrics cardinality defects
  (`metrics.audit.md` findings 2 and 5) are only latent: they become live the
  moment someone installs the provider that the config implies is already there.
- **Tightening `cors.allowed_origins` does nothing.** The value is ignored, so a
  hardening change lands, reviews clean, deploys, and changes no behaviour. Two
  types named `CORSConfig` in two packages is the mechanism; nothing warns.

The common thread is that none of this fails visibly. It compiles, the tests pass
(`pkg/middleware` has the repo's best test ratio — 1 127 test lines to 799 code
lines — all of it exercising code nothing calls), CI is green, and the config file
documents features that are absent. Under X2 these packages are not even in the
tested set, so the tests that do exist are not run.

**Recommendation.**

1. **Wire the middleware chain** in `pkg/server` from `MiddlewareConfig`,
   outermost first: size limiter → rate limiter → blacklist → `PanicRecovery`
   (innermost, so it sees handler panics; `trackRequestsMiddleware` at
   `manager.go:540` correctly stays outside). Fix the trusted-proxy handling
   (`middleware.audit.md` findings 2 and 3) **before** mounting the two IP-based
   layers, and do not mount the sanitizer at all until findings 5–7 there are
   resolved — as written it corrupts filter values and can synthesize a
   `javascript:` URI.
2. **Install a metrics provider** from config, gated on `metrics.enabled`, and
   add the missing `metrics` section to `pkg/config`. Bound the label sets first
   (`metrics.audit.md` findings 2 and 5) — installing the provider as-is converts
   two latent cardinality DoS findings into live ones.
3. **Delete the duplicate `CORSConfig`** or make `common.DefaultCORSConfig()`
   read `config.CORSConfig`. Two types with one name, one of them ignored, is a
   trap regardless of which way it is resolved.
4. **Make the class of defect detectable.** Log at startup which middleware,
   metrics provider and CORS policy are active, so an unwired subsystem is
   visible in the first ten lines of a boot log instead of during an incident.
   A CI check that every `mapstructure` field in `pkg/config` has at least one
   reader would have caught all three of these; so would enabling `unused` in
   `.golangci.json` for exported-but-unreferenced constructors.

---

## Recommended order of work

1. **X2 + X1** — point `go test` at `./pkg/...` and add a `-race` job. Everything
   else in this audit is easier to verify once these exist, and they will
   surface the six data races on their own.
2. **X3** — remove `continue-on-error` from the integration `go test` steps.
3. **X10** — mount the request-size limiter and rate limiter. Until this is
   done the service has no volumetric protection at all, and no metrics with
   which to see that. Fix `middleware.audit.md` findings 2 and 3 in the same
   change, since mounting the IP-based layers without them adds attack surface.
4. **X8 item 1 and 2** — add the Sentry `BeforeSend` scrubber and stop logging
   the `Authorization` header. Small, self-contained, stops an active credential
   leak.
5. **X7** — decide the panic convention; make the two `CatchPanic` sites in
   `pkg/security` fail closed, and stop returning the panic value to the client
   (`pkg/middleware/panic.go:28`).
6. **X5** — fix the globals in `pkg/logger`, `pkg/config`, `pkg/cache`
   (the shared dependencies) first.
7. **X6** — add TLS fields and invert the defaults.
8. **X4** — enable `gosec` and triage.
9. **X9** — backfill tests, in the order listed above.

## Per-package audits

`cache` · `common` · `config` · `dbmanager` · `errortracking` · `eventbroker` ·
`funcspec` · `logger` · `metrics` · `middleware` · `modelregistry` · `mqttspec` ·
`openapi` · `reflection` · `resolvemcp` · `resolvespec` · `restheadspec` ·
`security` · `server` · `spectypes` · `testmodels` · `tracing` · `websocketspec`

Each is `audit/pkg/<name>.audit.md`.
