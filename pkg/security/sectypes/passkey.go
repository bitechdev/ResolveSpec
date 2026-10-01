package sectypes

import "time"

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
