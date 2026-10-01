package security

import (
	"context"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// OAuthRegisterClient persists an OAuth2 client registration.
func (a *DatabaseAuthenticator) OAuthRegisterClient(ctx context.Context, client *OAuthServerClient) (*OAuthServerClient, error) {
	return a.src.get().OAuthClient.RegisterClient(ctx, client)
}

// OAuthGetClient retrieves a registered client by ID.
func (a *DatabaseAuthenticator) OAuthGetClient(ctx context.Context, clientID string) (*OAuthServerClient, error) {
	return a.src.get().OAuthClient.GetClient(ctx, clientID)
}

// OAuthSaveCode persists an authorization code.
func (a *DatabaseAuthenticator) OAuthSaveCode(ctx context.Context, code *OAuthCode) error {
	return a.src.get().OAuthClient.SaveCode(ctx, code)
}

// OAuthExchangeCode retrieves and deletes an authorization code (single use).
func (a *DatabaseAuthenticator) OAuthExchangeCode(ctx context.Context, code string) (*OAuthCode, error) {
	return a.src.get().OAuthClient.ExchangeCode(ctx, code)
}

// OAuthIntrospectToken validates a token and returns its metadata (RFC 7662).
func (a *DatabaseAuthenticator) OAuthIntrospectToken(ctx context.Context, token string) (*OAuthTokenInfo, error) {
	return a.src.get().OAuthClient.Introspect(ctx, token)
}

// OAuthRevokeToken revokes a token by deleting the session (RFC 7009).
func (a *DatabaseAuthenticator) OAuthRevokeToken(ctx context.Context, token string) error {
	return a.src.get().OAuthClient.Revoke(ctx, token)
}

// OAuthGrants returns the store holding consents, managed refresh tokens, device codes,
// pushed authorization requests and the replay cache.
func (a *DatabaseAuthenticator) OAuthGrants() lookup.OAuthGrantStore {
	return a.src.get().OAuthGrant
}

// OAuthUpdateClient replaces the registered metadata of a client (RFC 7592).
func (a *DatabaseAuthenticator) OAuthUpdateClient(ctx context.Context, client *OAuthServerClient) error {
	return a.src.get().OAuthClient.UpdateClient(ctx, client)
}

// OAuthDeleteClient deactivates a registered client (RFC 7592).
func (a *DatabaseAuthenticator) OAuthDeleteClient(ctx context.Context, clientID string) error {
	return a.src.get().OAuthClient.DeleteClient(ctx, clientID)
}

// OAuthGetUser returns the active user with the given id.
func (a *DatabaseAuthenticator) OAuthGetUser(ctx context.Context, userID int) (*UserContext, error) {
	return a.src.get().OAuthUser.GetUser(ctx, userID)
}
