package security

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func newTestOAuthServer(t *testing.T) (*OAuthServer, *DatabaseAuthenticator) {
	t.Helper()
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	srv := NewOAuthServer(OAuthServerConfig{Issuer: "https://auth.example.com", PersistCodes: true}, auth)
	t.Cleanup(srv.Close)
	return srv, auth
}

// s256Challenge computes the PKCE S256 code_challenge for a given verifier,
// matching validatePKCESHA256 in oauth_server.go.
func s256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body map[string]interface{}) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var reqBody *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = strings.NewReader(string(b))
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var parsed map[string]interface{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

func doForm(t *testing.T, mux http.Handler, path string, form url.Values, basicUser, basicPass string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var parsed map[string]interface{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

func doGet(mux http.Handler, path, bearer string) (*httptest.ResponseRecorder, map[string]interface{}) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var parsed map[string]interface{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

func TestOAuthServer_RegisterConfidentialClient_IssuesSecretOnce(t *testing.T) {
	srv, _ := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	rec, resp := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", rec.Code, rec.Body.String())
	}
	secret, _ := resp["client_secret"].(string)
	if secret == "" {
		t.Fatal("expected client_secret to be present in registration response")
	}
	if _, ok := resp["client_secret_hash"]; ok {
		t.Error("client_secret_hash must never be returned in the registration response")
	}
	if resp["token_endpoint_auth_method"] != "client_secret_basic" {
		t.Errorf("token_endpoint_auth_method = %v, want client_secret_basic", resp["token_endpoint_auth_method"])
	}
	if resp["client_id"] == "" || resp["client_id"] == nil {
		t.Fatal("expected non-empty client_id")
	}

	// Fetching the client back (e.g. via a later authorize/token call) must never leak the hash.
	clientID := resp["client_id"].(string)
	fetched, ok := srv.lookupOrFetchClient(context.Background(), clientID)
	if !ok {
		t.Fatal("expected client to be found")
	}
	if fetched.ClientSecretHash == "" {
		t.Error("expected ClientSecretHash to be stored internally")
	}
	if fetched.ClientSecretHash == secret {
		t.Error("stored hash must not equal the plaintext secret")
	}

	// Public registration (no client_credentials) should stay a public client with no secret.
	recPublic, respPublic := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{"https://app.example.com/callback"},
	})
	if recPublic.Code != http.StatusCreated {
		t.Fatalf("public register status = %d", recPublic.Code)
	}
	if _, ok := respPublic["client_secret"]; ok {
		t.Error("public client registration should not receive a client_secret")
	}
	if respPublic["token_endpoint_auth_method"] != "none" {
		t.Errorf("public client token_endpoint_auth_method = %v, want none", respPublic["token_endpoint_auth_method"])
	}
}

func TestOAuthServer_ClientCredentialsGrant(t *testing.T) {
	srv, _ := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	_, reg := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris":  []string{"https://app.example.com/callback"},
		"grant_types":    []string{"client_credentials"},
		"allowed_scopes": []string{"read", "write"},
	})
	clientID := reg["client_id"].(string)
	clientSecret := reg["client_secret"].(string)

	t.Run("valid credentials", func(t *testing.T) {
		rec, resp := doForm(t, mux, "/oauth/token", url.Values{
			"grant_type": {"client_credentials"},
			"scope":      {"read"},
		}, clientID, clientSecret)
		if rec.Code != http.StatusOK {
			t.Fatalf("token status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if resp["access_token"] == "" || resp["access_token"] == nil {
			t.Error("expected non-empty access_token")
		}
		if _, ok := resp["refresh_token"]; ok {
			t.Error("client_credentials must not issue a refresh_token (RFC 6749 §4.4.3)")
		}
		if resp["scope"] != "read" {
			t.Errorf("scope = %v, want read", resp["scope"])
		}
	})

	t.Run("scope not allowed for client", func(t *testing.T) {
		rec, resp := doForm(t, mux, "/oauth/token", url.Values{
			"grant_type": {"client_credentials"},
			"scope":      {"admin"},
		}, clientID, clientSecret)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if resp["error"] != "invalid_scope" {
			t.Errorf("error = %v, want invalid_scope", resp["error"])
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		rec, resp := doForm(t, mux, "/oauth/token", url.Values{
			"grant_type": {"client_credentials"},
		}, clientID, "wrong-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if resp["error"] != "invalid_client" {
			t.Errorf("error = %v, want invalid_client", resp["error"])
		}
	})

	t.Run("public client cannot use client_credentials", func(t *testing.T) {
		_, pubReg := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
			"redirect_uris": []string{"https://app.example.com/callback"},
		})
		pubClientID := pubReg["client_id"].(string)

		rec, _ := doForm(t, mux, "/oauth/token", url.Values{
			"grant_type": {"client_credentials"},
			"client_id":  {pubClientID},
		}, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for public client attempting client_credentials", rec.Code)
		}
	})
}

