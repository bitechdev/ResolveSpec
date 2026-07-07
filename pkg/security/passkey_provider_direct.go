package security

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Direct-mode implementations mirroring the resolvespec_passkey_* stored
// procedures in database_schema.sql using plain SQL against TableNames.
// credential_id/public_key/aaguid are stored as base64 TEXT (not native
// bytea) and transports as JSON-encoded TEXT, so the same schema works on
// SQLite, MySQL, and Postgres.

type storeCredentialParams struct {
	UserID          int
	CredentialID    string // base64
	PublicKey       string // base64
	AttestationType string
	SignCount       int
	Transports      []string
	BackupEligible  bool
	BackupState     bool
	Name            string
}

func (p *DatabasePasskeyProvider) storeCredentialDirect(ctx context.Context, params storeCredentialParams) (int64, error) {
	transportsJSON, err := json.Marshal(params.Transports)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal transports: %w", err)
	}

	var credentialID int64
	err = p.runDBOpWithReconnect(func(db *sql.DB) error {
		var exists int
		checkQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT 1 FROM %s WHERE credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		if err := db.QueryRowContext(ctx, checkQuery, params.CredentialID).Scan(&exists); err == nil {
			return fmt.Errorf("Credential already exists")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var userExists int
		userCheckQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT 1 FROM %s WHERE id = ?`, p.tableNames.Users))
		if err := db.QueryRowContext(ctx, userCheckQuery, params.UserID).Scan(&userExists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("User not found")
			}
			return err
		}

		now := time.Now()
		insertQuery := rewritePlaceholders(db, fmt.Sprintf(
			`INSERT INTO %s (user_id, credential_id, public_key, attestation_type, aaguid, sign_count, transports, backup_eligible, backup_state, name, created_at, last_used_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.tableNames.UserPasskeyCredentials))
		res, err := db.ExecContext(ctx, insertQuery, params.UserID, params.CredentialID, params.PublicKey, params.AttestationType,
			"", params.SignCount, string(transportsJSON), params.BackupEligible, params.BackupState, params.Name, now, now)
		if err != nil {
			return err
		}
		credentialID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return 0, err
	}
	return credentialID, nil
}

func (p *DatabasePasskeyProvider) getCredentialDirect(ctx context.Context, credentialIDB64 string) (userID int, signCount uint32, err error) {
	err = p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT user_id, sign_count FROM %s WHERE credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		return db.QueryRowContext(ctx, query, credentialIDB64).Scan(&userID, &signCount)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, fmt.Errorf("Credential not found")
		}
		return 0, 0, fmt.Errorf("failed to get credential: %w", err)
	}
	return userID, signCount, nil
}

