package security

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// standard claim sets released by scope (OIDC Core §5.4).
var scopeClaims = map[string][]string{
	"profile": {"name", "family_name", "given_name", "middle_name", "nickname", "preferred_username", "profile",
		"picture", "website", "gender", "birthdate", "zoneinfo", "locale", "updated_at"},
	"email":   {"email", "email_verified"},
	"address": {"address"},
	"phone":   {"phone_number", "phone_number_verified"},
}

// --------------------------------------------------------------------------
// Discovery — RFC 8414 / OIDC Discovery / RFC 9728
// --------------------------------------------------------------------------

func (s *OAuthServer) grantTypesSupported() []string {
	g := []string{"authorization_code", "refresh_token"}
	if s.auth != nil {
		g = append(g, "client_credentials")
	}
	if s.cfg.EnableDeviceFlow {
		g = append(g, grantDeviceCode)
	}
	if s.cfg.EnableTokenExchange {
		g = append(g, grantTokenExchange)
	}
	return g
}

func (s *OAuthServer) authMethodsSupported() []string {
	return []string{"none", "client_secret_basic", "client_secret_post", "private_key_jwt"}
}

// serverMetadata builds the fields shared by RFC 8414 authorization-server
// metadata and OIDC discovery metadata.
func (s *OAuthServer) serverMetadata() map[string]any {
	m := map[string]any{
		"issuer":                                           s.cfg.Issuer,
		"authorization_endpoint":                           s.endpoint("/oauth/authorize"),
		"token_endpoint":                                   s.endpoint("/oauth/token"),
		"registration_endpoint":                            s.endpoint("/oauth/register"),
		"revocation_endpoint":                              s.endpoint("/oauth/revoke"),
		"introspection_endpoint":                           s.endpoint("/oauth/introspect"),
		"userinfo_endpoint":                                s.endpoint("/oauth/userinfo"),
		"jwks_uri":                                         s.endpoint("/oauth/jwks.json"),
		"scopes_supported":                                 s.scopesSupported(),
		"response_types_supported":                         []string{"code"},
		"response_modes_supported":                         []string{"query", "form_post"},
		"grant_types_supported":                            s.grantTypesSupported(),
		"code_challenge_methods_supported":                 []string{"S256"},
		"token_endpoint_auth_methods_supported":            s.authMethodsSupported(),
		"token_endpoint_auth_signing_alg_values_supported": []string{"RS256", "PS256", "ES256", "ES384"},
		"revocation_endpoint_auth_methods_supported":       s.authMethodsSupported(),
		"introspection_endpoint_auth_methods_supported":    s.authMethodsSupported(),
		"authorization_response_iss_parameter_supported":   true,
		"prompt_values_supported":                          []string{"none", "login", "consent"},
		"request_parameter_supported":                      false,
		"request_uri_parameter_supported":                  s.cfg.EnablePAR || s.cfg.RequirePAR,
		"claims_parameter_supported":                       true,
		"subject_types_supported":                          []string{"public"},
		"id_token_signing_alg_values_supported":            s.keys.algs(),
		"userinfo_signing_alg_values_supported":            append([]string{"none"}, s.keys.algs()...),
		"claim_types_supported":                            []string{"normal"},
		"claims_supported":                                 s.claimsSupported(),
		"service_documentation":                            "https://github.com/bitechdev/ResolveSpec/blob/main/pkg/security/OAUTH2_SERVER.md",
	}
	if len(s.cfg.SupportedACR) > 0 {
		m["acr_values_supported"] = s.cfg.SupportedACR
	}
	if s.cfg.EnablePAR || s.cfg.RequirePAR {
		m["pushed_authorization_request_endpoint"] = s.endpoint("/oauth/par")
		m["require_pushed_authorization_requests"] = s.cfg.RequirePAR
	}
	if s.cfg.EnableDeviceFlow {
		m["device_authorization_endpoint"] = s.endpoint("/oauth/device_authorization")
	}
	if s.cfg.EnableDPoP {
		m["dpop_signing_alg_values_supported"] = []string{"ES256", "ES384", "RS256", "PS256"}
	}
	if !s.cfg.DisableLogout {
		m["end_session_endpoint"] = s.endpoint("/oauth/logout")
		m["backchannel_logout_supported"] = true
		m["backchannel_logout_session_supported"] = true
	}
	return m
}

