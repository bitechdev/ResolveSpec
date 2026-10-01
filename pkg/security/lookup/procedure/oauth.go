package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// OAuthUsers implements lookup.OAuthUserStore with the resolvespec_oauth_* procedures.
type OAuthUsers struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.OAuthUserStore = (*OAuthUsers)(nil)

// NewOAuthUsers creates the procedure-backed OAuthUserStore.
func NewOAuthUsers(run Runner, procs lookup.ProcNames) *OAuthUsers {
	return &OAuthUsers{run: run, procs: procs}
}

// GetOrCreateUser implements lookup.OAuthUserStore.
func (o *OAuthUsers) GetOrCreateUser(ctx context.Context, user *sectypes.UserContext, provider string) (int, error) {
	userJSON, err := json.Marshal(map[string]any{
		"username":      user.UserName,
		"email":         user.Email,
		"remote_id":     user.RemoteID,
		"user_level":    user.UserLevel,
		"roles":         user.Roles,
		"auth_provider": provider,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal user data: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	var userID sql.NullInt64
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_user_id
		FROM %s($1::jsonb)
	`, o.procs.OAuthGetOrCreateUser), userJSON).Scan(&success, &errMsg, &userID)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get or create user: %w", err)
	}
	if !success {
		return 0, failure(errMsg, "failed to get or create user")
	}
	if !userID.Valid {
		return 0, fmt.Errorf("user ID not returned")
	}
	return int(userID.Int64), nil
}

// CreateSession implements lookup.OAuthUserStore.
func (o *OAuthUsers) CreateSession(ctx context.Context, s lookup.OAuthSession) error {
	sessionJSON, err := json.Marshal(map[string]any{
		"session_token": s.SessionToken,
		"user_id":       s.UserID,
		"access_token":  s.AccessToken,
		"refresh_token": s.RefreshToken,
		"token_type":    s.TokenType,
		"expires_at":    s.ExpiresAt,
		"auth_provider": s.Provider,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, o.procs.OAuthCreateSession), sessionJSON).Scan(&success, &errMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	if !success {
		return failure(errMsg, "failed to create session")
	}
	return nil
}

// GetByRefreshToken implements lookup.OAuthUserStore.
func (o *OAuthUsers) GetByRefreshToken(ctx context.Context, refreshToken string) (*lookup.OAuthRefreshSession, error) {
	var success bool
	var errMsg sql.NullString
	var data []byte
	err := o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, o.procs.OAuthGetRefreshToken), refreshToken).Scan(&success, &errMsg, &data)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
	}
	if !success {
		return nil, failure(errMsg, "invalid or expired refresh token")
	}
	var session lookup.OAuthRefreshSession
	if err := json.Unmarshal(normalizeTimes(data), &session); err != nil {
		return nil, fmt.Errorf("failed to parse session data: %w", err)
	}
	return &session, nil
}

// UpdateRefreshToken implements lookup.OAuthUserStore.
func (o *OAuthUsers) UpdateRefreshToken(ctx context.Context, userID int, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken string, expiresAt time.Time) error {
	updateJSON, err := json.Marshal(map[string]any{
		"user_id":           userID,
		"old_refresh_token": oldRefreshToken,
		"new_session_token": newSessionToken,
		"new_access_token":  newAccessToken,
		"new_refresh_token": newRefreshToken,
		"expires_at":        expiresAt,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, o.procs.OAuthUpdateRefreshToken), updateJSON).Scan(&success, &errMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}
	if !success {
		return failure(errMsg, "failed to update session")
	}
	return nil
}

// GetUser implements lookup.OAuthUserStore.
func (o *OAuthUsers) GetUser(ctx context.Context, userID int) (*sectypes.UserContext, error) {
	var success bool
	var errMsg sql.NullString
	var data []byte
	err := o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, o.procs.OAuthGetUser), userID).Scan(&success, &errMsg, &data)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user data: %w", err)
	}
	if !success {
		return nil, failure(errMsg, "failed to get user data")
	}
	var userCtx sectypes.UserContext
	if err := json.Unmarshal(data, &userCtx); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}
	return &userCtx, nil
}

// OAuthClients implements lookup.OAuthClientStore with the resolvespec_oauth_* server procedures.
type OAuthClients struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.OAuthClientStore = (*OAuthClients)(nil)

// NewOAuthClients creates the procedure-backed OAuthClientStore.
func NewOAuthClients(run Runner, procs lookup.ProcNames) *OAuthClients {
	return &OAuthClients{run: run, procs: procs}
}

// callData runs a `(p_success, p_error, p_data)` procedure with one argument.
func (o *OAuthClients) callData(ctx context.Context, proc string, arg any) (data []byte, ok bool, errMsg sql.NullString, err error) {
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, proc), arg).Scan(&ok, &errMsg, &data)
	})
	return
}

// callNoData runs a `(p_success, p_error)` procedure with one argument.
func (o *OAuthClients) callNoData(ctx context.Context, proc string, arg any) (ok bool, errMsg sql.NullString, err error) {
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1)
	`, proc), arg).Scan(&ok, &errMsg)
	})
	return
}