func (p *DatabasePasskeyProvider) updateCounterDirect(ctx context.Context, credentialIDB64 string, newCounter uint32) (cloneWarning bool, err error) {
	err = p.runDBOpWithReconnect(func(db *sql.DB) error {
		var oldCounter int
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT sign_count FROM %s WHERE credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		if err := db.QueryRowContext(ctx, query, credentialIDB64).Scan(&oldCounter); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("Credential not found")
			}
			return err
		}

		if int(newCounter) <= oldCounter {
			cloneWarning = true
			updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET clone_warning = ? WHERE credential_id = ?`, p.tableNames.UserPasskeyCredentials))
			_, err := db.ExecContext(ctx, updQuery, true, credentialIDB64)
			return err
		}

		updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET sign_count = ?, last_used_at = ? WHERE credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		_, err := db.ExecContext(ctx, updQuery, newCounter, time.Now(), credentialIDB64)
		return err
	})
	return cloneWarning, err
}

func (p *DatabasePasskeyProvider) getUserCredentialsDirect(ctx context.Context, userID int) ([]PasskeyCredential, error) {
	var credentials []PasskeyCredential
	err := p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(
			`SELECT id, user_id, credential_id, public_key, attestation_type, aaguid, sign_count, clone_warning, transports, backup_eligible, backup_state, name, created_at, last_used_at
			 FROM %s WHERE user_id = ? ORDER BY created_at DESC`, p.tableNames.UserPasskeyCredentials))
		rows, err := db.QueryContext(ctx, query, userID)
		if err != nil {
			return err
		}
		defer rows.Close()

		credentials = make([]PasskeyCredential, 0)
		for rows.Next() {
			var id, uid int
			var credIDB64, pubKeyB64, attestationType, aaguidB64, name string
			var signCount uint32
			var cloneWarning, backupEligible, backupState bool
			var transportsJSON sql.NullString
			var createdAt, lastUsedAt time.Time

			if err := rows.Scan(&id, &uid, &credIDB64, &pubKeyB64, &attestationType, &aaguidB64, &signCount,
				&cloneWarning, &transportsJSON, &backupEligible, &backupState, &name, &createdAt, &lastUsedAt); err != nil {
				return err
			}

			credID, err := base64.StdEncoding.DecodeString(credIDB64)
			if err != nil {
				continue
			}
			pubKey, err := base64.StdEncoding.DecodeString(pubKeyB64)
			if err != nil {
				continue
			}
			aaguid, _ := base64.StdEncoding.DecodeString(aaguidB64)

			var transports []string
			if transportsJSON.Valid && transportsJSON.String != "" {
				_ = json.Unmarshal([]byte(transportsJSON.String), &transports)
			}

			credentials = append(credentials, PasskeyCredential{
				ID:              fmt.Sprintf("%d", id),
				UserID:          uid,
				CredentialID:    credID,
				PublicKey:       pubKey,
				AttestationType: attestationType,
				AAGUID:          aaguid,
				SignCount:       signCount,
				CloneWarning:    cloneWarning,
				Transports:      transports,
				BackupEligible:  backupEligible,
				BackupState:     backupState,
				Name:            name,
				CreatedAt:       createdAt,
				LastUsedAt:      lastUsedAt,
			})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	return credentials, nil
}

func (p *DatabasePasskeyProvider) deleteCredentialDirect(ctx context.Context, userID int, credentialIDB64 string) error {
	return p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ? AND credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		res, err := db.ExecContext(ctx, query, userID, credentialIDB64)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("Credential not found")
		}
		return nil
	})
}

func (p *DatabasePasskeyProvider) updateNameDirect(ctx context.Context, userID int, credentialIDB64, name string) error {
	return p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET name = ? WHERE user_id = ? AND credential_id = ?`, p.tableNames.UserPasskeyCredentials))
		res, err := db.ExecContext(ctx, query, name, userID, credentialIDB64)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("Credential not found")
		}
		return nil
	})
}

func (p *DatabasePasskeyProvider) getCredsByUsernameDirect(ctx context.Context, username string) (int, []struct {
	ID         string   `json:"credential_id"`
	Transports []string `json:"transports"`
}, error) {
	type credT = struct {
		ID         string   `json:"credential_id"`
		Transports []string `json:"transports"`
	}
	var userID int
	var creds []credT

	err := p.runDBOpWithReconnect(func(db *sql.DB) error {
		userQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT id FROM %s WHERE username = ? AND is_active = ?`, p.tableNames.Users))
		if err := db.QueryRowContext(ctx, userQuery, username, true).Scan(&userID); err != nil {
			return err
		}

		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT credential_id, transports FROM %s WHERE user_id = ?`, p.tableNames.UserPasskeyCredentials))
		rows, err := db.QueryContext(ctx, query, userID)
		if err != nil {
			return err
		}
		defer rows.Close()

		creds = make([]credT, 0)
		for rows.Next() {
			var credID string
			var transportsJSON sql.NullString
			if err := rows.Scan(&credID, &transportsJSON); err != nil {
				return err
			}
			var transports []string
			if transportsJSON.Valid && transportsJSON.String != "" {
				_ = json.Unmarshal([]byte(transportsJSON.String), &transports)
			}
			creds = append(creds, credT{ID: credID, Transports: transports})
		}
		return rows.Err()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, fmt.Errorf("User not found")
		}
		return 0, nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	return userID, creds, nil
}
