package security

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// DatabaseTwoFactorProvider implements TwoFactorAuthProvider using PostgreSQL stored procedures
// Procedure names are configurable via SQLNames (see DefaultSQLNames for defaults)
// See totp_database_schema.sql for procedure definitions
type DatabaseTwoFactorProvider struct {
	db         *sql.DB
	dbMu       sync.RWMutex
	dbFactory  func() (*sql.DB, error)
	totpGen    *TOTPGenerator
	sqlNames   *SQLNames
	tableNames *TableNames
	queryMode  QueryMode
	capability *dbCapability
}

// NewDatabaseTwoFactorProvider creates a new database-backed 2FA provider
func NewDatabaseTwoFactorProvider(db *sql.DB, config *TwoFactorConfig, names ...*SQLNames) *DatabaseTwoFactorProvider {
	if config == nil {
		config = DefaultTwoFactorConfig()
	}
	return &DatabaseTwoFactorProvider{
		db:         db,
		totpGen:    NewTOTPGenerator(config),
		sqlNames:   resolveSQLNames(names...),
		tableNames: DefaultTableNames(),
		capability: newDBCapability(),
	}
}

// WithDBFactory configures a factory used to reopen the database connection if it is closed.
func (p *DatabaseTwoFactorProvider) WithDBFactory(factory func() (*sql.DB, error)) *DatabaseTwoFactorProvider {
	p.dbFactory = factory
	return p
}

// WithTableNames configures Direct-mode table names. If names is nil, defaults are used.
func (p *DatabaseTwoFactorProvider) WithTableNames(names *TableNames) *DatabaseTwoFactorProvider {
	p.tableNames = resolveTableNames(names)
	return p
}

// WithQueryMode selects stored-procedure vs Direct-mode SQL (default ModeAuto).
func (p *DatabaseTwoFactorProvider) WithQueryMode(mode QueryMode) *DatabaseTwoFactorProvider {
	p.queryMode = mode
	return p
}

func (p *DatabaseTwoFactorProvider) getDB() *sql.DB {
	p.dbMu.RLock()
	defer p.dbMu.RUnlock()
	return p.db
}

func (p *DatabaseTwoFactorProvider) reconnectDB() error {
	if p.dbFactory == nil {
		return fmt.Errorf("no db factory configured for reconnect")
	}
	newDB, err := p.dbFactory()
	if err != nil {
		return err
	}
	p.dbMu.Lock()
	p.db = newDB
	p.dbMu.Unlock()
	if p.capability != nil {
		p.capability.reset()
	}
	return nil
}

func (p *DatabaseTwoFactorProvider) runDBOpWithReconnect(run func(*sql.DB) error) error {
	db := p.getDB()
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	err := run(db)
	if isDBClosed(err) {
		if reconnErr := p.reconnectDB(); reconnErr == nil {
			err = run(p.getDB())
		}
	}
	return err
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

	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPEnable) {
		return p.enable2FADirect(ctx, userID, secret, hashedCodes)
	}

	// Call stored procedure
	var success bool
	var errorMsg sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2, $3::jsonb)`, p.sqlNames.TOTPEnable)
	err = p.getDB().QueryRow(query, userID, secret, string(codesJSON)).Scan(&success, &errorMsg)
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
	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPDisable) {
		return p.disable2FADirect(ctx, userID)
	}

	var success bool
	var errorMsg sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1)`, p.sqlNames.TOTPDisable)
	err := p.getDB().QueryRow(query, userID).Scan(&success, &errorMsg)
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
	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPGetStatus) {
		return p.get2FAStatusDirect(ctx, userID)
	}

	var success bool
	var errorMsg sql.NullString
	var enabled bool

	query := fmt.Sprintf(`SELECT p_success, p_error, p_enabled FROM %s($1)`, p.sqlNames.TOTPGetStatus)
	err := p.getDB().QueryRow(query, userID).Scan(&success, &errorMsg, &enabled)
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
	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPGetSecret) {
		return p.get2FASecretDirect(ctx, userID)
	}

	var success bool
	var errorMsg sql.NullString
	var secret sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error, p_secret FROM %s($1)`, p.sqlNames.TOTPGetSecret)
	err := p.getDB().QueryRow(query, userID).Scan(&success, &errorMsg, &secret)
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

	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPRegenerateBackup) {
		if err := p.regenerateBackupCodesDirect(ctx, userID, hashedCodes); err != nil {
			return nil, err
		}
		return codes, nil
	}

	// Convert to JSON array
	codesJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal backup codes: %w", err)
	}

	// Call stored procedure
	var success bool
	var errorMsg sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2::jsonb)`, p.sqlNames.TOTPRegenerateBackup)
	err = p.getDB().QueryRow(query, userID, string(codesJSON)).Scan(&success, &errorMsg)
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

	ctx := context.Background()
	if !p.capability.ShouldUseProcedure(ctx, p.queryMode, p.getDB(), p.sqlNames.TOTPValidateBackupCode) {
		return p.validateBackupCodeDirect(ctx, userID, codeHash)
	}

	var success bool
	var errorMsg sql.NullString
	var valid bool

	query := fmt.Sprintf(`SELECT p_success, p_error, p_valid FROM %s($1, $2)`, p.sqlNames.TOTPValidateBackupCode)
	err := p.getDB().QueryRow(query, userID, codeHash).Scan(&success, &errorMsg, &valid)
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
