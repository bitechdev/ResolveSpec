package security

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// newRefreshToken stores a new managed refresh token (the first of a family) and returns it.
func (s *OAuthServer) newRefreshToken(ctx context.Context, g *tokenGrant, now time.Time) (string, *oauthError) {
	gs := s.grants()
	if gs == nil {
		return "", serverErr()
	}
	rt, err := randomOAuthToken()
	if err != nil {
		return "", serverErr()
	}
	family := g.FamilyID
	if family == "" {
		if family, err = randomOAuthToken(); err != nil {
			return "", serverErr()
		}
		family = family[:30]
	}
	extra := map[string]any{}
	if g.AuthTime != 0 {
		extra["auth_time"] = g.AuthTime
	}
	if g.ACR != "" {
		extra["acr"] = g.ACR
	}
	if len(g.AMR) > 0 {
		extra["amr"] = g.AMR
	}
	if len(g.Resource) > 0 {
		extra["aud"] = g.Resource
	}
	if g.DPoPJKT != "" {
		extra["dpop_jkt"] = g.DPoPJKT
	}
	if len(g.Claims) > 0 {
		extra["claims"] = g.Claims
	}
	if err := gs.SaveRefresh(ctx, lookup.RefreshToken{
		TokenHash: hashToken(rt), FamilyID: family, ClientID: g.Client.ClientID, UserID: g.UserID,
		SessionToken: g.SID, Scopes: g.Scopes, Extra: extra, ExpiresAt: now.Add(s.cfg.RefreshTokenTTL),
	}); err != nil {
		return "", serverErr()
	}
	return rt, nil
}

func (s *OAuthServer) handleRefreshGrant(r *http.Request) (map[string]any, *oauthError) {
	refreshToken := r.PostFormValue("refresh_token")
	if refreshToken == "" {
		return nil, oerr("invalid_request", "refresh_token required", http.StatusBadRequest)
	}

	if s.cfg.ManagedRefreshTokens {
		resp, e, handled := s.handleManagedRefresh(r, refreshToken)
		if handled {
			return resp, e
		}
		// Not one of ours: it may be a pass-through token issued before ManagedRefreshTokens was enabled.
	}
	return s.handleLegacyRefresh(r, refreshToken)
}

// handleManagedRefresh rotates a server-issued refresh token. handled is false when the token is
// not known to the store.
func (s *OAuthServer) handleManagedRefresh(r *http.Request, refreshToken string) (map[string]any, *oauthError, bool) {
	ctx := r.Context()
	gs := s.grants()
	if gs == nil {
		return nil, nil, false
	}
	hash := hashToken(refreshToken)
	peek, err := gs.PeekRefresh(ctx, hash)
	if errors.Is(err, lookup.ErrRefreshInvalid) || (err == nil && isAccessRecord(peek)) {
		return nil, nil, false
	}
	if err != nil {
		return nil, serverErr(), true
	}

	ac, e := s.requireClient(r)
	if e != nil {
		return nil, e, true
	}
	client := ac.Client
	if peek.ClientID != client.ClientID {
		return nil, oerr("invalid_grant", "refresh token was issued to another client", http.StatusBadRequest), true
	}
	if !grantAllowed(client, "refresh_token") && !oauthSliceContains(peek.Scopes, "offline_access") {
		return nil, oerr("unauthorized_client", "client may not use refresh_token", http.StatusBadRequest), true
	}

	scopes := peek.Scopes
	if req := strings.Fields(r.PostFormValue("scope")); len(req) > 0 {
		if !scopesCovered(peek.Scopes, req) {
			return nil, oerr("invalid_scope", "scope exceeds the original grant", http.StatusBadRequest), true
		}
		scopes = req
	}

	bound, _ := peek.Extra["dpop_jkt"].(string)
	proof, e := s.verifyDPoP(r, "")
	if e != nil {
		return nil, e, true
	}
	jkt := bound
	switch {
	case bound != "" && (proof == nil || proof.JKT != bound):
		return nil, oerr("invalid_dpop_proof", "the refresh token is bound to another DPoP key", http.StatusBadRequest), true
	case proof != nil:
		jkt = proof.JKT
	case client.DPoPBoundAccessTokens:
		return nil, oerr("invalid_dpop_proof", "this client must present a DPoP proof", http.StatusBadRequest), true
	}

	next, err := randomOAuthToken()
	if err != nil {
		return nil, serverErr(), true
	}
	extra := map[string]any{}
	for k, v := range peek.Extra {
		extra[k] = v
	}
	if jkt != "" && bound == "" && proof != nil && ac.Method == "none" {
		extra["dpop_jkt"] = jkt // a public client's first DPoP refresh binds the token
	}
	old, err := gs.RotateRefresh(ctx, hash, lookup.RefreshToken{
		TokenHash: hashToken(next), Scopes: peek.Scopes, Extra: extra, ExpiresAt: peek.ExpiresAt,
	})
	switch {
	case errors.Is(err, lookup.ErrRefreshReused):
		return nil, oerr("invalid_grant", "refresh token reuse detected; the session was ended", http.StatusBadRequest), true
	case errors.Is(err, lookup.ErrRefreshInvalid):
		return nil, oerr("invalid_grant", "refresh token expired or revoked", http.StatusBadRequest), true
	case err != nil:
		return nil, serverErr(), true
	}

	g := &tokenGrant{
		Client: client, UserID: old.UserID, Scopes: scopes, SID: old.SessionToken, DPoPJKT: jkt,
		NextRefresh: next, IDToken: true,
	}
	g.AuthTime = int64(numberOf(old.Extra["auth_time"]))
	g.ACR, _ = old.Extra["acr"].(string)
	g.AMR = stringsOf(old.Extra["amr"])
	g.Resource = stringsOf(old.Extra["aud"])
	if cl, ok := old.Extra["claims"].(map[string]any); ok {
		g.Claims = cl
	}
	resp, e := s.mintTokens(ctx, g)
	return resp, e, true
}

func numberOf(v any) float64 {
	f, _ := v.(float64)
	return f
}

func stringsOf(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, it := range list {
		if str, ok := it.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// handleLegacyRefresh passes the token through to the authenticators (the behaviour of earlier versions).
func (s *OAuthServer) handleLegacyRefresh(r *http.Request, refreshToken string) (map[string]any, *oauthError) {
	providerName := r.PostFormValue("provider")
	clientID := r.PostFormValue("client_id")
	ac, e := s.authenticateClient(r)
	if e != nil {
		return nil, e
	}
	var client *OAuthServerClient
	if ac != nil {
		client = ac.Client
	} else if c, ok := s.lookupOrFetchClient(r.Context(), clientID); ok {
		client = c
	}
	if client == nil {
		client = &OAuthServerClient{ClientID: clientID}
	}

	var resp *LoginResponse
	var err error
	if provider := s.providerByName(providerName); provider != nil {
		resp, err = provider.auth.OAuth2RefreshToken(r.Context(), refreshToken, providerName)
	} else if s.auth != nil {
		resp, err = s.auth.RefreshToken(r.Context(), refreshToken)
	} else {
		return nil, oerr("invalid_grant", "no provider available for refresh", http.StatusBadRequest)
	}
	if err != nil {
		return nil, oerr("invalid_grant", err.Error(), http.StatusBadRequest)
	}
	return s.mintTokens(r.Context(), &tokenGrant{
		Client: client, LegacyAccess: resp.Token, LegacyRefresh: resp.RefreshToken,
	})
}
