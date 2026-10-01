package security

import (
	"context"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// hashSHA256Hex is kept as a short alias for sectypes.HashKey inside this package.
func hashSHA256Hex(raw string) string { return sectypes.HashKey(raw) }

// KeyStore manages per-user auth keys with pluggable storage backends.
// Implementations: ConfigKeyStore (static list) and DatabaseKeyStore (stored procedures).
type KeyStore interface {
	// CreateKey generates a new key, stores its hash, and returns the raw key once.
	CreateKey(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error)

	// GetUserKeys returns all active, non-expired keys for a user.
	// Pass an empty KeyType to return all types.
	GetUserKeys(ctx context.Context, userID int, keyType KeyType) ([]UserKey, error)

	// DeleteKey soft-deletes a key by ID after verifying ownership.
	DeleteKey(ctx context.Context, userID int, keyID int64) error

	// ValidateKey checks a raw key, returns the matching UserKey on success.
	// The implementation hashes the raw key before any lookup.
	// Pass an empty KeyType to accept any type.
	ValidateKey(ctx context.Context, rawKey string, keyType KeyType) (*UserKey, error)
}
