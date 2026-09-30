package dbtrace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackerCounts(t *testing.T) {
	ctx, tr := Start(context.Background())
	Query(ctx) // pooled
	end := TxBegin(ctx)
	Query(ctx)
	Query(ctx)
	end()
	Query(ctx) // pooled again
	Raw(ctx, "auth.session")
	Raw(ctx, "auth.session")

	want := "tx=1 tx_queries=2 pooled=2 raw=2 [auth.session=2]"
	if got := tr.Summary(); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if tr.total() != 5 {
		t.Fatalf("total = %d, want 5", tr.total())
	}
}

func TestHelpersNoopWithoutTracker(t *testing.T) {
	ctx := context.Background()
	Query(ctx)
	Raw(ctx, "x")
	TxBegin(ctx)()
	if From(ctx) != nil {
		t.Fatal("unexpected tracker")
	}
}

func TestMiddlewareDisabledAddsNoTracker(t *testing.T) {
	Configure(Options{Enabled: false})
	var seen *Tracker
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = From(r.Context()) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if seen != nil {
		t.Fatal("tracker present while disabled")
	}
}

func TestMiddlewareEnabledTracks(t *testing.T) {
	Configure(Options{Enabled: true, MinCalls: 2})
	defer Configure(Options{})
	var summary string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Raw(r.Context(), "a")
		Query(r.Context())
		summary = From(r.Context()).Summary()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !strings.Contains(summary, "pooled=1 raw=1") {
		t.Fatalf("summary = %q", summary)
	}
}
