package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// authzRequest is a validated authorization request. It is carried through the login and consent
// forms as a sealed (HMAC-protected) value, so the server stays stateless between the steps.
type authzRequest struct {
	ClientID      string         `json:"cid"`
	RedirectURI   string         `json:"ru"`
	State         string         `json:"st,omitempty"`
	Nonce         string         `json:"n,omitempty"`
	CodeChallenge string         `json:"cc"`
	ResponseMode  string         `json:"rm,omitempty"`
	Scopes        []string       `json:"sc,omitempty"`
	Prompt        []string       `json:"pr,omitempty"`
	MaxAge        int            `json:"ma"` // -1 = not requested
	IDTokenHint   string         `json:"ith,omitempty"`
	LoginHint     string         `json:"lh,omitempty"`
	ACRValues     []string       `json:"acr,omitempty"`
	Claims        map[string]any `json:"cl,omitempty"`
	Resource      []string       `json:"res,omitempty"`
	Provider      string         `json:"pv,omitempty"`
	DPoPJKT       string         `json:"dj,omitempty"`
	ViaPAR        bool           `json:"par,omitempty"`

	LoginDone   bool        `json:"ld,omitempty"`
	ConsentDone bool        `json:"cd,omitempty"`
	Bind        string      `json:"b,omitempty"`  // sid the consent form is bound to
	Tx          string      `json:"tx,omitempty"` // browser binding (login CSRF)
	Sess        *ssoSession `json:"ss,omitempty"` // only when the SSO cookie is disabled
}

func (r *authzRequest) hasPrompt(p string) bool { return oauthSliceContains(r.Prompt, p) }

var pkceChallengeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

const txCookie = "resolvespec_oauth_tx"

// --------------------------------------------------------------------------
// Authorization endpoint — GET + POST /oauth/authorize
// --------------------------------------------------------------------------

func (s *OAuthServer) authorizeHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.authorizeGet(w, r)
	case http.MethodPost:
		s.authorizePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// authzDirectError answers an error that must not be redirected (the client or redirect_uri is
// not trusted): JSON for API callers, a page for browsers.
func (s *OAuthServer) authzDirectError(w http.ResponseWriter, r *http.Request, code, desc string) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		s.renderMessage(w, http.StatusBadRequest, "Authorization error", desc, true)
		return
	}
	writeOAuthError(w, code, desc, http.StatusBadRequest)
}

func (s *OAuthServer) authorizeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	viaPAR := false
	if uri := q.Get("request_uri"); uri != "" {
		pushed, e := s.resolvePushedRequest(r.Context(), q.Get("client_id"), uri)
		if e != nil {
			s.authzDirectError(w, r, e.Code, e.Desc)
			return
		}
		q, viaPAR = pushed, true
	}
	req, fail := s.parseAuthz(r.Context(), q, viaPAR)
	if fail != nil {
		fail.respond(s, w, r)
		return
	}
	s.continueAuthorize(w, r, req)
}

// authorizePost handles the login form, the consent form and, for compatibility, the legacy
// login form that repeated the request parameters as hidden fields.
func (s *OAuthServer) authorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var req *authzRequest
	if blob := r.PostFormValue("req"); blob != "" {
		req = &authzRequest{}
		if err := s.open("authz", blob, req); err != nil {
			s.renderMessage(w, http.StatusBadRequest, "Request expired", "This sign-in request has expired. Please go back to the application and start again.", true)
			return
		}
		if !s.txMatches(r, req) {
			s.renderMessage(w, http.StatusBadRequest, "Request rejected", "The browser session of this request does not match. Please start again.", true)
			return
		}
	} else {
		form := url.Values{}
		for k, v := range r.PostForm {
			form[k] = v
		}
		if form.Get("state") == "" {
			form.Set("state", form.Get("client_state"))
		}
		if form.Get("response_type") == "" {
			form.Set("response_type", "code")
		}
		var fail *authzFailure
		if req, fail = s.parseAuthz(r.Context(), form, false); fail != nil {
			fail.respond(s, w, r)
			return
		}
	}
	if r.PostFormValue("decision") != "" {
		s.consentSubmit(w, r, req)
		return
	}
	s.loginSubmit(w, r, req)
}

