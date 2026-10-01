package security

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var (
	reFormAction = regexp.MustCompile(`<form method="POST" action="([^"]*)"`)
	reHidden     = regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`)
)

// browserSubmit posts the first form of an HTML page with extra fields.
func browserSubmit(t *testing.T, c *http.Client, base, page string, extra url.Values) *http.Response {
	t.Helper()
	m := reFormAction.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no form in page: %s", page)
	}
	form := url.Values{}
	for _, h := range reHidden.FindAllStringSubmatch(page, -1) {
		form.Set(h[1], html.UnescapeString(h[2]))
	}
	for k, v := range extra {
		form[k] = v
	}
	action, _ := url.Parse(html.UnescapeString(m[1]))
	b, _ := url.Parse(base)
	resp, err := c.PostForm(b.ResolveReference(action).String(), form)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func bodyOf(t *testing.T, r *http.Response) string {
	t.Helper()
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

// TestOIDCClient_FullLoop runs the relying party against our own authorization server:
// discovery, PKCE, nonce, login, consent, code exchange, id_token validation and logout URL.
func TestOIDCClient_FullLoop(t *testing.T) {
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer ts.Close()

	idpAuth := NewDatabaseAuthenticatorWithOptions(newDirectTestDB(t), DatabaseAuthenticatorOptions{Lookup: directConfig})
	srv := NewOAuthServer(OAuthServerConfig{Issuer: ts.URL, PersistCodes: true, RequireConsent: true}, idpAuth)
	defer srv.Close()
	handler = srv.HTTPHandler()

	if _, err := idpAuth.Register(context.Background(), RegisterRequest{Username: "olivia", Password: "pw", Email: "olivia@example.com"}); err != nil {
		t.Fatal(err)
	}
	_, reg := doJSON(t, handler, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{"https://rp.example.com/callback"},
	})
	clientID := reg["client_id"].(string)

	rp := NewDatabaseAuthenticatorWithOptions(newDirectTestDB(t), DatabaseAuthenticatorOptions{Lookup: directConfig})
	if _, err := rp.WithOIDC(context.Background(), OIDCConfig{
		Issuer: ts.URL, ClientID: clientID, RedirectURL: "https://rp.example.com/callback", ProviderName: "idp",
	}); err != nil {
		t.Fatal(err)
	}

	authURL, err := rp.OAuth2GetAuthURL("idp", "state-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"code_challenge=", "code_challenge_method=S256", "nonce=", "state=state-1"} {
		if !strings.Contains(authURL, want) {
			t.Fatalf("auth URL lacks %q: %s", want, authURL)
		}
	}

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := browser.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	page := bodyOf(t, resp)
	resp = browserSubmit(t, browser, authURL, page, url.Values{"username": {"olivia"}, "password": {"pw"}})
	page = bodyOf(t, resp)
	if resp.StatusCode == http.StatusFound {
		t.Fatalf("expected the consent page, got redirect to %s", resp.Header.Get("Location"))
	}
	resp = browserSubmit(t, browser, authURL, page, url.Values{"decision": {"allow"}, "remember": {"1"}})
	loc := resp.Header.Get("Location")
	bodyOf(t, resp)
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("expected redirect to the RP, got %q (status %d)", loc, resp.StatusCode)
	}
	cb, _ := url.Parse(loc)
	if cb.Query().Get("iss") != ts.URL {
		t.Errorf("iss = %q", cb.Query().Get("iss"))
	}

	// A wrong iss (mix-up) is refused, and a replayed state is gone afterwards.
	bad := httptest.NewRequest(http.MethodGet, "/cb?code=x&state=state-1&iss=https://evil.example", nil)
	if _, err := rp.OAuth2HandleCallbackRequest(context.Background(), "idp", bad); err == nil {
		t.Fatal("mismatching iss must fail")
	}
	authURL, _ = rp.OAuth2GetAuthURL("idp", "state-1")
	_ = authURL

	// Redo the login for a fresh state/nonce (the failed attempt consumed the first state).
	authURL, _ = rp.OAuth2GetAuthURL("idp", "state-2")
	resp, _ = browser.Get(authURL)
	loc = resp.Header.Get("Location")
	bodyOf(t, resp)
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("SSO + remembered consent should redirect straight to the RP, got %q", loc)
	}
	cbReq := httptest.NewRequest(http.MethodGet, loc, nil)
	login, err := rp.OAuth2HandleCallbackRequest(context.Background(), "idp", cbReq)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if login.User == nil || login.User.Email != "olivia@example.com" {
		t.Fatalf("unexpected user: %+v", login.User)
	}
	idToken, _ := login.Meta["id_token"].(string)
	if idToken == "" {
		t.Fatal("id_token missing in Meta")
	}

	// Replaying the callback fails: the state is single use.
	if _, err := rp.OAuth2HandleCallbackRequest(context.Background(), "idp", cbReq); err == nil {
		t.Fatal("replayed callback must fail")
	}

	out, err := rp.OAuth2LogoutURL(context.Background(), "idp", idToken, "https://rp.example.com/bye", "s")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, ts.URL+"/oauth/logout?") || !strings.Contains(out, "id_token_hint=") {
		t.Errorf("logout URL = %s", out)
	}
}

func TestOIDCClient_RejectsBadIDToken(t *testing.T) {
	p := newOIDCProvider(&OAuth2Config{Issuer: "https://idp.example", ClientID: "c", JWKSURL: "http://127.0.0.1:1/jwks", AuthURL: "a", TokenURL: "t"})
	if _, err := p.validateIDToken(context.Background(), "not.a.jwt", "n", ""); err == nil {
		t.Fatal("garbage id_token must fail")
	}
}
