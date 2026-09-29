# Audit: `pkg/metrics`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/metrics` |
| **Files** | `interfaces.go` (98), `config.go` (64), `prometheus.go` (312), `example_test.go` (64) |
| **Audit date** | 2026-09-29 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |
| **Depth** | lighter (small, mostly-declarative package) — but reachability is traced into every consumer |

## Summary

`pkg/metrics` is the best-behaved global-state package in this repository: the
provider singleton is properly mutex-guarded and returns a working
`NoOpProvider` instead of `nil` (`interfaces.go:50-72`), which is the pattern
`pkg/logger` and `pkg/cache` should copy (see `_CROSS-CUTTING.audit.md`
finding X5). Prometheus client types are goroutine-safe, so there are **no
locking or data-race defects in this package**.

The dominant finding is not a defect in any one function but the state of the
subsystem as a whole: **no provider is ever installed.** `metrics.SetProvider` is
called only from `_test.go` files, and `NewPrometheusProvider` has no non-test
caller anywhere in the repository. Every `GetProvider()` therefore returns
`&NoOpProvider{}`, so all 39 database instrumentation points, the panic counter,
and the event-broker metrics discard their observations, and `/metrics` — if it
were routed — answers `404 Metrics provider not configured`. The instrumentation
is written, tested and wired, and then goes nowhere.

That fact reclassifies most of the rest. Two genuine label-cardinality hazards
exist (findings 2 and 5), but neither is live today: the HTTP middleware that
would create them has no callers, and the database labels are in practice bounded
by the model registry, because `pkg/restheadspec` returns early for an
unregistered `schema.entity` before any query is built (`handler.go:130-141`).
They are recorded here as **latent** — the traps that spring on whoever first
installs a real provider, which is exactly what fixing finding 1 requires. Fix
finding 1 without findings 2 and 5 and the result is a remotely triggerable
memory leak.

Secondary themes: a duplicate-registration panic at construction, an
unauthenticated handler that exposes the default registry, a config struct whose
`Enabled` and `Provider` fields are never read and have no section in
`pkg/config` to bind to, and a Pushgateway goroutine that discards its errors
under a comment admitting it should log.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **High** | correctness / logging | No provider is ever installed: all instrumentation is silently a no-op and `/metrics` 404s |
| 2 | **Medium** (latent) | security / slowness | `path` label is `r.URL.Path` — unbounded cardinality from any client, once `Middleware` is mounted |
| 3 | **Medium** | panic handling | `promauto` panics on duplicate registration; `NewPrometheusProvider` cannot be called twice and returns no error |
| 4 | **Medium** (latent) | security | `Handler()` returns `promhttp.Handler()` — no auth, and exposes the default registry including Go/process collectors |
| 5 | **Medium** (latent) | security / slowness | DB label values have no length or charset bound, and the fallback puts a 120-char SQL fragment in the `entity` label |
| 6 | **Medium** | correctness | `Config.Enabled` and `Config.Provider` are never read; there is no provider factory |
| 7 | **Medium** | logging | `startAutoPush` discards every push error with `_ = err`; the comment says a logger is needed; no `recover()` |
| 8 | **Medium** | panic handling | `StopAutoPush` panics on a second call and does not wait for the goroutine |
| 9 | **Medium** | correctness | `tableFromRawQuery` labels a subquery-in-`FROM` with the literal table name `SELECT` |
| 10 | **Low** | correctness | `ResponseWriter` drops `http.Flusher`/`http.Hijacker`/`io.ReaderFrom`, breaking SSE and WebSocket upgrade |
| 11 | **Low** | correctness | `ResponseWriter.statusCode` stays 200 when the handler never calls `WriteHeader` |
| 12 | **Low** | correctness | `eventDuration` reuses `DBQueryBuckets`, so a `db_query_buckets` change silently reshapes event histograms |
| 13 | **Low** | correctness | `PushgatewayInterval` is an untyped `int` of seconds, inconsistent with the duration strings used elsewhere |
| 14 | **Low** | correctness | `Push()` returns `nil` when Pushgateway is unconfigured, so a caller cannot tell "pushed" from "no-op" |
| 15 | **Low** | correctness | `Pusher` uses `prometheus.DefaultGatherer`, ignoring which registry the metrics went to |
| 16 | **Low** | security | `NoOpProvider.Handler()` returns a 404 body that fingerprints the metrics subsystem |
| 17 | **Low** | correctness | `metrics.Config` has no section in `pkg/config`, so its `mapstructure` tags bind to nothing |
| 18 | **Low** | slowness | `WithLabelValues` resolves a child by string on every observation; no pre-curried handles |

---

### 1. High — the metrics subsystem is never activated

`GetProvider()` falls back to a no-op when no provider has been set
(`interfaces.go:63-72`):

