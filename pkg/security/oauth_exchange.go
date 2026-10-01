package security

import (
	"net/http"
	"net/url"
	"strings"
)

const (
	tokenTypeAccess = "urn:ietf:params:oauth:token-type:access_token" //nolint:gosec // RFC 8693 URN, not a credential
	tokenTypeJWT    = "urn:ietf:params:oauth:token-type:jwt"          //nolint:gosec // RFC 8693 URN, not a credential
)

// handleTokenExchange implements RFC 8693 for access tokens issued by this server: the caller
// trades a subject token for one with a narrower scope and/or another audience. Delegation with an
// actor_token is not supported.
func (s *OAuthServer) handleTokenExchange(r *http.Request) (map[string]any, *oauthError) {
	ac, e := s.requireClient(r)
	if e != nil {
		return nil, e
	}
	client := ac.Client
	if ac.Method == "none" {
		return nil, invalidClient("token exchange requires a confidential client", true)
	}
	if !grantAllowed(client, grantTokenExchange) {
		return nil, oerr("unauthorized_client", "client may not use token exchange", http.StatusBadRequest)
	}
	if r.PostFormValue("actor_token") != "" {
		return nil, oerr("invalid_request", "actor_token is not supported", http.StatusBadRequest)
	}
	if t := r.PostFormValue("subject_token_type"); t != "" && t != tokenTypeAccess && t != tokenTypeJWT {
		return nil, oerr("invalid_request", "unsupported subject_token_type", http.StatusBadRequest)
	}
	if t := r.PostFormValue("requested_token_type"); t != "" && t != tokenTypeAccess {
		return nil, oerr("invalid_request", "only access tokens can be issued", http.StatusBadRequest)
	}
	subject := r.PostFormValue("subject_token")
	if subject == "" {
		return nil, oerr("invalid_request", "subject_token required", http.StatusBadRequest)
	}
	ti := s.resolveAccessToken(r.Context(), subject)
	if ti == nil {
		return nil, oerr("invalid_grant", "subject_token is invalid or inactive", http.StatusBadRequest)
	}
	if ti.JKT != "" {
		return nil, oerr("invalid_request", "DPoP-bound tokens cannot be exchanged", http.StatusBadRequest)
	}

	scopes := ti.Scopes
	if req := strings.Fields(r.PostFormValue("scope")); len(req) > 0 {
		if !scopesCovered(ti.Scopes, req) || (len(client.AllowedScopes) > 0 && !scopesCovered(client.AllowedScopes, req)) {
			return nil, oerr("invalid_scope", "scope exceeds the subject token or the client", http.StatusBadRequest)
		}
		scopes = req
	}
	var resource []string
	for _, v := range append(r.PostForm["audience"], r.PostForm["resource"]...) {
		if v == "" {
			continue
		}
		if u, err := url.Parse(v); r.PostForm["resource"] != nil && (err != nil || !u.IsAbs() || u.Fragment != "") {
			return nil, oerr("invalid_target", "resource must be an absolute URI", http.StatusBadRequest)
		}
		resource = append(resource, v)
	}

	resp, e := s.mintTokens(r.Context(), &tokenGrant{
		Client: client, UserID: ti.UserID, Scopes: scopes, Resource: resource, SID: ti.SID,
	})
	if e != nil {
		return nil, e
	}
	resp["issued_token_type"] = tokenTypeAccess
	return resp, nil
}
