# Plan: Proxy endpoint support for pkg/webserver2 static serving

Status: **`quickproxy` package implemented in ResolveSpec.** GoCore-side wiring
(config field, `webserver2/proxy.go`, `server.go` route ordering) is a
separate repo, not present here, and remains unimplemented.

## Goal

`pkg/webserver2` (via `WithStaticFS`) currently serves static files from
`WebResDir` (`/res`) and `WebStaticDir` (`/`), using ResolveSpec's
`pkg/server/staticweb` package, with an HTML fallback for SPA routing.

We're adding a proxy layer: requests are tried against a configured proxy
target first; if the proxy can't resolve them (unreachable, or upstream
returns 404), they fall back to the existing static file handling.

## Decisions (from Q&A with user)

1. **Config shape**: a list of prefix rules (like nginx `location` blocks),
   where one rule may use prefix `/` to act as a catch-all passthrough.
   So a single config list covers both "proxy just `/api`" and "proxy
   everything not otherwise matched" use cases.
2. **Package placement**: new package in the **ResolveSpec** repo
   (`/mnt/vault/ResolveSpec`, published as `github.com/bitechdev/ResolveSpec`),
   named **`quickproxy`**, living alongside `pkg/server/staticweb` at
   `pkg/server/quickproxy`. GoCore's `pkg/webserver2` will consume it the
   same way it consumes `staticweb` today (wire it up in `static.go`),
   after bumping the ResolveSpec dependency version — mirroring the existing
   `fix(resolve-spec): update ResolveSpec to vX.Y.Z` commit pattern already
   used in this repo's history.
3. **Fallback trigger**: fall back to static files both when the upstream is
   unreachable (dial/connect error, timeout) **and** when the upstream
   responds with `404`. Any other upstream response (2xx, other 4xx, 5xx) is
   passed through to the client as-is.
