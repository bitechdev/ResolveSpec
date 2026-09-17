package quickproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewService_Validation(t *testing.T) {
	tests := []struct {
		name    string
		rules   []Rule
		wantErr bool
	}{
		{"no rules", nil, true},
		{"empty rules", []Rule{}, true},
		{"bad prefix", []Rule{{URLPrefix: "api", Target: "http://localhost:1"}}, true},
		{"bad target", []Rule{{URLPrefix: "/api", Target: "not-a-url"}}, true},
		{"missing host", []Rule{{URLPrefix: "/api", Target: "http://"}}, true},
		{"duplicate prefix", []Rule{
			{URLPrefix: "/api", Target: "http://localhost:1"},
			{URLPrefix: "/api", Target: "http://localhost:2"},
		}, true},
		{"bad exclude prefix", []Rule{
			{URLPrefix: "/", Target: "http://localhost:1", Exclude: []string{"health"}},
		}, true},
		{"exclude outside rule's URLPrefix", []Rule{
			{URLPrefix: "/api", Target: "http://localhost:1", Exclude: []string{"/health"}},
		}, true},
		{"valid", []Rule{{URLPrefix: "/api", Target: "http://localhost:1"}}, false},
		{"valid with exclude", []Rule{
			{URLPrefix: "/", Target: "http://localhost:1", Exclude: []string{"/health"}},
		}, false},
		{"valid with nested exclude", []Rule{
			{URLPrefix: "/api", Target: "http://localhost:1", Exclude: []string{"/api/health"}},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(tt.rules)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewService() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func fallbackHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

func TestHandler_ProxiesSuccessResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream:" + r.URL.Path))
	}))
	defer upstream.Close()

	svc, err := NewService([]Rule{{URLPrefix: "/api", Target: upstream.URL}})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback"))

	req := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "upstream:/api/widgets" {
		t.Fatalf("body = %q", got)
	}
}

func TestHandler_404FallsBack(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("upstream not found"))
	}))
	defer upstream.Close()

	svc, err := NewService([]Rule{{URLPrefix: "/", Target: upstream.URL}})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback-content"))

	req := httptest.NewRequest(http.MethodGet, "/missing.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q, want fallback-content", got)
	}
}

func TestHandler_UnreachableUpstreamFallsBack(t *testing.T) {
	// A closed listener address: nothing is listening, so dialing fails.
	unreachable := "http://127.0.0.1:1"

	svc, err := NewService([]Rule{{URLPrefix: "/", Target: unreachable}}, WithTimeout(500*time.Millisecond))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback-content"))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q, want fallback-content", got)
	}
}

func TestHandler_NonNotFoundErrorsPassThrough(t *testing.T) {
	codes := []int{http.StatusOK, http.StatusForbidden, http.StatusBadRequest, http.StatusInternalServerError}

	for _, code := range codes {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("upstream response"))
		}))

		svc, err := NewService([]Rule{{URLPrefix: "/", Target: upstream.URL}})
		if err != nil {
			upstream.Close()
			t.Fatalf("NewService: %v", err)
		}

		handler := svc.Handler(fallbackHandler("fallback-content"))

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != code {
			t.Errorf("status for upstream code %d = %d, want %d", code, rr.Code, code)
		}
		if got := rr.Body.String(); got != "upstream response" {
			t.Errorf("body for upstream code %d = %q, want passthrough", code, got)
		}

		upstream.Close()
	}
}

func TestHandler_LongestPrefixMatch(t *testing.T) {
	specific := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("specific"))
	}))
	defer specific.Close()

	general := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("general"))
	}))
	defer general.Close()

	svc, err := NewService([]Rule{
		{URLPrefix: "/", Target: general.URL},
		{URLPrefix: "/api/v1", Target: specific.URL},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback"))

	for path, want := range map[string]string{
		"/api/v1/thing": "specific",
		"/api/other":    "general",
		"/anything":     "general",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if got := rr.Body.String(); got != want {
			t.Errorf("path %s: body = %q, want %q", path, got, want)
		}
	}
}

func TestHandler_ExcludeFallsBackToFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream:" + r.URL.Path))
	}))
	defer upstream.Close()

	svc, err := NewService([]Rule{
		{URLPrefix: "/", Target: upstream.URL, Exclude: []string{"/health"}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback-content"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q, want fallback-content", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q, want fallback-content", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/other", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Body.String(); got != "upstream:/other" {
		t.Fatalf("body = %q, want upstream:/other", got)
	}
}

func TestHandler_ExcludeFallsThroughToNextRule(t *testing.T) {
	specific := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("specific"))
	}))
	defer specific.Close()

	general := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("general"))
	}))
	defer general.Close()

	svc, err := NewService([]Rule{
		{URLPrefix: "/api", Target: general.URL},
		{URLPrefix: "/api/v1", Target: specific.URL, Exclude: []string{"/api/v1/health"}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback"))

	for path, want := range map[string]string{
		"/api/v1/thing":  "specific",
		"/api/v1/health": "general",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if got := rr.Body.String(); got != want {
			t.Errorf("path %s: body = %q, want %q", path, got, want)
		}
	}
}

func TestHandler_NoMatchFallsBack(t *testing.T) {
	svc, err := NewService([]Rule{{URLPrefix: "/api", Target: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback-content"))

	req := httptest.NewRequest(http.MethodGet, "/other", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "fallback-content" {
		t.Fatalf("body = %q, want fallback-content", got)
	}
}

func TestHandler_AllMethodsProxied(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Method + ":" + string(body)))
	}))
	defer upstream.Close()

	svc, err := NewService([]Rule{{URLPrefix: "/api", Target: upstream.URL}})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler := svc.Handler(fallbackHandler("fallback"))

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/widgets", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		want := method + ":"
		if got := rr.Body.String(); got != want {
			t.Errorf("method %s: body = %q, want %q", method, got, want)
		}
	}
}