```go
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

That fallback is sound design. The problem is that nothing ever replaces it.
Searching the whole module for the two functions that would install a provider:

| Symbol | Non-test callers | Test-only callers |
|---|---|---|
| `metrics.SetProvider` | **0** | `pkg/middleware/panic_test.go:32-33`, `pkg/metrics/example_test.go:13`, `:32`, `pkg/common/adapters/database/query_metrics_test.go` (9 pairs, `:106` … `:347`) |
| `metrics.NewPrometheusProvider` | **0** | `pkg/metrics/example_test.go:12`, `:31` |

Only three files in the repository import `pkg/metrics` outside of tests —
`pkg/eventbroker/metrics.go`, `pkg/middleware/panic.go` and
`pkg/common/adapters/database/query_metrics.go` — and all three are *producers*
that call `GetProvider()`. No `cmd/`, no `pkg/server`, and no example installs a
provider.

**Failure scenario.** Everything below is instrumented and none of it records
anything:

- **39 database call sites** (`bun.go` 12, `pgsql.go` 17, `gorm.go` 10) route
  through `recordQueryMetrics` (`query_metrics.go:15`), which calls
  `metrics.GetProvider().RecordDBQuery(...)` (`:20-27`) — into the no-op. Note
  these are additionally gated on a per-connection `metricsEnabled` flag
  (`:16-18`) whose config default is `false`
  (`pkg/config/manager.go:246`), so there are **two** independent switches, both
  off.
- **Panic counting.** `pkg/middleware/panic.go:19` calls
  `metrics.GetProvider().RecordPanic(...)`, so `panics_total` never increments.
  A service can panic on every request and the metric an operator would alert on
  stays at zero — the failure is invisible in precisely the way metrics exist to
  prevent.
- **Event broker.** `pkg/eventbroker/metrics.go:11`, `:18`, `:25` likewise.
- **`/metrics`.** `NoOpProvider.Handler()` (`interfaces.go:90-98`) returns
  `404 Metrics provider not configured`. A Prometheus scrape job configured
  against this service fails every scrape, and `up == 0` is the only signal.

The severity is in the silence. Nothing logs "metrics disabled", no startup
warning fires, and `NoOpProvider` satisfies the interface perfectly — so the
code reads as fully instrumented at every call site. An operator who enables
`enable_metrics: true` on a connection, expecting query metrics, gets nothing and
has no diagnostic to explain why. The cost already paid — ~39 instrumented call
sites, 350+ lines of `query_metrics.go`, a tested `PrometheusProvider` — delivers
zero observability.

**Recommendation.** Install a provider during startup, from config, and say so:

```go
// in server bootstrap, after config load
provider, err := metrics.NewProvider(&cfg.Metrics)   // see finding 6
if err != nil {
	return fmt.Errorf("metrics: %w", err)
}
metrics.SetProvider(provider)
logger.Info("Metrics initialized: provider=%s namespace=%q", cfg.Metrics.Provider, cfg.Metrics.Namespace)
```

and route the handler on an internal listener (finding 4). Order matters:
`SetProvider` must run before the first request is served, since the DB adapters
capture nothing before it. **Fix findings 2, 4 and 5 in the same change** — they
are dormant only because this one is unfixed. Add a startup `logger.Warn` when
`enable_metrics` is true on any connection while the global provider is still a
no-op; that single line turns this class of silent misconfiguration into a log
message.

---

### 2. Medium (latent) — `path` label is the raw request path

`prometheus.go:258-278`:

```go
func (p *PrometheusProvider) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Increment in-flight requests
		p.IncRequestsInFlight()
		defer p.DecRequestsInFlight()

		// Wrap response writer to capture status code
		rw := NewResponseWriter(w)

		// Call next handler
		next.ServeHTTP(rw, r)

		// Record metrics
		duration := time.Since(start)
		status := strconv.Itoa(rw.statusCode)

		p.RecordHTTPRequest(r.Method, r.URL.Path, status, duration)
	})
}
```

feeding metrics declared with `[]string{"method", "path", "status"}`
(`prometheus.go:64`, `:71`). `RecordHTTPRequest` calls
`WithLabelValues(method, path, status)` (`:192-193`), which **creates and
permanently retains** a child series per unseen tuple. `prometheus.HistogramVec`
has no eviction and no cardinality cap.

**Why this is latent, not live.** `Middleware` has **no callers** in the
repository, and `RecordHTTPRequest`/`IncRequestsInFlight` have none either — so
the HTTP metrics are dead code today, on top of finding 1. This is a trap, not a
live vulnerability.

**Failure scenario, once mounted.** An attacker requests `GET /api/public/orders/1`,
`/2`, `/3`, … or simply `GET /<random>` in a loop. Every distinct path creates
one `http_request_duration_seconds` child — with the default 11 buckets
(`config.go:43`) that is 11 counters plus `_sum`, `_count` and the retained label
strings, on the order of 400–600 bytes of live heap — plus one
`http_requests_total` child. A million distinct paths, reachable in minutes at a
few thousand requests per second and requiring **no valid credential** (the label
is recorded whatever the status), is several hundred megabytes of permanently
retained heap. The process is OOM-killed, and because the series live in the
default registry they cannot be dropped without a restart. The same requests
inflate every scrape into a multi-megabyte response, so the monitoring system
amplifies the attack and Prometheus's own ingestion suffers.

No hostile client is even needed: a REST API with ID-bearing paths — which this
project is — generates unbounded cardinality from ordinary traffic. `method` is a
second attacker-controlled axis, since an arbitrary request method string becomes
a label value unchecked.

**Recommendation.** Label with the matched route pattern, never the URL. Because
the pattern is only known after `ServeHTTP`, resolve it there:

```go
next.ServeHTTP(rw, r)

