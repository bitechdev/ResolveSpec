package sectypes

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// KeyType identifies the category of an auth key.
type KeyType string

const (
	// KeyTypeJWTSecret is a per-user JWT signing secret for token generation.
	KeyTypeJWTSecret KeyType = "jwt_secret"
	// KeyTypeHeaderAPI is a static API key sent via a request header.
	KeyTypeHeaderAPI KeyType = "header_api"
	// KeyTypeOAuth2 holds OAuth2 client credentials (client_id / client_secret).
	KeyTypeOAuth2 KeyType = "oauth2"
	// KeyTypeGenericAPI is a generic application API key.
	KeyTypeGenericAPI KeyType = "api"
)

// UserKey represents a single named auth key belonging to a user.
// KeyHash stores the SHA-256 hex digest of the raw key; the raw key is never persisted.
type UserKey struct {
	ID         int64          `json:"id"`
	UserID     int            `json:"user_id"`
	KeyType    KeyType        `json:"key_type"`
	KeyHash    string         `json:"key_hash"` // SHA-256 hex; never the raw key
	Name       string         `json:"name"`
	Scopes     []string       `json:"scopes,omitempty"`
	Meta       map[string]any `json:"meta,omitempty"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	LastUsedAt *time.Time     `json:"last_used_at,omitempty"`
	IsActive   bool           `json:"is_active"`
}

// CreateKeyRequest specifies the parameters for a new key.
type CreateKeyRequest struct {
	UserID    int
	KeyType   KeyType
	Name      string
	Scopes    []string
	Meta      map[string]any
	ExpiresAt *time.Time
}

// CreateKeyResponse is returned exactly once when a key is created.
// The caller is responsible for persisting RawKey; it is not stored anywhere.
type CreateKeyResponse struct {
	Key    UserKey
	RawKey string // crypto/rand 32 bytes, base64url-encoded
}

// HashKey returns the lowercase hex SHA-256 digest of a raw key. It is the value
// stored in UserKey.KeyHash; every keystore implementation uses it to hash raw
// keys before storage or lookup.
func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
