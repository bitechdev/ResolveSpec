package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const flowRedirect = "https://rp.example.com/callback"

type flowEnv struct {
	t        *testing.T
	ts       *httptest.Server
	srv      *OAuthServer
	auth     *DatabaseAuthenticator
	browser  *http.Client
	clientID string
}

// newFlowEnv starts a real HTTP server with the given config, one user (olivia/pw) and one
// registered public client.
func newFlowEnv(t *testing.T, cfg OAuthServerConfig) *flowEnv {
	t.Helper()
	var handler http.Handler
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(ts.Close)
	auth := NewDatabaseAuthenticatorWithOptions(newDirectTestDB(t), DatabaseAuthenticatorOptions{Lookup: directConfig})
	cfg.Issuer = ts.URL
	cfg.PersistCodes = true
	srv := NewOAuthServer(cfg, auth)
	t.Cleanup(srv.Close)
	handler = srv.HTTPHandler()
	if _, err := auth.Register(context.Background(), RegisterRequest{Username: "olivia", Password: "pw", Email: "olivia@example.com"}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	e := &flowEnv{t: t, ts: ts, srv: srv, auth: auth,
		browser: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	_, reg := doJSON(t, handler, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{flowRedirect},
		"grant_types":   []string{"authorization_code", "refresh_token"},
	})
	e.clientID, _ = reg["client_id"].(string)
	if e.clientID == "" {
		t.Fatalf("registration failed: %v", reg)
	}
	return e
}

func (e *flowEnv) authURL(extra url.Values) (string, string) {
	verifier := "verifier-0123456789-0123456789-0123456789-abcdef"
	q := url.Values{
		"response_type": {"code"}, "client_id": {e.clientID}, "redirect_uri": {flowRedirect},
		"code_challenge": {s256Challenge(verifier)}, "code_challenge_method": {"S256"},
		"scope": {"openid profile"}, "state": {"st"},
	}
	for k, v := range extra {
		q[k] = v
	}
	return e.ts.URL + "/oauth/authorize?" + q.Encode(), verifier
}

// login drives the browser through authorize until it is redirected to the RP and returns that URL.
func (e *flowEnv) login(authURL string, decision string) *url.URL {
	e.t.Helper()
	resp, err := e.browser.Get(authURL)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if loc := resp.Header.Get("Location"); loc != "" {
			bodyOf(e.t, resp)
			u, _ := url.Parse(loc)
			if !u.IsAbs() {
				u = resp.Request.URL.ResolveReference(u)
			}
			if u.Host == "rp.example.com" {
				return u
			}
			if resp, err = e.browser.Get(u.String()); err != nil {
				e.t.Fatal(err)
			}
			continue
		}
		page := bodyOf(e.t, resp)
		switch {
		case strings.Contains(page, `name="password"`):
			resp = browserSubmit(e.t, e.browser, authURL, page, url.Values{"username": {"olivia"}, "password": {"pw"}})
		case strings.Contains(page, `name="decision"`):
			resp = browserSubmit(e.t, e.browser, authURL, page, url.Values{"decision": {decision}})
		default:
			e.t.Fatalf("unexpected page (status %d): %s", resp.StatusCode, page)
		}
	}
	e.t.Fatal("too many steps")
	return nil
}

func (e *flowEnv) post(path string, form url.Values) (*httptest.ResponseRecorder, map[string]interface{}) {
	return doForm(e.t, e.srv.HTTPHandler(), path, form, "", "")
}

// tokens runs the full code flow and returns the token response.
func (e *flowEnv) tokens(extra url.Values) map[string]interface{} {
	e.t.Helper()
	authURL, verifier := e.authURL(extra)
	cb := e.login(authURL, "allow")
	code := cb.Query().Get("code")
	if code == "" {
		e.t.Fatalf("no code in %s", cb)
	}
	rec, tok := e.post("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {flowRedirect},
		"client_id": {e.clientID}, "code_verifier": {verifier},
	})
	if rec.Code != http.StatusOK {
		e.t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	return tok
}

