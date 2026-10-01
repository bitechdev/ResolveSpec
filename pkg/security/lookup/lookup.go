// Package lookup owns every database read and write the security package needs.
// pkg/security itself contains no SQL: it calls the store interfaces defined here.
//
// Each store has a procedure implementation (stored procedures, the Postgres default)
// and a direct implementation (tables through a dialect-driven query builder). Which
// one runs is decided per operation by Config.EffectiveMode.
package lookup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// AuthStore covers sessions, login, registration and password reset.
type AuthStore interface {
	Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error)
	Register(ctx context.Context, req sectypes.RegisterRequest) (*sectypes.LoginResponse, error)
	Logout(ctx context.Context, req sectypes.LogoutRequest) error
	// Session resolves a session token to its user. reference says where the token came
	// from ("authenticate", "cookie", "refresh"); the procedure backend passes it through.
	Session(ctx context.Context, token, reference string) (*sectypes.UserContext, error)
	// TouchSession records last activity for a session token. user is the context the
	// session resolved to; the procedure backend passes it to the update procedure.
	TouchSession(ctx context.Context, token string, user *sectypes.UserContext) error
	Refresh(ctx context.Context, refreshToken string) (*sectypes.LoginResponse, error)
	// LoginAPIKey logs in with a raw header/generic API key. Unknown, expired, inactive and
	// wrong-type keys all return the same error.
	LoginAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*sectypes.LoginResponse, error)
	JWTLogin(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error)
	JWTLogout(ctx context.Context, req sectypes.LogoutRequest) error
	ResetRequest(ctx context.Context, req sectypes.PasswordResetRequest) (*sectypes.PasswordResetResponse, error)
	ResetComplete(ctx context.Context, req sectypes.PasswordResetCompleteRequest) error
}

// ErrInvalidAPIKey is the single error LoginAPIKey returns for unknown, expired, inactive
// and wrong-type keys, so callers cannot tell them apart.
var ErrInvalidAPIKey = errors.New("invalid api key")

// KeyStore persists per-user auth keys. Hashing and raw-key generation happen in Go,
// so the store only sees key hashes.
type KeyStore interface {
	Create(ctx context.Context, req sectypes.CreateKeyRequest, keyHash string) (*sectypes.UserKey, error)
	List(ctx context.Context, userID int, keyType sectypes.KeyType) ([]sectypes.UserKey, error)
	// Delete soft-deletes a key after verifying ownership and returns its hash so callers can
	// invalidate caches. The hash is empty when the backend cannot report it.
	Delete(ctx context.Context, userID int, keyID int64) (keyHash string, err error)
	Validate(ctx context.Context, keyHash string, keyType sectypes.KeyType) (*sectypes.UserKey, error)
}

// OAuthClientStore persists the OAuth2 authorization server state (RFC 7591 clients,
// authorization codes, token introspection and revocation).
type OAuthClientStore interface {
	RegisterClient(ctx context.Context, client *sectypes.OAuthServerClient) (*sectypes.OAuthServerClient, error)
	GetClient(ctx context.Context, clientID string) (*sectypes.OAuthServerClient, error)
	SaveCode(ctx context.Context, code *sectypes.OAuthCode) error
	// ExchangeCode atomically consumes an authorization code.
	ExchangeCode(ctx context.Context, code string) (*sectypes.OAuthCode, error)
	Introspect(ctx context.Context, token string) (*sectypes.OAuthTokenInfo, error)
	Revoke(ctx context.Context, token string) error
}

// OAuthSession is the session row written after an OAuth2 client login.
type OAuthSession struct {
	SessionToken string
	UserID       int
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Provider     string
}

// OAuthRefreshSession is the stored token state needed to refresh an OAuth2 login.
type OAuthRefreshSession struct {
	UserID      int       `json:"user_id"`
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Expiry      time.Time `json:"expiry"`
}

// OAuthUserStore persists users and sessions created through OAuth2 client login.
type OAuthUserStore interface {
	GetOrCreateUser(ctx context.Context, user *sectypes.UserContext, provider string) (int, error)
	CreateSession(ctx context.Context, session OAuthSession) error
	GetByRefreshToken(ctx context.Context, refreshToken string) (*OAuthRefreshSession, error)
	UpdateRefreshToken(ctx context.Context, userID int, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken string, expiresAt time.Time) error
	GetUser(ctx context.Context, userID int) (*sectypes.UserContext, error)
}

