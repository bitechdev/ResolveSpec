package security

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

const (
	grantDeviceCode    = "urn:ietf:params:oauth:grant-type:device_code"
	grantTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // RFC 8693 URN, not a credential

	// accessGrantFamily marks oauth_refresh_tokens rows that record an issued access token
	// (scope, client, DPoP binding). They share the table so logout revokes both.
	accessGrantPrefix = "at_"
)

// isAccessRecord reports whether a stored token is the record of an access token.
func isAccessRecord(t *lookup.RefreshToken) bool {
	return strings.HasPrefix(t.FamilyID, accessGrantPrefix)
}

// accessKey is the token_hash of the record of an access token (keyed by the session token or the
// JWT's jti). The prefix is not hex, so it can never equal the hash of a refresh token.
func accessKey(id string) string { return accessGrantPrefix + hashToken(id)[:61] }

// tokenGrant describes the tokens to issue.
type tokenGrant struct {
	Client   *OAuthServerClient
	UserID   int
	Scopes   []string
	Resource []string
	Nonce    string
	AuthTime int64
	ACR      string
	AMR      []string
	SID      string
	Claims   map[string]any
	DPoPJKT  string

	// LegacyAccess is an existing session token that is used as the access token (codes saved
	// by earlier versions).
	LegacyAccess  string
	LegacyRefresh string // refresh token passed through from the DatabaseAuthenticator

	IssueRefresh bool      // issue a managed refresh token (new family)
	FamilyID     string    // family of the new refresh token
	NextRefresh  string    // already rotated refresh token to return
	FamilyExp    time.Time // absolute end of the family when rotating
	IDToken      bool
}

// mintTokens issues the tokens of g and returns the token endpoint response.
func (s *OAuthServer) mintTokens(ctx context.Context, g *tokenGrant) (map[string]any, *oauthError) {
	a := s.anyAuth()
	if a == nil {
		return nil, oerr("server_error", "no authenticator configured", http.StatusInternalServerError)
	}
	if g.LegacyAccess != "" && g.UserID == 0 {
		info, err := a.OAuthIntrospectToken(ctx, g.LegacyAccess)
		if err != nil || !info.Active {
			return nil, oerr("invalid_grant", "the session of this code has ended", http.StatusBadRequest)
		}
		g.UserID, _ = strconv.Atoi(info.Sub)
	}
	now := time.Now()
	exp := now.Add(s.cfg.AccessTokenTTL)
	sub := strconv.Itoa(g.UserID)
	tokenType := "Bearer"
	if g.DPoPJKT != "" {
		tokenType = "DPoP"
	}

	var access, recordID string
	switch {
	case s.cfg.JWTAccessTokens:
		jti, err := randomOAuthToken()
		if err != nil {
			return nil, serverErr()
		}
		aud := jwt.ClaimStrings{s.cfg.AccessTokenAudience}
		if len(g.Resource) > 0 {
			aud = g.Resource
		}
		claims := jwt.MapClaims{
			"iss": s.cfg.Issuer, "sub": sub, "aud": aud, "exp": exp.Unix(), "iat": now.Unix(),
			"nbf": now.Unix(), "jti": jti, "client_id": g.Client.ClientID,
		}
		if len(g.Scopes) > 0 {
			claims["scope"] = strings.Join(g.Scopes, " ")
		}
		if g.AuthTime != 0 {
			claims["auth_time"] = g.AuthTime
		}
		if g.SID != "" {
			claims["sid"] = g.SID
		}
		if g.DPoPJKT != "" {
			claims["cnf"] = map[string]any{"jkt": g.DPoPJKT}
		}
		var err2 error
		if access, err2 = s.keys.forAlg("").sign(claims, "at+jwt"); err2 != nil {
			return nil, serverErr()
		}
		recordID = jti
	case g.LegacyAccess != "":
		access, recordID = g.LegacyAccess, g.LegacyAccess
	default:
		tok, err := randomOAuthToken()
		if err != nil {
			return nil, serverErr()
		}
		if err := a.oauth2CreateSession(ctx, tok, g.UserID, &oauth2.Token{AccessToken: tok, TokenType: "Bearer"}, exp, "oauth2_server"); err != nil {
			return nil, serverErr()
		}
		access, recordID = tok, tok
	}

	if gs := s.grants(); gs != nil {
		extra := map[string]any{}
		if g.DPoPJKT != "" {
			extra["jkt"] = g.DPoPJKT
		}
		if len(g.Resource) > 0 {
			extra["aud"] = g.Resource
		}
		if len(g.Claims) > 0 {
			extra["claims"] = g.Claims
		}
		if err := gs.SaveRefresh(ctx, lookup.RefreshToken{
			TokenHash: accessKey(recordID), FamilyID: accessKey(recordID), ClientID: g.Client.ClientID, UserID: g.UserID,
			SessionToken: g.SID, Scopes: g.Scopes, Extra: extra, ExpiresAt: exp,
		}); err != nil {
			return nil, serverErr()
		}
	}

	resp := map[string]any{
		"access_token": access,
		"token_type":   tokenType,
		"expires_in":   int64(s.cfg.AccessTokenTTL.Seconds()),
	}
	if len(g.Scopes) > 0 {
		resp["scope"] = strings.Join(g.Scopes, " ")
	}

	switch {
	case g.NextRefresh != "":
		resp["refresh_token"] = g.NextRefresh
	case g.IssueRefresh && s.cfg.ManagedRefreshTokens:
		rt, e := s.newRefreshToken(ctx, g, now)
		if e != nil {
			return nil, e
		}
		resp["refresh_token"] = rt
	case g.LegacyRefresh != "":
		resp["refresh_token"] = g.LegacyRefresh
	}

	if g.IDToken && oauthSliceContains(g.Scopes, "openid") {
		idt, err := s.buildIDToken(ctx, idTokenParams{
			Client: g.Client, UserID: g.UserID, Scopes: g.Scopes, Nonce: g.Nonce, AuthTime: g.AuthTime,
			ACR: g.ACR, AMR: g.AMR, SID: g.SID, AccessToken: access, Claims: g.Claims,
		})
		if err != nil {
			return nil, oerr("server_error", "could not sign the id_token", http.StatusInternalServerError)
		}
		resp["id_token"] = idt
	}
	return resp, nil
}