route := "unmatched"
if cr := mux.CurrentRoute(r); cr != nil {
	if tpl, err := cr.GetPathTemplate(); err == nil {
		route = tpl
	}
}
p.RecordHTTPRequest(normalizeMethod(r.Method), route, status, duration)
```

(`chi.RouteContext(r.Context()).RoutePattern()` for chi.) Normalize `method`
against the known verbs, mapping anything else to `"OTHER"`. Add a hard backstop
independent of the router: track distinct label tuples per metric and collapse
anything past a few hundred to `"other"`, so no future consumer can reintroduce
this.

---

### 3. Medium — `promauto` panics on duplicate registration

Every metric is built with `promauto.New*` (`prometheus.go:58`–`:150`), which
registers into `prometheus.DefaultRegisterer` and **panics** on an
`AlreadyRegisteredError`. The constructor has no `recover()` and returns no
error:

```go
func NewPrometheusProvider(cfg *Config) *PrometheusProvider {
```

**Failure scenario.** Calling `NewPrometheusProvider` twice in one process —
two config sections, a reload path, a test constructing a provider per case, or
an embedding application that also configures metrics — panics with
`duplicate metrics collector registration attempted`. From `init` or `TestMain`
that is a hard abort; from a config-reload handler it takes down a running
server. Since the signature has no `error`, a caller cannot defend against it
without wrapping the call in its own `recover()`.

This is reachable **today in tests**: `example_test.go:12` and `:31` both call
`NewPrometheusProvider` in the same package. Those pass only because
`ExampleNewPrometheusProvider_custom` sets `Namespace` — `metricName`
(`prometheus.go:50-55`) prefixes names only when `cfg.Namespace != ""`, so the
two providers register under different names. Drop that namespace and the test
binary panics. The failure depends on configuration, not on code, which makes it
easy to ship.

**Recommendation.** Return an error and register against an injectable registry:

```go
func NewPrometheusProvider(cfg *Config) (*PrometheusProvider, error) {
	...
	reg := cfg.Registry
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	p := &PrometheusProvider{...}   // prometheus.NewHistogramVec, not promauto
	for _, c := range p.collectors() {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				continue   // or adopt are.ExistingCollector
			}
			return nil, fmt.Errorf("metrics: register %T: %w", c, err)
		}
	}
	return p, nil
}
```

An owned `*prometheus.Registry` also fixes findings 4 and 15.

---

### 4. Medium (latent) — the metrics handler has no access control

`prometheus.go:252-255`:

```go
func (p *PrometheusProvider) Handler() http.Handler {
	return promhttp.Handler()
}
```

`promhttp.Handler()` serves `prometheus.DefaultGatherer` — which, because the
default registry is used, includes the Go collector (`go_*`: goroutine count,
heap sizes, GC pauses, **Go version**) and the process collector (`process_*`:
resident memory, open file descriptors, start time, CPU seconds) alongside the
application metrics. There is no authentication, no IP restriction, and no
timeout or in-flight limit.

Latent for two reasons: no provider is installed (finding 1), and nothing routes
`Handler()` today. It becomes live the moment finding 1 is fixed in the obvious
way.

**Failure scenario.** Mounted on the public listener — the natural thing to do,
since `Handler()` is the package's only HTTP surface and the interface comment
says "e.g., /metrics endpoint" (`interfaces.go:46`) — any client learns the Go
version and build (fingerprinting against known CVEs), in-flight request counts
and traffic volume, cache hit/miss ratios, per-table database query latencies,
panic counts, and — via findings 2 and 5 — the set of paths and SQL shapes the
application uses. `process_open_fds` and `go_goroutines` also make the
effectiveness of a resource-exhaustion attack directly observable, letting an
attacker tune it in real time.

**Recommendation.** Serve metrics on a separate listener bound to a private
interface. `pkg/config` already supports multiple server instances
(`servers.instances.*`, `pkg/config/manager.go:182-186`), so this is
configuration plus a documented pattern. If it must share the public listener,
require authentication and bound the handler:

```go
func (p *PrometheusProvider) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		ErrorHandling:       promhttp.HTTPErrorOnError,
		Timeout:             10 * time.Second,
		MaxRequestsInFlight: 3,
	})
}
```

Using an owned registry (finding 3) also stops `go_*`/`process_*` from leaking;
register `collectors.NewGoCollector()` deliberately if those are wanted.

---

### 5. Medium (latent) — DB label values are unbounded by design, and the fallback puts SQL text in a label

`db_query_duration_seconds` uses `[]string{"operation", "schema", "entity", "table"}`
and `db_queries_total` adds `"status"` (`prometheus.go:86`, `:93`) — a four- and
five-dimensional label space. The values are produced by
`pkg/common/adapters/database/query_metrics.go` and normalized by four helpers
(`:30-66`) whose only sanitizer is `cleanMetricIdentifier` (`:330-335`):

```go
func cleanMetricIdentifier(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'`[]")
	value = strings.TrimRight(value, ";")
	return value
}
```

That trims quotes and a trailing semicolon. It does **not** bound length, does
not restrict the character set, and does not check against any allowlist.

**What bounds this today.** Ten of the 39 `recordQueryMetrics` call sites parse
their labels out of raw SQL (`bun.go:195`, `:215`, `:1731`, `:1739`;
`pgsql.go:128`, `:154`, `:1092`, `:1106`; `gorm.go:144`, `:165`). The rest take
them from the model-derived helpers `schemaAndTableFromModel` /
`entityNameFromModel` — 27 call sites, nine in each of `bun.go`, `pgsql.go` and
`gorm.go` — and `schemaAndTableFromModel` (`:89-96`) returns
`parseTableName(provider.TableName(), driverName)`, the model's own declared
name rather than request input. Critically, `pkg/restheadspec` resolves the
model **before** building any query and returns early when it does not exist
(`handler.go:130-141`):

```go
model, err := h.registry.GetModelByEntity(schema, entity)
if err != nil {
	// Model not found - call fallback handler if set, otherwise pass through
	logger.Debug("Model not found for %s.%s", schema, entity)
	...
	return
}
```

So `GET /api/<random>/<random>` never reaches the database, and the label space
is in practice bounded by the number of registered models. **The cardinality
risk is latent, resting on a guarantee enforced elsewhere and nowhere documented
here.**

**Failure scenario.** Two things remain wrong regardless:

- **SQL text as a label value.** When no table can be parsed,
  `fallbackMetricEntityFromQuery` (`:149-160`) puts up to 120 characters of the
  statement (`maxMetricFallbackEntityLength`, `:13`) into `entity`. The fallback
  triggers when `tableFromRawQuery` returns `""` (`:290-308`), i.e. whenever the
  first keyword is not `SELECT`/`INSERT`/`UPDATE`/`DELETE` — a `WITH` CTE,
  `EXPLAIN`, `CALL`, or a leading comment. Since `pkg/restheadspec` builds raw
  SQL from request-supplied filters, sort and `custom_sql_where`/`custom_sql_join`
  fragments (see `FetchRowNumber`, `handler.go:3198`), the label then varies with
  **placeholder arity**: `IN (?)`, `IN (?,?)`, `IN (?,?,?)` are three distinct
  shapes, so one client varying a filter list length mints a new permanent series
  per length. On an unauthenticated `/metrics` (finding 4), that same label
  discloses table names, join structure, and the presence of soft-delete and
  tenancy predicates. `sanitizeMetricQueryShape` (`:162-234`) is careful and does
  strip quoted literals and `?`/`$n` placeholders, so **parameter values do not
  leak** — the structure does.
- **No defensive bound.** `cleanMetricIdentifier` is the single choke point for
  every identifier label, and it enforces nothing. The safety property lives in a
  different package, so any new caller — a migration tool, an admin endpoint, a
  fallback handler that does reach the DB — silently reintroduces unbounded
  cardinality with no review signal.

**Recommendation.**

1. **Bound every label value at the choke point**, so the guarantee is local:

```go
var metricIdentRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