func (s *OAuthServer) scopesSupported() []string {
	out := append([]string(nil), s.cfg.DefaultScopes...)
	if s.cfg.ManagedRefreshTokens && !oauthSliceContains(out, "offline_access") {
		out = append(out, "offline_access")
	}
	return out
}

func (s *OAuthServer) claimsSupported() []string {
	set := map[string]struct{}{"sub": {}, "iss": {}, "aud": {}, "exp": {}, "iat": {}, "auth_time": {}, "nonce": {},
		"acr": {}, "amr": {}, "azp": {}, "at_hash": {}, "sid": {}}
	for _, sc := range s.scopesSupported() {
		for _, c := range scopeClaims[sc] {
			set[c] = struct{}{}
		}
	}
	return sortedKeys(set)
}

// metadataHandler serves one of the well-known documents. The path-insertion forms
// (/.well-known/x/<issuer path>) are answered only for this server's issuer path.
func (s *OAuthServer) metadataHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rest := r.PathValue("path"); rest != "" {
			if "/"+strings.Trim(rest, "/") != strings.TrimSuffix(s.issuerURL.Path, "/") {
				http.NotFound(w, r)
				return
			}
		}
		var doc map[string]any
		switch name {
		case "oauth-protected-resource":
			doc = map[string]any{
				"resource":                 s.cfg.ResourceIdentifier,
				"authorization_servers":    []string{s.cfg.Issuer},
				"scopes_supported":         s.cfg.DefaultScopes,
				"bearer_methods_supported": []string{"header"},
			}
			if s.cfg.EnableDPoP {
				doc["dpop_signing_alg_values_supported"] = []string{"ES256", "ES384", "RS256", "PS256"}
			}
		default:
			doc = s.serverMetadata()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		json.NewEncoder(w).Encode(doc) //nolint:errcheck,gosec // G104: best-effort write, error intentionally ignored
	}
}

func (s *OAuthServer) jwksHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	json.NewEncoder(w).Encode(map[string]any{"keys": s.keys.jwks()}) //nolint:errcheck,gosec // G104: best-effort write, error intentionally ignored
}

// --------------------------------------------------------------------------
// id_token
// --------------------------------------------------------------------------

type idTokenParams struct {
	Client      *OAuthServerClient
	UserID      int
	Scopes      []string
	Nonce       string
	AuthTime    int64
	ACR         string
	AMR         []string
	SID         string
	AccessToken string
	Claims      map[string]any // the OIDC "claims" request parameter
}

// halfHash is the left half of the hash of v (at_hash / c_hash), with the hash of the JWS alg.
func halfHash(alg, v string) string {
	var h hash.Hash
	switch {
	case strings.HasSuffix(alg, "384"):
		h = sha512.New384()
	case strings.HasSuffix(alg, "512"):
		h = sha512.New()
	default:
		h = sha256.New()
	}
	h.Write([]byte(v))
	sum := h.Sum(nil)
	return b64u(sum[:len(sum)/2])
}

func (s *OAuthServer) buildIDToken(ctx context.Context, p idTokenParams) (string, error) {
	a := s.anyAuth()
	if a == nil {
		return "", errors.New("no authenticator configured")
	}
	user, err := a.OAuthGetUser(ctx, p.UserID)
	if err != nil {
		return "", err
	}
	key := s.keys.forAlg(p.Client.IDTokenSignedResponseAlg)
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": s.cfg.Issuer, "sub": strconv.Itoa(p.UserID), "aud": p.Client.ClientID, "azp": p.Client.ClientID,
		"exp": now.Add(s.cfg.AccessTokenTTL).Unix(), "iat": now.Unix(),
	}
	if p.Nonce != "" {
		claims["nonce"] = p.Nonce
	}
	if p.AuthTime != 0 {
		claims["auth_time"] = p.AuthTime
	}
	if p.ACR != "" {
		claims["acr"] = p.ACR
	}
	if len(p.AMR) > 0 {
		claims["amr"] = p.AMR
	}
	if p.SID != "" {
		claims["sid"] = p.SID
	}
	if p.AccessToken != "" {
		claims["at_hash"] = halfHash(key.alg, p.AccessToken)
	}
	for k, v := range s.userClaims(ctx, user, p.UserID, p.Scopes, p.Claims, "id_token") {
		if _, reserved := claims[k]; !reserved {
			claims[k] = v
		}
	}
	return key.sign(claims, "")
}