// PasskeyCredentialRecord is a credential as persisted: byte fields are base64 text.
type PasskeyCredentialRecord struct {
	UserID          int
	CredentialID    string // base64
	PublicKey       string // base64
	AttestationType string
	SignCount       uint32
	Transports      []string
	BackupEligible  bool
	BackupState     bool
	Name            string
}

// PasskeyCredentialRef is a credential id and transports, as returned for a username lookup.
type PasskeyCredentialRef struct {
	CredentialID string   `json:"credential_id"`
	Transports   []string `json:"transports"`
}

// PasskeyStore persists WebAuthn credentials.
type PasskeyStore interface {
	Store(ctx context.Context, rec PasskeyCredentialRecord) (int64, error)
	// Get returns the owner and signature counter of a credential.
	Get(ctx context.Context, credentialID string) (userID int, signCount uint32, err error)
	UpdateCounter(ctx context.Context, credentialID string, newCounter uint32) (cloneWarning bool, err error)
	List(ctx context.Context, userID int) ([]sectypes.PasskeyCredential, error)
	Delete(ctx context.Context, userID int, credentialID string) error
	Rename(ctx context.Context, userID int, credentialID, name string) error
	ByUsername(ctx context.Context, username string) (userID int, creds []PasskeyCredentialRef, err error)
	Login(ctx context.Context, userID int, claims map[string]any) (*sectypes.LoginResponse, error)
}

// TOTPStore persists two-factor state. Backup codes arrive already hashed.
type TOTPStore interface {
	Enable(ctx context.Context, userID int, secret string, hashedCodes []string) error
	Disable(ctx context.Context, userID int) error
	Status(ctx context.Context, userID int) (bool, error)
	Secret(ctx context.Context, userID int) (string, error)
	RegenerateBackupCodes(ctx context.Context, userID int, hashedCodes []string) error
	ValidateBackupCode(ctx context.Context, userID int, codeHash string) (bool, error)
}

// PolicyStore loads column and row security rules. No rules is an empty result, never
// an error; failures are errors so callers fail closed.
type PolicyStore interface {
	ColumnSecurity(ctx context.Context, userID int, schema, table string) ([]sectypes.ColumnSecurity, error)
	RowSecurity(ctx context.Context, userRef any, schema, table string) (sectypes.RowSecurity, error)
}

// Provider bundles every store. Security constructors take a Provider.
type Provider struct {
	Auth        AuthStore
	Keys        KeyStore
	OAuthClient OAuthClientStore
	OAuthUser   OAuthUserStore
	Passkey     PasskeyStore
	TOTP        TOTPStore
	Policy      PolicyStore
}

// Config selects dialect, mode and naming. The zero value is valid: dialect detected from
// the driver, default mode per dialect, default procedure/table/column names.
type Config struct {
	// Dialect names a registered dialect ("postgres", "sqlite", "mysql", "mssql", or one added
	// with dialect.Register). Empty = detect from the driver.
	Dialect string
	// Mode is the default mode for every operation. See ModeDefault.
	Mode Mode
	// Overrides sets the mode per operation, e.g. direct for OpSession, procedure for OpLogin.
	Overrides map[Op]Mode
	// Procs overrides procedure names; empty fields keep the default.
	Procs ProcNames
	// Schema overrides table and column names; missing entries keep the default.
	Schema Schema
}

// Resolved is a Config merged with defaults and validated.
type Resolved struct {
	Config
	Procs  ProcNames
	Schema Schema
}

// Resolve merges c with the defaults and validates the result.
func (c Config) Resolve() (*Resolved, error) {
	if !c.Mode.valid() {
		return nil, fmt.Errorf("lookup: invalid mode %q", c.Mode)
	}
	for op, m := range c.Overrides {
		if !m.valid() {
			return nil, fmt.Errorf("lookup: invalid mode %q for %s", m, op)
		}
	}
	if c.Dialect != "" {
		if _, err := dialect.Get(c.Dialect); err != nil {
			return nil, fmt.Errorf("lookup: %w", err)
		}
	}
	procs := DefaultProcNames().Merge(c.Procs)
	if err := procs.Validate(); err != nil {
		return nil, err
	}
	schema := DefaultSchema().Merge(c.Schema)
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	return &Resolved{Config: c, Procs: procs, Schema: schema}, nil
}

// Registration conflicts reported by AuthStore.Register in direct mode.
var (
	ErrUsernameExists = errors.New("username already exists")
	ErrEmailExists    = errors.New("email already exists")
)
