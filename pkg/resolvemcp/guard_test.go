package resolvemcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/security"
	"github.com/bitechdev/ResolveSpec/pkg/security/providers"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// tokenAuth accepts the bearer token "good" and nothing else.
type tokenAuth struct{ security.Authenticator }

func (tokenAuth) Authenticate(r *http.Request) (*security.UserContext, error) {
	if r.Header.Get("Authorization") != "Bearer good" {
		return nil, errors.New("bad credentials")
	}
	return &security.UserContext{UserID: 7, UserName: "kim"}, nil
}

func newTestSecurityList(t *testing.T) *security.SecurityList {
	t.Helper()
	p, err := security.NewCompositeSecurityProvider(tokenAuth{},
		providers.NewConfigColumnSecurityProvider(map[string][]sectypes.ColumnSecurity{}),
		providers.NewConfigRowSecurityProvider(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	sl, err := security.NewSecurityList(p)
	if err != nil {
		t.Fatal(err)
	}
	return sl
}

func serve(h http.Handler, auth string, mark func(*http.Request) *http.Request) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	if mark != nil {
		r = mark(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGuardRejectsUnauthenticated(t *testing.T) {
	var gotUser int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uc, ok := security.GetUserContext(r.Context())
		if !ok {
			t.Error("user context missing downstream")
			return
		}
		gotUser = uc.UserID
	})
	g := Guard(newTestSecurityList(t))(next)

	for name, auth := range map[string]string{"none": "", "wrong": "Bearer bad"} {
		if w := serve(g, auth, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, w.Code)
		}
	}
	if w := serve(g, "Bearer good", nil); w.Code != http.StatusOK || gotUser != 7 {
		t.Errorf("good: status %d user %d, want 200 / 7", w.Code, gotUser)
	}
}

// Skip/optional markers on the request context must not open the MCP endpoint.
func TestGuardIgnoresSkipAndOptionalMarkers(t *testing.T) {
	called := false
	g := Guard(newTestSecurityList(t))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for name, mark := range map[string]func(*http.Request) *http.Request{
		"skip":     func(r *http.Request) *http.Request { return r.WithContext(security.SkipAuth(r.Context())) },
		"optional": func(r *http.Request) *http.Request { return r.WithContext(security.OptionalAuth(r.Context())) },
	} {
		if w := serve(g, "", mark); w.Code != http.StatusUnauthorized || called {
			t.Errorf("%s: status %d called=%v, want 401 and not called", name, w.Code, called)
		}
	}
}

func TestGuardFailsClosedWithoutProvider(t *testing.T) {
	called := false
	g := Guard(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	if w := serve(g, "Bearer good", nil); w.Code != http.StatusInternalServerError || called {
		t.Errorf("status %d called=%v, want 500 and not called", w.Code, called)
	}
	if requireGuard("test", nil) {
		t.Error("requireGuard(nil) must be false")
	}
}
