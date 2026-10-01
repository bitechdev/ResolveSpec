package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Auth implements lookup.AuthStore with stored procedures.
type Auth struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.AuthStore = (*Auth)(nil)

// NewAuth creates the procedure-backed AuthStore.
func NewAuth(run Runner, procs lookup.ProcNames) *Auth { return &Auth{run: run, procs: procs} }

// callData runs "SELECT p_success, p_error, p_data::text FROM proc($1::jsonb)".
func (a *Auth) callData(ctx context.Context, proc, queryErrOp string, arg any) (sql.NullString, error) {
	var success bool
	var errorMsg, dataJSON sql.NullString
	err := a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_data::text FROM %s($1::jsonb)`, proc) //nolint:gosec // G201: identifier comes from validated config
		return db.QueryRowContext(ctx, query, arg).Scan(&success, &errorMsg, &dataJSON)
	})
	if err != nil {
		return sql.NullString{}, fmt.Errorf("%s query failed: %w", queryErrOp, err)
	}
	if !success {
		return sql.NullString{}, failure(errorMsg, queryErrOp+" failed")
	}
	return dataJSON, nil
}

// Login implements lookup.AuthStore.
func (a *Auth) Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	reqJSON, err := json.Marshal(req) //nolint:gosec // G117: intentional: field must be serialized
	if err != nil {
		return nil, fmt.Errorf("failed to marshal login request: %w", err)
	}
	data, err := a.callData(ctx, a.procs.Login, "login", string(reqJSON))
	if err != nil {
		return nil, err
	}
	var response sectypes.LoginResponse
	if err := json.Unmarshal([]byte(data.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse login response: %w", err)
	}
	return &response, nil
}

// Register implements lookup.AuthStore.
func (a *Auth) Register(ctx context.Context, req sectypes.RegisterRequest) (*sectypes.LoginResponse, error) {
	reqJSON, err := json.Marshal(req) //nolint:gosec // G117: intentional: field must be serialized
	if err != nil {
		return nil, fmt.Errorf("failed to marshal register request: %w", err)
	}
	var success bool
	var errorMsg, dataJSON sql.NullString
	err = a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_data::text FROM %s($1::jsonb)`, a.procs.Register)
		return db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("register query failed: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "registration failed")
	}
	var response sectypes.LoginResponse
	if err := json.Unmarshal([]byte(dataJSON.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse register response: %w", err)
	}
	return &response, nil
}

// Logout implements lookup.AuthStore.
func (a *Auth) Logout(ctx context.Context, req sectypes.LogoutRequest) error {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal logout request: %w", err)
	}
	_, err = a.callData(ctx, a.procs.Logout, "logout", string(reqJSON))
	return err
}

// Session implements lookup.AuthStore.
func (a *Auth) Session(ctx context.Context, token, reference string) (*sectypes.UserContext, error) {
	var success bool
	var errorMsg, userJSON sql.NullString
	err := a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_user::text FROM %s($1, $2)`, a.procs.Session) //nolint:gosec // G701: identifier comes from trusted config, values are bound parameters
		return db.QueryRowContext(ctx, query, token, reference).Scan(&success, &errorMsg, &userJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("session query failed: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "invalid or expired session")
	}
	if !userJSON.Valid {
		return nil, fmt.Errorf("no user data in session")
	}
	var user sectypes.UserContext
	if err := json.Unmarshal([]byte(userJSON.String), &user); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}
	return &user, nil
}

// TouchSession implements lookup.AuthStore.
func (a *Auth) TouchSession(ctx context.Context, token string, user *sectypes.UserContext) error {
	userJSON, err := json.Marshal(user)
	if err != nil {
		return err
	}
	var success bool
	var errorMsg, updatedUserJSON sql.NullString
	return a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_user::text FROM %s($1, $2::jsonb)`, a.procs.SessionUpdate)
		return db.QueryRowContext(ctx, query, token, string(userJSON)).Scan(&success, &errorMsg, &updatedUserJSON) //nolint:gosec // G701: identifier comes from trusted config, values are bound parameters
	})
}

