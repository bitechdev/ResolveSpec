# Audit — `pkg/config`

- **Date:** 2026-09-29
- **Scope:** `pkg/config/{config,dbmanager,manager,paths,server}.go` (1023 LOC source, 608 LOC tests)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client. Config itself is operator-controlled, so the security
  focus here is **insecure defaults that the internet-facing layers inherit**, plus secret handling.

## Summary

Viper-backed configuration with a singleton `Manager`, a large `setDefaults` table, and per-section
validators. Two serious issues:

1. **`Manager` is a data race by construction.** It wraps a `*viper.Viper`, which has **no internal
   locking** (verified: no `sync.Mutex`/`RWMutex` anywhere in `viper@v1.21.0/viper.go`'s `Viper`
   struct), and exposes `Get`/`Set` as concurrently-callable methods on an unsynchronised lazy
   singleton. A concurrent `Set` + `Get` is a concurrent map write → **`fatal error`, not a
   recoverable panic**.
2. **The default configuration is insecure on every axis that matters** — wildcard CORS,
   `sslmode=disable`, `user: postgres` with a blank password — and `Load()` silently succeeds when
   no config file is found, so a misdeployment lands on exactly those defaults with no warning.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | **Critical** | Locking | `Manager.Set`/`Get` over a lock-free `*viper.Viper` → concurrent map write → process-fatal |
| 2 | **High** | Locking | `GetConfigManager()` is an unsynchronised lazy singleton; `NewManager()` also clobbers the global as a side effect |
| 3 | **High** | Security | **OPEN (deferred)** Insecure defaults: `cors.allowed_origins: ["*"]`, `allowed_headers: ["*"]`, `sslmode: disable`, `user: postgres` + blank password |
| 4 | **High** | Security | `SaveConfig` writes all secrets in plaintext at mode `0644` (viper default, never overridden) |
| 5 | Medium | Security | `AddConfigPath(".")` is searched first — CWD config injection |
| 6 | Medium | Observability | `Load()` swallows `ConfigFileNotFoundError` with no log at all |
| 7 | Medium | Correctness | `PathsConfig.Set` on a nil map panics; every sibling method nil-guards |
| 8 | Medium | Locking | `PathsConfig` is a bare `map[string]string` with a mutating `Set` — concurrent access is process-fatal |
| 9 | Medium | Slowness | `GetIPs()` does an uncontexted `net.LookupIP` — blocks on the resolver timeout |
| 10 | Medium | Correctness | `SetConfig` does a pointless `Unmarshal` into a discarded map whose error fails the call |
| 11 | Low | Panic | `GetIPs()` recovers to `fmt.Println`, bypassing the logger, and returns zeroed named results |
| 12 | Low | Security | No validation of `middleware.*` / `event_broker.worker_count` — `0` workers is accepted |
| 13 | Low | Correctness | `ServersConfig.GetDefault()` returns a pointer to a copy of a map value |
| 14 | Low | Security | `PathsConfig.Join` does not confine the result to the base path |

## Resolution status (2026-09-30)

- **#1** — Fixed: `sync.RWMutex` guards every viper access, options included
- **#2** — Fixed: mutex-guarded singleton; `NewManager` no longer touches the global (new `SetConfigManager` publishes explicitly)
- **#4** — Fixed: `SetConfigPermissions(0o600)` plus `chmod 0600` after write (secrets are not stripped)
- **#5** — Fixed: search order is `/etc/resolvespec`, `$HOME/.resolvespec`, `./config`, `.` (CWD last, not dropped)
- **#6** — Partly fixed: `ConfigFileUsed()` added; no log line because `pkg/config` cannot import `logger` (import cycle)
- **#7** — Fixed: `Set` has a pointer receiver and allocates
- **#8** — Not fixed: still a bare map; `Set` documented as not concurrency-safe
- **#9** — Fixed: `LookupIPAddr` with a 2s timeout, fallback normalised to bare IPs and populates the slice
- **#10** — Fixed: dead `Unmarshal` removed, `SetConfig` is atomic
- **#11** — Fixed: recover removed (nothing in the function can panic)
- **#12** — Partly fixed: `Config.Validate()` added, but it is not called from `GetConfig()`. The `*` CORS+credentials check is not implemented
- **#13** — Documented only: `GetDefault` returns a pointer to a copy
- **#14** — Fixed: `Join` errors if the result escapes the base
- **#3** — Open: default flips deferred by decision (breaking change).
- Tests: `pkg/config/hardening_test.go`.

