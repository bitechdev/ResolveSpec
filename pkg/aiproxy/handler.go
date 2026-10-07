package aiproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
	"github.com/tidwall/gjson"
)

type ctxKey struct{}

// call is the per-request state shared between the handler and the proxy callbacks.
type call struct {
	p     *Proxy
	t     *target
	hc    *HookContext
	start time.Time
	once  sync.Once
}

func (c *call) finish(status int, usage Usage, err error) {
	c.once.Do(func() {
		c.hc.StatusCode = status
		c.hc.Usage = usage
		c.hc.Error = err
		c.hc.Duration = time.Since(c.start)
		c.p.hooks.executeAfter(c.hc)
		outcome := OutcomeOK
		if err != nil || status >= 400 {
			outcome = OutcomeUpstreamError
		}
		c.p.record(c.t, c.hc, status, outcome, "", err)
	})
}

var defaultStrip = []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Api-Key"}

// Handler returns the proxy handler. auth is the authentication middleware, normally
// security.NewAuthMiddleware(securityList). It is required: without it every request is refused.
func (p *Proxy) Handler(auth func(http.Handler) http.Handler) http.Handler {
	if auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, "server_error", "authentication is not configured")
		})
	}
	return auth(http.HandlerFunc(p.serve))
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	// route: {prefix}/{name}/{rest}
	rel := strings.TrimPrefix(r.URL.Path, p.cfg.Prefix)
	if rel == r.URL.Path && p.cfg.Prefix != "" {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	rel = strings.TrimPrefix(rel, "/")
	name, rest, _ := strings.Cut(rel, "/")
	t := p.get(name)
	if t == nil {
		writeError(w, http.StatusNotFound, "not_found", "unknown upstream")
		return
	}
	rest = "/" + rest
	if rest != "/" {
		rest = path.Clean(rest)
	}
	if t.kind == KindOpenAI {
		switch {
		case rest == "/v1":
			rest = ""
		case strings.HasPrefix(rest, "/v1/"):
			rest = strings.TrimPrefix(rest, "/v1")
		default:
			writeError(w, http.StatusNotFound, "not_found", "unknown path")
			return
		}
	} else if rest == "/" {
		rest = ""
	}

	start := time.Now()
	user, _ := security.GetUserContext(r.Context())
	hc := &HookContext{
		Context:     r.Context(),
		Request:     r,
		UserContext: user,
		Upstream:    t.up.Name,
		Kind:        t.kind,
	}
	deny := func(code int, outcome Outcome, typ, msg string) {
		hc.Duration = time.Since(start)
		p.record(t, hc, code, outcome, msg, nil)
		writeError(w, code, typ, msg)
	}

	// authenticated user, no guests
	if user == nil || hasRole(user.Roles, "guest") {
		deny(http.StatusUnauthorized, OutcomeDenied, "invalid_api_key", "authentication required")
		return
	}
	if len(t.roles) > 0 && !anyRole(user.Roles, t.roles) {
		deny(http.StatusForbidden, OutcomeDenied, "forbidden", "not allowed to use this upstream")
		return
	}
	if rl := t.up.RateLimit; rl != nil {
		key := t.up.Name + "|" + strconv.Itoa(user.UserID) + "|" + user.UserName
		if allowed, wait := p.limits.allow(key, rl); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			deny(http.StatusTooManyRequests, OutcomeRateLimited, "rate_limit_exceeded", "rate limit exceeded")
			return
		}
	}

	if code, typ, msg := p.inspect(t, r, hc); code != 0 {
		deny(code, OutcomeDenied, typ, msg)
		return
	}

	if err := p.hooks.Execute(BeforeProxy, hc); err != nil {
		code := hc.AbortCode
		if code == 0 {
			code = http.StatusForbidden
		}
		msg := hc.AbortMessage
		if msg == "" {
			msg = "request rejected"
		}
		if !hc.Abort {
			logger.Error("aiproxy: %v", err)
			code, msg = http.StatusInternalServerError, "request failed"
		}
		deny(code, OutcomeDenied, "rejected", msg)
		return
	}

	c := &call{p: p, t: t, hc: hc, start: start}
	ctx := context.WithValue(r.Context(), ctxKey{}, &routed{call: c, rest: rest})
	t.proxy.ServeHTTP(w, hc.Request.WithContext(ctx))
}

// routed is stored on the request context for the ReverseProxy callbacks.
type routed struct {
	call *call
	rest string
}

