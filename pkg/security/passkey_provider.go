package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// DatabasePasskeyProvider implements PasskeyProvider using database storage
type DatabasePasskeyProvider struct {
	db       *sql.DB
	rpID     string // Relying Party ID (domain)
	rpName   string // Relying Party display name
	rpOrigin string // Expected origin for WebAuthn
	timeout  int64  // Timeout in milliseconds (default: 60000)
}

// DatabasePasskeyProviderOptions configures the passkey provider
type DatabasePasskeyProviderOptions struct {
	// RPID is the Relying Party ID (typically your domain, e.g., "example.com")
	RPID string
	// RPName is the display name for your relying party
	RPName string
	// RPOrigin is the expected origin (e.g., "https://example.com")
	RPOrigin string
	// Timeout is the timeout for operations in milliseconds (default: 60000)
	Timeout int64
}

// NewDatabasePasskeyProvider creates a new database-backed passkey provider
func NewDatabasePasskeyProvider(db *sql.DB, opts DatabasePasskeyProviderOptions) *DatabasePasskeyProvider {
	if opts.Timeout == 0 {
		opts.Timeout = 60000 // 60 seconds default
	}

	return &DatabasePasskeyProvider{
		db:       db,
		rpID:     opts.RPID,
		rpName:   opts.RPName,
		rpOrigin: opts.RPOrigin,
		timeout:  opts.Timeout,
	}
}

// BeginRegistration creates registration options for a new passkey
func (p *DatabasePasskeyProvider) BeginRegistration(ctx context.Context, userID int, username, displayName string) (*PasskeyRegistrationOptions, error) {
	// Generate challenge
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Get existing credentials to exclude
	credentials, err := p.GetCredentials(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing credentials: %w", err)
	}

	excludeCredentials := make([]PasskeyCredentialDescriptor, 0, len(credentials))
	for i := range credentials {
		excludeCredentials = append(excludeCredentials, PasskeyCredentialDescriptor{
			Type:       "public-key",
			ID:         credentials[i].CredentialID,
			Transports: credentials[i].Transports,
		})
	}

	// Create user handle (persistent user ID)
	userHandle := []byte(fmt.Sprintf("user_%d", userID))

	return &PasskeyRegistrationOptions{
		Challenge: challenge,
		RelyingParty: PasskeyRelyingParty{
			ID:   p.rpID,
			Name: p.rpName,
		},
		User: PasskeyUser{
			ID:          userHandle,
			Name:        username,
			DisplayName: displayName,
		},
		PubKeyCredParams: []PasskeyCredentialParam{
			{Type: "public-key", Alg: -7},   // ES256 (ECDSA with SHA-256)
			{Type: "public-key", Alg: -257}, // RS256 (RSASSA-PKCS1-v1_5 with SHA-256)
		},
		Timeout:            p.timeout,
		ExcludeCredentials: excludeCredentials,
		AuthenticatorSelection: &PasskeyAuthenticatorSelection{
			RequireResidentKey: false,
			ResidentKey:        "preferred",
			UserVerification:   "preferred",
		},
		Attestation: "none",
	}, nil
}

