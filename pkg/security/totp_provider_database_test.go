package security_test

import (
	"database/sql"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// Note: These tests require a PostgreSQL database with the schema from totp_database_schema.sql
// Set TEST_DATABASE_URL environment variable or skip tests

func setupTestDB(t *testing.T) *sql.DB {
	// Skip if no test database configured
	t.Skip("Database tests require TEST_DATABASE_URL environment variable")
	return nil
}

func TestDatabaseTwoFactorProvider_Enable2FA(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	// Generate secret and backup codes
	secret, err := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	if err != nil {
		t.Fatalf("Generate2FASecret() error = %v", err)
	}

	// Enable 2FA
	err = provider.Enable2FA(1, secret.Secret, secret.BackupCodes)
	if err != nil {
		t.Errorf("Enable2FA() error = %v", err)
	}

	// Verify enabled
	enabled, err := provider.Get2FAStatus(1)
	if err != nil {
		t.Fatalf("Get2FAStatus() error = %v", err)
	}

	if !enabled {
		t.Error("Get2FAStatus() = false, want true")
	}
}

func TestDatabaseTwoFactorProvider_Disable2FA(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	// Enable first
	secret, _ := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	provider.Enable2FA(1, secret.Secret, secret.BackupCodes)

	// Disable
	err := provider.Disable2FA(1)
	if err != nil {
		t.Errorf("Disable2FA() error = %v", err)
	}

	// Verify disabled
	enabled, err := provider.Get2FAStatus(1)
	if err != nil {
		t.Fatalf("Get2FAStatus() error = %v", err)
	}

	if enabled {
		t.Error("Get2FAStatus() = true, want false")
	}
}

func TestDatabaseTwoFactorProvider_GetSecret(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	// Enable 2FA
	secret, _ := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	provider.Enable2FA(1, secret.Secret, secret.BackupCodes)

	// Retrieve secret
	retrieved, err := provider.Get2FASecret(1)
	if err != nil {
		t.Errorf("Get2FASecret() error = %v", err)
	}

	if retrieved != secret.Secret {
		t.Errorf("Get2FASecret() = %v, want %v", retrieved, secret.Secret)
	}
}

func TestDatabaseTwoFactorProvider_ValidateBackupCode(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	// Enable 2FA
	secret, _ := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	provider.Enable2FA(1, secret.Secret, secret.BackupCodes)

	// Validate backup code
	valid, err := provider.ValidateBackupCode(1, secret.BackupCodes[0])
	if err != nil {
		t.Errorf("ValidateBackupCode() error = %v", err)
	}

	if !valid {
		t.Error("ValidateBackupCode() = false, want true")
	}

	// Try to use same code again
	valid, err = provider.ValidateBackupCode(1, secret.BackupCodes[0])
	if err == nil {
		t.Error("ValidateBackupCode() should error on reuse")
	}

	// Try invalid code
	valid, err = provider.ValidateBackupCode(1, "INVALID")
	if err != nil {
		t.Errorf("ValidateBackupCode() error = %v", err)
	}

	if valid {
		t.Error("ValidateBackupCode() = true for invalid code")
	}
}

func TestDatabaseTwoFactorProvider_RegenerateBackupCodes(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	// Enable 2FA
	secret, _ := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	provider.Enable2FA(1, secret.Secret, secret.BackupCodes)

	// Regenerate codes
	newCodes, err := provider.GenerateBackupCodes(1, 10)
	if err != nil {
		t.Errorf("GenerateBackupCodes() error = %v", err)
	}

	if len(newCodes) != 10 {
		t.Errorf("GenerateBackupCodes() returned %d codes, want 10", len(newCodes))
	}

	// Old codes should not work
	valid, _ := provider.ValidateBackupCode(1, secret.BackupCodes[0])
	if valid {
		t.Error("Old backup code should not work after regeneration")
	}

	// New codes should work
	valid, err = provider.ValidateBackupCode(1, newCodes[0])
	if err != nil {
		t.Errorf("ValidateBackupCode() error = %v", err)
	}

	if !valid {
		t.Error("ValidateBackupCode() = false for new code")
	}
}

func TestDatabaseTwoFactorProvider_Generate2FASecret(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()

	provider := security.NewDatabaseTwoFactorProvider(db, nil)

	secret, err := provider.Generate2FASecret(1, "TestApp", "test@example.com")
	if err != nil {
		t.Fatalf("Generate2FASecret() error = %v", err)
	}

	if secret.Secret == "" {
		t.Error("Generate2FASecret() returned empty secret")
	}

	if secret.QRCodeURL == "" {
		t.Error("Generate2FASecret() returned empty QR code URL")
	}

	if len(secret.BackupCodes) != 10 {
		t.Errorf("Generate2FASecret() returned %d backup codes, want 10", len(secret.BackupCodes))
	}

	if secret.Issuer != "TestApp" {
		t.Errorf("Generate2FASecret() Issuer = %v, want TestApp", secret.Issuer)
	}

	if secret.AccountName != "test@example.com" {
		t.Errorf("Generate2FASecret() AccountName = %v, want test@example.com", secret.AccountName)
	}
}
