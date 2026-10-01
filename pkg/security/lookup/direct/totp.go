package direct

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// TOTP implements lookup.TOTPStore: the secret and enabled flag live on the users table,
// backup code hashes in their own table.
type TOTP struct{ *Base }

var _ lookup.TOTPStore = (*TOTP)(nil)

// NewTOTP creates the direct TOTPStore.
func NewTOTP(b *Base) *TOTP { return &TOTP{Base: b} }

func (t *TOTP) replaceBackupCodes(ctx context.Context, q Querier, userID int, hashed []string) error {
	if _, err := t.Delete(lookup.EntityUserTOTPBackupCodes).Where(Eq(lookup.BackupCodesUserID, userID)).Exec(ctx, q); err != nil {
		return err
	}
	now := t.Now()
	for _, h := range hashed {
		if err := t.Insert(lookup.EntityUserTOTPBackupCodes).Set(
			Set(lookup.BackupCodesUserID, userID),
			Set(lookup.BackupCodesCodeHash, h),
			Set(lookup.BackupCodesUsed, false),
			Set(lookup.BackupCodesCreatedAt, now),
		).Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// Enable implements lookup.TOTPStore.
func (t *TOTP) Enable(ctx context.Context, userID int, secret string, hashedCodes []string) error {
	return t.tx(ctx, func(q Querier) error {
		n, err := t.Update(lookup.EntityUsers).Set(
			Set(lookup.UsersTOTPSecret, secret),
			Set(lookup.UsersTOTPEnabled, true),
			Set(lookup.UsersTOTPEnabledAt, t.Now()),
		).Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("user not found")
		}
		return t.replaceBackupCodes(ctx, q, userID, hashedCodes)
	})
}

// Disable implements lookup.TOTPStore.
func (t *TOTP) Disable(ctx context.Context, userID int) error {
	return t.tx(ctx, func(q Querier) error {
		n, err := t.Update(lookup.EntityUsers).Set(Set(lookup.UsersTOTPSecret, nil), Set(lookup.UsersTOTPEnabled, false)).
			Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("user not found")
		}
		_, err = t.Delete(lookup.EntityUserTOTPBackupCodes).Where(Eq(lookup.BackupCodesUserID, userID)).Exec(ctx, q)
		return err
	})
}

// Status implements lookup.TOTPStore.
func (t *TOTP) Status(ctx context.Context, userID int) (bool, error) {
	var enabled bool
	err := t.do(func(q Querier) error {
		return t.From(lookup.EntityUsers).Cols(lookup.UsersTOTPEnabled).Where(Eq(lookup.UsersID, userID)).
			QueryRow(ctx, q, t.boolDest(&enabled))
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("user not found")
		}
		return false, fmt.Errorf("get 2FA status query failed: %w", err)
	}
	return enabled, nil
}

// Secret implements lookup.TOTPStore.
func (t *TOTP) Secret(ctx context.Context, userID int) (string, error) {
	var secret sql.NullString
	var enabled bool
	err := t.do(func(q Querier) error {
		return t.From(lookup.EntityUsers).Cols(lookup.UsersTOTPSecret, lookup.UsersTOTPEnabled).Where(Eq(lookup.UsersID, userID)).
			QueryRow(ctx, q, &secret, t.boolDest(&enabled))
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("user not found")
		}
		return "", fmt.Errorf("get 2FA secret query failed: %w", err)
	}
	if !enabled {
		return "", fmt.Errorf("TOTP not enabled for user")
	}
	return secret.String, nil
}

// RegenerateBackupCodes implements lookup.TOTPStore.
func (t *TOTP) RegenerateBackupCodes(ctx context.Context, userID int, hashedCodes []string) error {
	return t.tx(ctx, func(q Querier) error {
		ok, err := t.From(lookup.EntityUsers).Cols(lookup.UsersID).
			Where(Eq(lookup.UsersID, userID), Eq(lookup.UsersTOTPEnabled, true)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("user not found or TOTP not enabled")
		}
		return t.replaceBackupCodes(ctx, q, userID, hashedCodes)
	})
}

// ValidateBackupCode implements lookup.TOTPStore. An unknown code is (false, nil); a used
// code is an error. The code is consumed with a conditional update so it cannot be spent twice.
func (t *TOTP) ValidateBackupCode(ctx context.Context, userID int, codeHash string) (bool, error) {
	var valid bool
	err := t.tx(ctx, func(q Querier) error {
		var id int64
		var used bool
		err := t.From(lookup.EntityUserTOTPBackupCodes).Cols(lookup.BackupCodesID, lookup.BackupCodesUsed).
			Where(Eq(lookup.BackupCodesUserID, userID), Eq(lookup.BackupCodesCodeHash, codeHash)).
			QueryRow(ctx, q, &id, t.boolDest(&used))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if used {
			return fmt.Errorf("backup code already used")
		}
		n, err := t.Update(lookup.EntityUserTOTPBackupCodes).
			Set(Set(lookup.BackupCodesUsed, true), Set(lookup.BackupCodesUsedAt, t.Now())).
			Where(Eq(lookup.BackupCodesID, id), Eq(lookup.BackupCodesUsed, false)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("backup code already used")
		}
		valid = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return valid, nil
}
