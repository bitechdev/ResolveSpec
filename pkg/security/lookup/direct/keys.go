package direct

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Keys implements lookup.KeyStore on the user keys table. scopes and meta are stored as
// JSON through the dialect (native JSON column or TEXT).
type Keys struct{ *Base }

var _ lookup.KeyStore = (*Keys)(nil)

// NewKeys creates the direct KeyStore.
func NewKeys(b *Base) *Keys { return &Keys{Base: b} }

// Create implements lookup.KeyStore.
func (k *Keys) Create(ctx context.Context, req sectypes.CreateKeyRequest, keyHash string) (*sectypes.UserKey, error) {
	scopes, err := k.d.EncodeJSON(req.Scopes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scopes: %w", err)
	}
	meta, err := k.d.EncodeJSON(req.Meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal meta: %w", err)
	}
	now := k.Now()
	var id int64
	err = k.do(func(q Querier) error {
		var err error
		id, err = k.Insert(lookup.EntityUserKeys).Set(
			Set(lookup.KeysUserID, req.UserID),
			Set(lookup.KeysKeyType, string(req.KeyType)),
			Set(lookup.KeysKeyHash, keyHash),
			Set(lookup.KeysName, req.Name),
			Set(lookup.KeysScopes, scopes),
			Set(lookup.KeysMeta, meta),
			Set(lookup.KeysExpiresAt, req.ExpiresAt),
			Set(lookup.KeysCreatedAt, now),
			Set(lookup.KeysIsActive, true),
		).ExecID(ctx, q, lookup.KeysID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("create key query failed: %w", err)
	}
	return &sectypes.UserKey{
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

// keyScan holds the destinations for one key row.
type keyScan struct {
	k                         sectypes.UserKey
	keyType                   string
	scopes, meta              any
	expiresAt, created, lastU time.Time
	active                    bool
}

func (k *Keys) keyCols(withLastUsed bool) []lookup.Column {
	cols := []lookup.Column{lookup.KeysID, lookup.KeysUserID, lookup.KeysKeyType, lookup.KeysName, lookup.KeysScopes,
		lookup.KeysMeta, lookup.KeysExpiresAt, lookup.KeysCreatedAt, lookup.KeysIsActive}
	if withLastUsed {
		cols = append(cols, lookup.KeysLastUsedAt)
	}
	return cols
}

func (k *Keys) dest(s *keyScan, withLastUsed bool) []any {
	d := []any{&s.k.ID, &s.k.UserID, &s.keyType, &s.k.Name, &s.scopes, &s.meta,
		k.timeDest(&s.expiresAt), k.timeDest(&s.created), k.boolDest(&s.active)}
	if withLastUsed {
		d = append(d, k.timeDest(&s.lastU))
	}
	return d
}

func (k *Keys) finish(s *keyScan) sectypes.UserKey {
	out := s.k
	out.KeyType = sectypes.KeyType(s.keyType)
	out.CreatedAt = s.created
	out.IsActive = s.active
	_ = k.d.DecodeJSON(s.scopes, &out.Scopes)
	_ = k.d.DecodeJSON(s.meta, &out.Meta)
	if !s.expiresAt.IsZero() {
		t := s.expiresAt
		out.ExpiresAt = &t
	}
	if !s.lastU.IsZero() {
		t := s.lastU
		out.LastUsedAt = &t
	}
	return out
}

// List implements lookup.KeyStore: active, non-expired keys; an empty keyType means all types.
func (k *Keys) List(ctx context.Context, userID int, keyType sectypes.KeyType) ([]sectypes.UserKey, error) {
	keys := []sectypes.UserKey{}
	conds := []Cond{
		Eq(lookup.KeysUserID, userID),
		Eq(lookup.KeysIsActive, true),
		Or(IsNull(lookup.KeysExpiresAt), Gt(lookup.KeysExpiresAt, k.Now())),
	}
	if keyType != "" {
		conds = append(conds, Eq(lookup.KeysKeyType, string(keyType)))
	}
	err := k.do(func(q Querier) error {
		keys = keys[:0]
		rows, err := k.From(lookup.EntityUserKeys).Cols(k.keyCols(true)...).Where(conds...).OrderBy(lookup.KeysID).Query(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s keyScan
			if err := rows.Scan(k.dest(&s, true)...); err != nil {
				return err
			}
			keys = append(keys, k.finish(&s))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("get user keys query failed: %w", err)
	}
	return keys, nil
}

// Delete implements lookup.KeyStore: soft-deletes the key after checking ownership and
// returns its hash.
func (k *Keys) Delete(ctx context.Context, userID int, keyID int64) (string, error) {
	var keyHash string
	err := k.tx(ctx, func(q Querier) error {
		match := []Cond{Eq(lookup.KeysID, keyID), Eq(lookup.KeysUserID, userID), Eq(lookup.KeysIsActive, true)}
		if err := k.From(lookup.EntityUserKeys).Cols(lookup.KeysKeyHash).Where(match...).QueryRow(ctx, q, &keyHash); err != nil {
			return err
		}
		_, err := k.Update(lookup.EntityUserKeys).Set(Set(lookup.KeysIsActive, false)).Where(match...).Exec(ctx, q)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("key not found or already deleted")
		}
		return "", fmt.Errorf("delete key query failed: %w", err)
	}
	return keyHash, nil
}

// Validate implements lookup.KeyStore: finds an active, non-expired key by hash (optionally of
// one type) and stamps last_used_at.
func (k *Keys) Validate(ctx context.Context, keyHash string, keyType sectypes.KeyType) (*sectypes.UserKey, error) {
	conds := []Cond{
		Eq(lookup.KeysKeyHash, keyHash),
		Eq(lookup.KeysIsActive, true),
		Or(IsNull(lookup.KeysExpiresAt), Gt(lookup.KeysExpiresAt, k.Now())),
	}
	if keyType != "" {
		conds = append(conds, Eq(lookup.KeysKeyType, string(keyType)))
	}
	var s keyScan
	now := k.Now()
	err := k.tx(ctx, func(q Querier) error {
		s = keyScan{}
		if err := k.From(lookup.EntityUserKeys).Cols(k.keyCols(false)...).Where(conds...).QueryRow(ctx, q, k.dest(&s, false)...); err != nil {
			return err
		}
		_, err := k.Update(lookup.EntityUserKeys).Set(Set(lookup.KeysLastUsedAt, now)).Where(Eq(lookup.KeysID, s.k.ID)).Exec(ctx, q)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid or expired key")
		}
		return nil, fmt.Errorf("validate key query failed: %w", err)
	}
	out := k.finish(&s)
	out.KeyHash = keyHash
	out.LastUsedAt = &now
	return &out, nil
}