func TestOAuthFlow_ConsentDenyAndScopeFilteredUserinfo(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{RequireConsent: true})

	authURL, _ := e.authURL(nil)
	cb := e.login(authURL, "deny")
	if cb.Query().Get("error") != "access_denied" || cb.Query().Get("state") != "st" || cb.Query().Get("iss") != e.ts.URL {
		t.Errorf("deny redirect = %s", cb)
	}

	tok := e.tokens(url.Values{"scope": {"openid"}})
	rec, info := doGet(e.srv.HTTPHandler(), "/oauth/userinfo", tok["access_token"].(string))
	if rec.Code != http.StatusOK {
		t.Fatalf("userinfo %d %s", rec.Code, rec.Body.String())
	}
	if info["sub"] == nil {
		t.Error("sub missing")
	}
	if _, ok := info["email"]; ok {
		t.Errorf("email must not be released without the email scope: %v", info)
	}
}

func TestOAuthFlow_PromptNoneWithoutSession(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{})
	authURL, _ := e.authURL(url.Values{"prompt": {"none"}})
	cb := e.login(authURL, "allow")
	if cb.Query().Get("error") != "login_required" {
		t.Errorf("want login_required, got %s", cb)
	}
}

func TestOAuthFlow_RefreshRotationAndReuseDetection(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{ManagedRefreshTokens: true})
	tok := e.tokens(nil)
	r1, _ := tok["refresh_token"].(string)
	if r1 == "" {
		t.Fatalf("no refresh token: %v", tok)
	}
	refresh := func(rt string) (int, map[string]interface{}) {
		rec, body := e.post("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {e.clientID}})
		return rec.Code, body
	}
	code, t2 := refresh(r1)
	if code != http.StatusOK {
		t.Fatalf("refresh: %d %v", code, t2)
	}
	r2, _ := t2["refresh_token"].(string)
	if r2 == "" || r2 == r1 {
		t.Fatalf("refresh token must rotate: %q -> %q", r1, r2)
	}
	// Presenting the used token again is theft: it fails and kills the family.
	if code, body := refresh(r1); code != http.StatusBadRequest || body["error"] != "invalid_grant" {
		t.Fatalf("reuse must fail with invalid_grant: %d %v", code, body)
	}
	if code, _ := refresh(r2); code == http.StatusOK {
		t.Fatal("the family must be revoked after reuse")
	}
}

func TestOAuthFlow_JWTAccessTokenVerify(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{JWTAccessTokens: true, AccessTokenAudience: "https://api.example.com"})
	tok := e.tokens(nil)
	at := tok["access_token"].(string)
	if strings.Count(at, ".") != 2 {
		t.Fatalf("access token is not a JWT: %s", at)
	}
	claims, err := e.srv.VerifyAccessToken(context.Background(), at, VerifyAccessTokenOptions{Audience: "https://api.example.com", Scopes: []string{"openid"}})
	if err != nil {
		t.Fatal(err)
	}
	if !claims.JWT || claims.ClientID != e.clientID {
		t.Errorf("claims = %+v", claims)
	}
	if _, err := e.srv.VerifyAccessToken(context.Background(), at, VerifyAccessTokenOptions{Audience: "https://other"}); err == nil {
		t.Error("wrong audience must fail")
	}
	if _, err := e.srv.VerifyAccessToken(context.Background(), at, VerifyAccessTokenOptions{Scopes: []string{"admin"}}); err == nil {
		t.Error("missing scope must fail")
	}
}

func TestOAuthFlow_IntrospectionRequiresClientAuth(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{})
	tok := e.tokens(nil)
	rec, _ := e.post("/oauth/introspect", url.Values{"token": {tok["access_token"].(string)}})
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusBadRequest {
		t.Errorf("anonymous introspection must be refused, got %d", rec.Code)
	}
}

