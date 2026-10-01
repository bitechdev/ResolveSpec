# OAuth 2.1 / OpenID Connect Authorization Server

`OAuthServer` turns a `DatabaseAuthenticator` into a standards-based identity provider, and `OIDCConfig` / `WithOIDC` make the same package a relying party for any OpenID Connect provider. This guide covers both. For the older "log in with Google/GitHub" client flow see [OAUTH2.md](OAUTH2.md); a runnable end-to-end wiring is in [`oauth2_full_example.go`](oauth2_full_example.go) (`ExampleOAuth2FullServer`, `ExampleOAuth2FullClient`).

Every feature beyond the original authorization-code flow is **opt-in**: the zero value of each `OAuthServerConfig` option keeps the previous behaviour.

## Contents

1. [Architecture](#architecture)
2. [Quick start](#quick-start)
3. [Endpoints](#endpoints)
4. [Configuration reference](#configuration-reference)
5. [Flows](#flows)
6. [Resource servers](#resource-servers)
7. [Client side: logging in with an OpenID Connect provider](#client-side-relying-party)
8. [Database setup](#database-setup)
9. [Security checklist](#security-checklist)
10. [Not supported](#not-supported)

## Architecture

```
 browser / app ──► OAuthServer.HTTPHandler() ──► DatabaseAuthenticator (users, sessions, login)
                          │
                          └────────────────────► lookup.Provider ──► procedure | direct backend
                                                  oauth_clients, oauth_codes, oauth_consents,
                                                  oauth_refresh_tokens, oauth_device_codes,
                                                  oauth_par_requests, oauth_jti
```

- **State is in the database** (clients, codes, consents, rotating refresh tokens, device codes, pushed requests, the replay cache), so any number of instances can serve one issuer.
- **Stateless pieces** (the SSO cookie, the login/consent form state, the device and logout state) are HMAC-sealed with `CookieSecret`. Instances must share it, or share the first signing key it is derived from.
- **Signing keys**: RSA or ECDSA (P-256/P-384); `kid` is the RFC 7638 thumbprint. All keys are published in the JWKS.
- Access tokens are either the opaque session token (default) or RFC 9068 JWTs. Either way an *access-grant record* stores scope, client, audience and DPoP binding, so introspection, revocation and logout work for both.

## Quick start

```go
auth := security.NewDatabaseAuthenticatorWithOptions(db, security.DatabaseAuthenticatorOptions{})
srv := security.NewOAuthServer(security.OAuthServerConfig{
    Issuer:               "https://auth.example.com",
    PersistClients:       true,
    PersistCodes:         true,
    RequireConsent:       true,
    ManagedRefreshTokens: true,
}, auth)
defer srv.Close()

mux := http.NewServeMux()
mux.Handle("/", srv.HTTPHandler())
```

Register a first-party client from code (no consent screen), or let clients register themselves with `POST /oauth/register`:

```go
client, secret, err := srv.RegisterTrustedClient(ctx, security.OAuthServerClient{
    ClientName:   "Admin console",
    RedirectURIs: []string{"https://console.example.com/callback"},
})
```

## Endpoints

| Method | Path | Spec | Notes |
|---|---|---|---|
| GET | `/.well-known/oauth-authorization-server` | RFC 8414 | Also `/{path}` variants for issuers with a path |
| GET | `/.well-known/openid-configuration` | OIDC Discovery | Same document, plus OIDC fields |
| GET | `/.well-known/oauth-protected-resource` | RFC 9728 | |
| POST | `/oauth/register` | RFC 7591 | Needs `InitialAccessToken` when configured |
| GET PUT DELETE | `/oauth/register/{client_id}` | RFC 7592 | `registration_access_token` as Bearer |
| POST | `/oauth/register/{client_id}/rotate-secret` | RFC 7592 | |
| GET POST | `/oauth/authorize` | RFC 6749, OIDC Core | PKCE S256 required; `response_mode` `query` or `form_post` |
| POST | `/oauth/token` | RFC 6749 | `authorization_code`, `refresh_token`, `client_credentials`, device code, token exchange |
| POST | `/oauth/par` | RFC 9126 | `EnablePAR` / `RequirePAR` |
| POST | `/oauth/device_authorization` | RFC 8628 | `EnableDeviceFlow` |
| GET POST | `/oauth/device` | RFC 8628 | Verification page (login, consent) |
| POST | `/oauth/revoke` | RFC 7009 | Client authentication required |
| POST | `/oauth/introspect` | RFC 7662 | Client authentication required |
| GET POST | `/oauth/userinfo` | OIDC Core | Scope-filtered; signed response if the client asks |
| GET | `/oauth/jwks.json` | RFC 7517 | |
| GET POST | `/oauth/logout` | OIDC RP-Initiated + Back-Channel Logout | |
| GET | `ProviderCallbackPath` | | Callback of a federated upstream provider |

Client authentication methods: `client_secret_basic`, `client_secret_post`, `private_key_jwt` (`jwks` or `jwks_uri`), `none` (public clients).

## Configuration reference

| Option | Default | Purpose |
|---|---|---|
| `Issuer` | required | Public base URL; `iss` of every token. May contain a path |
| `SigningKeys` / `SigningKey` | generated RSA-2048 | Persistent keys for multi-instance and restarts; first signs |
| `CookieSecret` | derived from first key | HMAC key of cookie and form state |
| `SSOCookie` | `resolvespec_sso`, 8h, Lax, Secure when issuer is https | Enables `prompt=none`, `max_age`, single sign-on, logout |
| `PersistClients`, `PersistCodes` | false | Store clients/codes in the DB (needed for several instances) |
| `RequireConsent`, `ConsentTTL` | false, 90 days | Consent screen for non-first-party clients; remembered per user and client |
| `ScopeDescriptions` | built-in for the OIDC scopes | Text on the consent screen |
| `ManagedRefreshTokens`, `RefreshTokenTTL` | false, 30 days | Server-issued rotating refresh tokens with reuse detection |
| `JWTAccessTokens`, `AccessTokenAudience` | false, `ResourceIdentifier` | RFC 9068 access tokens |
| `AccessTokenTTL`, `AuthCodeTTL` | 24h, 2 min | |
| `EnableDPoP` | false | RFC 9449 sender-constrained tokens |
| `EnablePAR`, `RequirePAR`, `PARTTL` | false, false, 90s | |
| `EnableDeviceFlow`, `DeviceCodeTTL`, `DevicePollSeconds` | false, 10 min, 5 | |
| `EnableTokenExchange` | false | RFC 8693 |
| `ClaimsProvider` | `sub`, `preferred_username`, `email` | Source of profile/email/address/phone/custom claims |
| `SupportedACR` | none | Advertised and accepted `acr_values` |
| `DisableLogout` | false | Do not serve `/oauth/logout` |
| `InitialAccessToken` | none | Bearer secret required to register clients |
| `AllowAnonymousIntrospection` | false | Skip client authentication at introspect/revoke |
| `RateLimiter` | none | `func(r, endpoint) bool`; false answers 429 |
| `AllowPrivateNetworkFetch` | false | Allow `jwks_uri` fetches to private addresses (SSRF guard) |
| `LoginTemplate`, `ConsentTemplate` | built-in | `html/template` overrides (`OAuthLoginPage`, `OAuthConsentPage`) |

## Flows

### Authorization code with PKCE

```
GET /oauth/authorize?response_type=code&client_id=ID&redirect_uri=https://app/cb
    &scope=openid%20profile&state=S&nonce=N
    &code_challenge=BASE64URL(SHA256(V))&code_challenge_method=S256
```

The user signs in (a cookie keeps the session), approves the consent screen if required, and is redirected to `redirect_uri?code=…&state=S&iss=<issuer>` (RFC 9207; compare `iss`). Exchange it:

```
curl -X POST https://auth.example.com/oauth/token \
  -d grant_type=authorization_code -d code=CODE -d redirect_uri=https://app/cb \
  -d client_id=ID -d code_verifier=V
```

Request parameters: `prompt` (`none`, `login`, `consent`, `select_account`), `max_age`, `id_token_hint`, `login_hint`, `acr_values`, `claims`, `resource` (RFC 8707), `response_mode=form_post`. Once the `redirect_uri` is validated, errors are returned to the client as redirects (`error`, `state`, `iss`); before that they are shown to the user. `prompt=none` without a session answers `login_required`.

The id_token carries `iss`, `sub`, `aud`, `exp`, `iat`, `nonce`, `auth_time`, `acr`, `amr`, `sid`, `at_hash`, `azp` and the claims the granted scopes entitle the client to.

### Consent and scopes

Requested scopes are intersected with the client's `AllowedScopes`; an empty result is `invalid_scope`. With `RequireConsent` (or per client `require_consent`), a third-party client sees a consent screen unless a stored consent already covers the scopes. A client marked `first_party` (`RegisterTrustedClient`) never does. Approval can be remembered for `ConsentTTL`; a denial redirects with `access_denied`.

### Refresh token rotation

With `ManagedRefreshTokens`, every refresh returns a **new** refresh token and invalidates the old one:

```
curl -X POST …/oauth/token -d grant_type=refresh_token -d refresh_token=R1 -d client_id=ID
```

Presenting a rotated token again (a stolen copy) answers `invalid_grant` and **revokes the whole family**, so the legitimate holder has to sign in again. A refresh may downscope (`scope=`) but never widen. Confidential clients must authenticate. Request `offline_access` or allow the `refresh_token` grant to receive one. Without `ManagedRefreshTokens` the refresh token of the underlying authenticator is passed through as in earlier versions.

### JWT access tokens (RFC 9068)

`JWTAccessTokens` issues `at+jwt` tokens with `iss sub aud exp iat jti client_id scope` (and `cnf` for DPoP). See [Resource servers](#resource-servers).

### DPoP (RFC 9449)

With `EnableDPoP` a client sends a `DPoP` proof header (typ `dpop+jwt`, `htm`, `htu`, `iat`, `jti`, public `jwk`) to the token endpoint. The access and refresh tokens are then bound to the proof key; the response has `token_type: DPoP`. Use them as `Authorization: DPoP <token>` plus a proof carrying `ath = base64url(SHA256(token))`. Proof `jti`s are single use (replay cache in `oauth_jti`). A DPoP-bound token is refused as a Bearer token. Set the client's `dpop_bound_access_tokens` to require proofs.

### Pushed authorization requests (RFC 9126)

```
curl -X POST …/oauth/par -d client_id=ID -d response_type=code … -d code_challenge=…   # → {"request_uri": "urn:ietf:params:oauth:request_uri:…", "expires_in": 90}
GET /oauth/authorize?client_id=ID&request_uri=urn:ietf:params:oauth:request_uri:…
```

Confidential clients authenticate at the PAR endpoint. A `request_uri` is single use. `RequirePAR` rejects plain authorization requests.

### Device grant (RFC 8628)

```
curl -X POST …/oauth/device_authorization -d client_id=ID -d scope=openid
# → device_code, user_code, verification_uri, verification_uri_complete, interval
curl -X POST …/oauth/token -d grant_type=urn:ietf:params:oauth:grant-type:device_code -d device_code=… -d client_id=ID
```

The user opens `verification_uri` on another device, signs in, enters the code and approves. The device polls; answers are `authorization_pending`, `slow_down` (polled faster than `interval`), `access_denied`, `expired_token`. A code is consumed by the first successful poll.

### Token exchange (RFC 8693)

A confidential client holding the `urn:ietf:params:oauth:grant-type:token-exchange` grant swaps a user's access token for a narrower one (`scope`, `audience`/`resource`). The scope can only shrink; DPoP-bound subject tokens and `actor_token` are not accepted.

### Client credentials

Unchanged: `grant_type=client_credentials` with client authentication returns a token for the client itself.

### Dynamic registration (RFC 7591/7592)

`POST /oauth/register` accepts `redirect_uris` (https, loopback http, or a custom scheme; no fragments), `grant_types`, `response_types`, `token_endpoint_auth_method`, `scope`, `jwks` / `jwks_uri`, `client_name`, `client_uri`, `logo_uri`, `contacts`, `post_logout_redirect_uris`, `backchannel_logout_uri`, `id_token_signed_response_alg`, `userinfo_signed_response_alg`, `dpop_bound_access_tokens`. The response includes `registration_access_token` and `registration_client_uri`; use them to read, update or delete the client and to rotate its secret. Secrets are stored hashed and shown once. Loopback redirect URIs ignore the port (RFC 8252).

### Logout

`/oauth/logout?id_token_hint=…&post_logout_redirect_uri=…&state=…` ends the SSO session, revokes the session's tokens and refresh families, and redirects to a `post_logout_redirect_uri` the client registered. Without a hint the user is asked to confirm. Clients with `backchannel_logout_uri` receive a signed `logout_token` (best effort, in the background).

### Federation

`srv.RegisterExternalProvider(auth, "google")` lets users sign in through an upstream provider; the server remains the issuer for your clients and your users get the same tokens, consent and logout behaviour. `login_hint` pre-fills the built-in login form.

## Resource servers

```go
claims, err := srv.VerifyAccessToken(ctx, token, security.VerifyAccessTokenOptions{
    Audience: "https://api.example.com",
    Scopes:   []string{"orders:read"},
})
```

JWT tokens are verified locally against the key set; opaque tokens are looked up. `claims` holds `Subject`, `UserID`, `ClientID`, `Scopes`, `Audience`, `JTI` and the DPoP key thumbprint. For DPoP-bound tokens also check the proof (`verifyDPoP` runs on the server's own endpoints; an external API validates the `DPoP` header and compares `DPoPKey`). A ready-made middleware is in `ExampleOAuth2FullServer`. Services in another process can call `/oauth/introspect` with their client credentials, or verify the JWT with `GET /oauth/jwks.json`.

## Client side (relying party)

`WithOIDC` discovers the endpoints from `{issuer}/.well-known/openid-configuration` and registers a provider with these protections switched on:

- PKCE (S256) and a `nonce`, both kept with the `state` and used once.
- The `id_token` is verified: signature against the provider's JWKS (refetched once when a `kid` is unknown), algorithm allow-list (`RS256 PS256 ES256 ES384` by default), `iss`, `aud`/`azp`, `exp` (1 minute skew), `nonce`, `at_hash`.
- The userinfo `sub` must equal the id_token `sub`; the RFC 9207 `iss` parameter must match.

```go
auth.WithOIDC(ctx, security.OIDCConfig{Issuer: "https://auth.example.com", ClientID: id, ClientSecret: secret,
    RedirectURL: "https://app/auth/callback", ProviderName: "company"})

url, _ := auth.OAuth2GetAuthURLWithOptions("company", state, security.OAuth2AuthOptions{Prompt: "login"})
login, err := auth.OAuth2HandleCallbackRequest(ctx, "company", r)   // r is the callback request
logout, _ := auth.OAuth2LogoutURL(ctx, "company", login.Meta["id_token"].(string), "https://app/", "")
```

`OAuth2HandleCallback(ctx, provider, code, state)` still works. The raw `id_token` is returned in `LoginResponse.Meta["id_token"]` (keep it for the logout hint); `OAuth2RefreshToken` re-validates a new id_token when the provider returns one. The Google preset validates id_tokens as well; for other providers set `Issuer` and `JWKSURL` on `OAuth2Config`, or use `WithOIDC`.

## Database setup

Apply the schema for your backend (see the lookup section of the [README](README.md)):

- Postgres procedures: `lookup/database_schema.sql`
- Table-only (any dialect, direct mode): `lookup/ddl/{postgres,sqlite,mysql,mssql}.sql`

The OAuth additions are the `oauth_clients.metadata` and `oauth_codes.extra` JSON columns plus the tables `oauth_consents`, `oauth_refresh_tokens`, `oauth_device_codes`, `oauth_par_requests` and `oauth_jti`. Existing installs: see [breaking_changes.md](breaking_changes.md#step-8-full-oauth2--openid-connect) for the ALTER statements. Purge expired rows of the new tables periodically (`expires_at < now`).

## Security checklist

- Serve the issuer over HTTPS only; the SSO cookie is `Secure` automatically then.
- Persist `SigningKeys` and share `CookieSecret` across instances.
- Set `InitialAccessToken` unless open dynamic registration is intended.
- Use `ManagedRefreshTokens` for public clients; keep `RequireConsent` on for third-party clients.
- Put a `RateLimiter` in front of `token`, `authorize`, `device` and `register`.
- Only PKCE S256 is accepted; redirect URIs match exactly (except the loopback port).
- Keep `AllowPrivateNetworkFetch` off; `jwks_uri` fetches are SSRF-guarded.
- Authenticate callers of `introspect` and `revoke` (the default).

## Not supported

`client_secret_jwt`, signed request objects (`request` / `request_uri` to a remote JWT), the DPoP server nonce, `c_hash`, encrypted id_tokens, and actor tokens in token exchange. Discovery does not advertise them.
