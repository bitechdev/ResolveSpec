package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Keys implements lookup.KeyStore with the resolvespec_keystore_* procedures.
type Keys struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.KeyStore = (*Keys)(nil)

// NewKeys creates the procedure-backed KeyStore.
func NewKeys(run Runner, procs lookup.ProcNames) *Keys { return &Keys{run: run, procs: procs} }

// orDefault returns the procedure's error message when it is non-empty, otherwise def.
func orDefault(s sql.NullString, def string) string {
	if s.Valid && s.String != "" {
		return s.String
	}
	return def
}

// Create implements lookup.KeyStore.
func (k *Keys) Create(ctx context.Context, req sectypes.CreateKeyRequest, keyHash string) (*sectypes.UserKey, error) {
	type createRequest struct {
		UserID    int              `json:"user_id"`
		KeyType   sectypes.KeyType `json:"key_type"`
		KeyHash   string           `json:"key_hash"`
		Name      string           `json:"name"`
		Scopes    []string         `json:"scopes,omitempty"`
		Meta      map[string]any   `json:"meta,omitempty"`
		ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	}
	reqJSON, err := json.Marshal(createRequest{
		UserID:    req.UserID,
		KeyType:   req.KeyType,
		KeyHash:   keyHash,
		Name:      req.Name,
		Scopes:    req.Scopes,
		Meta:      req.Meta,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create key request: %w", err)
	}

	var success bool
	var errorMsg, keyJSON sql.NullString
	err = k.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_key::text FROM %s($1::jsonb)`, k.procs.KeystoreCreateKey)
		return db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &keyJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("create key procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(orDefault(errorMsg, "create key failed"))
	}
	key, err := decodeKey([]byte(keyJSON.String))
	if err != nil {
		return nil, fmt.Errorf("failed to parse created key: %w", err)
	}
	return key, nil
}

// List implements lookup.KeyStore.
func (k *Keys) List(ctx context.Context, userID int, keyType sectypes.KeyType) ([]sectypes.UserKey, error) {
	var success bool
	var errorMsg, keysJSON sql.NullString
	err := k.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_keys::text FROM %s($1, $2)`, k.procs.KeystoreGetUserKeys)
		return db.QueryRowContext(ctx, query, userID, string(keyType)).Scan(&success, &errorMsg, &keysJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("get user keys procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(orDefault(errorMsg, "get user keys failed"))
	}
	var keys []sectypes.UserKey
	if keysJSON.Valid && keysJSON.String != "" && keysJSON.String != "[]" {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(keysJSON.String), &raw); err != nil {
			return nil, fmt.Errorf("failed to parse user keys: %w", err)
		}
		for _, r := range raw {
			k, err := decodeKey(r)
			if err != nil {
				return nil, fmt.Errorf("failed to parse user keys: %w", err)
			}
			keys = append(keys, *k)
		}
	}
	if keys == nil {
		keys = []sectypes.UserKey{}
	}
	return keys, nil
}

// Delete implements lookup.KeyStore. The procedure returns the key hash.
func (k *Keys) Delete(ctx context.Context, userID int, keyID int64) (string, error) {
	var success bool
	var errorMsg, keyHash sql.NullString
	err := k.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_key_hash FROM %s($1, $2)`, k.procs.KeystoreDeleteKey)
		return db.QueryRowContext(ctx, query, userID, keyID).Scan(&success, &errorMsg, &keyHash)
	})
	if err != nil {
		return "", fmt.Errorf("delete key procedure failed: %w", err)
	}
	if !success {
		return "", errors.New(orDefault(errorMsg, "delete key failed"))
	}
	return keyHash.String, nil
}

// Validate implements lookup.KeyStore.
func (k *Keys) Validate(ctx context.Context, keyHash string, keyType sectypes.KeyType) (*sectypes.UserKey, error) {
	var success bool
	var errorMsg, keyJSON sql.NullString
	err := k.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_key::text FROM %s($1, $2)`, k.procs.KeystoreValidateKey)
		return db.QueryRowContext(ctx, query, keyHash, string(keyType)).Scan(&success, &errorMsg, &keyJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("validate key procedure failed: %w", err)
	}
	if !success {
		return nil, errors.New(orDefault(errorMsg, "invalid or expired key"))
	}
	key, err := decodeKey([]byte(keyJSON.String))
	if err != nil {
		return nil, fmt.Errorf("failed to parse validated key: %w", err)
	}
	return key, nil
}

// decodeKey reads one key record from a key procedure.
func decodeKey(raw []byte) (*sectypes.UserKey, error) {
	var k sectypes.UserKey
	if err := json.Unmarshal(normalizeTimes(raw), &k); err != nil {
		return nil, err
	}
	return &k, nil
}
