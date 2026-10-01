package totp

import (
	"context"
	"fmt"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
	"net/http"
)

// BaseAuthenticator is the subset of security.Authenticator that Authenticator wraps.
// It is declared here so totp does not import the core security package.
type BaseAuthenticator interface {
	Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error)
	Logout(ctx context.Context, req sectypes.LogoutRequest) error
	Authenticate(r *http.Request) (*sectypes.UserContext, error)
}

// Authenticator wraps an Authenticator and adds 2FA support
type Authenticator struct {
	baseAuth BaseAuthenticator
	totp     *Generator
	provider AuthProvider
}

// NewAuthenticator creates a new 2FA-enabled authenticator
func NewAuthenticator(baseAuth BaseAuthenticator, provider AuthProvider, config *Config) *Authenticator {
	if config == nil {
		config = DefaultConfig()
	}
	return &Authenticator{
		baseAuth: baseAuth,
		totp:     NewGenerator(config),
		provider: provider,
	}
}

// Login authenticates with 2FA support
func (t *Authenticator) Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	// First, perform standard authentication
	resp, err := t.baseAuth.Login(ctx, req)
	if err != nil {
		return nil, err
	}

	// Check if user has 2FA enabled
	if resp.User == nil {
		return resp, nil
	}

	has2FA, err := t.provider.Get2FAStatus(resp.User.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to check 2FA status: %w", err)
	}

	if !has2FA {
		// User doesn't have 2FA enabled, return normal response
		return resp, nil
	}

	// User has 2FA enabled
	if req.TwoFactorCode == "" {
		// No 2FA code provided, require it
		resp.Requires2FA = true
		resp.Token = "" // Don't return token until 2FA is verified
		resp.RefreshToken = ""
		return resp, nil
	}

	// Validate 2FA code
	secret, err := t.provider.Get2FASecret(resp.User.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get 2FA secret: %w", err)
	}

	// Try TOTP code first
	valid, err := t.totp.ValidateCode(secret, req.TwoFactorCode)
	if err != nil {
		return nil, fmt.Errorf("failed to validate 2FA code: %w", err)
	}

	if !valid {
		// Try backup code
		valid, err = t.provider.ValidateBackupCode(resp.User.UserID, req.TwoFactorCode)
		if err != nil {
			return nil, fmt.Errorf("failed to validate backup code: %w", err)
		}
	}

	if !valid {
		return nil, fmt.Errorf("invalid 2FA code")
	}

	// 2FA verified, return full response with token
	resp.User.TwoFactorEnabled = true
	return resp, nil
}

// Logout delegates to base authenticator
func (t *Authenticator) Logout(ctx context.Context, req sectypes.LogoutRequest) error {
	return t.baseAuth.Logout(ctx, req)
}

// Authenticate delegates to base authenticator
func (t *Authenticator) Authenticate(r *http.Request) (*sectypes.UserContext, error) {
	return t.baseAuth.Authenticate(r)
}

// Setup2FA initiates 2FA setup for a user
func (t *Authenticator) Setup2FA(userID int, issuer, accountName string) (*sectypes.TwoFactorSecret, error) {
	return t.provider.Generate2FASecret(userID, issuer, accountName)
}

// Enable2FA completes 2FA setup after user confirms with a valid code
func (t *Authenticator) Enable2FA(userID int, secret, verificationCode string) error {
	// Verify the code before enabling
	valid, err := t.totp.ValidateCode(secret, verificationCode)
	if err != nil {
		return fmt.Errorf("failed to validate code: %w", err)
	}

	if !valid {
		return fmt.Errorf("invalid verification code")
	}

	// Generate backup codes
	backupCodes, err := t.provider.GenerateBackupCodes(userID, 10)
	if err != nil {
		return fmt.Errorf("failed to generate backup codes: %w", err)
	}

	// Enable 2FA
	return t.provider.Enable2FA(userID, secret, backupCodes)
}

// Disable2FA removes 2FA from a user account
func (t *Authenticator) Disable2FA(userID int) error {
	return t.provider.Disable2FA(userID)
}

// RegenerateBackupCodes creates new backup codes for a user
func (t *Authenticator) RegenerateBackupCodes(userID int, count int) ([]string, error) {
	return t.provider.GenerateBackupCodes(userID, count)
}
