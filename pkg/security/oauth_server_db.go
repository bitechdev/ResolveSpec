package security

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// OAuthServerClient is a persisted RFC 7591 registered OAuth2 client.
type OAuthServerClient struct {
	ClientID      string   `json:"client_id"`
	RedirectURIs  []string `json:"redirect_uris"`
	ClientName    string   `json:"client_name,omitempty"`
	GrantTypes    []string `json:"grant_types"`
	AllowedScopes []string `json:"allowed_scopes,omitempty"`
}

// OAuthCode is a short-lived authorization code.
type OAuthCode struct {
	Code                string    `json:"code"`
	ClientID            string    `json:"client_id"`
	RedirectURI         string    `json:"redirect_uri"`
	ClientState         string    `json:"client_state,omitempty"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	SessionToken        string    `json:"session_token"`
	RefreshToken        string    `json:"refresh_token,omitempty"`
	Scopes              []string  `json:"scopes,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`
}

// OAuthTokenInfo is the RFC 7662 token introspection response.
type OAuthTokenInfo struct {
	Active    bool     `json:"active"`
	Sub       string   `json:"sub,omitempty"`
	Username  string   `json:"username,omitempty"`
	Email     string   `json:"email,omitempty"`
	UserLevel int      `json:"user_level,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Exp       int64    `json:"exp,omitempty"`
	Iat       int64    `json:"iat,omitempty"`
}

// OAuthRegisterClient persists an OAuth2 client registration.
func (a *DatabaseAuthenticator) OAuthRegisterClient(ctx context.Context, client *OAuthServerClient) (*OAuthServerClient, error) {
	input, err := json.Marshal(client)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client: %w", err)
	}

	var success bool
	var errMsg *string
	var data []byte

	err = a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1::jsonb)
	`, a.sqlNames.OAuthRegisterClient), input).Scan(&success, &errMsg, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("failed to register client")
	}

	var result OAuthServerClient
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse registered client: %w", err)
	}
	return &result, nil
}

// OAuthGetClient retrieves a registered client by ID.
func (a *DatabaseAuthenticator) OAuthGetClient(ctx context.Context, clientID string) (*OAuthServerClient, error) {
	var success bool
	var errMsg *string
	var data []byte

	err := a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, a.sqlNames.OAuthGetClient), clientID).Scan(&success, &errMsg, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("client not found")
	}

	var result OAuthServerClient
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse client: %w", err)
	}
	return &result, nil
}

// OAuthSaveCode persists an authorization code.
func (a *DatabaseAuthenticator) OAuthSaveCode(ctx context.Context, code *OAuthCode) error {
	input, err := json.Marshal(code)
	if err != nil {
		return fmt.Errorf("failed to marshal code: %w", err)
	}

	var success bool
	var errMsg *string

	err = a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, a.sqlNames.OAuthSaveCode), input).Scan(&success, &errMsg)
	if err != nil {
		return fmt.Errorf("failed to save code: %w", err)
	}
	if !success {
		if errMsg != nil {
			return fmt.Errorf("%s", *errMsg)
		}
		return fmt.Errorf("failed to save code")
	}
	return nil
}

// OAuthExchangeCode retrieves and deletes an authorization code (single use).
func (a *DatabaseAuthenticator) OAuthExchangeCode(ctx context.Context, code string) (*OAuthCode, error) {
	var success bool
	var errMsg *string
	var data []byte

	err := a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, a.sqlNames.OAuthExchangeCode), code).Scan(&success, &errMsg, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("invalid or expired code")
	}

	var result OAuthCode
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse code data: %w", err)
	}
	result.Code = code
	return &result, nil
}

// OAuthIntrospectToken validates a token and returns its metadata (RFC 7662).
func (a *DatabaseAuthenticator) OAuthIntrospectToken(ctx context.Context, token string) (*OAuthTokenInfo, error) {
	var success bool
	var errMsg *string
	var data []byte

	err := a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, a.sqlNames.OAuthIntrospect), token).Scan(&success, &errMsg, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect token: %w", err)
	}
	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("introspection failed")
	}

	var result OAuthTokenInfo
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse token info: %w", err)
	}
	return &result, nil
}

// OAuthRevokeToken revokes a token by deleting the session (RFC 7009).
func (a *DatabaseAuthenticator) OAuthRevokeToken(ctx context.Context, token string) error {
	var success bool
	var errMsg *string

	err := a.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1)
	`, a.sqlNames.OAuthRevoke), token).Scan(&success, &errMsg)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	if !success {
		if errMsg != nil {
			return fmt.Errorf("%s", *errMsg)
		}
		return fmt.Errorf("failed to revoke token")
	}
	return nil
}
