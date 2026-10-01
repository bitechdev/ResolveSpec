package security

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// userCodeAlphabet avoids vowels and look-alike characters (RFC 8628 §6.1).
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

func newUserCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i, v := range b {
		out[i] = userCodeAlphabet[int(v)%len(userCodeAlphabet)]
	}
	return string(out), nil
}

func normalizeUserCode(c string) string {
	c = strings.ToUpper(c)
	c = strings.ReplaceAll(c, "-", "")
	return strings.ReplaceAll(c, " ", "")
}

func formatUserCode(c string) string {
	if len(c) == 8 {
		return c[:4] + "-" + c[4:]
	}
	return c
}

// filterScopes intersects the requested scopes with the client's allowed scopes.
func filterScopes(c *OAuthServerClient, requested []string) ([]string, bool) {
	if len(c.AllowedScopes) == 0 || len(requested) == 0 {
		return requested, true
	}
	var out []string
	for _, sc := range requested {
		if oauthSliceContains(c.AllowedScopes, sc) {
			out = append(out, sc)
		}
	}
	return out, len(out) > 0
}

// --------------------------------------------------------------------------
// RFC 8628 — device authorization: POST /oauth/device_authorization
// --------------------------------------------------------------------------

func (s *OAuthServer) deviceAuthorizationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	ac, e := s.requireClient(r)
	if e != nil {
		e.write(w)
		return
	}
	client := ac.Client
	if !grantAllowed(client, grantDeviceCode) {
		writeOAuthError(w, "unauthorized_client", "client may not use the device grant", http.StatusBadRequest)
		return
	}
	scopes, ok := filterScopes(client, strings.Fields(r.PostFormValue("scope")))
	if !ok {
		writeOAuthError(w, "invalid_scope", "none of the requested scopes is allowed for this client", http.StatusBadRequest)
		return
	}
	gs := s.grants()
	if gs == nil {
		writeOAuthError(w, "server_error", "", http.StatusInternalServerError)
		return
	}

	deviceCode, err := randomOAuthToken()
	if err != nil {
		writeOAuthError(w, "server_error", "", http.StatusInternalServerError)
		return
	}
	var userCode string
	for attempt := 0; ; attempt++ {
		if userCode, err = newUserCode(); err == nil {
			err = gs.CreateDevice(r.Context(), lookup.DeviceCode{
				DeviceHash: hashToken(deviceCode), UserCode: userCode, ClientID: client.ClientID, Scopes: scopes,
				Interval: s.cfg.DevicePollSeconds, ExpiresAt: time.Now().Add(s.cfg.DeviceCodeTTL),
			})
		}
		if err == nil {
			break
		}
		if attempt >= 3 { // a user-code collision is the only expected failure; give up after a few tries
			writeOAuthError(w, "server_error", "", http.StatusInternalServerError)
			return
		}
	}
	verification := s.endpoint("/oauth/device")
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 formatUserCode(userCode),
		"verification_uri":          verification,
		"verification_uri_complete": verification + "?user_code=" + formatUserCode(userCode),
		"expires_in":                int(s.cfg.DeviceCodeTTL.Seconds()),
		"interval":                  s.cfg.DevicePollSeconds,
	})
}

// --------------------------------------------------------------------------
// Verification page: GET/POST /oauth/device
// --------------------------------------------------------------------------

type deviceState struct {
	UserCode string `json:"uc"`
	Bind     string `json:"b,omitempty"`
	Tx       string `json:"tx,omitempty"`
}