// userClaims returns the claims of a user released by scopes plus those requested one by one in the
// OIDC "claims" parameter for destination ("id_token" or "userinfo").
func (s *OAuthServer) userClaims(ctx context.Context, user *UserContext, userID int, scopes []string, claimsParam map[string]any, destination string) map[string]any {
	base := map[string]any{}
	if user != nil {
		if user.UserName != "" {
			base["preferred_username"] = user.UserName
		}
		if user.Email != "" {
			base["email"] = user.Email
		}
	}
	requested := map[string]struct{}{}
	if m, ok := claimsParam[destination].(map[string]any); ok {
		for k := range m {
			requested[k] = struct{}{}
		}
	}

	all := base
	if s.cfg.ClaimsProvider != nil {
		extra, err := s.cfg.ClaimsProvider(ctx, OAuthClaimsRequest{
			UserID: userID, Sub: strconv.Itoa(userID), Scopes: scopes, Requested: sortedKeys(requested),
			Destination: destination, Base: base,
		})
		if err == nil {
			all = map[string]any{}
			for k, v := range base {
				all[k] = v
			}
			for k, v := range extra {
				all[k] = v
			}
		}
	}

	out := map[string]any{}
	allowed := map[string]struct{}{}
	for _, sc := range scopes {
		for _, c := range scopeClaims[sc] {
			allowed[c] = struct{}{}
		}
	}
	for k, v := range all {
		_, byScope := allowed[k]
		_, asked := requested[k]
		if byScope || asked {
			out[k] = v
		}
	}
	return out
}

// --------------------------------------------------------------------------
// Access token resolution
// --------------------------------------------------------------------------

// tokenInfo is an active access token.
type tokenInfo struct {
	UserID    int
	Sub       string
	Username  string
	Email     string
	Roles     []string
	UserLevel int
	Scopes    []string
	ClientID  string
	Exp, Iat  int64
	JKT       string
	Aud       []string
	JTI       string
	SID       string
	Claims    map[string]any
	JWT       bool
	Legacy    bool // a session token that was not issued through a grant (no recorded scope)
}

func (t *tokenInfo) tokenType() string {
	if t.JKT != "" {
		return "DPoP"
	}
	return "Bearer"
}

// resolveAccessToken returns the active access token, or nil. Tokens are our JWT access tokens or
// opaque session tokens.
func (s *OAuthServer) resolveAccessToken(ctx context.Context, token string) *tokenInfo {
	if token == "" {
		return nil
	}
	a := s.anyAuth()
	if a == nil {
		return nil
	}
	gs := s.grants()

	if strings.Count(token, ".") == 2 {
		claims, err := s.parseOwnJWT(token, "at+jwt")
		if err != nil {
			return nil
		}
		jti, _ := claims["jti"].(string)
		sub, _ := claims["sub"].(string)
		uid, _ := strconv.Atoi(sub)
		if jti == "" || uid == 0 || gs == nil {
			return nil
		}
		rec, err := gs.PeekRefresh(ctx, accessKey(jti))
		if err != nil {
			return nil // revoked or expired
		}
		user, err := a.OAuthGetUser(ctx, uid)
		if err != nil {
			return nil
		}
		ti := &tokenInfo{UserID: uid, Sub: sub, Username: user.UserName, Email: user.Email, Roles: user.Roles,
			UserLevel: user.UserLevel, Scopes: rec.Scopes, ClientID: rec.ClientID, JTI: jti, JWT: true, SID: rec.SessionToken}
		ti.Exp = int64(numberOf(claims["exp"]))
		ti.Iat = int64(numberOf(claims["iat"]))
		ti.JKT, _ = rec.Extra["jkt"].(string)
		ti.Aud = stringsOf(rec.Extra["aud"])
		ti.Claims, _ = rec.Extra["claims"].(map[string]any)
		if len(ti.Aud) == 0 {
			ti.Aud = audOf(claims["aud"])
		}
		return ti
	}

	info, err := a.OAuthIntrospectToken(ctx, token)
	if err != nil || !info.Active {
		return nil
	}
	uid, _ := strconv.Atoi(info.Sub)
	ti := &tokenInfo{UserID: uid, Sub: info.Sub, Username: info.Username, Email: info.Email, Roles: info.Roles,
		UserLevel: info.UserLevel, Exp: info.Exp, Iat: info.Iat, Legacy: true}
	if gs != nil {
		if rec, err := gs.PeekRefresh(ctx, accessKey(token)); err == nil {
			ti.Legacy = false
			ti.Scopes, ti.ClientID, ti.SID = rec.Scopes, rec.ClientID, rec.SessionToken
			ti.JKT, _ = rec.Extra["jkt"].(string)
			ti.Aud = stringsOf(rec.Extra["aud"])
			ti.Claims, _ = rec.Extra["claims"].(map[string]any)
		} else if !errors.Is(err, lookup.ErrRefreshInvalid) {
			return nil
		}
	}
	return ti
}