func cleanMetricIdentifier(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "\"'`[]")
	value = strings.TrimRight(value, ";")
	if !metricIdentRe.MatchString(value) {
		return ""      // callers already map "" to "default"/"unknown"
	}
	return value
}
```

2. **Delete the query-shape fallback** — return `"unknown"`. A truncated SQL
   statement is not a label value; if the shape is wanted for debugging, log it
   at debug level or attach it to a trace span, which has no cardinality
   contract.
3. **Resolve identifiers against the model registry** in `query_metrics.go`
   rather than trusting SQL parsing, so the bound is explicit rather than
   inherited.
4. **Collapse `entity` and `table`.** They are identical whenever a table is
   parsed (`:145` sets `entity = cleanMetricIdentifier(table)`), so the fourth
   dimension buys nothing and widens every tuple.
5. Add the cardinality backstop from finding 2 in `pkg/metrics` itself.

---

### 6. Medium — `Config.Enabled` and `Config.Provider` are never read

`config.go:4-6` declares the flag:

```go
type Config struct {
	// Enabled determines whether metrics collection is enabled
	Enabled bool `mapstructure:"enabled"`
```

`DefaultConfig()` sets it to `true` (`:40`); `ApplyDefaults()` (`:50-64`) never
touches it; `NewPrometheusProvider` never reads it. The same holds for
`Config.Provider` (`:9`), defaulted in two places (`:41`, `:52`) and never read —
nothing in the package dispatches on it to choose between `prometheus` and
`noop`, because no factory exists.

**Failure scenario.** An operator sets `enabled: false` to stop collection —
perhaps because finding 2 or 5 is consuming memory. Nothing happens.
`NewPrometheusProvider` still registers every collector and records every
observation; the only thing that decides is which constructor the caller
hard-coded. Setting `provider: noop` fails identically. The configuration lies,
and silently: no warning, no log line. Combined with finding 17 (no `metrics`
section exists to set these in) the config surface is entirely decorative.

**Recommendation.** Add the factory the config implies, and make it the only
supported entry point:

```go
func NewProvider(cfg *Config) (Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	cfg.ApplyDefaults()
	if !cfg.Enabled {
		return &NoOpProvider{}, nil
	}
	switch cfg.Provider {
	case "prometheus":
		return NewPrometheusProvider(cfg)   // per finding 3
	case "noop":
		return &NoOpProvider{}, nil
	default:
		return nil, fmt.Errorf("metrics: unknown provider %q", cfg.Provider)
	}
}
```

Erroring on an unknown name matters — today a typo would fall through to
whatever the caller chose.

---

### 7. Medium — the Pushgateway goroutine discards every error

`prometheus.go:290-304`:

```go
func (p *PrometheusProvider) startAutoPush() {
	for {
		select {
		case <-p.pushTicker.C:
			if err := p.Push(); err != nil {
				// Log error but continue pushing
				// Note: In production, you might want to use a proper logger
				_ = err
			}
		case <-p.pushStop:
			p.pushTicker.Stop()
			return
		}
	}
}
```

The comment states the requirement and the code does the opposite. There is also
no `recover()` in this goroutine.

**Failure scenario.** The Pushgateway URL is wrong, DNS fails, or the gateway
rejects the payload. Every tick fails, forever, in complete silence — no log
line, no counter, nothing. Pushgateway is chosen for short-lived or non-scrapable
workloads, so those are exactly the deployments where **there is no other path
for metrics to arrive**: the observability system is dark and nothing says so.
Discovery happens when someone notices a dashboard has been empty for a week.

The missing `recover()` compounds it: `push.Pusher.Push` gathers from
`DefaultGatherer`, so a panicking custom collector would crash the whole process
from a background goroutine with no handler in the stack — the pattern flagged in
`_CROSS-CUTTING.audit.md` finding X7.

**Recommendation.** `pkg/metrics` already imports `pkg/logger`
(`interfaces.go:8`), so this costs nothing:

```go
func (p *PrometheusProvider) startAutoPush() {
	defer logger.CatchPanicCallback("metrics.startAutoPush", nil)()
	failures := 0
	for {
		select {
		case <-p.pushTicker.C:
			if err := p.Push(); err != nil {
				failures++
				if failures == 1 || failures%60 == 0 {
					logger.Warn("metrics: pushgateway push failed (%d consecutive): %v", failures, err)
				}
				continue
			}
			failures = 0
		case <-p.pushStop:
			p.pushTicker.Stop()
			return
		}
	}
}
```

The backoff matters: an unconditional `Warn` per tick forwards a message to
Sentry per tick (`_CROSS-CUTTING.audit.md` finding X8), turning a broken gateway
into a second incident. Add a `metrics_push_failures_total` counter so the
failure is visible in whatever monitoring *is* working.

---

### 8. Medium — `StopAutoPush` panics on a second call and does not wait

`prometheus.go:308-312`:

```go
func (p *PrometheusProvider) StopAutoPush() {
	if p.pushStop != nil {
		close(p.pushStop)
	}
}
```

`close` of an already-closed channel panics with `close of closed channel`. The
check guards only `nil`, not "already closed", and the method is documented as a
shutdown hook — the code most likely to run twice. (`pushStop` is declared
`chan bool` at `prometheus.go:35` and created at `:163`; the value sent is never
read, so `chan struct{}` is the more honest type.)

**Failure scenario.** A shutdown path calls `StopAutoPush` and a deferred cleanup
or signal handler calls it again — or two signals arrive. The second `close`
panics **during shutdown**, where `pkg/middleware/panic.go` is not in the stack
because this is not a request. The process aborts mid-drain, losing the in-flight
requests that the 25-second `servers.drain_timeout`
(`pkg/config/manager.go:176`) exists to protect.

Separately, the method returns without waiting for `startAutoPush` to observe the
close. A push in progress is abandoned and the process's final interval of
metrics is lost — precisely the interval a Pushgateway deployment most wants.

**Recommendation.** `sync.Once` plus a done channel, making shutdown idempotent
and synchronous:

```go
type PrometheusProvider struct {
	...
	pushStop     chan struct{}
	pushStopOnce sync.Once
	pushDone     chan struct{}
}

func (p *PrometheusProvider) StopAutoPush() {
	if p.pushStop == nil {
		return
	}
	p.pushStopOnce.Do(func() { close(p.pushStop) })
	<-p.pushDone
}
```

with `defer close(p.pushDone)` in `startAutoPush` and a final `p.Push()` before
it returns. Consider `StopAutoPush(ctx)` so the wait is bounded.

---

### 9. Medium — a subquery in `FROM` is labelled as a table named `SELECT`

`tableFromRawQuery` (`query_metrics.go:290-308`) takes the first token after the
first `FROM`, and `tokenizeQuery` (`:319-328`) replaces `(` and `)` with spaces:

```go
func tokenizeQuery(query string) []string {
	replacer := strings.NewReplacer(
		"\n", " ", "\t", " ", "(", " ", ")", " ", ",", " ",
	)
	return strings.Fields(replacer.Replace(query))
}
```

so for `SELECT ... FROM ( SELECT ... )` the token after `FROM` is the inner
`SELECT`. `parseTableName("SELECT", driver)` then yields `table = "SELECT"`, and
`metricTargetFromRawQuery` sets `entity = cleanMetricIdentifier(table)` (`:145`),
so both identifier labels become the keyword.

**Failure scenario.** `FetchRowNumber` (`pkg/restheadspec/handler.go:3114`)
builds exactly that shape — an outer select over a `ROW_NUMBER() OVER (...)`
subquery, assembled with `fmt.Sprintf` at `:3173-3189` — and executes it through
the raw path (`db.Query(ctx, &result, queryStr, pkValue)`, `:3198`). Its metrics are recorded with
`entity = "SELECT"` and `table = "SELECT"`. Every such query across every table
collapses into one series named after a keyword. An operator investigating slow
cursor pagination sees a `db_query_duration_seconds{table="SELECT"}` bucket with
no indication of which table is slow, while the real table's own series omits
this traffic entirely — the metric is not merely missing but **actively
misleading**, and it silently aggregates unrelated tables into one latency
distribution.

The same tokenizer means `tokenAfter` matches a `FROM` appearing anywhere,
including inside a subquery or a `CASE` expression, so the first `FROM` is not
reliably the outer one.

**Recommendation.** Treat an unparseable target as unknown rather than guessing.
Reject any candidate that is a SQL keyword before accepting it:

```go
var sqlKeywords = map[string]struct{}{
	"select": {}, "insert": {}, "update": {}, "delete": {}, "with": {},
	"values": {}, "set": {}, "where": {}, "from": {}, "join": {}, "as": {},
}

func tokenAfter(tokens []string, keyword string) string {
	for idx, token := range tokens {
		if strings.EqualFold(token, keyword) && idx+1 < len(tokens) {
			cand := cleanMetricIdentifier(tokens[idx+1])
			if _, isKw := sqlKeywords[strings.ToLower(cand)]; isKw {
				return ""
			}
			return cand
		}
	}
	return ""
}
```

Better: have the callers that already know the target pass it explicitly.
`FetchRowNumber` knows `tableName` — threading it into the metric call is more
accurate than any amount of SQL parsing, and removes the guesswork for the
highest-traffic raw query in the codebase. Note that with finding 5's
recommendation applied, `""` here means `entity`/`table` become `"unknown"`
rather than falling back to a SQL fragment.

---

### 10. Low — `ResponseWriter` drops optional interfaces

`prometheus.go:172-176`:

```go
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
}
```

Embedding promotes only `Header`, `Write` and `WriteHeader` (the latter
overridden at `:185-188`). The wrapper does not implement `http.Flusher`,
`http.Hijacker`, `http.Pusher` or `io.ReaderFrom`, so a handler's type assertion
for any of them fails once this middleware is in the chain.

**Failure scenario.** This repository has `pkg/websocketspec`. A WebSocket
upgrade requires `w.(http.Hijacker)`; with this middleware in front, the
assertion fails and `gorilla/websocket` returns
`websocket: response does not implement http.Hijacker`. **Every WebSocket
connection fails**, and the cause is a metrics wrapper several layers up — a hard
bug to locate, since removing the middleware "fixes" it and the code looks
unrelated. The same applies to SSE, chunked streaming and long-polling: without
`Flusher`, output buffers until the handler returns. Losing `io.ReaderFrom`
silently disables `sendfile` for large bodies.

**Recommendation.** Implement the optional interfaces with pass-through and a
capability check, and add `Unwrap`:

```go
func (rw *ResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("metrics: ResponseWriter does not support Hijack")
}

