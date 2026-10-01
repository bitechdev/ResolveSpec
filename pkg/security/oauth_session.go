package security

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// deriveOAuthSecret derives the HMAC key for cookies and form state from the default signing key.
func deriveOAuthSecret(kr *oauthKeyring) []byte {
	h := sha256.New()
	h.Write([]byte("resolvespec-oauth-state-v1"))
	switch k := kr.keys[0].signer.(type) {
	case *rsa.PrivateKey:
		h.Write(k.D.Bytes())
	case *ecdsa.PrivateKey:
		h.Write(k.D.Bytes())
	}
	return h.Sum(nil)
}

type sealed struct {
	Exp  int64           `json:"e"`
	Kind string          `json:"k"`
	V    json.RawMessage `json:"v"`
}

func (s *OAuthServer) mac(data []byte) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write(data)
	return m.Sum(nil)
}

// seal returns v as a tamper-proof string valid for ttl. kind separates the uses (a sealed login
// form cannot be replayed as a cookie).
func (s *OAuthServer) seal(kind string, v any, ttl time.Duration) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(sealed{Exp: time.Now().Add(ttl).Unix(), Kind: kind, V: raw})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(s.mac(body)), nil
}

// open verifies and decodes a value produced by seal.
func (s *OAuthServer) open(kind, token string, v any) error {
	i := strings.IndexByte(token, '.')
	if i < 0 {
		return fmt.Errorf("malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(token[:i])
	if err != nil {
		return fmt.Errorf("malformed")
	}
	sig, err := base64.RawURLEncoding.DecodeString(token[i+1:])
	if err != nil || !hmac.Equal(sig, s.mac(body)) {
		return fmt.Errorf("bad signature")
	}
	var env sealed
	if err := json.Unmarshal(body, &env); err != nil || env.Kind != kind {
		return fmt.Errorf("malformed")
	}
	if time.Now().Unix() > env.Exp {
		return fmt.Errorf("expired")
	}
	return json.Unmarshal(env.V, v)
}

// ssoSession is the content of the SSO cookie. The login session token never reaches a client.
type ssoSession struct {
	Token    string   `json:"t"` // login session token (user_sessions row)
	UserID   int      `json:"u"`
	AuthTime int64    `json:"a"`
	SID      string   `json:"s"`           // OIDC session id
	Provider string   `json:"p,omitempty"` // external provider that authenticated the user
	Clients  []string `json:"c,omitempty"` // clients that received tokens (back-channel logout)
}

func (s *OAuthServer) cookieSecure() bool {
	return !s.cfg.SSOCookie.Insecure && s.issuerURL.Scheme == "https"
}

// ssoFromRequest returns the live SSO session of the request, or nil.
func (s *OAuthServer) ssoFromRequest(r *http.Request) *ssoSession {
	if s.cfg.SSOCookie.Disable {
		return nil
	}
	c, err := r.Cookie(s.cfg.SSOCookie.Name)
	if err != nil {
		return nil
	}
	var sess ssoSession
	if err := s.open("sso", c.Value, &sess); err != nil {
		return nil
	}
	a := s.anyAuth()
	if a == nil {
		return nil
	}
	if info, err := a.OAuthIntrospectToken(r.Context(), sess.Token); err != nil || !info.Active {
		return nil
	}
	return &sess
}

func (s *OAuthServer) setSSO(w http.ResponseWriter, sess *ssoSession) {
	if s.cfg.SSOCookie.Disable {
		return
	}
	v, err := s.seal("sso", sess, s.cfg.SSOCookie.TTL)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the issuer scheme (cookieSecure)
		Name: s.cfg.SSOCookie.Name, Value: v, Path: s.cfg.SSOCookie.Path,
		MaxAge: int(s.cfg.SSOCookie.TTL.Seconds()), HttpOnly: true, Secure: s.cookieSecure(),
		SameSite: s.cfg.SSOCookie.SameSite,
	})
}

func (s *OAuthServer) clearSSO(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the issuer scheme (cookieSecure)
		Name: s.cfg.SSOCookie.Name, Value: "", Path: s.cfg.SSOCookie.Path, MaxAge: -1,
		HttpOnly: true, Secure: s.cookieSecure(), SameSite: s.cfg.SSOCookie.SameSite,
	})
}

// newSSO builds the session for a freshly authenticated user.
func (s *OAuthServer) newSSO(token string, userID int, provider string) (*ssoSession, error) {
	sid, err := randomOAuthToken()
	if err != nil {
		return nil, err
	}
	return &ssoSession{Token: token, UserID: userID, AuthTime: time.Now().Unix(), SID: sid[:22], Provider: provider}, nil
}

// noteClient records that clientID received tokens in this SSO session.
func (s *OAuthServer) noteClient(w http.ResponseWriter, sess *ssoSession, clientID string) {
	if sess == nil || oauthSliceContains(sess.Clients, clientID) || len(sess.Clients) >= 12 {
		return
	}
	sess.Clients = append(sess.Clients, clientID)
	s.setSSO(w, sess)
}