// Refresh implements lookup.AuthStore.
func (a *Auth) Refresh(ctx context.Context, refreshToken string) (*sectypes.LoginResponse, error) {
	// Get the current session to pass to refresh.
	var success bool
	var errorMsg, userJSON sql.NullString
	err := a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_user::text FROM %s($1, $2)`, a.procs.Session)
		return db.QueryRowContext(ctx, query, refreshToken, "refresh").Scan(&success, &errorMsg, &userJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token query failed: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "invalid refresh token")
	}

	var newSuccess bool
	var newErrorMsg, newUserJSON sql.NullString
	err = a.run.Run(func(db *sql.DB) error {
		refreshQuery := fmt.Sprintf(`SELECT p_success, p_error, p_user::text FROM %s($1, $2::jsonb)`, a.procs.RefreshToken)
		return db.QueryRowContext(ctx, refreshQuery, refreshToken, userJSON).Scan(&newSuccess, &newErrorMsg, &newUserJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token generation failed: %w", err)
	}
	if !newSuccess {
		return nil, failure(newErrorMsg, "failed to refresh token")
	}

	var userCtx sectypes.UserContext
	if err := json.Unmarshal([]byte(newUserJSON.String), &userCtx); err != nil {
		return nil, fmt.Errorf("failed to parse user context: %w", err)
	}

	// A resolvespec_refresh_token implementation that issues its own rotating
	// refresh token (independent of the access/session token) returns it
	// under claims.refresh_token, since UserContext has no dedicated field
	// for it. Surface that into LoginResponse.RefreshToken so callers don't
	// need to reach into User.Claims themselves. claims.expires_in
	// (seconds) similarly overrides the default access-token ExpiresIn when
	// the procedure provides a real value. Implementations that don't set
	// these claims keep today's behavior unchanged (empty RefreshToken,
	// 24h ExpiresIn default).
	resp := &sectypes.LoginResponse{
		Token:     userCtx.SessionID, // New session token from stored procedure
		User:      &userCtx,
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}
	if rt, ok := userCtx.Claims["refresh_token"].(string); ok && rt != "" {
		resp.RefreshToken = rt
	}
	if expiresIn, ok := userCtx.Claims["expires_in"].(float64); ok && expiresIn > 0 {
		resp.ExpiresIn = int64(expiresIn)
	}
	return resp, nil
}

// LoginAPIKey implements lookup.AuthStore. Unknown, expired and inactive keys all return
// lookup.ErrInvalidAPIKey; the raw key is never logged.
func (a *Auth) LoginAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*sectypes.LoginResponse, error) {
	if rawKey == "" {
		return nil, lookup.ErrInvalidAPIKey
	}
	reqJSON, err := json.Marshal(map[string]any{"api_key": rawKey, "claims": claims})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal api key login request: %w", err)
	}
	var success bool
	var errorMsg, dataJSON sql.NullString
	err = a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_data::text FROM %s($1::jsonb)`, a.procs.LoginAPIKey)
		return db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("api key login query failed: %w", err)
	}
	if !success {
		return nil, lookup.ErrInvalidAPIKey
	}
	var response sectypes.LoginResponse
	if err := json.Unmarshal([]byte(dataJSON.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse api key login response: %w", err)
	}
	return &response, nil
}

// JWTLogin implements lookup.AuthStore. The password is verified inside the procedure;
// the hash is never returned. The token is a placeholder until JWT signing is wired in.
func (a *Auth) JWTLogin(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	var success bool
	var errorMsg sql.NullString
	var userJSON []byte
	err := a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_user FROM %s($1, $2)`, a.procs.JWTLogin)
		return db.QueryRowContext(ctx, query, req.Username, req.Password).Scan(&success, &errorMsg, &userJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("login query failed: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "invalid credentials")
	}
	var user struct {
		ID        int    `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		UserLevel int    `json:"user_level"`
		Roles     string `json:"roles"`
	}
	if err := json.Unmarshal(userJSON, &user); err != nil {
		return nil, fmt.Errorf("failed to parse user data: %w", err)
	}
	roles := []string{}
	if user.Roles != "" {
		roles = strings.Split(user.Roles, ",")
	}
	expiresAt := time.Now().Add(24 * time.Hour)
	return &sectypes.LoginResponse{
		Token: fmt.Sprintf("token_%d_%d", user.ID, expiresAt.Unix()),
		User: &sectypes.UserContext{
			UserID:    user.ID,
			UserName:  user.Username,
			Email:     user.Email,
			UserLevel: user.UserLevel,
			Roles:     roles,
		},
		ExpiresIn: int64(24 * time.Hour.Seconds()),
	}, nil
}

// JWTLogout implements lookup.AuthStore.
func (a *Auth) JWTLogout(ctx context.Context, req sectypes.LogoutRequest) error {
	var success bool
	var errorMsg sql.NullString
	err := a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2)`, a.procs.JWTLogout)
		return db.QueryRowContext(ctx, query, req.Token, req.UserID).Scan(&success, &errorMsg)
	})
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}
	if !success {
		return failure(errorMsg, "logout failed")
	}
	return nil
}

// ResetRequest implements lookup.AuthStore.
func (a *Auth) ResetRequest(ctx context.Context, req sectypes.PasswordResetRequest) (*sectypes.PasswordResetResponse, error) {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal password reset request: %w", err)
	}
	data, err := a.callData(ctx, a.procs.PasswordResetRequest, "password reset request", string(reqJSON))
	if err != nil {
		return nil, err
	}
	var response sectypes.PasswordResetResponse
	if data.Valid && data.String != "" {
		if err := json.Unmarshal([]byte(data.String), &response); err != nil {
			return nil, fmt.Errorf("failed to parse password reset response: %w", err)
		}
	}
	return &response, nil
}

// ResetComplete implements lookup.AuthStore.
func (a *Auth) ResetComplete(ctx context.Context, req sectypes.PasswordResetCompleteRequest) error {
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal password reset complete request: %w", err)
	}
	var success bool
	var errorMsg sql.NullString
	err = a.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1::jsonb)`, a.procs.PasswordResetComplete)
		return db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg)
	})
	if err != nil {
		return fmt.Errorf("password reset complete query failed: %w", err)
	}
	if !success {
		return failure(errorMsg, "password reset failed")
	}
	return nil
}
