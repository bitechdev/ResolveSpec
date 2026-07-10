package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Direct-mode implementations for DatabaseAuthenticator and JWTAuthenticator.
// These mirror the plpgsql bodies in database_schema.sql using plain
// parameterized SQL against the configured TableNames, so they work on
// SQLite, MySQL, or Postgres without the resolvespec_* functions installed.
//
// Password verification is intentionally not implemented here: the stored
// procedures never verify the password hash either (see the TODOs in
// database_schema.sql), so Direct mode matches that behavior exactly rather
// than introducing a mismatch between modes.

var (
	errUsernameExists = errors.New("username already exists")
	errEmailExists    = errors.New("email already exists")
)

func (a *DatabaseAuthenticator) loginDirect(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var userID int
	var email, roles, programUserTable sql.NullString
	var userLevel, programUserID sql.NullInt64

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT id, email, user_level, roles, program_user_id, program_user_table FROM %s WHERE username = ? AND is_active = ?`,
			a.tableNames.Users))
		return db.QueryRowContext(ctx, query, req.Username, true).Scan(&userID, &email, &userLevel, &roles, &programUserID, &programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	sessionToken, err := generateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)
	ipAddress, userAgent := claimStrings(req.Claims)

	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		insertQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.UserSessions))
		if _, err := db.ExecContext(ctx, insertQuery, sessionToken, userID, expiresAt, ipAddress, userAgent, now, now); err != nil {
			return err
		}
		updateQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET last_login_at = ? WHERE id = ?`, a.tableNames.Users))
		_, err := db.ExecContext(ctx, updateQuery, now, userID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	userCtx := &UserContext{
		UserID:           userID,
		UserName:         req.Username,
		Email:            email.String,
		UserLevel:        int(userLevel.Int64),
		Roles:            parseRoles(roles.String),
		SessionID:        sessionToken,
		ProgramUserID:    int(programUserID.Int64),
		ProgramUserTable: programUserTable.String,
	}

	return &LoginResponse{
		Token:     sessionToken,
		User:      userCtx,
		ExpiresIn: 86400,
	}, nil
}

func (a *DatabaseAuthenticator) registerDirect(ctx context.Context, req RegisterRequest) (*LoginResponse, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if req.Password == "" {
		return nil, fmt.Errorf("password is required")
	}

	rolesStr := strings.Join(req.Roles, ",")
	now := time.Now()
	ipAddress, userAgent := claimStrings(req.Claims)

	var userID int64
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		var count int
		checkQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE username = ?`, a.tableNames.Users))
		if err := db.QueryRowContext(ctx, checkQuery, req.Username).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errUsernameExists
		}
		checkQuery2 := rewritePlaceholders(db, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE email = ?`, a.tableNames.Users))
		if err := db.QueryRowContext(ctx, checkQuery2, req.Email).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return errEmailExists
		}

		insertQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (username, email, password, user_level, roles, is_active, created_at, updated_at, program_user_id, program_user_table) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.Users))
		res, err := db.ExecContext(ctx, insertQuery, req.Username, req.Email, req.Password, req.UserLevel, rolesStr, true, now, now, 0, "")
		if err != nil {
			return err
		}
		userID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		if errors.Is(err, errUsernameExists) {
			return nil, errUsernameExists
		}
		if errors.Is(err, errEmailExists) {
			return nil, errEmailExists
		}
		return nil, fmt.Errorf("register query failed: %w", err)
	}

	sessionToken, err := generateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	expiresAt := now.Add(24 * time.Hour)

	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		insertSession := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.UserSessions))
		if _, err := db.ExecContext(ctx, insertSession, sessionToken, userID, expiresAt, ipAddress, userAgent, now, now); err != nil {
			return err
		}
		updUser := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET last_login_at = ? WHERE id = ?`, a.tableNames.Users))
		_, err := db.ExecContext(ctx, updUser, now, userID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("register query failed: %w", err)
	}

	userCtx := &UserContext{
		UserID:           int(userID),
		UserName:         req.Username,
		Email:            req.Email,
		UserLevel:        req.UserLevel,
		Roles:            parseRoles(rolesStr),
		SessionID:        sessionToken,
		ProgramUserID:    0,
		ProgramUserTable: "",
	}

	return &LoginResponse{
		Token:     sessionToken,
		User:      userCtx,
		ExpiresIn: 86400,
	}, nil
}

func (a *DatabaseAuthenticator) logoutDirect(ctx context.Context, req LogoutRequest) error {
	token := req.Token
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	var rows int64
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE session_token = ? AND user_id = ?`, a.tableNames.UserSessions))
		res, err := db.ExecContext(ctx, query, token, req.UserID)
		if err != nil {
			return err
		}
		rows, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found")
	}

	if req.Token != "" {
		cacheKey := fmt.Sprintf("auth:session:%s", req.Token)
		_ = a.cache.Delete(ctx, cacheKey)
	}
	return nil
}

