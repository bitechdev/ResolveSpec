package security

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DatabaseTwoFactorProvider implements TwoFactorAuthProvider using PostgreSQL stored procedures
// Requires stored procedures: resolvespec_totp_enable, resolvespec_totp_disable,
// resolvespec_totp_get_status, resolvespec_totp_get_secret,
// resolvespec_totp_regenerate_backup_codes, resolvespec_totp_validate_backup_code
// See totp_database_schema.sql for procedure definitions
type DatabaseTwoFactorProvider struct {
	db      *sql.DB
	totpGen *TOTPGenerator
}

// NewDatabaseTwoFactorProvider creates a new database-backed 2FA provider
func NewDatabaseTwoFactorProvider(db *sql.DB, config *TwoFactorConfig) *DatabaseTwoFactorProvider {
	if config == nil {
		config = DefaultTwoFactorConfig()
	}
	return &DatabaseTwoFactorProvider{
		db:      db,
		totpGen: NewTOTPGenerator(config),
	}
}

// Generate2FASecret creates a new secret for a user
func (p *DatabaseTwoFactorProvider) Generate2FASecret(userID int, issuer, accountName string) (*TwoFactorSecret, error) {
	secret, err := p.totpGen.GenerateSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to generate secret: %w", err)
	}

	qrURL := p.totpGen.GenerateQRCodeURL(secret, issuer, accountName)

	backupCodes, err := GenerateBackupCodes(10)
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

	// Convert to JSON array
	codesJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	// Call stored procedure
	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_totp_enable($1, $2, $3::jsonb)`
	err = p.db.QueryRow(query, userID, secret, string(codesJSON)).Scan(&success, &errorMsg)
	if err != nil {
		return fmt.Errorf("enable 2FA query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("failed to enable 2FA")
	}

	return nil
}

// Disable2FA deactivates 2FA for a user
func (p *DatabaseTwoFactorProvider) Disable2FA(userID int) error {
	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_totp_disable($1)`
	err := p.db.QueryRow(query, userID).Scan(&success, &errorMsg)
	if err != nil {
		return fmt.Errorf("disable 2FA query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("failed to disable 2FA")
	}

	return nil
}

// Get2FAStatus checks if user has 2FA enabled
func (p *DatabaseTwoFactorProvider) Get2FAStatus(userID int) (bool, error) {
	var success bool
	var errorMsg sql.NullString
	var enabled bool

	query := `SELECT p_success, p_error, p_enabled FROM resolvespec_totp_get_status($1)`
	err := p.db.QueryRow(query, userID).Scan(&success, &errorMsg, &enabled)
	if err != nil {
		return false, fmt.Errorf("get 2FA status query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return false, fmt.Errorf("%s", errorMsg.String)
		}
		return false, fmt.Errorf("failed to get 2FA status")
	}

	return enabled, nil
}

// Get2FASecret retrieves the user's 2FA secret
func (p *DatabaseTwoFactorProvider) Get2FASecret(userID int) (string, error) {
	var success bool
	var errorMsg sql.NullString
	var secret sql.NullString

	query := `SELECT p_success, p_error, p_secret FROM resolvespec_totp_get_secret($1)`
	err := p.db.QueryRow(query, userID).Scan(&success, &errorMsg, &secret)
	if err != nil {
		return "", fmt.Errorf("get 2FA secret query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return "", fmt.Errorf("%s", errorMsg.String)
		}
		return "", fmt.Errorf("failed to get 2FA secret")
	}

	if !secret.Valid {
		return "", fmt.Errorf("2FA secret not found")
	}

	return secret.String, nil
}

// GenerateBackupCodes creates backup codes for 2FA
func (p *DatabaseTwoFactorProvider) GenerateBackupCodes(userID int, count int) ([]string, error) {
	codes, err := GenerateBackupCodes(count)
	if err != nil {
		return nil, fmt.Errorf("failed to generate backup codes: %w", err)
	}

	// Hash backup codes for storage
	hashedCodes := make([]string, len(codes))
	for i, code := range codes {
		hash := sha256.Sum256([]byte(code))
		hashedCodes[i] = hex.EncodeToString(hash[:])
	}

	// Convert to JSON array
	codesJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	// Call stored procedure
	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_totp_regenerate_backup_codes($1, $2::jsonb)`
	err = p.db.QueryRow(query, userID, string(codesJSON)).Scan(&success, &errorMsg)
	if err != nil {
		return nil, fmt.Errorf("regenerate backup codes query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("failed to regenerate backup codes")
	}

	// Return unhashed codes to user (only time they see them)
	return codes, nil
}

// ValidateBackupCode checks and consumes a backup code
func (p *DatabaseTwoFactorProvider) ValidateBackupCode(userID int, code string) (bool, error) {
	// Hash the code
	hash := sha256.Sum256([]byte(code))
	codeHash := hex.EncodeToString(hash[:])

	var success bool
	var errorMsg sql.NullString
	var valid bool

	query := `SELECT p_success, p_error, p_valid FROM resolvespec_totp_validate_backup_code($1, $2)`
	err := p.db.QueryRow(query, userID, codeHash).Scan(&success, &errorMsg, &valid)
	if err != nil {
		return false, fmt.Errorf("validate backup code query failed: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return false, fmt.Errorf("%s", errorMsg.String)
		}
		return false, nil
	}

	return valid, nil
}