// RegisterClient implements lookup.OAuthClientStore.
func (o *OAuthClients) RegisterClient(ctx context.Context, client *sectypes.OAuthServerClient) (*sectypes.OAuthServerClient, error) {
	input, err := json.Marshal(client)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal client: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	var data []byte
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1::jsonb)
	`, o.procs.OAuthRegisterClient), input).Scan(&success, &errMsg, &data)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	if !success {
		return nil, failure(errMsg, "failed to register client")
	}
	var result sectypes.OAuthServerClient
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse registered client: %w", err)
	}
	return &result, nil
}

// GetClient implements lookup.OAuthClientStore.
func (o *OAuthClients) GetClient(ctx context.Context, clientID string) (*sectypes.OAuthServerClient, error) {
	data, ok, errMsg, err := o.callData(ctx, o.procs.OAuthGetClient, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	if !ok {
		return nil, failure(errMsg, "client not found")
	}
	var result sectypes.OAuthServerClient
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse client: %w", err)
	}
	return &result, nil
}

// SaveCode implements lookup.OAuthClientStore.
func (o *OAuthClients) SaveCode(ctx context.Context, code *sectypes.OAuthCode) error {
	input, err := json.Marshal(code) //nolint:gosec // G117: intentional: field must be serialized
	if err != nil {
		return fmt.Errorf("failed to marshal code: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, o.procs.OAuthSaveCode), input).Scan(&success, &errMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to save code: %w", err)
	}
	if !success {
		return failure(errMsg, "failed to save code")
	}
	return nil
}

// ExchangeCode implements lookup.OAuthClientStore.
func (o *OAuthClients) ExchangeCode(ctx context.Context, code string) (*sectypes.OAuthCode, error) {
	data, ok, errMsg, err := o.callData(ctx, o.procs.OAuthExchangeCode, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	if !ok {
		return nil, failure(errMsg, "invalid or expired code")
	}
	var result sectypes.OAuthCode
	if err := json.Unmarshal(normalizeTimes(data), &result); err != nil {
		return nil, fmt.Errorf("failed to parse code data: %w", err)
	}
	result.Code = code
	return &result, nil
}

// Introspect implements lookup.OAuthClientStore.
func (o *OAuthClients) Introspect(ctx context.Context, token string) (*sectypes.OAuthTokenInfo, error) {
	data, ok, errMsg, err := o.callData(ctx, o.procs.OAuthIntrospect, token)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect token: %w", err)
	}
	if !ok {
		return nil, failure(errMsg, "introspection failed")
	}
	var result sectypes.OAuthTokenInfo
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse token info: %w", err)
	}
	return &result, nil
}

// Revoke implements lookup.OAuthClientStore.
func (o *OAuthClients) Revoke(ctx context.Context, token string) error {
	ok, errMsg, err := o.callNoData(ctx, o.procs.OAuthRevoke, token)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}
	if !ok {
		return failure(errMsg, "failed to revoke token")
	}
	return nil
}

// UpdateClient implements lookup.OAuthClientStore.
func (o *OAuthClients) UpdateClient(ctx context.Context, client *sectypes.OAuthServerClient) error {
	input, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("failed to marshal client: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, o.procs.OAuthUpdateClient), input).Scan(&success, &errMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to update client: %w", err)
	}
	if !success {
		return failure(errMsg, "failed to update client")
	}
	return nil
}

// DeleteClient implements lookup.OAuthClientStore.
func (o *OAuthClients) DeleteClient(ctx context.Context, clientID string) error {
	ok, errMsg, err := o.callNoData(ctx, o.procs.OAuthDeleteClient, clientID)
	if err != nil {
		return fmt.Errorf("failed to delete client: %w", err)
	}
	if !ok {
		return failure(errMsg, "failed to delete client")
	}
	return nil
}
