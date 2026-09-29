# Audit — `pkg/modelregistry`

- **Date:** 2026-09-29
- **Scope:** `pkg/modelregistry/model_registry.go` (381 LOC, 1 file, **no tests**)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client. This package holds the `ModelRules` that
  `pkg/security/hooks.go` consults to authorise read/update/create/delete, so it is **on the
  authorisation path**.

## Summary

This package is the highest-risk find in the audit. It has been deliberately reworked to "never
hang" by replacing blocking `Lock`/`RLock` with **bounded `TryLock` retry loops that give up and
return a wrong answer** — and because those wrong answers are consumed by
`pkg/security/hooks.go` as authorisation decisions, the result is an **authorisation control that
fails open under lock contention**.

The comments in the file are explicit about the trade-off ("falls back to the last known value
without synchronization", "the call is a no-op") — so the hazard was known at the time of writing.
What appears not to have been traced is where those degraded results end up. They end up in
`checkModelUpdateAllowed` / `checkModelDeleteAllowed`, which treat any error as *permit*.

There are **no tests** in this package and no `-race` coverage of it anywhere.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | **Critical** | Security + Locking | `GetModel`'s "registry locked" error is consumed by `pkg/security/hooks.go` as *allow by default* → authorisation fails open under write-lock contention |
| 2 | **High** | Locking | `GetDefaultRegistry` documents and performs an unsynchronised read of `defaultRegistry` on lock-acquire failure — a data race by design |
| 3 | **High** | Locking | `SetDefaultRegistry` silently no-ops after ~20 ms of contention; caller gets no error |
| 4 | **High** | Security | `RegisterModelWithRules` is non-atomic: the model is visible with permissive `DefaultModelRules` before its real rules are applied (TOCTOU) |
| 5 | Medium | Correctness | `GetAllModels` returns an empty map, and `GetModels` silently skips whole registries, on lock-acquire failure |
| 6 | Medium | Locking | `IterateModels` invokes the caller's callback while holding `RLock` → guaranteed self-deadlock if the callback touches the registry |
| 7 | Medium | Slowness | `time.Sleep(1ms)` spin loops add up to 20 ms of latency per call and defeat mutex fairness/hand-off |
| 8 | Medium | Locking | `defaultRegistry` is read unsynchronised by six package-level functions while `SetDefaultRegistry` writes it under lock |
| 9 | Medium | Locking | Inconsistent discipline: `SetModelRules`/`GetModelRules`/`AddRegistry`/`IterateModels` use blocking locks; the rest use try-locks |
| 10 | Low | Slowness/Locking | Reflection (`TypeOf`, unwrap loop, `reflect.New`) runs while holding the registry **write** lock |
| 11 | Low | Availability | Unbounded unwrap loop: a recursive pointer type (`type T *T`) spins forever holding the write lock (**verified**) |
| 12 | Low | Panic | Package has no `recover` anywhere, and calls a caller-supplied callback under a lock (see 6) |
| 13 | Low | Security | `DefaultModelRules()` grants `CanRead/Update/Create/Delete: true` — registration without explicit rules is fully mutable |

---

## Findings

### 1. Authorisation fails open under lock contention (Critical, Security + Locking)

The mechanism spans two packages.

**Here**, `GetModel` conflates "not found" with "could not lock" into a single `error` return
(`model_registry.go:198-210`):

```go
func (r *DefaultModelRegistry) GetModel(name string) (interface{}, error) {
	if !r.tryRLock() {
		return nil, fmt.Errorf("failed to get model %s: registry locked", name)
	}
	defer r.mutex.RUnlock()

	model, exists := r.models[name]
	if !exists {
		return nil, fmt.Errorf("model %s not found", name)
	}
	return model, nil
}
```

`GetModelRulesByName` (`model_registry.go:364-376`) uses `GetModel` as its existence probe:

```go
for _, registry := range registries {
	if _, err := registry.GetModel(name); err == nil {
		return registry.GetModelRules(name)
	}
}
return ModelRules{}, fmt.Errorf("model %s not found in any registry", name)
```

So a `tryRLock` failure makes the registry look like it does not contain the model.

**In `pkg/security/hooks.go`**, that outcome is interpreted as *permit*
(`pkg/security/hooks.go:274-294`, and identically at `:298-318`):

```go
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

Note the context fast-path at `hooks.go:275`: if `NewModelAuthMiddleware` already put rules in the
context, the registry is not consulted and this bug does not fire. The registry fallback runs
whenever that middleware is absent or did not resolve rules — so the blast radius depends on
deployment wiring. `audit/pkg/security.audit.md` covers whether that middleware is mandatory.

`return nil` from `checkModelUpdateAllowed` means **the update is authorised**. Same for
`checkModelDeleteAllowed`. `GetModelRules(name)` for the "found" path also uses a blocking
`RLock` (`model_registry.go:253`) — so the two calls in `GetModelRulesByName` don't even use the
same locking discipline.

**Failure scenario.** A model `public.employees` is registered with `CanDelete: false`. A
concurrent `RegisterModel` (or `SetModelRules`, or `RegisterModelWithRules`) holds the write lock
for longer than `lockRetryAttempts * lockRetryDelay` = 20 ms — which is entirely achievable given
finding 10 (reflection under the write lock) and finding 7 (each waiter sleeps in 1 ms
increments, so N waiters serialise). During that window every `DELETE` request against
`public.employees` has `GetModelRulesByName` return an error, `checkModelDeleteAllowed` return
`nil`, and the delete proceeds. The model's `CanDelete: false` is not enforced.

This is remotely triggerable if any request path can cause a model registration or a rules
update; even without that, it is a straightforward race that will fire under load.

**Recommendation, in order of value:**

1. Make the security layer **fail closed**: distinguish a sentinel `ErrModelNotFound` from any
   other error, and only allow-by-default on `ErrModelNotFound`. Any other error must deny.
2. Delete the try-lock scheme here entirely and use plain `RLock`/`Lock` (see finding 2 for why
   the scheme does not achieve its stated goal anyway).
3. Separate the existence probe from the rules fetch so `GetModelRulesByName` takes each registry's
   lock once and returns a typed "found / not found / unavailable" result.

### 2. `GetDefaultRegistry` races by design (High, Locking)

`model_registry.go:71-84`

```go
// GetDefaultRegistry returns the current default registry. It uses a
// bounded TryRLock instead of a blocking RLock so it can never hang;
// if the lock can't be acquired in time it falls back to the last known
// value without synchronization.
func GetDefaultRegistry() *DefaultModelRegistry {
	for i := 0; i < lockRetryAttempts; i++ {
		if registriesMutex.TryRLock() {
			defer registriesMutex.RUnlock()
			return defaultRegistry
		}
		time.Sleep(lockRetryDelay)
	}
	return defaultRegistry
}
```

The `return defaultRegistry` on line 83 reads a pointer that `SetDefaultRegistry`
(`model_registry.go:89-116`) writes under the write lock. The only time this path is taken is
precisely when a writer holds or is contending for the lock — i.e. the fallback executes
*exactly* in the window where the race is live. The trade is not "hang vs. slightly stale value";
it is "block for 20 ms vs. data race", and a torn/`nil` pointer read here means a nil-pointer
dereference in the caller.

The premise is also wrong: a `sync.RWMutex.RLock` that is only ever held for a map lookup cannot
"hang". The hang this was written to avoid must have had a different root cause — most likely
finding 6 (self-deadlock through `IterateModels`) or a lock-ordering inversion — and the try-lock
scheme papers over it rather than fixing it.

**Recommendation:** revert to `RLock`/`RUnlock`. If a real hang was observed, reproduce it under
`-race` and `GODEBUG=gctrace`/`SIGQUIT` stack dump; the fix belongs at the deadlock, not here.

### 3. `SetDefaultRegistry` silently no-ops (High, Locking)

`model_registry.go:90-100`

```go
acquired := false
for i := 0; i < lockRetryAttempts; i++ {
	if registriesMutex.TryLock() { acquired = true; break }
	time.Sleep(lockRetryDelay)
}
if !acquired {
	return
}
```

The function returns no error. A caller that swaps in a registry — plausibly one with *restrictive*
`ModelRules* — has no way to learn the swap did not happen, and continues believing the new
registry is in effect. Every subsequent authorisation check consults the old registry's rules.

`GetModels` (`model_registry.go:319-329`) has the same shape and returns `nil`.

**Recommendation:** return `error` from `SetDefaultRegistry`; or (better) use a blocking `Lock`,
since this is a startup-time operation where blocking is correct.

### 4. `RegisterModelWithRules` is non-atomic (High, Security)

`model_registry.go:270-282`

```go
func (r *DefaultModelRegistry) RegisterModelWithRules(name string, model interface{}, rules ModelRules) error {
	// First register the model
	if err := r.RegisterModel(name, model); err != nil {
		return err
	}

	// Then set the rules (we need to lock again for rules)
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.rules[name] = rules
	return nil
}
```

`RegisterModel` releases the write lock before returning, and it initialises the model's rules to
`DefaultModelRules()` (`model_registry.go:191-194`) — which is **permissive**:
`CanRead/CanUpdate/CanCreate/CanDelete` all `true`.

Between the two lock acquisitions, any concurrent `GetModelRulesByName` sees the model registered
with full read/update/create/delete permission, regardless of the restrictive `rules` the caller
passed. The comment "we need to lock again for rules" acknowledges the re-lock without noticing
the gap it opens.

**Failure scenario.** `RegisterModelWithRules("public.audit_log", AuditLog{}, ModelRules{CanRead:
true})` — intended read-only. A `DELETE /public.audit_log/...` that lands in the window is
authorised because `rules.CanDelete` is `true` from the default.

**Recommendation:** add an unexported `registerLocked(name, model, rules)` that writes both maps
under one lock acquisition, and build both public constructors on it. Also change the default
initialisation in `RegisterModel` to deny-by-default, or require rules at registration.

### 5. Degraded results indistinguishable from real results (Medium, Correctness)

Three functions return a plausible-looking answer when they cannot lock:

- `GetAllModels` (`model_registry.go:212-215`) — `return make(map[string]interface{})`, i.e. "the
  registry is empty".
- `GetModels` (`model_registry.go:327-329`) — `return nil` on `registriesMutex` failure, and
  `model_registry.go:336-338` `continue`s past any individual registry it cannot read, returning a
  **partial** list with no indication of truncation.
- `GetDefaultRegistry` — finding 2.

Consumers of `GetModels`/`GetAllModels` (schema introspection, OpenAPI generation, migration
helpers) will emit a document that is missing models, and there is no error to log. Note these two
have no callers in `pkg/` today, which is the only reason this is Medium.

**Recommendation:** return `(T, error)`; never manufacture an empty-but-valid result.

### 6. `IterateModels` calls a user callback under a read lock (Medium, Locking)

`model_registry.go:307-314`

```go
func IterateModels(fn func(name string, model interface{})) {
	defaultRegistry.mutex.RLock()
	defer defaultRegistry.mutex.RUnlock()

	for name, model := range defaultRegistry.models {
		fn(name, model)
	}
}
```

`fn` is arbitrary caller code running with `defaultRegistry.mutex` read-held. `sync.RWMutex` is not
reentrant, and once a writer is blocked on `Lock` it also blocks *new* readers. So:

- `fn` calling `modelregistry.RegisterModel` / `SetModelRules` → `Lock` waits for the reader, which
  is the same goroutine. **Permanent self-deadlock.**
- `fn` calling `GetModel` → `tryRLock` fails for 20 ms and returns "registry locked" for every
  model, which is silent nonsense rather than a deadlock (and feeds finding 1).
- `fn` doing anything slow (I/O, a DB call) holds the registry read lock for that whole duration,
  blocking all registration and — via the blocked-writer rule — all other readers too.

This is the most likely original cause of the "hang" the try-lock scheme was introduced to work
around.

**Recommendation:** snapshot under the lock, release, then iterate:

```go
func IterateModels(fn func(name string, model interface{})) {
	reg := GetDefaultRegistry()
	snapshot := reg.GetAllModels()   // takes and releases the lock
	for name, model := range snapshot {
		fn(name, model)
	}
}
```

### 7. `time.Sleep` spin loops (Medium, Slowness)

`tryLock` (`model_registry.go:128-136`), `tryRLock` (`:140-148`), and the inline loops in
`GetDefaultRegistry`, `SetDefaultRegistry`, `GetModels`.

```go
for i := 0; i < lockRetryAttempts; i++ {
	if r.mutex.TryLock() { return true }
	time.Sleep(lockRetryDelay)   // 1ms
}
```

Problems:

- **Latency floor.** A contended call costs a multiple of 1 ms even if the lock frees after 10 µs,
  because the waiter is asleep. A blocking `Lock` would be handed the mutex in microseconds. So the
  "no-hang" scheme is *slower* in the common contended case, not faster.
- **No fairness.** `sync.Mutex` has a starvation-avoidance mode that hands the lock to a waiter
  queued > 1 ms. `TryLock` participates in none of it, so a try-lock waiter can be starved
  indefinitely by a stream of blocking `Lock` callers (`SetModelRules`, `AddRegistry`,
  `IterateModels` all still block) — see finding 9.
- **Timer churn.** 20 timer allocations per contended call.
- Sleeping in a loop scales badly: 50 concurrent callers each sleep and wake 20 times, producing
  1000 needless scheduler round-trips for what a mutex does with one park/unpark.

**Recommendation:** delete the try-lock helpers. If a bounded wait is genuinely required for an
SLO, express it as `context`-aware acquisition (a buffered-channel semaphore with a `select` on
`ctx.Done()`), which gives a real deadline *and* a real error — not a silent wrong answer.

### 8. `defaultRegistry` read without the guarding mutex (Medium, Locking)

`SetDefaultRegistry` writes `defaultRegistry` (`model_registry.go:110`) under `registriesMutex`.
These read it **without** taking that mutex:

- `RegisterModel` (`model_registry.go:288`)
- `IterateModels` (`model_registry.go:308`, `311`)
- `SetModelRules` (`model_registry.go:354`)
- `GetModelRules` (`model_registry.go:359`)
- `GetDefaultRegistry`'s fallback (`model_registry.go:83`, finding 2)

A data race on the pointer, and semantically these functions may operate on the *previous* default
registry after a swap — so rules set through `SetModelRules` can land on a registry nobody consults
any more.

**Recommendation:** route every access through one accessor that takes the lock (and make
`defaultRegistry` an `atomic.Pointer[DefaultModelRegistry]` if lock-free reads are wanted — that is
the correct way to get the "never blocks" property finding 2 was reaching for).

### 9. Inconsistent locking discipline (Medium, Locking)

Within one 381-line file:

| Function | `registriesMutex` | `r.mutex` |
|---|---|---|
| `GetDefaultRegistry` | `TryRLock` + fallback | — |
| `SetDefaultRegistry` | `TryLock`, no-op on fail | — |
| `AddRegistry` (`:120`) | blocking `Lock` | — |
| `GetModelByName` (`:293`) | blocking `RLock` | via `GetModel` → `TryRLock` |
| `GetModelRulesByName` (`:365`) | blocking `RLock` | `TryRLock` then blocking `RLock` |
| `GetModels` (`:318`) | `TryRLock`, nil on fail | `tryRLock`, skip on fail |
| `RegisterModel` (`:150`) | — | `tryLock`, error on fail |
| `GetModel` (`:198`) | — | `tryRLock`, error on fail |
| `GetAllModels` (`:212`) | — | `tryRLock`, empty on fail |
| `SetModelRules` (`:237`) | — | blocking `Lock` |
| `GetModelRules` (`:252`) | — | blocking `RLock` |
| `IterateModels` (`:307`) | — | blocking `RLock` |

Four different failure behaviours for the same class of event. The mix also means the try-lock
callers can be starved by the blocking ones (finding 7), so the functions that "can never hang" are
the ones most likely to return garbage.

**Recommendation:** pick one discipline — blocking locks with snapshot-and-release — and apply it
uniformly.

### 10. Reflection under the write lock (Low, Slowness + Locking)

`RegisterModel` holds `r.mutex` (write) from `model_registry.go:151` through `:195`, and inside
that window does `reflect.TypeOf` (`:161`), the unwrap loop (`:169-171`), `reflect.New(...).Elem().Interface()`
(`:181`), and another `reflect.TypeOf` (`:185`). None of that touches `r.models`/`r.rules` and none
of it needs the lock.

This directly lengthens the window that makes finding 1 exploitable. Validate first, then take the
lock only for the two map writes.

### 11. Unbounded unwrap loop on a recursive pointer type (Low, Availability)

`model_registry.go:169-171`

```go
for modelType.Kind() == reflect.Pointer || modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array {
	modelType = modelType.Elem()
}
```

`type T *T` is legal Go, and `reflect.Type.Elem()` on it returns itself — so the loop never
terminates. **Verified experimentally:**

```go
type T *T
var x T
tt := reflect.TypeOf(x)            // main.T
for tt.Kind() == reflect.Pointer { tt = tt.Elem() }   // spins on main.T forever
// → "INFINITE LOOP CONFIRMED after 101 iterations, still main.T"
```

Because the loop runs with the write lock held (finding 10), this doesn't just hang one goroutine —
it wedges the registry permanently, at which point every try-lock caller starts returning
"registry locked", which via finding 1 means **authorisation fails open for the rest of the process
lifetime**.

Requires a pathological model type, so exploitability is near zero; the fix is a one-line depth cap
and it converts a permanent fail-open into an error return.

**Recommendation:** bound the loop (`for depth := 0; depth < 16 && ...; depth++`) and return an
error if the cap is hit.

### 12. No panic handling at all (Low, Panic handling)

The package contains **zero** `recover()` calls and never logs — it does not import `pkg/logger`.
For a pure data structure that is a defensible choice, with two caveats:

- `IterateModels` runs a caller callback under a read lock (finding 6). If `fn` panics, the
  `defer RUnlock` does release the lock, so the registry is not wedged — that part is fine — but
  the panic propagates to whatever boundary handler exists, and nothing here records which model
  was being processed. A `logger`-free package can still name the model in a re-panic.
- Every failure mode in the package is reported as a `fmt.Errorf` string with no wrapping and no
  sentinel values, so callers cannot distinguish them (finding 1). That is the panic/error-handling
  defect that actually matters here.

**Recommendation:** define `ErrModelNotFound`, `ErrModelExists`, `ErrRegistryUnavailable` as
sentinels and wrap them, so `errors.Is` works at the security layer.

### 13. Permissive default rules (Low, Security)

`DefaultModelRules()` (`model_registry.go:24-36`) returns `CanRead`, `CanUpdate`, `CanCreate`,
`CanDelete` all `true`. `RegisterModel` applies it to any model registered without explicit rules
(`model_registry.go:191-194`), and `GetModelRules` falls back to it as well (`model_registry.go:266`).

The `CanPublic*` flags default to `false` and `SecurityDisabled` to `false`, which is right. But the
authenticated-path flags default open, so `RegisterModel(name, m)` — the form used by
`pkg/testmodels/business.go` `RegisterTestModels` and the `modelregistry.RegisterModel` convenience wrapper —
yields a fully mutable model. Combined with `pkg/security/hooks.go`'s allow-on-error, the system's
default posture at every layer is permit.

**Recommendation:** default to deny and make permissions opt-in, or at minimum log at registration
time when a model is registered without explicit rules.

---

## What looks right

- The struct-vs-pointer validation in `RegisterModel` (`model_registry.go:160-194`) is careful and
  well-reasoned: it rejects `nil`, unwraps pointer/slice/array to find the base type, rejects
  non-struct kinds with a message naming the original type, normalises a pointer/slice input to a
  zero struct value, and re-checks the final type. The error message even tells the caller to use
  `MyModel{}` instead of `&MyModel{}`. Good API ergonomics.
- Duplicate registration is rejected (`model_registry.go:156-158`) rather than silently overwriting
  — important, since silent overwrite would be a rules-replacement primitive.
- `GetAllModels` returns a **copy** of the map (`model_registry.go:218-222`) rather than the
  internal one, so callers cannot mutate registry state or race on it after the lock is dropped.
  This is the pattern the rest of the package should follow.
- `GetModelByEntity` (`model_registry.go:225-234`) tries `schema.entity` before bare `entity`,
  which is the right precedence and matches what `pkg/security/hooks.go` does.
- `GetModels` de-duplicates by name across registries (`model_registry.go:335-347`), so
  registry-order precedence is consistent with `GetModelByName`'s first-match rule.
- Every `defer` for an acquired lock is correctly paired; there is no missing-`Unlock` path. The
  problems here are about *which* lock discipline was chosen, not about leaking locks.

## Suggested follow-up

Ordered by risk:

1. **Make `pkg/security/hooks.go` fail closed** (finding 1). This is the single change that
   converts a Critical authorisation bypass into a Medium availability issue. It does not require
   touching this package.
2. **Remove the try-lock scheme** (findings 2, 3, 5, 7, 9) and fix the underlying hang by
   snapshotting in `IterateModels` (finding 6).
3. **Make `RegisterModelWithRules` atomic** (finding 4).
4. Route `defaultRegistry` access through a single locked accessor or `atomic.Pointer` (finding 8).
5. Move reflection out of the write-locked region and cap the unwrap loop (findings 10, 11).
6. **Add tests.** This package has none. Priority cases: `-race` test with concurrent
   `RegisterModel` + `GetModelRulesByName` asserting that rules are *never* observed as permissive
   for a restrictively-registered model; a test that `GetModelRulesByName` under contention does
   not return a "not found"-shaped error; `IterateModels` with a callback that calls back into the
   registry (should not deadlock); sentinel-error assertions.