// --------------------------------------------------------------------------
// Parsing and validation
// --------------------------------------------------------------------------

// authzFailure is an invalid authorization request. When req is set the client and redirect_uri
// are trusted and the error goes back to the client; otherwise it is shown directly.
type authzFailure struct {
	req  *authzRequest
	code string
	desc string
}

func (f *authzFailure) respond(s *OAuthServer, w http.ResponseWriter, r *http.Request) {
	if f.req == nil {
		s.authzDirectError(w, r, f.code, f.desc)
		return
	}
	s.authzRedirectError(w, r, f.req, f.code, f.desc)
}

// redirectURIMatches compares exactly, except that a registered http loopback URI matches any port
// (RFC 8252 §7.3).
func redirectURIMatches(registered []string, uri string) bool {
	if oauthSliceContains(registered, uri) {
		return true
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
		return false
	}
	for _, reg := range registered {
		ru, err := url.Parse(reg)
		if err != nil || ru.Scheme != "http" || !isLoopbackHost(ru.Hostname()) {
			continue
		}
		if ru.Hostname() == u.Hostname() && ru.Path == u.Path && ru.RawQuery == u.RawQuery {
			return true
		}
	}
	return false
}

func isLoopbackHost(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

func (s *OAuthServer) parseAuthz(ctx context.Context, q url.Values, viaPAR bool) (*authzRequest, *authzFailure) {
	client, ok := s.lookupOrFetchClient(ctx, q.Get("client_id"))
	if !ok {
		return nil, &authzFailure{code: "invalid_client", desc: "unknown client_id"}
	}
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" && len(client.RedirectURIs) == 1 {
		redirectURI = client.RedirectURIs[0]
	}
	if !redirectURIMatches(client.RedirectURIs, redirectURI) {
		return nil, &authzFailure{code: "invalid_request", desc: "redirect_uri not registered"}
	}

	req := &authzRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, State: q.Get("state"), Nonce: q.Get("nonce"),
		MaxAge: -1, IDTokenHint: q.Get("id_token_hint"), LoginHint: q.Get("login_hint"),
		Provider: q.Get("provider"), DPoPJKT: q.Get("dpop_jkt"), ViaPAR: viaPAR,
		ResponseMode: q.Get("response_mode"), CodeChallenge: q.Get("code_challenge"),
	}
	bad := func(code, desc string) (*authzRequest, *authzFailure) {
		return nil, &authzFailure{req: req, code: code, desc: desc}
	}

	if req.ResponseMode != "" && req.ResponseMode != "query" && req.ResponseMode != "form_post" {
		req.ResponseMode = ""
		return bad("invalid_request", "unsupported response_mode")
	}
	if q.Get("response_type") != "code" {
		return bad("unsupported_response_type", "only 'code' is supported")
	}
	if q.Get("request") != "" {
		return bad("request_not_supported", "request objects are not supported")
	}
	if q.Get("request_uri") != "" && !viaPAR {
		return bad("request_uri_not_supported", "use the pushed authorization request endpoint")
	}
	if (s.cfg.RequirePAR || client.RequirePAR) && !viaPAR {
		return bad("invalid_request", "this client must use pushed authorization requests")
	}
	if req.CodeChallenge == "" {
		return bad("invalid_request", "code_challenge required (PKCE S256)")
	}
	if m := q.Get("code_challenge_method"); m != "" && m != "S256" {
		return bad("invalid_request", "only S256 code_challenge_method is supported")
	}
	if !pkceChallengeRE.MatchString(req.CodeChallenge) {
		return bad("invalid_request", "code_challenge must be a base64url SHA-256 value")
	}

	requested := strings.Fields(q.Get("scope"))
	req.Scopes = requested
	if len(client.AllowedScopes) > 0 && len(requested) > 0 {
		req.Scopes = nil
		for _, sc := range requested {
			if oauthSliceContains(client.AllowedScopes, sc) {
				req.Scopes = append(req.Scopes, sc)
			}
		}
		if len(req.Scopes) == 0 {
			return bad("invalid_scope", "none of the requested scopes is allowed for this client")
		}
	}

	req.Prompt = strings.Fields(q.Get("prompt"))
	for _, p := range req.Prompt {
		if !oauthSliceContains([]string{"none", "login", "consent", "select_account"}, p) {
			return bad("invalid_request", "unsupported prompt value")
		}
	}
	if req.hasPrompt("none") && len(req.Prompt) > 1 {
		return bad("invalid_request", "prompt=none cannot be combined with other values")
	}
	if v := q.Get("max_age"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return bad("invalid_request", "max_age must be a non-negative integer")
		}
		req.MaxAge = n
	}
	if v := q.Get("claims"); v != "" {
		if err := json.Unmarshal([]byte(v), &req.Claims); err != nil {
			return bad("invalid_request", "claims must be a JSON object")
		}
	}
	req.ACRValues = strings.Fields(q.Get("acr_values"))
	for _, res := range q["resource"] {
		u, err := url.Parse(res)
		if err != nil || !u.IsAbs() || u.Fragment != "" {
			return bad("invalid_target", "resource must be an absolute URI without fragment")
		}
		req.Resource = append(req.Resource, res)
	}
	return req, nil
}

