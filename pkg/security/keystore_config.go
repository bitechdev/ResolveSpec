package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ConfigKeyStore is an in-memory keystore backed by a static slice of UserKey values.
// It is designed for config-file driven setups (e.g. service accounts defined in YAML)
// with a small, bounded number of keys. For large or dynamic key sets use DatabaseKeyStore.
//
// Pre-existing entries must have KeyHash set to the SHA-256 hex of the intended raw key.
// Keys created at runtime via CreateKey are held in memory only and lost on restart.
type ConfigKeyStore struct {
	mu   sync.RWMutex
	keys []UserKey
	next int64 // monotonic ID counter for runtime-created keys (atomic)
}

// NewConfigKeyStore creates a ConfigKeyStore seeded with the provided keys.
// Pass nil or an empty slice to start with no pre-loaded keys.
// Zero-value entries (CreatedAt is zero) are treated as active and assigned the current time.
func NewConfigKeyStore(keys []UserKey) *ConfigKeyStore {
	var maxID int64
	copied := make([]UserKey, len(keys))
	copy(copied, keys)
	for i := range copied {
		if copied[i].CreatedAt.IsZero() {
			copied[i].IsActive = true
			copied[i].CreatedAt = time.Now()
		}
		if copied[i].ID > maxID {
			maxID = copied[i].ID
		}
	}
	return &ConfigKeyStore{keys: copied, next: maxID}
}

// CreateKey generates a new raw key, stores its SHA-256 hash, and returns the raw key once.
func (s *ConfigKeyStore) CreateKey(_ context.Context, req CreateKeyRequest) (*CreateKeyResponse, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key material: %w", err)
	}
	rawKey := base64.RawURLEncoding.EncodeToString(rawBytes)
	hash := hashSHA256Hex(rawKey)

	id := atomic.AddInt64(&s.next, 1)
	key := UserKey{
		ID:        id,
		UserID:    req.UserID,
		KeyType:   req.KeyType,
		KeyHash:   hash,
		Name:      req.Name,
		Scopes:    req.Scopes,
		Meta:      req.Meta,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: time.Now(),
		IsActive:  true,
	}

	s.mu.Lock()
	s.keys = append(s.keys, key)
	s.mu.Unlock()

	return &CreateKeyResponse{Key: key, RawKey: rawKey}, nil
}

// GetUserKeys returns all active, non-expired keys for the given user.
// Pass an empty KeyType to return all types.
func (s *ConfigKeyStore) GetUserKeys(_ context.Context, userID int, keyType KeyType) ([]UserKey, error) {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []UserKey
	for i := range s.keys {
		k := &s.keys[i]
		if k.UserID != userID || !k.IsActive {
			continue
		}
		if k.ExpiresAt != nil && k.ExpiresAt.Before(now) {
			continue
		}
		if keyType != "" && k.KeyType != keyType {
			continue
		}
		result = append(result, *k)
	}
	return result, nil
}

// DeleteKey soft-deletes a key by setting IsActive to false after ownership verification.
func (s *ConfigKeyStore) DeleteKey(_ context.Context, userID int, keyID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.keys {
		if s.keys[i].ID == keyID {
			if s.keys[i].UserID != userID {
				return fmt.Errorf("key not found or permission denied")
			}
			s.keys[i].IsActive = false
			return nil
		}
	}
	return fmt.Errorf("key not found")
}

// ValidateKey hashes the raw key and finds a matching, active, non-expired entry.
// Uses constant-time comparison to prevent timing side-channels.
// Pass an empty KeyType to accept any type.
func (s *ConfigKeyStore) ValidateKey(_ context.Context, rawKey string, keyType KeyType) (*UserKey, error) {
	hash := hashSHA256Hex(rawKey)
	hashBytes, _ := hex.DecodeString(hash)
	now := time.Now()

	// Write lock: ValidateKey updates LastUsedAt on the matched entry.
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.keys {
		k := &s.keys[i]
		if !k.IsActive {
			continue
		}
		if k.ExpiresAt != nil && k.ExpiresAt.Before(now) {
			continue
		}
		if keyType != "" && k.KeyType != keyType {
			continue
		}
		stored, _ := hex.DecodeString(k.KeyHash)
		if subtle.ConstantTimeCompare(hashBytes, stored) != 1 {
			continue
		}
		k.LastUsedAt = &now
		result := *k
		return &result, nil
	}
	return nil, fmt.Errorf("invalid or expired key")
}
