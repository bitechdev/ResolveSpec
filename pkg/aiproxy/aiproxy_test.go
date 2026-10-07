package aiproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuth authenticates by the X-Test-User header: "name:role1,role2". Empty means no user.
func testAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("X-Test-User")
		if h == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		name, roles, _ := strings.Cut(h, ":")
		uc := &security.UserContext{UserID: len(name), UserName: name, Roles: strings.Split(roles, ",")}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), security.UserContextKey, uc)))
	})
}

type captured struct {
	mu   sync.Mutex
	req  *http.Request
	body string
}

func (c *captured) set(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.req, c.body = r.Clone(context.Background()), string(b)
	c.mu.Unlock()
}

func newProxy(t *testing.T, upstream http.Handler, mod func(*Proxy, string)) (*Proxy, *httptest.Server) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	p := New(Config{Prefix: "/ai"})
	mod(p, up.URL)
	front := httptest.NewServer(p.Handler(testAuth))
	t.Cleanup(front.Close)
	return p, front
}

func do(t *testing.T, method, url, body, user string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestOpenAIKeyInjectionAndHiding(t *testing.T) {
	var c captured
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.set(r)
		w.Header().Set("Set-Cookie", "s=1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}), func(p *Proxy, u string) {
		require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "gpt", BaseURL: u + "/v1", APIKey: "sk-secret"}}))
	})

	resp, body := do(t, "POST", front.URL+"/ai/gpt/v1/chat/completions?x=1", `{"model":"m"}`, "bob:user",
		map[string]string{"Authorization": "Bearer client-token", "Cookie": "a=b"})
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, `{"ok":true}`, body)
	assert.Empty(t, resp.Header.Get("Set-Cookie"))
	assert.Equal(t, "/v1/chat/completions", c.req.URL.Path)
	assert.Equal(t, "x=1", c.req.URL.RawQuery)
	assert.Equal(t, "Bearer sk-secret", c.req.Header.Get("Authorization"))
	assert.Empty(t, c.req.Header.Get("Cookie"))
	assert.Equal(t, `{"model":"m"}`, c.body)
	assert.NotContains(t, body, "sk-secret")
}

func TestCustomAuthHeaderAndFormat(t *testing.T) {
	var c captured
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { c.set(r); io.WriteString(w, "{}") }),
		func(p *Proxy, u string) {
			require.NoError(t, p.RegisterMCP(MCPUpstream{Upstream: Upstream{Name: "a", BaseURL: u + "/mcp", APIKey: "k1", AuthHeader: "X-Token", Headers: map[string]string{"X-Tenant": "t1"}}}))
			require.NoError(t, p.RegisterMCP(MCPUpstream{Upstream: Upstream{Name: "b", BaseURL: u + "/mcp", APIKey: "k2", AuthFormat: "Token %s"}}))
		})

	do(t, "POST", front.URL+"/ai/a", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "bob:user", map[string]string{"X-Token": "client"})
	assert.Equal(t, "k1", c.req.Header.Get("X-Token"))
	assert.Equal(t, "t1", c.req.Header.Get("X-Tenant"))
	assert.Equal(t, "/mcp", c.req.URL.Path)

	do(t, "POST", front.URL+"/ai/b", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "bob:user", nil)
	assert.Equal(t, "Token k2", c.req.Header.Get("Authorization"))
}

func TestAuthAndRoles(t *testing.T) {
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") }),
		func(p *Proxy, u string) {
			require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "gpt", BaseURL: u + "/v1", AllowedRoles: []string{"ai"}}}))
		})
	url := front.URL + "/ai/gpt/v1/models"

	resp, _ := do(t, "GET", url, "", "", nil)
	assert.Equal(t, 401, resp.StatusCode)
	resp, _ = do(t, "GET", url, "", "bob:user", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = do(t, "GET", url, "", "bob:guest", nil)
	assert.Equal(t, 401, resp.StatusCode)
	resp, _ = do(t, "GET", url, "", "bob:user,ai", nil)
	assert.Equal(t, 200, resp.StatusCode)

	resp, _ = do(t, "GET", front.URL+"/ai/nope/v1/models", "", "bob:ai", nil)
	assert.Equal(t, 404, resp.StatusCode)
	resp, _ = do(t, "GET", front.URL+"/ai/gpt/other", "", "bob:ai", nil)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestNilAuthRefuses(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Config{}).Handler(nil).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, 500, rec.Code)
}