func audOf(v any) []string {
	switch a := v.(type) {
	case string:
		return []string{a}
	case []any:
		return stringsOf(a)
	}
	return nil
}

// parseOwnJWT verifies a JWT signed by one of this server's keys and returns its claims.
func (s *OAuthServer) parseOwnJWT(token, typ string) (jwt.MapClaims, error) {
	return s.parseOwnJWTOpts(token, typ, false)
}

// parseOwnJWTOpts is parseOwnJWT; allowExpired accepts tokens past their exp (id_token_hint).
func (s *OAuthServer) parseOwnJWTOpts(token, typ string, allowExpired bool) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	opts := []jwt.ParserOption{jwt.WithValidMethods(s.keys.algs()), jwt.WithIssuer(s.cfg.Issuer)}
	if allowExpired {
		opts = append(opts, jwt.WithoutClaimsValidation())
	} else {
		opts = append(opts, jwt.WithExpirationRequired())
	}
	tok, err := jwt.NewParser(opts...).
		ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
			if typ != "" && t.Header["typ"] != typ {
				return nil, errors.New("wrong token type")
			}
			kid, _ := t.Header["kid"].(string)
			pub, k := s.keys.publicFor(kid)
			if k == nil {
				return nil, errors.New("unknown key")
			}
			return pub, nil
		})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	if allowExpired && claims["iss"] != s.cfg.Issuer {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// --------------------------------------------------------------------------
// UserInfo — GET/POST /oauth/userinfo
// --------------------------------------------------------------------------

// bearerFromRequest extracts an access token and its scheme (Bearer or DPoP).
func bearerFromRequest(r *http.Request) (token, scheme string) {
	h := r.Header.Get("Authorization")
	for _, sch := range []string{"Bearer", "DPoP"} {
		if len(h) > len(sch)+1 && strings.EqualFold(h[:len(sch)], sch) && h[len(sch)] == ' ' {
			return strings.TrimSpace(h[len(sch)+1:]), sch
		}
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			if t := r.PostFormValue("access_token"); t != "" {
				return t, "Bearer"
			}
		}
	}
	return "", ""
}

