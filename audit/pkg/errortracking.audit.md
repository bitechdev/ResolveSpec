# Audit — `pkg/errortracking`

- **Date:** 2026-09-29
- **Scope:** `pkg/errortracking/{interfaces,noop,sentry,factory}.go` (260 LOC, 4 source files + 1 test file, 67 LOC)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client; error messages and `extra` maps may contain attacker-shaped content.

## Summary

Small, clean abstraction: a `Provider` interface, a no-op implementation, a Sentry implementation,
and a config-driven factory. The concurrency story is fine — `sentry.Hub` is internally
mutex-guarded and the provider holds no mutable state of its own. The real exposure is **what
this package sends out of the trust boundary**: it is the egress point for every `Warn`/`Error`
in the codebase (see `audit/pkg/logger.audit.md` findings 2 and 3) and it applies **no scrubbing
whatsoever**.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | **High** | Security | No `BeforeSend` scrubber — messages, stack traces and `extra` leave the trust boundary verbatim |
| 2 | Medium | Security | `sentry.Init` mutates process-global state; `NewSentryProvider` can be called repeatedly and silently replaces the global client |
| 3 | Medium | Slowness | `Flush(timeout int)` is second-granularity only; combined with `Close()` gives up to 7 s of shutdown stall |
| 4 | Medium | Slowness | `CapturePanic` stringifies the whole stack trace into an `extra` field on every panic |
| 5 | Low | Security | `AttachStacktrace: true` is hardcoded — source paths and function names of the deployment leak to the SaaS |
| 6 | Low | Correctness | `CaptureError` produces an `Exception` with a nil `Stacktrace` for plain `errors.New` values |
| 7 | Low | Correctness | Config-provided `SampleRate == 0` silently means "send everything", not "send nothing" |
| 8 | Low | Architecture | `factory.go` imports `pkg/config`, coupling the lowest-level package to the config layer |

---

## Findings

### 1. No scrubbing before egress (High, Security)

`sentry.go:29-42`

```go
err := sentry.Init(sentry.ClientOptions{
	Dsn:              config.DSN,
	Environment:      config.Environment,
	Release:          config.Release,
	Debug:            config.Debug,
	AttachStacktrace: true,
	SampleRate:       config.SampleRate,
	TracesSampleRate: config.TracesSampleRate,
})
```

`BeforeSend` is not set. Neither is `BeforeSendTransaction`. Nothing in `CaptureError`
(`sentry.go:46`), `CaptureMessage` (`sentry.go:75`) or `CapturePanic` (`sentry.go:97`) inspects or
redacts its inputs; all three copy straight into `event.Message` / `event.Exception.Value` /
`event.Contexts["extra"]` and hand it to `hub.CaptureEvent`.

Because `pkg/logger.Error`/`Warn` forward every formatted message here unconditionally, the set of
things that can reach Sentry is "every error string produced anywhere in ResolveSpec". In this
codebase that includes driver errors (which embed DSNs and sometimes credentials on connect
failure), SQL fragments with bound values, and identifiers taken from request headers.

Under the hostile-client threat model this is an **attacker-reachable exfiltration channel**: shape
an input that lands in an error message, and its content is written to a third-party system
outside the operator's control.

**Recommendation:** set `BeforeSend` to run a redaction pass over `Message`,
`Exception[].Value` and `Contexts` — at minimum strip `password=`, `://user:pass@`, `Bearer `,
and anything matching the configured DSN patterns. Consider an `extra`-key allowlist rather than
passing the caller's map through (`sentry.go:70`, `92`, `114-121`).

### 2. `sentry.Init` mutates process-global state (Medium, Security/Correctness)

`sentry.go:29` calls the package-level `sentry.Init`, which installs a global client, and
`sentry.go:40` then captures `sentry.CurrentHub()`. Consequences:

- Calling `NewSentryProvider` twice (two `NewProviderFromConfig` calls, or a config reload)
  replaces the global client. Any previously-created `SentryProvider` keeps a `hub` pointer whose
  client has been swapped underneath it — events start going to the *new* DSN. If the two configs
  have different environments or DSNs, events are misrouted with no error.
- Events enqueued on the old client at swap time may be dropped without flush.
- It means this "provider" abstraction is a lie: you cannot actually have two Sentry providers
  with different configs in one process.

**Recommendation:** build a dedicated client with `sentry.NewClient(opts)` and bind it to an
owned `sentry.NewHub(client, scope)` rather than touching the global. That also makes `Close()`
able to genuinely release resources.

### 3. Coarse, additive shutdown flush (Medium, Slowness)

`sentry.go:125-128`

```go
func (s *SentryProvider) Flush(timeout int) bool {
	return sentry.Flush(time.Duration(timeout) * time.Second)
}
```

`timeout` is an `int` interpreted as whole seconds — the interface (`interfaces.go:30`) cannot
express 500 ms. `Close()` (`sentry.go:131-134`) then runs a *second* `sentry.Flush(2s)`.

`pkg/logger.CloseErrorTracking` (`logger.go:69-75`) calls `Flush(5)` then `Close()`, so a graceful
shutdown blocks for **up to 7 seconds** in this package alone, before the HTTP drain and DB close
budgets in `pkg/server`. If the Sentry endpoint is unreachable (the common case during an
outage — which is when you are restarting) both flushes run to full timeout.

Note `Flush` also flushes the *global* client, not `s.hub`'s, which is the same object today only
because of finding 2.