4. **Method scope**: proxying applies to **all HTTP methods**, not just GET
   (unlike today's static routes, which are GET-only).

## New package: `github.com/bitechdev/ResolveSpec/pkg/server/quickproxy`

Repo location: `/mnt/vault/ResolveSpec/pkg/server/quickproxy`

### Types

```go
package quickproxy

// Rule maps a URL prefix to an upstream target.
// A Rule with URLPrefix "/" acts as a catch-all passthrough.
type Rule struct {
    URLPrefix string // e.g. "/api", "/"
    Target    string // upstream base URL, e.g. "http://localhost:3000"
}

// Service holds the compiled set of rules and does longest-prefix matching.
type Service struct { ... }

func NewService(rules []Rule) (*Service, error)
```

### Matching & fallback behavior

- Longest-prefix match against configured rules, same convention as
  `staticweb`'s mount points.
- Implemented with `net/http/httputil.ReverseProxy` per matched rule
  (director rewrites scheme/host/path, adds `X-Forwarded-Host` /
  `X-Forwarded-For`).
- **404 detection without buffering the body**: use
  `ReverseProxy.ModifyResponse` to inspect `resp.StatusCode` as soon as
  headers arrive from upstream, *before* the body is streamed to the
  client. If it's 404, return a sentinel error from `ModifyResponse` — this
  causes `ReverseProxy` to invoke `ErrorHandler` instead of writing
  anything to the client, so we never commit a partial/wrong response.
- **Connection failure detection**: `ReverseProxy.ErrorHandler` catches
  both the sentinel 404 error and real transport errors (dial failure,
  timeout, connection reset).
- In both cases, `ErrorHandler` invokes a caller-supplied fallback
  `http.Handler` (the existing static file service) instead of writing an
  error response. Because nothing has been written to the `ResponseWriter`
  yet in either failure path, the fallback handler can write a normal
  response (status, headers, body) as if the proxy had never been tried.
- Any other status code (including other 4xx/5xx) streams straight through
  to the client — no masking of real upstream errors.

### Handler API

```go
// Handler returns an http.Handler that tries the configured proxy rules
// first (longest-prefix match), and calls fallback when no rule matches,
// the upstream is unreachable, or the upstream returns 404.
func (s *Service) Handler(fallback http.Handler) http.Handler
```

This mirrors the shape of `staticweb.StaticFileService.Handler()` so it
composes the same way in `pkg/webserver2`.

## Config changes (GoCore)

`pkg/cfg/settings-model.go` — add a new field to `Settings`, following the
existing flat-JSON convention used by `WebStaticDir`/`WebResDir`:

```go
type ProxyRule struct {
    URLPrefix string `json:"urlprefix"`
    Target    string `json:"target"`
}

// in Settings:
WebProxyRules []ProxyRule `json:"webproxyrules"`
```

## Wiring changes (GoCore, `pkg/webserver2`)

New file `pkg/webserver2/proxy.go` (parallel to `static.go`), e.g.:

```go
func (r *Router) WithProxyFS(corestate cfg.State) *Router {
    if len(corestate.Cfg.WebProxyRules) == 0 {
        return r
    }
    rules := make([]quickproxy.Rule, 0, len(corestate.Cfg.WebProxyRules))
    for _, pr := range corestate.Cfg.WebProxyRules {
        rules = append(rules, quickproxy.Rule{URLPrefix: pr.URLPrefix, Target: pr.Target})
    }
    service, err := quickproxy.NewService(rules)
    if err != nil {
        logger.Error("could not configure proxy service %v", err)
        return r
    }
    r.proxyService = service // stored for static.go to consume as fallback target
    return r
}
```

### Integration point with existing static routing — needs care

This is the trickiest part of the wiring, flagged here rather than decided,
since it affects route registration order in `server.go`:

- For a proxy rule prefix that **doesn't** overlap with the static mounts
  (e.g. `/api`), we can register new routes directly:
  `r.Handle(method, "/api/*path", quickproxy handler)` for all methods,
  with no fallback (there's no static content at `/api` anyway).
- For the **catch-all `/` proxy rule**, it directly overlaps the existing
  `WebStaticDir` mount at `/`. Rather than registering a second competing
  route, `WithStaticFS`'s existing GET handler for `/*path` needs to become
  `proxyService.Handler(staticAuth(service.Handler()))` — i.e. the proxy
  wraps the static handler as its fallback, instead of the two being
  separate router registrations. This means `WithProxyFS` needs to run
  *before* `WithStaticFS` (or the two need to be merged into one method),
  and `server.go`'s call chain (`WithStaticFS(corestate)`) needs adjusting
  accordingly.
- Non-GET methods on the catch-all `/` rule have no static fallback to
  offer (static routes are GET-only today) — they'd just proxy or 404.

## Release flow

Since `quickproxy` lives in the separate ResolveSpec module:
1. Implement + test in `/mnt/vault/ResolveSpec`.
2. Tag/publish a new ResolveSpec version.
3. Bump `github.com/bitechdev/ResolveSpec` in GoCore's `go.mod`
   (`go get github.com/bitechdev/ResolveSpec@vX.Y.Z`), same as the recent
   `fix(resolve-spec): update ResolveSpec to v1.1.46` commit.
4. Land the GoCore-side wiring (`cfg` field, `proxy.go`, `server.go` call
   order change) in the same or a follow-up commit.

## Resolved decisions (quickproxy implementation)

- **Timeouts**: a single global default (`quickproxy.DefaultTimeout`,
  10s), overridable via `quickproxy.WithTimeout(d)` passed to `NewService`.
  It bounds dial + response-header wait only; response body streaming is
  unbounded, so it won't cut off long downloads or upgraded connections.
- **Auth middleware**: out of scope for `quickproxy`. Whether/how
  `staticAuth` wraps proxied requests is entirely GoCore's call at the
  wiring layer, not something this package decides.
- **Path rewriting**: no prefix stripping — the full incoming path/query is
  forwarded unchanged to the target host (matches nginx `proxy_pass`
  without a trailing slash). Only scheme/host are rewritten, and
  `X-Forwarded-Host` is set from the original `Host` header.

## Open questions / not yet decided

- WebSocket upgrade proxying (needed if any proxied target does live
  reload / HMR, e.g. a frontend dev server) — plain `ReverseProxy` handles
  this automatically via its `Transport`, should confirm it's not disabled.
- Whether request bodies need special handling for large uploads proxied
  to non-GET targets (streaming vs. buffering — `ReverseProxy` streams by
  default, should be fine, but worth confirming with the intended proxy
  targets).

## Testing plan

- `quickproxy` package: unit tests in ResolveSpec covering longest-prefix
  matching, 404-triggers-fallback, connection-error-triggers-fallback,
  pass-through of non-404 error codes, all-methods support.
- `pkg/webserver2`: integration test standing up a fake upstream
  (`httptest.Server`) plus a temp static dir, verifying the
  proxy-then-static-fallback order end to end.
