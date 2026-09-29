# Audit: `pkg/middleware`

| | |
|---|---|
| **Package** | `github.com/bitechdev/ResolveSpec/pkg/middleware` |
| **Files** | `panic.go` (33), `ratelimit.go` (233), `blacklist.go` (212), `sanitize.go` (251), `sizelimit.go` (70), `README.md` (18 KB) |
| **Tests** | `panic_test.go` (86), `ratelimit_test.go` (388), `blacklist_test.go` (254), `sanitize_test.go` (273), `sizelimit_test.go` (126) — 1 127 lines |
| **Audit date** | 2026-09-29 |
| **Axes** | thread locking/waiting, slowness, security, panic handling & logging |
| **Threat model** | hostile internet client; request bodies, headers, query params, schema/table/column names all attacker-controlled |
| **Depth** | deep |

## Summary

This package is the project's perimeter: rate limiting, IP blacklisting, request
size limiting, input sanitization and panic recovery. It is also the package where
the threat model bites hardest, and it has two structural problems.

**First, almost none of it is mounted.** `NewRateLimiter`, `NewIPBlacklist`,
`NewRequestSizeLimiter`, `DefaultSanitizer` and `StrictSanitizer` have **zero
non-test callers** anywhere in the repository. Only `pkg/server/manager.go`
imports the package, and only to apply `PanicRecovery` (`manager.go:466`). The
`MiddlewareConfig` fields that would configure the rest
(`pkg/config/config.go:123-125`) are declared, defaulted
(`pkg/config/manager.go:209-211`) and **read by nothing**. So against the stated
threat model there is currently **no rate limiting, no request-size limit, no IP
blocking and no input sanitization in the serving path** — finding 1.

**Second, the protections themselves would not hold if mounted.** Both
IP-based controls derive the client address from `getClientIP`
(`ratelimit.go:210-233`), which trusts `X-Forwarded-For` unconditionally and with
no trusted-proxy configuration. One attacker-chosen header therefore bypasses the
rate limiter entirely *and* makes its limiter map grow without bound (finding 2),
and the same header defeats the IP blacklist outright when `UseProxy` is set
(finding 3). The sanitizer is a denylist that I verified is bypassable four
different ways — and in one case **manufactures a `javascript:` URI from input
that contained none** — while simultaneously corrupting ordinary filter values
like `price>100` and any JSON parameter (findings 6 and 7).

The one live middleware, `PanicRecovery`, writes the panic value into the HTTP 500
body (`panic.go:28`), and `panic_test.go:57` asserts that it does — the leak is
test-locked as the intended contract (finding 4).

On the positive side the locking is genuinely careful: `getLimiter`
(`ratelimit.go:42-62`) implements correct double-checked locking, the blacklist
guards every field with an `RWMutex`, and the package carries 1 127 lines of
tests — more than the 799 lines of code. There are **no data races** in the
package. The defects are in trust boundaries and in what is wired up, not in
concurrency.

## Findings

| # | Severity | Axis | Finding |
|---|---|---|---|
| 1 | **High** | security | No protective middleware is mounted: rate limit, size limit, blacklist and sanitizer all have zero non-test callers, and `MiddlewareConfig` is read by nothing |
| 2 | **High** | security / slowness | `getClientIP` trusts `X-Forwarded-For` unconditionally — the rate limiter is bypassable per-request and its limiter map grows without bound |
| 3 | **High** | security | With `UseProxy: true` the IP blacklist is defeated by one client-supplied header; with it false, behind a proxy, it blocks everyone or no one |
| 4 | **High** | security / panic handling | `PanicRecovery` writes the panic value into the 500 body, and a test asserts it |
| 5 | **High** | security | `Sanitize` pattern-stripping **creates** a `javascript:` URI from input that had none (verified) |
| 6 | **High** | correctness | `EscapeHTML` on query params corrupts every value containing `< > & " '` — `price>100`, `A&B Corp` and all JSON params (verified) |
| 7 | **Medium** | security | Three further verified denylist bypasses: newline in `<script>`, unclosed `<script src=…>`, `</SCRIPT >` |
| 8 | **Medium** | correctness | `cleanupRoutine` flushes every limiter every 5 minutes, handing each client a fresh full burst |
| 9 | **Medium** | security | `MiddlewareWithKeyFunc` falls back to `r.RemoteAddr` **with port**, giving each TCP connection its own bucket |
| 10 | **Medium** | security | Both `StatsHandler`s expose client IPs, remaining budgets and the whole blacklist with no authentication |
| 11 | **Medium** | logging | No log line or metric is emitted when a request is rate-limited, blocked or sanitized — the perimeter has no audit trail |
| 12 | **Medium** | correctness | `UnblockCIDR` silently fails to unblock a non-canonical CIDR while deleting its reason |
| 13 | **Medium** | correctness | `IsBlocked` fails open on an unparseable IP, and stores/compares IPs as raw strings, so IPv6 forms evade blocking |
| 14 | **Medium** | correctness | Byte-slicing `MaxStringLength` and `SanitizeFilename` produces invalid UTF-8 (verified) |
| 15 | **Medium** | correctness | Sanitizing one query param rewrites `RawQuery` via `q.Encode()`, dropping malformed params and reordering the rest |
| 16 | **Medium** | slowness / correctness | `cleanupRoutine` goroutine has no stop, no `recover()`, and leaks per `RateLimiter` |
| 17 | **Low** | correctness | `removeControlCharacters` keeps DEL and the C1 range despite its contract (verified) |
| 18 | **Low** | correctness | 429 responses send JSON with `Content-Type: text/plain` and no `Retry-After` |
| 19 | **Low** | slowness | `IsBlocked` is O(n) in CIDRs per request, with a redundant O(m) reason scan nested inside |
| 20 | **Low** | correctness | `GetAllRateLimitInfo` takes n+1 lock acquisitions |
| 21 | **Low** | security | `SanitizeURL` blocks only two schemes by prefix; `SanitizeFilename` misses encoded traversal and Windows drive prefixes |
| 22 | **Low** | correctness | `getClientIP` mishandles a port-less IPv6 `RemoteAddr` and returns bracketed forms inconsistently |
| 23 | **Low** | maintainability | `//nolint:all` at `ratelimit.go:4` blanket-suppresses linting |

---

### 1. High — the perimeter is not connected

Searching the module for every constructor in this package:

| Constructor | Non-test callers |
|---|---|
| `NewRateLimiter` | **0** |
| `NewIPBlacklist` | **0** |
| `NewRequestSizeLimiter` | **0** |
| `DefaultSanitizer` | **0** outside the package (one internal, `sanitize.go:57`) |
| `StrictSanitizer` | **0** |
| `PanicRecovery` | 1 (`pkg/server/manager.go:466`) |

Only one file outside the package imports it at all — `pkg/server/manager.go` —
and only for panic recovery (`:466`). The configuration that exists to drive the
rest is inert:

```go
// pkg/config/config.go:121-126
// MiddlewareConfig holds middleware configuration
type MiddlewareConfig struct {
	RateLimitRPS   float64 `mapstructure:"rate_limit_rps"`
	RateLimitBurst int     `mapstructure:"rate_limit_burst"`
	MaxRequestSize int64   `mapstructure:"max_request_size"`
}
```

All three have defaults (`pkg/config/manager.go:209-211`) and **no reader
anywhere** — grep for `RateLimitRPS`, `RateLimitBurst` and `MaxRequestSize`
outside tests returns only these declarations and `pkg/middleware`'s own
unrelated `DefaultMaxRequestSize` constant.

