package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// MemoryTwoFactorProvider is an in-memory implementation of TwoFactorAuthProvider for testing/examples
type MemoryTwoFactorProvider struct {
	mu          sync.RWMutex
	secrets     map[int]string          // userID -> secret
	backupCodes map[int]map[string]bool // userID -> backup codes (code -> used)
	totpGen     *TOTPGenerator
}

// NewMemoryTwoFactorProvider creates a new in-memory 2FA provider
func NewMemoryTwoFactorProvider(config *TwoFactorConfig) *MemoryTwoFactorProvider {
	if config == nil {
		config = DefaultTwoFactorConfig()
	}
	return &MemoryTwoFactorProvider{
		secrets:     make(map[int]string),
		backupCodes: make(map[int]map[string]bool),
		totpGen:     NewTOTPGenerator(config),
	}
}

// Generate2FASecret creates a new secret for a user
func (m *MemoryTwoFactorProvider) Generate2FASecret(userID int, issuer, accountName string) (*TwoFactorSecret, error) {
	secret, err := m.totpGen.GenerateSecret()
	if err != nil {
		return nil, err
	}

	qrURL := m.totpGen.GenerateQRCodeURL(secret, issuer, accountName)

	backupCodes, err := GenerateBackupCodes(10)
	if err != nil {
		return nil, err
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
func (m *MemoryTwoFactorProvider) Validate2FACode(secret string, code string) (bool, error) {
	return m.totpGen.ValidateCode(secret, code)
}

// Enable2FA activates 2FA for a user
func (m *MemoryTwoFactorProvider) Enable2FA(userID int, secret string, backupCodes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.secrets[userID] = secret

	// Store backup codes
	if m.backupCodes[userID] == nil {
		m.backupCodes[userID] = make(map[string]bool)
	}

	for _, code := range backupCodes {
		// Hash backup codes for security
		hash := sha256.Sum256([]byte(code))
		m.backupCodes[userID][hex.EncodeToString(hash[:])] = false
	}

	return nil
}

// Disable2FA deactivates 2FA for a user
func (m *MemoryTwoFactorProvider) Disable2FA(userID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.secrets, userID)
	delete(m.backupCodes, userID)
	return nil
}

// Get2FAStatus checks if user has 2FA enabled
func (m *MemoryTwoFactorProvider) Get2FAStatus(userID int) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.secrets[userID]
	return exists, nil
}

// Get2FASecret retrieves the user's 2FA secret
func (m *MemoryTwoFactorProvider) Get2FASecret(userID int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	secret, exists := m.secrets[userID]
	if !exists {
		return "", fmt.Errorf("user does not have 2FA enabled")
	}
	return secret, nil
}

// GenerateBackupCodes creates backup codes for 2FA
func (m *MemoryTwoFactorProvider) GenerateBackupCodes(userID int, count int) ([]string, error) {
	codes, err := GenerateBackupCodes(count)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear old backup codes and store new ones
	m.backupCodes[userID] = make(map[string]bool)
	for _, code := range codes {
		hash := sha256.Sum256([]byte(code))
		m.backupCodes[userID][hex.EncodeToString(hash[:])] = false
	}

	return codes, nil
}

// ValidateBackupCode checks and consumes a backup code
func (m *MemoryTwoFactorProvider) ValidateBackupCode(userID int, code string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userCodes, exists := m.backupCodes[userID]
	if !exists {
		return false, nil
	}

	// Hash the provided code
	hash := sha256.Sum256([]byte(code))
	hashStr := hex.EncodeToString(hash[:])

	used, exists := userCodes[hashStr]
	if !exists {
		return false, nil
	}

	if used {
		return false, fmt.Errorf("backup code already used")
	}

	// Mark as used
	userCodes[hashStr] = true
	return true, nil
}
