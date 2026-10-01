package security

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var supportedGrantTypes = []string{"authorization_code", "refresh_token", "client_credentials", grantDeviceCode, grantTokenExchange}

// validateRedirectURI checks a redirect (or post-logout) URI at registration: absolute, no
// fragment, https, http only for loopback, or a private-use scheme (RFC 8252).
func validateRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopbackHost(u.Hostname())
	case "javascript", "data", "file", "vbscript", "about", "blob", "ftp":
		return false
	}
	return true
}

func (s *OAuthServer) validateClientMetadata(c *OAuthServerClient) *oauthError {
	meta := func(desc string) *oauthError { return oerr("invalid_client_metadata", desc, http.StatusBadRequest) }

	for _, g := range c.GrantTypes {
		if !oauthSliceContains(supportedGrantTypes, g) {
			return meta("unsupported grant type " + g)
		}
	}
	if oauthSliceContains(c.GrantTypes, grantDeviceCode) && !s.cfg.EnableDeviceFlow {
		return meta("the device grant is not enabled on this server")
	}
	if oauthSliceContains(c.GrantTypes, grantTokenExchange) && !s.cfg.EnableTokenExchange {
		return meta("token exchange is not enabled on this server")
	}
	for _, rt := range c.ResponseTypes {
		if rt != "code" {
			return meta("unsupported response type " + rt)
		}
	}
	needsRedirect := false
	for _, g := range c.GrantTypes {
		if g == "authorization_code" {
			needsRedirect = true
		}
	}
	if needsRedirect && len(c.RedirectURIs) == 0 {
		return oerr("invalid_redirect_uri", "redirect_uris required", http.StatusBadRequest)
	}
	for _, u := range c.RedirectURIs {
		if !validateRedirectURI(u) {
			return oerr("invalid_redirect_uri", "invalid redirect_uri "+u, http.StatusBadRequest)
		}
	}
	for _, u := range c.PostLogoutRedirectURIs {
		if !validateRedirectURI(u) {
			return meta("invalid post_logout_redirect_uri " + u)
		}
	}
	if !oauthSliceContains(s.authMethodsSupported(), c.TokenEndpointAuthMethod) {
		return meta("unsupported token_endpoint_auth_method")
	}
	if c.TokenEndpointAuthMethod == "private_key_jwt" {
		if (len(c.JWKS) == 0) == (c.JWKSURI == "") {
			return meta("private_key_jwt needs exactly one of jwks and jwks_uri")
		}
		if len(c.JWKS) > 0 {
			if _, err := parseJWKS(c.JWKS); err != nil {
				return meta("jwks: " + err.Error())
			}
		}
	}
	if c.JWKSURI != "" && !validWebURL(c.JWKSURI, s.cfg.AllowPrivateNetworkFetch) {
		return meta("jwks_uri must be an https URL")
	}
	if a := c.TokenEndpointAuthSigningAlg; a != "" && !oauthSliceContains([]string{"RS256", "PS256", "ES256", "ES384"}, a) {
		return meta("unsupported token_endpoint_auth_signing_alg")
	}
	if a := c.IDTokenSignedResponseAlg; a != "" && !oauthSliceContains(s.keys.algs(), a) {
		return meta("id_token_signed_response_alg is not offered by this server")
	}
	if a := c.UserinfoSignedResponseAlg; a != "" && a != "none" && !oauthSliceContains(s.keys.algs(), a) {
		return meta("userinfo_signed_response_alg is not offered by this server")
	}
	if c.BackchannelLogoutURI != "" && !validWebURL(c.BackchannelLogoutURI, s.cfg.AllowPrivateNetworkFetch) {
		return meta("backchannel_logout_uri must be an https URL")
	}
	for _, u := range []string{c.ClientURI, c.LogoURI} {
		if u != "" && !validWebURL(u, true) {
			return meta("client_uri and logo_uri must be http(s) URLs")
		}
	}
	if len(c.ClientName) > 200 {
		return meta("client_name too long")
	}
	return nil
}

// validWebURL accepts https URLs, and http URLs when loose is set or the host is loopback.
func validWebURL(raw string, loose bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (loose || isLoopbackHost(u.Hostname())))
}

// clientView is the RFC 7591 representation of a client: no secret hash, no registration token hash.
func clientView(c *OAuthServerClient) map[string]any {
	v := *c
	v.ClientSecretHash, v.RegistrationAccessTokenHash = "", ""
	raw, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	m["client_id"] = c.ClientID
	m["token_endpoint_auth_method"] = c.TokenEndpointAuthMethod
	m["grant_types"] = c.GrantTypes
	m["redirect_uris"] = c.RedirectURIs
	if len(c.AllowedScopes) > 0 {
		m["scope"] = strings.Join(c.AllowedScopes, " ")
	}
	return m
}

// --------------------------------------------------------------------------
// RFC 7591 — Dynamic client registration
// --------------------------------------------------------------------------