**Failure scenario.** Under the stated threat model — a hostile internet client —
the service as assembled has:

- **No rate limit.** A single client can issue unlimited requests. Every one is a
  database round trip through `pkg/restheadspec`, so a trivial loop exhausts the
  connection pool (`dbmanager.max_open_conns` defaults to 25,
  `pkg/config/manager.go:224`) and the service stops answering for everyone.
  There is no other limiter in the stack.
- **No request-size limit.** `max_request_size: 10485760` is configured and
  unenforced, so `r.Body` is unbounded. A single `POST` with a multi-gigabyte body
  is read into memory by the JSON decoder and OOM-kills the process. This is the
  cheapest possible denial of service and the configuration says it is prevented.
- **No IP blacklist**, so an operator has no way to shed a known-bad source.
- **No input sanitization** — less serious, given findings 5–7 argue the
  sanitizer should not be mounted in its current form.

The gap is invisible from the config file, which advertises all three knobs, and
invisible from `pkg/middleware/README.md` (18 KB of documentation for middleware
that is never installed). An operator tuning `rate_limit_rps` downward during an
incident would see no change and reasonably conclude the attack was overwhelming
the limit rather than that no limit exists.

**Recommendation.** Wire the chain in `pkg/server` from `MiddlewareConfig`, in an
order that matters (outermost first):

```go
// outermost → innermost
handler = sizeLimiter.Middleware(handler)   // cheapest rejection first
handler = rateLimiter.Middleware(handler)
handler = blacklist.Middleware(handler)
handler = middleware.PanicRecovery(handler) // innermost: must see handler panics
```

Rationale for the order: the size limit and blacklist are O(1)-ish and should
reject before any expensive work; `PanicRecovery` must be **innermost** of these
so that a panic in the handler is converted to a 500 before the outer layers
observe the response — and note `trackRequestsMiddleware` is applied later still
(`manager.go:540`), so it correctly ends up outside `PanicRecovery`. Fix findings
2 and 3 *before* mounting the IP-based layers, and findings 5–7 before mounting
the sanitizer; mounting them as they stand adds attack surface rather than
removing it. Add a startup log line naming which middleware is active, so the
inert state cannot recur silently.

---

### 2. High — `getClientIP` trusts `X-Forwarded-For` unconditionally

`ratelimit.go:210-233`:

```go
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (most common in production)
	// Format: X-Forwarded-For: client, proxy1, proxy2
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (the original client)
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header (used by some proxies like nginx)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	...
}
```

There is no trusted-proxy list, no hop counting, and no validation that the
returned string is even an IP address. The value is whatever the client sent, and
it becomes the rate-limiter key at `ratelimit.go:83`.

**Failure scenario — bypass.** The attacker sends a distinct
`X-Forwarded-For` on every request:

```
GET /api/public/orders HTTP/1.1
X-Forwarded-For: 1.2.3.4
...
X-Forwarded-For: 1.2.3.5
```

Each value is a new key, so `getLimiter` mints a **fresh** `rate.Limiter` with a
full burst allowance for every request. `limiter.Allow()` on a brand-new limiter
always succeeds, so **the rate limiter never denies anything**. Its entire purpose
is defeated by a header an attacker types once. Note the first-value choice makes
this worse than the usual mistake: when a real proxy *is* in front, the
left-most XFF entry is precisely the one field the client controls end to end, so
the header cannot be trusted even in the deployment it was written for.

**Failure scenario — amplification.** The same requests grow
`rl.limiters` without bound (`ratelimit.go:59-60`):

```go
limiter = rate.NewLimiter(rl.rate, rl.burst)
rl.limiters[key] = limiter
```

Each entry is a `*rate.Limiter` (~64 bytes) plus the attacker-supplied key string
plus map overhead — call it 150–200 bytes, and the key length is attacker-chosen
up to the header size limit, so it can be far larger. There is no cap on entries
and no per-key validation. A few million requests — minutes of traffic — is
hundreds of megabytes of live heap, and the key is unvalidated so it need not
resemble an IP at all. **The component intended to prevent resource exhaustion
becomes the most efficient way to cause it**, because a single cheap request with
no authentication allocates permanent server memory. Finding 8's five-minute flush
bounds the growth to one interval's worth, which is the only thing standing
between this and a certain OOM.

**Recommendation.** Only consult proxy headers when the immediate peer is a
trusted proxy, and validate the result:

```go
type ClientIPConfig struct {
	TrustedProxies []*net.IPNet // empty ⇒ never trust XFF/X-Real-IP
}

func (c *ClientIPConfig) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return host
	}
	if !c.isTrusted(peer) {
		return peer.String()          // normalized; ignore all proxy headers
	}
	// Walk XFF right-to-left, returning the first address that is NOT a
	// trusted proxy — that is the furthest hop we can actually vouch for.
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip != nil && !c.isTrusted(ip) {
			return ip.String()
		}
	}
	return peer.String()
}
```

Three properties matter and all three are missing today: **trust is opt-in**
(default deny), the scan is **right-to-left** so the client cannot inject hops,
and the result is **normalized through `net.ParseIP`** so it is a canonical
address and nothing else. Independently, cap `rl.limiters` (evict LRU past N) so
a key-space bug can never again be a memory leak, and add the same
trusted-proxy plumbing to `blacklist.go` (finding 3). Surface `TrustedProxies` in
`MiddlewareConfig`.

---

### 3. High — the IP blacklist is defeated by a client header

`blacklist.go:154-169`:

```go
func (bl *IPBlacklist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var clientIP string
		if bl.useProxy {
			clientIP = getClientIP(r)
			// Clean up IPv6 brackets if present
			clientIP = strings.Trim(clientIP, "[]")
		} else {
			// Extract IP from RemoteAddr
			if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
				clientIP = r.RemoteAddr[:idx]
			} else {
				clientIP = r.RemoteAddr
			}
			clientIP = strings.Trim(clientIP, "[]")
		}
		...
```

**Failure scenario.** `UseProxy` is a single boolean
(`blacklist.go:23-26`) and both settings are wrong without a trusted-proxy list:

- **`UseProxy: true`** — the address comes from `getClientIP`, so a blocked
  attacker sends `X-Forwarded-For: 203.0.113.9` and is no longer blocked. The
  blacklist is a **security control that any client can switch off by naming a
  header**, and because `IsBlocked` also fails open on an unparseable address
  (finding 13), even `X-Forwarded-For: x` suffices. Nothing is logged when a block
  is evaded, or at all (finding 11), so an operator watching the blacklist "work"
  sees blocked counts of zero and no indication why.
- **`UseProxy: false` behind a proxy** — every request carries the proxy's
  address, so the blacklist either blocks nothing (the proxy is not listed) or
  blocks **all traffic at once** the moment the proxy's IP is added. Blocking one
  abusive client is impossible.

Since a deployment behind a load balancer or CDN is the normal case for an
internet-facing API, the correct configuration does not exist.

**Recommendation.** Replace `UseProxy bool` with the `TrustedProxies` plumbing
from finding 2 and share one `ClientIP` implementation between both middlewares —
the two files currently disagree about bracket handling and port stripping, which
is itself a source of mismatch between "the IP we rate-limit" and "the IP we
block". Normalize through `net.ParseIP(...).String()` before both storing and
comparing (finding 13), and log every block at `Info` with the matched rule
(finding 11).

