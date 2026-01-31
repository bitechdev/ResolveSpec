package security_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

// MockAuthenticator is a simple authenticator for testing 2FA
type MockAuthenticator struct {
	users map[string]*security.UserContext
}

func NewMockAuthenticator() *MockAuthenticator {
	return &MockAuthenticator{
		users: map[string]*security.UserContext{
			"testuser": {
				UserID:   1,
				UserName: "testuser",
				Email:    "test@example.com",
			},
		},
	}
}

func (m *MockAuthenticator) Login(ctx context.Context, req security.LoginRequest) (*security.LoginResponse, error) {
	user, exists := m.users[req.Username]
	if !exists || req.Password != "password" {
		return nil, ErrInvalidCredentials
	}

	return &security.LoginResponse{
		Token:        "mock-token",
		RefreshToken: "mock-refresh-token",
		User:         user,
		ExpiresIn:    3600,
	}, nil
}

func (m *MockAuthenticator) Logout(ctx context.Context, req security.LogoutRequest) error {
	return nil
}

func (m *MockAuthenticator) Authenticate(r *http.Request) (*security.UserContext, error) {
	return m.users["testuser"], nil
}

func TestTwoFactorAuthenticator_Setup(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup 2FA
	secret, err := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	if err != nil {
		t.Fatalf("Setup2FA() error = %v", err)
	}

	if secret.Secret == "" {
		t.Error("Setup2FA() returned empty secret")
	}

	if secret.QRCodeURL == "" {
		t.Error("Setup2FA() returned empty QR code URL")
	}

	if len(secret.BackupCodes) == 0 {
		t.Error("Setup2FA() returned no backup codes")
	}

	if secret.Issuer != "TestApp" {
		t.Errorf("Setup2FA() Issuer = %s, want TestApp", secret.Issuer)
	}

	if secret.AccountName != "test@example.com" {
		t.Errorf("Setup2FA() AccountName = %s, want test@example.com", secret.AccountName)
	}
}

func TestTwoFactorAuthenticator_Enable2FA(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup 2FA
	secret, err := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	if err != nil {
		t.Fatalf("Setup2FA() error = %v", err)
	}

	// Generate valid code
	totp := security.NewTOTPGenerator(nil)
	code, err := totp.GenerateCode(secret.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}

	// Enable 2FA with valid code
	err = tfaAuth.Enable2FA(1, secret.Secret, code)
	if err != nil {
		t.Errorf("Enable2FA() error = %v", err)
	}

	// Verify 2FA is enabled
	status, err := provider.Get2FAStatus(1)
	if err != nil {
		t.Fatalf("Get2FAStatus() error = %v", err)
	}

	if !status {
		t.Error("Enable2FA() did not enable 2FA")
	}
}

func TestTwoFactorAuthenticator_Enable2FA_InvalidCode(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup 2FA
	secret, err := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	if err != nil {
		t.Fatalf("Setup2FA() error = %v", err)
	}

	// Try to enable with invalid code
	err = tfaAuth.Enable2FA(1, secret.Secret, "000000")
	if err == nil {
		t.Error("Enable2FA() should fail with invalid code")
	}

	// Verify 2FA is not enabled
	status, _ := provider.Get2FAStatus(1)
	if status {
		t.Error("Enable2FA() should not enable 2FA with invalid code")
	}
}

func TestTwoFactorAuthenticator_Login_Without2FA(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	req := security.LoginRequest{
		Username: "testuser",
		Password: "password",
	}

	resp, err := tfaAuth.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if resp.Requires2FA {
		t.Error("Login() should not require 2FA when not enabled")
	}

	if resp.Token == "" {
		t.Error("Login() should return token when 2FA not required")
	}
}

func TestTwoFactorAuthenticator_Login_With2FA_NoCode(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Try to login without 2FA code
	req := security.LoginRequest{
		Username: "testuser",
		Password: "password",
	}

	resp, err := tfaAuth.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if !resp.Requires2FA {
		t.Error("Login() should require 2FA when enabled")
	}

	if resp.Token != "" {
		t.Error("Login() should not return token when 2FA required but not provided")
	}
}

