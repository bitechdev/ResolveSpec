package security

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

const parURIPrefix = "urn:ietf:params:oauth:request_uri:"

// --------------------------------------------------------------------------
// RFC 9126 — Pushed authorization requests: POST /oauth/par
// --------------------------------------------------------------------------

func (s *OAuthServer) parHandler(w http.ResponseWriter, r *http.Request) {
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
	if r.PostForm.Get("request_uri") != "" {
		writeOAuthError(w, "invalid_request", "request_uri must not be pushed", http.StatusBadRequest)
		return
	}
	form := url.Values{}
	for k, v := range r.PostForm {
		switch k {
		case "client_secret", "client_assertion", "client_assertion_type":
			continue
		}
		form[k] = v
	}
	form.Set("client_id", ac.Client.ClientID)
	if _, fail := s.parseAuthz(r.Context(), form, true); fail != nil {
		writeOAuthError(w, fail.code, fail.desc, http.StatusBadRequest)
		return
	}
	id, err := randomOAuthToken()
	gs := s.grants()
	if err != nil || gs == nil {
		writeOAuthError(w, "server_error", "", http.StatusInternalServerError)
		return
	}
	uri := parURIPrefix + id
	if err := gs.SavePushedRequest(r.Context(), lookup.PushedRequest{
		RequestURI: uri, ClientID: ac.Client.ClientID, Params: map[string]string{"q": form.Encode()},
		ExpiresAt: time.Now().Add(s.cfg.PARTTL),
	}); err != nil {
		writeOAuthError(w, "server_error", "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"request_uri": uri, "expires_in": int(s.cfg.PARTTL.Seconds())})
}

// resolvePushedRequest consumes the pushed request behind request_uri (single use).
func (s *OAuthServer) resolvePushedRequest(ctx context.Context, clientID, uri string) (url.Values, *oauthError) {
	gs := s.grants()
	if gs == nil || (!s.cfg.EnablePAR && !s.cfg.RequirePAR) {
		return nil, oerr("request_uri_not_supported", "pushed authorization requests are not enabled", http.StatusBadRequest)
	}
	pr, err := gs.ConsumePushedRequest(ctx, uri)
	if err != nil {
		return nil, oerr("invalid_request_uri", "request_uri is unknown, expired or already used", http.StatusBadRequest)
	}
	if clientID != "" && clientID != pr.ClientID {
		return nil, oerr("invalid_request", "client_id does not match the pushed request", http.StatusBadRequest)
	}
	q, err := url.ParseQuery(pr.Params["q"])
	if err != nil {
		return nil, oerr("invalid_request_uri", "stored request is unreadable", http.StatusBadRequest)
	}
	q.Set("client_id", pr.ClientID)
	return q, nil
}