---

### 4. High — the panic value is returned to the client, and a test locks it in

`panic.go:14-33` is the only middleware actually mounted:

```go
func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rcv := recover(); rcv != nil {
				// Record the panic metric
				metrics.GetProvider().RecordPanic(panicMiddlewareMethodName)
				...
				ctx := r.Context()
				err := logger.HandlePanic(panicMiddlewareMethodName, rcv, ctx)

				// Respond with a 500 error
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

`logger.HandlePanic` returns `fmt.Errorf("panic in %s: %v", methodName, r)`
(`pkg/logger/logger.go:210`), so the **raw panic value is written into the
response body**.

**Failure scenario.** An attacker probes endpoints until something panics, then
reads the internals out of the 500 body:

- `panic in PanicMiddleware: runtime error: index out of range [5] with length 3`
  — confirms an exploitable parsing path and gives exact bounds.
- A panicking database driver leaks the failing SQL, and `pq`/`pgx` errors can
  carry the connection string — including credentials, since
  `dbmanager.connections.default.password` is part of the DSN.
- `runtime error: invalid memory address or nil pointer dereference` maps
  attacker input to specific unguarded code paths, turning blind probing into a
  guided search.

Because panics are reachable from request parsing, this is a repeatable oracle
rather than a one-off. `pkg/restheadspec` and the adapters recover in several
places, so the panics that reach here are the unanticipated ones — exactly those
whose messages are most revealing.

The leak is **deliberate and test-locked** — `panic_test.go:57`:

```go
assert.Contains(t, rr.Body.String(), "panic in PanicMiddleware: something went terribly wrong", "expected error message in response body")
```

so any fix must change this assertion. That matters: a reviewer changing
`panic.go` would see a test failure and assume they had broken something.

**Mitigation that already exists.** `pkg/server` lets a caller supply
`PanicHandler` (`pkg/server/interfaces.go:47`), used in preference to this
middleware (`manager.go:456-466`). An embedder can therefore avoid the leak — but
it is opt-out, not opt-in, so the insecure path is the default.

**Recommendation.** Log in full, respond with nothing:

```go
if rcv := recover(); rcv != nil {
	metrics.GetProvider().RecordPanic(panicMiddlewareMethodName)
	_ = logger.HandlePanic(panicMiddlewareMethodName, rcv, r.Context())
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}
```

Update `panic_test.go:57` to assert the body does **not** contain the panic text,
which converts the test from locking in the bug to guarding the fix. Consider
emitting a correlation ID in both the log line and the response body so support
can still tie a report to a stack trace without disclosing it.

Two further defects in the same nine lines:

- **`http.Error` after a partial response.** If the handler wrote a 200 and
  streamed bytes before panicking, `WriteHeader(500)` is ignored ("superfluous
  WriteHeader" is logged by `net/http`) and the error text is appended **into the
  middle of the response body**. The client receives a 200 with corrupt trailing
  content — worse than a clean failure, because caches and clients treat it as
  valid. Track whether anything was written (see `metrics.audit.md` finding 11)
  and, when it was, abandon the connection with `panic(http.ErrAbortHandler)`
  instead.
- **`RecordPanic` goes nowhere.** `metrics.GetProvider()` returns a
  `NoOpProvider` because nothing ever installs a provider
  (`metrics.audit.md` finding 1), so `panics_total` never increments. This is the
  *only* caller of `RecordPanic` in the repository, so panic counting is entirely
  non-functional.

---

### 5. High — the sanitizer manufactures a `javascript:` URI

`sanitize.go:80-85` applies each block pattern exactly once:

```go
	// Check block patterns
	for _, pattern := range s.BlockPatterns {
		if pattern.MatchString(value) {
			// Replace matched pattern with empty string
			value = pattern.ReplaceAllString(value, "")
		}
	}
```

Removing a substring can **join its neighbours into a new match**, and because
each pattern runs once there is no second pass to catch the result. I verified
this against a faithful reimplementation of `DefaultSanitizer`
(`sanitize.go:35-53`):

| Input | Output |
|---|---|
| `javajavascript:script:alert(1)` | **`javascript:alert(1)`** |

Removing the inner `javascript:` leaves `java` + `script:alert(1)` — a working
`javascript:` URI. **The sanitizer produced the exact payload it exists to
block, from input that did not contain it.**

**Failure scenario.** An attacker submits
`javajavascript:script:alert(1)` as a profile URL, comment or any stored string.
Every layer that inspects the input sees a harmless string with no `javascript:`
substring — a WAF, a manual review, a downstream denylist. The sanitizer then
converts it into a live XSS payload, which is stored and later rendered into an
`href`. The control inverts: input that would have been rejected as suspicious is
laundered into something dangerous, and the transformation happens inside the
component whose job is to prevent it. Note the HTML-escaping step
(`sanitize.go:93-94`) does not help here, because the resulting string contains no
HTML metacharacters to escape.

**Recommendation.** Do not sanitize by deletion. Two changes, in order of
importance:

1. **Reject rather than repair.** If a pattern matches, fail the request with
   400 — never edit the value and continue. Editing guarantees this class of bug
   and silently changes user data (finding 11 means nobody finds out).
2. **Stop relying on a denylist for XSS.** Escape at the point of *output*,
   per context (HTML body, attribute, JS, URL), which is the only place the
   correct encoding is known. For rich text use a real allowlist sanitizer with a
   parser — `bluemonday` — instead of regexes over a string. Regex-based HTML
   filtering cannot be made correct; findings 7's bypasses are symptoms of that,
   not fixable individually.

If deletion must be kept as a stopgap, loop each pattern to a fixed point
(`for pattern.MatchString(v) { v = pattern.ReplaceAllString(v, "") }`) with an
iteration cap — but note this makes the function O(n·k) on attacker-controlled
input and still does not make the denylist complete.

---

### 6. High — HTML-escaping query parameters corrupts ordinary data

`DefaultSanitizer` sets `EscapeHTML: true` (`sanitize.go:38`), applied at
`sanitize.go:93-94`:

```go
	// Escape HTML entities
	if s.EscapeHTML && !s.StripHTML {
		value = html.EscapeString(value)
	}
