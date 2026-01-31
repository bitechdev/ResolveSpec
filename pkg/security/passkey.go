package security

import (
	"context"
	"encoding/json"
	"time"
)

// PasskeyCredential represents a stored WebAuthn/FIDO2 credential
type PasskeyCredential struct {
	ID              string    `json:"id"`
	UserID          int       `json:"user_id"`
	CredentialID    []byte    `json:"credential_id"`        // Raw credential ID from authenticator
	PublicKey       []byte    `json:"public_key"`           // COSE public key
	AttestationType string    `json:"attestation_type"`     // none, indirect, direct
	AAGUID          []byte    `json:"aaguid"`               // Authenticator AAGUID
	SignCount       uint32    `json:"sign_count"`           // Signature counter
	CloneWarning    bool      `json:"clone_warning"`        // True if cloning detected
	Transports      []string  `json:"transports,omitempty"` // usb, nfc, ble, internal
	BackupEligible  bool      `json:"backup_eligible"`      // Credential can be backed up
	BackupState     bool      `json:"backup_state"`         // Credential is currently backed up
	Name            string    `json:"name,omitempty"`       // User-friendly name
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      time.Time `json:"last_used_at"`
}

// PasskeyRegistrationOptions contains options for beginning passkey registration
type PasskeyRegistrationOptions struct {
	Challenge              []byte                         `json:"challenge"`
	RelyingParty           PasskeyRelyingParty            `json:"rp"`
	User                   PasskeyUser                    `json:"user"`
	PubKeyCredParams       []PasskeyCredentialParam       `json:"pubKeyCredParams"`
	Timeout                int64                          `json:"timeout,omitempty"` // Milliseconds
	ExcludeCredentials     []PasskeyCredentialDescriptor  `json:"excludeCredentials,omitempty"`
	AuthenticatorSelection *PasskeyAuthenticatorSelection `json:"authenticatorSelection,omitempty"`
	Attestation            string                         `json:"attestation,omitempty"` // none, indirect, direct, enterprise
	Extensions             map[string]any                 `json:"extensions,omitempty"`
}

// PasskeyAuthenticationOptions contains options for beginning passkey authentication
type PasskeyAuthenticationOptions struct {
	Challenge        []byte                        `json:"challenge"`
	Timeout          int64                         `json:"timeout,omitempty"`
	RelyingPartyID   string                        `json:"rpId,omitempty"`
	AllowCredentials []PasskeyCredentialDescriptor `json:"allowCredentials,omitempty"`
	UserVerification string                        `json:"userVerification,omitempty"` // required, preferred, discouraged
	Extensions       map[string]any                `json:"extensions,omitempty"`
}

// PasskeyRelyingParty identifies the relying party
type PasskeyRelyingParty struct {
	ID   string `json:"id"`   // Domain (e.g., "example.com")
	Name string `json:"name"` // Display name
}

// PasskeyUser identifies the user
type PasskeyUser struct {
	ID          []byte `json:"id"`          // User handle (unique, persistent)
	Name        string `json:"name"`        // Username
	DisplayName string `json:"displayName"` // Display name
}

// PasskeyCredentialParam specifies supported public key algorithm
type PasskeyCredentialParam struct {
	Type string `json:"type"` // "public-key"
	Alg  int    `json:"alg"`  // COSE algorithm identifier (e.g., -7 for ES256, -257 for RS256)
}

// PasskeyCredentialDescriptor describes a credential
type PasskeyCredentialDescriptor struct {
	Type       string   `json:"type"`                 // "public-key"
	ID         []byte   `json:"id"`                   // Credential ID
	Transports []string `json:"transports,omitempty"` // usb, nfc, ble, internal
}

// PasskeyAuthenticatorSelection specifies authenticator requirements
type PasskeyAuthenticatorSelection struct {
	AuthenticatorAttachment string `json:"authenticatorAttachment,omitempty"` // platform, cross-platform
	RequireResidentKey      bool   `json:"requireResidentKey,omitempty"`
	ResidentKey             string `json:"residentKey,omitempty"`      // discouraged, preferred, required
	UserVerification        string `json:"userVerification,omitempty"` // required, preferred, discouraged
}

// PasskeyRegistrationResponse contains the client's registration response
type PasskeyRegistrationResponse struct {
	ID                     string                                  `json:"id"`    // Base64URL encoded credential ID
	RawID                  []byte                                  `json:"rawId"` // Raw credential ID
	Type                   string                                  `json:"type"`  // "public-key"
	Response               PasskeyAuthenticatorAttestationResponse `json:"response"`
	ClientExtensionResults map[string]any                          `json:"clientExtensionResults,omitempty"`
	Transports             []string                                `json:"transports,omitempty"`
}

// PasskeyAuthenticatorAttestationResponse contains attestation data
type PasskeyAuthenticatorAttestationResponse struct {
	ClientDataJSON    []byte   `json:"clientDataJSON"`
	AttestationObject []byte   `json:"attestationObject"`
	Transports        []string `json:"transports,omitempty"`
}

// PasskeyAuthenticationResponse contains the client's authentication response
type PasskeyAuthenticationResponse struct {
	ID                     string                                `json:"id"`    // Base64URL encoded credential ID
	RawID                  []byte                                `json:"rawId"` // Raw credential ID
	Type                   string                                `json:"type"`  // "public-key"
	Response               PasskeyAuthenticatorAssertionResponse `json:"response"`
	ClientExtensionResults map[string]any                        `json:"clientExtensionResults,omitempty"`
}

// PasskeyAuthenticatorAssertionResponse contains assertion data
type PasskeyAuthenticatorAssertionResponse struct {
	ClientDataJSON    []byte `json:"clientDataJSON"`
	AuthenticatorData []byte `json:"authenticatorData"`
	Signature         []byte `json:"signature"`
	UserHandle        []byte `json:"userHandle,omitempty"`
}

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
