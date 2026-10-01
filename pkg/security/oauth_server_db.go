package security

import (
	"context"
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