```

and `Middleware` runs `Sanitize` over **every query parameter value**
(`sanitize.go:145-159`) and four headers (`:163-177`), writing the results back
into the request. Verified outputs:

| Input | Output |
|---|---|
| `price>100` | `price&gt;100` |
| `A&B Corp` | `A&amp;B Corp` |
| `{"filter":"name = 'O'Brien'"}` | `{&#34;filter&#34;:&#34;name = &#39;O&#39;Brien&#39;&#34;}` |

**Failure scenario.** This project's API is query- and header-driven: filters,
sort specifications and expand options arrive as parameters, and
`pkg/restheadspec` carries structured options in headers. With this middleware
mounted:

- A filter `price>100` reaches the handler as `price&gt;100` and either fails to
  parse or is treated as a literal — the query silently returns the wrong rows.
- A customer named `A&B Corp` is **written to the database** as `A&amp;B Corp`.
  The corruption is persistent, silent, and compounds on each edit
  (`&amp;amp;`…). There is no log line (finding 11), so the first report is a user
  asking why their company name looks wrong.
- Any JSON-valued parameter has its quotes turned into `&#34;` and becomes
  **unparseable**, so requests fail with a decoding error that names the JSON, not
  the middleware.

This is output encoding applied at input time — the canonical mistake. It does
not prevent XSS (the escape is applied again, or undone, at render time,
producing double-escaping or none) and it destroys data integrity for every
non-HTML consumer, which here is all of them.

**Recommendation.** Set `EscapeHTML: false` in `DefaultSanitizer` and escape in
the template or serializer that renders the value, where the output context is
known. If a stored-XSS defense is wanted at ingress, validate and reject
(finding 5) rather than transform. Note `StrictSanitizer` (`sanitize.go:56-61`)
is worse still: it sets `StripHTML: true`, so `<` in a legitimate value silently
deletes everything up to the next `>`.

---

### 7. Medium — three further verified denylist bypasses

Also verified against `DefaultSanitizer`'s patterns
(`sanitize.go:44-51`) — all three reach the handler with the script intact
(HTML-escaped only, which finding 6 shows is not a defense and is undone
elsewhere):

| Input | Result | Cause |
|---|---|---|
| `<script>\nalert(1)\n</script>` | **not matched** | `.` does not match `\n` without the `(?s)` flag, so `<script[^>]*>.*?</script>` cannot span lines |
| `<script src=//evil.com/x.js>` | **not matched** | the pattern requires a closing `</script>`; an external-source tag has none |
| `<script>alert(1)</SCRIPT >` | **not matched** | `</script>` is matched literally, so a space before `>` evades it |

The newline case is the most serious: a multi-line `<script>` block is the
ordinary way to write one, so the package's primary XSS pattern fails on the
common form rather than an exotic one. `<scr<script>ipt>alert(1)</script>`
produces `&lt;scr`, which is safe here but demonstrates the same
removal-creates-new-text mechanism as finding 5.

**Failure scenario.** Any of the three is stored and rendered, giving stored XSS
in an application whose operators believe input is sanitized — the false
assurance is the harm, because it displaces real output encoding.

**Recommendation.** As finding 5: reject instead of strip, and replace the regex
set with an allowlist parser. If the patterns are kept for defense in depth, add
`(?s)` and make the closing-tag matching tolerant (`</\s*script\s*>`), while
treating the list as best-effort and never as the control.

---

### 8. Medium — the cleanup routine hands every client a fresh burst

`ratelimit.go:65-76`:

```go
func (rl *RateLimiter) cleanupRoutine() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		// Simple cleanup: remove all limiters
		// In production, you might want to track last access time
		rl.limiters = make(map[string]*rate.Limiter)
		rl.mu.Unlock()
	}
}
```

The interval is hard-coded to 5 minutes (`:32`) with no way to configure it.

**Failure scenario.** This does not evict *stale* limiters — it discards **all**
of them, including those belonging to clients currently being throttled. A client
that has exhausted its bucket gets a brand-new limiter with a full `burst`
allowance (default 200, `pkg/config/manager.go:210`) at the next tick. An attacker
who simply waits out the interval receives 200 free requests every 5 minutes on
top of the sustained rate, indefinitely, and the penalty for abuse resets on a
predictable schedule. Because the flush is unconditional, **the harder a client is
being throttled, the more it gains** from each tick.

The comment acknowledges the design is provisional; the consequence is that the
limiter's long-run behaviour is not the configured rate.

**Recommendation.** Evict by last use, not wholesale:

```go
type entry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // unix nanos, updated in getLimiter
}

for range ticker.C {
	cutoff := time.Now().Add(-idleTTL).UnixNano()
	rl.mu.Lock()
	for k, e := range rl.limiters {
		if e.lastSeen.Load() < cutoff {
			delete(rl.limiters, k)
		}
	}
	rl.mu.Unlock()
}
```

An idle limiter at full tokens is indistinguishable from no limiter, so evicting
only idle entries is both safe and sufficient. Deleting in place also avoids
handing the whole old map to the GC at once. Make the interval and TTL
configurable, and pair this with the hard entry cap from finding 2 — eviction by
age alone does not bound a burst of new keys within one interval.

---

### 9. Medium — `MiddlewareWithKeyFunc` falls back to a per-connection key

`ratelimit.go:97-115`:

```go
			key := keyFunc(r)
			if key == "" {
				key = r.RemoteAddr
			}
```

`r.RemoteAddr` is `"ip:port"` — **including the ephemeral source port** — unlike
`getClientIP`, which strips it (`ratelimit.go:228-230`).

**Failure scenario.** Whenever the supplied `keyFunc` returns `""` — for an
unauthenticated request if it keys on user ID, for a request missing the chosen
header, or for every request if the function has a bug — the key becomes unique
**per TCP connection**. A client that opens a new connection per request (trivial:
`Connection: close`, or any non-pooling HTTP client) therefore gets a fresh
limiter every time and is never limited. The failure is silent and
input-dependent: the limiter appears to work for authenticated traffic and
disappears for exactly the anonymous traffic that most needs limiting.

**Recommendation.** Use the same normalized client IP as the default path, and
make the fallback explicit:

```go
key := keyFunc(r)
if key == "" {
	key = "ip:" + clientIP(r)   // shared, port-stripped, trusted-proxy aware
}
```

Prefixing by key type (`ip:`, `user:`) also prevents a user ID from colliding with
an IP string in the shared map — worth doing regardless.

---

### 10. Medium — both stats handlers are unauthenticated

`ratelimit.go:175-206` and `blacklist.go:195-212` return JSON with no
authentication, no authorization and no way to require any:

```go
// ratelimit.go:191-198
		stats := map[string]interface{}{
			"total_tracked_ips": len(allInfo),
			"rate_limit_config": map[string]interface{}{
				"requests_per_second": float64(rl.rate),
				"burst":               rl.burst,
			},
			"tracked_ips": allInfo,
		}
```

**Failure scenario.** If either is routed — and the doc comment invites it,
"Example: GET /rate-limit-stats" (`ratelimit.go:174`) — any client learns:

- **Every client IP currently using the service**, with remaining token counts
  (`RateLimitInfo`, `:118-123`). That is personal data about other users
  disclosed to an anonymous third party, and in most jurisdictions an IP address
  tied to activity is regulated.
- **The exact limiter configuration** — `requests_per_second` and `burst` — plus,
  via `?ip=` (`:178`), the attacker's **own remaining budget in real time**. That
  converts rate-limit evasion from guesswork into a control loop: poll the
  endpoint, stay one token below the threshold, never get a 429.
- From the blacklist handler, **the complete set of blocked IPs and CIDRs** with
  their `reason` strings (`blacklist.go:199-204`). An attacker learns which
  ranges to avoid and which proxies remain usable, and the reasons are free-text
  operator notes that may name incidents, customers or internal tooling.

**Recommendation.** Do not ship unauthenticated introspection of a security
control. Require authentication and authorization at the route, bind these to an
internal-only listener (`servers.instances.*`, `pkg/config/manager.go:182-186`) as
recommended for `/metrics` in `metrics.audit.md` finding 4, and drop `tracked_ips`
from the default payload — the aggregate count is enough for a dashboard. If
per-IP detail is needed, gate it behind an explicit admin scope and log each
access.

---

### 11. Medium — the perimeter emits no audit trail

Neither rate limiting nor blacklisting nor sanitization logs anything on the path
that matters. `ratelimit.go:87-90`:

```go
		if !limiter.Allow() {
			http.Error(w, `{"error":"rate_limit_exceeded","message":"Too many requests"}`, http.StatusTooManyRequests)
			return
		}
```

`blacklist.go:171-188` likewise writes a 403 with no log line — the only
`logger` call in either file is a `Debug` on a **JSON encoding failure**
(`blacklist.go:185`, `ratelimit.go:183`, `:203`). `sanitize.go` imports no logger
at all and modifies request data silently. No middleware in the package records a
metric; `panic.go:19` is the package's only `metrics` call, and it reaches a no-op
(finding 4).

**Failure scenario.** During an attack an operator cannot answer the first
questions asked: is the rate limiter firing, for which clients, and at what rate?
Nothing distinguishes "no attack" from "limiter bypassed via finding 2" from
"limiter not mounted at all" (finding 1) — all three produce identical silence.
There is no signal to alert on, no data to tune `rate_limit_rps` with, and no
forensic record of which addresses were blocked or why. For the blacklist, an
operator adding an entry gets no confirmation that it ever matched. And because
the sanitizer edits values without logging, the data corruption in finding 6 is
undiagnosable from the server side — the request that arrived and the value that
was stored differ, with nothing recording the difference.

**Recommendation.** Log every enforcement action at `Info` with the client IP, the
matched rule and the request path, and add counters:

```go
if !limiter.Allow() {
	logger.Info("rate limit exceeded: client=%s path=%s rps=%v burst=%d", key, r.URL.Path, rl.rate, rl.burst)
	metrics.GetProvider().RecordRateLimited(key)   // new interface method
	...
}
```

Log at `Info`, not `Warn`: per `_CROSS-CUTTING.audit.md` finding X8 every `Warn`
is forwarded to the error tracker, so logging a rate-limit event at `Warn` would
turn a volumetric attack into an equal-volume flood of Sentry events — a second
outage caused by the instrumentation. The `metrics.Provider` interface has no
method for this today (`pkg/metrics/interfaces.go:12-48`); adding
`RecordRateLimited`/`RecordIPBlocked` is the right place, and note the label must
not be the raw client IP unless bounded — see `metrics.audit.md` findings 2 and 5.

---

### 12. Medium — `UnblockCIDR` cannot unblock a non-canonical range

`blacklist.go:82-94`:

```go
func (bl *IPBlacklist) UnblockCIDR(cidr string) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	// Find and remove the CIDR
	for i, ipNet := range bl.cidrs {
		if ipNet.String() == cidr {
			bl.cidrs = append(bl.cidrs[:i], bl.cidrs[i+1:]...)
			break
		}
	}
	delete(bl.reason, cidr)
}
```

`BlockCIDR` stores the parsed network in `bl.cidrs` but files the reason under the
**caller's original string** (`:65-68`), while `UnblockCIDR` compares against
`ipNet.String()` — the *canonical* form.

**Failure scenario.** An operator blocks `BlockCIDR("10.0.0.1/8", "abuse")`.
`net.ParseCIDR` normalizes the network to `10.0.0.0/8`, so `bl.cidrs` holds
`10.0.0.0/8` while `bl.reason` holds the key `10.0.0.1/8`. Calling
`UnblockCIDR("10.0.0.1/8")` then:

- fails the comparison (`"10.0.0.0/8" != "10.0.0.1/8"`), so **the range stays
  blocked**, and
- succeeds at `delete(bl.reason, cidr)`, so the reason is erased.

The operator's own input, echoed back verbatim, does not undo their own action;
the range remains blocked with no recorded reason, and nothing is logged or
returned — `UnblockCIDR` has no error return. Restoring service to a wrongly
blocked customer requires knowing to pass the canonical form, which is never
displayed. `GetBlacklist` reports `ipNet.String()` (`:147`), so the value shown to
the operator *is* the one that works — but the value they typed is not, and no
message connects the two.

The same mismatch makes the reason unreachable in `IsBlocked`, which looks it up
by `ipNet.String()` (`:114-118`): a non-canonically-blocked range is reported with
an empty reason, which is what the dead fallback loop at `:120-124` was evidently
meant to fix. That loop cannot work — it tests `key == cidr` after the map lookup
for exactly that key already failed — and `if i < len(bl.cidrs)` at `:126` is
always true inside `range bl.cidrs`, so the whole block reduces to
`return true, ""`.

**Recommendation.** Canonicalize on the way in and key everything consistently:

```go
func (bl *IPBlacklist) BlockCIDR(cidr, reason string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	key := ipNet.String()          // canonical, for both maps
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.cidrs = append(bl.cidrs, ipNet)
	if reason != "" {
		bl.reason[key] = reason
	}
	return nil
}
```

and have `UnblockCIDR` parse its argument the same way, returning an error when
the CIDR is invalid or not present, so a failed unblock is visible. Delete the
dead loop at `:120-128`. Storing `cidrs` as a map keyed by the canonical string
would remove the O(n) removal and the possibility of duplicate entries at the same
time.

---

### 13. Medium — `IsBlocked` fails open, and IPs are compared as raw strings

`blacklist.go:97-133`:

```go
	// Check individual IPs
	if bl.ips[ip] {
		return true, bl.reason[ip]
	}

	// Check CIDR ranges
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false, ""
	}
```

Two problems. **Fail-open on unparseable input**: when `net.ParseIP` fails the
function returns `false` — allow. **String-keyed exact matching**: `BlockIP`
stores the caller's string verbatim (`:48`) and the lookup at `:102` is a raw map
hit, so the textual form must match exactly.

**Failure scenario — fail-open.** With `UseProxy: true` (finding 3),
`getClientIP` returns arbitrary attacker text. `X-Forwarded-For: blocked` is not
in `bl.ips`, does not parse as an IP, and therefore **returns allow** — the CIDR
checks are skipped entirely. Any garbage value bypasses every range rule. A
security control should fail closed on input it cannot interpret, or at minimum
log and fall back to `RemoteAddr`; this does neither, silently.

**Failure scenario — IPv6 evasion.** `BlockIP("2001:db8::1", …)` stores that
exact string. The same host presenting `2001:0db8:0:0:0:0:0:1` or
`2001:DB8::1` — all valid textual forms of one address — produces a different map
key, misses, and (being parseable) is only caught if a CIDR happens to cover it.
IPv4-mapped forms (`::ffff:192.0.2.1` vs `192.0.2.1`) diverge the same way.
Blocking a single IPv6 address is therefore unreliable in a way that is invisible
in testing, because tests naturally reuse the same string on both sides.

**Recommendation.** Normalize once, at both ends, and fail closed:

```go
func normalizeIP(s string) (string, bool) {
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(s), "[]"))
	if ip == nil {
		return "", false
	}
	return ip.String(), true          // canonical form for map keys
}
```

Use it in `BlockIP`, `UnblockIP` and `IsBlocked`. When the address cannot be
parsed, log it and decide deliberately — for a blacklist, treating an
uninterpretable client address as blocked is the defensible default, and is only
safe to do once finding 2's trusted-proxy handling guarantees the value comes
from the connection rather than a header.

---

### 14. Medium — byte-slicing truncation produces invalid UTF-8

`sanitize.go:97-100`:

```go
	// Apply max length
	if s.MaxStringLength > 0 && len(value) > s.MaxStringLength {
		value = value[:s.MaxStringLength]
	}
```

and `SanitizeFilename` (`:217-219`) does the same at 255. `len()` counts **bytes**
and the slice cuts at a byte offset, so a multi-byte rune can be split. Verified:
truncating `"héllo wörld"` at 2 yields `"h\xc3"` — not valid UTF-8.

**Failure scenario.** `StrictSanitizer` sets `MaxStringLength: 10000`
(`sanitize.go:59`), so any string field at the limit whose 10 000th byte falls
inside a multi-byte character is corrupted. Consequences downstream:

- `encoding/json` replaces the invalid byte with U+FFFD when marshalling, so the
  stored value silently gains a replacement character.
- PostgreSQL **rejects** invalid UTF-8 outright (`invalid byte sequence for
  encoding "UTF8"`), so the insert fails with a 500 that names an encoding
  problem, pointing an engineer at the database rather than at this middleware.

Either way the trigger is a specific input length combined with non-ASCII text, so
it reproduces for some users and never for others. Any non-Latin script hits the
limit sooner and more often, so the bug lands hardest on non-English data.

**Recommendation.** Truncate on rune boundaries, and count what the limit is meant
to mean:

```go
if s.MaxStringLength > 0 && utf8.RuneCountInString(value) > s.MaxStringLength {
	runes := []rune(value)
	value = string(runes[:s.MaxStringLength])
}
```

If the limit is genuinely a byte budget (a database column width), keep `len` but
back off to the last valid boundary, e.g. with
`for !utf8.ValidString(value) { value = value[:len(value)-1] }` or
`utf8.DecodeLastRuneInString`. Apply the same fix at `sanitize.go:218`.

---

### 15. Medium — sanitizing one parameter rewrites the whole query string

`sanitize.go:142-160`:

```go
		if r.URL.RawQuery != "" {
			q := r.URL.Query()
			sanitized := false
			for key, values := range q {
				for i, value := range values {
					sanitizedValue := s.Sanitize(value)
					if sanitizedValue != value {
						values[i] = sanitizedValue
						sanitized = true
					}
				}
				if sanitized {
					q[key] = values
				}
			}
			if sanitized {
				r.URL.RawQuery = q.Encode()
			}
		}
```

**Failure scenario.** A single modified value triggers
`r.URL.RawQuery = q.Encode()`, which re-serializes the query from the parsed map.
That loses information `url.Values` cannot represent:

- **Malformed pairs are discarded.** `r.URL.Query()` drops any pair with invalid
  percent-encoding (and returns an error the code never checks). Those parameters
  vanish from the rewritten query, so a request that would have failed validation
  visibly instead proceeds with fields **missing**.
- **Bare keys gain `=`.** `?flag` re-encodes as `flag=`, changing presence
  semantics for any handler distinguishing the two.
- **Order is destroyed.** `Encode` sorts keys alphabetically, so any handler
  reading repeated parameters positionally sees a different request.

Given finding 6 — where `EscapeHTML` alters almost any value containing `&`, `<`,
`>` or a quote — the `sanitized` flag is set on most real requests, so this
rewrite is the common path rather than the exception. The `sanitized` flag is also
never reset between keys (it is declared outside the `for key` loop at `:144`), so
once any value changes, `q[key] = values` executes for every later key too; that
assignment is harmless — `values` already aliases the map's slice — but it shows
the flag is not doing what it appears to.

**Recommendation.** Do not rewrite `RawQuery`. Under finding 5's recommendation
the middleware rejects rather than edits, which removes this code path entirely.
If values must be modified, attach the sanitized `url.Values` to the request
context and have handlers read from there, leaving `r.URL` untouched:

```go
ctx := context.WithValue(r.Context(), sanitizedQueryKey{}, q)
next.ServeHTTP(w, r.WithContext(ctx))
```

Also check the error from `url.ParseQuery` and reject a malformed query string
outright instead of silently discarding parameters.

---

### 16. Medium — the cleanup goroutine cannot be stopped and has no recover

`NewRateLimiter` starts a goroutine (`ratelimit.go:36`) with no corresponding
`Stop`/`Close`:

```go
	// Start cleanup goroutine
	go rl.cleanupRoutine()
```

`cleanupRoutine` loops `for range ticker.C` forever (`:69`); the
`defer ticker.Stop()` at `:67` is unreachable because the loop has no exit. There
is no `recover()` in the goroutine.

**Failure scenario.** Every `RateLimiter` ever constructed leaks one goroutine and
one live `time.Ticker` for the process lifetime. With a single limiter created at
startup that is negligible — but the type is constructed per configuration, and
any test suite, config reload, or per-tenant limiter accumulates them. The
goroutine also retains the whole `RateLimiter`, so every limiter map it ever held
stays reachable and unreclaimable.

The missing `recover()` is the more serious half: a panic anywhere in this
goroutine — today only a map operation, but any future eviction logic — crashes
the **entire process**, because a panic in a goroutine cannot be recovered by
`PanicRecovery` or by any handler in the stack. This is the pattern flagged in
`_CROSS-CUTTING.audit.md` finding X7.

**Recommendation.** Give it a lifecycle and a guard:

```go
func (rl *RateLimiter) cleanupRoutine() {
	defer logger.CatchPanicCallback("middleware.cleanupRoutine", nil)()
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.evictIdle()          // finding 8
		case <-rl.stop:
			return
		}
	}
}

// Close stops the cleanup goroutine. Safe to call more than once.
func (rl *RateLimiter) Close() {
	rl.stopOnce.Do(func() { close(rl.stop) })
}
```

Use `sync.Once` so a double `Close` cannot panic — the defect
`metrics.audit.md` finding 8 records for `StopAutoPush`. Have `pkg/server` call
`Close` during shutdown once the limiter is wired (finding 1).

---

### 17. Low — `removeControlCharacters` keeps DEL and the C1 range

`sanitize.go:186-195`:

```go
// removeControlCharacters removes control characters except \n, \r, \t
func removeControlCharacters(s string) string {
	var result strings.Builder
	for _, r := range s {
		// Keep newline, carriage return, tab, and non-control characters
		if r == '\n' || r == '\r' || r == '\t' || r >= 32 {
			result.WriteRune(r)
		}
	}
	return result.String()
}
```

`r >= 32` admits everything above the C0 block. Verified: `\x7f` (DEL) and
`U+0085` (NEL, a C1 control) both survive, contradicting the doc comment.

**Failure scenario.** Minor in isolation. DEL and C1 controls reaching logs can
corrupt or forge log lines — `U+0085` is a line terminator to some log processors,
enabling log injection by an attacker who controls a sanitized field. The function
also passes Unicode characters that matter more than C1 controls: U+202E
(right-to-left override) for filename and display spoofing, and zero-width
characters (U+200B, U+FEFF) for filter evasion and homograph tricks. The contract
says control characters are removed, so callers reasonably trust it.

**Recommendation.** Use the standard predicate and extend to the formatting
category:

```go
for _, r := range s {
	switch {
	case r == '\n' || r == '\t' || r == '\r':
		result.WriteRune(r)
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r):   // Cf: bidi + zero-width
		// drop
	default:
		result.WriteRune(r)
	}
}
```

`unicode.IsControl` covers both C0 and C1; `unicode.Cf` covers the bidi overrides
and zero-width characters. Also consider normalizing to NFC.

---

### 18. Low — 429 responses are mislabelled and omit `Retry-After`

`ratelimit.go:88` and `:108`:

```go
			http.Error(w, `{"error":"rate_limit_exceeded","message":"Too many requests"}`, http.StatusTooManyRequests)
```

`http.Error` sets `Content-Type: text/plain; charset=utf-8` and appends a newline,
so a JSON body is served declared as plain text. There is no `Retry-After`.

**Failure scenario.** A client that dispatches on `Content-Type` — the correct
behaviour — treats the body as text and cannot read the `error` code, so it
surfaces a generic failure instead of "rate limited". Without `Retry-After`, a
well-behaved client has no idea how long to wait and will typically retry
immediately, so the limiter produces **more** load from compliant clients than it
would with the header. `rate.Limiter` can supply the delay directly, and the
blacklist path does this correctly (`blacklist.go:181-183` sets the header and
uses `json.NewEncoder`), so the package is internally inconsistent.

**Recommendation.** Mirror the blacklist's approach and include the delay:

```go
res := limiter.Reserve()
if !res.OK() || res.Delay() > 0 {
	if d := res.Delay(); d > 0 {
		res.Cancel()
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(d.Seconds()))))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "rate_limit_exceeded", "message": "Too many requests",
	})
	return
}
```

Note `Reserve` must be `Cancel`ed when rejecting, or the token is consumed twice.
Adding `X-RateLimit-Limit`/`-Remaining` is conventional, but see finding 10 — they
disclose budget state, so expose them only to authenticated clients.

---

### 19. Low — `IsBlocked` is O(n) per request with a redundant nested scan

`blacklist.go:112-130` walks every CIDR on every request under `RLock`, and for
each *match* additionally iterates the whole `reason` map (`:120-124`) in a loop
that cannot succeed (finding 12).

**Failure scenario.** A sizeable blacklist — a few thousand ranges from a threat
feed is ordinary — means a few thousand `ipNet.Contains` calls per request, each
allocating nothing but branching over 4 or 16 bytes. Measurable but not fatal;
the concern is that the work happens while holding `RLock`, so it contends with
`BlockIP`/`BlockCIDR` writers, and grows linearly with a list operators are
encouraged to extend. Coupled with finding 8's write-lock flush in the rate
limiter, a large deployment sees both maps serialized on the request path.

**Recommendation.** Use a prefix-trie lookup (`cidranger`, or
`netipx.IPSet` with `net/netip`) for O(prefix-length) matching instead of O(n),
and drop the nested reason scan. Migrating to `net/netip` also removes the
per-request `net.IP` allocation that `net.ParseIP` makes today.

---

### 20. Low — `GetAllRateLimitInfo` takes n+1 lock acquisitions

`ratelimit.go:162-171`:

```go
func (rl *RateLimiter) GetAllRateLimitInfo() []*RateLimitInfo {
	ips := rl.GetTrackedIPs()
	info := make([]*RateLimitInfo, 0, len(ips))

	for _, ip := range ips {
		info = append(info, rl.GetRateLimitInfo(ip))
	}

	return info
}
```

`GetTrackedIPs` takes `RLock` once (`:127`), then `GetRateLimitInfo` takes it
again per IP (`:139`).

**Failure scenario.** Serving the stats endpoint acquires and releases the read
lock once per tracked IP. Reads do not block each other, but each acquisition
contends with the write lock, and under finding 2 the map can hold millions of
entries — so one stats request performs millions of lock operations while
`getLimiter` writers and finding 8's flush are trying to acquire the write lock.
An unauthenticated endpoint (finding 10) that scales its own cost with
attacker-controlled map size is a small amplification primitive. Entries added
between the snapshot and the per-IP read are also reported with default values, so
the output is mildly inconsistent.

**Recommendation.** Collect everything under one `RLock`:

```go
func (rl *RateLimiter) GetAllRateLimitInfo() []*RateLimitInfo {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	info := make([]*RateLimitInfo, 0, len(rl.limiters))
	for ip, l := range rl.limiters {
		info = append(info, &RateLimitInfo{
			IP: ip, TokensRemaining: l.Tokens(),
			Limit: float64(rl.rate), Burst: rl.burst,
		})
	}
	return info
}
```

and bound or paginate the result.

---

### 21. Low — `SanitizeURL` and `SanitizeFilename` are easily evaded

`sanitize.go:236-251`:

```go
	// Block javascript: and data: protocols
	if strings.HasPrefix(strings.ToLower(url), "javascript:") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		return ""
	}
```

Only `\x00` is stripped beforehand (`:240`), so `"\x01javascript:alert(1)"` fails
both prefix tests and is returned unchanged — browsers ignore the leading control
character and execute it. `vbscript:`, `file:` and `blob:` are not covered, and a
scheme-relative `//evil.com` passes. `SanitizeFilename` (`:207-222`) removes `..`,
`/` and `\` textually, so it misses percent-encoded traversal (`%2e%2e%2f`, since
nothing decodes) and Windows drive prefixes (`C:`), while mangling legitimate
names containing `..`.

**Failure scenario.** A stored URL passes `SanitizeURL` and is rendered into an
`href`, giving XSS on click. An uploaded filename passes `SanitizeFilename` in
encoded form and escapes the intended directory once some later layer decodes it.
Both functions are exported helpers, so callers reasonably treat them as
sufficient — that assumption is the risk, more than the specific gaps.

**Recommendation.** Parse rather than pattern-match. For URLs, use
`net/url.Parse` and allowlist the scheme:

```go
u, err := url.Parse(strings.TrimSpace(raw))
if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
	return ""
}
```

For filenames, take `filepath.Base` of the **decoded** value and validate the
result against `^[A-Za-z0-9._-]{1,255}$`, rejecting `.`/`..` explicitly, rather
than deleting substrings. Never construct a path by removing characters.

---

### 22. Low — `getClientIP` mishandles port-less and bracketed addresses

`ratelimit.go:228-232`:

```go
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}

	return r.RemoteAddr
