// Package quickproxy provides a small reverse-proxy layer that tries a set
// of configured upstream targets first, and falls back to a caller-supplied
// http.Handler (typically static file serving) when the upstream is
// unreachable or returns 404.
package quickproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Rule maps a URL path prefix to an upstream target.
// A Rule with URLPrefix "/" acts as a catch-all passthrough.
type Rule struct {
	// URLPrefix is the URL path prefix this rule matches. Must start with "/".
	URLPrefix string

	// Target is the upstream base URL, e.g. "http://localhost:3000".
	// The incoming request path and query are forwarded unchanged; only the
	// scheme and host are rewritten to Target's.
	Target string

	// Exclude is a list of URL path prefixes that this rule should not
	// proxy, even though they fall under URLPrefix. Each entry must start
	// with "/". A request matching an Exclude prefix is treated as if this
	// rule didn't match at all: matching continues against any other
	// configured rule, falling back if none match. This is typically used
	// to carve out paths (e.g. "/health") from a catch-all "/" rule so
	// they're served by the fallback handler instead of being proxied.
	Exclude []string
}

// DefaultTimeout is the dial and response-header timeout applied to
// upstream requests when no WithTimeout option is given. It does not limit
// response body streaming.
const DefaultTimeout = 10 * time.Second

// Option configures a Service.
type Option func(*options)

type options struct {
	timeout time.Duration
}

// WithTimeout sets the dial and response-header timeout used when
// connecting to upstream targets. It does not limit response body
// streaming, so it won't interrupt long-lived downloads or SSE/WebSocket
// connections once established.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// compiledRule pairs a Rule with its ready-to-use reverse proxy.
type compiledRule struct {
	prefix   string
	excludes []string
	proxy    *httputil.ReverseProxy
}

// excluded reports whether path falls under one of the rule's Exclude prefixes.
func (r *compiledRule) excluded(path string) bool {
	for _, ex := range r.excludes {
		if strings.HasPrefix(path, ex) {
			return true
		}
	}
	return false
}

// Service holds a compiled set of proxy rules and performs longest-prefix
// matching against them. A Service is safe for concurrent use once
// returned from NewService; Handler must be called once per Service to
// wire up the fallback handler before the returned http.Handler is served.
type Service struct {
	rules []compiledRule // sorted by descending prefix length
}

// errUpstreamNotFound is a sentinel error returned from ModifyResponse to
// make ReverseProxy invoke ErrorHandler (our fallback path) instead of
// writing the upstream's 404 to the client. Nothing has been written to
// the ResponseWriter yet when this happens.
var errUpstreamNotFound = errors.New("quickproxy: upstream returned 404")

// NewService compiles the given rules into a Service. Rules are matched by
// longest URLPrefix, so a catch-all "/" rule can coexist with more specific
// rules such as "/api".
func NewService(rules []Rule, opts ...Option) (*Service, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("quickproxy: no rules configured")
	}

	cfg := options{timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}

	seen := make(map[string]bool, len(rules))
	compiled := make([]compiledRule, 0, len(rules))

	for _, r := range rules {
		if !strings.HasPrefix(r.URLPrefix, "/") {
			return nil, fmt.Errorf("quickproxy: rule prefix %q must start with /", r.URLPrefix)
		}
		if seen[r.URLPrefix] {
			return nil, fmt.Errorf("quickproxy: duplicate rule prefix %q", r.URLPrefix)
		}
		seen[r.URLPrefix] = true

		target, err := url.Parse(r.Target)
		if err != nil || target.Scheme == "" || target.Host == "" {
			return nil, fmt.Errorf("quickproxy: invalid target %q for prefix %q", r.Target, r.URLPrefix)
		}

		for _, ex := range r.Exclude {
			if !strings.HasPrefix(ex, "/") {
				return nil, fmt.Errorf("quickproxy: exclude prefix %q for rule %q must start with /", ex, r.URLPrefix)
			}
		}

		compiled = append(compiled, compiledRule{
			prefix:   r.URLPrefix,
			excludes: r.Exclude,
			proxy:    newReverseProxy(target, cfg.timeout),
		})
	}

	// Longest prefix first, so the first match in Handler is always the
	// most specific one.
	sort.Slice(compiled, func(i, j int) bool {
		return len(compiled[i].prefix) > len(compiled[j].prefix)
	})

	return &Service{rules: compiled}, nil
}

func newReverseProxy(target *url.URL, timeout time.Duration) *httputil.ReverseProxy {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
		ResponseHeaderTimeout: timeout,
	}

	return &httputil.ReverseProxy{
		Transport: transport,
		Director: func(req *http.Request) {
			originalHost := req.Host

			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if originalHost != "" {
				req.Header.Set("X-Forwarded-Host", originalHost)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode == http.StatusNotFound {
				return errUpstreamNotFound
			}
			return nil
		},
	}
}

// Handler returns an http.Handler that tries the configured proxy rules
// first (longest-prefix match), and calls fallback when no rule matches,
// the upstream is unreachable, or the upstream returns 404. Any other
// upstream response (2xx, other 4xx, 5xx) is streamed through to the
// client unchanged.
//
// Handler wires up ErrorHandler on the Service's compiled rules, so it
// should be called once per Service, before the returned http.Handler
// starts serving requests.
func (s *Service) Handler(fallback http.Handler) http.Handler {
	if fallback == nil {
		fallback = http.NotFoundHandler()
	}

	for i := range s.rules {
		s.rules[i].proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
			fallback.ServeHTTP(w, r)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rule := s.match(r.URL.Path)
		if rule == nil {
			fallback.ServeHTTP(w, r)
			return
		}
		rule.proxy.ServeHTTP(w, r)
	})
}

// match returns the longest-prefix rule matching path, or nil if none match.
// A rule whose Exclude covers path is skipped, and matching continues
// against the next-longest-prefix rule.
func (s *Service) match(path string) *compiledRule {
	for i := range s.rules {
		if !strings.HasPrefix(path, s.rules[i].prefix) {
			continue
		}
		if s.rules[i].excluded(path) {
			continue
		}
		return &s.rules[i]
	}
	return nil
}