func TestModelAllowlist(t *testing.T) {
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") }),
		func(p *Proxy, u string) {
			require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "gpt", BaseURL: u + "/v1"}, AllowedModels: []string{"good"}}))
		})
	url := front.URL + "/ai/gpt/v1/chat/completions"
	resp, _ := do(t, "POST", url, `{"model":"good"}`, "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	resp, _ = do(t, "POST", url, `{"model":"bad"}`, "bob:u", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = do(t, "POST", url, `{}`, "bob:u", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = do(t, "POST", url, `x`, "bob:u", map[string]string{"Content-Type": "text/plain"})
	assert.Equal(t, 400, resp.StatusCode)
	resp, _ = do(t, "GET", front.URL+"/ai/gpt/v1/models", "", "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestBodyTooLarge(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	p := New(Config{Prefix: "/ai", MaxBodyBytes: 10})
	require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: up.URL + "/v1"}}))
	front := httptest.NewServer(p.Handler(testAuth))
	defer front.Close()
	resp, _ := do(t, "POST", front.URL+"/ai/g/v1/x", `{"model":"aaaaaaaaaaaaaaaa"}`, "bob:u", nil)
	assert.Equal(t, 413, resp.StatusCode)
}

func TestMCPToolAllowlist(t *testing.T) {
	var c captured
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.set(r)
		w.Header().Set("Mcp-Session-Id", "sess1")
		io.WriteString(w, "{}")
	}), func(p *Proxy, u string) {
		require.NoError(t, p.RegisterMCP(MCPUpstream{Upstream: Upstream{Name: "t", BaseURL: u + "/mcp"}, AllowedTools: []string{"ok"}}))
	})
	url := front.URL + "/ai/t"
	call := func(name string) string {
		return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `"}}`
	}

	resp, _ := do(t, "POST", url, call("ok"), "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "sess1", resp.Header.Get("Mcp-Session-Id"))
	resp, _ = do(t, "POST", url, call("evil"), "bob:u", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = do(t, "POST", url, `[`+call("ok")+`,`+call("evil")+`]`, "bob:u", nil)
	assert.Equal(t, 403, resp.StatusCode)
	resp, _ = do(t, "POST", url, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	resp, _ = do(t, "DELETE", url, "", "bob:u", map[string]string{"Mcp-Session-Id": "sess1"})
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "sess1", c.req.Header.Get("Mcp-Session-Id"))
}

func TestRateLimit(t *testing.T) {
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") }),
		func(p *Proxy, u string) {
			require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: u + "/v1", RateLimit: &RateLimit{PerSecond: 0.1, Burst: 2}}}))
		})
	url := front.URL + "/ai/g/v1/models"
	for i := 0; i < 2; i++ {
		resp, _ := do(t, "GET", url, "", "bob:u", nil)
		assert.Equal(t, 200, resp.StatusCode)
	}
	resp, _ := do(t, "GET", url, "", "bob:u", nil)
	assert.Equal(t, 429, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Retry-After"))
	// other user has own bucket
	resp, _ = do(t, "GET", url, "", "alice:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestHooksAndUsage(t *testing.T) {
	var p *Proxy
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-From-Hook") == "" {
			t.Error("hook header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}), func(pp *Proxy, u string) {
		p = pp
		require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: u + "/v1"}}))
	})
	done := make(chan *HookContext, 1)
	p.Hooks().Register(BeforeProxy, func(h *HookContext) error {
		h.Request.Header.Set("X-From-Hook", "1")
		if h.Model == "blocked" {
			h.Abort, h.AbortMessage, h.AbortCode = true, "nope", 418
		}
		return nil
	})
	p.Hooks().Register(AfterProxy, func(h *HookContext) error { done <- h; return nil })

	resp, _ := do(t, "POST", front.URL+"/ai/g/v1/chat/completions", `{"model":"blocked"}`, "bob:u", nil)
	assert.Equal(t, 418, resp.StatusCode)

	resp, _ = do(t, "POST", front.URL+"/ai/g/v1/chat/completions", `{"model":"m"}`, "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	select {
	case h := <-done:
		assert.Equal(t, "m", h.Model)
		assert.Equal(t, "bob", h.UserContext.UserName)
		assert.Equal(t, 200, h.StatusCode)
		assert.Equal(t, Usage{3, 4, 7}, h.Usage)
	case <-time.After(2 * time.Second):
		t.Fatal("AfterProxy not called")
	}
}

