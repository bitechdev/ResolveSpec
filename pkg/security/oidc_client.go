package security

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OIDCConfig configures an OpenID Connect provider found by discovery.
type OIDCConfig struct {
	// Issuer is the provider's issuer URL; /.well-known/openid-configuration is fetched from it.
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes defaults to openid, profile, email.
	Scopes       []string
	ProviderName string // default "oidc"

	// Optional, see OAuth2Config.
	UserInfoParser func(userInfo map[string]any) (*UserContext, error)
	AllowedAlgs    []string
	AuthStyle      string
	HTTPClient     *http.Client
	ClockSkew      time.Duration
}

// oidcProvider is the OpenID Connect part of an OAuth2Provider: discovered endpoints and the
// id_token validator.
type oidcProvider struct {
	issuer      string
	clientID    string
	jwksURL     string
	endSession  string
	algs        []string
	skew        time.Duration
	client      *http.Client
	keys        *jwksCache
	needsLookup bool // endpoints still have to be discovered

	mu sync.Mutex
}

func newOIDCProvider(cfg *OAuth2Config) *oidcProvider {
	p := &oidcProvider{
		issuer:     cfg.Issuer,
		clientID:   cfg.ClientID,
		jwksURL:    cfg.JWKSURL,
		endSession: cfg.EndSessionURL,
		algs:       cfg.AllowedAlgs,
		skew:       cfg.ClockSkew,
		client:     cfg.HTTPClient,
	}
	if len(p.algs) == 0 {
		p.algs = []string{"RS256", "PS256", "ES256", "ES384"}
	}
	if p.skew == 0 {
		p.skew = time.Minute
	}
	if p.client == nil {
		p.client = &http.Client{Timeout: 10 * time.Second}
	}
	p.keys = newJWKSCache(p.client)
	p.needsLookup = cfg.AuthURL == "" || cfg.TokenURL == "" || cfg.JWKSURL == ""
	return p
}

// ensureEndpoints runs discovery once when the endpoints were not configured.
func (p *oidcProvider) ensureEndpoints(ctx context.Context, op *OAuth2Provider) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.needsLookup {
		return nil
	}
	doc, err := fetchOIDCDiscovery(ctx, p.client, p.issuer)
	if err != nil {
		return err
	}
	if doc.Issuer != p.issuer {
		return fmt.Errorf("discovery issuer mismatch: got %q, want %q", doc.Issuer, p.issuer)
	}
	if op.config.Endpoint.AuthURL == "" {
		op.config.Endpoint.AuthURL = doc.AuthorizationEndpoint
	}
	if op.config.Endpoint.TokenURL == "" {
		op.config.Endpoint.TokenURL = doc.TokenEndpoint
	}
	if op.userInfoURL == "" {
		op.userInfoURL = doc.UserinfoEndpoint
	}
	if p.jwksURL == "" {
		p.jwksURL = doc.JWKSURI
	}
	if p.endSession == "" {
		p.endSession = doc.EndSessionEndpoint
	}
	if op.config.Endpoint.AuthURL == "" || op.config.Endpoint.TokenURL == "" || p.jwksURL == "" {
		return errors.New("discovery document lacks authorization, token or jwks endpoint")
	}
	p.needsLookup = false
	return nil
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

func fetchOIDCDiscovery(ctx context.Context, client *http.Client, issuer string) (*oidcDiscovery, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery: status %d", resp.StatusCode)
	}
	var doc oidcDiscovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	return &doc, nil
}