func (rw *ResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
```

`Unwrap` is what Go 1.20+'s `http.ResponseController` uses, making the wrapper
transparent to any handler written against that API. Better still, use
`httpsnoop` or `ResponseController` and delete the wrapper. Audit the other
response wrappers in `pkg/middleware` and `pkg/server` for the same defect.

---

### 11. Low — `statusCode` stays 200 when nothing was written

`NewResponseWriter` seeds `statusCode: http.StatusOK` (`prometheus.go:178-183`),
correct for Go's implicit-200 behaviour, and the middleware records it
unconditionally after `ServeHTTP` returns (`:273-276`).

**Failure scenario.** A handler panics. If `PanicRecovery` is *inside* the
metrics middleware it writes a 500 and the label is right; if it is *outside* —
or if the panic escapes to `net/http`'s own recovery, which writes nothing —
`statusCode` is still 200 and a failed request is counted as a success.
Identically, a client that disconnects mid-response, or a handler that returns
without writing, is recorded as `status="200"`. Error-rate alerts built on
`http_requests_total{status=~"5.."}` under-report exactly the failures they exist
to catch.

**Recommendation.** Track whether the header was written and label accordingly:

```go
type ResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *ResponseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return          // also suppresses the "superfluous WriteHeader" warning
	}
	rw.statusCode, rw.wroteHeader = code, true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true      // implicit 200
	}
	return rw.ResponseWriter.Write(b)
}
```

Use a distinct `"none"` status when `!wroteHeader`, and document that
`PanicRecovery` must be mounted **inside** the metrics middleware so panics are
observed as 500s. Ordering is covered in `middleware.audit.md` and
`server.audit.md`.

---

### 12. Low — event histograms reuse the DB query buckets

`prometheus.go:130-137`:

```go
eventDuration: promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    metricName("event_processing_duration_seconds"),
		Help:    "Event processing duration in seconds",
		Buckets: cfg.DBQueryBuckets, // Events are typically fast like DB queries
	},
	[]string{"source", "event_type"},
),
```

There is no `EventBuckets` field in `Config`.

**Failure scenario.** An operator tunes `db_query_buckets` for their database —
say narrowing the top bucket to 1s because queries are fast — and silently
changes the resolution of event-processing histograms too. Event handlers do
arbitrary work, including outbound HTTP, so their latency distribution has no
reason to match a query's. Observations above the last bucket land only in
`+Inf`, so `histogram_quantile` returns the last boundary and a slow consumer is
hidden. The coupling is invisible from the config file.

**Recommendation.** Add `EventBuckets []float64` to `Config` with its own default
in `DefaultConfig`/`ApplyDefaults`, defaulting to the DB buckets for
compatibility. Consider a native histogram (`NativeHistogramBucketFactor`), which
removes bucket tuning entirely on a Prometheus that supports it.

---

### 13. Low — `PushgatewayInterval` is an untyped seconds `int`

`config.go:34` declares `PushgatewayInterval int` and `prometheus.go:164` reads
it as seconds:

```go
p.pushTicker = time.NewTicker(time.Duration(cfg.PushgatewayInterval) * time.Second)
```

Everywhere else in this project, intervals are duration strings parsed by viper —
`servers.shutdown_timeout: "30s"`, `dbmanager.health_check_interval: "30s"`,
`cache.memcache.timeout: "100ms"` (`pkg/config/manager.go:175`, `:231`, `:202`).

**Failure scenario.** An operator follows the surrounding convention and writes
`pushgateway_interval: "30s"`. viper's `mapstructure` decode into an `int` fails
or yields `0`, and `0` is the documented "automatic pushing is disabled" value
(`config.go:32-33`) — so **pushing is silently off** and, as in finding 7,
nothing logs it. A value of `1` instead pushes every second, hammering the
gateway.

**Recommendation.** Change the field to `time.Duration`; viper decodes duration
strings natively via `StringToTimeDurationHookFunc`. Validate in `ApplyDefaults`
that a configured interval is at least a second, and log the effective value once
at startup.

---

### 14. Low — `Push()` cannot report "no-op"

`prometheus.go:282-287`:

```go
func (p *PrometheusProvider) Push() error {
	if p.pusher == nil {
		return nil // Pushgateway not configured, silently skip
	}
	return p.pusher.Push()
}
```

**Failure scenario.** A short-lived job — a migration, a cron task — calls
`Push()` before exiting, checks the error, sees `nil`, and exits believing its
metrics were delivered. `PushgatewayURL` was empty (a typo, an unset environment
variable), so nothing was sent and nothing will ever scrape a process that has
already exited. The metrics are lost with an explicit success report, which is
worse than an error.

**Recommendation.** Return a sentinel the caller can distinguish:

```go
var ErrPushgatewayNotConfigured = errors.New("metrics: pushgateway not configured")