```

**Failure scenario.** For the normal `"[::1]:54321"` this happens to work, giving
`"[::1]"` — but the brackets are kept, while a client-supplied `X-Forwarded-For`
would give the same address as `"::1"`. The two forms are different map keys, so
one client can occupy two rate-limit buckets (and evade one blacklist entry).
`blacklist.go:160` and `:168` paper over this with `strings.Trim(clientIP, "[]")`
while `ratelimit.go` does not — so the two middlewares key on different strings
for the same client. If `RemoteAddr` ever lacks a port (a synthetic request, a
test, a non-TCP listener), `LastIndex` finds a colon **inside** the IPv6 address
and truncates it — `"::1"` becomes `"::"` — silently merging unrelated clients
into one bucket.

**Recommendation.** Use the standard parser and normalize, once, in the shared
helper from finding 2:

```go
host, _, err := net.SplitHostPort(r.RemoteAddr)
if err != nil {
	host = r.RemoteAddr
}
if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
	return ip.String()
}
return host
```

`net.ParseIP(...).String()` yields one canonical form, which fixes the
rate-limit/blacklist key mismatch and finding 13's IPv6 evasion together.

---

### 23. Low — `//nolint:all` suppresses linting on the import block

`ratelimit.go:4`:

```go
// Package middleware provides HTTP middleware functionalities such as rate limiting and IP blacklisting.
package middleware

//nolint:all
import (
```

