package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/bitechdev/ResolveSpec/pkg/cache"
	"github.com/bitechdev/ResolveSpec/pkg/dbtrace"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/backends"
)

// DatabaseKeyStoreOptions configures DatabaseKeyStore.
type DatabaseKeyStoreOptions struct {
	// Cache is an optional cache instance. If nil, uses the default cache.
	Cache *cache.Cache
	// CacheTTL is the duration to cache ValidateKey results.
	// Default: 2 minutes.
	CacheTTL time.Duration
	// Lookup selects dialect, query mode and procedure/table/column names.
	// The zero value uses stored procedures on Postgres and direct SQL elsewhere.
	Lookup lookup.Config
	// LookupProvider, when set, is used instead of building one from Lookup and the db.
	LookupProvider *lookup.Provider
	// DBFactory is called to obtain a fresh *sql.DB when the existing connection is closed.
	// If nil, reconnection is disabled.
	DBFactory func() (*sql.DB, error)
}

// DatabaseKeyStore is a KeyStore backed by the lookup package (stored procedures on
// Postgres by default, direct SQL elsewhere). The raw key is never passed to the database.
//
// See lookup/keystore_schema.sql for the required table and procedure definitions.
//
// Note: DeleteKey invalidates the cache entry for the deleted key. Due to the
// cache TTL, a deleted key may continue to authenticate for up to CacheTTL
// (default 2 minutes) if the cache entry cannot be invalidated.
type DatabaseKeyStore struct {
	src      *lookupSource
	cache    *cache.Cache
	cacheTTL time.Duration

	// validateLoads collapses concurrent key lookups for the same key
	validateLoads singleflight.Group
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
	src := newLookupSource(db)
	src.cfg = o.Lookup
	src.provider = o.LookupProvider
	src.opts = backends.Options{DBFactory: o.DBFactory}
	return &DatabaseKeyStore{src: src, cache: c, cacheTTL: o.CacheTTL}
}

func (ks *DatabaseKeyStore) keys() lookup.KeyStore { return ks.src.get().Keys }

// CreateKey generates a raw key, stores its SHA-256 hash via the create procedure,
// and returns the raw key once.
func (ks *DatabaseKeyStore) CreateKey(ctx context.Context, req CreateKeyRequest) (*CreateKeyResponse, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to generate key material: %w", err)
	}
	rawKey := base64.RawURLEncoding.EncodeToString(rawBytes)
	hash := hashSHA256Hex(rawKey)

	key, err := ks.keys().Create(ctx, req, hash)
	if err != nil {
		return nil, err
	}
	return &CreateKeyResponse{Key: *key, RawKey: rawKey}, nil
}

// GetUserKeys returns all active, non-expired keys for the given user.
// Pass an empty KeyType to return all types.
func (ks *DatabaseKeyStore) GetUserKeys(ctx context.Context, userID int, keyType KeyType) ([]UserKey, error) {
	return ks.keys().List(ctx, userID, keyType)
}

// DeleteKey soft-deletes a key after verifying ownership and invalidates its cache entry.
// The delete procedure returns the key_hash so no separate lookup is needed.
// Note: cache invalidation is best-effort; a cached entry may persist for up to CacheTTL.
func (ks *DatabaseKeyStore) DeleteKey(ctx context.Context, userID int, keyID int64) error {
	keyHash, err := ks.keys().Delete(ctx, userID, keyID)
	if err != nil {
		return err
	}
	if keyHash != "" && ks.cache != nil {
		_ = ks.cache.Delete(ctx, keystoreCacheKey(keyHash))
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

	// Concurrent misses for the same key share one database lookup.
	v, err, _ := ks.validateLoads.Do(cacheKey+"|"+string(keyType), func() (any, error) {
		return ks.validateKeyLoad(ctx, hash, cacheKey, keyType)
	})
	if err != nil {
		return nil, err
	}
	key, _ := v.(*UserKey)
	if key == nil {
		return nil, errors.New("invalid or expired key")
	}
	cp := *key
	return &cp, nil
}

// validateKeyLoad validates against the database and fills the cache.
func (ks *DatabaseKeyStore) validateKeyLoad(ctx context.Context, hash, cacheKey string, keyType KeyType) (*UserKey, error) {
	dbtrace.Raw(ctx, "keystore.validate")
	key, err := ks.keys().Validate(ctx, hash, keyType)
	if err != nil {
		return nil, err
	}

	if ks.cache != nil {
		_ = ks.cache.Set(ctx, cacheKey, *key, ks.cacheTTL)
	}

	return key, nil
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
