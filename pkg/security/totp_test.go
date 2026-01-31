package security

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPGenerator_GenerateSecret(t *testing.T) {
	totp := NewTOTPGenerator(nil)

	secret, err := totp.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret() error = %v", err)
	}

	if secret == "" {
		t.Error("GenerateSecret() returned empty secret")
	}

	// Secret should be base32 encoded
	if len(secret) < 16 {
		t.Error("GenerateSecret() returned secret that is too short")
	}
}

func TestTOTPGenerator_GenerateQRCodeURL(t *testing.T) {
	totp := NewTOTPGenerator(nil)

	secret := "JBSWY3DPEHPK3PXP"
	issuer := "TestApp"
	accountName := "user@example.com"

	url := totp.GenerateQRCodeURL(secret, issuer, accountName)

	if !strings.HasPrefix(url, "otpauth://totp/") {
		t.Errorf("GenerateQRCodeURL() = %v, want otpauth://totp/ prefix", url)
	}

	if !strings.Contains(url, "secret="+secret) {
		t.Errorf("GenerateQRCodeURL() missing secret parameter")
	}

	if !strings.Contains(url, "issuer="+issuer) {
		t.Errorf("GenerateQRCodeURL() missing issuer parameter")
	}
}

func TestTOTPGenerator_GenerateCode(t *testing.T) {
	config := &TwoFactorConfig{
		Algorithm:  "SHA1",
		Digits:     6,
		Period:     30,
		SkewWindow: 1,
	}
	totp := NewTOTPGenerator(config)

	secret := "JBSWY3DPEHPK3PXP"

	// Test with known time
	timestamp := time.Unix(1234567890, 0)
	code, err := totp.GenerateCode(secret, timestamp)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	if len(code) != 6 {
		t.Errorf("GenerateCode() returned code with length %d, want 6", len(code))
	}

	// Code should be numeric
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("GenerateCode() returned non-numeric code: %s", code)
			break
		}
	}
}

func TestTOTPGenerator_ValidateCode(t *testing.T) {
	config := &TwoFactorConfig{
		Algorithm:  "SHA1",
		Digits:     6,
		Period:     30,
		SkewWindow: 1,
	}
	totp := NewTOTPGenerator(config)

	secret := "JBSWY3DPEHPK3PXP"

	// Generate a code for current time
	now := time.Now()
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	// Validate the code
	valid, err := totp.ValidateCode(secret, code)
	if err != nil {
		t.Fatalf("ValidateCode() error = %v", err)
	}

	if !valid {
		t.Error("ValidateCode() = false, want true for current code")
	}

	// Test with invalid code
	valid, err = totp.ValidateCode(secret, "000000")
	if err != nil {
		t.Fatalf("ValidateCode() error = %v", err)
	}

	// This might occasionally pass if 000000 is the correct code, but very unlikely
	if valid && code != "000000" {
		t.Error("ValidateCode() = true for invalid code")
	}
}

func TestTOTPGenerator_ValidateCode_WithSkew(t *testing.T) {
	config := &TwoFactorConfig{
		Algorithm:  "SHA1",
		Digits:     6,
		Period:     30,
		SkewWindow: 2, // Allow 2 periods before/after
	}
	totp := NewTOTPGenerator(config)

	secret := "JBSWY3DPEHPK3PXP"

	// Generate code for 1 period ago
	past := time.Now().Add(-30 * time.Second)
	code, err := totp.GenerateCode(secret, past)
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	// Should still validate with skew window
	valid, err := totp.ValidateCode(secret, code)
	if err != nil {
		t.Fatalf("ValidateCode() error = %v", err)
	}

	if !valid {
		t.Error("ValidateCode() = false, want true for code within skew window")
	}
}

func TestTOTPGenerator_DifferentAlgorithms(t *testing.T) {
	algorithms := []string{"SHA1", "SHA256", "SHA512"}
	secret := "JBSWY3DPEHPK3PXP"

	for _, algo := range algorithms {
		t.Run(algo, func(t *testing.T) {
			config := &TwoFactorConfig{
				Algorithm:  algo,
				Digits:     6,
				Period:     30,
				SkewWindow: 1,
			}
			totp := NewTOTPGenerator(config)

			code, err := totp.GenerateCode(secret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode() with %s error = %v", algo, err)
			}

			valid, err := totp.ValidateCode(secret, code)
			if err != nil {
				t.Fatalf("ValidateCode() with %s error = %v", algo, err)
			}

			if !valid {
				t.Errorf("ValidateCode() with %s = false, want true", algo)
			}
		})
	}
}

func TestTOTPGenerator_8Digits(t *testing.T) {
	config := &TwoFactorConfig{
		Algorithm:  "SHA1",
		Digits:     8,
		Period:     30,
		SkewWindow: 1,
	}
	totp := NewTOTPGenerator(config)

	secret := "JBSWY3DPEHPK3PXP"

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	if len(code) != 8 {
		t.Errorf("GenerateCode() returned code with length %d, want 8", len(code))
	}

	valid, err := totp.ValidateCode(secret, code)
	if err != nil {
		t.Fatalf("ValidateCode() error = %v", err)
	}

	if !valid {
		t.Error("ValidateCode() = false, want true for 8-digit code")
	}
}

func TestGenerateBackupCodes(t *testing.T) {
	count := 10
	codes, err := GenerateBackupCodes(count)
	if err != nil {
		t.Fatalf("GenerateBackupCodes() error = %v", err)
	}

	if len(codes) != count {
		t.Errorf("GenerateBackupCodes() returned %d codes, want %d", len(codes), count)
	}

	// Check uniqueness
	seen := make(map[string]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("GenerateBackupCodes() generated duplicate code: %s", code)
		}
		seen[code] = true

		// Check format (8 hex characters)
		if len(code) != 8 {
			t.Errorf("GenerateBackupCodes() code length = %d, want 8", len(code))
		}
	}
}

func TestDefaultTwoFactorConfig(t *testing.T) {
	config := DefaultTwoFactorConfig()

	if config.Algorithm != "SHA1" {
		t.Errorf("DefaultTwoFactorConfig() Algorithm = %s, want SHA1", config.Algorithm)
	}

	if config.Digits != 6 {
		t.Errorf("DefaultTwoFactorConfig() Digits = %d, want 6", config.Digits)
	}

	if config.Period != 30 {
		t.Errorf("DefaultTwoFactorConfig() Period = %d, want 30", config.Period)
	}

	if config.SkewWindow != 1 {
		t.Errorf("DefaultTwoFactorConfig() SkewWindow = %d, want 1", config.SkewWindow)
	}
}

func TestTOTPGenerator_InvalidSecret(t *testing.T) {
	totp := NewTOTPGenerator(nil)

	// Test with invalid base32 secret
	_, err := totp.GenerateCode("INVALID!!!", time.Now())
	if err == nil {
		t.Error("GenerateCode() with invalid secret should return error")
	}

	_, err = totp.ValidateCode("INVALID!!!", "123456")
	if err == nil {
		t.Error("ValidateCode() with invalid secret should return error")
	}
}

// Benchmark tests
func BenchmarkTOTPGenerator_GenerateCode(b *testing.B) {
	totp := NewTOTPGenerator(nil)
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = totp.GenerateCode(secret, now)
	}
}

func BenchmarkTOTPGenerator_ValidateCode(b *testing.B) {
	totp := NewTOTPGenerator(nil)
	secret := "JBSWY3DPEHPK3PXP"
	code, _ := totp.GenerateCode(secret, time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = totp.ValidateCode(secret, code)
	}
}