// --------------------------------------------------------------------------
// Flow
// --------------------------------------------------------------------------

func userIDOfLogin(ctx context.Context, a *DatabaseAuthenticator, resp *LoginResponse) int {
	if resp.User != nil && resp.User.UserID != 0 {
		return resp.User.UserID
	}
	if info, err := a.OAuthIntrospectToken(ctx, resp.Token); err == nil && info.Active {
		id, _ := strconv.Atoi(info.Sub)
		return id
	}
	return 0
}

// continueAuthorize runs the authorization pipeline for the browser's current SSO session.
func (s *OAuthServer) continueAuthorize(w http.ResponseWriter, r *http.Request, req *authzRequest) {
	s.continueWith(w, r, req, s.ssoFromRequest(r))
}

func (s *OAuthServer) continueWith(w http.ResponseWriter, r *http.Request, req *authzRequest, sso *ssoSession) {
	client, ok := s.lookupOrFetchClient(r.Context(), req.ClientID)
	if !ok {
		s.authzDirectError(w, r, "invalid_client", "unknown client_id")
		return
	}

	needLogin := sso == nil
	if sso != nil && !req.LoginDone {
		switch {
		case req.hasPrompt("login"):
			needLogin = true
		case req.MaxAge >= 0 && time.Now().Unix()-sso.AuthTime > int64(req.MaxAge):
			needLogin = true
		case req.IDTokenHint != "":
			if sub, _ := s.hintSubject(req.IDTokenHint); sub != "" && sub != strconv.Itoa(sso.UserID) {
				needLogin = true
			}
		}
	}
	if needLogin {
		if req.hasPrompt("none") {
			s.authzRedirectError(w, r, req, "login_required", "no authenticated session")
			return
		}
		switch {
		case s.hasProviders():
			s.redirectToExternalProvider(w, r, req)
		case s.auth != nil:
			s.renderLogin(w, r, req, client, "")
		default:
			http.Error(w, "no authentication provider configured", http.StatusInternalServerError)
		}
		return
	}

	need, err := s.consentRequired(r.Context(), req, client, sso.UserID)
	if err != nil {
		s.authzRedirectError(w, r, req, "server_error", "could not evaluate consent")
		return
	}
	if need {
		if req.hasPrompt("none") {
			s.authzRedirectError(w, r, req, "consent_required", "user consent is required")
			return
		}
		s.renderConsent(w, r, req, client, sso)
		return
	}
	s.issueCode(w, r, req, sso)
}

// txBinding sets the browser binding cookie and returns its value.
func (s *OAuthServer) txBinding(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(txCookie); err == nil && len(c.Value) >= 16 {
		return c.Value
	}
	v, err := randomOAuthToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the issuer scheme (cookieSecure)
		Name: txCookie, Value: v, Path: "/", MaxAge: 900, HttpOnly: true,
		Secure: s.cookieSecure(), SameSite: http.SameSiteLaxMode})
	return v
}