// CompleteRegistration verifies and stores a new passkey credential
// NOTE: This is a simplified implementation. In production, you should use a WebAuthn library
// like github.com/go-webauthn/webauthn to properly verify attestation and parse credentials.
func (p *DatabasePasskeyProvider) CompleteRegistration(ctx context.Context, userID int, response PasskeyRegistrationResponse, expectedChallenge []byte) (*PasskeyCredential, error) {
	// TODO: Implement full WebAuthn verification
	// 1. Verify clientDataJSON contains correct challenge and origin
	// 2. Parse and verify attestationObject
	// 3. Extract public key and credential ID
	// 4. Verify attestation signature (if not "none")

	// For now, this is a placeholder that stores the credential data
	// In production, you MUST use a proper WebAuthn library

	credData := map[string]any{
		"user_id":          userID,
		"credential_id":    base64.StdEncoding.EncodeToString(response.RawID),
		"public_key":       base64.StdEncoding.EncodeToString(response.Response.AttestationObject),
		"attestation_type": "none",
		"sign_count":       0,
		"transports":       response.Transports,
		"backup_eligible":  false,
		"backup_state":     false,
		"name":             "Passkey",
	}

	credJSON, err := json.Marshal(credData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal credential data: %w", err)
	}

	var success bool
	var errorMsg sql.NullString
	var credentialID sql.NullInt64

	query := `SELECT p_success, p_error, p_credential_id FROM resolvespec_passkey_store_credential($1::jsonb)`
	err = p.db.QueryRowContext(ctx, query, string(credJSON)).Scan(&success, &errorMsg, &credentialID)
	if err != nil {
		return nil, fmt.Errorf("failed to store credential: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("failed to store credential")
	}

	return &PasskeyCredential{
		ID:              fmt.Sprintf("%d", credentialID.Int64),
		UserID:          userID,
		CredentialID:    response.RawID,
		PublicKey:       response.Response.AttestationObject,
		AttestationType: "none",
		Transports:      response.Transports,
		CreatedAt:       time.Now(),
		LastUsedAt:      time.Now(),
	}, nil
}

// BeginAuthentication creates authentication options for passkey login
func (p *DatabasePasskeyProvider) BeginAuthentication(ctx context.Context, username string) (*PasskeyAuthenticationOptions, error) {
	// Generate challenge
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// If username is provided, get user's credentials
	var allowCredentials []PasskeyCredentialDescriptor
	if username != "" {
		var success bool
		var errorMsg sql.NullString
		var userID sql.NullInt64
		var credentialsJSON sql.NullString

		query := `SELECT p_success, p_error, p_user_id, p_credentials::text FROM resolvespec_passkey_get_credentials_by_username($1)`
		err := p.db.QueryRowContext(ctx, query, username).Scan(&success, &errorMsg, &userID, &credentialsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to get credentials: %w", err)
		}

		if !success {
			if errorMsg.Valid {
				return nil, fmt.Errorf("%s", errorMsg.String)
			}
			return nil, fmt.Errorf("failed to get credentials")
		}

		// Parse credentials
		var creds []struct {
			ID         string   `json:"credential_id"`
			Transports []string `json:"transports"`
		}
		if err := json.Unmarshal([]byte(credentialsJSON.String), &creds); err != nil {
			return nil, fmt.Errorf("failed to parse credentials: %w", err)
		}

		allowCredentials = make([]PasskeyCredentialDescriptor, 0, len(creds))
		for _, cred := range creds {
			credID, err := base64.StdEncoding.DecodeString(cred.ID)
			if err != nil {
				continue
			}
			allowCredentials = append(allowCredentials, PasskeyCredentialDescriptor{
				Type:       "public-key",
				ID:         credID,
				Transports: cred.Transports,
			})
		}
	}

	return &PasskeyAuthenticationOptions{
		Challenge:        challenge,
		Timeout:          p.timeout,
		RelyingPartyID:   p.rpID,
		AllowCredentials: allowCredentials,
		UserVerification: "preferred",
	}, nil
}

// CompleteAuthentication verifies a passkey assertion and returns the user ID
// NOTE: This is a simplified implementation. In production, you should use a WebAuthn library
// like github.com/go-webauthn/webauthn to properly verify the assertion signature.
func (p *DatabasePasskeyProvider) CompleteAuthentication(ctx context.Context, response PasskeyAuthenticationResponse, expectedChallenge []byte) (int, error) {
	// TODO: Implement full WebAuthn verification
	// 1. Verify clientDataJSON contains correct challenge and origin
	// 2. Verify authenticatorData
	// 3. Verify signature using stored public key
	// 4. Update sign counter and check for cloning

	// Get credential from database
	var success bool
	var errorMsg sql.NullString
	var credentialJSON sql.NullString

	query := `SELECT p_success, p_error, p_credential::text FROM resolvespec_passkey_get_credential($1)`
	err := p.db.QueryRowContext(ctx, query, response.RawID).Scan(&success, &errorMsg, &credentialJSON)
	if err != nil {
		return 0, fmt.Errorf("failed to get credential: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return 0, fmt.Errorf("%s", errorMsg.String)
		}
		return 0, fmt.Errorf("credential not found")
	}

	// Parse credential
	var cred struct {
		UserID    int    `json:"user_id"`
		SignCount uint32 `json:"sign_count"`
	}
	if err := json.Unmarshal([]byte(credentialJSON.String), &cred); err != nil {
		return 0, fmt.Errorf("failed to parse credential: %w", err)
	}

	// TODO: Verify signature here
	// For now, we'll just update the counter as a placeholder

	// Update counter (in production, this should be done after successful verification)
	newCounter := cred.SignCount + 1
	var updateSuccess bool
	var updateError sql.NullString
	var cloneWarning sql.NullBool

	updateQuery := `SELECT p_success, p_error, p_clone_warning FROM resolvespec_passkey_update_counter($1, $2)`
	err = p.db.QueryRowContext(ctx, updateQuery, response.RawID, newCounter).Scan(&updateSuccess, &updateError, &cloneWarning)
	if err != nil {
		return 0, fmt.Errorf("failed to update counter: %w", err)
	}

	if cloneWarning.Valid && cloneWarning.Bool {
		return 0, fmt.Errorf("credential cloning detected")
	}

	return cred.UserID, nil
}

// GetCredentials returns all passkey credentials for a user
func (p *DatabasePasskeyProvider) GetCredentials(ctx context.Context, userID int) ([]PasskeyCredential, error) {
	var success bool
	var errorMsg sql.NullString
	var credentialsJSON sql.NullString

	query := `SELECT p_success, p_error, p_credentials::text FROM resolvespec_passkey_get_user_credentials($1)`
	err := p.db.QueryRowContext(ctx, query, userID).Scan(&success, &errorMsg, &credentialsJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return nil, fmt.Errorf("%s", errorMsg.String)
		}
		return nil, fmt.Errorf("failed to get credentials")
	}

	// Parse credentials
	var rawCreds []struct {
		ID              int       `json:"id"`
		UserID          int       `json:"user_id"`
		CredentialID    string    `json:"credential_id"`
		PublicKey       string    `json:"public_key"`
		AttestationType string    `json:"attestation_type"`
		AAGUID          string    `json:"aaguid"`
		SignCount       uint32    `json:"sign_count"`
		CloneWarning    bool      `json:"clone_warning"`
		Transports      []string  `json:"transports"`
		BackupEligible  bool      `json:"backup_eligible"`
		BackupState     bool      `json:"backup_state"`
		Name            string    `json:"name"`
		CreatedAt       time.Time `json:"created_at"`
		LastUsedAt      time.Time `json:"last_used_at"`
	}

	if err := json.Unmarshal([]byte(credentialsJSON.String), &rawCreds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	credentials := make([]PasskeyCredential, 0, len(rawCreds))
	for i := range rawCreds {
		raw := rawCreds[i]
		credID, err := base64.StdEncoding.DecodeString(raw.CredentialID)
		if err != nil {
			continue
		}
		pubKey, err := base64.StdEncoding.DecodeString(raw.PublicKey)
		if err != nil {
			continue
		}
		aaguid, _ := base64.StdEncoding.DecodeString(raw.AAGUID)

		credentials = append(credentials, PasskeyCredential{
			ID:              fmt.Sprintf("%d", raw.ID),
			UserID:          raw.UserID,
			CredentialID:    credID,
			PublicKey:       pubKey,
			AttestationType: raw.AttestationType,
			AAGUID:          aaguid,
			SignCount:       raw.SignCount,
			CloneWarning:    raw.CloneWarning,
			Transports:      raw.Transports,
			BackupEligible:  raw.BackupEligible,
			BackupState:     raw.BackupState,
			Name:            raw.Name,
			CreatedAt:       raw.CreatedAt,
			LastUsedAt:      raw.LastUsedAt,
		})
	}

	return credentials, nil
}

// DeleteCredential removes a passkey credential
func (p *DatabasePasskeyProvider) DeleteCredential(ctx context.Context, userID int, credentialID string) error {
	credID, err := base64.StdEncoding.DecodeString(credentialID)
	if err != nil {
		return fmt.Errorf("invalid credential ID: %w", err)
	}

	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_passkey_delete_credential($1, $2)`
	err = p.db.QueryRowContext(ctx, query, userID, credID).Scan(&success, &errorMsg)
	if err != nil {
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("failed to delete credential")
	}

	return nil
}

// UpdateCredentialName updates the friendly name of a credential
func (p *DatabasePasskeyProvider) UpdateCredentialName(ctx context.Context, userID int, credentialID string, name string) error {
	credID, err := base64.StdEncoding.DecodeString(credentialID)
	if err != nil {
		return fmt.Errorf("invalid credential ID: %w", err)
	}

	var success bool
	var errorMsg sql.NullString

	query := `SELECT p_success, p_error FROM resolvespec_passkey_update_name($1, $2, $3)`
	err = p.db.QueryRowContext(ctx, query, userID, credID, name).Scan(&success, &errorMsg)
	if err != nil {
		return fmt.Errorf("failed to update credential name: %w", err)
	}

	if !success {
		if errorMsg.Valid {
			return fmt.Errorf("%s", errorMsg.String)
		}
		return fmt.Errorf("failed to update credential name")
	}

	return nil
}