// validateIDToken verifies the signature and the claims of an id_token (OIDC Core 3.1.3.7).
// nonce is checked when non-empty (not on refresh); accessToken is checked against at_hash.
func (p *oidcProvider) validateIDToken(ctx context.Context, raw, nonce, accessToken string) (map[string]any, error) {
	claims := jwt.MapClaims{}
	verify := func(refresh bool) (*jwt.Token, error) {
		set, err := p.keys.get(ctx, p.jwksURL, refresh)
		if err != nil {
			return nil, err
		}
		return verifyJWTWithSet(raw, set, p.algs, claims,
			jwt.WithIssuer(p.issuer), jwt.WithAudience(p.clientID),
			jwt.WithExpirationRequired(), jwt.WithLeeway(p.skew))
	}
	tok, err := verify(false)
	if err != nil {
		// A rotated key: refetch the key set once.
		claims = jwt.MapClaims{}
		if tok, err = verify(true); err != nil {
			return nil, err
		}
	}

	if sub, _ := claims["sub"].(string); sub == "" {
		return nil, errors.New("missing sub")
	}
	// With several audiences azp must name this client.
	if aud, _ := claims.GetAudience(); len(aud) > 1 {
		if azp, _ := claims["azp"].(string); azp != p.clientID {
			return nil, errors.New("azp does not match the client")
		}
	}
	if azp, ok := claims["azp"].(string); ok && azp != p.clientID {
		return nil, errors.New("azp does not match the client")
	}
	if nonce != "" {
		got, _ := claims["nonce"].(string)
		if subtle.ConstantTimeCompare([]byte(got), []byte(nonce)) != 1 {
			return nil, errors.New("nonce mismatch")
		}
	}
	if accessToken != "" {
		if want, ok := claims["at_hash"].(string); ok {
			alg, _ := tok.Header["alg"].(string)
			if halfHash(alg, accessToken) != want {
				return nil, errors.New("at_hash mismatch")
			}
		}
	}
	return claims, nil
}

// WithOIDC registers an OpenID Connect provider. The endpoints come from the issuer's discovery
// document. Login uses PKCE and a nonce, and the id_token is validated on callback and refresh.
func (a *DatabaseAuthenticator) WithOIDC(ctx context.Context, cfg OIDCConfig) (*DatabaseAuthenticator, error) {
	if cfg.Issuer == "" {
		return a, errors.New("OIDC issuer is required")
	}
	if cfg.ProviderName == "" {
		cfg.ProviderName = "oidc"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	doc, err := fetchOIDCDiscovery(ctx, cfg.HTTPClient, cfg.Issuer)
	if err != nil {
		return a, err
	}
	if doc.Issuer != strings.TrimRight(cfg.Issuer, "/") && doc.Issuer != cfg.Issuer {
		return a, fmt.Errorf("discovery issuer mismatch: got %q, want %q", doc.Issuer, cfg.Issuer)
	}
	return a.WithOAuth2(OAuth2Config{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		RedirectURL:    cfg.RedirectURL,
		Scopes:         cfg.Scopes,
		AuthURL:        doc.AuthorizationEndpoint,
		TokenURL:       doc.TokenEndpoint,
		UserInfoURL:    doc.UserinfoEndpoint,
		ProviderName:   cfg.ProviderName,
		UserInfoParser: cfg.UserInfoParser,
		Issuer:         doc.Issuer,
		JWKSURL:        doc.JWKSURI,
		EndSessionURL:  doc.EndSessionEndpoint,
		AllowedAlgs:    cfg.AllowedAlgs,
		AuthStyle:      cfg.AuthStyle,
		HTTPClient:     cfg.HTTPClient,
		ClockSkew:      cfg.ClockSkew,
	}), nil
}

// OAuth2LogoutURL returns the provider's RP-initiated logout URL (OIDC RP-Initiated Logout 1.0).
// idTokenHint is LoginResponse.Meta["id_token"]. It fails when the provider has no end_session_endpoint.
func (a *DatabaseAuthenticator) OAuth2LogoutURL(ctx context.Context, providerName, idTokenHint, postLogoutRedirect, state string) (string, error) {
	provider, err := a.getOAuth2Provider(providerName)
	if err != nil {
		return "", err
	}
	if provider.oidc == nil {
		return "", fmt.Errorf("provider %q is not an OpenID Connect provider", providerName)
	}
	if err := provider.oidc.ensureEndpoints(provider.withHTTPClient(ctx), provider); err != nil {
		return "", err
	}
	provider.oidc.mu.Lock()
	end := provider.oidc.endSession
	provider.oidc.mu.Unlock()
	if end == "" {
		return "", fmt.Errorf("provider %q has no end_session_endpoint", providerName)
	}
	u, err := url.Parse(end)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
		if state != "" {
			q.Set("state", state)
		}
	}
	q.Set("client_id", provider.config.ClientID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