**Failure scenario.** A blanket `//nolint:all` with no rule named and no
justification. Attached to the import declaration its effect is narrow, but it
suppresses *every* linter there — including `depguard` and `gosec` import rules,
which is exactly where a prohibited or vulnerable dependency would be flagged.
More practically it sets a precedent: the directive gives no reason, so nobody can
tell whether it is still needed, and it survives indefinitely. Given `gosec` is
not enabled at all (`_CROSS-CUTTING.audit.md` finding X4), suppressions that hide
security linting deserve removal on principle.

**Recommendation.** Delete it and fix whatever it was hiding; if a suppression is
genuinely required, name the rule and give a reason —
`//nolint:depguard // x/time/rate is an approved dependency`. `golangci-lint`'s
`nolintlint` setting enforces exactly this and is worth enabling repo-wide.

---

## What looks right

- **`getLimiter` implements correct double-checked locking**
  (`ratelimit.go:42-62`): read under `RLock`, release, take `Lock`, **re-check**
  before creating. The re-check at `:55` is the step usually omitted, and getting
  it right means two goroutines racing on a new key cannot produce two limiters —
  which would silently double a client's allowance. This is the pattern
  `pkg/cache`'s `defaultCache` singleton should adopt (`cache.audit.md`
  finding 3).
- **Every shared field is consistently guarded.** `IPBlacklist` takes `Lock` for
  all four mutators (`blacklist.go:45`, `:62`, `:74`, `:83`) and `RLock` for both
  readers (`:98`, `:137`); `RateLimiter` does the same. There are **no
  unsynchronized globals in this package**, unlike most others in the repo
  (`_CROSS-CUTTING.audit.md` finding X5), and I found no data race on any path.
