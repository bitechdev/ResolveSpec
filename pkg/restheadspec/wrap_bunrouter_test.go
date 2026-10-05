package restheadspec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uptrace/bunrouter"
)

type wrapCtxKey struct{}

// The auth wrapper must hand the handler the middleware-enriched request
// without dropping the bunrouter route params.
func TestWrapBunRouterHandler_PreservesRouteParams(t *testing.T) {
	var gotSchema, gotEntity, gotID string
	var gotCtxVal any

	handler := func(w http.ResponseWriter, req bunrouter.Request) error {
		gotSchema = req.Param("schema")
		gotEntity = req.Param("entity")
		gotID = req.Param("id")
		gotCtxVal = req.Context().Value(wrapCtxKey{})
		return nil
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), wrapCtxKey{}, "enriched")))
		})
	}

	router := bunrouter.New()
	router.GET("/:schema/:entity/:id", wrapBunRouterHandler(handler, auth))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public/users/42", nil))

	if gotSchema != "public" || gotEntity != "users" || gotID != "42" {
		t.Errorf("route params lost: schema=%q entity=%q id=%q", gotSchema, gotEntity, gotID)
	}
	if gotCtxVal != "enriched" {
		t.Errorf("handler did not see middleware-enriched context, got %v", gotCtxVal)
	}
}

func TestWrapBunRouterHandler_NilAuthPassesThrough(t *testing.T) {
	var gotID string
	handler := func(w http.ResponseWriter, req bunrouter.Request) error {
		gotID = req.Param("id")
		return nil
	}

	router := bunrouter.New()
	router.GET("/:schema/:entity/:id", wrapBunRouterHandler(handler, nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/public/users/7", nil))

	if gotID != "7" {
		t.Errorf("id = %q, want 7", gotID)
	}
}
