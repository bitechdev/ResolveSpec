package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// ExampleOAuth2FullServer runs a complete OAuth 2.1 / OpenID Connect provider: login, consent,
// rotating refresh tokens, JWT access tokens, DPoP, PAR, the device grant and token exchange.
// OAUTH2_SERVER.md walks through every endpoint.
func ExampleOAuth2FullServer() {
	db, _ := sql.Open("postgres", "postgres://user:pass@localhost/app?sslmode=disable")

	// 1. The authenticator holds users and sessions. The OAuth state (clients, codes, consents,
	//    refresh tokens, device codes, PAR requests, replay cache) lives in the same database:
	//    apply lookup/database_schema.sql (Postgres procedures) or lookup/ddl/<dialect>.sql.
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{
		Lookup: lookup.Config{},
	})

	// 2. Signing keys are persistent so every instance publishes and accepts the same keys.
	//    The first key signs; add the next key here first when rotating.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader) // load from your secret store instead

	srv := NewOAuthServer(OAuthServerConfig{ //nolint:gosec // example secrets
		Issuer:      "https://auth.example.com",
		SigningKeys: []OAuthSigningKey{{Key: key, Alg: "ES256"}},
		// Share this secret between instances so the SSO cookie works on all of them.
		CookieSecret: []byte("a-32-byte-or-longer-random-secret"),

		PersistClients: true, // clients survive restarts
		PersistCodes:   true, // codes work across instances

		RequireConsent:       true, // ask the user before releasing scopes to a third-party client
		ManagedRefreshTokens: true, // rotate refresh tokens, revoke the family on reuse
		JWTAccessTokens:      true, // RFC 9068: resource servers verify locally
		AccessTokenAudience:  "https://api.example.com",

		EnableDPoP:          true, // RFC 9449 sender-constrained tokens
		EnablePAR:           true, // RFC 9126
		EnableDeviceFlow:    true, // RFC 8628 for TVs and CLIs
		EnableTokenExchange: true, // RFC 8693 downscoping for service calls

		InitialAccessToken: "registration-secret", // only trusted callers may register clients
		ScopeDescriptions:  map[string]string{"orders:read": "Read your orders"},
		RateLimiter: func(r *http.Request, endpoint string) bool {
			return true // plug in your limiter; false answers 429
		},
	}, auth)
	// 3. First-party applications skip the consent screen. The secret is shown once.
	app, secret, err := srv.RegisterTrustedClient(context.Background(), OAuthServerClient{
		ClientName:    "Admin console",
		RedirectURIs:  []string{"https://console.example.com/callback"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		AllowedScopes: []string{"openid", "profile", "email", "offline_access"},
	})
	if err != nil {
		srv.Close()
		log.Fatal(err)
	}
	fmt.Println(app.ClientID, secret)

	mux := http.NewServeMux()
	mux.Handle("/", srv.HTTPHandler())

	// 4. A protected API verifies the JWT access token locally (no database call).
	mux.Handle("/api/orders", requireScope(srv, "orders:read", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(accessClaimsKey{}).(*AccessTokenClaims)
		fmt.Fprintf(w, "orders of %s", claims.Subject)
	})))

	httpSrv := &http.Server{Addr: ":8443", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	err = httpSrv.ListenAndServe()
	srv.Close()
	log.Fatal(err)
}

type accessClaimsKey struct{}

// requireScope is a resource-server middleware around OAuthServer.VerifyAccessToken.
func requireScope(srv *OAuthServer, scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := srv.VerifyAccessToken(r.Context(), token, VerifyAccessTokenOptions{
			Audience: "https://api.example.com",
			Scopes:   []string{scope},
		})
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessClaimsKey{}, claims)))
	})
}

// ExampleOAuth2FullClient is the relying-party side: log users in with any OpenID Connect
// provider (including the server above). Discovery, PKCE, nonce and id_token validation are
// automatic.
func ExampleOAuth2FullClient() {
	db, _ := sql.Open("postgres", "postgres://user:pass@localhost/app?sslmode=disable")
	auth := NewDatabaseAuthenticator(db)

	if _, err := auth.WithOIDC(context.Background(), OIDCConfig{
		Issuer:       "https://auth.example.com",
		ClientID:     "my-client-id",
		ClientSecret: "my-client-secret", // empty for a public client
		RedirectURL:  "https://app.example.com/auth/callback",
		ProviderName: "company",
		Scopes:       []string{"openid", "profile", "email", "offline_access"},
	}); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := auth.OAuth2GenerateState()
		// Keep state in a cookie so the callback can be tied to this browser.
		http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
		maxAge := 3600
		url, err := auth.OAuth2GetAuthURLWithOptions("company", state, OAuth2AuthOptions{MaxAge: &maxAge})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})

	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("oauth_state"); err != nil || c.Value != r.URL.Query().Get("state") {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		// Checks state, PKCE, the RFC 9207 iss parameter, the id_token (signature, iss, aud, exp,
		// nonce, at_hash) and the userinfo subject; then creates the local user and session.
		login, err := auth.OAuth2HandleCallbackRequest(r.Context(), "company", r)
		if err != nil {
			http.Error(w, "login failed", http.StatusUnauthorized)
			return
		}
		idToken, _ := login.Meta["id_token"].(string) // keep it for logout
		http.SetCookie(w, &http.Cookie{Name: "session", Value: login.Token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: "id_token", Value: idToken, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusFound)
	})

	mux.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		hint := ""
		if c, err := r.Cookie("id_token"); err == nil {
			hint = c.Value
		}
		// Ends the provider session too (RP-initiated logout).
		url, err := auth.OAuth2LogoutURL(r.Context(), "company", hint, "https://app.example.com/", "bye")
		if err != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	})

	srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
