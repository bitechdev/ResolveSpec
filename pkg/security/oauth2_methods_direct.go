package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// oauthRefreshSession is the session data needed to refresh an OAuth2 token,
// shared by both the stored-procedure and Direct-mode code paths.
type oauthRefreshSession struct {
	UserID      int       `json:"user_id"`
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Expiry      time.Time `json:"expiry"`
}

// oauth2GetOrCreateUserDirect mirrors resolvespec_oauth_getorcreateuser.
func (a *DatabaseAuthenticator) oauth2GetOrCreateUserDirect(ctx context.Context, userCtx *UserContext, providerName string) (int, error) {
	rolesStr := strings.Join(userCtx.Roles, ",")
	var userID int

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT id FROM %s WHERE email = ?`, a.tableNames.Users))
		err := db.QueryRowContext(ctx, query, userCtx.Email).Scan(&userID)
		if err == nil {
			now := time.Now()
			updQuery := rewritePlaceholders(db, fmt.Sprintf(
				`UPDATE %s SET last_login_at = ?, updated_at = ?, remote_id = COALESCE(remote_id, ?), auth_provider = COALESCE(auth_provider, ?) WHERE id = ?`,
				a.tableNames.Users))
			_, err := db.ExecContext(ctx, updQuery, now, now, userCtx.RemoteID, providerName, userID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		now := time.Now()
		insQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (username, email, password, user_level, roles, is_active, created_at, updated_at, last_login_at, remote_id, auth_provider) VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.Users))
		res, err := db.ExecContext(ctx, insQuery, userCtx.UserName, userCtx.Email, userCtx.UserLevel, rolesStr, true, now, now, now, userCtx.RemoteID, providerName)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		userID = int(id)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get or create user: %w", err)
	}
	return userID, nil
}

// oauth2CreateSessionDirect mirrors resolvespec_oauth_createsession (insert-or-update by session_token).
func (a *DatabaseAuthenticator) oauth2CreateSessionDirect(ctx context.Context, sessionToken string, userID int, token *oauth2.Token, expiresAt time.Time, providerName string) error {
	return a.runDBOpWithReconnect(func(db *sql.DB) error {
		var exists int
		checkQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT 1 FROM %s WHERE session_token = ?`, a.tableNames.UserSessions))
		err := db.QueryRowContext(ctx, checkQuery, sessionToken).Scan(&exists)
		now := time.Now()
		if err == nil {
			updQuery := rewritePlaceholders(db, fmt.Sprintf(
				`UPDATE %s SET access_token = ?, refresh_token = ?, token_type = ?, expires_at = ?, last_activity_at = ? WHERE session_token = ?`,
				a.tableNames.UserSessions))
			_, err := db.ExecContext(ctx, updQuery, token.AccessToken, token.RefreshToken, token.TokenType, expiresAt, now, sessionToken)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		insQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (session_token, user_id, expires_at, created_at, last_activity_at, access_token, refresh_token, token_type, auth_provider) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.UserSessions))
		_, err = db.ExecContext(ctx, insQuery, sessionToken, userID, expiresAt, now, now, token.AccessToken, token.RefreshToken, token.TokenType, providerName)
		return err
	})
}

// oauthGetByRefreshToken retrieves the session for a refresh token, dispatching between
// the resolvespec_oauth_getrefreshtoken stored procedure and Direct-mode SQL.
func (a *DatabaseAuthenticator) oauthGetByRefreshToken(ctx context.Context, refreshToken string) (*oauthRefreshSession, error) {
	if !a.capability.ShouldUseProcedure(ctx, a.queryMode, a.getDB(), a.sqlNames.OAuthGetRefreshToken) {
		var session oauthRefreshSession
		err := a.runDBOpWithReconnect(func(db *sql.DB) error {
			query := rewritePlaceholders(db, fmt.Sprintf(
				`SELECT user_id, access_token, token_type, expires_at FROM %s WHERE refresh_token = ? AND expires_at > ?`,
				a.tableNames.UserSessions))
			return db.QueryRowContext(ctx, query, refreshToken, time.Now()).Scan(&session.UserID, &session.AccessToken, &session.TokenType, &session.Expiry)
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("refresh token not found or expired")
			}
			return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
		}
		return &session, nil
	}

	var success bool
	var errMsg *string
	var sessionData []byte

	err := a.getDB().QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, a.sqlNames.OAuthGetRefreshToken), refreshToken).Scan(&success, &errMsg, &sessionData)
	if err != nil {
		return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
	}
	if !success {
		if errMsg != nil {
			return nil, fmt.Errorf("%s", *errMsg)
		}
		return nil, fmt.Errorf("invalid or expired refresh token")
	}

	var session oauthRefreshSession
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session data: %w", err)
	}
	return &session, nil
}