func (s *OAuthServer) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.InitialAccessToken != "" {
		tok, _ := bearerFromHeader(r)
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.InitialAccessToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeOAuthError(w, "invalid_token", "an initial access token is required to register clients", http.StatusUnauthorized)
			return
		}
	}
	c, e := s.decodeClientMetadata(r)
	if e != nil {
		e.write(w)
		return
	}

	// client_credentials is a machine-to-machine grant and requires a confidential
	// client (RFC 6749 §4.4), so it always forces secret issuance regardless of the
	// requested auth method.
	if oauthSliceContains(c.GrantTypes, "client_credentials") && c.TokenEndpointAuthMethod == "none" {
		c.TokenEndpointAuthMethod = "client_secret_basic"
	}
	if e := s.validateClientMetadata(c); e != nil {
		e.write(w)
		return
	}

	id, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	c.ClientID = id
	c.ClientIDIssuedAt = time.Now().Unix()
	var secret string
	if strings.HasPrefix(c.TokenEndpointAuthMethod, "client_secret_") {
		if secret, err = randomOAuthToken(); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		c.ClientSecretHash = hashClientSecret(secret)
	}
	regToken, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	c.RegistrationAccessTokenHash = hashToken(regToken)

	if err := s.saveClient(r.Context(), c, false); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// RFC 7591 registration response: the plaintext secret and registration token are returned
	// exactly once here and never persisted or served again — only their hashes are stored.
	resp := clientView(c)
	if secret != "" {
		resp["client_secret"] = secret
		resp["client_secret_expires_at"] = 0
	}
	resp["registration_access_token"] = regToken
	resp["registration_client_uri"] = s.endpoint("/oauth/register/" + c.ClientID)
	writeJSON(w, http.StatusCreated, resp)
}

// decodeClientMetadata reads the registration document and strips every field the caller must not
// control.
func (s *OAuthServer) decodeClientMetadata(r *http.Request) (*OAuthServerClient, *oauthError) {
	var req struct {
		OAuthServerClient
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&req); err != nil {
		return nil, oerr("invalid_client_metadata", "malformed JSON", http.StatusBadRequest)
	}
	c := req.OAuthServerClient
	c.ClientID, c.ClientSecretHash, c.RegistrationAccessTokenHash = "", "", ""
	c.FirstParty, c.ClientSecretExpiresAt, c.ClientIDIssuedAt = false, 0, 0
	if len(c.GrantTypes) == 0 {
		c.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(c.ResponseTypes) == 0 {
		c.ResponseTypes = []string{"code"}
	}
	if len(c.AllowedScopes) == 0 {
		if f := strings.Fields(req.Scope); len(f) > 0 {
			c.AllowedScopes = f
		} else {
			c.AllowedScopes = s.cfg.DefaultScopes
		}
	}
	if c.TokenEndpointAuthMethod == "" {
		c.TokenEndpointAuthMethod = "none"
	}
	return &c, nil
}

func bearerFromHeader(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

// registrationClient authenticates a RFC 7592 management request and returns the client.
func (s *OAuthServer) registrationClient(w http.ResponseWriter, r *http.Request) *OAuthServerClient {
	deny := func() *OAuthServerClient {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeOAuthError(w, "invalid_token", "invalid registration access token", http.StatusUnauthorized)
		return nil
	}
	c, ok := s.lookupOrFetchClient(r.Context(), r.PathValue("id"))
	tok, _ := bearerFromHeader(r)
	if !ok || c.RegistrationAccessTokenHash == "" || tok == "" ||
		subtle.ConstantTimeCompare([]byte(hashToken(tok)), []byte(c.RegistrationAccessTokenHash)) != 1 {
		return deny()
	}
	return c
}

// registrationManageHandler serves GET, PUT and DELETE /oauth/register/{id} (RFC 7592).
func (s *OAuthServer) registrationManageHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPut, http.MethodDelete:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cur := s.registrationClient(w, r)
	if cur == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		resp := clientView(cur)
		resp["registration_client_uri"] = s.endpoint("/oauth/register/" + cur.ClientID)
		writeJSON(w, http.StatusOK, resp)
	case http.MethodDelete:
		if err := s.removeClient(r.Context(), cur.ClientID); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		next, e := s.decodeClientMetadata(r)
		if e != nil {
			e.write(w)
			return
		}
		if oauthSliceContains(next.GrantTypes, "client_credentials") && next.TokenEndpointAuthMethod == "none" {
			next.TokenEndpointAuthMethod = "client_secret_basic"
		}
		if e := s.validateClientMetadata(next); e != nil {
			e.write(w)
			return
		}
		wantsSecret := strings.HasPrefix(next.TokenEndpointAuthMethod, "client_secret_")
		if wantsSecret && cur.ClientSecretHash == "" {
			oerr("invalid_client_metadata", "the client has no secret; register a new client instead", http.StatusBadRequest).write(w)
			return
		}
		next.ClientID = cur.ClientID
		next.ClientIDIssuedAt = cur.ClientIDIssuedAt
		next.RegistrationAccessTokenHash = cur.RegistrationAccessTokenHash
		next.FirstParty = cur.FirstParty // only trusted code may change this
		next.ClientSecretExpiresAt = cur.ClientSecretExpiresAt
		if wantsSecret {
			next.ClientSecretHash = cur.ClientSecretHash
		}
		if err := s.saveClient(r.Context(), next, true); err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		resp := clientView(next)
		resp["registration_client_uri"] = s.endpoint("/oauth/register/" + next.ClientID)
		writeJSON(w, http.StatusOK, resp)
	}
}

// registrationRotateHandler issues a new client secret: POST /oauth/register/{id}/rotate-secret.
func (s *OAuthServer) registrationRotateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cur := s.registrationClient(w, r)
	if cur == nil {
		return
	}
	if cur.ClientSecretHash == "" {
		writeOAuthError(w, "invalid_request", "this client has no secret", http.StatusBadRequest)
		return
	}
	secret, err := randomOAuthToken()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	next := *cur
	next.ClientSecretHash = hashClientSecret(secret)
	if err := s.saveClient(r.Context(), &next, true); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	resp := clientView(&next)
	resp["client_secret"] = secret
	resp["client_secret_expires_at"] = 0
	writeJSON(w, http.StatusOK, resp)
}
