package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/backends"
)

// DatabasePasskeyProvider implements PasskeyProvider on top of the lookup package
// (stored procedures on Postgres by default, direct SQL elsewhere).
type DatabasePasskeyProvider struct {
	src      *lookupSource
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
	// Lookup selects dialect, query mode and procedure/table/column names.
	Lookup lookup.Config
	// LookupProvider, when set, is used instead of building one from Lookup and the db.
	LookupProvider *lookup.Provider
	// DBFactory is called to obtain a fresh *sql.DB when the existing connection is closed.
	// If nil, reconnection is disabled.
	DBFactory func() (*sql.DB, error)
}

// NewDatabasePasskeyProvider creates a new database-backed passkey provider
func NewDatabasePasskeyProvider(db *sql.DB, opts DatabasePasskeyProviderOptions) *DatabasePasskeyProvider {
	if opts.Timeout == 0 {
		opts.Timeout = 60000 // 60 seconds default
	}
	src := newLookupSource(db)
	src.cfg = opts.Lookup
	src.provider = opts.LookupProvider
	src.opts = backends.Options{DBFactory: opts.DBFactory}
	return &DatabasePasskeyProvider{
		src:      src,
		rpID:     opts.RPID,
		rpName:   opts.RPName,
		rpOrigin: opts.RPOrigin,
		timeout:  opts.Timeout,
	}
}

func (p *DatabasePasskeyProvider) store() lookup.PasskeyStore { return p.src.get().Passkey }

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

	credIDB64 := base64.StdEncoding.EncodeToString(response.RawID)
	pubKeyB64 := base64.StdEncoding.EncodeToString(response.Response.AttestationObject)

	credentialID, err := p.store().Store(ctx, lookup.PasskeyCredentialRecord{
		UserID:          userID,
		CredentialID:    credIDB64,
		PublicKey:       pubKeyB64,
		AttestationType: "none",
		Transports:      response.Transports,
		Name:            "Passkey",
	})
	if err != nil {
		return nil, err
	}

	return &PasskeyCredential{
		ID:              fmt.Sprintf("%d", credentialID),
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
		_, refs, err := p.store().ByUsername(ctx, username)
		if err != nil {
			return nil, err
		}
		creds := refs

		allowCredentials = make([]PasskeyCredentialDescriptor, 0, len(creds))
		for _, cred := range creds {
			credID, err := base64.StdEncoding.DecodeString(cred.CredentialID)
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

	credIDB64 := base64.StdEncoding.EncodeToString(response.RawID)

	// TODO: Verify signature here
	// For now, we'll just update the counter as a placeholder
	store := p.store()
	userID, signCount, err := store.Get(ctx, credIDB64)
	if err != nil {
		return 0, err
	}

	// Update counter (in production, this should be done after successful verification)
	cloneWarning, err := store.UpdateCounter(ctx, credIDB64, signCount+1)
	if err != nil {
		return 0, fmt.Errorf("failed to update counter: %w", err)
	}
	if cloneWarning {
		return 0, fmt.Errorf("credential cloning detected")
	}

	return userID, nil
}

// GetCredentials returns all passkey credentials for a user
func (p *DatabasePasskeyProvider) GetCredentials(ctx context.Context, userID int) ([]PasskeyCredential, error) {
	return p.store().List(ctx, userID)
}

// DeleteCredential removes a passkey credential
func (p *DatabasePasskeyProvider) DeleteCredential(ctx context.Context, userID int, credentialID string) error {
	_, err := base64.StdEncoding.DecodeString(credentialID)
	if err != nil {
		return fmt.Errorf("invalid credential ID: %w", err)
	}

	return p.store().Delete(ctx, userID, credentialID)
}

// UpdateCredentialName updates the friendly name of a credential
func (p *DatabasePasskeyProvider) UpdateCredentialName(ctx context.Context, userID int, credentialID string, name string) error {
	_, err := base64.StdEncoding.DecodeString(credentialID)
	if err != nil {
		return fmt.Errorf("invalid credential ID: %w", err)
	}

	return p.store().Rename(ctx, userID, credentialID, name)
}
