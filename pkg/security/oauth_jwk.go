package security

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OAuthSigningKey is a key the authorization server signs tokens with. The first configured
// key is the default; the others are published in the JWKS so tokens signed before a rotation
// stay verifiable, and a client can ask for one by id_token_signed_response_alg.
type OAuthSigningKey struct {
	// ID is the JWKS "kid". Derived from the public key (RFC 7638 thumbprint) when empty.
	ID string
	// Key is an *rsa.PrivateKey (RS256) or an *ecdsa.PrivateKey (ES256 for P-256, ES384 for P-384).
	Key crypto.Signer
	// Alg overrides the algorithm inferred from Key (RS256, PS256, ES256, ES384).
	Alg string
}

type oauthKey struct {
	id     string
	alg    string
	signer crypto.Signer
	method jwt.SigningMethod
}

// oauthKeyring holds the server's signing keys.
type oauthKeyring struct {
	keys []oauthKey
}

func newOAuthKeyring(cfg *OAuthServerConfig) (*oauthKeyring, error) {
	in := cfg.SigningKeys
	if len(in) == 0 && cfg.SigningKey != nil {
		in = []OAuthSigningKey{{Key: cfg.SigningKey}}
	}
	if len(in) == 0 {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate signing key: %w", err)
		}
		in = []OAuthSigningKey{{Key: k}}
	}
	kr := &oauthKeyring{}
	for _, k := range in {
		if k.Key == nil {
			return nil, fmt.Errorf("signing key without a private key")
		}
		alg := k.Alg
		if alg == "" {
			switch key := k.Key.(type) {
			case *rsa.PrivateKey:
				alg = "RS256"
			case *ecdsa.PrivateKey:
				switch key.Curve {
				case elliptic.P256():
					alg = "ES256"
				case elliptic.P384():
					alg = "ES384"
				default:
					return nil, fmt.Errorf("unsupported signing curve %s", key.Curve.Params().Name)
				}
			default:
				return nil, fmt.Errorf("unsupported signing key type %T", k.Key)
			}
		}
		method := jwt.GetSigningMethod(alg)
		if method == nil {
			return nil, fmt.Errorf("unsupported signing algorithm %q", alg)
		}
		id := k.ID
		if id == "" {
			tp, err := jwkThumbprint(k.Key.Public())
			if err != nil {
				return nil, err
			}
			id = tp[:16]
		}
		kr.keys = append(kr.keys, oauthKey{id: id, alg: alg, signer: k.Key, method: method})
	}
	return kr, nil
}

// forAlg returns the first key signing with alg, or the default key when alg is empty or unknown.
func (kr *oauthKeyring) forAlg(alg string) *oauthKey {
	if alg != "" {
		for i := range kr.keys {
			if kr.keys[i].alg == alg {
				return &kr.keys[i]
			}
		}
	}
	return &kr.keys[0]
}

func (kr *oauthKeyring) algs() []string {
	var out []string
	for _, k := range kr.keys {
		if !oauthSliceContains(out, k.alg) {
			out = append(out, k.alg)
		}
	}
	return out
}

func (kr *oauthKeyring) jwks() []map[string]any {
	out := make([]map[string]any, 0, len(kr.keys))
	for _, k := range kr.keys {
		if jwk, err := jwkFromPublic(k.signer.Public(), k.id, k.alg); err == nil {
			out = append(out, jwk)
		}
	}
	return out
}

// publicFor returns the public key with kid (or the default one when kid is empty).
func (kr *oauthKeyring) publicFor(kid string) (crypto.PublicKey, *oauthKey) {
	for i := range kr.keys {
		if kid == "" || kr.keys[i].id == kid {
			return kr.keys[i].signer.Public(), &kr.keys[i]
		}
	}
	return nil, nil
}

// sign signs claims with k, adding typ when non-empty.
func (k *oauthKey) sign(claims jwt.Claims, typ string) (string, error) {
	t := jwt.NewWithClaims(k.method, claims)
	t.Header["kid"] = k.id
	if typ != "" {
		t.Header["typ"] = typ
	}
	return t.SignedString(k.signer)
}

// --- JWK encoding ----------------------------------------------------------------------------

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func jwkFromPublic(pub crypto.PublicKey, kid, alg string) (map[string]any, error) {
	jwk := map[string]any{"use": "sig"}
	if kid != "" {
		jwk["kid"] = kid
	}
	if alg != "" {
		jwk["alg"] = alg
	}
	switch k := pub.(type) {
	case *rsa.PublicKey:
		jwk["kty"] = "RSA"
		jwk["n"] = b64u(k.N.Bytes())
		jwk["e"] = b64u(big.NewInt(int64(k.E)).Bytes())
	case *ecdsa.PublicKey:
		size := (k.Curve.Params().BitSize + 7) / 8
		jwk["kty"] = "EC"
		jwk["crv"] = k.Curve.Params().Name
		jwk["x"] = b64u(k.X.FillBytes(make([]byte, size)))
		jwk["y"] = b64u(k.Y.FillBytes(make([]byte, size)))
	default:
		return nil, fmt.Errorf("unsupported key type %T", pub)
	}
	return jwk, nil
}