func TestSSEUsageAndStreaming(t *testing.T) {
	var p *Proxy
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		io.WriteString(w, "data: {\"choices\":[{}]}\n\n")
		f.Flush()
		io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\ndata: [DONE]\n\n")
		f.Flush()
	}), func(pp *Proxy, u string) {
		p = pp
		require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: u + "/v1"}}))
	})
	done := make(chan *HookContext, 1)
	p.Hooks().Register(AfterProxy, func(h *HookContext) error { done <- h; return nil })

	resp, body := do(t, "POST", front.URL+"/ai/g/v1/chat/completions", `{"model":"m","stream":true}`, "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, body, "[DONE]")
	h := <-done
	assert.Equal(t, Usage{1, 2, 3}, h.Usage)
}

func TestResponsesAPIUsage(t *testing.T) {
	u, ok := parseUsage([]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":6}}}`))
	assert.True(t, ok)
	assert.Equal(t, Usage{5, 6, 11}, u)
}

func TestUpstreamUnauthorizedIsMasked(t *testing.T) {
	_, front := newProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", "Bearer realm=x")
		w.WriteHeader(401)
	}), func(p *Proxy, u string) {
		require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: u + "/v1", APIKey: "bad"}}))
	})
	resp, body := do(t, "GET", front.URL+"/ai/g/v1/models", "", "bob:u", nil)
	assert.Equal(t, 502, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Www-Authenticate"))
	assert.Contains(t, body, "bad_gateway")
}

func TestUpstreamDownHidesURL(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	url := up.URL
	up.Close()
	p := New(Config{Prefix: "/ai"})
	require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: url + "/v1"}}))
	front := httptest.NewServer(p.Handler(testAuth))
	defer front.Close()
	resp, body := do(t, "GET", front.URL+"/ai/g/v1/models", "", "bob:u", nil)
	assert.Equal(t, 502, resp.StatusCode)
	assert.NotContains(t, body, url)
}

func TestRegisterValidationAndRedaction(t *testing.T) {
	p := New(Config{})
	assert.Error(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "bad name", BaseURL: "http://x"}}))
	assert.Error(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "a", BaseURL: "ftp://x"}}))
	assert.Error(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "a", BaseURL: "http://x", AuthFormat: "%d"}}))
	assert.Error(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "a", BaseURL: "http://x", RateLimit: &RateLimit{}}}))
	require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "a", BaseURL: "http://x", APIKey: "sk-secret"}}))
	assert.Error(t, p.RegisterMCP(MCPUpstream{Upstream: Upstream{Name: "a", BaseURL: "http://x"}}))

	u := Upstream{Name: "a", BaseURL: "http://x", APIKey: "sk-secret"}
	js, _ := json.Marshal(u)
	for _, s := range []string{u.String(), string(js), strings.TrimSpace(errors.New(u.String()).Error())} {
		assert.NotContains(t, s, "sk-secret")
	}
}
