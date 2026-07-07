package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Direct-mode implementations mirroring the OAuth2 server stored procedures
// (resolvespec_oauth_register_client, etc.) in database_schema.sql, using
// plain SQL against TableNames.OAuthClients / TableNames.OAuthCodes.
// Array columns (redirect_uris, grant_types, allowed_scopes, scopes) are
// JSON-encoded TEXT instead of native Postgres arrays.

func (a *DatabaseAuthenticator) oauthRegisterClientDirect(ctx context.Context, client *OAuthServerClient) (*OAuthServerClient, error) {
	grantTypes := client.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	allowedScopes := client.AllowedScopes
	if len(allowedScopes) == 0 {
		allowedScopes = []string{"openid", "profile", "email"}
	}

	redirectURIsJSON, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal redirect_uris: %w", err)
	}
	grantTypesJSON, err := json.Marshal(grantTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal grant_types: %w", err)
	}
	allowedScopesJSON, err := json.Marshal(allowedScopes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal allowed_scopes: %w", err)
	}

	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (client_id, redirect_uris, client_name, grant_types, allowed_scopes, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.OAuthClients))
		_, err := db.ExecContext(ctx, query, client.ClientID, string(redirectURIsJSON), client.ClientName, string(grantTypesJSON), string(allowedScopesJSON), true, time.Now())
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}

	return &OAuthServerClient{
		ClientID:      client.ClientID,
		RedirectURIs:  client.RedirectURIs,
		ClientName:    client.ClientName,
		GrantTypes:    grantTypes,
		AllowedScopes: allowedScopes,
	}, nil
}

func (a *DatabaseAuthenticator) oauthGetClientDirect(ctx context.Context, clientID string) (*OAuthServerClient, error) {
	var redirectURIsJSON, grantTypesJSON, allowedScopesJSON sql.NullString
	var clientName sql.NullString

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT redirect_uris, client_name, grant_types, allowed_scopes FROM %s WHERE client_id = ? AND is_active = ?`,
			a.tableNames.OAuthClients))
		return db.QueryRowContext(ctx, query, clientID, true).Scan(&redirectURIsJSON, &clientName, &grantTypesJSON, &allowedScopesJSON)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("client not found")
		}
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	result := &OAuthServerClient{ClientID: clientID, ClientName: clientName.String}
	if redirectURIsJSON.Valid {
		_ = json.Unmarshal([]byte(redirectURIsJSON.String), &result.RedirectURIs)
	}
	if grantTypesJSON.Valid {
		_ = json.Unmarshal([]byte(grantTypesJSON.String), &result.GrantTypes)
	}
	if allowedScopesJSON.Valid {
		_ = json.Unmarshal([]byte(allowedScopesJSON.String), &result.AllowedScopes)
	}
	return result, nil
}

func (a *DatabaseAuthenticator) oauthSaveCodeDirect(ctx context.Context, code *OAuthCode) error {
	scopesJSON, err := json.Marshal(code.Scopes)
	if err != nil {
		return fmt.Errorf("failed to marshal scopes: %w", err)
	}
	method := code.CodeChallengeMethod
	if method == "" {
		method = "S256"
	}

	return a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (code, client_id, redirect_uri, client_state, code_challenge, code_challenge_method, session_token, refresh_token, scopes, expires_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.tableNames.OAuthCodes))
		_, err := db.ExecContext(ctx, query, code.Code, code.ClientID, code.RedirectURI, code.ClientState, code.CodeChallenge,
			method, code.SessionToken, code.RefreshToken, string(scopesJSON), code.ExpiresAt, time.Now())
		return err
	})
}

func (a *DatabaseAuthenticator) oauthExchangeCodeDirect(ctx context.Context, code string) (*OAuthCode, error) {
	var result OAuthCode
	var clientState, refreshToken sql.NullString
	var scopesJSON sql.NullString

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT client_id, redirect_uri, client_state, code_challenge, code_challenge_method, session_token, refresh_token, scopes
			 FROM %s WHERE code = ? AND expires_at > ?`, a.tableNames.OAuthCodes))
		err := db.QueryRowContext(ctx, query, code, time.Now()).Scan(
			&result.ClientID, &result.RedirectURI, &clientState, &result.CodeChallenge, &result.CodeChallengeMethod, &result.SessionToken, &refreshToken, &scopesJSON)
		if err != nil {
			return err
		}
		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE code = ?`, a.tableNames.OAuthCodes))
		_, err = db.ExecContext(ctx, delQuery, code)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired code")
		}
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	result.Code = code
	result.ClientState = clientState.String
	result.RefreshToken = refreshToken.String
	if scopesJSON.Valid {
		_ = json.Unmarshal([]byte(scopesJSON.String), &result.Scopes)
	}
	return &result, nil
}

func (a *DatabaseAuthenticator) oauthIntrospectTokenDirect(ctx context.Context, token string) (*OAuthTokenInfo, error) {
	var info OAuthTokenInfo
	var roles sql.NullString
	var exp, iat sql.NullTime

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT u.id, u.username, u.email, u.user_level, u.roles, s.expires_at, s.created_at
			 FROM %s s JOIN %s u ON u.id = s.user_id
			 WHERE s.session_token = ? AND s.expires_at > ? AND u.is_active = ?`,
			a.tableNames.UserSessions, a.tableNames.Users))
		var userID int
		err := db.QueryRowContext(ctx, query, token, time.Now(), true).Scan(&userID, &info.Username, &info.Email, &info.UserLevel, &roles, &exp, &iat)
		if err != nil {
			return err
		}
		info.Sub = fmt.Sprintf("%d", userID)
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &OAuthTokenInfo{Active: false}, nil
		}
		return nil, fmt.Errorf("failed to introspect token: %w", err)
	}

	info.Active = true
	info.Roles = parseRoles(roles.String)
	if exp.Valid {
		info.Exp = exp.Time.Unix()
	}
	if iat.Valid {
		info.Iat = iat.Time.Unix()
	}
	return &info, nil
}

func (a *DatabaseAuthenticator) oauthRevokeTokenDirect(ctx context.Context, token string) error {
	return a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE session_token = ?`, a.tableNames.UserSessions))
		_, err := db.ExecContext(ctx, query, token)
		return err
	})
}