func TestOAuthFlow_PAR(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{EnablePAR: true})
	_, verifier := e.authURL(nil)
	rec, par := e.post("/oauth/par", url.Values{
		"response_type": {"code"}, "client_id": {e.clientID}, "redirect_uri": {flowRedirect},
		"code_challenge": {s256Challenge(verifier)}, "code_challenge_method": {"S256"}, "scope": {"openid"}, "state": {"par-state"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("par: %d %s", rec.Code, rec.Body.String())
	}
	uri, _ := par["request_uri"].(string)
	if !strings.HasPrefix(uri, "urn:ietf:params:oauth:request_uri:") {
		t.Fatalf("request_uri = %q", uri)
	}
	cb := e.login(e.ts.URL+"/oauth/authorize?"+url.Values{"client_id": {e.clientID}, "request_uri": {uri}}.Encode(), "allow")
	if cb.Query().Get("code") == "" || cb.Query().Get("state") != "par-state" {
		t.Fatalf("callback = %s", cb)
	}
	// A request_uri is single use.
	resp, _ := e.browser.Get(e.ts.URL + "/oauth/authorize?" + url.Values{"client_id": {e.clientID}, "request_uri": {uri}}.Encode())
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "code=") {
		t.Errorf("request_uri was reusable: %s", loc)
	}
	bodyOf(t, resp)
}

func TestOAuthFlow_DeviceGrant(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{EnableDeviceFlow: true, DevicePollSeconds: 1})
	// the device client
	_, reg := doJSON(t, e.srv.HTTPHandler(), http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris":              []string{flowRedirect},
		"grant_types":                []string{"urn:ietf:params:oauth:grant-type:device_code"},
		"token_endpoint_auth_method": "none",
	})
	cid, _ := reg["client_id"].(string)
	rec, dev := e.post("/oauth/device_authorization", url.Values{"client_id": {cid}, "scope": {"openid"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("device_authorization: %d %s", rec.Code, rec.Body.String())
	}
	poll := func() (int, map[string]interface{}) {
		r, b := e.post("/oauth/token", url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {dev["device_code"].(string)}, "client_id": {cid},
		})
		return r.Code, b
	}
	if code, b := poll(); code != http.StatusBadRequest || b["error"] != "authorization_pending" {
		t.Fatalf("first poll: %d %v", code, b)
	}
	if _, b := poll(); b["error"] != "slow_down" {
		t.Errorf("polling too fast must give slow_down, got %v", b)
	}

	// The user approves in the browser.
	resp, _ := e.browser.Get(e.ts.URL + "/oauth/device?user_code=" + url.QueryEscape(dev["user_code"].(string)))
	page := bodyOf(t, resp)
	for i := 0; i < 4 && !strings.Contains(page, "Approve") && !strings.Contains(page, `value="allow"`); i++ {
		switch {
		case strings.Contains(page, `name="password"`):
			resp = browserSubmit(t, e.browser, e.ts.URL+"/oauth/device", page, url.Values{"username": {"olivia"}, "password": {"pw"}})
		case strings.Contains(page, `name="user_code"`):
			resp = browserSubmit(t, e.browser, e.ts.URL+"/oauth/device", page, url.Values{"user_code": {dev["user_code"].(string)}})
		default:
			t.Fatalf("unexpected device page: %s", page)
		}
		page = bodyOf(t, resp)
	}
	if strings.Contains(page, `value="allow"`) {
		resp = browserSubmit(t, e.browser, e.ts.URL+"/oauth/device", page, url.Values{"decision": {"allow"}})
		bodyOf(t, resp)
	}
	time.Sleep(1100 * time.Millisecond) // past the polling interval
	if code, b := poll(); code != http.StatusOK || b["access_token"] == nil {
		t.Fatalf("poll after approval: %d %v", code, b)
	}
	if code, b := poll(); code == http.StatusOK {
		t.Errorf("device code must be single use: %v", b)
	}
}

func TestOAuthFlow_Discovery(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{EnablePAR: true, EnableDeviceFlow: true, EnableDPoP: true})
	rec, doc := doGet(e.srv.HTTPHandler(), "/.well-known/openid-configuration", "")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	for _, k := range []string{"end_session_endpoint", "device_authorization_endpoint", "pushed_authorization_request_endpoint",
		"dpop_signing_alg_values_supported", "authorization_response_iss_parameter_supported", "prompt_values_supported", "response_modes_supported"} {
		if doc[k] == nil {
			t.Errorf("discovery lacks %s", k)
		}
	}
	if methods, _ := doc["code_challenge_methods_supported"].([]interface{}); len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v", methods)
	}
	// The issuer has no path, so a path-inserted form names another issuer.
	if rec, _ := doGet(e.srv.HTTPHandler(), "/.well-known/oauth-authorization-server/tenant1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("metadata for a foreign issuer path = %d, want 404", rec.Code)
	}
}