---

## Findings

### 1. `Manager` exposes a lock-free viper as a concurrent API (Critical, Locking)

`manager.go:10-13`, `manager.go:133-158`

```go
type Manager struct {
	v *viper.Viper
}
...
func (m *Manager) Get(key string) interface{}    { return m.v.Get(key) }
func (m *Manager) GetString(key string) string   { return m.v.GetString(key) }
func (m *Manager) Set(key string, value interface{}) { m.v.Set(key, value) }
```

`viper.Viper` carries its configuration in plain maps (`override`, `config`, `defaults`, `aliases`,
…) and has **no mutex**. Verified against the module in use:

```
$ grep -n 'sync\.\|Lock()' $(go env GOMODCACHE)/github.com/spf13/viper@v1.21.0/viper.go
319:	initWG := sync.WaitGroup{}      # inside WatchConfig only
340:		eventsWG := sync.WaitGroup{}  # inside WatchConfig only
```

`Set` writes to `v.override`; `Get` reads across those maps. Because `GetConfigManager()` hands the
*same* `*Manager` to every caller, any code path that calls `Manager.Set` at runtime while another
goroutine reads config is a concurrent map read/write. Go's runtime detects this and issues
`fatal error: concurrent map read and map write` — which **`recover()` cannot catch**, so none of
the panic handlers elsewhere in the codebase will save the process.

This is latent-but-loaded: it needs one runtime `Set` to become a crash. `SetConfig`
(`manager.go:107-131`) performs eleven `m.v.Set` calls, so any dynamic reconfiguration triggers it.

**Recommendation:** add a `sync.RWMutex` to `Manager` and take it in every method that touches
`m.v` (including the `Option` functions at `manager.go:60-85`, which also mutate viper). Better:
load once into an immutable `*Config` at startup and pass that value around, keeping `Manager`
confined to startup.

### 2. Unsynchronised lazy singleton (High, Locking)

`manager.go:15-45`

```go
var configInstance *Manager

func GetConfigManager() *Manager {
	if configInstance == nil {
		configInstance = NewManager()
	}
	return configInstance
}
```

Classic check-then-act race: two concurrent first calls both see `nil`, both build a `Manager`,
and the two callers get *different* instances — so a `Set` through one is invisible through the
other. The unsynchronised pointer write races with the read.

Worse, `NewManager()` (`manager.go:27-45`) assigns `configInstance = &Manager{v: v}` at line 43 as
a **side effect**. So a caller who deliberately builds an isolated manager silently replaces the
global one, and `NewManagerWithOptions` (`manager.go:48-54`) publishes a half-configured manager to
the global *before* applying its options — another goroutine can observe the instance mid-mutation.

**Recommendation:** `sync.Once` for the singleton; remove the global assignment from `NewManager`.

### 3. Insecure-by-default configuration (High, Security)

`manager.go:203-247`:

```go
v.SetDefault("cors.allowed_origins", []string{"*"})
v.SetDefault("cors.allowed_headers", []string{"*"})
...
v.SetDefault("dbmanager.connections.default.user", "postgres")
v.SetDefault("dbmanager.connections.default.password", "")
v.SetDefault("dbmanager.connections.default.sslmode", "disable")
```

Each of these is inherited by an internet-facing layer:

- **`allowed_origins: ["*"]` + `allowed_headers: ["*"]`** — any origin may make cross-origin calls
  with arbitrary headers. Whether this is exploitable depends on whether the CORS middleware also
  sets `Access-Control-Allow-Credentials`; see `audit/pkg/middleware.audit.md` for that
  determination. Even without credentials, wildcard origin plus wildcard headers defeats any
  header-based CSRF defence and lets a malicious page read responses from a
  network-position-authenticated deployment (IP allowlisted, mTLS-terminated, VPN).
- **`sslmode: disable`** — DB traffic unencrypted by default. Every row that crosses the wire,
  including whatever the internet-facing handlers select, is plaintext on the network.