**Recommendation:** change the interface to `Flush(context.Context) bool` or
`Flush(time.Duration) bool`; have `Close` not re-flush; and pass the server's shutdown deadline
through instead of hardcoding 5.

### 4. Whole stack trace stringified into `extra` on every panic (Medium, Slowness)

`sentry.go:117-119`

```go
if stackTrace != nil {
	extraCtx["stack_trace"] = string(stackTrace)
}
```

The caller (`pkg/logger.CatchPanicCallback`, `HandlePanic`) already produced the trace via
`debug.Stack()`. Here it is copied again into a string and shipped as a context field. Per
recovered panic that's two full copies of a multi-kilobyte trace plus a network event. With
panics recovered rather than fatal on the request path, a reliably-panicking input is a cheap
amplification primitive (see `audit/pkg/logger.audit.md` finding 5).

Sentry also truncates large context values server-side, so much of this payload is wasted.

**Recommendation:** put the trace in `Exception[0].Stacktrace` as structured frames (which Sentry
groups and displays properly) rather than a blob in `extra`, and cap the byte length.

### 5. `AttachStacktrace: true` hardcoded (Low, Security)

`sentry.go:35`. Not configurable. Every event carries absolute source paths, package layout and
function names of the build. That's mostly a reconnaissance leak to whoever can read the Sentry
project rather than to the internet attacker, but it should be an operator choice, especially for
on-prem deployments sending to a hosted DSN.

### 6. Nil stack trace for plain errors (Low, Correctness)

`sentry.go:62`

```go
Stacktrace: sentry.ExtractStacktrace(err),
```

`ExtractStacktrace` only finds a trace if the error implements `StackTrace()`/`Callers()`
(`pkg/errors`-style). Nearly all errors in this codebase come from `fmt.Errorf`, so this returns
`nil` and the Sentry event has an exception with no frames — grouping falls back to the message
string, which (because messages embed request-specific values) fragments what should be one issue
into thousands.

**Recommendation:** fall back to `sentry.NewStacktrace()` when extraction yields nil, and set an
explicit `event.Fingerprint` derived from a stable prefix rather than the full message.

### 7. `SampleRate == 0` means "send everything" (Low, Correctness)

`factory.go:20-27` passes `cfg.SampleRate` through untouched, and `pkg/config/manager.go`
registers **no default** for `error_tracking.sample_rate`. So an operator who leaves it out gets
`0.0`, and `sentry-go@v0.46.2` `client.go:339-341` rewrites `0.0` → `1.0`.

Verified in the module cache:

```go
if options.SampleRate == 0.0 {
	options.SampleRate = 1.0
}
```

Fail-open rather than fail-closed, which is arguably the right choice for an error tracker — but
it means an operator who *intends* to disable sampling by setting `0` gets the opposite, silently.

**Recommendation:** make `SampleRate` a `*float64` in the config struct, or register an explicit
default in `setDefaults`, and validate/log the effective value at init.

### 8. `factory.go` imports `pkg/config` (Low, Architecture)

`factory.go:6` — `errortracking` is imported by `pkg/logger`, which is imported by essentially
everything. Pulling `pkg/config` (and therefore `viper`) into that dependency chain means the
lowest-level logging path transitively depends on the configuration layer. It works today only
because `pkg/config` imports nothing from ResolveSpec; the first time it wants to log, there is
an import cycle.

**Recommendation:** move `NewProviderFromConfig` into `pkg/config`-adjacent wiring code (or take
a small local options struct instead of `config.ErrorTrackingConfig`) so `errortracking` stays a
leaf.

---

## What looks right

- **Concurrency is genuinely fine.** `SentryProvider` holds only an immutable `*sentry.Hub`;
  `sentry.Hub` guards its own state with a mutex, and `CaptureEvent` hands off to a background
  worker with a bounded queue, so it does not block the caller and does not need a lock here.
- `GetHubFromContext(ctx)` with fallback to `s.hub` (`sentry.go:53-56`, `81-84`, `103-106`) is the
  correct Sentry idiom and preserves per-request scope when middleware installs a hub.
- Nil-input guards on all three capture methods (`sentry.go:47`, `76`, `98`) — a nil error, empty
  message or nil recovered value is dropped rather than producing a junk event.
- `event.Contexts` is safe to index: `sentry.NewEvent()` initialises the map, so
  `event.Contexts["extra"] = ...` cannot nil-panic.
- `NoOpProvider` means a disabled tracker is always safe to call — no nil checks needed at call
  sites beyond the one in `pkg/logger`.
- `factory.go:15-17` correctly refuses to start with `provider: sentry` and an empty DSN rather
  than silently no-oping.

## Panic handling

The package neither panics nor recovers, which is correct for its role — it is the *sink* for
panic reports, not a place that should be generating them. The nil-guards in finding "what looks
right" cover the realistic nil-deref paths. One residual: `CapturePanic` ranges over `extra`
(`sentry.go:115`) without a nil check, which is safe in Go (ranging a nil map yields zero
iterations) — noted only to confirm it was checked.

## Suggested follow-up

1. Add `BeforeSend` redaction (finding 1). This is the highest-value single change in the package.
2. Stop using the global Sentry client (finding 2) — unblocks real multi-provider support and a
   meaningful `Close()`.
3. Widen `Flush` to a duration/context (finding 3) and wire it to the server shutdown budget.
4. Add tests for the Sentry path. The existing test file covers only `NoOpProvider`, severity
   string mapping and interface satisfaction — `SentryProvider`'s capture methods have no
   coverage at all. `sentry-go` ships a test transport that makes this straightforward.