func (s *OAuthServer) writeTokenResponse(w http.ResponseWriter, resp map[string]any) {
	writeJSON(w, http.StatusOK, resp)
}

// --------------------------------------------------------------------------
// Token endpoint — POST /oauth/token
// --------------------------------------------------------------------------

func (s *OAuthServer) tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	var resp map[string]any
	var e *oauthError
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		resp, e = s.handleAuthCodeGrant(r)
	case "refresh_token":
		resp, e = s.handleRefreshGrant(r)
	case "client_credentials":
		resp, e = s.handleClientCredentialsGrant(r)
	case grantDeviceCode:
		if !s.cfg.EnableDeviceFlow {
			e = oerr("unsupported_grant_type", "", http.StatusBadRequest)
		} else {
			resp, e = s.handleDeviceGrant(r)
		}
	case grantTokenExchange:
		if !s.cfg.EnableTokenExchange {
			e = oerr("unsupported_grant_type", "", http.StatusBadRequest)
		} else {
			resp, e = s.handleTokenExchange(r)
		}
	default:
		e = oerr("unsupported_grant_type", "", http.StatusBadRequest)
	}
	if e != nil {
		e.write(w)
		return
	}
	s.writeTokenResponse(w, resp)
}

// grantAllowed reports whether the client may use grantType. Clients that declare no grant
// types keep every authorization-code style grant.
func grantAllowed(c *OAuthServerClient, grantType string) bool {
	if len(c.GrantTypes) == 0 {
		return grantType == "authorization_code" || grantType == "refresh_token"
	}
	return oauthSliceContains(c.GrantTypes, grantType)
}

// dpopForClient validates the DPoP header of a token request and returns the key to bind to.
func (s *OAuthServer) dpopForClient(r *http.Request, c *OAuthServerClient) (string, *oauthError) {
	proof, e := s.verifyDPoP(r, "")
	if e != nil {
		return "", e
	}
	if proof == nil {
		if c.DPoPBoundAccessTokens {
			return "", oerr("invalid_dpop_proof", "this client must present a DPoP proof", http.StatusBadRequest)
		}
		return "", nil
	}
	return proof.JKT, nil
}