func (s *OAuthServer) txMatches(r *http.Request, req *authzRequest) bool {
	if req.Tx == "" {
		return true
	}
	c, err := r.Cookie(txCookie)
	return err == nil && c.Value == req.Tx
}

func (s *OAuthServer) sealRequest(w http.ResponseWriter, r *http.Request, req *authzRequest) (string, error) {
	req.Tx = s.txBinding(w, r)
	return s.seal("authz", req, 15*time.Minute)
}

func (s *OAuthServer) renderLogin(w http.ResponseWriter, r *http.Request, req *authzRequest, client *OAuthServerClient, errMsg string) {
	state, err := s.sealRequest(w, r, req)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.renderHTML(w, http.StatusOK, s.tmpl.login, "login", OAuthLoginPage{
		Title: s.cfg.LoginTitle, Error: errMsg, Action: "authorize", State: state,
		ClientName: client.ClientName, LoginHint: req.LoginHint,
	})
}

// loginSubmit verifies the posted credentials and continues the pipeline.
func (s *OAuthServer) loginSubmit(w http.ResponseWriter, r *http.Request, req *authzRequest) {
	client, ok := s.lookupOrFetchClient(r.Context(), req.ClientID)
	if !ok {
		s.authzDirectError(w, r, "invalid_client", "unknown client_id")
		return
	}
	if s.auth == nil {
		http.Error(w, "no authentication provider configured", http.StatusInternalServerError)
		return
	}
	loginResp, err := s.auth.Login(r.Context(), LoginRequest{
		Username: r.PostFormValue("username"),
		Password: r.PostFormValue("password"),
	})
	if err != nil || loginResp == nil || loginResp.Token == "" {
		msg := "Invalid username or password"
		if loginResp != nil && loginResp.Requires2FA {
			msg = "Two-factor authentication is not supported on this sign-in form"
		}
		s.renderLogin(w, r, req, client, msg)
		return
	}
	userID := userIDOfLogin(r.Context(), s.auth, loginResp)
	if userID == 0 {
		s.renderLogin(w, r, req, client, "Invalid username or password")
		return
	}
	sso, err := s.newSSO(loginResp.Token, userID, "")
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.setSSO(w, sso)
	req.LoginDone = true
	if s.cfg.SSOCookie.Disable {
		req.Sess = sso
	}
	s.continueWith(w, r, req, sso)
}

