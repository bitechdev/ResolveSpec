package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// dpopProof is a validated RFC 9449 proof.
type dpopProof struct {
	JKT string // base64url SHA-256 thumbprint of the proof key
}

// verifyDPoP validates the DPoP header of r. It returns (nil, nil) when the request has none.
// accessToken, when non-empty, is the token the proof must be bound to (the "ath" claim).
func (s *OAuthServer) verifyDPoP(r *http.Request, accessToken string) (*dpopProof, *oauthError) {
	vals := r.Header.Values("DPoP")
	if len(vals) == 0 {
		return nil, nil
	}
	bad := func(desc string) (*dpopProof, *oauthError) {
		return nil, oerr("invalid_dpop_proof", desc, http.StatusBadRequest)
	}
	if !s.cfg.EnableDPoP {
		return bad("DPoP is not enabled")
	}
	if len(vals) != 1 {
		return bad("exactly one DPoP header is required")
	}

	var jkt string
	claims := &struct {
		jwt.RegisteredClaims
		HTM string `json:"htm"`
		HTU string `json:"htu"`
		ATH string `json:"ath"`
	}{}
	tok, err := jwt.NewParser(jwt.WithValidMethods([]string{"ES256", "ES384", "RS256", "PS256"})).
		ParseWithClaims(vals[0], claims, func(t *jwt.Token) (any, error) {
			if t.Header["typ"] != "dpop+jwt" {
				return nil, jwt.ErrTokenUnverifiable
			}
			m, ok := t.Header["jwk"].(map[string]any)
			if !ok {
				return nil, jwt.ErrTokenUnverifiable
			}
			if _, private := m["d"]; private {
				return nil, jwt.ErrTokenUnverifiable
			}
			pub, err := publicFromJWK(m)
			if err != nil {
				return nil, err
			}
			if jkt, err = jwkThumbprint(pub); err != nil {
				return nil, err
			}
			return pub, nil
		})
	if err != nil || !tok.Valid {
		return bad("proof signature or header invalid")
	}
	if claims.HTM != r.Method {
		return bad("htm does not match the request method")
	}
	if !sameURLNoQuery(claims.HTU, s.requestURL(r)) {
		return bad("htu does not match the request URL")
	}
	if claims.IssuedAt == nil || claims.ID == "" {
		return bad("iat and jti are required")
	}
	iat := claims.IssuedAt.Time
	if d := time.Since(iat); d > 2*time.Minute || d < -time.Minute {
		return bad("proof is not fresh")
	}
	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		if subtle.ConstantTimeCompare([]byte(b64u(sum[:])), []byte(claims.ATH)) != 1 {
			return bad("ath does not match the access token")
		}
	}
	seen, err := s.replayed(r.Context(), "dpop:"+jkt+":"+claims.ID, iat.Add(3*time.Minute))
	if err != nil {
		return nil, serverErr()
	}
	if seen {
		return bad("proof replayed")
	}
	return &dpopProof{JKT: jkt}, nil
}

func sameURLNoQuery(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	norm := func(u *url.URL) string {
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.EscapedPath()
	}
	return norm(ua) == norm(ub)
}
