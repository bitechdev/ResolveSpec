package security

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const backchannelEvent = "http://schemas.openid.net/event/backchannel-logout"

// hintSubject returns the subject and client of an id_token_hint issued by this server ("" when
// the hint is not valid). The hint may be expired.
func (s *OAuthServer) hintSubject(hint string) (sub, clientID string) {
	claims, err := s.parseOwnJWTOpts(hint, "", true)
	if err != nil {
		return "", ""
	}
	sub, _ = claims["sub"].(string)
	if auds := audOf(claims["aud"]); len(auds) > 0 {
		clientID = auds[0]
	}
	return sub, clientID
}

type logoutState struct {
	ClientID    string `json:"c,omitempty"`
	RedirectURI string `json:"r,omitempty"`
	State       string `json:"s,omitempty"`
	Sid         string `json:"i,omitempty"`
	Sub         string `json:"u,omitempty"`
}

// --------------------------------------------------------------------------
// OIDC RP-Initiated Logout — GET/POST /oauth/logout
// --------------------------------------------------------------------------

func (s *OAuthServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// Confirmation answer to the page rendered below.
	if blob := r.PostFormValue("lreq"); blob != "" {
		var st logoutState
		if err := s.open("logout", blob, &st); err != nil {
			s.renderMessage(w, http.StatusBadRequest, "Request expired", "This sign-out request has expired.", true)
			return
		}
		if r.PostFormValue("confirm") != "yes" {
			s.finishLogout(w, r, &st, false)
			return
		}
		s.performLogout(w, r, &st)
		return
	}

	st := logoutState{State: r.FormValue("state")}
	hint := r.FormValue("id_token_hint")
	if hint != "" {
		claims, err := s.parseOwnJWTOpts(hint, "", true)
		if err != nil {
			s.renderMessage(w, http.StatusBadRequest, "Invalid request", "The id_token_hint is not valid.", true)
			return
		}
		st.Sub, _ = claims["sub"].(string)
		st.Sid, _ = claims["sid"].(string)
		if auds := audOf(claims["aud"]); len(auds) > 0 {
			st.ClientID = auds[0]
		}
		if p := r.FormValue("client_id"); p != "" && p != st.ClientID {
			s.renderMessage(w, http.StatusBadRequest, "Invalid request", "client_id does not match the id_token_hint.", true)
			return
		}
	} else {
		st.ClientID = r.FormValue("client_id")
	}
	if uri := r.FormValue("post_logout_redirect_uri"); uri != "" {
		client, ok := s.lookupOrFetchClient(r.Context(), st.ClientID)
		if !ok || !oauthSliceContains(client.PostLogoutRedirectURIs, uri) {
			s.renderMessage(w, http.StatusBadRequest, "Invalid request", "post_logout_redirect_uri is not registered for this client.", true)
			return
		}
		st.RedirectURI = uri
	}

	if hint != "" {
		s.performLogout(w, r, &st)
		return
	}
	blob, err := s.seal("logout", st, 15*time.Minute)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	s.renderHTML(w, http.StatusOK, nil, "logout", oauthLogoutPage{
		Title: "Sign out", Action: "logout", Hidden: map[string]string{"lreq": blob},
	})
}

// performLogout ends the SSO session, revokes the refresh tokens bound to it and notifies the
// clients that hold tokens for it.
func (s *OAuthServer) performLogout(w http.ResponseWriter, r *http.Request, st *logoutState) {
	ctx := r.Context()
	sids := map[string]struct{}{}
	if st.Sid != "" {
		sids[st.Sid] = struct{}{}
	}
	clients := map[string]struct{}{}
	if st.ClientID != "" {
		clients[st.ClientID] = struct{}{}
	}

	sub, sid := st.Sub, st.Sid
	if c, err := r.Cookie(s.cfg.SSOCookie.Name); err == nil {
		var sess ssoSession
		if s.open("sso", c.Value, &sess) == nil && (st.Sub == "" || st.Sub == strconv.Itoa(sess.UserID)) {
			sids[sess.SID] = struct{}{}
			for _, cl := range sess.Clients {
				clients[cl] = struct{}{}
			}
			sub, sid = strconv.Itoa(sess.UserID), sess.SID
			if a := s.anyAuth(); a != nil {
				_ = a.Logout(ctx, LogoutRequest{Token: sess.Token})
			}
		}
	}
	if gs := s.grants(); gs != nil {
		for id := range sids {
			_ = gs.RevokeRefreshBySession(ctx, id)
		}
	}
	s.clearSSO(w)
	for id := range clients {
		if client, ok := s.lookupOrFetchClient(ctx, id); ok && client.BackchannelLogoutURI != "" {
			s.sendBackchannelLogout(client, sub, sid)
		}
	}
	s.finishLogout(w, r, st, true)
}

func (s *OAuthServer) finishLogout(w http.ResponseWriter, r *http.Request, st *logoutState, done bool) {
	if st.RedirectURI != "" {
		u, err := url.Parse(st.RedirectURI)
		if err == nil {
			if st.State != "" {
				q := u.Query()
				q.Set("state", st.State)
				u.RawQuery = q.Encode()
			}
			http.Redirect(w, r, u.String(), http.StatusFound)
			return
		}
	}
	if done {
		s.renderMessage(w, http.StatusOK, "Signed out", "You have been signed out.", false)
		return
	}
	s.renderMessage(w, http.StatusOK, "Still signed in", "You are still signed in.", false)
}

// sendBackchannelLogout posts a logout token to the client (OIDC Back-Channel Logout 1.0), best effort.
func (s *OAuthServer) sendBackchannelLogout(client *OAuthServerClient, sub, sid string) {
	jti, err := randomOAuthToken()
	if err != nil {
		return
	}
	claims := jwt.MapClaims{
		"iss": s.cfg.Issuer, "aud": client.ClientID, "iat": time.Now().Unix(), "jti": jti, "sub": sub,
		"events": map[string]any{backchannelEvent: map[string]any{}},
	}
	if sid != "" {
		claims["sid"] = sid
	}
	token, err := s.keys.forAlg(client.IDTokenSignedResponseAlg).sign(claims, "logout+jwt")
	if err != nil {
		return
	}
	target := client.BackchannelLogoutURI
	s.bcWG.Add(1)
	go func() {
		defer s.bcWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		body := url.Values{"logout_token": {token}}.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewBufferString(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := s.jwks.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
}