// redirectToExternalProvider stores the request and sends the browser to the configured provider.
func (s *OAuthServer) redirectToExternalProvider(w http.ResponseWriter, r *http.Request, req *authzRequest) {
	var provider *externalProvider
	if req.Provider != "" {
		if provider = s.providerByName(req.Provider); provider == nil {
			http.Error(w, fmt.Sprintf("provider %q not found", req.Provider), http.StatusBadRequest)
			return
		}
	} else {
		s.mu.RLock()
		provider = &s.providers[0]
		s.mu.RUnlock()
	}

	providerState, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.pending[providerState] = &pendingAuth{Req: req, Provider: provider.providerName, ExpiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()

	authURL, err := provider.auth.OAuth2GetAuthURL(provider.providerName, providerState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// --------------------------------------------------------------------------
// External provider callback — GET {ProviderCallbackPath}
// --------------------------------------------------------------------------

func (s *OAuthServer) providerCallbackHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	providerState := r.URL.Query().Get("state")

	s.mu.Lock()
	pending, ok := s.pending[providerState]
	if ok {
		delete(s.pending, providerState)
	}
	s.mu.Unlock()

	if !ok || time.Now().After(pending.ExpiresAt) {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.authzRedirectError(w, r, pending.Req, "access_denied", "the identity provider refused the request")
		return
	}
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	provider := s.providerByName(pending.Provider)
	if provider == nil {
		http.Error(w, fmt.Sprintf("provider %q not found", pending.Provider), http.StatusInternalServerError)
		return
	}
	loginResp, err := provider.auth.OAuth2HandleCallback(r.Context(), pending.Provider, code, providerState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userID := userIDOfLogin(r.Context(), provider.auth, loginResp)
	if userID == 0 {
		http.Error(w, "could not resolve the user", http.StatusInternalServerError)
		return
	}
	sso, err := s.newSSO(loginResp.Token, userID, pending.Provider)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.setSSO(w, sso)
	req := pending.Req
	req.LoginDone = true
	if s.cfg.SSOCookie.Disable {
		req.Sess = sso
	}
	s.continueWith(w, r, req, sso)
}

// --------------------------------------------------------------------------
// Code issuance and responses
// --------------------------------------------------------------------------

func (s *OAuthServer) saveCode(ctx context.Context, c *OAuthCode) error {
	if s.cfg.PersistCodes && s.auth != nil {
		return s.auth.OAuthSaveCode(ctx, c)
	}
	s.mu.Lock()
	s.codes[c.Code] = c
	s.mu.Unlock()
	return nil
}

// takeCode returns and invalidates a code (single use).
func (s *OAuthServer) takeCode(ctx context.Context, code string) (*OAuthCode, bool) {
	if s.cfg.PersistCodes && s.auth != nil {
		c, err := s.auth.OAuthExchangeCode(ctx, code)
		return c, err == nil
	}
	s.mu.Lock()
	c, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(c.ExpiresAt) {
		return nil, false
	}
	return c, true
}

func (s *OAuthServer) issueCode(w http.ResponseWriter, r *http.Request, req *authzRequest, sso *ssoSession) {
	authCode, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	acr := ""
	if len(s.cfg.SupportedACR) > 0 {
		acr = s.cfg.SupportedACR[0]
	}
	amr := []string{"pwd"}
	if sso.Provider != "" {
		amr = []string{"fed"}
	}
	c := &OAuthCode{
		Code: authCode, ClientID: req.ClientID, RedirectURI: req.RedirectURI, ClientState: req.State,
		CodeChallenge: req.CodeChallenge, CodeChallengeMethod: "S256",
		SessionToken: sso.SID, Scopes: req.Scopes, ExpiresAt: time.Now().Add(s.cfg.AuthCodeTTL),
		UserID: sso.UserID, Nonce: req.Nonce, AuthTime: sso.AuthTime, ACR: acr, AMR: amr, SessionID: sso.SID,
		Claims: req.Claims, Resource: req.Resource, DPoPJKT: req.DPoPJKT, ResponseType: "code",
	}
	if err := s.saveCode(r.Context(), c); err != nil {
		s.authzRedirectError(w, r, req, "server_error", "could not issue the authorization code")
		return
	}
	s.noteClient(w, sso, req.ClientID)
	s.authzRespond(w, r, req, map[string]string{"code": authCode})
}

func (s *OAuthServer) authzRedirectError(w http.ResponseWriter, r *http.Request, req *authzRequest, code, desc string) {
	p := map[string]string{"error": code}
	if desc != "" {
		p["error_description"] = desc
	}
	s.authzRespond(w, r, req, p)
}

// authzRespond sends params to the client's redirect_uri (query or form_post) with state and the
// RFC 9207 iss parameter.
func (s *OAuthServer) authzRespond(w http.ResponseWriter, r *http.Request, req *authzRequest, params map[string]string) {
	if req.State != "" {
		params["state"] = req.State
	}
	params["iss"] = s.cfg.Issuer
	if req.ResponseMode == "form_post" {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", "script-src 'unsafe-inline'; frame-ancestors 'none'")
		var b strings.Builder
		b.WriteString(`<!DOCTYPE html><html><body onload="document.forms[0].submit()"><form method="post" action="`)
		b.WriteString(htmlEscape(req.RedirectURI))
		b.WriteString(`">`)
		for k, v := range params {
			b.WriteString(`<input type="hidden" name="` + htmlEscape(k) + `" value="` + htmlEscape(v) + `">`)
		}
		b.WriteString(`<noscript><button type="submit">Continue</button></noscript></form></body></html>`)
		w.Write([]byte(b.String())) //nolint:errcheck,gosec // G104: best-effort write, G705: values are HTML-escaped
		return
	}
	u, err := url.Parse(req.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusInternalServerError)
		return
	}
	qp := u.Query()
	for k, v := range params {
		qp.Set(k, v)
	}
	u.RawQuery = qp.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound) //nolint:gosec // u is built from the registered redirect URI
}