func (p *PrometheusProvider) Push() error {
	if p.pusher == nil {
		return ErrPushgatewayNotConfigured
	}
	return p.pusher.Push()
}
```

`startAutoPush` never reaches `Push` with a nil pusher — the goroutine starts only
inside the `cfg.PushgatewayURL != ""` branch (`:157-167`) — so the sentinel needs
no special handling there, but treat it as non-failing if that changes.

---

### 15. Low — the pusher gathers from the default gatherer

`prometheus.go:158-159`:

```go
p.pusher = push.New(cfg.PushgatewayURL, cfg.PushgatewayJobName).
	Gatherer(prometheus.DefaultGatherer)
```

**Failure scenario.** Consistent with `promauto` registering into the default
registerer, so it works today — but it silently couples the pusher to global
state. Once findings 3 and 4 move the provider to an owned registry, the pusher
keeps gathering from the default one and pushes **the wrong set of metrics** (Go
and process collectors, plus whatever any other library registered) while
omitting the application metrics it was configured for. Nothing errors; the
gateway receives a plausible payload.

It also means the pushed payload currently includes `go_*` and `process_*` from
every instance, the Pushgateway anti-pattern — those are per-process and the
gateway has no instance label to separate them, so instances overwrite each
other.

**Recommendation.** Gather from the provider's own registry and add an instance
grouping:

```go
p.pusher = push.New(cfg.PushgatewayURL, cfg.PushgatewayJobName).
	Gatherer(p.registry).
	Grouping("instance", instanceID)
