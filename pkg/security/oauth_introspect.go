package security

import (
	"errors"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// introspectionCaller authenticates the caller of the revocation / introspection endpoints.
func (s *OAuthServer) introspectionCaller(r *http.Request) (*authedClient, *oauthError) {
	ac, e := s.authenticateClient(r)
	if e != nil {
		return nil, e
	}
	if (ac == nil || ac.Method == "none") && !s.cfg.AllowAnonymousIntrospection {
		return nil, invalidClient("client authentication required", true)
	}
	return ac, nil
}

// --------------------------------------------------------------------------
// RFC 7662 — Token introspection
// --------------------------------------------------------------------------

func (s *OAuthServer) introspectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	if _, e := s.introspectionCaller(r); e != nil {
		e.write(w)
		return
	}
	token := r.PostFormValue("token")
	inactive := map[string]any{"active": false}
	if token == "" {
		writeJSON(w, http.StatusOK, inactive)
		return
	}

	if r.PostFormValue("token_type_hint") != "access_token" {
		if gs := s.grants(); gs != nil {
			if rt, err := gs.PeekRefresh(r.Context(), hashToken(token)); err == nil && !isAccessRecord(rt) {
				writeJSON(w, http.StatusOK, map[string]any{
					"active": true, "sub": itoa(rt.UserID), "client_id": rt.ClientID, "scope": joinScopes(rt.Scopes),
					"exp": rt.ExpiresAt.Unix(), "iss": s.cfg.Issuer, "token_type": "refresh_token",
				})
				return
			}
		}
	}
	ti := s.resolveAccessToken(r.Context(), token)
	if ti == nil {
		writeJSON(w, http.StatusOK, inactive)
		return
	}
	writeJSON(w, http.StatusOK, s.introspectionInfo(ti))
}

// --------------------------------------------------------------------------
// RFC 7009 — Token revocation
// --------------------------------------------------------------------------

func (s *OAuthServer) revokeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, "invalid_request", "cannot parse form", http.StatusBadRequest)
		return
	}
	ac, e := s.introspectionCaller(r)
	if e != nil {
		e.write(w)
		return
	}
	token := r.PostFormValue("token")
	if token == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	owns := func(clientID string) bool { return ac == nil || clientID == "" || ac.Client.ClientID == clientID }

	ctx := r.Context()
	if gs := s.grants(); gs != nil {
		if rt, err := gs.PeekRefresh(ctx, hashToken(token)); err == nil && !isAccessRecord(rt) {
			if owns(rt.ClientID) {
				_ = gs.RevokeRefreshFamily(ctx, rt.FamilyID)
			}
			w.WriteHeader(http.StatusOK)
			return
		} else if err != nil && !errors.Is(err, lookup.ErrRefreshInvalid) {
			w.WriteHeader(http.StatusOK)
			return
		}
		if ti := s.resolveAccessToken(ctx, token); ti != nil {
			if owns(ti.ClientID) {
				id := token
				if ti.JWT {
					id = ti.JTI
				}
				_ = gs.RevokeRefreshFamily(ctx, accessKey(id))
				if !ti.JWT && ti.ClientID != "" {
					if a := s.anyAuth(); a != nil {
						_ = a.OAuthRevokeToken(ctx, token)
					}
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	// Tokens issued by earlier versions (session tokens without a recorded grant, pass-through refresh tokens).
	if a := s.anyAuth(); a != nil {
		_ = a.OAuthRevokeToken(ctx, token)
	}
	w.WriteHeader(http.StatusOK)
}
