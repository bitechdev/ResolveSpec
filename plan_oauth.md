# Complete the OAuth2 / discovery spec in pkg/security

## Context

`pkg/security/oauth_server.go` already implements a solid OAuth 2.1 authorization server: PKCE-only authorization code grant, refresh token grant, RFC 7591 dynamic client registration, RFC 7009 revocation, RFC 7662 introspection, and RFC 8414 authorization-server metadata discovery. The user asked to close the remaining gaps identified in review:

1. **Client authentication** (`client_secret_basic` / `client_secret_post`) — today every registered client is a public client; there is no `client_secret` at all.
2. **`client_credentials` grant** (RFC 6749 §4.4) — machine-to-machine tokens with no end user.
3. **RFC 9728** OAuth Protected Resource Metadata (`/.well-known/oauth-protected-resource`).
4. **OIDC-flavored discovery + `id_token`** (`/.well-known/openid-configuration`, JWKS, `id_token` issuance) — not core RFC 6749/8414 but explicitly requested.

The hard architectural constraint (found during exploration): every access token in this codebase is an opaque string backed by a `user_sessions` row with a `user_id`, and `RLS`-scoping hooks (`pkg/resolvespec/handler.go`, `pkg/restheadspec/handler.go`) derive `UserContext` from that row via `OAuthIntrospectToken`. There is no JWT signing/validation live anywhere (`JWTAuthenticator` in `providers.go` is a stub). To keep `client_credentials` tokens compatible with the existing RLS pipeline without touching hooks.go or the introspection contract, client-credentials tokens will be backed by a **synthetic service-account user row**, reusing the *existing* get-or-create-user-by-email + create-session stored procedures/direct-mode functions that already exist for social login (`oauth2GetOrCreateUser` / `oauth2CreateSession` in `oauth2_methods.go`, same package, unexported but directly callable from `oauth_server.go`). This means **no new tables and only two new columns** are needed for the whole feature set.

## 1. Client authentication (`client_secret_basic` / `client_secret_post`)

**Schema** (`database_schema.sql` `oauth_clients` table ~line 1593, and `database_schema_sqlite.sql` ~line 96): add
```sql
client_secret_hash TEXT,
token_endpoint_auth_method VARCHAR(30) DEFAULT 'none'
```
`resolvespec_oauth_register_client` (database_schema.sql:1628) already returns `to_jsonb(oauth_clients.*)`, so the new columns automatically appear in the client JSON with no extra procedure change beyond the INSERT column/value list.

**Go structs**: add `ClientSecretHash string \`json:"client_secret_hash,omitempty"\`` and `TokenEndpointAuthMethod string \`json:"token_endpoint_auth_method,omitempty"\`` to `OAuthServerClient` (`oauth_server_db.go:11`) and to `oauthClient` (`oauth_server.go:50`).

**Persistence**:
- `oauth_server_db_direct.go` `oauthRegisterClientDirect`: extend INSERT to include the two new columns; `oauthGetClientDirect`: extend SELECT + Scan.
- Procedure mode needs no new function — only the `INSERT` column/value list inside `resolvespec_oauth_register_client` changes.