func (s *OAuthServer) userinfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	challenge := func(code, desc string) {
		h := `Bearer error="` + code + `"`
		if s.cfg.EnableDPoP {
			h += `, DPoP algs="ES256 ES384 RS256 PS256"`
		}
		w.Header().Set("WWW-Authenticate", h)
		writeOAuthError(w, code, desc, http.StatusUnauthorized)
	}
	token, scheme := bearerFromRequest(r)
	if token == "" {
		challenge("invalid_token", "missing bearer token")
		return
	}
	ti := s.resolveAccessToken(r.Context(), token)
	if ti == nil {
		challenge("invalid_token", "token is inactive or invalid")
		return
	}
	if e := s.checkDPoPBinding(r, token, scheme, ti); e != nil {
		e.write(w)
		return
	}
	scopes := ti.Scopes
	if ti.Legacy {
		scopes = []string{"openid", "profile", "email"} // a session token without a recorded grant
	} else if !oauthSliceContains(scopes, "openid") {
		w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="openid"`)
		writeOAuthError(w, "insufficient_scope", "the openid scope is required", http.StatusForbidden)
		return
	}

	user := &UserContext{UserName: ti.Username, Email: ti.Email}
	out := s.userClaims(r.Context(), user, ti.UserID, scopes, ti.Claims, "userinfo")
	out["sub"] = ti.Sub

	var client *OAuthServerClient
	if ti.ClientID != "" {
		client, _ = s.lookupOrFetchClient(r.Context(), ti.ClientID)
	}
	if client != nil && client.UserinfoSignedResponseAlg != "" && client.UserinfoSignedResponseAlg != "none" {
		key := s.keys.forAlg(client.UserinfoSignedResponseAlg)
		claims := jwt.MapClaims{"iss": s.cfg.Issuer, "aud": client.ClientID, "iat": time.Now().Unix()}
		for k, v := range out {
			claims[k] = v
		}
		signed, err := key.sign(claims, "")
		if err != nil {
			writeOAuthError(w, "server_error", "could not sign the response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/jwt")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(signed)) //nolint:errcheck,gosec // G104: best-effort write
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// checkDPoPBinding enforces sender-constraining: a DPoP-bound token needs a matching proof and the
// DPoP scheme, and a Bearer-scheme request must not carry a bound token.
func (s *OAuthServer) checkDPoPBinding(r *http.Request, token, scheme string, ti *tokenInfo) *oauthError {
	if ti.JKT == "" {
		if scheme == "DPoP" {
			return &oauthError{Code: "invalid_token", Desc: "token is not DPoP bound", Status: http.StatusUnauthorized,
				WWWAuth: `Bearer error="invalid_token"`}
		}
		return nil
	}
	fail := func(code, desc string) *oauthError {
		return &oauthError{Code: code, Desc: desc, Status: http.StatusUnauthorized,
			WWWAuth: `DPoP error="` + code + `", algs="ES256 ES384 RS256 PS256"`}
	}
	if scheme != "DPoP" {
		return fail("invalid_token", "DPoP-bound tokens must use the DPoP scheme")
	}
	proof, e := s.verifyDPoP(r, token)
	if e != nil {
		return fail("invalid_dpop_proof", e.Desc)
	}
	if proof == nil || proof.JKT != ti.JKT {
		return fail("invalid_dpop_proof", "proof key does not match the token")
	}
	return nil
}

// --------------------------------------------------------------------------
// Resource server helpers
// --------------------------------------------------------------------------

// AccessTokenClaims describes a verified access token.
type AccessTokenClaims struct {
	Subject   string
	UserID    int
	ClientID  string
	Scopes    []string
	Audience  []string
	JTI       string
	SessionID string
	ExpiresAt time.Time
	// DPoPKey is the thumbprint of the key the token is bound to, or "".
	DPoPKey string
	// JWT is true for RFC 9068 JWT access tokens.
	JWT bool
}

// VerifyAccessTokenOptions tunes VerifyAccessToken.
type VerifyAccessTokenOptions struct {
	// Audience, when set, must be one of the token's audiences.
	Audience string
	// Scopes that must all be granted.
	Scopes []string
}

// VerifyAccessToken validates an access token issued by this server (JWT or opaque) against the
// store, so revoked tokens are rejected.
func (s *OAuthServer) VerifyAccessToken(ctx context.Context, token string, opts VerifyAccessTokenOptions) (*AccessTokenClaims, error) {
	ti := s.resolveAccessToken(ctx, token)
	if ti == nil {
		return nil, errors.New("invalid or inactive access token")
	}
	if opts.Audience != "" && !oauthSliceContains(ti.Aud, opts.Audience) {
		return nil, errors.New("access token audience mismatch")
	}
	if !scopesCovered(ti.Scopes, opts.Scopes) {
		return nil, errors.New("insufficient scope")
	}
	return &AccessTokenClaims{
		Subject: ti.Sub, UserID: ti.UserID, ClientID: ti.ClientID, Scopes: ti.Scopes, Audience: ti.Aud, JTI: ti.JTI,
		SessionID: ti.SID, ExpiresAt: time.Unix(ti.Exp, 0), DPoPKey: ti.JKT, JWT: ti.JWT,
	}, nil
}

// introspectionInfo converts a resolved token to the RFC 7662 response.
func (s *OAuthServer) introspectionInfo(ti *tokenInfo) *OAuthTokenInfo {
	info := &OAuthTokenInfo{
		Active: true, Sub: ti.Sub, Username: ti.Username, Email: ti.Email, UserLevel: ti.UserLevel, Roles: ti.Roles,
		Exp: ti.Exp, Iat: ti.Iat, Scope: strings.Join(ti.Scopes, " "), ClientID: ti.ClientID,
		TokenType: ti.tokenType(), Iss: s.cfg.Issuer, Aud: ti.Aud, Jti: ti.JTI,
	}
	if ti.JKT != "" {
		info.Cnf = map[string]any{"jkt": ti.JKT}
	}
	return info
}

func itoa(i int) string { return strconv.Itoa(i) }

func joinScopes(s []string) string { return strings.Join(s, " ") }