```

`pkg/config` already has `event_broker.instance_id`
(`pkg/config/manager.go:256`) that could supply the value.

---

### 16. Low — the no-op handler fingerprints the subsystem

`interfaces.go:90-98`:

```go
func (n *NoOpProvider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, err := w.Write([]byte("Metrics provider not configured"))
		if err != nil {
			logger.Warn("Failed to write. %v", err)
		}
	})
}
```

**Failure scenario.** Minor, but distinguishable: a client probing `/metrics`
gets `404 Metrics provider not configured` rather than the router's generic 404,
confirming the route is registered and metrics merely disabled. That tells an
attacker a configuration change or restart may expose the data in finding 4, and
distinguishes this application from others behind the same proxy. Given finding
1, this is also the response a real Prometheus scrape gets today.

The `logger.Warn` on a failed write is the wrong level: a client disconnecting
mid-response is routine, and per `_CROSS-CUTTING.audit.md` finding X8 every
`Warn` is forwarded to Sentry — so a closed connection generates a third-party
error event.

**Recommendation.** Return a bare `http.NotFound(w, r)` so the response is
indistinguishable from any unregistered path, and drop the log line or make it
`Debug`.

---

### 17. Low — `metrics.Config` is not wired into `pkg/config`

`pkg/config/config.go` has sections for tracing, cache, logger, error tracking,
middleware, CORS, event broker, dbmanager, paths and extensions, and
`setDefaults` (`pkg/config/manager.go:170-293`) sets defaults for all of them.
There is **no `metrics` section and no `metrics.*` default**, and
`Manager.SetConfig` (`:112-134`) likewise sets every section but metrics.
`metrics.Config`'s `mapstructure` tags (`config.go:6`, `:9`, `:12`, …) therefore
have nothing to bind to.

**Failure scenario.** An operator cannot configure metrics through the normal
mechanism — `RESOLVESPEC_METRICS_ENABLED` does nothing, and neither does a
`metrics:` block in `config.yaml`. The struct's tags imply otherwise, so the
natural conclusion is that configuration is broken rather than absent, and the
only way to learn the truth is to read `setDefaults`. Together with findings 1
and 6, metrics are unconfigurable and uninstallable without writing Go.

**Recommendation.** Add `Metrics metrics.Config \`mapstructure:"metrics"\`` to
`config.Config` and defaults to `setDefaults`:

```go
v.SetDefault("metrics.enabled", false)   // see findings 2 and 5 before defaulting to true
v.SetDefault("metrics.provider", "prometheus")
v.SetDefault("metrics.namespace", "resolvespec")
```

Check the import direction first: `pkg/metrics` imports `pkg/logger`
(`interfaces.go:8`), and `pkg/logger` imports `pkg/errortracking`
(`logger.go:12`), so `pkg/config` → `pkg/metrics` → `pkg/logger` →
`pkg/errortracking` must not close back on `pkg/config`
(`errortracking.audit.md` finding 8 flags this coupling). If it does, duplicate
the fields in `pkg/config` rather than importing.

---

### 18. Low — `WithLabelValues` on every observation

Each record method resolves its child from strings on every call, e.g.
`prometheus.go:192-193`:

```go
p.requestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
p.requestTotal.WithLabelValues(method, path, status).Inc()
```

`WithLabelValues` hashes the label values and takes the `MetricVec`'s internal
`RWMutex` to look up or create the child — twice here, twice in `RecordDBQuery`
(`:212-213`) and twice in `RecordEventProcessed` (`:238-239`).

**Failure scenario.** A small constant cost, not a defect, and the normal way to
use the API. It becomes measurable only when the label space is large — which
findings 2 and 5 would guarantee. A map with millions of entries has poor cache
locality, so the two lookups per request grow from tens of nanoseconds to
microseconds, and the `MetricVec` mutex becomes a contention point across all
request-serving goroutines.

**Recommendation.** Fix findings 2 and 5 first; the cardinality bound is the real
remedy. If the DB path proves hot afterwards, pre-curry per-table children at
model-registration time and cache the `prometheus.Observer`/`Counter` handles
rather than resolving by string per query — but only with a bounded, known label
set.

---

## What looks right

- **The global provider is correctly synchronized** (`interfaces.go:50-72`) —
  `sync.RWMutex`, read under `RLock`, written under `Lock`, with the pointer
  copied to a local before the lock is released. This is the only global in
  `pkg/` that gets this right, and `GetProvider` returning `&NoOpProvider{}`
  instead of `nil` means no caller needs a nil check. `pkg/logger`, `pkg/cache`,
  `pkg/config` and `pkg/tracing` should adopt this shape
  (`_CROSS-CUTTING.audit.md` finding X5).
- `NoOpProvider` implements the full interface (`interfaces.go:74-98`) with
  genuine no-ops, so disabling metrics costs one interface dispatch.
- Prometheus client types are goroutine-safe by contract, so the record methods
  need no locking of their own — and correctly have none.
- `IncRequestsInFlight`/`DecRequestsInFlight` are paired with `defer` in the
  middleware (`prometheus.go:263-264`), so the gauge cannot leak on a panic.
