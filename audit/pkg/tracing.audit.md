# Audit — `pkg/tracing`

- **Date:** 2026-09-29
- **Scope:** `pkg/tracing/tracing.go` (146 LOC, 1 file, **no tests**)
- **Axes:** thread locking/waiting · slowness · security · panic handling & logging
- **Threat model:** hostile internet client. Span names and attributes here are built directly from
  request-controlled data (method, path, full URL, Host header).

## Summary

A thin OpenTelemetry wrapper: `InitTracer`, an HTTP middleware, and helpers. The abstraction is
fine; the **hardcoded choices** are the problem. Three of them are not configurable at all and each
is wrong for a production, internet-facing deployment:

- `otlptracegrpc.WithInsecure()` — trace export is **plaintext**, with a source comment admitting it.
- `sdktrace.AlwaysSample()` — **100% of requests** are traced, with no sampling knob in config.
- `semconv.HTTPURLKey.String(r.URL.String())` — the **full URL including query string** is exported.

Combined: every request's full URL is shipped unencrypted to a collector, and an attacker sets the
export volume. Span names are also built from raw paths, giving unbounded cardinality.

`config.TracingConfig` (`pkg/config/config.go:86-91`) exposes only `Enabled`, `ServiceName`,
`ServiceVersion` and `Endpoint` — there is no field for TLS or sample rate, so these cannot be fixed
by configuration alone.

| # | Severity | Axis | Finding |
|---|----------|------|---------|
| 1 | **High** | Security | `WithInsecure()` hardcoded — traces exported in plaintext, not configurable |
| 2 | **High** | Security | Full URL **including query string** exported as a span attribute |
| 3 | **High** | Slowness | `AlwaysSample()` hardcoded — 100% trace volume, attacker-controlled, no sampling config |
| 4 | Medium | Slowness | Span name is `method + " " + r.URL.Path` — unbounded cardinality from raw path IDs |
| 5 | Medium | Locking | `tracer` global written by `InitTracer`, read unsynchronised by `Middleware`/`StartSpan` |
| 6 | Medium | Observability | `Middleware` records no HTTP status and no error status — spans never show failures |
| 7 | Medium | Panic | `Middleware` does not recover; a downstream panic leaves the span unmarked (`Unset` status) |
| 8 | Low | Slowness | `InitTracer` has no timeout/deadline on exporter or resource creation |
| 9 | Low | Maintenance | `semconv/v1.4.0` (2021) — deprecated attribute names modern collectors no longer index |
| 10 | Low | Security | `SetAttributes`/`AddEvent` pass caller data through with no size or cardinality limit |

---

## Findings

### 1. `WithInsecure()` hardcoded (High, Security)

`tracing.go:38-42`

```go
client := otlptracegrpc.NewClient(
	otlptracegrpc.WithEndpoint(config.Endpoint),
	otlptracegrpc.WithInsecure(), // Use WithTLSCredentials in production
)
```

The comment names the fix and the code does not implement it, and — critically — `Config`
(`tracing.go:21-27`) has no field to express it:

```go
type Config struct {
	ServiceName    string
	ServiceVersion string
	Endpoint       string
	Enabled        bool
}
```

So there is **no supported way** to enable TLS on trace export short of editing this file. Every
span — carrying the full request URL per finding 2 — crosses the network in cleartext, and the
collector endpoint is unauthenticated (no OTLP headers/bearer token option either), so anything that
can reach it can also *inject* fabricated spans.

**Recommendation:** add `Insecure bool`, `TLSConfig *tls.Config` and `Headers map[string]string` to
`Config` (and the matching `tracing.*` keys to `pkg/config`), default to TLS on, and require an
explicit opt-in for insecure. Wire `otlptracegrpc.WithTLSCredentials` / `WithHeaders`.

### 2. Full URL with query string exported (High, Security)

`tracing.go:95-103`

```go
ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
	trace.WithSpanKind(trace.SpanKindServer),
	trace.WithAttributes(
		semconv.HTTPMethodKey.String(r.Method),
		semconv.HTTPURLKey.String(r.URL.String()),   // <- full URL, query string included
		semconv.HTTPTargetKey.String(r.URL.Path),
		semconv.HTTPSchemeKey.String(r.URL.Scheme),
		semconv.NetHostNameKey.String(r.Host),
	),
)
```

`r.URL.String()` includes `RawQuery`. For this API the query string is where the interesting data
lives: filter expressions, column lists, and — for any client that passes credentials as a query
parameter (`?api_key=`, `?token=`, signed-URL style parameters) — secrets. All of it lands in the
tracing backend, and per finding 1 it gets there in plaintext.