func (a *DatabaseAuthenticator) sessionDirect(ctx context.Context, token string) (*UserContext, error) {
	var userID int
	var username, email, roles, programUserTable sql.NullString
	var userLevel, programUserID sql.NullInt64

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT s.user_id, u.username, u.email, u.user_level, u.roles, u.program_user_id, u.program_user_table
			 FROM %s s JOIN %s u ON s.user_id = u.id
			 WHERE s.session_token = ? AND s.expires_at > ? AND u.is_active = ?`,
			a.tableNames.UserSessions, a.tableNames.Users))
		return db.QueryRowContext(ctx, query, token, time.Now(), true).Scan(&userID, &username, &email, &userLevel, &roles, &programUserID, &programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired session")
		}
		return nil, fmt.Errorf("session query failed: %w", err)
	}

	return &UserContext{
		UserID:           userID,
		UserName:         username.String,
		Email:            email.String,
		UserLevel:        int(userLevel.Int64),
		SessionID:        token,
		Roles:            parseRoles(roles.String),
		ProgramUserID:    int(programUserID.Int64),
		ProgramUserTable: programUserTable.String,
	}, nil
}

func (a *DatabaseAuthenticator) updateSessionActivityDirect(ctx context.Context, sessionToken string) error {
	return a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET last_activity_at = ? WHERE session_token = ? AND expires_at > ?`, a.tableNames.UserSessions))
		_, err := db.ExecContext(ctx, query, time.Now(), sessionToken, time.Now())
		return err
	})
}

func (a *DatabaseAuthenticator) refreshTokenDirect(ctx context.Context, oldToken string) (*LoginResponse, error) {
	var userID int
	var username, email, roles, ipAddress, userAgent, programUserTable sql.NullString
	var userLevel, programUserID sql.NullInt64

	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT s.user_id, u.username, u.email, u.user_level, u.roles, s.ip_address, s.user_agent, u.program_user_id, u.program_user_table
			 FROM %s s JOIN %s u ON s.user_id = u.id
			 WHERE s.session_token = ? AND s.expires_at > ? AND u.is_active = ?`,
			a.tableNames.UserSessions, a.tableNames.Users))
		return db.QueryRowContext(ctx, query, oldToken, time.Now(), true).Scan(&userID, &username, &email, &userLevel, &roles, &ipAddress, &userAgent, &programUserID, &programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired refresh token")
		}
		return nil, fmt.Errorf("refresh token query failed: %w", err)
	}

	newToken, err := generateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		insertQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.tableNames.UserSessions))
		if _, err := db.ExecContext(ctx, insertQuery, newToken, userID, expiresAt, ipAddress.String, userAgent.String, now, now); err != nil {
			return err
		}
		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE session_token = ?`, a.tableNames.UserSessions))
		_, err := db.ExecContext(ctx, delQuery, oldToken)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token generation failed: %w", err)
	}

	userCtx := &UserContext{
		UserID:           userID,
		UserName:         username.String,
		Email:            email.String,
		UserLevel:        int(userLevel.Int64),
		SessionID:        newToken,
		Roles:            parseRoles(roles.String),
		ProgramUserID:    int(programUserID.Int64),
		ProgramUserTable: programUserTable.String,
	}

	return &LoginResponse{
		Token:     newToken,
		User:      userCtx,
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

func (a *DatabaseAuthenticator) requestPasswordResetDirect(ctx context.Context, req PasswordResetRequest) (*PasswordResetResponse, error) {
	if req.Email == "" && req.Username == "" {
		return nil, fmt.Errorf("email or username is required")
	}

	var userID int
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		var query string
		var arg string
		if req.Email != "" {
			query = rewritePlaceholders(db, fmt.Sprintf(`SELECT id FROM %s WHERE email = ? AND is_active = ?`, a.tableNames.Users))
			arg = req.Email
		} else {
			query = rewritePlaceholders(db, fmt.Sprintf(`SELECT id FROM %s WHERE username = ? AND is_active = ?`, a.tableNames.Users))
			arg = req.Username
		}
		return db.QueryRowContext(ctx, query, arg, true).Scan(&userID)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return generic success even when user not found to avoid user enumeration.
			return &PasswordResetResponse{Token: "", ExpiresIn: 0}, nil
		}
		return nil, fmt.Errorf("password reset request query failed: %w", err)
	}

	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to generate reset token: %w", err)
	}
	rawToken := hex.EncodeToString(rawBytes)
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])
	now := time.Now()
	expiresAt := now.Add(1 * time.Hour)

	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ? AND used = ?`, a.tableNames.UserPasswordResets))
		if _, err := db.ExecContext(ctx, delQuery, userID, false); err != nil {
			return err
		}
		insQuery := rewritePlaceholders(db, fmt.Sprintf(`INSERT INTO %s (user_id, token_hash, expires_at, created_at, used) VALUES (?, ?, ?, ?, ?)`, a.tableNames.UserPasswordResets))
		_, err := db.ExecContext(ctx, insQuery, userID, tokenHash, expiresAt, now, false)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("password reset request query failed: %w", err)
	}

	return &PasswordResetResponse{Token: rawToken, ExpiresIn: 3600}, nil
}

func (a *DatabaseAuthenticator) completePasswordResetDirect(ctx context.Context, req PasswordResetCompleteRequest) error {
	if req.Token == "" {
		return fmt.Errorf("token is required")
	}
	if req.NewPassword == "" {
		return fmt.Errorf("new_password is required")
	}

	hash := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(hash[:])

	var resetID, userID int
	var expiresAt time.Time
	err := a.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT id, user_id, expires_at FROM %s WHERE token_hash = ? AND used = ?`, a.tableNames.UserPasswordResets))
		return db.QueryRowContext(ctx, query, tokenHash, false).Scan(&resetID, &userID, &expiresAt)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("invalid or expired token")
		}
		return fmt.Errorf("password reset complete query failed: %w", err)
	}
	if !expiresAt.After(time.Now()) {
		return fmt.Errorf("invalid or expired token")
	}

	now := time.Now()
	err = a.runDBOpWithReconnect(func(db *sql.DB) error {
		updUser := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET password = ?, updated_at = ? WHERE id = ?`, a.tableNames.Users))
		if _, err := db.ExecContext(ctx, updUser, req.NewPassword, now, userID); err != nil {
			return err
		}
		delSessions := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, a.tableNames.UserSessions))
		if _, err := db.ExecContext(ctx, delSessions, userID); err != nil {
			return err
		}
		updReset := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET used = ?, used_at = ? WHERE id = ?`, a.tableNames.UserPasswordResets))
		_, err := db.ExecContext(ctx, updReset, true, now, resetID)
		return err
	})
	if err != nil {
		return fmt.Errorf("password reset complete query failed: %w", err)
	}
	return nil
}