func TestTwoFactorAuthenticator_Login_With2FA_ValidCode(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Generate new valid code for login
	newCode, _ := totp.GenerateCode(secret.Secret, time.Now())

	// Login with 2FA code
	req := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: newCode,
	}

	resp, err := tfaAuth.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if resp.Requires2FA {
		t.Error("Login() should not require 2FA when valid code provided")
	}

	if resp.Token == "" {
		t.Error("Login() should return token when 2FA validated")
	}

	if !resp.User.TwoFactorEnabled {
		t.Error("Login() should set TwoFactorEnabled on user")
	}
}

func TestTwoFactorAuthenticator_Login_With2FA_InvalidCode(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Try to login with invalid code
	req := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: "000000",
	}

	_, err := tfaAuth.Login(context.Background(), req)
	if err == nil {
		t.Error("Login() should fail with invalid 2FA code")
	}
}

func TestTwoFactorAuthenticator_Login_WithBackupCode(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Get backup codes
	backupCodes, _ := tfaAuth.RegenerateBackupCodes(1, 10)

	// Login with backup code
	req := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: backupCodes[0],
	}

	resp, err := tfaAuth.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login() with backup code error = %v", err)
	}

	if resp.Token == "" {
		t.Error("Login() should return token when backup code validated")
	}

	// Try to use same backup code again
	req2 := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: backupCodes[0],
	}

	_, err = tfaAuth.Login(context.Background(), req2)
	if err == nil {
		t.Error("Login() should fail when reusing backup code")
	}
}

func TestTwoFactorAuthenticator_Disable2FA(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Disable 2FA
	err := tfaAuth.Disable2FA(1)
	if err != nil {
		t.Errorf("Disable2FA() error = %v", err)
	}

	// Verify 2FA is disabled
	status, _ := provider.Get2FAStatus(1)
	if status {
		t.Error("Disable2FA() did not disable 2FA")
	}

	// Login should not require 2FA
	req := security.LoginRequest{
		Username: "testuser",
		Password: "password",
	}

	resp, err := tfaAuth.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if resp.Requires2FA {
		t.Error("Login() should not require 2FA after disabling")
	}
}

func TestTwoFactorAuthenticator_RegenerateBackupCodes(t *testing.T) {
	baseAuth := NewMockAuthenticator()
	provider := security.NewMemoryTwoFactorProvider(nil)
	tfaAuth := security.NewTwoFactorAuthenticator(baseAuth, provider, nil)

	// Setup and enable 2FA
	secret, _ := tfaAuth.Setup2FA(1, "TestApp", "test@example.com")
	totp := security.NewTOTPGenerator(nil)
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	tfaAuth.Enable2FA(1, secret.Secret, code)

	// Get initial backup codes
	codes1, err := tfaAuth.RegenerateBackupCodes(1, 10)
	if err != nil {
		t.Fatalf("RegenerateBackupCodes() error = %v", err)
	}

	if len(codes1) != 10 {
		t.Errorf("RegenerateBackupCodes() returned %d codes, want 10", len(codes1))
	}

	// Regenerate backup codes
	codes2, err := tfaAuth.RegenerateBackupCodes(1, 10)
	if err != nil {
		t.Fatalf("RegenerateBackupCodes() error = %v", err)
	}

	// Old codes should not work
	req := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: codes1[0],
	}

	_, err = tfaAuth.Login(context.Background(), req)
	if err == nil {
		t.Error("Login() should fail with old backup code after regeneration")
	}

	// New codes should work
	req2 := security.LoginRequest{
		Username:      "testuser",
		Password:      "password",
		TwoFactorCode: codes2[0],
	}

	resp, err := tfaAuth.Login(context.Background(), req2)
	if err != nil {
		t.Fatalf("Login() with new backup code error = %v", err)
	}

	if resp.Token == "" {
		t.Error("Login() should return token with new backup code")
	}
}