- Bucket defaults are sensible and distinct for HTTP versus DB
  (`config.go:43-45`), with the reasoning in comments.
- `ApplyDefaults` (`config.go:50-64`) is idempotent and only fills empty values.
- `RecordDBQuery` derives `status` from `err != nil` (`prometheus.go:208-211`)
  rather than taking a caller-supplied string, so that label is bounded to two
  values by construction — the right instinct, applied to one label out of five.
- In the consumers: `sanitizeMetricQueryShape` (`query_metrics.go:162-234`)
  correctly strips single-quoted literals (including `''` escapes) and `?`/`$n`
  placeholders before query text is used as a label, so **parameter values do not
  reach `/metrics`**. `tableNameProviderType` is cached at package level (`:99`)
  and `tableNameProviderFromModel` checks `Implements` before `reflect.New`
  (`:121-125`) to avoid an allocation — careful, deliberate work.
- The adapter raw-query methods wrap themselves in
  `recover()` + `logger.HandlePanic` (e.g. `bun.go:189-193`, `:209-213`) and
  still call `recordQueryMetrics` on the error path (`:204`, `:222`), so a failed
  query is counted rather than dropped.
- `pkg/common/adapters/database/query_metrics_test.go` has 11 test functions,
  nine of which install a recording provider via `SetProvider` and restore the
  previous one with `defer` — the correct pattern for a global, and the
  best-tested metrics code in the repo.
- `pkg/eventbroker/metrics.go:11`, `:18`, `:25` nil-check `GetProvider()`
  defensively even though it cannot return nil. Harmless belt-and-braces.
- `example_test.go` is correctly a `_test.go` file in package `metrics_test` with
  `// Output:` assertions — unlike `pkg/cache/example_usage.go`, which ships
  `log.Fatal` in the library (`cache.audit.md` finding 25).

## Suggested follow-up

1. **Decide whether metrics are a supported feature.** If yes, install a provider
   at startup from config (finding 1) and add the factory and config section
   (findings 6, 17). If no, delete `PrometheusProvider` and the 39 instrumented
   call sites rather than carrying dead weight that reads as working
   instrumentation. The current middle state is the worst of both.
2. **Fix findings 2, 4 and 5 in the same change as finding 1**, not after. Route
   templates plus a cardinality backstop, an internal-only listener, and a bounded
   `cleanMetricIdentifier`. Activating metrics without these converts three latent
   defects into a remotely triggerable memory leak and an unauthenticated
   information leak on the same day.
3. **Make `NewPrometheusProvider` return an error against an owned registry**
   (finding 3), which also fixes findings 4 and 15 and makes the package testable.
4. **Fix the `SELECT`-as-table-name mislabelling** (finding 9) — have
   `FetchRowNumber` and the other raw-query callers pass the target they already
   know.
5. **Fix the `ResponseWriter` wrapper** (findings 10, 11) before anything depends
   on streaming or a WebSocket upgrade through this middleware. Add `Unwrap()`.
6. **Log Pushgateway failures with backoff and make `StopAutoPush` idempotent**
   (findings 7, 8).
7. **Add `go test -race ./pkg/metrics/...` to CI.** The package's 64 test lines
   are four `Example*` functions with no assertions beyond `// Output:`, and it is
   not in the tested package set (`_CROSS-CUTTING.audit.md` findings X1, X2, X9).
   A test that constructs two providers with the same namespace would catch
   finding 3 immediately; one asserting `GetProvider()` is not a `*NoOpProvider`
   after bootstrap would catch finding 1.

## Cross-references

- `audit/pkg/_CROSS-CUTTING.audit.md` — **X10 (declared-but-never-installed
  subsystems) generalizes finding 1 of this audit**: the same defect shape appears
  in `pkg/middleware` and in the ignored CORS config, and X10 carries the combined
  remediation order. Finding X5 cites this package's
  `globalProviderMu` as the pattern to copy; X1/X2/X9 cover the absent tests; X8
  is why finding 7's log line needs rate limiting and why finding 16's `Warn` is
  the wrong level.
- `audit/pkg/cache.audit.md` — the `Provider` interface here has
  `RecordCacheHit`/`RecordCacheMiss`/`UpdateCacheSize` but **no cache-error
  counter**, and `pkg/cache` calls none of the three (finding 17 there). Those
  three methods have zero callers repo-wide, so cache observability is not merely
  a no-op but entirely unwritten.
- `audit/pkg/middleware.audit.md` — `PanicRecovery`
  (`pkg/middleware/panic.go:14-33`) is the only caller of `RecordPanic`, and its
  ordering relative to this package's `Middleware` decides whether finding 11
  misreports panics as 200s. `panic.go:28` also returns the panic value to the
  client, covered there.
- `audit/pkg/common.audit.md` — `query_metrics.go` produces findings 5 and 9's
  label values; `cleanMetricIdentifier` (`:330`) is the choke point, and
  `tokenizeQuery` (`:319`) the parser at fault.
- `audit/pkg/restheadspec.audit.md` — `handler.go:130-141` is the early return
  that currently bounds finding 5; `FetchRowNumber` (`:3175-3198`) is the
  raw-query path behind findings 5 and 9 (`FetchRowNumber` begins at `:3114`).
- `audit/pkg/config.audit.md` — finding 17 (no `metrics` section);
  `dbmanager.connections.*.enable_metrics` defaults to `false`
  (`manager.go:246`), the second of the two switches keeping finding 5 dormant.
- `audit/pkg/eventbroker.audit.md` — `UpdateEventQueueSize` is an unlabelled
  gauge, so it is cardinality-safe; `source` and `event_type`
  (`prometheus.go:121`, `:128`, `:136`) are bounded only if event types are.
- `audit/pkg/server.audit.md` — where a separate metrics listener would be
  configured, where `SetProvider` should be called, and where middleware order is
  decided.
