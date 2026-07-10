package security

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Direct-mode implementations mirroring the resolvespec_totp_* stored
// procedures in database_schema.sql using plain SQL against TableNames.

func (p *DatabaseTwoFactorProvider) enable2FADirect(ctx context.Context, userID int, secret string, hashedCodes []string) error {
	return p.runDBOpWithReconnect(func(db *sql.DB) error {
		updQuery := rewritePlaceholders(db, fmt.Sprintf(
			`UPDATE %s SET totp_secret = ?, totp_enabled = ?, totp_enabled_at = ? WHERE id = ?`, p.tableNames.Users))
		res, err := db.ExecContext(ctx, updQuery, secret, true, time.Now(), userID)
		if err != nil {
			return err
		}
		if rows, err := res.RowsAffected(); err != nil {
			return err
		} else if rows == 0 {
			return fmt.Errorf("user not found")
		}

		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, p.tableNames.UserTOTPBackupCodes))
		if _, err := db.ExecContext(ctx, delQuery, userID); err != nil {
			return err
		}

		insQuery := rewritePlaceholders(db, fmt.Sprintf(`INSERT INTO %s (user_id, code_hash) VALUES (?, ?)`, p.tableNames.UserTOTPBackupCodes))
		for _, hash := range hashedCodes {
			if _, err := db.ExecContext(ctx, insQuery, userID, hash); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *DatabaseTwoFactorProvider) disable2FADirect(ctx context.Context, userID int) error {
	return p.runDBOpWithReconnect(func(db *sql.DB) error {
		updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET totp_secret = NULL, totp_enabled = ? WHERE id = ?`, p.tableNames.Users))
		res, err := db.ExecContext(ctx, updQuery, false, userID)
		if err != nil {
			return err
		}
		if rows, err := res.RowsAffected(); err != nil {
			return err
		} else if rows == 0 {
			return fmt.Errorf("user not found")
		}

		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, p.tableNames.UserTOTPBackupCodes))
		_, err = db.ExecContext(ctx, delQuery, userID)
		return err
	})
}

func (p *DatabaseTwoFactorProvider) get2FAStatusDirect(ctx context.Context, userID int) (bool, error) {
	var enabled sql.NullBool
	err := p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT totp_enabled FROM %s WHERE id = ?`, p.tableNames.Users))
		return db.QueryRowContext(ctx, query, userID).Scan(&enabled)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("user not found")
		}
		return false, fmt.Errorf("get 2FA status query failed: %w", err)
	}
	return enabled.Bool, nil
}

func (p *DatabaseTwoFactorProvider) get2FASecretDirect(ctx context.Context, userID int) (string, error) {
	var secret sql.NullString
	var enabled sql.NullBool
	err := p.runDBOpWithReconnect(func(db *sql.DB) error {
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT totp_secret, totp_enabled FROM %s WHERE id = ?`, p.tableNames.Users))
		return db.QueryRowContext(ctx, query, userID).Scan(&secret, &enabled)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("user not found")
		}
		return "", fmt.Errorf("get 2FA secret query failed: %w", err)
	}
	if !enabled.Bool {
		return "", fmt.Errorf("TOTP not enabled for user")
	}
	return secret.String, nil
}

func (p *DatabaseTwoFactorProvider) regenerateBackupCodesDirect(ctx context.Context, userID int, hashedCodes []string) error {
	return p.runDBOpWithReconnect(func(db *sql.DB) error {
		var count int
		checkQuery := rewritePlaceholders(db, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ? AND totp_enabled = ?`, p.tableNames.Users))
		if err := db.QueryRowContext(ctx, checkQuery, userID, true).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("user not found or TOTP not enabled")
		}

		delQuery := rewritePlaceholders(db, fmt.Sprintf(`DELETE FROM %s WHERE user_id = ?`, p.tableNames.UserTOTPBackupCodes))
		if _, err := db.ExecContext(ctx, delQuery, userID); err != nil {
			return err
		}

		insQuery := rewritePlaceholders(db, fmt.Sprintf(`INSERT INTO %s (user_id, code_hash) VALUES (?, ?)`, p.tableNames.UserTOTPBackupCodes))
		for _, hash := range hashedCodes {
			if _, err := db.ExecContext(ctx, insQuery, userID, hash); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *DatabaseTwoFactorProvider) validateBackupCodeDirect(ctx context.Context, userID int, codeHash string) (bool, error) {
	var valid bool
	err := p.runDBOpWithReconnect(func(db *sql.DB) error {
		var codeID int
		var used bool
		query := rewritePlaceholders(db, fmt.Sprintf(`SELECT id, used FROM %s WHERE user_id = ? AND code_hash = ?`, p.tableNames.UserTOTPBackupCodes))
		err := db.QueryRowContext(ctx, query, userID, codeHash).Scan(&codeID, &used)
		if errors.Is(err, sql.ErrNoRows) {
			valid = false
			return nil
		}
		if err != nil {
			return err
		}
		if used {
			return fmt.Errorf("backup code already used")
		}

		updQuery := rewritePlaceholders(db, fmt.Sprintf(`UPDATE %s SET used = ?, used_at = ? WHERE id = ?`, p.tableNames.UserTOTPBackupCodes))
		if _, err := db.ExecContext(ctx, updQuery, true, time.Now(), codeID); err != nil {
			return err
		}
		valid = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return valid, nil
}
