package direct

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

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

const sessionLifetime = 24 * time.Hour

// AuthOptions tunes Auth.
type AuthOptions struct {
	// UpgradePasswordHash rewrites legacy cleartext passwords as bcrypt on a successful login.
	UpgradePasswordHash bool
}

// Auth implements lookup.AuthStore on the tables. Passwords are verified with bcrypt;
// legacy cleartext rows are accepted at login and only rewritten when UpgradePasswordHash
// is set. Registration never honours client-supplied user_level or roles. Multi-step writes
// (login, register, refresh, reset) run in one transaction.
type Auth struct {
	*Base
	opts AuthOptions
}

var _ lookup.AuthStore = (*Auth)(nil)

// NewAuth creates the direct AuthStore.
func NewAuth(b *Base, opts AuthOptions) *Auth { return &Auth{Base: b, opts: opts} }

// GenerateSessionToken returns "sess_<64 hex>_<unix>".
func GenerateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("sess_%s_%d", hex.EncodeToString(buf), time.Now().Unix()), nil
}

// ParseRoles splits the comma-separated roles column.
func ParseRoles(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func claimStrings(claims map[string]any) (ip, ua string) {
	if claims == nil {
		return "", ""
	}
	if v, ok := claims["ip_address"].(string); ok {
		ip = v
	}
	if v, ok := claims["user_agent"].(string); ok {
		ua = v
	}
	return ip, ua
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// userRow is the users columns every session-bearing response needs.
type userRow struct {
	id               int
	username         sql.NullString
	email            sql.NullString
	roles            sql.NullString
	programUserTable sql.NullString
	userLevel        sql.NullInt64
	programUserID    sql.NullInt64
}

func (u *userRow) context(sessionID string) *sectypes.UserContext {
	return &sectypes.UserContext{
		UserID:           u.id,
		UserName:         u.username.String,
		Email:            u.email.String,
		UserLevel:        int(u.userLevel.Int64),
		SessionID:        sessionID,
		Roles:            ParseRoles(u.roles.String),
		ProgramUserID:    int(u.programUserID.Int64),
		ProgramUserTable: u.programUserTable.String,
	}
}

// insertSession writes a session row and stamps the user's last login.
func (a *Auth) insertSession(ctx context.Context, q Querier, token string, userID int64, expiresAt time.Time, ip, ua string, now time.Time) error {
	err := a.Insert(lookup.EntityUserSessions).Set(
		Set(lookup.SessionsToken, token),
		Set(lookup.SessionsUserID, userID),
		Set(lookup.SessionsExpiresAt, expiresAt),
		Set(lookup.SessionsIPAddress, ip),
		Set(lookup.SessionsUserAgent, ua),
		Set(lookup.SessionsLastActivityAt, now),
		Set(lookup.SessionsCreatedAt, now),
	).Exec(ctx, q)
	if err != nil {
		return err
	}
	return a.touchLastLogin(ctx, q, userID, now)
}

func (a *Auth) touchLastLogin(ctx context.Context, q Querier, userID int64, now time.Time) error {
	_, err := a.Update(lookup.EntityUsers).Set(Set(lookup.UsersLastLoginAt, now)).Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
	return err
}

// Login implements lookup.AuthStore.
func (a *Auth) Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	var userID int
	var email, roles, programUserTable, storedPassword sql.NullString
	var userLevel, programUserID sql.NullInt64

	err := a.do(func(q Querier) error {
		return a.From(lookup.EntityUsers).
			Cols(lookup.UsersID, lookup.UsersEmail, lookup.UsersUserLevel, lookup.UsersRoles,
				lookup.UsersProgramUserID, lookup.UsersProgramUserTable, lookup.UsersPassword).
			Where(Eq(lookup.UsersUsername, req.Username), Eq(lookup.UsersIsActive, true)).
			QueryRow(ctx, q, &userID, &email, &userLevel, &roles, &programUserID, &programUserTable, &storedPassword)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			BurnPasswordCheck(req.Password)
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	ok, needsRehash := VerifyPassword(storedPassword.String, req.Password)
	if !ok {
		if storedPassword.String == "" {
			BurnPasswordCheck(req.Password)
		}
		return nil, fmt.Errorf("invalid credentials")
	}
	if needsRehash && a.opts.UpgradePasswordHash {
		a.upgradePasswordHash(ctx, userID, req.Password)
	}

	token, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	now := a.Now()
	ip, ua := claimStrings(req.Claims)
	err = a.tx(ctx, func(q Querier) error {
		return a.insertSession(ctx, q, token, int64(userID), now.Add(sessionLifetime), ip, ua, now)
	})
	if err != nil {
		return nil, fmt.Errorf("login query failed: %w", err)
	}

	return &sectypes.LoginResponse{
		Token: token,
		User: &sectypes.UserContext{
			UserID:           userID,
			UserName:         req.Username,
			Email:            email.String,
			UserLevel:        int(userLevel.Int64),
			Roles:            ParseRoles(roles.String),
			SessionID:        token,
			ProgramUserID:    int(programUserID.Int64),
			ProgramUserTable: programUserTable.String,
		},
		ExpiresIn: int64(sessionLifetime.Seconds()),
	}, nil
}

// upgradePasswordHash replaces a legacy cleartext password with a bcrypt hash. Failure is
// logged and ignored: the login itself already succeeded.
func (a *Auth) upgradePasswordHash(ctx context.Context, userID int, password string) {
	h, err := HashPassword(password)
	if err != nil {
		return
	}
	err = a.do(func(q Querier) error {
		_, err := a.Update(lookup.EntityUsers).
			Set(Set(lookup.UsersPassword, h), Set(lookup.UsersUpdatedAt, a.Now())).
			Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		logger.Warn("failed to upgrade legacy password hash for user %d: %v", userID, err)
	}
}

// Register implements lookup.AuthStore.
func (a *Auth) Register(ctx context.Context, req sectypes.RegisterRequest) (*sectypes.LoginResponse, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if req.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}

	// Privileges are never taken from the request: self-registration always creates an
	// unprivileged user.
	const userLevel = 0
	now := a.Now()
	ip, ua := claimStrings(req.Claims)

	var userID int64
	err = a.tx(ctx, func(q Querier) error {
		exists, err := a.From(lookup.EntityUsers).Cols(lookup.UsersID).Where(Eq(lookup.UsersUsername, req.Username)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			return lookup.ErrUsernameExists
		}
		exists, err = a.From(lookup.EntityUsers).Cols(lookup.UsersID).Where(Eq(lookup.UsersEmail, req.Email)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			return lookup.ErrEmailExists
		}
		userID, err = a.Insert(lookup.EntityUsers).Set(
			Set(lookup.UsersUsername, req.Username),
			Set(lookup.UsersEmail, req.Email),
			Set(lookup.UsersPassword, hash),
			Set(lookup.UsersUserLevel, userLevel),
			Set(lookup.UsersRoles, ""),
			Set(lookup.UsersIsActive, true),
			Set(lookup.UsersCreatedAt, now),
			Set(lookup.UsersUpdatedAt, now),
			Set(lookup.UsersProgramUserID, 0),
			Set(lookup.UsersProgramUserTable, ""),
		).ExecID(ctx, q, lookup.UsersID)
		if err != nil {
			return err
		}
		return a.insertSession(ctx, q, token, userID, now.Add(sessionLifetime), ip, ua, now)
	})
	if err != nil {
		if errors.Is(err, lookup.ErrUsernameExists) || errors.Is(err, lookup.ErrEmailExists) {
			return nil, err
		}
		return nil, fmt.Errorf("register query failed: %w", err)
	}

	return &sectypes.LoginResponse{
		Token: token,
		User: &sectypes.UserContext{
			UserID:    int(userID),
			UserName:  req.Username,
			Email:     req.Email,
			UserLevel: userLevel,
			Roles:     ParseRoles(""),
			SessionID: token,
		},
		ExpiresIn: int64(sessionLifetime.Seconds()),
	}, nil
}

// Logout implements lookup.AuthStore.
func (a *Auth) Logout(ctx context.Context, req sectypes.LogoutRequest) error {
	token := strings.TrimPrefix(strings.TrimPrefix(req.Token, "Bearer "), "bearer ")
	var rows int64
	err := a.do(func(q Querier) error {
		var err error
		rows, err = a.Delete(lookup.EntityUserSessions).
			Where(Eq(lookup.SessionsToken, token), Eq(lookup.SessionsUserID, req.UserID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}

// sessionUser selects the user behind a live session token.
func (a *Auth) sessionUser(ctx context.Context, q Querier, token string, extra ...lookup.Column) (*userRow, []any, error) {
	var u userRow
	dest := []any{&u.id, &u.username, &u.email, &u.userLevel, &u.roles, &u.programUserID, &u.programUserTable}
	cols := []lookup.Column{lookup.SessionsUserID, lookup.UsersUsername, lookup.UsersEmail, lookup.UsersUserLevel,
		lookup.UsersRoles, lookup.UsersProgramUserID, lookup.UsersProgramUserTable}
	extras := make([]any, len(extra))
	for i, c := range extra {
		cols = append(cols, c)
		extras[i] = new(sql.NullString)
		dest = append(dest, extras[i])
	}
	err := a.From(lookup.EntityUserSessions).Cols(cols...).
		Join(lookup.EntityUsers, EqCol(lookup.SessionsUserID, lookup.UsersID)).
		Where(Eq(lookup.SessionsToken, token), Gt(lookup.SessionsExpiresAt, a.Now()), Eq(lookup.UsersIsActive, true)).
		QueryRow(ctx, q, dest...)
	return &u, extras, err
}

// Session implements lookup.AuthStore. reference is only meaningful to the procedure backend.
func (a *Auth) Session(ctx context.Context, token, _ string) (*sectypes.UserContext, error) {
	var u *userRow
	err := a.do(func(q Querier) error {
		var err error
		u, _, err = a.sessionUser(ctx, q, token)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired session")
		}
		return nil, fmt.Errorf("session query failed: %w", err)
	}
	return u.context(token), nil
}

// TouchSession implements lookup.AuthStore.
func (a *Auth) TouchSession(ctx context.Context, token string, _ *sectypes.UserContext) error {
	return a.do(func(q Querier) error {
		now := a.Now()
		_, err := a.Update(lookup.EntityUserSessions).Set(Set(lookup.SessionsLastActivityAt, now)).
			Where(Eq(lookup.SessionsToken, token), Gt(lookup.SessionsExpiresAt, now)).Exec(ctx, q)
		return err
	})
}

// Refresh implements lookup.AuthStore: the old session is replaced by a new one.
func (a *Auth) Refresh(ctx context.Context, oldToken string) (*sectypes.LoginResponse, error) {
	var u *userRow
	var extras []any
	err := a.do(func(q Querier) error {
		var err error
		u, extras, err = a.sessionUser(ctx, q, oldToken, lookup.SessionsIPAddress, lookup.SessionsUserAgent)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired refresh token")
		}
		return nil, fmt.Errorf("refresh token query failed: %w", err)
	}
	ip := extras[0].(*sql.NullString).String
	ua := extras[1].(*sql.NullString).String

	newToken, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	now := a.Now()
	err = a.tx(ctx, func(q Querier) error {
		err := a.Insert(lookup.EntityUserSessions).Set(
			Set(lookup.SessionsToken, newToken),
			Set(lookup.SessionsUserID, u.id),
			Set(lookup.SessionsExpiresAt, now.Add(sessionLifetime)),
			Set(lookup.SessionsIPAddress, ip),
			Set(lookup.SessionsUserAgent, ua),
			Set(lookup.SessionsLastActivityAt, now),
			Set(lookup.SessionsCreatedAt, now),
		).Exec(ctx, q)
		if err != nil {
			return err
		}
		_, err = a.Delete(lookup.EntityUserSessions).Where(Eq(lookup.SessionsToken, oldToken)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token generation failed: %w", err)
	}
	return &sectypes.LoginResponse{
		Token:     newToken,
		User:      u.context(newToken),
		ExpiresIn: int64(sessionLifetime.Seconds()),
	}, nil
}

// apiKeyTypes are the key types accepted by LoginAPIKey.
var apiKeyTypes = []any{string(sectypes.KeyTypeHeaderAPI), string(sectypes.KeyTypeGenericAPI)}

// LoginAPIKey implements lookup.AuthStore. Unknown, expired, inactive and wrong-type keys
// (and inactive users) all return lookup.ErrInvalidAPIKey; the raw key is never logged.
func (a *Auth) LoginAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*sectypes.LoginResponse, error) {
	if rawKey == "" {
		return nil, lookup.ErrInvalidAPIKey
	}
	now := a.Now()
	var keyID int64
	var u userRow
	err := a.do(func(q Querier) error {
		return a.From(lookup.EntityUserKeys).
			Cols(lookup.KeysID, lookup.UsersID, lookup.UsersUsername, lookup.UsersEmail, lookup.UsersUserLevel,
				lookup.UsersRoles, lookup.UsersProgramUserID, lookup.UsersProgramUserTable).
			Join(lookup.EntityUsers, EqCol(lookup.KeysUserID, lookup.UsersID)).
			Where(
				Eq(lookup.KeysKeyHash, sectypes.HashKey(rawKey)),
				In(lookup.KeysKeyType, apiKeyTypes...),
				Eq(lookup.KeysIsActive, true),
				Or(IsNull(lookup.KeysExpiresAt), Gt(lookup.KeysExpiresAt, now)),
				Eq(lookup.UsersIsActive, true),
			).QueryRow(ctx, q, &keyID, &u.id, &u.username, &u.email, &u.userLevel, &u.roles, &u.programUserID, &u.programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, lookup.ErrInvalidAPIKey
		}
		return nil, fmt.Errorf("api key login query failed: %w", err)
	}

	token, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	ip, ua := claimStrings(claims)
	err = a.tx(ctx, func(q Querier) error {
		if err := a.insertSession(ctx, q, token, int64(u.id), now.Add(sessionLifetime), ip, ua, now); err != nil {
			return err
		}
		_, err := a.Update(lookup.EntityUserKeys).Set(Set(lookup.KeysLastUsedAt, now)).Where(Eq(lookup.KeysID, keyID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("api key login query failed: %w", err)
	}
	return &sectypes.LoginResponse{
		Token:     token,
		User:      u.context(token),
		ExpiresIn: int64(sessionLifetime.Seconds()),
	}, nil
}

// JWTLogin implements lookup.AuthStore (mirrors resolvespec_jwt_login). The token is a
// placeholder until JWT signing is wired in.
func (a *Auth) JWTLogin(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	var userID int
	var email, roles, storedPassword sql.NullString
	var userLevel sql.NullInt64
	err := a.do(func(q Querier) error {
		return a.From(lookup.EntityUsers).
			Cols(lookup.UsersID, lookup.UsersEmail, lookup.UsersUserLevel, lookup.UsersRoles, lookup.UsersPassword).
			Where(Eq(lookup.UsersUsername, req.Username), Eq(lookup.UsersIsActive, true)).
			QueryRow(ctx, q, &userID, &email, &userLevel, &roles, &storedPassword)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			BurnPasswordCheck(req.Password)
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("login query failed: %w", err)
	}
	ok, needsRehash := VerifyPassword(storedPassword.String, req.Password)
	if !ok {
		if storedPassword.String == "" {
			BurnPasswordCheck(req.Password)
		}
		return nil, fmt.Errorf("invalid credentials")
	}
	if needsRehash && a.opts.UpgradePasswordHash {
		a.upgradePasswordHash(ctx, userID, req.Password)
	}
	expiresAt := a.Now().Add(sessionLifetime)
	return &sectypes.LoginResponse{
		Token: fmt.Sprintf("token_%d_%d", userID, expiresAt.Unix()),
		User: &sectypes.UserContext{
			UserID:    userID,
			UserName:  req.Username,
			Email:     email.String,
			UserLevel: int(userLevel.Int64),
			Roles:     ParseRoles(roles.String),
		},
		ExpiresIn: int64(sessionLifetime.Seconds()),
	}, nil
}

// JWTLogout implements lookup.AuthStore: the token goes on the blacklist.
func (a *Auth) JWTLogout(ctx context.Context, req sectypes.LogoutRequest) error {
	now := a.Now()
	err := a.do(func(q Querier) error {
		return a.Insert(lookup.EntityTokenBlacklist).Set(
			Set(lookup.BlacklistToken, req.Token),
			Set(lookup.BlacklistUserID, req.UserID),
			Set(lookup.BlacklistExpiresAt, now.Add(sessionLifetime)),
			Set(lookup.BlacklistCreatedAt, now),
		).Exec(ctx, q)
	})
	if err != nil {
		return fmt.Errorf("logout query failed: %w", err)
	}
	return nil
}

// ResetRequest implements lookup.AuthStore. An unknown user yields a generic empty success
// so accounts cannot be enumerated.
func (a *Auth) ResetRequest(ctx context.Context, req sectypes.PasswordResetRequest) (*sectypes.PasswordResetResponse, error) {
	if req.Email == "" && req.Username == "" {
		return nil, fmt.Errorf("email or username is required")
	}
	var userID int
	err := a.do(func(q Querier) error {
		lookupCol, val := lookup.UsersUsername, req.Username
		if req.Email != "" {
			lookupCol, val = lookup.UsersEmail, req.Email
		}
		return a.From(lookup.EntityUsers).Cols(lookup.UsersID).
			Where(Eq(lookupCol, val), Eq(lookup.UsersIsActive, true)).QueryRow(ctx, q, &userID)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &sectypes.PasswordResetResponse{Token: "", ExpiresIn: 0}, nil
		}
		return nil, fmt.Errorf("password reset request query failed: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("failed to generate reset token: %w", err)
	}
	rawToken := hex.EncodeToString(raw)
	now := a.Now()
	err = a.tx(ctx, func(q Querier) error {
		if _, err := a.Delete(lookup.EntityUserPasswordResets).
			Where(Eq(lookup.ResetsUserID, userID), Eq(lookup.ResetsUsed, false)).Exec(ctx, q); err != nil {
			return err
		}
		return a.Insert(lookup.EntityUserPasswordResets).Set(
			Set(lookup.ResetsUserID, userID),
			Set(lookup.ResetsTokenHash, sha256Hex(rawToken)),
			Set(lookup.ResetsExpiresAt, now.Add(time.Hour)),
			Set(lookup.ResetsCreatedAt, now),
			Set(lookup.ResetsUsed, false),
		).Exec(ctx, q)
	})
	if err != nil {
		return nil, fmt.Errorf("password reset request query failed: %w", err)
	}
	return &sectypes.PasswordResetResponse{Token: rawToken, ExpiresIn: 3600}, nil
}

// ResetComplete implements lookup.AuthStore: sets the new password, ends every session of
// the user and consumes the reset token, atomically.
func (a *Auth) ResetComplete(ctx context.Context, req sectypes.PasswordResetCompleteRequest) error {
	if req.Token == "" {
		return fmt.Errorf("token is required")
	}
	if req.NewPassword == "" {
		return fmt.Errorf("new_password is required")
	}
	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	tokenHash := sha256Hex(req.Token)

	now := a.Now()
	var resetID, userID int
	var expiresAt time.Time
	err = a.do(func(q Querier) error {
		return a.From(lookup.EntityUserPasswordResets).
			Cols(lookup.ResetsID, lookup.ResetsUserID, lookup.ResetsExpiresAt).
			Where(Eq(lookup.ResetsTokenHash, tokenHash), Eq(lookup.ResetsUsed, false)).
			QueryRow(ctx, q, &resetID, &userID, a.timeDest(&expiresAt))
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("invalid or expired token")
		}
		return fmt.Errorf("password reset complete query failed: %w", err)
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("invalid or expired token")
	}

	err = a.tx(ctx, func(q Querier) error {
		if _, err := a.Update(lookup.EntityUsers).
			Set(Set(lookup.UsersPassword, newHash), Set(lookup.UsersUpdatedAt, now)).
			Where(Eq(lookup.UsersID, userID)).Exec(ctx, q); err != nil {
			return err
		}
		if _, err := a.Delete(lookup.EntityUserSessions).Where(Eq(lookup.SessionsUserID, userID)).Exec(ctx, q); err != nil {
			return err
		}
		_, err := a.Update(lookup.EntityUserPasswordResets).
			Set(Set(lookup.ResetsUsed, true), Set(lookup.ResetsUsedAt, now)).
			Where(Eq(lookup.ResetsID, resetID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return fmt.Errorf("password reset complete query failed: %w", err)
	}
	return nil
}
