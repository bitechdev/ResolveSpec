package direct

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Passkey implements lookup.PasskeyStore. credential_id, public_key and aaguid are base64
// TEXT (not native bytea) and transports is JSON, so one schema works on every dialect.
type Passkey struct{ *Base }

var _ lookup.PasskeyStore = (*Passkey)(nil)

// NewPasskey creates the direct PasskeyStore.
func NewPasskey(b *Base) *Passkey { return &Passkey{Base: b} }

// Store implements lookup.PasskeyStore.
func (p *Passkey) Store(ctx context.Context, rec lookup.PasskeyCredentialRecord) (int64, error) {
	transports, err := p.d.EncodeJSON(rec.Transports)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal transports: %w", err)
	}
	var id int64
	err = p.tx(ctx, func(q Querier) error {
		exists, err := p.From(lookup.EntityUserPasskeyCredentials).Cols(lookup.PasskeyID).
			Where(Eq(lookup.PasskeyCredentialID, rec.CredentialID)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("credential already exists")
		}
		userExists, err := p.From(lookup.EntityUsers).Cols(lookup.UsersID).Where(Eq(lookup.UsersID, rec.UserID)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if !userExists {
			return fmt.Errorf("user not found")
		}
		now := p.Now()
		id, err = p.Insert(lookup.EntityUserPasskeyCredentials).Set(
			Set(lookup.PasskeyUserID, rec.UserID),
			Set(lookup.PasskeyCredentialID, rec.CredentialID),
			Set(lookup.PasskeyPublicKey, rec.PublicKey),
			Set(lookup.PasskeyAttestationType, rec.AttestationType),
			Set(lookup.PasskeyAAGUID, ""),
			Set(lookup.PasskeySignCount, int64(rec.SignCount)),
			Set(lookup.PasskeyTransports, transports),
			Set(lookup.PasskeyBackupEligible, rec.BackupEligible),
			Set(lookup.PasskeyBackupState, rec.BackupState),
			Set(lookup.PasskeyName, rec.Name),
			Set(lookup.PasskeyCreatedAt, now),
			Set(lookup.PasskeyLastUsedAt, now),
		).ExecID(ctx, q, lookup.PasskeyID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Get implements lookup.PasskeyStore.
func (p *Passkey) Get(ctx context.Context, credentialID string) (userID int, signCount uint32, err error) {
	var count int64
	err = p.do(func(q Querier) error {
		return p.From(lookup.EntityUserPasskeyCredentials).Cols(lookup.PasskeyUserID, lookup.PasskeySignCount).
			Where(Eq(lookup.PasskeyCredentialID, credentialID)).QueryRow(ctx, q, &userID, &count)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, fmt.Errorf("credential not found")
		}
		return 0, 0, fmt.Errorf("failed to get credential: %w", err)
	}
	return userID, uint32(count), nil //nolint:gosec // sign counters are stored from uint32 values
}

// UpdateCounter implements lookup.PasskeyStore. A counter that did not advance flags the
// credential as possibly cloned and leaves the stored counter unchanged.
func (p *Passkey) UpdateCounter(ctx context.Context, credentialID string, newCounter uint32) (bool, error) {
	var clone bool
	err := p.tx(ctx, func(q Querier) error {
		match := Eq(lookup.PasskeyCredentialID, credentialID)
		var old int64
		if err := p.From(lookup.EntityUserPasskeyCredentials).Cols(lookup.PasskeySignCount).Where(match).QueryRow(ctx, q, &old); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("credential not found")
			}
			return err
		}
		if int64(newCounter) <= old {
			clone = true
			_, err := p.Update(lookup.EntityUserPasskeyCredentials).Set(Set(lookup.PasskeyCloneWarning, true)).Where(match).Exec(ctx, q)
			return err
		}
		_, err := p.Update(lookup.EntityUserPasskeyCredentials).
			Set(Set(lookup.PasskeySignCount, int64(newCounter)), Set(lookup.PasskeyLastUsedAt, p.Now())).
			Where(match).Exec(ctx, q)
		return err
	})
	return clone, err
}

// List implements lookup.PasskeyStore, newest first.
func (p *Passkey) List(ctx context.Context, userID int) ([]sectypes.PasskeyCredential, error) {
	var out []sectypes.PasskeyCredential
	err := p.do(func(q Querier) error {
		out = make([]sectypes.PasskeyCredential, 0)
		rows, err := p.From(lookup.EntityUserPasskeyCredentials).
			Cols(lookup.PasskeyID, lookup.PasskeyUserID, lookup.PasskeyCredentialID, lookup.PasskeyPublicKey,
				lookup.PasskeyAttestationType, lookup.PasskeyAAGUID, lookup.PasskeySignCount, lookup.PasskeyCloneWarning,
				lookup.PasskeyTransports, lookup.PasskeyBackupEligible, lookup.PasskeyBackupState, lookup.PasskeyName,
				lookup.PasskeyCreatedAt, lookup.PasskeyLastUsedAt).
			Where(Eq(lookup.PasskeyUserID, userID)).OrderBy(lookup.PasskeyCreatedAt).Query(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, uid int
			var credB64, pubB64 string
			var attestation, aaguidB64, name sql.NullString
			var count sql.NullInt64
			var clone, eligible, state bool
			var transports any
			var created, last time.Time
			if err := rows.Scan(&id, &uid, &credB64, &pubB64, &attestation, &aaguidB64, &count, p.boolDest(&clone),
				&transports, p.boolDest(&eligible), p.boolDest(&state), &name, p.timeDest(&created), p.timeDest(&last)); err != nil {
				return err
			}
			credID, err := base64.StdEncoding.DecodeString(credB64)
			if err != nil {
				continue
			}
			pub, err := base64.StdEncoding.DecodeString(pubB64)
			if err != nil {
				continue
			}
			aaguid, _ := base64.StdEncoding.DecodeString(aaguidB64.String)
			c := sectypes.PasskeyCredential{
				ID:              fmt.Sprintf("%d", id),
				UserID:          uid,
				CredentialID:    credID,
				PublicKey:       pub,
				AttestationType: attestation.String,
				AAGUID:          aaguid,
				SignCount:       uint32(count.Int64), //nolint:gosec // stored from uint32 values
				CloneWarning:    clone,
				BackupEligible:  eligible,
				BackupState:     state,
				Name:            name.String,
				CreatedAt:       created,
				LastUsedAt:      last,
			}
			_ = p.d.DecodeJSON(transports, &c.Transports)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	// newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Delete implements lookup.PasskeyStore.
func (p *Passkey) Delete(ctx context.Context, userID int, credentialID string) error {
	var rows int64
	err := p.do(func(q Querier) error {
		var err error
		rows, err = p.Base.Delete(lookup.EntityUserPasskeyCredentials).
			Where(Eq(lookup.PasskeyUserID, userID), Eq(lookup.PasskeyCredentialID, credentialID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// Rename implements lookup.PasskeyStore.
func (p *Passkey) Rename(ctx context.Context, userID int, credentialID, name string) error {
	var rows int64
	err := p.do(func(q Querier) error {
		var err error
		rows, err = p.Update(lookup.EntityUserPasskeyCredentials).Set(Set(lookup.PasskeyName, name)).
			Where(Eq(lookup.PasskeyUserID, userID), Eq(lookup.PasskeyCredentialID, credentialID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// ByUsername implements lookup.PasskeyStore.
func (p *Passkey) ByUsername(ctx context.Context, username string) (int, []lookup.PasskeyCredentialRef, error) {
	var userID int
	var creds []lookup.PasskeyCredentialRef
	err := p.do(func(q Querier) error {
		creds = make([]lookup.PasskeyCredentialRef, 0)
		if err := p.From(lookup.EntityUsers).Cols(lookup.UsersID).
			Where(Eq(lookup.UsersUsername, username), Eq(lookup.UsersIsActive, true)).QueryRow(ctx, q, &userID); err != nil {
			return err
		}
		rows, err := p.From(lookup.EntityUserPasskeyCredentials).Cols(lookup.PasskeyCredentialID, lookup.PasskeyTransports).
			Where(Eq(lookup.PasskeyUserID, userID)).Query(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var ref lookup.PasskeyCredentialRef
			var transports any
			if err := rows.Scan(&ref.CredentialID, &transports); err != nil {
				return err
			}
			_ = p.d.DecodeJSON(transports, &ref.Transports)
			creds = append(creds, ref)
		}
		return rows.Err()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, fmt.Errorf("user not found")
		}
		return 0, nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	return userID, creds, nil
}

// Login implements lookup.PasskeyStore: it creates the session for a user whose passkey
// assertion was already verified.
func (p *Passkey) Login(ctx context.Context, userID int, claims map[string]any) (*sectypes.LoginResponse, error) {
	var u userRow
	err := p.do(func(q Querier) error {
		return p.From(lookup.EntityUsers).
			Cols(lookup.UsersUsername, lookup.UsersEmail, lookup.UsersUserLevel, lookup.UsersRoles,
				lookup.UsersProgramUserID, lookup.UsersProgramUserTable).
			Where(Eq(lookup.UsersID, userID), Eq(lookup.UsersIsActive, true)).
			QueryRow(ctx, q, &u.username, &u.email, &u.userLevel, &u.roles, &u.programUserID, &u.programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("passkey login query failed: %w", err)
	}
	u.id = userID
	token, err := GenerateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	now := p.Now()
	ip, ua := claimStrings(claims)
	err = p.tx(ctx, func(q Querier) error {
		if err := p.Insert(lookup.EntityUserSessions).Set(
			Set(lookup.SessionsToken, token),
			Set(lookup.SessionsUserID, userID),
			Set(lookup.SessionsExpiresAt, now.Add(sessionLifetime)),
			Set(lookup.SessionsIPAddress, ip),
			Set(lookup.SessionsUserAgent, ua),
			Set(lookup.SessionsLastActivityAt, now),
			Set(lookup.SessionsCreatedAt, now),
		).Exec(ctx, q); err != nil {
			return err
		}
		_, err := p.Update(lookup.EntityUsers).Set(Set(lookup.UsersLastLoginAt, now)).Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("passkey login query failed: %w", err)
	}
	return &sectypes.LoginResponse{Token: token, User: u.context(token), ExpiresIn: int64(sessionLifetime.Seconds())}, nil
}
