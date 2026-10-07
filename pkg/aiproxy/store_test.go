package aiproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memStore struct {
	mu   sync.Mutex
	defs []Definition
	err  error
}

func (m *memStore) List(context.Context) ([]Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Definition(nil), m.defs...), m.err
}

func def(name, url, key string) Definition {
	return Definition{Kind: KindOpenAI, Upstream: Upstream{Name: name, BaseURL: url + "/v1", APIKey: key}}
}

func TestReloadAddChangeRemove(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Header.Get("Authorization"))
	}))
	defer up.Close()
	p := New(Config{Prefix: "/ai", Audit: NopAuditSink{}})
	require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "code", BaseURL: up.URL + "/v1", APIKey: "c"}}))
	front := httptest.NewServer(p.Handler(testAuth))
	defer front.Close()
	get := func(name string) (int, string) {
		resp, body := do(t, "GET", front.URL+"/ai/"+name+"/v1/models", "", "bob:u", nil)
		return resp.StatusCode, body
	}

	st := &memStore{defs: []Definition{def("a", up.URL, "k1"), def("code", up.URL, "stolen")}}
	err := p.Reload(context.Background(), st)
	assert.ErrorContains(t, err, "conflicts with a code-registered")
	code, body := get("a")
	assert.Equal(t, 200, code)
	assert.Equal(t, "Bearer k1", body)
	_, body = get("code")
	assert.Equal(t, "Bearer c", body) // code-registered untouched

	st.defs = []Definition{def("a", up.URL, "k2"), def("b", up.URL, "kb")}
	require.NoError(t, p.Reload(context.Background(), st))
	_, body = get("a")
	assert.Equal(t, "Bearer k2", body)
	_, body = get("b")
	assert.Equal(t, "Bearer kb", body)

	st.defs = []Definition{def("b", up.URL, "kb")}
	require.NoError(t, p.Reload(context.Background(), st))
	code, _ = get("a")
	assert.Equal(t, 404, code)
	code, _ = get("code")
	assert.Equal(t, 200, code)

	// store failure changes nothing; invalid def keeps last good one
	st.err = errors.New("db down")
	assert.Error(t, p.Reload(context.Background(), st))
	code, _ = get("b")
	assert.Equal(t, 200, code)
	st.err = nil
	st.defs = []Definition{{Kind: KindOpenAI, Upstream: Upstream{Name: "b", BaseURL: "ftp://x"}}}
	assert.Error(t, p.Reload(context.Background(), st))
	_, body = get("b")
	assert.Equal(t, "Bearer kb", body)

	assert.True(t, p.Unregister("code"))
	assert.False(t, p.Unregister("code"))
}

func TestReloadKeepsRateLimitStateWhenUnchanged(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	p := New(Config{Prefix: "/ai", Audit: NopAuditSink{}})
	d := def("a", up.URL, "")
	d.Upstream.RateLimit = &RateLimit{PerSecond: 0.01, Burst: 1}
	st := &memStore{defs: []Definition{d}}
	require.NoError(t, p.Reload(context.Background(), st))
	front := httptest.NewServer(p.Handler(testAuth))
	defer front.Close()

	resp, _ := do(t, "GET", front.URL+"/ai/a/v1/x", "", "bob:u", nil)
	assert.Equal(t, 200, resp.StatusCode)
	require.NoError(t, p.Reload(context.Background(), st))
	resp, _ = do(t, "GET", front.URL+"/ai/a/v1/x", "", "bob:u", nil)
	assert.Equal(t, 429, resp.StatusCode)
}

