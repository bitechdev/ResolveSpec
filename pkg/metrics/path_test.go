package metrics

import (
	"context"
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"":                    "/",
		"/":                   "/",
		"/users":              "/users",
		"/users/123":          "/users/:id",
		"/users/123/orders/9": "/users/:id/orders/:id",
		"/x/550e8400-e29b-41d4-a716-446655440000": "/x/:id",
		"/x/507f1f77bcf86cd799439011":             "/x/:id",
		"/api/public/users":                       "/api/public/users",
		"/files/report.v2":                        "/files/report.v2",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteLabel(t *testing.T) {
	r := httptest.NewRequest("GET", "/users/42", nil)
	if got := routeLabel(r, nil); got != "/users/:id" {
		t.Errorf("fallback = %q", got)
	}
	r.Pattern = "GET /users/{id}"
	if got := routeLabel(r, nil); got != "/users/{id}" {
		t.Errorf("pattern = %q", got)
	}
	got := routeLabel(r, func(*http.Request) string { return "/custom" })
	if got != "/custom" {
		t.Errorf("custom = %q", got)
	}
}

func TestPathLimiter(t *testing.T) {
	l := newPathLimiter(2)
	for _, p := range []string{"/a", "/b", "/a"} {
		if got := l.label(p); got != p {
			t.Errorf("label(%q) = %q", p, got)
		}
	}
	if got := l.label("/c"); got != overflowPathLabel {
		t.Errorf("overflow = %q", got)
	}
	if got := newPathLimiter(-1).label("/z"); got != "/z" {
		t.Errorf("disabled = %q", got)
	}
}

func TestMiddlewareUsesPattern(t *testing.T) {
	p := NewPrometheusProvider(&Config{Enabled: true, Namespace: "pathtest"})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {})
	h := p.Middleware(mux)
	for _, id := range []string{"1", "2", "abc"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/users/"+id, nil))
	}
	if n := len(p.pathLimiter.seen); n != 1 {
		t.Errorf("distinct paths = %d, want 1", n)
	}
	if _, ok := p.pathLimiter.seen["/users/{id}"]; !ok {
		t.Errorf("seen = %v", p.pathLimiter.seen)
	}
}

func TestResetAndHandler(t *testing.T) {
	p := NewPrometheusProvider(&Config{Enabled: true, Namespace: "resettest"})
	p.RecordHTTPRequest("GET", "/a/1", "200", 0)
	p.RecordDBQuery("SELECT", "s", "e", "t", 0, nil)
	p.IncRequestsInFlight()

	count := func() int {
		mfs, _ := prometheus.DefaultGatherer.Gather()
		n := 0
		for _, mf := range mfs {
			if strings.HasPrefix(mf.GetName(), "resettest_") && mf.GetName() != "resettest_http_requests_in_flight" && mf.GetName() != "resettest_event_queue_size" {
				n += len(mf.GetMetric())
			}
		}
		return n
	}
	if count() == 0 {
		t.Fatal("expected recorded series")
	}

	rec := httptest.NewRecorder()
	p.ResetHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/reset", nil))
	if rec.Code != http.StatusMethodNotAllowed || count() == 0 {
		t.Fatalf("GET should be rejected, code=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	p.ResetHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/reset", nil))
	if rec.Code != http.StatusNoContent || count() != 0 {
		t.Fatalf("reset failed, code=%d series=%d", rec.Code, count())
	}
	if len(p.pathLimiter.seen) != 0 {
		t.Error("path limiter not reset")
	}

	// push=true without a pushgateway must fail and not be silent
	rec = httptest.NewRecorder()
	p.ResetHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/reset?push=true", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("push without gateway code=%d", rec.Code)
	}
}

func TestPushAndResetKeepsStatsOnFailure(t *testing.T) {
	p := NewPrometheusProvider(&Config{Enabled: true, Namespace: "pushfail", PushgatewayURL: "http://127.0.0.1:1"})
	p.RecordHTTPRequest("GET", "/a", "200", 0)
	if err := p.PushAndReset(); err == nil {
		t.Fatal("expected push error")
	}
	if len(p.pathLimiter.seen) != 1 {
		t.Error("stats were reset despite failed push")
	}
}

func TestPushToEndpoint(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		var gotCT, gotAuth string
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			gotCT, gotAuth = r.Header.Get("Content-Type"), r.Header.Get("Authorization")
			gotBody, _ = io.ReadAll(r.Body)
		}))

		ns := "ep" + format
		p := NewPrometheusProvider(&Config{
			Enabled:                    true,
			Namespace:                  ns,
			PushEndpointURL:            srv.URL,
			PushEndpointFormat:         format,
			PushEndpointHeaders:        map[string]string{"Authorization": "Bearer x"},
			PushEndpointResetOnSuccess: true,
		})
		p.RecordHTTPRequest("GET", "/a", "200", time.Millisecond)

		if err := p.PushToEndpoint(context.Background()); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		srv.Close()
		if gotAuth != "Bearer x" || !strings.Contains(string(gotBody), ns+"_http_requests_total") {
			t.Errorf("%s: auth=%q body=%.200s", format, gotAuth, gotBody)
		}
		if format == "json" && gotCT != "application/json" || format == "text" && !strings.HasPrefix(gotCT, "text/plain") {
			t.Errorf("%s: content-type %q", format, gotCT)
		}
		if len(p.pathLimiter.seen) != 0 {
			t.Errorf("%s: stats not reset after success", format)
		}
	}
}

func TestPushToEndpointFailureKeepsStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := NewPrometheusProvider(&Config{Enabled: true, Namespace: "epfail", PushEndpointURL: srv.URL, PushEndpointResetOnSuccess: true})
	p.RecordHTTPRequest("GET", "/a", "200", 0)
	if err := p.PushToEndpoint(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
	if len(p.pathLimiter.seen) != 1 {
		t.Error("stats reset despite failure")
	}
	if err := NewPrometheusProvider(&Config{Enabled: true, Namespace: "epnone"}).PushToEndpoint(context.Background()); err == nil {
		t.Error("expected error without endpoint")
	}
}

func TestDisabledProvider(t *testing.T) {
	p := NewPrometheusProvider(&Config{Namespace: "disabled", PushEndpointURL: "http://127.0.0.1:1", PushEndpointInterval: 1})
	p.RecordHTTPRequest("GET", "/a", "200", 0)
	if len(p.pathLimiter.seen) != 0 {
		t.Error("disabled provider recorded")
	}
	for name, h := range map[string]http.Handler{"handler": p.Handler(), "json": p.JSONHandler()} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/m", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s code=%d", name, rec.Code)
		}
	}
	if p.endpoint != nil || p.PushToEndpoint(context.Background()) == nil || p.Push() == nil {
		t.Error("disabled provider must not push")
	}
}

func TestJSONHandler(t *testing.T) {
	p := NewPrometheusProvider(&Config{Enabled: true, Namespace: "jsonpull"})
	p.RecordHTTPRequest("GET", "/a", "200", time.Millisecond)

	rec := httptest.NewRecorder()
	p.JSONHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/m", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var fams []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fams); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range fams {
		if f["name"] == "jsonpull_http_requests_total" {
			found = true
		}
	}
	if !found {
		t.Error("metric family missing from JSON")
	}

	rec = httptest.NewRecorder()
	p.JSONHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/m", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code=%d", rec.Code)
	}
}