func (p *Proxy) buildProxy(t *target) *httputil.ReverseProxy {
	strip := append(append([]string{}, defaultStrip...), p.cfg.StripHeaders...)
	if t.authName != "" {
		strip = append(strip, t.authName)
	}
	return &httputil.ReverseProxy{
		Transport:     newTransport(t.up.Timeout),
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			rt, _ := pr.In.Context().Value(ctxKey{}).(*routed)
			out := pr.Out
			u := *t.base
			u.Path = t.base.Path
			if rt != nil {
				u.Path += rt.rest
			}
			u.RawPath = ""
			u.RawQuery = pr.In.URL.RawQuery
			out.URL = &u
			out.Host = u.Host
			for _, h := range strip {
				out.Header.Del(h)
			}
			out.Header.Del("Accept-Encoding") // let the transport decompress so usage can be read
			for k, v := range t.up.Headers {
				out.Header.Set(k, v)
			}
			if t.authName != "" {
				out.Header.Set(t.authName, t.authVal)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			rt, _ := resp.Request.Context().Value(ctxKey{}).(*routed)
			h := resp.Header
			h.Del("Set-Cookie")
			h.Del("Www-Authenticate")
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				h.Del("Location")
			}
			if resp.StatusCode == http.StatusUnauthorized {
				logger.Warn("aiproxy: upstream %q rejected its credentials", t.up.Name)
				resp.Body.Close()
				body, _ := json.Marshal(errorBody("bad_gateway", "upstream rejected the proxy credentials"))
				resp.StatusCode, resp.Status = http.StatusBadGateway, "502 Bad Gateway"
				resp.Body = io.NopCloser(bytes.NewReader(body))
				resp.ContentLength = int64(len(body))
				h.Set("Content-Type", "application/json")
				h.Set("Content-Length", strconv.Itoa(len(body)))
			}
			if rt != nil {
				sse := strings.HasPrefix(h.Get("Content-Type"), "text/event-stream")
				resp.Body = newWatchBody(resp.Body, t.kind == KindOpenAI, sse, resp.StatusCode, rt.call)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if rt, _ := r.Context().Value(ctxKey{}).(*routed); rt != nil {
				rt.call.finish(http.StatusBadGateway, Usage{}, err)
			}
			if !errors.Is(err, context.Canceled) {
				logger.Warn("aiproxy: upstream %q error: %v", t.up.Name, err)
			}
			writeError(w, http.StatusBadGateway, "bad_gateway", "upstream request failed")
		},
	}
}

// inspect reads JSON request bodies to extract/enforce model and tools.
// It returns a non-zero status when the request must be rejected.
func (p *Proxy) inspect(t *target, r *http.Request, hc *HookContext) (int, string, string) {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return 0, "", ""
	}
	if r.Body == nil || r.Body == http.NoBody {
		return 0, "", ""
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	isJSON := mt == "application/json" || strings.HasSuffix(mt, "+json")

	if !isJSON {
		if t.kind == KindOpenAI && len(t.models) > 0 {
			return http.StatusBadRequest, "invalid_request_error", "model cannot be verified for this content type"
		}
		return 0, "", ""
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, p.cfg.MaxBodyBytes+1))
	r.Body.Close()
	if err != nil {
		return http.StatusBadRequest, "invalid_request_error", "could not read request body"
	}
	if int64(len(body)) > p.cfg.MaxBodyBytes {
		return http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large"
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))

	switch t.kind {
	case KindOpenAI:
		hc.Model = gjson.GetBytes(body, "model").String()
		if len(t.models) > 0 {
			if _, ok := t.models[hc.Model]; !ok {
				return http.StatusForbidden, "model_not_allowed", "model not allowed"
			}
		}
	case KindMCP:
		msgs := []gjson.Result{gjson.ParseBytes(body)}
		if msgs[0].IsArray() {
			msgs = msgs[0].Array()
		}
		for _, m := range msgs {
			if m.Get("method").String() != "tools/call" {
				continue
			}
			tool := m.Get("params.name").String()
			if len(t.tools) > 0 {
				if _, ok := t.tools[tool]; !ok {
					return http.StatusForbidden, "tool_not_allowed", "tool not allowed"
				}
			}
			hc.Tools = append(hc.Tools, tool)
		}
	}
	return 0, "", ""
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

func anyRole(roles []string, allowed map[string]struct{}) bool {
	for _, r := range roles {
		if _, ok := allowed[r]; ok {
			return true
		}
	}
	return false
}

func errorBody(typ, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"message": msg, "type": typ, "code": typ}}
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(typ, msg))
}
