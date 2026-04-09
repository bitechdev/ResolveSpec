package security

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// KeyStoreAuthenticator implements the Authenticator interface using a KeyStore.
// It is suitable for long-lived application credentials (API keys, JWT secrets, etc.)
// rather than interactive sessions. Login and Logout are not supported — key lifecycle
// is managed directly through the KeyStore.
//
// Key extraction order:
//  1. Authorization: Bearer <key>
//  2. Authorization: ApiKey <key>
//  3. X-API-Key header
type KeyStoreAuthenticator struct {
	keyStore KeyStore
	keyType  KeyType // empty = accept any type
}

// NewKeyStoreAuthenticator creates a KeyStoreAuthenticator.
// Pass an empty keyType to accept keys of any type.
func NewKeyStoreAuthenticator(ks KeyStore, keyType KeyType) *KeyStoreAuthenticator {
	return &KeyStoreAuthenticator{keyStore: ks, keyType: keyType}
}

// Login is not supported for keystore authentication.
func (a *KeyStoreAuthenticator) Login(_ context.Context, _ LoginRequest) (*LoginResponse, error) {
	return nil, fmt.Errorf("keystore authenticator does not support login")
}

// Logout is not supported for keystore authentication.
func (a *KeyStoreAuthenticator) Logout(_ context.Context, _ LogoutRequest) error {
	return nil
}

// Authenticate extracts an API key from the request and validates it against the KeyStore.
// Returns a UserContext built from the matching UserKey on success.
func (a *KeyStoreAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	rawKey := extractAPIKey(r)
	if rawKey == "" {
		return nil, fmt.Errorf("API key required (Authorization: Bearer/ApiKey <key> or X-API-Key header)")
	}

	userKey, err := a.keyStore.ValidateKey(r.Context(), rawKey, a.keyType)
	if err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	return userKeyToUserContext(userKey), nil
}

// extractAPIKey extracts a raw key from the request using the following precedence:
//  1. Authorization: Bearer <key>
//  2. Authorization: ApiKey <key>
//  3. X-API-Key header
func extractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(auth, "ApiKey "); ok {
			return strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// userKeyToUserContext converts a UserKey into a UserContext.
// Scopes are mapped to Roles. Key type and name are stored in Claims.
func userKeyToUserContext(k *UserKey) *UserContext {
	claims := map[string]any{
		"key_type": string(k.KeyType),
		"key_name": k.Name,
	}

	meta := k.Meta
	if meta == nil {
		meta = map[string]any{}
	}

	roles := k.Scopes
	if roles == nil {
		roles = []string{}
	}

	return &UserContext{
		UserID:    k.UserID,
		SessionID: fmt.Sprintf("key:%d", k.ID),
		Roles:     roles,
		Claims:    claims,
		Meta:      meta,
	}
}
