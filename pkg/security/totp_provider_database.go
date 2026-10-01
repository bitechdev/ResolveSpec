package security

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/totp"
)

// DatabaseTwoFactorProvider implements TwoFactorAuthProvider on top of the lookup package
// (stored procedures on Postgres by default, direct SQL elsewhere).
// See lookup/database_schema.sql for procedure definitions
type DatabaseTwoFactorProvider struct {
	src     *lookupSource
	totpGen *totp.Generator
}

// NewDatabaseTwoFactorProvider creates a new database-backed 2FA provider
func NewDatabaseTwoFactorProvider(db *sql.DB, config *totp.Config) *DatabaseTwoFactorProvider {
	if config == nil {
		config = totp.DefaultConfig()
	}
	return &DatabaseTwoFactorProvider{src: newLookupSource(db), totpGen: totp.NewGenerator(config)}
}

// WithDBFactory configures a factory used to reopen the database connection if it is closed.
func (p *DatabaseTwoFactorProvider) WithDBFactory(factory func() (*sql.DB, error)) *DatabaseTwoFactorProvider {
	p.src.opts.DBFactory = factory
	return p
}

// WithLookup configures dialect, query mode and names. Call before first use.
func (p *DatabaseTwoFactorProvider) WithLookup(cfg lookup.Config) *DatabaseTwoFactorProvider {
	p.src.cfg = cfg
	return p
}

// WithLookupProvider uses an existing provider instead of building one.
func (p *DatabaseTwoFactorProvider) WithLookupProvider(lp *lookup.Provider) *DatabaseTwoFactorProvider {
	p.src.provider = lp
	return p
}

func (p *DatabaseTwoFactorProvider) store() lookup.TOTPStore { return p.src.get().TOTP }

// Generate2FASecret creates a new secret for a user
func (p *DatabaseTwoFactorProvider) Generate2FASecret(userID int, issuer, accountName string) (*TwoFactorSecret, error) {
	secret, err := p.totpGen.GenerateSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to generate secret: %w", err)
	}

	qrURL := p.totpGen.GenerateQRCodeURL(secret, issuer, accountName)

	backupCodes, err := totp.GenerateBackupCodes(10)
	if err != nil {
		return nil, fmt.Errorf("failed to generate backup codes: %w", err)
	}

	return &TwoFactorSecret{
		Secret:      secret,
		QRCodeURL:   qrURL,
		BackupCodes: backupCodes,
		Issuer:      issuer,
		AccountName: accountName,
	}, nil
}

// Validate2FACode verifies a TOTP code
func (p *DatabaseTwoFactorProvider) Validate2FACode(secret string, code string) (bool, error) {
	return p.totpGen.ValidateCode(secret, code)
}

// Enable2FA activates 2FA for a user
func (p *DatabaseTwoFactorProvider) Enable2FA(userID int, secret string, backupCodes []string) error {
	// Hash backup codes for secure storage
	hashedCodes := make([]string, len(backupCodes))
	for i, code := range backupCodes {
		hash := sha256.Sum256([]byte(code))
		hashedCodes[i] = hex.EncodeToString(hash[:])
	}

	ctx := context.Background()
	return p.store().Enable(ctx, userID, secret, hashedCodes)
}

// Disable2FA deactivates 2FA for a user
func (p *DatabaseTwoFactorProvider) Disable2FA(userID int) error {
	ctx := context.Background()
	return p.store().Disable(ctx, userID)
}

// Get2FAStatus checks if user has 2FA enabled
func (p *DatabaseTwoFactorProvider) Get2FAStatus(userID int) (bool, error) {
	ctx := context.Background()
	return p.store().Status(ctx, userID)
}

// Get2FASecret retrieves the user's 2FA secret
func (p *DatabaseTwoFactorProvider) Get2FASecret(userID int) (string, error) {
	ctx := context.Background()
	return p.store().Secret(ctx, userID)
}

// GenerateBackupCodes creates backup codes for 2FA
func (p *DatabaseTwoFactorProvider) GenerateBackupCodes(userID int, count int) ([]string, error) {
	codes, err := totp.GenerateBackupCodes(count)
	if err != nil {
		return nil, fmt.Errorf("failed to generate backup codes: %w", err)
	}

	// Hash backup codes for storage
	hashedCodes := make([]string, len(codes))
	for i, code := range codes {
		hash := sha256.Sum256([]byte(code))
		hashedCodes[i] = hex.EncodeToString(hash[:])
	}

	ctx := context.Background()
	if err := p.store().RegenerateBackupCodes(ctx, userID, hashedCodes); err != nil {
		return nil, err
	}

	// Return unhashed codes to user (only time they see them)
	return codes, nil
}

// ValidateBackupCode checks and consumes a backup code
func (p *DatabaseTwoFactorProvider) ValidateBackupCode(userID int, code string) (bool, error) {
	// Hash the code
	hash := sha256.Sum256([]byte(code))
	codeHash := hex.EncodeToString(hash[:])

	ctx := context.Background()
	return p.store().ValidateBackupCode(ctx, userID, codeHash)
}