func TestOAuthServer_DiscoveryPathInsertion(t *testing.T) {
	auth := NewDatabaseAuthenticatorWithOptions(newDirectTestDB(t), DatabaseAuthenticatorOptions{Lookup: directConfig})
	srv := NewOAuthServer(OAuthServerConfig{Issuer: "https://auth.example.com/tenant1"}, auth)
	defer srv.Close()
	h := srv.HTTPHandler()
	for _, p := range []string{
		"/.well-known/oauth-authorization-server/tenant1", // RFC 8414 path insertion
		"/.well-known/openid-configuration/tenant1",
		"/tenant1/.well-known/openid-configuration", // OIDC Discovery appending
	} {
		rec, doc := doGet(h, p, "")
		if rec.Code != http.StatusOK || doc["issuer"] != "https://auth.example.com/tenant1" {
			t.Errorf("%s: %d %v", p, rec.Code, doc["issuer"])
		}
	}
}

func TestOAuthFlow_RegistrationManagement(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{})
	h := e.srv.HTTPHandler()
	_, reg := doJSON(t, h, http.MethodPost, "/oauth/register", map[string]interface{}{"redirect_uris": []string{flowRedirect}, "client_name": "App"})
	id, _ := reg["client_id"].(string)
	rat, _ := reg["registration_access_token"].(string)
	if rat == "" {
		t.Fatalf("no registration_access_token: %v", reg)
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/register/"+id, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("management without token = %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/oauth/register/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+rat)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got["client_name"] != "App" {
		t.Fatalf("management read: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/oauth/register/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+rat)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
}

func TestOAuthFlow_Logout(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{})
	tok := e.tokens(nil)
	idt, _ := tok["id_token"].(string)
	if idt == "" {
		t.Fatal("no id_token")
	}
	resp, err := e.browser.Get(e.ts.URL + "/oauth/logout?" + url.Values{"id_token_hint": {idt}, "client_id": {e.clientID}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	page := bodyOf(t, resp)
	if resp.StatusCode == http.StatusOK && strings.Contains(page, `name="confirm"`) {
		resp = browserSubmit(t, e.browser, e.ts.URL+"/oauth/logout", page, url.Values{"confirm": {"yes"}})
		bodyOf(t, resp)
	}
	// SSO is gone: prompt=none now reports login_required.
	authURL, _ := e.authURL(url.Values{"prompt": {"none"}})
	if cb := e.login(authURL, "allow"); cb.Query().Get("error") != "login_required" {
		t.Errorf("after logout want login_required, got %s", cb)
	}
}

func makeDPoP(t *testing.T, key *ecdsa.PrivateKey, method, target, accessToken, jti string) string {
	t.Helper()
	jwk, err := jwkFromPublic(&key.PublicKey, "", "ES256")
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{"htm": method, "htu": target, "iat": time.Now().Unix(), "jti": jti}
	if accessToken != "" {
		h := sha256.Sum256([]byte(accessToken))
		claims["ath"] = base64.RawURLEncoding.EncodeToString(h[:])
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["typ"] = "dpop+jwt"
	tok.Header["jwk"] = jwk
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOAuthFlow_DPoP(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{EnableDPoP: true})
	h := e.srv.HTTPHandler()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	authURL, verifier := e.authURL(nil)
	code := e.login(authURL, "allow").Query().Get("code")
	tokenURL := e.ts.URL + "/oauth/token"
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {flowRedirect},
		"client_id": {e.clientID}, "code_verifier": {verifier}}

	// A proof for another URL is refused (and does not burn the code).
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", makeDPoP(t, key, "POST", e.ts.URL+"/other", "", "j1"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_dpop_proof") {
		t.Fatalf("wrong htu: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", makeDPoP(t, key, "POST", tokenURL, "", "j2"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var tok map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok["token_type"] != "DPoP" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	at := tok["access_token"].(string)

	userinfo := func(scheme, proof string) int {
		req := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
		req.Header.Set("Authorization", scheme+" "+at)
		if proof != "" {
			req.Header.Set("DPoP", proof)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := userinfo("Bearer", ""); c != http.StatusUnauthorized {
		t.Errorf("a DPoP-bound token must not work as a Bearer token, got %d", c)
	}
	if c := userinfo("DPoP", makeDPoP(t, key, "GET", e.ts.URL+"/oauth/userinfo", at, "j3")); c != http.StatusOK {
		t.Errorf("valid DPoP userinfo = %d", c)
	}
	if c := userinfo("DPoP", makeDPoP(t, key, "GET", e.ts.URL+"/oauth/userinfo", at, "j3")); c == http.StatusOK {
		t.Error("a replayed proof (same jti) must be refused")
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if c := userinfo("DPoP", makeDPoP(t, other, "GET", e.ts.URL+"/oauth/userinfo", at, "j4")); c == http.StatusOK {
		t.Error("a proof from another key must be refused")
	}
}

func TestOAuthFlow_TokenExchange(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{EnableTokenExchange: true})
	h := e.srv.HTTPHandler()
	_, reg := doJSON(t, h, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris":              []string{flowRedirect},
		"grant_types":                []string{"urn:ietf:params:oauth:grant-type:token-exchange"},
		"token_endpoint_auth_method": "client_secret_basic",
	})
	cid, secret := reg["client_id"].(string), reg["client_secret"].(string)
	user := e.tokens(url.Values{"scope": {"openid profile"}})

	rec, out := doForm(t, h, "/oauth/token", url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {user["access_token"].(string)}, "subject_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
		"scope": {"openid"},
	}, cid, secret)
	if rec.Code != http.StatusOK || out["access_token"] == nil || out["issued_token_type"] != "urn:ietf:params:oauth:token-type:access_token" {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	if out["scope"] != "openid" {
		t.Errorf("scope must be downscoped, got %v", out["scope"])
	}
	// Widening the scope is refused.
	rec, _ = doForm(t, h, "/oauth/token", url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {out["access_token"].(string)}, "scope": {"openid email"},
	}, cid, secret)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("scope widening = %d", rec.Code)
	}
	// The public client of the user cannot exchange.
	rec, _ = e.post("/oauth/token", url.Values{
		"grant_type":    {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {user["access_token"].(string)}, "client_id": {e.clientID},
	})
	if rec.Code == http.StatusOK {
		t.Error("public clients must not use token exchange")
	}
}

func TestOAuthFlow_PrivateKeyJWTClientAuth(t *testing.T) {
	e := newFlowEnv(t, OAuthServerConfig{})
	h := e.srv.HTTPHandler()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jwk, err := jwkFromPublic(&key.PublicKey, "k1", "ES256")
	if err != nil {
		t.Fatal(err)
	}
	rec, reg := doJSON(t, h, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris":              []string{flowRedirect},
		"grant_types":                []string{"client_credentials"},
		"token_endpoint_auth_method": "private_key_jwt",
		"jwks":                       map[string]interface{}{"keys": []interface{}{jwk}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	cid := reg["client_id"].(string)
	assertion := func(jti, aud string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"iss": cid, "sub": cid, "aud": aud, "jti": jti, "exp": time.Now().Add(time.Minute).Unix(),
		})
		tok.Header["kid"] = "k1"
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call := func(a string) int {
		rec, _ := doForm(t, h, "/oauth/token", url.Values{
			"grant_type": {"client_credentials"}, "client_id": {cid},
			"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {a},
		}, "", "")
		return rec.Code
	}
	good := assertion("a1", e.ts.URL+"/oauth/token")
	if c := call(good); c != http.StatusOK {
		t.Fatalf("valid assertion = %d", c)
	}
	if c := call(good); c == http.StatusOK {
		t.Error("a replayed assertion must be refused")
	}
	if c := call(assertion("a2", "https://elsewhere.example/token")); c == http.StatusOK {
		t.Error("an assertion for another audience must be refused")
	}
}