- Locks are held for short, bounded critical sections with no I/O, no callbacks
  and no lock nesting inside them, so deadlock is structurally impossible here.
- `blacklist.go` returns its 403 correctly: sets `Content-Type` **before**
  `WriteHeader`, then encodes (`:181-186`) — the ordering `ratelimit.go:88` gets
  wrong (finding 18).
- `BlockIP` validates with `net.ParseIP` before storing (`blacklist.go:41-43`) and
  `BlockCIDR` propagates `net.ParseCIDR`'s error (`:57-60`), so invalid rules are
  rejected at the point of entry rather than silently ignored.
- `RequestSizeLimiter` uses `http.MaxBytesReader` (`sizelimit.go:36`) rather than
  trusting `Content-Length` — the correct choice, since `Content-Length` is
  attacker-supplied and absent on chunked requests. `NewRequestSizeLimiter`
  defaults a non-positive `maxSize` to 10 MB (`:24-26`) rather than to unlimited,
  which is the safe direction. (It is still never mounted — finding 1.)
- `PanicRecovery` passes the request context into `logger.HandlePanic`
  (`panic.go:24-25`) so the error tracker can correlate the panic with the request
  trace — a deliberate touch, and the comment explains why.
- `pkg/server` applies `PanicRecovery` **inside** `trackRequestsMiddleware`
  (`manager.go:466` vs `:540`), so in-flight accounting survives a panicking
  handler, and offers `PanicHandler` (`interfaces.go:47`) as a documented override
  for embedders who need different behaviour.
