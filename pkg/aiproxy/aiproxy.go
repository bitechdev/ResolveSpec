// Package aiproxy is an authenticated reverse proxy for OpenAI-compatible APIs and
// MCP servers (streamable HTTP). Upstream URLs and keys stay on the server; clients
// authenticate with the normal security layer and call the proxy instead.
//
//	p := aiproxy.New(aiproxy.Config{Prefix: "/ai"})
//	p.RegisterOpenAI(aiproxy.OpenAIUpstream{Upstream: aiproxy.Upstream{Name: "gpt", BaseURL: "https://api.openai.com/v1", APIKey: key}})
//	p.RegisterMCP(aiproxy.MCPUpstream{Upstream: aiproxy.Upstream{Name: "tools", BaseURL: "http://localhost:3000/mcp", APIKey: key}})
//	mux.Handle("/ai/", p.Handler(security.NewAuthMiddleware(securityList)))
//
// Client routes: {prefix}/{name}/v1/... (OpenAI) and {prefix}/{name} (MCP).
package aiproxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxBody = 10 << 20
	defaultTimeout = 120 * time.Second
)

// Config configures a Proxy.
type Config struct {
	// Prefix the handler is mounted under, e.g. "/ai". Empty when mounted with http.StripPrefix.
	Prefix string
	// MaxBodyBytes caps JSON request bodies that are inspected. Default 10 MB.
	MaxBodyBytes int64
	// StripHeaders are removed from client requests in addition to the defaults
	// (Authorization, Cookie, Proxy-Authorization, X-Api-Key).
	StripHeaders []string
	// Audit receives one record per request. Default: LogAuditSink. Use NopAuditSink to disable.
	Audit AuditSink
}

// Definition describes one upstream independent of how it was registered.
type Definition struct {
	Kind     Kind
	Upstream Upstream
	// Allowed holds AllowedModels (OpenAI) or AllowedTools (MCP).
	Allowed []string
}

// target is a registered upstream, ready to serve.
type target struct {
	def     Definition
	kind    Kind
	up      Upstream
	base    *url.URL
	roles   map[string]struct{}
	models  map[string]struct{}
	tools   map[string]struct{}
	proxy   *httputil.ReverseProxy
	managed bool // loaded by Reload; code-registered upstreams are never touched by it

	authName string
	authVal  string
}

// Proxy routes authenticated requests to registered upstreams.
type Proxy struct {
	cfg      Config
	hooks    *HookRegistry
	limits   *limiters
	labels   modelLabels
	mu       sync.RWMutex
	upstream map[string]*target
}

// New creates a Proxy.
func New(cfg Config) *Proxy {
	cfg.Prefix = strings.TrimRight(cfg.Prefix, "/")
	if cfg.Audit == nil {
		cfg.Audit = LogAuditSink{}
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBody
	}
	return &Proxy{
		cfg:      cfg,
		hooks:    NewHookRegistry(),
		limits:   newLimiters(),
		upstream: make(map[string]*target),
	}
}

// Hooks returns the hook registry.
func (p *Proxy) Hooks() *HookRegistry { return p.hooks }

// RegisterOpenAI registers an OpenAI-compatible upstream.
func (p *Proxy) RegisterOpenAI(u OpenAIUpstream) error {
	return p.Register(Definition{Kind: KindOpenAI, Upstream: u.Upstream, Allowed: u.AllowedModels})
}

// RegisterMCP registers an MCP server (streamable HTTP).
func (p *Proxy) RegisterMCP(u MCPUpstream) error {
	return p.Register(Definition{Kind: KindMCP, Upstream: u.Upstream, Allowed: u.AllowedTools})
}

// Register registers an upstream from a Definition. The name must be unused.
func (p *Proxy) Register(d Definition) error {
	t, err := p.build(d)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.upstream[t.up.Name]; dup {
		return fmt.Errorf("aiproxy: upstream %q already registered", t.up.Name)
	}
	p.upstream[t.up.Name] = t
	return nil
}

// Unregister removes an upstream (code-registered or loaded). It reports whether it existed.
// In-flight requests finish normally.
func (p *Proxy) Unregister(name string) bool {
	p.mu.Lock()
	t, ok := p.upstream[name]
	delete(p.upstream, name)
	p.mu.Unlock()
	if ok {
		p.retire(t)
	}
	return ok
}

// retire releases what a removed or replaced target held.
func (p *Proxy) retire(t *target) {
	p.limits.forget(t.up.Name)
	if tr, ok := t.proxy.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}

func (p *Proxy) build(d Definition) (*target, error) {
	if d.Kind != KindOpenAI && d.Kind != KindMCP {
		return nil, fmt.Errorf("aiproxy: upstream %q has unknown kind %q", d.Upstream.Name, d.Kind)
	}
	base, err := d.Upstream.validate()
	if err != nil {
		return nil, err
	}
	t := &target{def: d, kind: d.Kind, up: d.Upstream, base: base, roles: toSet(d.Upstream.AllowedRoles)}
	if d.Kind == KindOpenAI {
		t.models = toSet(d.Allowed)
	} else {
		t.tools = toSet(d.Allowed)
	}
	t.authName, t.authVal = d.Upstream.authValue()
	t.proxy = p.buildProxy(t)
	return t, nil
}

func (p *Proxy) get(name string) *target {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.upstream[name]
}

// Names lists registered upstream names (no URLs or keys).
func (p *Proxy) Names() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.upstream))
	for n := range p.upstream {
		out = append(out, n)
	}
	return out
}

func newTransport(timeout time.Duration) *http.Transport {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
}
