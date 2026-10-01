package security

import (
	"context"
	"encoding/json"
)

// PasskeyProvider handles passkey registration and authentication
type PasskeyProvider interface {
	// BeginRegistration creates registration options for a new passkey
	BeginRegistration(ctx context.Context, userID int, username, displayName string) (*PasskeyRegistrationOptions, error)

	// CompleteRegistration verifies and stores a new passkey credential
	CompleteRegistration(ctx context.Context, userID int, response PasskeyRegistrationResponse, expectedChallenge []byte) (*PasskeyCredential, error)

	// BeginAuthentication creates authentication options for passkey login
	BeginAuthentication(ctx context.Context, username string) (*PasskeyAuthenticationOptions, error)

	// CompleteAuthentication verifies a passkey assertion and returns the user
	CompleteAuthentication(ctx context.Context, response PasskeyAuthenticationResponse, expectedChallenge []byte) (int, error)

	// GetCredentials returns all passkey credentials for a user
	GetCredentials(ctx context.Context, userID int) ([]PasskeyCredential, error)

	// DeleteCredential removes a passkey credential
	DeleteCredential(ctx context.Context, userID int, credentialID string) error

	// UpdateCredentialName updates the friendly name of a credential
	UpdateCredentialName(ctx context.Context, userID int, credentialID string, name string) error
}

// PasskeyLoginRequest contains passkey authentication data
type PasskeyLoginRequest struct {
	Response          PasskeyAuthenticationResponse `json:"response"`
	ExpectedChallenge []byte                        `json:"expected_challenge"`
	Claims            map[string]any                `json:"claims"` // Additional login data
}

// PasskeyRegisterRequest contains passkey registration data
type PasskeyRegisterRequest struct {
	UserID            int                         `json:"user_id"`
	Response          PasskeyRegistrationResponse `json:"response"`
	ExpectedChallenge []byte                      `json:"expected_challenge"`
	CredentialName    string                      `json:"credential_name,omitempty"`
}

// PasskeyBeginRegistrationRequest contains options for starting passkey registration
type PasskeyBeginRegistrationRequest struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// PasskeyBeginAuthenticationRequest contains options for starting passkey authentication
type PasskeyBeginAuthenticationRequest struct {
	Username string `json:"username,omitempty"` // Optional for resident key flow
}

// ParsePasskeyRegistrationResponse parses a JSON passkey registration response
func ParsePasskeyRegistrationResponse(data []byte) (*PasskeyRegistrationResponse, error) {
	var response PasskeyRegistrationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// ParsePasskeyAuthenticationResponse parses a JSON passkey authentication response
func ParsePasskeyAuthenticationResponse(data []byte) (*PasskeyAuthenticationResponse, error) {
	var response PasskeyAuthenticationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