- **`user: postgres` with an empty password** — the default connection targets the PostgreSQL
  superuser. Combined with the identifier-handling concerns in
  `audit/pkg/common.audit.md` / `audit/pkg/restheadspec.audit.md`, running as superuser removes the
  last line of defence (least-privilege) against a query-construction bug.

Because of finding 6, a deployment with a missing or misnamed config file runs on **all** of these
simultaneously and reports success.

**Recommendation:** default to `sslmode: require`, no default DB user/password (fail loudly if
unset), and `cors.allowed_origins: []` with wildcard requiring an explicit opt-in. Add a
`Config.Validate()` that refuses `allowed_origins: ["*"]` together with credentials.

### 4. `SaveConfig` writes secrets in plaintext at 0644 (High, Security)

`manager.go:160-166`

```go
func (m *Manager) SaveConfig(path string) error {
	if err := m.v.WriteConfigAs(path); err != nil { ... }
}
```

`WriteConfigAs` serialises the **entire** merged configuration. That includes
`dbmanager.connections.*.password`, `cache.redis.password`, `event_broker.redis.password` and
`error_tracking.dsn` (a Sentry DSN is a credential).

Viper writes with `v.configPermissions`, which defaults to `0o644`
(`viper@v1.21.0/viper.go:198`). `SetConfigPermissions` is **never called anywhere in this repo**
(verified by grep), so the file is world-readable. Any local user or any other container sharing
the mount can read the DB superuser password.

**Recommendation:** call `v.SetConfigPermissions(0o600)` in `NewManager`; better, strip secret keys
before writing and document that secrets come from env/secret-manager only.

### 5. Current-working-directory config injection (Medium, Security)

`manager.go:32-36`

```go
v.AddConfigPath(".")
v.AddConfigPath("./config")
v.AddConfigPath("/etc/resolvespec")
v.AddConfigPath("$HOME/.resolvespec")
```

Viper searches these **in order** and takes the first hit, so `./config.yaml` wins over
`/etc/resolvespec/config.yaml`. For a daemon this is backwards: the CWD is the least trustworthy of
the four. If the process is ever started with its CWD in a shared or user-writable directory (a
tmp dir, a bind-mounted volume, `/` in some container setups), an attacker with local write
capability redirects the DB connection, disables TLS, or points `error_tracking.dsn` at their own
collector — turning finding 1 of `audit/pkg/errortracking.audit.md` into a full exfiltration path.

**Recommendation:** search `/etc/resolvespec` first, drop `"."` from the default list (keep it
available via `WithConfigPath`), and log the resolved path at startup (`v.ConfigFileUsed()`).

### 6. `Load()` is silent about a missing config file (Medium, Observability)

`manager.go:87-97`

```go
if err := m.v.ReadInConfig(); err != nil {
	if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
		return fmt.Errorf("error reading config file: %w", err)
	}
	// Config file not found; will rely on defaults and env vars
}
return nil
```

The comment is the only trace. No log line, no returned indicator, no `ConfigFileUsed()` report.
A typo in the filename, a wrong working directory, or a container that forgot to mount the
ConfigMap is indistinguishable from a deliberate defaults-only run — and the defaults are the ones
in finding 3.

**Recommendation:** log at info level whether a file was used and which one; expose
`ConfigFileUsed()` on `Manager` so startup can print it.

### 7. `PathsConfig.Set` panics on a nil map (Medium, Panic handling)

`paths.go:38-40`

```go
func (pc PathsConfig) Set(name, path string) {
	pc[name] = path
}
```

`PathsConfig` is `map[string]string` (`config.go:200`). `Get`, `GetOrDefault`, `Has` and `List` all
begin with `if pc == nil`. `Set` does not — and assignment to a nil map is
`panic: assignment to entry in nil map`.

`Config.Paths` is populated by `mapstructure`, which leaves the map nil when the `paths` key is
absent from the file. `setDefaults` does register `paths.data_dir` etc. (`manager.go:249-253`), so
the map is non-nil on the normal `GetConfig()` path — but a `Config` built in code
(`config.Config{}`) or produced by a partial unmarshal has a nil `Paths`, and `Set` on it panics.
Nothing in `pkg/` currently calls `Set` (verified by grep), so this is a latent API defect.

