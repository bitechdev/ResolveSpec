package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Direct-mode implementations mirroring the resolvespec_keystore_* stored
// procedures in keystore_schema.sql using plain SQL against
// TableNames.UserKeys. meta/scopes are stored as JSON-encoded TEXT instead
// of Postgres JSONB.

func (ks *DatabaseKeyStore) createKeyDirect(ctx context.Context, req CreateKeyRequest, keyHash string) (*UserKey, error) {
	scopesJSON, err := json.Marshal(req.Scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scopes: %w", err)
	}
	var metaJSON []byte
	if req.Meta != nil {
		metaJSON, err = json.Marshal(req.Meta)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal meta: %w", err)
		}
	}

	now := time.Now()
	var id int64
	err = ks.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (user_id, key_type, key_hash, name, scopes, meta, expires_at, created_at, is_active) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ks.tableNames.UserKeys))
		res, err := db.ExecContext(ctx, query, req.UserID, string(req.KeyType), keyHash, req.Name, string(scopesJSON), nullableString(metaJSON), req.ExpiresAt, now, true)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create key query failed: %w", err)
	}

	return &UserKey{
		ID:        id,
		UserID:    req.UserID,
		KeyType:   req.KeyType,
		KeyHash:   keyHash,
		Name:      req.Name,
		Scopes:    req.Scopes,
		Meta:      req.Meta,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: now,
		IsActive:  true,
	}, nil
}

func (ks *DatabaseKeyStore) runDBOpWithReconnect(run func(*sql.DB) error) error {
	db := ks.getDB()
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	err := run(db)
	if isDBClosed(err) {
		if reconnErr := ks.reconnectDB(); reconnErr == nil {
			err = run(ks.getDB())
		}
	}
	return err
}

func (ks *DatabaseKeyStore) getUserKeysDirect(ctx context.Context, userID int, keyType KeyType) ([]UserKey, error) {
	keys := []UserKey{}
	err := ks.runDBOpWithReconnect(func(db *sql.DB) error {
		var query string
		var args []any
		if keyType == "" {
			query = rewritePlaceholders(db, fmt.Sprintf(
				`SELECT id, user_id, key_type, name, scopes, meta, expires_at, created_at, last_used_at, is_active
				 FROM %s WHERE user_id = ? AND is_active = ? AND (expires_at IS NULL OR expires_at > ?)`, ks.tableNames.UserKeys))
			args = []any{userID, true, time.Now()}
		} else {
			query = rewritePlaceholders(db, fmt.Sprintf(
				`SELECT id, user_id, key_type, name, scopes, meta, expires_at, created_at, last_used_at, is_active
				 FROM %s WHERE user_id = ? AND is_active = ? AND (expires_at IS NULL OR expires_at > ?) AND key_type = ?`, ks.tableNames.UserKeys))
			args = []any{userID, true, time.Now(), string(keyType)}
		}

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var k UserKey
			var kt string
			var scopesJSON, metaJSON sql.NullString
			var expiresAt, lastUsedAt sql.NullTime
			if err := rows.Scan(&k.ID, &k.UserID, &kt, &k.Name, &scopesJSON, &metaJSON, &expiresAt, &k.CreatedAt, &lastUsedAt, &k.IsActive); err != nil {
				return err
			}
			k.KeyType = KeyType(kt)
			if scopesJSON.Valid && scopesJSON.String != "" {
				_ = json.Unmarshal([]byte(scopesJSON.String), &k.Scopes)
			}
			if metaJSON.Valid && metaJSON.String != "" {
				_ = json.Unmarshal([]byte(metaJSON.String), &k.Meta)
			}
			if expiresAt.Valid {
				t := expiresAt.Time
				k.ExpiresAt = &t
			}
			if lastUsedAt.Valid {
				t := lastUsedAt.Time
				k.LastUsedAt = &t
			}
			keys = append(keys, k)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get user keys query failed: %w", err)
	}
	return keys, nil
}

func (ks *DatabaseKeyStore) deleteKeyDirect(ctx context.Context, userID int, keyID int64) error {
	var keyHash string
	err := ks.runDBOpWithReconnect(func(db *sql.DB) error {
		selQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT key_hash FROM %s WHERE id = ? AND user_id = ? AND is_active = ?`, ks.tableNames.UserKeys))
		if err := db.QueryRowContext(ctx, selQuery, keyID, userID, true).Scan(&keyHash); err != nil {
			return err
		}
		updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET is_active = ? WHERE id = ? AND user_id = ? AND is_active = ?`, ks.tableNames.UserKeys))
		_, err := db.ExecContext(ctx, updQuery, false, keyID, userID, true)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("key not found or already deleted")
		}
		return fmt.Errorf("delete key query failed: %w", err)
	}

	if keyHash != "" && ks.cache != nil {
		_ = ks.cache.Delete(ctx, keystoreCacheKey(keyHash))
	}
	return nil
}

func (ks *DatabaseKeyStore) validateKeyDirect(ctx context.Context, keyHash string, keyType KeyType) (*UserKey, error) {
	var k UserKey
	var kt string
	var scopesJSON, metaJSON sql.NullString
	var expiresAt, lastUsedAt sql.NullTime

	err := ks.runDBOpWithReconnect(func(db *sql.DB) error {
		var query string
		var args []any
		if keyType == "" {
			query = rewritePlaceholders(db, fmt.Sprintf(
				`SELECT id, user_id, key_type, name, scopes, meta, expires_at, created_at, is_active
				 FROM %s WHERE key_hash = ? AND is_active = ? AND (expires_at IS NULL OR expires_at > ?)`, ks.tableNames.UserKeys))
			args = []any{keyHash, true, time.Now()}
		} else {
			query = rewritePlaceholders(db, fmt.Sprintf(
				`SELECT id, user_id, key_type, name, scopes, meta, expires_at, created_at, is_active
				 FROM %s WHERE key_hash = ? AND is_active = ? AND (expires_at IS NULL OR expires_at > ?) AND key_type = ?`, ks.tableNames.UserKeys))
			args = []any{keyHash, true, time.Now(), string(keyType)}
		}

		if err := db.QueryRowContext(ctx, query, args...).Scan(&k.ID, &k.UserID, &kt, &k.Name, &scopesJSON, &metaJSON, &expiresAt, &k.CreatedAt, &k.IsActive); err != nil {
			return err
		}

		now := time.Now()
		updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET last_used_at = ? WHERE id = ?`, ks.tableNames.UserKeys))
		_, err := db.ExecContext(ctx, updQuery, now, k.ID)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid or expired key")
		}
		return nil, fmt.Errorf("validate key query failed: %w", err)
	}

	k.KeyType = KeyType(kt)
	k.KeyHash = keyHash
	if scopesJSON.Valid && scopesJSON.String != "" {
		_ = json.Unmarshal([]byte(scopesJSON.String), &k.Scopes)
	}
	if metaJSON.Valid && metaJSON.String != "" {
		_ = json.Unmarshal([]byte(metaJSON.String), &k.Meta)
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		k.ExpiresAt = &t
	}
	_ = lastUsedAt
	now := time.Now()
	k.LastUsedAt = &now

	return &k, nil
}

func nullableString(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}
