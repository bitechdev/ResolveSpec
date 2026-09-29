# Audit — `pkg/logger`

- **Date:** 2026-09-29
- **Scope:** `pkg/logger/logger.go` (211 LOC, 1 file, no tests)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client; request bodies, headers, params and identifiers are attacker-controlled.

## Summary

`pkg/logger` is a thin package-global wrapper over `zap.SugaredLogger` plus a fan-out to
`pkg/errortracking`. It is the single most widely imported package in the repo, so its defects
are systemic. Two classes of problem dominate: **unsynchronised global mutable state** (a real
data race between logger re-initialisation and request-path logging), and **unbounded,
unsampled, unscrubbed egress of formatted messages to a third-party error tracker** on every
`Warn`/`Error` call — which under hostile input is both a data-leak and a cost/latency
amplification channel.

There are **zero tests** in this package.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | **High** | Locking | Unsynchronised writes to `Logger` / `errorTracker` globals race with every log call |
| 2 | **High** | Security | Every `Warn`/`Error` message is shipped verbatim to Sentry — no scrubbing, no allowlist |
| 3 | **High** | Slowness | No rate limit, sampling or dedup on error-tracker fan-out; attacker-triggerable |
| 4 | **High** | Panic | `CatchPanic` swallows panics unconditionally — and both call sites are security enforcement functions (fail-open) |
| 5 | Medium | Slowness | `debug.Stack()` + full stack stringification on every recovered panic |
| 6 | Medium | Security | `log.Printf(template, args...)` fallback is a format-string sink for caller-supplied text |
| 7 | Medium | Security | No CRLF/control-char sanitisation on the stdlib fallback path → log injection |
| 8 | Medium | Correctness | `Info`/`Debug` do not strip `context.Context` args; `Warn`/`Error` do |
| 9 | Low | Correctness | `UpdateLogger` leaks the previous zap logger / file descriptor |
| 10 | Low | Correctness | No `Sync()` exported → buffered log lines lost on exit |
| 11 | Low | Slowness | `os.Getpid()` called on every log line |
| 12 | Low | Observability | `UpdateLogger` build failure degrades silently to stdlib `log` |

---

## Findings

### 1. Unsynchronised global mutable state — data race (High, Locking)

`logger.go:14-15`

```go
var Logger *zap.SugaredLogger
var errorTracker errortracking.Provider
```

`Logger` is written by `Init` → `UpdateLogger` (`logger.go:51`) and by `UpdateLoggerPath`
(`logger.go:29`). `errorTracker` is written by `InitErrorTracking` (`logger.go:57`) and read by
`GetErrorTracker`, `CloseErrorTracking`, `Warn`, `Error`, `CatchPanicCallback`, `HandlePanic`.

Every read site (`logger.go:100`, `108`, `123`, `139`, `156`, `199`) is unguarded. There is no
mutex, no `atomic.Value`, no `sync.Once`.

- **Benign case:** everything is initialised once in `main` before goroutines start. Then it's fine.
- **Real case:** `UpdateLoggerPath` is an exported, runtime-callable API. A config reload, a
  log-rotation hook, or a test helper calling it while HTTP handlers log concurrently is an
  unsynchronised write to an interface value and a pointer, concurrent with reads. Under the Go
  memory model this is undefined behaviour; in practice a torn interface read (type word from the
  new value, data word from the old) faults.
- `CloseErrorTracking` (`logger.go:69`) does a read-check-then-use on `errorTracker` with no
  guard, so a concurrent `InitErrorTracking(nil)` yields a nil-interface dereference inside
  `Flush`.

**Recommendation:** store both behind `atomic.Pointer`/`atomic.Value` (or an `sync.RWMutex`),
and gate first-time init behind `sync.Once`. Run the test suite with `-race` — see finding 12 of
`audit/pkg/config.audit.md` for the same pattern in the config singleton.

### 2. Unscrubbed message egress to third-party error tracker (High, Security)

`logger.go:110-118` and `logger.go:126-134`

```go
message := fmt.Sprintf(template, remainingArgs...)
...
errorTracker.CaptureMessage(ctx, message, errortracking.SeverityError, ...)
```

*Every* `Warn` and `Error` call in the entire codebase has its fully-formatted message sent to
the configured provider (Sentry, in practice). There is no allowlist, no redaction hook, and
`pkg/errortracking/sentry.go` configures no `BeforeSend` scrubber.

