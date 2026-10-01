package security

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const clientAssertionTypeJWT = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// authedClient is a client that has identified itself at an endpoint.
type authedClient struct {
	Client *OAuthServerClient
	Method string // none, client_secret_basic, client_secret_post, private_key_jwt
}

func invalidClient(desc string, basic bool) *oauthError {
	e := oerr("invalid_client", desc, http.StatusUnauthorized)
	if basic {
		e.WWWAuth = `Basic realm="oauth"`
	}
	return e
}

// authenticateClient identifies the client of a request from client_secret_basic,
// client_secret_post, private_key_jwt or (for public clients) client_id alone. It returns
// (nil, nil) when the request carries nothing that identifies a client.
func (s *OAuthServer) authenticateClient(r *http.Request) (*authedClient, *oauthError) {
	if assertion := r.FormValue("client_assertion"); assertion != "" {
		return s.authenticateAssertion(r, assertion)
	}

	id, secret, basic := r.BasicAuth()
	method := "client_secret_basic"
	if basic {
		// RFC 6749 §2.3.1: the id and secret are form-urlencoded before Base64 encoding.
		if v, err := url.QueryUnescape(id); err == nil {
			id = v
		}
		if v, err := url.QueryUnescape(secret); err == nil {
			secret = v
		}
	} else {
		id, secret = r.FormValue("client_id"), r.FormValue("client_secret")
		method = "client_secret_post"
	}
	if id == "" {
		return nil, nil
	}
	client, ok := s.lookupOrFetchClient(r.Context(), id)
	if !ok {
		return nil, invalidClient("invalid client credentials", basic)
	}
	if secret == "" {
		if basic || needsClientAuth(client) {
			return nil, invalidClient("client authentication required", basic)
		}
		return &authedClient{Client: client, Method: "none"}, nil
	}
	if client.ClientSecretHash == "" ||
		subtle.ConstantTimeCompare([]byte(hashClientSecret(secret)), []byte(client.ClientSecretHash)) != 1 {
		return nil, invalidClient("invalid client credentials", basic)
	}
	if client.ClientSecretExpiresAt != 0 && time.Now().Unix() > client.ClientSecretExpiresAt {
		return nil, invalidClient("client secret expired", basic)
	}
	return &authedClient{Client: client, Method: method}, nil
}

// requireClient is authenticateClient for endpoints that need an identified client.
func (s *OAuthServer) requireClient(r *http.Request) (*authedClient, *oauthError) {
	ac, e := s.authenticateClient(r)
	if e != nil {
		return nil, e
	}
	if ac == nil {
		return nil, invalidClient("client authentication required", false)
	}
	return ac, nil
}

func (s *OAuthServer) authenticateAssertion(r *http.Request, assertion string) (*authedClient, *oauthError) {
	if r.FormValue("client_assertion_type") != clientAssertionTypeJWT {
		return nil, invalidClient("unsupported client_assertion_type", false)
	}
	unverified, _, err := jwt.NewParser().ParseUnverified(assertion, &jwt.RegisteredClaims{})
	if err != nil {
		return nil, invalidClient("malformed client_assertion", false)
	}
	rc, _ := unverified.Claims.(*jwt.RegisteredClaims)
	clientID := rc.Subject
	if formID := r.FormValue("client_id"); formID != "" && formID != clientID {
		return nil, invalidClient("client_id does not match the assertion", false)
	}
	client, ok := s.lookupOrFetchClient(r.Context(), clientID)
	if !ok || client.TokenEndpointAuthMethod != "private_key_jwt" {
		return nil, invalidClient("invalid client credentials", false)
	}
	set, err := s.clientKeySet(r.Context(), client, false)
	if err != nil {
		return nil, invalidClient("client keys are unavailable", false)
	}

	alg := []string{"RS256", "PS256", "ES256", "ES384"}
	if client.TokenEndpointAuthSigningAlg != "" {
		alg = []string{client.TokenEndpointAuthSigningAlg}
	}
	claims := &jwt.RegisteredClaims{}
	verify := func(set *jwkSet) error {
		_, err := verifyJWTWithSet(assertion, set, alg, claims,
			jwt.WithIssuer(clientID), jwt.WithSubject(clientID), jwt.WithExpirationRequired(), jwt.WithLeeway(30*time.Second))
		return err
	}
	if err := verify(set); err != nil && client.JWKSURI != "" {
		// The client may have rotated its keys: refetch once and try again.
		if set, ferr := s.clientKeySet(r.Context(), client, true); ferr == nil {
			err = verify(set)
		}
		if err != nil {
			return nil, invalidClient("client_assertion rejected", false)
		}
	} else if err != nil {
		return nil, invalidClient("client_assertion rejected", false)
	}

	if !s.assertionAudienceOK(claims.Audience, r) {
		return nil, invalidClient("client_assertion audience mismatch", false)
	}
	exp := claims.ExpiresAt.Time
	if exp.After(time.Now().Add(10 * time.Minute)) {
		return nil, invalidClient("client_assertion lifetime too long", false)
	}
	if claims.ID == "" {
		return nil, invalidClient("client_assertion needs a jti", false)
	}
	if seen, err := s.replayed(r.Context(), "cla:"+clientID+":"+claims.ID, exp.Add(time.Minute)); err != nil {
		return nil, serverErr()
	} else if seen {
		return nil, invalidClient("client_assertion replayed", false)
	}
	return &authedClient{Client: client, Method: "private_key_jwt"}, nil
}

func (s *OAuthServer) assertionAudienceOK(aud jwt.ClaimStrings, r *http.Request) bool {
	for _, a := range aud {
		if a == s.cfg.Issuer || a == s.endpoint("/oauth/token") || a == s.requestURL(r) {
			return true
		}
	}
	return false
}

// clientKeySet returns the verification keys of a private_key_jwt client.
func (s *OAuthServer) clientKeySet(ctx context.Context, c *OAuthServerClient, refresh bool) (*jwkSet, error) {
	if len(c.JWKS) > 0 {
		return parseJWKS(c.JWKS)
	}
	return s.jwks.get(ctx, c.JWKSURI, refresh)
}

// replayed records key until expires and reports whether it was seen before.
func (s *OAuthServer) replayed(ctx context.Context, key string, expires time.Time) (bool, error) {
	g := s.grants()
	if g == nil {
		return false, nil
	}
	if len(key) > 250 { // keys are bounded by the jti_key column
		key = hashToken(key)
	}
	return g.SeenJTI(ctx, key, expires)
}

// requestURL is the public URL of the request, built from the issuer so it survives reverse proxies.
func (s *OAuthServer) requestURL(r *http.Request) string {
	path := r.URL.Path
	prefix := ""
	if p := s.issuerURL.Path; p != "" && p != "/" && !hasPathPrefix(path, p) {
		prefix = p
	}
	return s.issuerURL.Scheme + "://" + s.issuerURL.Host + prefix + path
}

func hasPathPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}
