package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
)

// DatabaseKeyStoreOptions configures DatabaseKeyStore.
type DatabaseKeyStoreOptions struct {
	// Cache is an optional cache instance. If nil, uses the default cache.
	Cache *cache.Cache
	// CacheTTL is the duration to cache ValidateKey results.
	// Default: 2 minutes.
	CacheTTL time.Duration
	// SQLNames provides custom procedure names. If nil, uses DefaultKeyStoreSQLNames().
	SQLNames *KeyStoreSQLNames
}

// DatabaseKeyStore is a KeyStore backed by PostgreSQL stored procedures.
// All DB operations go through configurable procedure names; the raw key is
// never passed to the database.
//
// See keystore_schema.sql for the required table and procedure definitions.
//
// Note: DeleteKey invalidates the cache entry for the deleted key. Due to the
// cache TTL, a deleted key may continue to authenticate for up to CacheTTL
// (default 2 minutes) if the cache entry cannot be invalidated.
type DatabaseKeyStore struct {
	db       *sql.DB
	sqlNames *KeyStoreSQLNames
	cache    *cache.Cache
	cacheTTL time.Duration
}

// NewDatabaseKeyStore creates a DatabaseKeyStore with optional configuration.
func NewDatabaseKeyStore(db *sql.DB, opts ...DatabaseKeyStoreOptions) *DatabaseKeyStore {
	o := DatabaseKeyStoreOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.CacheTTL == 0 {
		o.CacheTTL = 2 * time.Minute
	}
	c := o.Cache
	if c == nil {
		c = cache.GetDefaultCache()
	}
	names := MergeKeyStoreSQLNames(DefaultKeyStoreSQLNames(), o.SQLNames)
	return &DatabaseKeyStore{
		db:       db,
		sqlNames: names,
		cache:    c,
		cacheTTL: o.CacheTTL,
	}
}

// CreateKey generates a raw key, stores its SHA-256 hash via the create procedure,
// and returns the raw key once.
func (ks *DatabaseKeyStore) CreateKey(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key material: %w", err)
	}
	rawKey := base64.RawURLEncoding.EncodeToString(rawBytes)
	hash := hashSHA256Hex(rawKey)

	type createRequest struct {
		UserID    int            `json:"user_id"`
		KeyType   KeyType        `json:"key_type"`
		KeyHash   string         `json:"key_hash"`
		Name      string         `json:"name"`
		Scopes    []string       `json:"scopes,omitempty"`
		Meta      map[string]any `json:"meta,omitempty"`
		ExpiresAt *time.Time     `json:"expires_at,omitempty"`
	}

	reqJSON, err := json.Marshal(createRequest{
		UserID:    req.UserID,
		KeyType:   req.KeyType,
		KeyHash:   hash,
		Name:      req.Name,
		Scopes:    req.Scopes,
		Meta:      req.Meta,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create key request: %w", err)
	}

	var success bool
	var errorMsg sql.NullString
	var keyJSON sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error, p_key::text FROM %s($1::jsonb)`, ks.sqlNames.CreateKey)
	if err = ks.db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &keyJSON); err != nil {
		return nil, fmt.Errorf("create key procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(nullStringOr(errorMsg, "create key failed"))
	}

	var key UserKey
	if err = json.Unmarshal([]byte(keyJSON.String), &key); err != nil {
		return nil, fmt.Errorf("failed to parse created key: %w", err)
	}

	return &CreateKeyResponse{Key: key, RawKey: rawKey}, nil
}

// GetUserKeys returns all active, non-expired keys for the given user.
// Pass an empty KeyType to return all types.
func (ks *DatabaseKeyStore) GetUserKeys(ctx context.Context, userID int, keyType KeyType) ([]UserKey, error) {
	var success bool
	var errorMsg sql.NullString
	var keysJSON sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error, p_keys::text FROM %s($1, $2)`, ks.sqlNames.GetUserKeys)
	if err := ks.db.QueryRowContext(ctx, query, userID, string(keyType)).Scan(&success, &errorMsg, &keysJSON); err != nil {
		return nil, fmt.Errorf("get user keys procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(nullStringOr(errorMsg, "get user keys failed"))
	}

	var keys []UserKey
	if keysJSON.Valid && keysJSON.String != "" && keysJSON.String != "[]" {
		if err := json.Unmarshal([]byte(keysJSON.String), &keys); err != nil {
			return nil, fmt.Errorf("failed to parse user keys: %w", err)
		}
	}
	if keys == nil {
		keys = []UserKey{}
	}
	return keys, nil
}

// DeleteKey soft-deletes a key after verifying ownership and invalidates its cache entry.
// The delete procedure returns the key_hash so no separate lookup is needed.
// Note: cache invalidation is best-effort; a cached entry may persist for up to CacheTTL.
func (ks *DatabaseKeyStore) DeleteKey(ctx context.Context, userID int, keyID int64) error {
	var success bool
	var errorMsg sql.NullString
	var keyHash sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error, p_key_hash FROM %s($1, $2)`, ks.sqlNames.DeleteKey)
	if err := ks.db.QueryRowContext(ctx, query, userID, keyID).Scan(&success, &errorMsg, &keyHash); err != nil {
		return fmt.Errorf("delete key procedure failed: %w", err)
	}
	if !success {
		return errors.New(nullStringOr(errorMsg, "delete key failed"))
	}

	if keyHash.Valid && keyHash.String != "" && ks.cache != nil {
		_ = ks.cache.Delete(ctx, keystoreCacheKey(keyHash.String))
	}
	return nil
}

// ValidateKey hashes the raw key and calls the validate procedure.
// Results are cached for CacheTTL to reduce DB load on hot paths.
func (ks *DatabaseKeyStore) ValidateKey(ctx context.Context, rawKey string, keyType KeyType) (*UserKey, error) {
	hash := hashSHA256Hex(rawKey)
	cacheKey := keystoreCacheKey(hash)

	if ks.cache != nil {
		var cached UserKey
		if err := ks.cache.Get(ctx, cacheKey, &cached); err == nil {
			if cached.IsActive {
				return &cached, nil
			}
			return nil, errors.New("invalid or expired key")
		}
	}

	var success bool
	var errorMsg sql.NullString
	var keyJSON sql.NullString

	query := fmt.Sprintf(`SELECT p_success, p_error, p_key::text FROM %s($1, $2)`, ks.sqlNames.ValidateKey)
	if err := ks.db.QueryRowContext(ctx, query, hash, string(keyType)).Scan(&success, &errorMsg, &keyJSON); err != nil {
		return nil, fmt.Errorf("validate key procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(nullStringOr(errorMsg, "invalid or expired key"))
	}

	var key UserKey
	if err := json.Unmarshal([]byte(keyJSON.String), &key); err != nil {
		return nil, fmt.Errorf("failed to parse validated key: %w", err)
	}

	if ks.cache != nil {
		_ = ks.cache.Set(ctx, cacheKey, key, ks.cacheTTL)
	}

	return &key, nil
}

func keystoreCacheKey(hash string) string {
	return "keystore:validate:" + hash
}

// nullStringOr returns s.String if valid, otherwise the fallback.
func nullStringOr(s sql.NullString, fallback string) string {
	if s.Valid && s.String != "" {
		return s.String
	}
	return fallback
}