Concretely, formatted error strings across `pkg/` embed: SQL fragments and bound values, DB
connection strings, schema/table/column identifiers, filter expressions built from request
input, and raw request bodies in a few handlers. Under the hostile-client threat model this is
two problems at once:

- **Outbound data leak:** secrets that appear in wrapped driver errors (DSNs, credentials from
  `pq`/`pgx` connect failures) leave the trust boundary to a SaaS endpoint.
- **Attacker-controlled exfil channel:** an attacker who can shape a value that ends up in an
  error message gets that value written to a third-party system — useful for exfiltrating data
  read out of the DB via an induced error.

**Recommendation:** add a redaction step before `CaptureMessage`/`CapturePanic` (regex-strip
DSN/`password=`/bearer-token shapes at minimum), and set Sentry's `BeforeSend` as a second
layer. Prefer passing structured fields with an explicit allowlist over shipping the rendered
string.

### 3. No rate limiting or sampling on error-tracker fan-out (High, Slowness)

`logger.go:113`, `logger.go:129`

An unauthenticated request that reliably produces one `Error` log (a malformed filter, an unknown
column, a bad JSON body — all of which the spec handlers log at error level) becomes one Sentry
event. At even modest request rates this means:

- Sentry quota burn → a direct billing-DoS.
- `sentry-go` enqueues onto a bounded worker queue; once saturated events are dropped, so the
  *real* errors are the ones lost.
- `pkg/errortracking/sentry.go:34` passes `SampleRate` straight through from config, and
  `config/manager.go` sets **no default** for it. `sentry-go@v0.46.2` `client.go:339` maps
  `SampleRate == 0.0` → `1.0`, so the out-of-the-box behaviour is *send 100% of events*.

**Recommendation:** default `error_tracking.sample_rate` to something < 1.0 for the message path,
and put a token-bucket or a fingerprint-dedup in front of `CaptureMessage`. Keep panics at 100%.

### 4. `CatchPanic` swallows panics unconditionally, fail-open at both call sites (High, Panic handling)

`logger.go:145-176`

```go
func CatchPanicCallback(location string, cb func(err any), args ...interface{}) func() {
	...
	if err := recover(); err != nil { ... if cb != nil { cb(err) } }
}
```

The recovered value is logged and then discarded. There is no variant that logs-and-re-panics
and no way for the caller to signal "this panic means state is corrupt, take the process down".

This is the right default for an HTTP handler boundary. The two current call sites are **not**
handler boundaries:

- `pkg/security/provider.go:302` — `defer logger.CatchPanic("ApplyColumnSecurity")()`
- `pkg/security/provider.go:443` — `defer logger.CatchPanic("GetRowSecurityTemplate")()`

Both are *security enforcement* functions. Swallowing a panic there means the column-security
filter or row-security template silently does not get applied, and the caller — which has no way
to learn a panic occurred, since `CatchPanic` returns nothing and sets no error — proceeds as if
security was applied. That is a fail-open security control; see
`audit/pkg/security.audit.md` for the full write-up of those two sites.

Separately: a panic while a mutex is held does not release that mutex unless an intervening
`defer Unlock` exists, so swallowing converts a crash into a permanent deadlock at any
lock-holding call site.

**Recommendation:** add `CatchPanicRethrow(location string)` for internal use and reserve the
swallowing form for the outermost request/goroutine boundary. Document which is which.

### 5. Full stack capture on every recovered panic (Medium, Slowness)

`logger.go:158` and `logger.go:197`

```go
callstack := debug.Stack()
```

`debug.Stack()` stops the world briefly and allocates; `HandlePanic` then formats the whole trace
into a string *and* ships it to Sentry. Because panics on the request path are recovered rather
than fatal (finding 4), an attacker who finds one reliably-panicking input turns each request
into a stack capture + string build + network event. That is a solid amplification factor over a
normal request.