func (s *OAuthServer) deviceVerificationHandler(w http.ResponseWriter, r *http.Request) {
	if s.cfg.SSOCookie.Disable {
		s.renderMessage(w, http.StatusNotImplemented, "Not available", "The device flow needs the SSO cookie, which is disabled on this server.", true)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		if code := r.FormValue("user_code"); code != "" {
			s.deviceNext(w, r, normalizeUserCode(code))
			return
		}
		s.renderHTML(w, http.StatusOK, nil, "device", oauthDevicePage{Title: "Connect a device", Action: "device"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.PostFormValue("step") {
	case "code":
		s.deviceNext(w, r, normalizeUserCode(r.PostFormValue("user_code")))
	case "login":
		st, ok := s.openDeviceState(w, r, "device-login")
		if !ok {
			return
		}
		if s.auth == nil {
			http.Error(w, "no authentication provider configured", http.StatusInternalServerError)
			return
		}
		resp, err := s.auth.Login(r.Context(), LoginRequest{Username: r.PostFormValue("username"), Password: r.PostFormValue("password")})
		uid := 0
		if err == nil && resp != nil && resp.Token != "" {
			uid = userIDOfLogin(r.Context(), s.auth, resp)
		}
		if uid == 0 {
			s.renderDeviceLogin(w, r, st.UserCode, "Invalid username or password")
			return
		}
		sso, err := s.newSSO(resp.Token, uid, "")
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		s.setSSO(w, sso)
		s.deviceConsent(w, r, st.UserCode, sso)
	case "decide":
		st, ok := s.openDeviceState(w, r, "device-consent")
		if !ok {
			return
		}
		sso := s.ssoFromRequest(r)
		if sso == nil || st.Bind != sso.SID {
			s.renderMessage(w, http.StatusBadRequest, "Session expired", "Your session has expired. Start again from the device.", true)
			return
		}
		err := s.grants().DeviceDecide(r.Context(), st.UserCode, r.PostFormValue("decision") == "allow", sso.UserID, sso.SID)
		if err != nil {
			s.renderMessage(w, http.StatusBadRequest, "Code not valid", "This code is unknown, expired or already used.", true)
			return
		}
		if r.PostFormValue("decision") == "allow" {
			s.renderMessage(w, http.StatusOK, "Device connected", "You can return to your device now.", false)
		} else {
			s.renderMessage(w, http.StatusOK, "Request denied", "The device was not given access.", false)
		}
	default:
		http.Error(w, "invalid request", http.StatusBadRequest)
	}
}

func (s *OAuthServer) openDeviceState(w http.ResponseWriter, r *http.Request, kind string) (*deviceState, bool) {
	var st deviceState
	if err := s.open(kind, r.PostFormValue("req"), &st); err != nil {
		s.renderMessage(w, http.StatusBadRequest, "Request expired", "This request has expired. Enter the code again.", true)
		return nil, false
	}
	if c, err := r.Cookie(txCookie); st.Tx != "" && (err != nil || c.Value != st.Tx) {
		s.renderMessage(w, http.StatusBadRequest, "Request rejected", "The browser session of this request does not match.", true)
		return nil, false
	}
	return &st, true
}

// deviceNext continues with the entered code: login when needed, then the approval screen.
func (s *OAuthServer) deviceNext(w http.ResponseWriter, r *http.Request, userCode string) {
	if _, err := s.grants().DeviceByUserCode(r.Context(), userCode); err != nil {
		code := http.StatusBadRequest
		if !errors.Is(err, lookup.ErrNotFound) {
			code = http.StatusInternalServerError
		}
		s.renderHTML(w, code, nil, "device", oauthDevicePage{Title: "Connect a device", Action: "device",
			Error: "This code is unknown or has expired."})
		return
	}
	if sso := s.ssoFromRequest(r); sso != nil {
		s.deviceConsent(w, r, userCode, sso)
		return
	}
	if s.auth == nil {
		s.renderMessage(w, http.StatusNotImplemented, "Sign-in unavailable",
			"Device sign-in needs the server's own login form, which is not configured.", true)
		return
	}
	s.renderDeviceLogin(w, r, userCode, "")
}

func (s *OAuthServer) renderDeviceLogin(w http.ResponseWriter, r *http.Request, userCode, errMsg string) {
	state, err := s.seal("device-login", deviceState{UserCode: userCode, Tx: s.txBinding(w, r)}, 15*time.Minute)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.renderHTML(w, http.StatusOK, s.tmpl.login, "login", OAuthLoginPage{
		Title: s.cfg.LoginTitle, Error: errMsg, Action: "device", State: state, Hidden: map[string]string{"step": "login"},
	})
}

func (s *OAuthServer) deviceConsent(w http.ResponseWriter, r *http.Request, userCode string, sso *ssoSession) {
	dc, err := s.grants().DeviceByUserCode(r.Context(), userCode)
	if err != nil {
		s.renderMessage(w, http.StatusBadRequest, "Code not valid", "This code is unknown, expired or already used.", true)
		return
	}
	client, ok := s.lookupOrFetchClient(r.Context(), dc.ClientID)
	if !ok {
		s.renderMessage(w, http.StatusBadRequest, "Unknown application", "The application that requested this code no longer exists.", true)
		return
	}
	state, err := s.seal("device-consent", deviceState{UserCode: userCode, Bind: sso.SID, Tx: s.txBinding(w, r)}, 15*time.Minute)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	name := client.ClientName
	if name == "" {
		name = client.ClientID
	}
	user := ""
	if a := s.anyAuth(); a != nil {
		if info, err := a.OAuthIntrospectToken(r.Context(), sso.Token); err == nil && info.Active {
			user = info.Username
		}
	}
	s.renderHTML(w, http.StatusOK, s.tmpl.consent, "consent", OAuthConsentPage{
		Title: "Connect " + name, Action: "device", State: state, ClientName: name,
		ClientURI: safeWebURL(client.ClientURI), LogoURI: safeWebURL(client.LogoURI),
		Scopes: s.scopeInfos(dc.Scopes), User: user, Hidden: map[string]string{"step": "decide"},
	})
}

// --------------------------------------------------------------------------
// Token endpoint side
// --------------------------------------------------------------------------

func (s *OAuthServer) handleDeviceGrant(r *http.Request) (map[string]any, *oauthError) {
	ac, e := s.requireClient(r)
	if e != nil {
		return nil, e
	}
	client := ac.Client
	if !grantAllowed(client, grantDeviceCode) {
		return nil, oerr("unauthorized_client", "client may not use the device grant", http.StatusBadRequest)
	}
	code := r.PostFormValue("device_code")
	if code == "" {
		return nil, oerr("invalid_request", "device_code required", http.StatusBadRequest)
	}
	gs := s.grants()
	if gs == nil {
		return nil, serverErr()
	}
	dc, err := gs.DevicePoll(r.Context(), hashToken(code))
	switch {
	case errors.Is(err, lookup.ErrDevicePending):
		return nil, oerr("authorization_pending", "", http.StatusBadRequest)
	case errors.Is(err, lookup.ErrDeviceSlowDown):
		return nil, oerr("slow_down", "", http.StatusBadRequest)
	case errors.Is(err, lookup.ErrDeviceDenied):
		return nil, oerr("access_denied", "", http.StatusBadRequest)
	case errors.Is(err, lookup.ErrDeviceExpired):
		return nil, oerr("expired_token", "", http.StatusBadRequest)
	case err != nil:
		return nil, serverErr()
	}
	if dc.ClientID != client.ClientID {
		return nil, oerr("invalid_grant", "device_code was issued to another client", http.StatusBadRequest)
	}
	jkt, e := s.dpopForClient(r, client)
	if e != nil {
		return nil, e
	}
	return s.mintTokens(r.Context(), &tokenGrant{
		Client: client, UserID: dc.UserID, Scopes: dc.Scopes, SID: dc.SessionToken, AuthTime: time.Now().Unix(),
		AMR: []string{"pwd"}, DPoPJKT: jkt, IDToken: true, IssueRefresh: refreshAllowed(client, dc.Scopes),
	})
}