**Secret issuance** (`registerHandler`, `oauth_server.go:243`): decode an optional `token_endpoint_auth_method` field on the registration request. If it is `client_secret_basic`/`client_secret_post`, or the requested `grant_types` includes `client_credentials` (which forces confidential — see §2), generate a secret with the existing `randomOAuthToken()`, store `sha256(secret)` hex in `client_secret_hash`, and return the **plaintext** `client_secret` + `client_secret_expires_at: 0` once in the registration response only (never persisted in plaintext, never returned by `GET`/introspection). Default `token_endpoint_auth_method` is `"none"` (today's public-client behavior, unchanged).

**Enforcement**: add `authenticateClient(r *http.Request) (*oauthClient, error)` helper: reads `Authorization: Basic base64(client_id:client_secret)` or `client_id`/`client_secret` form fields, looks up the client, and does `subtle.ConstantTimeCompare` against `sha256(provided)` vs `ClientSecretHash`. In `handleAuthCodeGrant` and the new `handleClientCredentialsGrant`, if the resolved client has a non-empty `ClientSecretHash`, require successful `authenticateClient`; if empty (public client), keep today's behavior (PKCE only, no secret check) — fully backward compatible.

**Metadata**: `metadataHandler` (`oauth_server.go:220`) — change `token_endpoint_auth_methods_supported` to `["none","client_secret_basic","client_secret_post"]`.

## 2. `client_credentials` grant

- `tokenHandler` switch (`oauth_server.go:575`): add `case "client_credentials": s.handleClientCredentialsGrant(w, r)`.
- New `handleClientCredentialsGrant`:
  1. Authenticate the client via `authenticateClient` (mandatory — RFC 6749 §4.4 requires a confidential client). No secret ⇒ `invalid_client`.
  2. Confirm `"client_credentials"` is in the client's registered `GrantTypes` ⇒ else `unauthorized_client`.
  3. Compute effective scopes = requested `scope` form value ∩ `client.AllowedScopes` (default to `AllowedScopes` if `scope` omitted).
  4. Build a synthetic `UserContext{UserName: "client:"+clientID, Email: "oauth-client-"+clientID+"@service.internal", RemoteID: clientID, Roles: effectiveScopes}` and call the existing unexported `a.auth.oauth2GetOrCreateUser(ctx, userCtx, "oauth2_client")` to get-or-create the backing user row (deterministic synthetic email makes this idempotent per client).
  5. Generate `sessionToken := randomOAuthToken()`, call the existing unexported `a.auth.oauth2CreateSession(ctx, sessionToken, userID, &oauth2.Token{AccessToken: sessionToken, TokenType: "Bearer"}, time.Now().Add(cfg.AccessTokenTTL), "oauth2_client")` to persist it in `user_sessions` (same table/procedure everything else already reads via introspection).
  6. `s.writeOAuthToken(w, sessionToken, "" /* no refresh token per RFC 6749 §4.4.3 */, effectiveScopes)`.
  - Only wired for the `s.auth != nil` (DatabaseAuthenticator) case — external-provider-only servers return `unsupported_grant_type` (no local user store to back a service account).
- `metadataHandler`: add `"client_credentials"` to `grant_types_supported`.

## 3. RFC 9728 Protected Resource Metadata

- Add `cfg.ResourceIdentifier string` to `OAuthServerConfig` (`oauth_server.go:18`), defaulting to `cfg.Issuer` in `NewOAuthServer`.
- New handler `protectedResourceHandler` returning:
  ```json
  {"resource": "<ResourceIdentifier>", "authorization_servers": ["<Issuer>"], "scopes_supported": cfg.DefaultScopes, "bearer_methods_supported": ["header"]}
  ```
- Register in `HTTPHandler()`: `mux.HandleFunc("/.well-known/oauth-protected-resource", s.protectedResourceHandler)`.

## 4. OIDC discovery + `id_token`

- **Dependency**: add `github.com/golang-jwt/jwt/v5` as a direct dependency (`go get github.com/golang-jwt/jwt/v5@v5.3.1` — already resolved in `go.sum` transitively, so this should be a clean, network-light `go mod tidy`).
- **Signing key**: in `NewOAuthServer`, generate an RSA-2048 keypair in memory (`rsa.GenerateKey`) unless `cfg.SigningKey *rsa.PrivateKey` is supplied (new optional config field, documented for multi-instance deployments that need id_tokens verifiable consistently across restarts/instances). Compute a stable `kid` from a SHA-256 fingerprint of the public key modulus.
- **JWKS endpoint**: `GET /oauth/jwks.json` → standard JWKS JSON (`kty:"RSA"`, `n`, `e`, `kid`, `use:"sig"`, `alg:"RS256"`).
- **OIDC discovery**: `GET /.well-known/openid-configuration` → same fields as `/.well-known/oauth-authorization-server` plus `jwks_uri`, `userinfo_endpoint`, `subject_types_supported: ["public"]`, `id_token_signing_alg_values_supported: ["RS256"]`, `response_types_supported`, `grant_types_supported` (mirrors §1/§2 additions).
- **`id_token` issuance**: in `writeOAuthToken` (or a thin wrapper called only from `handleAuthCodeGrant`/`handleRefreshGrant`, not from client_credentials — OIDC has no id_token for that grant), if `scopes` contains `"openid"`: call the already-existing `s.auth.OAuthIntrospectToken(ctx, accessToken)` (or the matching provider's) to fetch `OAuthTokenInfo{Sub,Username,Email}` for the just-issued token, then build and RS256-sign a JWT with claims `iss`, `sub`, `aud` (client_id — needs to be threaded down from the token handler), `exp`, `iat`, and (if scope has `profile`/`email`) `preferred_username`/`email`. Attach as `id_token` in the JSON response.
- **Userinfo endpoint**: `GET/POST /oauth/userinfo` with `Authorization: Bearer <token>` → reuse `OAuthIntrospectToken`, return `{sub, preferred_username, email}` (401 `{"error":"invalid_token"}` if inactive).
- Register `/oauth/jwks.json`, `/.well-known/openid-configuration`, `/oauth/userinfo` in `HTTPHandler()`.

## Files touched

- `pkg/security/oauth_server.go` — bulk of the new logic (client auth, client_credentials grant, RFC 9728, OIDC discovery, JWKS, userinfo, id_token signing), updated doc comment listing endpoints.
- `pkg/security/oauth_server_db.go` — `OAuthServerClient` struct fields.
- `pkg/security/oauth_server_db_direct.go` — INSERT/SELECT column updates.
- `pkg/security/database_schema.sql`, `pkg/security/database_schema_sqlite.sql` — `oauth_clients` new columns; `resolvespec_oauth_register_client` INSERT list.
- `pkg/security/OAUTH2.md` — document new endpoints/grant/columns.
- `go.mod`/`go.sum` — new direct dependency.
- New `pkg/security/oauth_server_test.go` — HTTP-level tests (see below); extend `direct_mode_test.go`'s `TestDirectMode_OAuthGetOrCreateUserAndSession`-style coverage only if the column changes require it.

## Verification

- `go build ./...` and `go vet ./...` in the module root.
- `go test ./pkg/security/... -run OAuth -v` after adding tests covering:
  - Register a confidential client (`grant_types: ["client_credentials"]`) → assert `client_secret` present once, `client_secret_hash` never leaks via `GET`/introspection.
  - `client_credentials` grant: valid secret → 200 with `access_token`, no `refresh_token`; bad secret → `invalid_client`; public client (no secret) attempting `client_credentials` → `invalid_client`.
  - Authorization-code flow for a confidential client without credentials on `/oauth/token` → `invalid_client`; with credentials → succeeds (regression check that public-client flow with no secret is unaffected).
  - `/.well-known/oauth-protected-resource` returns expected JSON shape.
  - `/.well-known/openid-configuration` + `/oauth/jwks.json` shape; parse a returned `id_token` (scope `openid profile email`) with `golang-jwt` using the JWKS public key and assert `iss`/`sub`/`aud`/`exp` claims.
  - `/oauth/userinfo` with a valid vs. revoked token.
- Full existing suite: `go test ./pkg/security/...` to confirm no regression in direct-mode / procedure-mode OAuth tests already in `direct_mode_test.go`.