**Recommendation:** cap the captured stack (`runtime.Stack` into a fixed 8–16 KiB buffer rather
than `debug.Stack()`'s grow-until-it-fits loop), and rate-limit identical panic fingerprints.

### 6. Format-string sink in the stdlib fallback (Medium, Security)

`logger.go:100`, `logger.go:142` (and `108`/`123` with `"%s"`, correctly)

```go
func Info(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)   // template is the caller's, args may be empty
```

`Info` and `Debug` pass `template` directly to `log.Printf`. If any caller ever does
`logger.Info(someUserString)` — the idiomatic-looking single-argument call — a `%s` or `%n` in
that string is interpreted as a verb, producing `%!s(MISSING)` garbage and mangled logs. Note
`Warn`/`Error` already avoid this on the fallback path by using `log.Printf("%s", message)`;
`Info`/`Debug` do not.

A grep of `pkg/` found **no** current single-argument call sites, so this is a latent API footgun
rather than a live bug — but it is one that costs one line to close.

**Recommendation:** mirror `Warn`'s shape: format first, then `log.Printf("%s", message)`.
`govet` runs by default under golangci-lint v2's standard set, and its `printf` analyser infers
wrappers like these — so once the fallback is fixed, call sites are checked at build time for
free. (Note `gosec` is *not* in `.golangci.json`'s `linters.enable` list; it appears only in the
exclusion rules. Worth enabling repo-wide.)

### 7. No log-injection sanitisation on the fallback path (Medium, Security)

On the zap path, the JSON encoder escapes newlines and control characters, so injected content
can't forge a log record. On the `Logger == nil` fallback path, `log.Printf` writes raw bytes: a
value containing `\n2026-09-29 ... level=info authorized=true` forges a plausible second log
line. Combined with finding 12 (silent degradation to the fallback path) this is reachable
without the operator noticing the encoder changed.

**Recommendation:** strip/escape `\r`, `\n` and other C0 control characters from formatted
messages before the stdlib write.

### 8. `Info`/`Debug` don't strip `context.Context` arguments (Medium, Correctness)

`extractContext` (`logger.go:79-98`) exists precisely so callers can pass a `ctx` as a trailing
variadic arg. `Warn` (`logger.go:106`) and `Error` (`logger.go:121`) call it. `Info`
(`logger.go:99`) and `Debug` (`logger.go:137`) **do not** — they pass every arg to `Sprintf`.

So `logger.Info("saved %s", name, ctx)` renders as
`saved widget%!(EXTRA *context.valueCtx=context.Background...)`, dumping the context's contents
(which in this codebase carry auth/tenant values) into the log line. That is both noise and a
minor disclosure.

**Recommendation:** call `extractContext` in all four level functions for uniform behaviour.

### 9. `UpdateLogger` leaks the previous logger (Low)

`logger.go:37-53` builds a new zap logger and overwrites `Logger` without calling `Sync()`/close
on the old one. `UpdateLoggerPath` opens a new file sink each call; repeated calls leak a file
descriptor each time and buffered lines in the old logger are lost.

### 10. No `Sync()` on shutdown (Low)

Nothing in the package exposes `Logger.Sync()`, and `CloseErrorTracking` (`logger.go:69`) flushes
only the error tracker. zap buffers writes to file sinks, so the last lines before exit — often
the interesting ones — are dropped. Add `func Sync() error` and call it from the server's
shutdown path alongside `CloseErrorTracking`.

### 11. `os.Getpid()` per log line (Low, Slowness)

`logger.go:102`, `111`, `127`, `140`, `165`, `202`. On Linux `getpid` is cached by the runtime so
this is cheap, but the PID cannot change for the life of the process — cache it in a package var
and drop six calls from the hot path.

### 12. Silent degradation when the logger fails to build (Low, Observability)

`logger.go:45-49`

```go
logger, err := config.Build()
if err != nil { log.Print(err); return }
```

`Logger` stays `nil`, so the whole process silently falls back to unstructured stdlib logging
(and thereby onto the format-string and log-injection paths of findings 6 and 7) with a single
line of warning that itself goes to stderr. A bad `logger.path` in config (unwritable directory)
triggers exactly this.

**Recommendation:** return the error from `Init`/`UpdateLogger` and let the caller decide whether
to fail startup.

---

## What looks right

- `extractContext` correctly ignores second and subsequent contexts rather than fighting over them.
- `Warn`/`Error` use `log.Printf("%s", message)` on the fallback path — the safe form.
- `HandlePanic` returns an `error` rather than swallowing, which lets callers convert a panic into
  a normal error return. This is the better of the two panic idioms in the package.
- The `errortracking.Provider` indirection means a nil/noop provider is always safe to call.

## Suggested follow-up

1. Guard the two globals (finding 1) — prerequisite for running the suite under `-race`.
2. Add redaction + sampling in front of the error-tracker fan-out (findings 2, 3).
3. Split `CatchPanic` into swallow/rethrow variants and re-audit the ~60 `recover()` sites
   listed in the other package audits against the split (finding 4).
4. Add a test file. Minimum: concurrent `UpdateLogger` + `Error` under `-race`, `Info` with a
   `%`-bearing message, and nil-provider paths.
