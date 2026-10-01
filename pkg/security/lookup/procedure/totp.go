package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// TOTP implements lookup.TOTPStore with the resolvespec_totp_* procedures.
type TOTP struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.TOTPStore = (*TOTP)(nil)

// NewTOTP creates the procedure-backed TOTPStore.
func NewTOTP(run Runner, procs lookup.ProcNames) *TOTP { return &TOTP{run: run, procs: procs} }

// exec runs a "p_success, p_error" procedure and maps failure to an error.
func (t *TOTP) exec(ctx context.Context, query, op, def string, args ...any) error {
	var success bool
	var errorMsg sql.NullString
	err := t.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, query, args...).Scan(&success, &errorMsg)
	})
	if err != nil {
		return fmt.Errorf("%s query failed: %w", op, err)
	}
	if !success {
		return failure(errorMsg, def)
	}
	return nil
}

// Enable implements lookup.TOTPStore.
func (t *TOTP) Enable(ctx context.Context, userID int, secret string, hashedCodes []string) error {
	codesJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal backup codes: %w", err)
	}
	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2, $3::jsonb)`, t.procs.TOTPEnable)
	return t.exec(ctx, query, "enable 2FA", "failed to enable 2FA", userID, secret, string(codesJSON))
}

// Disable implements lookup.TOTPStore.
func (t *TOTP) Disable(ctx context.Context, userID int) error {
	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1)`, t.procs.TOTPDisable)
	return t.exec(ctx, query, "disable 2FA", "failed to disable 2FA", userID)
}

// Status implements lookup.TOTPStore.
func (t *TOTP) Status(ctx context.Context, userID int) (bool, error) {
	var success, enabled bool
	var errorMsg sql.NullString
	err := t.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_enabled FROM %s($1)`, t.procs.TOTPGetStatus)
		return db.QueryRowContext(ctx, query, userID).Scan(&success, &errorMsg, &enabled)
	})
	if err != nil {
		return false, fmt.Errorf("get 2FA status query failed: %w", err)
	}
	if !success {
		return false, failure(errorMsg, "failed to get 2FA status")
	}
	return enabled, nil
}

// Secret implements lookup.TOTPStore.
func (t *TOTP) Secret(ctx context.Context, userID int) (string, error) {
	var success bool
	var errorMsg, secret sql.NullString
	err := t.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_secret FROM %s($1)`, t.procs.TOTPGetSecret)
		return db.QueryRowContext(ctx, query, userID).Scan(&success, &errorMsg, &secret)
	})
	if err != nil {
		return "", fmt.Errorf("get 2FA secret query failed: %w", err)
	}
	if !success {
		return "", failure(errorMsg, "failed to get 2FA secret")
	}
	if !secret.Valid {
		return "", fmt.Errorf("2FA secret not found")
	}
	return secret.String, nil
}

// RegenerateBackupCodes implements lookup.TOTPStore.
func (t *TOTP) RegenerateBackupCodes(ctx context.Context, userID int, hashedCodes []string) error {
	codesJSON, err := json.Marshal(hashedCodes)
	if err != nil {
		return fmt.Errorf("failed to marshal backup codes: %w", err)
	}
	query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2::jsonb)`, t.procs.TOTPRegenerateBackup)
	return t.exec(ctx, query, "regenerate backup codes", "failed to regenerate backup codes", userID, string(codesJSON))
}

// ValidateBackupCode implements lookup.TOTPStore. A failure without a message means
// "not valid", not an error.
func (t *TOTP) ValidateBackupCode(ctx context.Context, userID int, codeHash string) (bool, error) {
	var success, valid bool
	var errorMsg sql.NullString
	err := t.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_valid FROM %s($1, $2)`, t.procs.TOTPValidateBackupCode)
		return db.QueryRowContext(ctx, query, userID, codeHash).Scan(&success, &errorMsg, &valid)
	})
	if err != nil {
		return false, fmt.Errorf("validate backup code query failed: %w", err)
	}
	if !success {
		if errorMsg.Valid {
			return false, fmt.Errorf("%s", errorMsg.String)
		}
		return false, nil
	}
	return valid, nil
}
