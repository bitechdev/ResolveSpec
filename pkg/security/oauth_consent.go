package security

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

var builtinScopeDescriptions = map[string]string{
	"openid":         "Verify your identity",
	"profile":        "Read your username and profile",
	"email":          "Read your email address",
	"offline_access": "Stay signed in and refresh access without asking again",
}

func (s *OAuthServer) scopeInfos(scopes []string) []OAuthScopeInfo {
	out := make([]OAuthScopeInfo, 0, len(scopes))
	for _, sc := range scopes {
		d := s.cfg.ScopeDescriptions[sc]
		if d == "" {
			d = builtinScopeDescriptions[sc]
		}
		out = append(out, OAuthScopeInfo{Name: sc, Description: d})
	}
	return out
}

func scopesCovered(have, want []string) bool {
	for _, w := range want {
		if !oauthSliceContains(have, w) {
			return false
		}
	}
	return true
}

// consentRequired reports whether the user has to approve the request on the consent screen.
func (s *OAuthServer) consentRequired(ctx context.Context, req *authzRequest, client *OAuthServerClient, userID int) (bool, error) {
	if client.FirstParty || req.ConsentDone {
		return false, nil
	}
	if !s.cfg.RequireConsent && !client.RequireConsent {
		return false, nil
	}
	if req.hasPrompt("consent") {
		return true, nil
	}
	g := s.grants()
	if g == nil {
		return true, nil
	}
	c, err := g.GetConsent(ctx, userID, client.ClientID)
	if errors.Is(err, lookup.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !scopesCovered(c.Scopes, req.Scopes), nil
}

func (s *OAuthServer) renderConsent(w http.ResponseWriter, r *http.Request, req *authzRequest, client *OAuthServerClient, sso *ssoSession) {
	req.Bind = sso.SID
	req.LoginDone = true
	if s.cfg.SSOCookie.Disable {
		req.Sess = sso
	}
	state, err := s.sealRequest(w, r, req)
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
			if user == "" {
				user = info.Email
			}
		}
	}
	s.renderHTML(w, http.StatusOK, s.tmpl.consent, "consent", OAuthConsentPage{
		Title: "Authorize " + name, Action: "authorize", State: state, ClientName: name,
		ClientURI: safeWebURL(client.ClientURI), LogoURI: safeWebURL(client.LogoURI),
		Scopes: s.scopeInfos(req.Scopes), User: user,
	})
}

// safeWebURL returns u when it is an http(s) URL, so client-supplied links cannot be javascript: URLs.
func safeWebURL(u string) string {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return u
	}
	return ""
}

// consentSubmit handles the consent form.
func (s *OAuthServer) consentSubmit(w http.ResponseWriter, r *http.Request, req *authzRequest) {
	sso := s.ssoFromRequest(r)
	if sso == nil && req.Sess != nil {
		if a := s.anyAuth(); a != nil {
			if info, err := a.OAuthIntrospectToken(r.Context(), req.Sess.Token); err == nil && info.Active {
				sso = req.Sess
			}
		}
	}
	if sso == nil || req.Bind == "" || req.Bind != sso.SID {
		s.renderMessage(w, http.StatusBadRequest, "Session expired", "Your session has expired. Please start again from the application.", true)
		return
	}
	if r.PostFormValue("decision") != "allow" {
		s.authzRedirectError(w, r, req, "access_denied", "the user denied the request")
		return
	}
	if g := s.grants(); g != nil && r.PostFormValue("remember") == "1" {
		scopes := req.Scopes
		if old, err := g.GetConsent(r.Context(), sso.UserID, req.ClientID); err == nil {
			for _, sc := range old.Scopes {
				if !oauthSliceContains(scopes, sc) {
					scopes = append(scopes, sc)
				}
			}
		}
		if err := g.SaveConsent(r.Context(), lookup.Consent{
			UserID: sso.UserID, ClientID: req.ClientID, Scopes: scopes, ExpiresAt: time.Now().Add(s.cfg.ConsentTTL),
		}); err != nil {
			s.authzRedirectError(w, r, req, "server_error", "could not store the consent")
			return
		}
	}
	req.ConsentDone = true
	s.issueCode(w, r, req, sso)
}
