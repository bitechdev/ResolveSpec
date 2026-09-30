package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func setup(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tr := tp.Tracer("test")
	tracer.Store(&tr)
	t.Cleanup(func() { tracer.Store(nil) })
	return sr
}

func attrs(s sdktrace.ReadOnlySpan) map[string]string {
	m := map[string]string{}
	for _, kv := range s.Attributes() {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

func TestMiddlewareRedactsQueryAndUsesRoute(t *testing.T) {
	sr := setup(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	h := Middleware(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/42?token=secret", nil))

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	s := spans[0]
	if s.Name() != "GET GET /api/{id}" {
		t.Errorf("name = %q", s.Name())
	}
	for k, v := range attrs(s) {
		if v == "secret" || k == "http.url" || k == "url.full" || k == "url.query" {
			t.Errorf("leaked %s=%s", k, v)
		}
	}
	if a := attrs(s); a["http.response.status_code"] != "500" {
		t.Errorf("status attr = %q", a["http.response.status_code"])
	}
	if s.Status().Code.String() != "Error" {
		t.Errorf("status = %v", s.Status().Code)
	}
}

func TestMiddlewareUnmatchedRoute(t *testing.T) {
	sr := setup(t)
	h := Middleware(http.NotFoundHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/random/7f3c", nil))
	if n := sr.Ended()[0].Name(); n != "GET <unmatched>" {
		t.Errorf("name = %q", n)
	}
}

func TestMiddlewareRecordsPanicAndRepanics(t *testing.T) {
	sr := setup(t)
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	}()
	s := sr.Ended()[0]
	if s.Status().Code.String() != "Error" || len(s.Events()) == 0 {
		t.Errorf("panic not recorded: %v %d", s.Status(), len(s.Events()))
	}
	var _ trace.Span
}

func TestInitTwiceErrors(t *testing.T) {
	cfg := Config{Enabled: true, ServiceName: "t", Endpoint: "localhost:4317", Insecure: true}
	shutdown, err := InitTracer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InitTracer(cfg); err == nil {
		t.Error("expected error on second init")
	}
	_ = shutdown(t.Context())
	if _, err := InitTracer(Config{Enabled: true, SampleRate: 2}); err == nil {
		t.Error("expected invalid sample rate error")
	}
}