- `sanitizeValue` recurses correctly through nested maps and slices
  (`sanitize.go:120-135`) with a `default` branch that passes non-strings through
  untouched, so numbers and booleans are not stringified.
- `Sanitizer`'s fields are read-only after construction and `regexp.Regexp` is
  safe for concurrent matching, so a shared `*Sanitizer` across handlers is
  race-free — provided callers do not mutate the exported fields at runtime, which
  nothing guards against but nothing does.
- **1 127 lines of tests against 799 lines of code**, the best ratio in the
  repository, including concurrency tests. The tests are why findings 12, 13 and
  17 are the only correctness bugs of their kind left; they are also why finding 4
  is *locked in* rather than merely present.

## Suggested follow-up

1. **Decide the perimeter story (finding 1).** Either wire rate limiting, size
   limiting and blacklisting into `pkg/server` from `MiddlewareConfig`, or delete
   them and the 18 KB README that documents them as available. The present state —
   configurable, documented, tested, unmounted — is the one that misleads
   operators into believing they are protected.
2. **Fix the trusted-proxy model before mounting anything IP-based**
   (findings 2, 3, 22). One shared, normalized, default-deny `ClientIP` helper for
   both middlewares, with `TrustedProxies` in config. Mounting the current
   `getClientIP` gives an attacker an unauthenticated memory-growth primitive and
   a header that switches the blacklist off.
3. **Stop returning the panic value** (finding 4) and update
   `panic_test.go:57` to assert the opposite. Smallest diff, largest security
   win, and it is the only middleware currently in the request path.
4. **Do not mount the sanitizer as it stands** (findings 5, 6, 7, 14, 15).
   Convert it from strip-and-continue to validate-and-reject, set
   `EscapeHTML: false`, and move XSS defense to context-aware output encoding.
   Mounting it today would corrupt filter values and JSON parameters on the first
   request while leaving multi-line `<script>` blocks intact.
5. **Bound the limiter map and evict by age** (findings 2, 8), and give the
   cleanup goroutine a `Close` and a `recover()` (finding 16).
6. **Add enforcement logging and counters** (finding 11) at `Info`, never `Warn`,
   and require authentication on both stats handlers or move them to an internal
   listener (finding 10).
7. **Fix the CIDR canonicalization bugs** (findings 12, 13) — an operator who
   cannot unblock a range they blocked will find out during an incident.
8. **Add `go test -race ./pkg/middleware/...` to CI.** The package has the repo's
   best tests and is not in the tested set (`_CROSS-CUTTING.audit.md` findings X1,
   X2). A test asserting `X-Forwarded-For` does **not** change the rate-limit key
   would pin finding 2 shut permanently.

## Cross-references

- `audit/pkg/_CROSS-CUTTING.audit.md` — **X10 (declared-but-never-installed
  subsystems) is finding 1 of this audit generalized**, and is where the combined
  remediation order lives; X1/X2 (no `-race`, `go test` scope) cover
  why these tests do not run in CI; X7 (three panic conventions) is the context
  for findings 4 and 16; X8 (every `Warn` reaches Sentry) is why finding 11
  specifies `Info`; X4 (`gosec` not enabled) relates to finding 23.
- `audit/pkg/metrics.audit.md` — finding 1 there is why `RecordPanic`
  (`panic.go:19`) does nothing; finding 11 there is the same
  response-already-written problem as finding 4's second half; findings 2 and 5
  there are why finding 11's new counters must not use a raw client IP as a label.
- `audit/pkg/server.audit.md` — `manager.go:456-466` and `:540` set the middleware
  order this audit's finding 1 recommends; `PanicHandler`
  (`interfaces.go:47`) is the existing opt-out for finding 4; the metrics/stats
  listener recommended in finding 10 is a `servers.instances.*` question.
- `audit/pkg/config.audit.md` — `MiddlewareConfig` (`config.go:121-126`) and its
  defaults (`manager.go:209-211`) are declared and unread (finding 1);
  `TrustedProxies` needs adding there for findings 2 and 3.
- `audit/pkg/logger.audit.md` — `HandlePanic` returning the panic value as an
  `error` (`logger.go:210`) is what makes finding 4 possible; any caller that
  writes that error to a response has the same leak.
- `audit/pkg/restheadspec.audit.md` — the package whose filters and header-borne
  options finding 6 corrupts, and whose unbounded queries finding 1's missing rate
  limit exposes.
- `audit/pkg/cache.audit.md` — finding 3 there (unsynchronized lazy singleton) is
  the bug that `getLimiter`'s double-checked locking here avoids.