// jwkThumbprint is the RFC 7638 SHA-256 thumbprint (base64url).
func jwkThumbprint(pub crypto.PublicKey) (string, error) {
	var canonical string
	switch k := pub.(type) {
	case *rsa.PublicKey:
		canonical = fmt.Sprintf(`{"e":%q,"kty":"RSA","n":%q}`, b64u(big.NewInt(int64(k.E)).Bytes()), b64u(k.N.Bytes()))
	case *ecdsa.PublicKey:
		size := (k.Curve.Params().BitSize + 7) / 8
		canonical = fmt.Sprintf(`{"crv":%q,"kty":"EC","x":%q,"y":%q}`, k.Curve.Params().Name,
			b64u(k.X.FillBytes(make([]byte, size))), b64u(k.Y.FillBytes(make([]byte, size))))
	default:
		return "", fmt.Errorf("unsupported key type %T", pub)
	}
	sum := sha256.Sum256([]byte(canonical))
	return b64u(sum[:]), nil
}

func publicFromJWK(m map[string]any) (crypto.PublicKey, error) {
	str := func(k string) string { s, _ := m[k].(string); return s }
	dec := func(k string) (*big.Int, error) {
		raw, err := base64.RawURLEncoding.DecodeString(str(k))
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("invalid JWK member %q", k)
		}
		return new(big.Int).SetBytes(raw), nil
	}
	switch str("kty") {
	case "RSA":
		n, err := dec("n")
		if err != nil {
			return nil, err
		}
		e, err := dec("e")
		if err != nil {
			return nil, err
		}
		if n.BitLen() < 2048 || !e.IsInt64() || e.Int64() < 3 {
			return nil, fmt.Errorf("RSA key too weak")
		}
		return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
	case "EC":
		var curve elliptic.Curve
		switch str("crv") {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		default:
			return nil, fmt.Errorf("unsupported curve %q", str("crv"))
		}
		x, err := dec("x")
		if err != nil {
			return nil, err
		}
		y, err := dec("y")
		if err != nil {
			return nil, err
		}
		pub := &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
		if _, err := pub.ECDH(); err != nil { // rejects points that are not on the curve
			return nil, fmt.Errorf("invalid EC point: %w", err)
		}
		return pub, nil
	}
	return nil, fmt.Errorf("unsupported key type %q", str("kty"))
}

// jwkEntry is one verification key of a JWK set.
type jwkEntry struct {
	kid string
	alg string
	use string
	pub crypto.PublicKey
}

// jwkSet is a parsed JWK set.
type jwkSet struct{ keys []jwkEntry }

func parseJWKS(data []byte) (*jwkSet, error) {
	var doc struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid JWKS: %w", err)
	}
	set := &jwkSet{}
	for _, k := range doc.Keys {
		pub, err := publicFromJWK(k)
		if err != nil {
			continue // skip keys of a type we cannot use
		}
		e := jwkEntry{pub: pub}
		e.kid, _ = k["kid"].(string)
		e.alg, _ = k["alg"].(string)
		e.use, _ = k["use"].(string)
		set.keys = append(set.keys, e)
	}
	if len(set.keys) == 0 {
		return nil, fmt.Errorf("JWKS contains no usable keys")
	}
	return set, nil
}

// candidates returns the keys that may have produced a token with kid and alg.
func (s *jwkSet) candidates(kid, alg string) []crypto.PublicKey {
	var out []crypto.PublicKey
	for _, k := range s.keys {
		if k.use != "" && k.use != "sig" {
			continue
		}
		if kid != "" && k.kid != "" && k.kid != kid {
			continue
		}
		if k.alg != "" && alg != "" && k.alg != alg {
			continue
		}
		out = append(out, k.pub)
	}
	return out
}

// verifyJWTWithSet verifies token against the set. allowed lists the accepted algorithms.
func verifyJWTWithSet(token string, set *jwkSet, allowed []string, claims jwt.Claims, opts ...jwt.ParserOption) (*jwt.Token, error) {
	opts = append([]jwt.ParserOption{jwt.WithValidMethods(allowed)}, opts...)
	parser := jwt.NewParser(opts...)
	lastErr := fmt.Errorf("no matching key")
	unverified, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	kid, _ := unverified.Header["kid"].(string)
	alg, _ := unverified.Header["alg"].(string)
	for _, pub := range set.candidates(kid, alg) {
		tok, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return pub, nil })
		if err == nil {
			return tok, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// --- remote JWKS fetching --------------------------------------------------------------------

// jwksCache fetches and caches remote JWK sets.
type jwksCache struct {
	client *http.Client
	ttl    time.Duration

	mu      sync.Mutex
	entries map[string]jwksCacheEntry
}

type jwksCacheEntry struct {
	set     *jwkSet
	fetched time.Time
}

func newJWKSCache(client *http.Client) *jwksCache {
	return &jwksCache{client: client, ttl: time.Hour, entries: map[string]jwksCacheEntry{}}
}

// get returns the set at uri. refresh forces a fetch (used when a kid is not in the cached set),
// rate limited to one fetch per 30 seconds per URI.
func (c *jwksCache) get(ctx context.Context, uri string, refresh bool) (*jwkSet, error) {
	c.mu.Lock()
	e, ok := c.entries[uri]
	c.mu.Unlock()
	age := time.Since(e.fetched)
	if ok && age < c.ttl && (!refresh || age < 30*time.Second) {
		return e.set, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if ok {
			return e.set, nil // keep using the stale set while the endpoint is down
		}
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch JWKS: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	set, err := parseJWKS(body)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[uri] = jwksCacheEntry{set: set, fetched: time.Now()}
	c.mu.Unlock()
	return set, nil
}

// publicHTTPClient returns an HTTP client for fetching URLs supplied by clients. It refuses to
// connect to loopback, private and link-local addresses (SSRF) unless allowPrivate is set.
func publicHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
				ip.IsUnspecified() || ip.IsMulticast() {
				return fmt.Errorf("address %s is not allowed", host)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