func TestOAuthServer_ConfidentialClient_AuthCodeGrantRequiresSecret(t *testing.T) {
	srv, auth := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	regResp, err := auth.Register(context.Background(), RegisterRequest{
		Username: "nadia", Password: "p", Email: "nadia@example.com",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, reg := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris":              []string{"https://app.example.com/callback"},
		"token_endpoint_auth_method": "client_secret_basic",
	})
	clientID := reg["client_id"].(string)
	clientSecret := reg["client_secret"].(string)

	verifier := "verifier-nadia-1234567890"
	challenge := s256Challenge(verifier)
	if err := auth.OAuthSaveCode(context.Background(), &OAuthCode{
		Code:          "code-no-auth",
		ClientID:      clientID,
		RedirectURI:   "https://app.example.com/callback",
		CodeChallenge: challenge,
		SessionToken:  regResp.Token,
		Scopes:        []string{"profile"},
		ExpiresAt:     futureTime(),
	}); err != nil {
		t.Fatalf("OAuthSaveCode() error = %v", err)
	}

	// No client credentials supplied -> must be rejected for a confidential client.
	rec, resp := doForm(t, mux, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code-no-auth"},
		"redirect_uri":  {"https://app.example.com/callback"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without client auth, body=%s", rec.Code, rec.Body.String())
	}
	if resp["error"] != "invalid_client" {
		t.Errorf("error = %v, want invalid_client", resp["error"])
	}

	// Same code, now with correct client credentials -> succeeds.
	if err := auth.OAuthSaveCode(context.Background(), &OAuthCode{
		Code:          "code-with-auth",
		ClientID:      clientID,
		RedirectURI:   "https://app.example.com/callback",
		CodeChallenge: challenge,
		SessionToken:  regResp.Token,
		Scopes:        []string{"profile"},
		ExpiresAt:     futureTime(),
	}); err != nil {
		t.Fatalf("OAuthSaveCode() error = %v", err)
	}
	rec2, _ := doForm(t, mux, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"code-with-auth"},
		"redirect_uri":  {"https://app.example.com/callback"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}, clientID, clientSecret)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with client auth, body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestOAuthServer_PublicClient_AuthCodeGrantUnaffected(t *testing.T) {
	srv, auth := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	regResp, err := auth.Register(context.Background(), RegisterRequest{
		Username: "oscar", Password: "p", Email: "oscar@example.com",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, reg := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{"https://app.example.com/callback"},
	})
	clientID := reg["client_id"].(string)
	if _, ok := reg["client_secret"]; ok {
		t.Fatal("expected no client_secret for a default (public) registration")
	}

	verifier := "verifier-oscar-1234567890"
	challenge := s256Challenge(verifier)
	if err := auth.OAuthSaveCode(context.Background(), &OAuthCode{
		Code:          "public-code",
		ClientID:      clientID,
		RedirectURI:   "https://app.example.com/callback",
		CodeChallenge: challenge,
		SessionToken:  regResp.Token,
		Scopes:        []string{"profile"},
		ExpiresAt:     futureTime(),
	}); err != nil {
		t.Fatalf("OAuthSaveCode() error = %v", err)
	}

	// No credentials needed for a public client — PKCE alone is sufficient, unchanged.
	rec, _ := doForm(t, mux, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"public-code"},
		"redirect_uri":  {"https://app.example.com/callback"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for public client without credentials, body=%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthServer_ProtectedResourceMetadata(t *testing.T) {
	srv, _ := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	rec, resp := doGet(mux, "/.well-known/oauth-protected-resource", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if resp["resource"] != "https://auth.example.com" {
		t.Errorf("resource = %v", resp["resource"])
	}
	servers, ok := resp["authorization_servers"].([]interface{})
	if !ok || len(servers) != 1 || servers[0] != "https://auth.example.com" {
		t.Errorf("authorization_servers = %v", resp["authorization_servers"])
	}
}

func TestOAuthServer_OIDCDiscoveryAndIDToken(t *testing.T) {
	srv, auth := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	// Discovery document.
	rec, disc := doGet(mux, "/.well-known/openid-configuration", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery status = %d", rec.Code)
	}
	if disc["jwks_uri"] != "https://auth.example.com/oauth/jwks.json" {
		t.Errorf("jwks_uri = %v", disc["jwks_uri"])
	}
	grantTypes, _ := disc["grant_types_supported"].([]interface{})
	found := false
	for _, g := range grantTypes {
		if g == "client_credentials" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected client_credentials in grant_types_supported, got %v", grantTypes)
	}

	// JWKS.
	rec, _ = doGet(mux, "/oauth/jwks.json", "")
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &jwks)
	if len(jwks.Keys) != 1 {
		t.Fatalf("expected 1 JWKS key, got %d", len(jwks.Keys))
	}

	// End-to-end authorization_code flow with scope=openid, verifying the id_token
	// signature against the published JWKS key.
	regResp, err := auth.Register(context.Background(), RegisterRequest{
		Username: "olivia", Password: "p", Email: "olivia@example.com",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, clientReg := doJSON(t, mux, http.MethodPost, "/oauth/register", map[string]interface{}{
		"redirect_uris": []string{"https://app.example.com/callback"},
	})
	clientID := clientReg["client_id"].(string)

	verifier := "test-code-verifier-1234567890"
	challenge := s256Challenge(verifier)
	if err := auth.OAuthSaveCode(context.Background(), &OAuthCode{
		Code:          "oidc-code",
		ClientID:      clientID,
		RedirectURI:   "https://app.example.com/callback",
		CodeChallenge: challenge,
		SessionToken:  regResp.Token,
		Scopes:        []string{"openid", "profile", "email"},
		ExpiresAt:     futureTime(),
	}); err != nil {
		t.Fatalf("OAuthSaveCode() error = %v", err)
	}

	tokRec, tokenResp := doForm(t, mux, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"oidc-code"},
		"redirect_uri":  {"https://app.example.com/callback"},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}, "", "")
	if tokRec.Code != http.StatusOK {
		t.Fatalf("token exchange status = %d, body = %s", tokRec.Code, tokRec.Body.String())
	}
	idTokenStr, _ := tokenResp["id_token"].(string)
	if idTokenStr == "" {
		t.Fatal("expected id_token in response for scope containing openid")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].N)
	if err != nil {
		t.Fatalf("decode n: %v", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].E)
	if err != nil {
		t.Fatalf("decode e: %v", err)
	}
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(new(big.Int).SetBytes(eBytes).Int64())}

	parsed, err := jwt.Parse(idTokenStr, func(token *jwt.Token) (interface{}, error) {
		return pub, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		t.Fatalf("id_token did not verify against JWKS key: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["iss"] != "https://auth.example.com" {
		t.Errorf("iss claim = %v", claims["iss"])
	}
	if claims["aud"] != clientID {
		t.Errorf("aud claim = %v, want %v", claims["aud"], clientID)
	}
	if claims["preferred_username"] != "olivia" {
		t.Errorf("preferred_username claim = %v", claims["preferred_username"])
	}
	if claims["email"] != "olivia@example.com" {
		t.Errorf("email claim = %v", claims["email"])
	}
}

func TestOAuthServer_Userinfo(t *testing.T) {
	srv, auth := newTestOAuthServer(t)
	mux := srv.HTTPHandler()

	regResp, err := auth.Register(context.Background(), RegisterRequest{
		Username: "pete", Password: "p", Email: "pete@example.com",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rec, info := doGet(mux, "/oauth/userinfo", regResp.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("userinfo status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if info["preferred_username"] != "pete" {
		t.Errorf("preferred_username = %v", info["preferred_username"])
	}

	rec2, _ := doGet(mux, "/oauth/userinfo", "not-a-real-token")
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for invalid token", rec2.Code)
	}
}