func (s *OAuthServer) handleAuthCodeGrant(r *http.Request) (map[string]any, *oauthError) {
	ac, e := s.requireClient(r)
	if e != nil {
		return nil, e
	}
	client := ac.Client
	if !grantAllowed(client, "authorization_code") {
		return nil, oerr("unauthorized_client", "client may not use authorization_code", http.StatusBadRequest)
	}
	code := r.PostFormValue("code")
	verifier := r.PostFormValue("code_verifier")
	if code == "" || verifier == "" {
		return nil, oerr("invalid_request", "code and code_verifier required", http.StatusBadRequest)
	}
	jkt, e := s.dpopForClient(r, client)
	if e != nil {
		return nil, e
	}

	c, ok := s.takeCode(r.Context(), code)
	if !ok {
		// A code that is presented twice means it leaked: end the tokens issued from it.
		if s.cfg.ManagedRefreshTokens {
			if g := s.grants(); g != nil {
				_ = g.RevokeRefreshFamily(r.Context(), codeFamily(code))
			}
		}
		return nil, oerr("invalid_grant", "code expired or invalid", http.StatusBadRequest)
	}
	switch {
	case c.ClientID != client.ClientID:
		return nil, oerr("invalid_grant", "code was issued to another client", http.StatusBadRequest)
	case c.RedirectURI != r.PostFormValue("redirect_uri"):
		return nil, oerr("invalid_grant", "redirect_uri mismatch", http.StatusBadRequest)
	case !validatePKCESHA256(c.CodeChallenge, verifier):
		return nil, oerr("invalid_grant", "code_verifier invalid", http.StatusBadRequest)
	case c.DPoPJKT != "" && c.DPoPJKT != jkt:
		return nil, oerr("invalid_dpop_proof", "the DPoP key does not match dpop_jkt of the authorization request", http.StatusBadRequest)
	}

	g := &tokenGrant{
		Client: client, UserID: c.UserID, Scopes: c.Scopes, Resource: c.Resource, Nonce: c.Nonce,
		AuthTime: c.AuthTime, ACR: c.ACR, AMR: c.AMR, SID: c.SessionID, Claims: c.Claims, DPoPJKT: jkt,
		IDToken: true, FamilyID: codeFamily(code),
		IssueRefresh: refreshAllowed(client, c.Scopes),
	}
	if c.UserID == 0 { // saved by an earlier version: the code carries the session itself
		g.LegacyAccess, g.LegacyRefresh = c.SessionToken, c.RefreshToken
		g.IssueRefresh = false
	}
	return s.mintTokens(r.Context(), g)
}

// codeFamily derives the refresh family of the tokens issued from a code.
func codeFamily(code string) string { return "c" + hashToken(code)[:30] }

// refreshAllowed reports whether a managed refresh token is issued for the grant.
func refreshAllowed(c *OAuthServerClient, scopes []string) bool {
	return oauthSliceContains(scopes, "offline_access") || grantAllowed(c, "refresh_token")
}

// --------------------------------------------------------------------------
// RFC 6749 §4.4 — Client credentials grant
// --------------------------------------------------------------------------

func (s *OAuthServer) handleClientCredentialsGrant(r *http.Request) (map[string]any, *oauthError) {
	if s.auth == nil {
		return nil, oerr("unsupported_grant_type", "client_credentials requires a local user store", http.StatusBadRequest)
	}
	ac, e := s.requireClient(r)
	if e != nil {
		e.WWWAuth = `Basic realm="oauth"`
		return nil, e
	}
	client := ac.Client
	if ac.Method == "none" {
		return nil, invalidClient("client_credentials requires a confidential client", true)
	}
	if !oauthSliceContains(client.GrantTypes, "client_credentials") {
		return nil, oerr("unauthorized_client", "client is not authorized for client_credentials", http.StatusBadRequest)
	}

	requested := strings.Fields(r.PostFormValue("scope"))
	effective := client.AllowedScopes
	if len(requested) > 0 {
		effective = nil
		for _, sc := range requested {
			if oauthSliceContains(client.AllowedScopes, sc) {
				effective = append(effective, sc)
			}
		}
		if len(effective) == 0 {
			return nil, oerr("invalid_scope", "no requested scope is allowed for this client", http.StatusBadRequest)
		}
	}
	jkt, e := s.dpopForClient(r, client)
	if e != nil {
		return nil, e
	}

	// client_credentials tokens have no end user, but the rest of the stack (RLS-scoping
	// hooks, introspection) expects every access token to resolve to a user_sessions row
	// with a user_id. Represent the client as a deterministic synthetic "service account"
	// user so the existing get-or-create/create-session/introspection pipeline handles it
	// unchanged — no new tables or code paths required.
	userID, err := s.auth.oauth2GetOrCreateUser(r.Context(), &UserContext{
		UserName: "client:" + client.ClientID,
		Email:    "oauth-client-" + client.ClientID + "@service.internal",
		RemoteID: client.ClientID,
		Roles:    effective,
	}, "oauth2_client")
	if err != nil {
		return nil, serverErr()
	}
	// No refresh token per RFC 6749 §4.4.3, and no id_token — client_credentials has no
	// end-user subject to represent in OIDC terms.
	var resource []string
	if res := r.PostForm["resource"]; len(res) > 0 {
		resource = res
	}
	return s.mintTokens(r.Context(), &tokenGrant{
		Client: client, UserID: userID, Scopes: effective, Resource: resource, DPoPJKT: jkt,
	})
}