**Recommendation:** nil-guard consistently, or change the receiver to `*PathsConfig` so `Set` can
allocate.

### 8. `PathsConfig` has no synchronisation (Medium, Locking)

Same type: a bare map with a mutating `Set` and reading `Get`/`Has`/`List`/`EnsureDir`/`AbsPath`/
`Join`. If any consumer calls `Set` at runtime while request handlers resolve paths, that is a
concurrent map write — again the **unrecoverable** `fatal error` class, not a panic.

Currently unused outside the package, so severity is capped at Medium. If the intent is a runtime
path registry, it needs a mutex and an unexported map.

### 9. `GetIPs()` blocks on an uncontexted DNS lookup (Medium, Slowness)

`server.go:113-149`

```go
hostname, _ = os.Hostname()
...
addrs, err := net.LookupIP(hostname)
```

`net.LookupIP` has no context and no timeout override — it blocks for the resolver's own timeout,
which on a misconfigured or slow-resolver host is 5 s per attempt and up to ~15–20 s with retries
across `/etc/resolv.conf` entries. In a container whose hostname is not in DNS (the normal case)
this fails, but only *after* the resolver gives up.

There is no caller in `pkg/` today, so it is not on the request path yet. It is exported and
named like a utility, so the risk is that it lands on one.

Secondary correctness problem in the same function: the fallback branch (`server.go:139-147`)
appends `a.String()` for a `net.Addr` from `net.InterfaceAddrs()`, which renders as CIDR
(`192.168.1.5/24`), into the same comma-joined string that the primary branch fills with bare IPs.
Consumers get two formats from one field. That branch also never appends to `ipaddrlist`, so the
third return value is empty whenever the fallback is taken.

**Recommendation:** `net.DefaultResolver.LookupIPAddr(ctx, host)` with a short deadline; cache the
result; normalise the fallback to bare IPs via `net.Addr.(*net.IPNet).IP`.

### 10. `SetConfig` does dead work that can fail the call (Medium, Correctness)

`manager.go:107-131`

```go
configMap := make(map[string]interface{})
if err := m.v.Unmarshal(&configMap); err != nil {
	return fmt.Errorf("failed to prepare config map: %w", err)
}
// configMap is never read again
m.v.Set("servers", cfg.Servers)
...
```

`configMap` is written and then never used. The comment says "Marshal the config to a map structure
that viper can use", but it unmarshals *viper's current state* into a throwaway map — it has
nothing to do with `cfg`. The only effect is that a decode error in the **existing** config makes
`SetConfig` fail for no reason. It also does a full reflective decode of the whole config tree on
every call.

Note also that `SetConfig` stores Go structs into viper via `Set`, and the eleven `Set` calls are
not atomic — a concurrent `GetConfig()` observes a torn config (new `servers`, old `cors`), on top
of finding 1's race.

**Recommendation:** delete the `configMap` block.

### 11. `GetIPs()` panic handling bypasses the logger (Low, Panic handling)

`server.go:114-118`

```go
defer func() {
	if err := recover(); err != nil {
		fmt.Println("Recovered in GetIPs", err)
	}
}()
```

- Writes to stdout with `fmt.Println` rather than `logger.Error`/`logger.HandlePanic`, so the event
  never reaches the error tracker and is invisible to structured log collection.
- No stack trace captured.
- The function's results are named (`hostname, ipList string, ipNetList []net.IP`) but the body
  builds `iplist`/`ipaddrlist` **locals** and only assigns via the `return` statements. On a panic,
  the deferred recover swallows it and the function returns the *zero* named values — `ipNetList`
  is nil rather than the empty slice callers might expect. Silent empty success.

`pkg/config` is otherwise the only package outside `pkg/logger` that hand-rolls a recover instead
of using the shared helpers.

**Recommendation:** use `defer logger.CatchPanic("GetIPs")()`, or drop the recover — there is no
panicking operation in this function for it to catch.

### 12. No validation of numeric/limit settings (Low, Security)

`ServerInstanceConfig.Validate` (`server.go:37-68`) and `ServersConfig.Validate`
(`server.go:71-95`) are good — port range, mutually-exclusive TLS modes, cert/key pairing,
AutoTLS domains. But nothing validates:

- `middleware.rate_limit_rps` / `rate_limit_burst` — `0` disables rate limiting silently.
- `middleware.max_request_size` — `0` may mean unlimited depending on the middleware; see
  `audit/pkg/middleware.audit.md`.
- `event_broker.worker_count` (default 10) — `0` means no consumers; see
  `audit/pkg/eventbroker.audit.md` for whether that deadlocks publishers or drops events.
- `dbmanager.max_open_conns`, retry counts/delays — negative or zero values.
- `cors.allowed_origins: ["*"]` in combination with credentials.

There is also no top-level `Config.Validate()` that calls the section validators, so nothing
guarantees `ServersConfig.Validate` ever runs.

**Recommendation:** add `func (c *Config) Validate() error` that fans out to every section, and
call it from `GetConfig()`.

### 13. `GetDefault()` returns a pointer to a copy (Low, Correctness)

`server.go:98-110`

```go
instance, ok := sc.Instances[sc.DefaultServer]
...
return &instance, nil
```

`instance` is a copy of the map value. A caller that mutates through the returned pointer — which
the `*ServerInstanceConfig` receiver on `ApplyGlobalDefaults` (`server.go:12`) invites — changes
only the copy, and `sc.Instances` is unaffected. This is exactly the shape of bug where timeouts
appear to be applied but aren't.

**Recommendation:** make `Instances` a `map[string]*ServerInstanceConfig`, or return by value.

### 14. `PathsConfig.Join` does not confine to the base (Low, Security)

`paths.go:96-104`

```go
parts := append([]string{base}, elem...)
return filepath.Join(parts...), nil
```

`filepath.Join` calls `Clean`, which *resolves* `..` rather than rejecting it: `Join("data",
"../../etc/passwd")` returns `../etc/passwd`. Any consumer that passes a request-derived segment
gets directory traversal out of the configured base. No consumer does today, hence Low, but the
method's name promises confinement it does not provide.

**Recommendation:** after joining, verify `strings.HasPrefix(filepath.Clean(result), filepath.Clean(base)+string(os.PathSeparator))`, or use `os.Root`/`filepath.Localize` on the elements.

---

## What looks right

- `ServerInstanceConfig.Validate` / `ServersConfig.Validate` (`server.go:37-95`) are thorough:
  port bounds, mutual exclusion of the three TLS modes, cert/key co-presence, AutoTLS domain
  requirement, and a key-vs-`Name` consistency check on the instances map. This is the strongest
  code in the package.
- `ApplyGlobalDefaults` (`server.go:12-32`) uses `*time.Duration` fields so "unset" is
  distinguishable from "zero" — the right modelling choice, and it copies into a fresh local
  before taking its address rather than aliasing the loop/parameter variable.
- `Load()` correctly distinguishes `ConfigFileNotFoundError` from real read errors instead of
  treating every failure as fatal (the *silence* is the problem, not the branch).
- `SetEnvPrefix("RESOLVESPEC")` + `SetEnvKeyReplacer(".", "_")` + `AutomaticEnv`
  (`manager.go:38-41`) is the correct trio for env overrides, and because every key has a
  registered default, `AutomaticEnv` actually resolves nested keys — so secrets *can* be supplied
  via env instead of the file. That's the mitigation for finding 4, and it should be documented as
  the only supported way to pass secrets.
- The defaults table is comprehensive and one place — easy to review, which is how findings 3 and
  12 were found.
- Test coverage is reasonable for a config package (608 LOC of tests against 1023 of source),
  though it does not cover concurrency, `SaveConfig` permissions, or `PathsConfig.Set`.

## Suggested follow-up

1. Lock `Manager` or make config immutable after load (findings 1, 2). Until then, treat
   `Manager.Set` as unsafe to call after startup and consider removing it from the public API.
2. Flip the insecure defaults and add `Config.Validate()` (findings 3, 12).
3. `SetConfigPermissions(0o600)` and secret-stripping in `SaveConfig` (finding 4).
4. Reorder the config search path and log the resolved file (findings 5, 6).
5. Delete the dead `Unmarshal` in `SetConfig` (finding 10).
