package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"net/url"
	"strings"
	"time"
)

// TwoFactorAuthProvider defines interface for 2FA operations
type TwoFactorAuthProvider interface {
	// Generate2FASecret creates a new secret for a user
	Generate2FASecret(userID int, issuer, accountName string) (*TwoFactorSecret, error)

	// Validate2FACode verifies a TOTP code
	Validate2FACode(secret string, code string) (bool, error)

	// Enable2FA activates 2FA for a user (store secret in your database)
	Enable2FA(userID int, secret string, backupCodes []string) error

	// Disable2FA deactivates 2FA for a user
	Disable2FA(userID int) error

	// Get2FAStatus checks if user has 2FA enabled
	Get2FAStatus(userID int) (bool, error)

	// Get2FASecret retrieves the user's 2FA secret
	Get2FASecret(userID int) (string, error)

	// GenerateBackupCodes creates backup codes for 2FA
	GenerateBackupCodes(userID int, count int) ([]string, error)

	// ValidateBackupCode checks and consumes a backup code
	ValidateBackupCode(userID int, code string) (bool, error)
}

// TwoFactorSecret contains 2FA setup information
type TwoFactorSecret struct {
	Secret      string   `json:"secret"`       // Base32 encoded secret
	QRCodeURL   string   `json:"qr_code_url"`  // URL for QR code generation
	BackupCodes []string `json:"backup_codes"` // One-time backup codes
	Issuer      string   `json:"issuer"`       // Application name
	AccountName string   `json:"account_name"` // User identifier (email/username)
}

// TwoFactorConfig holds TOTP configuration
type TwoFactorConfig struct {
	Algorithm  string // SHA1, SHA256, SHA512
	Digits     int    // Number of digits in code (6 or 8)
	Period     int    // Time step in seconds (default 30)
	SkewWindow int    // Number of time steps to check before/after (default 1)
}

// DefaultTwoFactorConfig returns standard TOTP configuration
func DefaultTwoFactorConfig() *TwoFactorConfig {
	return &TwoFactorConfig{
		Algorithm:  "SHA1",
		Digits:     6,
		Period:     30,
		SkewWindow: 1,
	}
}

// TOTPGenerator handles TOTP code generation and validation
type TOTPGenerator struct {
	config *TwoFactorConfig
}

// NewTOTPGenerator creates a new TOTP generator with config
func NewTOTPGenerator(config *TwoFactorConfig) *TOTPGenerator {
	if config == nil {
		config = DefaultTwoFactorConfig()
	}
	return &TOTPGenerator{
		config: config,
	}
}

// GenerateSecret creates a random base32-encoded secret
func (t *TOTPGenerator) GenerateSecret() (string, error) {
	secret := make([]byte, 20)
	_, err := rand.Read(secret)
	if err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

// GenerateQRCodeURL creates a URL for QR code generation
func (t *TOTPGenerator) GenerateQRCodeURL(secret, issuer, accountName string) string {
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", t.config.Algorithm)
	params.Set("digits", fmt.Sprintf("%d", t.config.Digits))
	params.Set("period", fmt.Sprintf("%d", t.config.Period))

	label := url.PathEscape(fmt.Sprintf("%s:%s", issuer, accountName))
	return fmt.Sprintf("otpauth://totp/%s?%s", label, params.Encode())
}

// GenerateCode creates a TOTP code for a given time
func (t *TOTPGenerator) GenerateCode(secret string, timestamp time.Time) (string, error) {
	// Decode secret
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("invalid secret: %w", err)
	}

	// Calculate counter (time steps since Unix epoch)
	counter := uint64(timestamp.Unix()) / uint64(t.config.Period)

	// Generate HMAC
	h := t.getHashFunc()
	mac := hmac.New(h, key)

	// Convert counter to 8-byte array
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac.Write(buf)

	sum := mac.Sum(nil)

	// Dynamic truncation
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:]) & 0x7fffffff

	// Generate code with specified digits
	code := truncated % uint32(math.Pow10(t.config.Digits))

	format := fmt.Sprintf("%%0%dd", t.config.Digits)
	return fmt.Sprintf(format, code), nil
}

// ValidateCode checks if a code is valid for the secret
func (t *TOTPGenerator) ValidateCode(secret, code string) (bool, error) {
	now := time.Now()

	// Check current time and skew window
	for i := -t.config.SkewWindow; i <= t.config.SkewWindow; i++ {
		timestamp := now.Add(time.Duration(i*t.config.Period) * time.Second)
		expected, err := t.GenerateCode(secret, timestamp)
		if err != nil {
			return false, err
		}

		if code == expected {
			return true, nil
		}
	}

	return false, nil
}

// getHashFunc returns the hash function based on algorithm
func (t *TOTPGenerator) getHashFunc() func() hash.Hash {
	switch strings.ToUpper(t.config.Algorithm) {
	case "SHA256":
		return sha256.New
	case "SHA512":
		return sha512.New
	default:
		return sha1.New
	}
}

// GenerateBackupCodes creates random backup codes
func GenerateBackupCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		code := make([]byte, 4)
		_, err := rand.Read(code)
		if err != nil {
			return nil, fmt.Errorf("failed to generate backup code: %w", err)
		}
		codes[i] = fmt.Sprintf("%08X", binary.BigEndian.Uint32(code))
	}
	return codes, nil
}