// claimStrings extracts ip_address/user_agent from a request's Claims map, mirroring
// p_request->'claims'->>'ip_address' / 'user_agent' in the plpgsql procedures.
func claimStrings(claims map[string]any) (ipAddress, userAgent string) {
	if claims == nil {
		return "", ""
	}
	if v, ok := claims["ip_address"].(string); ok {
		ipAddress = v
	}
	if v, ok := claims["user_agent"].(string); ok {
		userAgent = v
	}
	return ipAddress, userAgent
}

// jwtLoginDirect mirrors resolvespec_jwt_login.
func (a *JWTAuthenticator) jwtLoginDirect(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var userID int
	var email, roles sql.NullString
	var userLevel sql.NullInt64

	runQuery := func() error {
		query := rewritePlaceholders(a.getDB(), fmt.Sprintf(`SELECT id, email, user_level, roles FROM %s WHERE username = ? AND is_active = ?`, a.tableNames.Users))
		return a.getDB().QueryRowContext(ctx, query, req.Username, true).Scan(&userID, &email, &userLevel, &roles)
	}
	err := runQuery()
	if isDBClosed(err) {
		if reconnErr := a.reconnectDB(); reconnErr == nil {
			err = runQuery()
		}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	tokenString := fmt.Sprintf("token_%d_%d", userID, expiresAt.Unix())

	return &LoginResponse{
		Token: tokenString,
		User: &UserContext{
			UserID:    userID,
			UserName:  req.Username,
			Email:     email.String,
			UserLevel: int(userLevel.Int64),
			Roles:     parseRoles(roles.String),
		},
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

// jwtLogoutDirect mirrors resolvespec_jwt_logout (adds token to the blacklist table).
func (a *JWTAuthenticator) jwtLogoutDirect(ctx context.Context, req LogoutRequest) error {
	db := a.getDB()
	expiresAt := time.Now().Add(24 * time.Hour)
	query := rewritePlaceholders(db, fmt.Sprintf(`INSERT INTO %s (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, a.tableNames.TokenBlacklist))
	_, err := db.ExecContext(ctx, query, req.Token, req.UserID, expiresAt, time.Now())
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}
	return nil
}