func TestAutoReload(t *testing.T) {
	p := New(Config{Audit: NopAuditSink{}})
	st := &memStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.AutoReload(ctx, st, 20*time.Millisecond))
	assert.Empty(t, p.Names())
	st.mu.Lock()
	st.defs = []Definition{def("a", "http://x", "")}
	st.mu.Unlock()
	assert.Eventually(t, func() bool { return len(p.Names()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Error(t, p.AutoReload(ctx, st, 0))
}

func procRows(data string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(true, nil, []byte(data))
}

func TestProcStore(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	_, err = NewProcStore(db, "x; drop table y")
	assert.Error(t, err)
	_, err = NewProcStore(nil, "")
	assert.Error(t, err)
	s, err := NewProcStore(db, "")
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT p_success, p_error, p_data FROM resolvespec_ai_proxies\(\)`).WillReturnRows(procRows(`[
		{"name":"gpt","kind":"openai","base_url":"https://api.openai.com/v1","api_key":"sk","auth_header":"api-key",
		 "auth_format":"%s","headers":{"X-A":"1"},"allowed_roles":["ai"],"allowed":["m1","m2"],
		 "rate_per_second":2.5,"rate_burst":5,"timeout_seconds":30},
		{"name":"tools","kind":"mcp","base_url":"http://localhost:3000/mcp","allowed":["t1"],"enabled":true},
		{"name":"off","kind":"mcp","base_url":"http://localhost:3001/mcp","enabled":false},
		{"name":5}
	]`))
	defs, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, defs, 2)

	gpt := defs[0]
	assert.Equal(t, KindOpenAI, gpt.Kind)
	assert.Equal(t, "sk", gpt.Upstream.APIKey)
	assert.Equal(t, "api-key", gpt.Upstream.AuthHeader)
	assert.Equal(t, map[string]string{"X-A": "1"}, gpt.Upstream.Headers)
	assert.Equal(t, []string{"ai"}, gpt.Upstream.AllowedRoles)
	assert.Equal(t, []string{"m1", "m2"}, gpt.Allowed)
	assert.Equal(t, &RateLimit{PerSecond: 2.5, Burst: 5}, gpt.Upstream.RateLimit)
	assert.Equal(t, 30*time.Second, gpt.Upstream.Timeout)
	assert.Equal(t, KindMCP, defs[1].Kind)
	assert.Nil(t, defs[1].Upstream.RateLimit)

	// feeds Reload
	mock.ExpectQuery(`FROM resolvespec_ai_proxies`).WillReturnRows(procRows(`[{"name":"a","kind":"mcp","base_url":"http://x/mcp"},{"name":"bad","kind":"nope","base_url":"http://x"}]`))
	p := New(Config{Audit: NopAuditSink{}})
	assert.Error(t, p.Reload(context.Background(), s)) // "bad" reported
	assert.Equal(t, []string{"a"}, p.Names())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProcStoreFailures(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()
	s, err := NewProcStore(db, "myschema.my_proxies")
	require.NoError(t, err)

	mock.ExpectQuery(`FROM myschema.my_proxies\(\)`).WillReturnRows(
		sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(false, "boom", []byte(`[]`)))
	_, err = s.List(context.Background())
	assert.EqualError(t, err, "boom")

	mock.ExpectQuery(`FROM myschema.my_proxies`).WillReturnRows(procRows(`{not json`))
	_, err = s.List(context.Background())
	assert.ErrorContains(t, err, "parse AI proxies")

	mock.ExpectQuery(`FROM myschema.my_proxies`).WillReturnError(errors.New("conn lost"))
	_, err = s.List(context.Background())
	assert.ErrorContains(t, err, "conn lost")

	mock.ExpectQuery(`FROM myschema.my_proxies`).WillReturnRows(
		sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(true, nil, nil))
	defs, err := s.List(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, defs)
}

type auditCollector struct {
	mu   sync.Mutex
	recs []AuditRecord
}

func (a *auditCollector) Record(r AuditRecord) {
	a.mu.Lock()
	a.recs = append(a.recs, r)
	a.mu.Unlock()
}

type metricsCollector struct {
	metrics.Provider
	mu    sync.Mutex
	calls []string
	toks  int64
}

func (m *metricsCollector) RecordAIProxy(up, kind, model, class, outcome string, d time.Duration, pt, ct int64) {
	m.mu.Lock()
	m.calls = append(m.calls, up+"|"+kind+"|"+model+"|"+class+"|"+outcome)
	m.toks += pt + ct
	m.mu.Unlock()
}

func TestAuditAndMetrics(t *testing.T) {
	old := metrics.GetProvider()
	mc := &metricsCollector{Provider: old}
	metrics.SetProvider(mc)
	defer metrics.SetProvider(old)

	ac := &auditCollector{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	}))
	defer up.Close()
	p := New(Config{Prefix: "/ai", Audit: ac})
	require.NoError(t, p.RegisterOpenAI(OpenAIUpstream{Upstream: Upstream{Name: "g", BaseURL: up.URL + "/v1", APIKey: "sk-secret", AllowedRoles: []string{"ai"}}, AllowedModels: []string{"m"}}))
	front := httptest.NewServer(p.Handler(testAuth))
	defer front.Close()

	do(t, "POST", front.URL+"/ai/g/v1/chat/completions?k=v", `{"model":"m"}`, "bob:ai", nil)
	do(t, "POST", front.URL+"/ai/g/v1/chat/completions", `{"model":"zzz"}`, "bob:ai", nil)
	do(t, "GET", front.URL+"/ai/g/v1/models", "", "bob:user", nil)

	require.Eventually(t, func() bool { ac.mu.Lock(); defer ac.mu.Unlock(); return len(ac.recs) == 3 }, 2*time.Second, 10*time.Millisecond)
	ac.mu.Lock()
	r := ac.recs
	ac.mu.Unlock()
	assert.Equal(t, OutcomeOK, r[0].Outcome)
	assert.Equal(t, "bob", r[0].User)
	assert.Equal(t, "/ai/g/v1/chat/completions", r[0].Path) // no query
	assert.Equal(t, "m", r[0].Model)
	assert.Equal(t, int64(3), r[0].Usage.TotalTokens)
	assert.Equal(t, OutcomeDenied, r[1].Outcome)
	assert.Equal(t, 403, r[1].Status)
	assert.Equal(t, OutcomeDenied, r[2].Outcome)
	for _, rec := range r {
		assert.NotContains(t, rec.Reason+rec.Error+rec.Path, "sk-secret")
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()
	assert.Equal(t, []string{"g|openai|m|2xx|ok", "g|openai||4xx|denied", "g|openai||4xx|denied"}, mc.calls)
	assert.Equal(t, int64(3), mc.toks)
}

func TestModelLabelsAreBounded(t *testing.T) {
	var m modelLabels
	for i := 0; i < maxModelLabels+50; i++ {
		m.label("u", "model-"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26)))
	}
	assert.Equal(t, "other", m.label("u", "brand-new"))
	assert.Equal(t, "", m.label("u", ""))
}