Note `HTTPTargetKey` is also set to `r.URL.Path`, so the *useful* part is already captured
separately; `HTTPURLKey` adds only the sensitive part.

Secondary: `r.Host` comes from the `Host` header, which is client-controlled and unvalidated here —
so an attacker can pollute the `net.host.name` dimension with arbitrary values (cardinality blowup,
and log/dashboard spoofing).

**Recommendation:** export a redacted URL (scheme + host + path, query keys only or dropped
entirely). OTel's own guidance is to strip or redact query parameters for exactly this reason.

### 3. `AlwaysSample()` hardcoded (High, Slowness)

`tracing.go:61-65`

```go
tp := sdktrace.NewTracerProvider(
	sdktrace.WithBatcher(exporter),
	sdktrace.WithResource(res),
	sdktrace.WithSampler(sdktrace.AlwaysSample()),
)
```

Every request produces a recorded, exported span. There is no `SampleRate` in `Config` and no
`tracing.sample_rate` key in `pkg/config/manager.go`'s defaults, so this is not tunable.

Under the hostile-client threat model the request rate — and therefore the span rate, the batch
queue pressure, the serialisation cost and the outbound bandwidth — is set by the attacker. Each
request pays span allocation, attribute encoding (including the full URL string), and a share of
batch export. When the batch queue fills, the SDK drops spans, so a flood also destroys the
observability you need to see the flood.

**Recommendation:** default to `sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))` with a
configurable rate (e.g. 0.01–0.1), keeping `AlwaysSample` available for development. `ParentBased`
also means an upstream sampling decision is respected, which `AlwaysSample` currently overrides.

### 4. Unbounded span-name cardinality (Medium, Slowness)

`tracing.go:95` — the span name is `r.Method + " " + r.URL.Path`.

This API's paths embed identifiers (`/api/public/employees/7f3c…`, `/api/<schema>/<entity>/<id>`), so
each distinct ID becomes a distinct span name. Consequences:

- Tracing backends index on span name; unbounded distinct names is the classic cardinality-explosion
  cost bomb (and in some backends, a hard limit that starts rejecting data).
- It violates the OTel HTTP convention, which requires a **low-cardinality route template**
  (`GET /api/{schema}/{entity}/{id}`), with the concrete value in `http.route`/attributes.
- It is attacker-driven: requests to random paths — including 404s — each mint a new span name.

**Recommendation:** derive the name from the matched route pattern. `pkg/server`'s router
(chi/mux/gin, see `audit/pkg/server.audit.md`) exposes the route template after matching; use it, and
place this middleware after the router so the pattern is available. Fall back to
`r.Method + " " + "<unmatched>"` rather than the raw path.

### 5. Unsynchronised `tracer` global (Medium, Locking)

`tracing.go:19`

```go
var tracer trace.Tracer
```

Written at `tracing.go:77` (`tracer = tp.Tracer(config.ServiceName)`), read at `tracing.go:86`,
`:95` (`Middleware`) and `:116`, `:119` (`StartSpan`). No mutex, no `atomic.Value`.

Same pattern as `pkg/logger`'s `Logger` global (see `audit/pkg/logger.audit.md` finding 1). Benign if
`InitTracer` runs once before any request is served; a race the moment tracing is re-initialised at
runtime. `InitTracer` is exported and callable at any time, and calling it twice also leaks the
first `TracerProvider` (nothing shuts it down) — its batch processor goroutine and gRPC connection
stay alive for the life of the process.

Note the nil checks at `:86` and `:116` are the read-half of the race: a goroutine can observe a
non-nil-but-torn interface value.

**Recommendation:** `atomic.Pointer` or a `sync.Once`-guarded init; return an error from a second
`InitTracer` call, or shut down the previous provider first.

### 6. No HTTP status or error status on spans (Medium, Observability)

`Middleware` (`tracing.go:84-112`) never wraps `w`, so it cannot observe the status code. It sets no
`semconv.HTTPStatusCodeKey` and never calls `span.SetStatus`. Every span therefore has status
`Unset`, which tracing backends render as "OK".

The practical effect: you cannot find failing requests in the traces. A 500-storm and a healthy
period look identical in the span data, which defeats the main reason to run tracing on an
internet-facing service.

**Recommendation:** wrap the `ResponseWriter` to capture the status, set
`semconv.HTTPStatusCodeKey.Int(status)`, and `span.SetStatus(codes.Error, ...)` for 5xx.

### 7. No panic handling in the middleware (Medium, Panic handling)

`Middleware` has `defer span.End()` (`tracing.go:106`) but no `recover()`. If `next.ServeHTTP`
panics:

- The `defer span.End()` **does** run, so no span is leaked — that part is correct.
- But the span is ended with status `Unset` and no exception event, so the panic is invisible in the
  trace. The one place a trace would be most valuable records nothing.
- The panic propagates up to whichever handler is outermost. Whether that is
  `pkg/middleware/panic.go` depends on middleware ordering — if `tracing.Middleware` is installed
  *outside* the panic middleware, the panic escapes to `net/http`'s per-connection recovery, which
  kills the connection and logs to the default logger, bypassing `pkg/logger` and the error tracker
  entirely. See `audit/pkg/middleware.audit.md` and `audit/pkg/server.audit.md` for the actual order.

This package does not import `pkg/logger` at all, so nothing here can be logged.

**Recommendation:** recover, record `span.RecordError` + `span.SetStatus(codes.Error, …)`, then
re-panic so the dedicated panic middleware still handles the response. Document the required
middleware order.

### 8. No deadline on initialisation (Low, Slowness)

`tracing.go:36` uses `ctx := context.Background()` for both `otlptrace.New` (`:44`) and
`resource.New` (`:51`). `otlptracegrpc` does not block on connect by default, so this is unlikely to
hang today — but `resource.New` with detectors can perform network calls (cloud metadata endpoints),
and an unreachable metadata service is a classic multi-second startup stall. `InitTracer` should
accept a `context.Context` from the caller so startup has a deadline.

### 9. `semconv/v1.4.0` (Low, Maintenance)

`tracing.go:15` pins the 2021 semantic conventions. `http.method`, `http.url`, `http.target`,
`http.scheme`, `net.host.name` were all renamed in v1.20+ (`http.request.method`, `url.full`,
`url.path`, `url.scheme`, `server.address`). Current collectors, dashboards and backend
auto-instrumentation views key off the new names, so these spans will not populate standard HTTP
dashboards.

### 10. No limits on caller-supplied span data (Low, Security)

`StartSpan`, `AddEvent`, `SetAttributes` (`tracing.go:115-145`) forward caller attributes verbatim.
If any caller passes request-derived values (a filter expression, a row payload), span size is
attacker-influenced. The SDK's default limits (128 attributes, 128 events) cap the count but not the
*value* length — a 1 MB string attribute is accepted.

**Recommendation:** set explicit `sdktrace.WithSpanLimits` including `AttributeValueLengthLimit`.

---

## What looks right

- **Disabled path is genuinely free.** `InitTracer` with `Enabled: false` (`tracing.go:31-34`)
  returns a no-op shutdown func and never builds an exporter, so a disabled deployment pays nothing
  and cannot leak.
- **Nil-tracer guards everywhere.** `Middleware` (`tracing.go:86-89`) passes through untouched and
  `StartSpan` (`tracing.go:116-118`) returns the incoming context plus the context's (no-op) span.
  So a partially-initialised process degrades safely rather than nil-panicking — a pattern
  `pkg/logger` gets right too.
- **Context propagation is correct.** `Extract` from `propagation.HeaderCarrier(r.Header)`
  (`tracing.go:92`), a composite `TraceContext` + `Baggage` propagator (`tracing.go:72-75`), and
  `r = r.WithContext(ctx)` (`tracing.go:109`) before calling `next` — the span context actually
  reaches downstream handlers, which is the part most hand-rolled middlewares get wrong.
- `SpanKindServer` is set correctly (`tracing.go:96`).
- `WithBatcher` rather than a simple/sync span processor (`tracing.go:62`) — export does not block
  the request path.
- `InitTracer` returns `tp.Shutdown` (`tracing.go:80`), giving the caller a real flush-on-shutdown
  hook with a caller-supplied context, which is better than the fixed-timeout pattern in
  `pkg/errortracking` (see that audit, finding 3).
- `RecordError` nil-guards (`tracing.go:140-143`) so `RecordError(ctx, nil)` is a no-op.

## Suggested follow-up

1. Extend `Config` with `Insecure`, TLS credentials, OTLP headers and `SampleRate`; add the matching
   keys to `pkg/config` (findings 1, 3). These cannot be fixed without an API change, so they should
   go together.
2. Redact the query string from exported attributes (finding 2).
3. Move to route-template span names, which requires positioning the middleware after routing
   (finding 4).
4. Capture status code and panics in the middleware (findings 6, 7).
5. Guard the `tracer` global (finding 5) and upgrade `semconv` (finding 9).
6. Add tests: this package has none. A tracetest/in-memory exporter makes assertions on span name,
   attributes and status straightforward, and would have caught findings 2, 4 and 6.