// oauthUpdateRefreshTokenRecord updates a session with new tokens, dispatching between
// the resolvespec_oauth_updaterefreshtoken stored procedure and Direct-mode SQL.
func (a *DatabaseAuthenticator) oauthUpdateRefreshTokenRecord(ctx context.Context, userID int, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken string, expiresAt time.Time) error {
	if !a.capability.ShouldUseProcedure(ctx, a.queryMode, a.getDB(), a.sqlNames.OAuthUpdateRefreshToken) {
		var rows int64
		err := a.runDBOpWithReconnect(func(db *sql.DB) error {
			query := rewritePlaceholders(db, fmt.Sprintf(
				`UPDATE %s SET session_token = ?, access_token = ?, refresh_token = ?, expires_at = ?, last_activity_at = ? WHERE user_id = ? AND refresh_token = ?`,
				a.tableNames.UserSessions))
			res, err := db.ExecContext(ctx, query, newSessionToken, newAccessToken, newRefreshToken, expiresAt, time.Now(), userID, oldRefreshToken)
			if err != nil {
				return err
			}
			rows, err = res.RowsAffected()
			return err
		})
		if err != nil {
			return fmt.Errorf("failed to update session: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("session not found")
		}
		return nil
	}

	updateData := map[string]interface{}{
		"user_id":           userID,
		"old_refresh_token": oldRefreshToken,
		"new_session_token": newSessionToken,
		"new_access_token":  newAccessToken,
		"new_refresh_token": newRefreshToken,
		"expires_at":        expiresAt,
	}
	updateJSON, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("failed to marshal update data: %w", err)
	}

	var updateSuccess bool
	var updateErrMsg *string
	err = a.getDB().QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error
		FROM %s($1::jsonb)
	`, a.sqlNames.OAuthUpdateRefreshToken), updateJSON).Scan(&updateSuccess, &updateErrMsg)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}
	if !updateSuccess {
		if updateErrMsg != nil {
			return fmt.Errorf("%s", *updateErrMsg)
		}
		return fmt.Errorf("failed to update session")
	}
	return nil
}

// oauthGetUserByID retrieves user data by ID, dispatching between the
// resolvespec_oauth_getuser stored procedure and Direct-mode SQL.
func (a *DatabaseAuthenticator) oauthGetUserByID(ctx context.Context, userID int) (*UserContext, error) {
	if !a.capability.ShouldUseProcedure(ctx, a.queryMode, a.getDB(), a.sqlNames.OAuthGetUser) {
		var username, email, roles, programUserTable sql.NullString
		var userLevel, programUserID sql.NullInt64
		err := a.runDBOpWithReconnect(func(db *sql.DB) error {
			query := rewritePlaceholders(db, fmt.Sprintf(
				`SELECT username, email, user_level, roles, program_user_id, program_user_table FROM %s WHERE id = ? AND is_active = ?`,
				a.tableNames.Users))
			return db.QueryRowContext(ctx, query, userID, true).Scan(&username, &email, &userLevel, &roles, &programUserID, &programUserTable)
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("user not found")
			}
			return nil, fmt.Errorf("failed to get user data: %w", err)
		}
		return &UserContext{
			UserID:           userID,
			UserName:         username.String,
			Email:            email.String,
			UserLevel:        int(userLevel.Int64),
			Roles:            parseRoles(roles.String),
			ProgramUserID:    int(programUserID.Int64),
			ProgramUserTable: programUserTable.String,
		}, nil
	}

	var userSuccess bool
	var userErrMsg *string
	var userData []byte
	err := a.getDB().QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1)
	`, a.sqlNames.OAuthGetUser), userID).Scan(&userSuccess, &userErrMsg, &userData)
	if err != nil {
		return nil, fmt.Errorf("failed to get user data: %w", err)
	}
	if !userSuccess {
		if userErrMsg != nil {
			return nil, fmt.Errorf("%s", *userErrMsg)
		}
		return nil, fmt.Errorf("failed to get user data")
	}
	var userCtx UserContext
	if err := json.Unmarshal(userData, &userCtx); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}
	return &userCtx, nil
}
