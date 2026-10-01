package direct

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// OAuthGrants implements lookup.OAuthGrantStore on tables. Every multi-step operation runs in
// one transaction, and single-use records (refresh rotation, device codes, pushed requests)
// are consumed with a conditional write so concurrent callers cannot both succeed.
type OAuthGrants struct{ *Base }

var _ lookup.OAuthGrantStore = (*OAuthGrants)(nil)

// NewOAuthGrants creates the direct OAuthGrantStore.
func NewOAuthGrants(b *Base) *OAuthGrants { return &OAuthGrants{Base: b} }

// optTime reads a nullable time column scanned into an `any`.
func (o *OAuthGrants) optTime(src any) (time.Time, bool) {
	if src == nil {
		return time.Time{}, false
	}
	t, err := o.d.ScanTime(src)
	if err != nil || t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// jsonArg encodes v for a JSON/TEXT column; an empty value is NULL.
func (o *OAuthGrants) jsonArg(v any) (any, error) { return o.d.EncodeJSON(v) }

// SaveConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SaveConsent(ctx context.Context, c lookup.Consent) error {
	scopes, err := o.jsonArg(c.Scopes)
	if err != nil {
		return err
	}
	return o.tx(ctx, func(q Querier) error {
		if _, err := o.Delete(lookup.EntityOAuthConsents).
			Where(Eq(lookup.OAuthConsentsUserID, c.UserID), Eq(lookup.OAuthConsentsClientID, c.ClientID)).Exec(ctx, q); err != nil {
			return err
		}
		return o.Insert(lookup.EntityOAuthConsents).Set(
			Set(lookup.OAuthConsentsUserID, c.UserID),
			Set(lookup.OAuthConsentsClientID, c.ClientID),
			Set(lookup.OAuthConsentsScopes, scopes),
			Set(lookup.OAuthConsentsCreatedAt, o.Now()),
			Set(lookup.OAuthConsentsExpiresAt, c.ExpiresAt),
		).Exec(ctx, q)
	})
}

// GetConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) GetConsent(ctx context.Context, userID int, clientID string) (*lookup.Consent, error) {
	var scopes any
	var exp time.Time
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityOAuthConsents).
			Cols(lookup.OAuthConsentsScopes, lookup.OAuthConsentsExpiresAt).
			Where(Eq(lookup.OAuthConsentsUserID, userID), Eq(lookup.OAuthConsentsClientID, clientID),
				Gt(lookup.OAuthConsentsExpiresAt, o.Now())).
			QueryRow(ctx, q, &scopes, o.timeDest(&exp))
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, lookup.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get consent: %w", err)
	}
	c := &lookup.Consent{UserID: userID, ClientID: clientID, ExpiresAt: exp}
	_ = o.d.DecodeJSON(scopes, &c.Scopes)
	return c, nil
}

// RevokeConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeConsent(ctx context.Context, userID int, clientID string) error {
	return o.do(func(q Querier) error {
		_, err := o.Delete(lookup.EntityOAuthConsents).
			Where(Eq(lookup.OAuthConsentsUserID, userID), Eq(lookup.OAuthConsentsClientID, clientID)).Exec(ctx, q)
		return err
	})
}

func (o *OAuthGrants) insertRefresh(ctx context.Context, q Querier, t lookup.RefreshToken) error {
	scopes, err := o.jsonArg(t.Scopes)
	if err != nil {
		return err
	}
	extra, err := o.jsonArg(t.Extra)
	if err != nil {
		return err
	}
	return o.Insert(lookup.EntityOAuthRefreshTokens).Set(
		Set(lookup.OAuthRefreshTokenHash, t.TokenHash),
		Set(lookup.OAuthRefreshFamilyID, t.FamilyID),
		Set(lookup.OAuthRefreshClientID, t.ClientID),
		Set(lookup.OAuthRefreshUserID, t.UserID),
		Set(lookup.OAuthRefreshSessionToken, t.SessionToken),
		Set(lookup.OAuthRefreshScopes, scopes),
		Set(lookup.OAuthRefreshExtra, extra),
		Set(lookup.OAuthRefreshCreatedAt, o.Now()),
		Set(lookup.OAuthRefreshExpiresAt, t.ExpiresAt),
	).Exec(ctx, q)
}

// SaveRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SaveRefresh(ctx context.Context, t lookup.RefreshToken) error {
	return o.do(func(q Querier) error { return o.insertRefresh(ctx, q, t) })
}

type refreshRow struct {
	lookup.RefreshToken
	used, revoked bool
	expired       bool
}

func (o *OAuthGrants) loadRefresh(ctx context.Context, q Querier, hash string) (*refreshRow, error) {
	var scopes, extra, usedAt, revokedAt any
	var exp time.Time
	var session sql.NullString
	r := &refreshRow{}
	r.TokenHash = hash
	err := o.From(lookup.EntityOAuthRefreshTokens).
		Cols(lookup.OAuthRefreshFamilyID, lookup.OAuthRefreshClientID, lookup.OAuthRefreshUserID,
			lookup.OAuthRefreshSessionToken, lookup.OAuthRefreshScopes, lookup.OAuthRefreshExtra,
			lookup.OAuthRefreshExpiresAt, lookup.OAuthRefreshUsedAt, lookup.OAuthRefreshRevokedAt).
		Where(Eq(lookup.OAuthRefreshTokenHash, hash)).
		QueryRow(ctx, q, &r.FamilyID, &r.ClientID, &r.UserID, &session, &scopes, &extra, o.timeDest(&exp), &usedAt, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, lookup.ErrRefreshInvalid
		}
		return nil, err
	}
	r.SessionToken = session.String
	r.ExpiresAt = exp
	_ = o.d.DecodeJSON(scopes, &r.Scopes)
	_ = o.d.DecodeJSON(extra, &r.Extra)
	_, r.used = o.optTime(usedAt)
	_, r.revoked = o.optTime(revokedAt)
	r.expired = !exp.After(o.Now())
	return r, nil
}

func (o *OAuthGrants) revokeFamilyTx(ctx context.Context, q Querier, family string) error {
	_, err := o.Update(lookup.EntityOAuthRefreshTokens).
		Set(Set(lookup.OAuthRefreshRevokedAt, o.Now())).
		Where(Eq(lookup.OAuthRefreshFamilyID, family), IsNull(lookup.OAuthRefreshRevokedAt)).Exec(ctx, q)
	return err
}

// RotateRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RotateRefresh(ctx context.Context, oldHash string, next lookup.RefreshToken) (*lookup.RefreshToken, error) {
	var old *refreshRow
	var reused bool
	err := o.tx(ctx, func(q Querier) error {
		r, err := o.loadRefresh(ctx, q, oldHash)
		if err != nil {
			return err
		}
		if r.revoked || r.expired {
			return lookup.ErrRefreshInvalid
		}
		old = r
		if r.used {
			// A rotated token came back: the family is compromised. The revoke must commit, so
			// the reuse is reported after the transaction instead of rolling it back.
			reused = true
			return o.revokeFamilyTx(ctx, q, r.FamilyID)
		}
		n, err := o.Update(lookup.EntityOAuthRefreshTokens).
			Set(Set(lookup.OAuthRefreshUsedAt, o.Now())).
			Where(Eq(lookup.OAuthRefreshTokenHash, oldHash), IsNull(lookup.OAuthRefreshUsedAt)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 { // lost a race with a concurrent rotation of the same token
			reused = true
			return o.revokeFamilyTx(ctx, q, r.FamilyID)
		}
		next.FamilyID = r.FamilyID
		next.ClientID = r.ClientID
		next.UserID = r.UserID
		if next.SessionToken == "" {
			next.SessionToken = r.SessionToken
		}
		return o.insertRefresh(ctx, q, next)
	})
	if err != nil {
		if errors.Is(err, lookup.ErrRefreshInvalid) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to rotate refresh token: %w", err)
	}
	tok := old.RefreshToken
	if reused {
		return &tok, lookup.ErrRefreshReused
	}
	return &tok, nil
}

// PeekRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) PeekRefresh(ctx context.Context, hash string) (*lookup.RefreshToken, error) {
	var r *refreshRow
	err := o.do(func(q Querier) error {
		var err error
		r, err = o.loadRefresh(ctx, q, hash)
		return err
	})
	if err != nil {
		if errors.Is(err, lookup.ErrRefreshInvalid) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read refresh token: %w", err)
	}
	if r.revoked || r.expired { // a rotated token is still returned so its reuse is detected by RotateRefresh
		return nil, lookup.ErrRefreshInvalid
	}
	t := r.RefreshToken
	return &t, nil
}

// RevokeRefreshFamily implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeRefreshFamily(ctx context.Context, familyID string) error {
	return o.do(func(q Querier) error { return o.revokeFamilyTx(ctx, q, familyID) })
}

// RevokeRefreshBySession implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeRefreshBySession(ctx context.Context, sessionToken string) error {
	return o.do(func(q Querier) error {
		_, err := o.Update(lookup.EntityOAuthRefreshTokens).
			Set(Set(lookup.OAuthRefreshRevokedAt, o.Now())).
			Where(Eq(lookup.OAuthRefreshSessionToken, sessionToken), IsNull(lookup.OAuthRefreshRevokedAt)).Exec(ctx, q)
		return err
	})
}

// CreateDevice implements lookup.OAuthGrantStore.
func (o *OAuthGrants) CreateDevice(ctx context.Context, d lookup.DeviceCode) error {
	scopes, err := o.jsonArg(d.Scopes)
	if err != nil {
		return err
	}
	status := d.Status
	if status == "" {
		status = lookup.DevicePending
	}
	return o.do(func(q Querier) error {
		return o.Insert(lookup.EntityOAuthDeviceCodes).Set(
			Set(lookup.OAuthDeviceHash, d.DeviceHash),
			Set(lookup.OAuthDeviceUserCode, strings.ToUpper(d.UserCode)),
			Set(lookup.OAuthDeviceClientID, d.ClientID),
			Set(lookup.OAuthDeviceScopes, scopes),
			Set(lookup.OAuthDeviceStatus, string(status)),
			Set(lookup.OAuthDeviceInterval, d.Interval),
			Set(lookup.OAuthDeviceCreatedAt, o.Now()),
			Set(lookup.OAuthDeviceExpiresAt, d.ExpiresAt),
		).Exec(ctx, q)
	})
}

// DeviceByUserCode implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DeviceByUserCode(ctx context.Context, userCode string) (*lookup.DeviceCode, error) {
	var d lookup.DeviceCode
	var scopes any
	var status string
	var exp time.Time
	userCode = strings.ToUpper(userCode)
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityOAuthDeviceCodes).
			Cols(lookup.OAuthDeviceHash, lookup.OAuthDeviceClientID, lookup.OAuthDeviceScopes, lookup.OAuthDeviceStatus,
				lookup.OAuthDeviceInterval, lookup.OAuthDeviceExpiresAt).
			Where(Eq(lookup.OAuthDeviceUserCode, userCode), Eq(lookup.OAuthDeviceStatus, string(lookup.DevicePending)),
				Gt(lookup.OAuthDeviceExpiresAt, o.Now())).
			QueryRow(ctx, q, &d.DeviceHash, &d.ClientID, &scopes, &status, &d.Interval, o.timeDest(&exp))
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, lookup.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read device code: %w", err)
	}
	d.UserCode = userCode
	d.Status = lookup.DeviceStatus(status)
	d.ExpiresAt = exp
	_ = o.d.DecodeJSON(scopes, &d.Scopes)
	return &d, nil
}

// DeviceDecide implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DeviceDecide(ctx context.Context, userCode string, approve bool, userID int, sessionToken string) error {
	status := lookup.DeviceDenied
	sets := []Assignment{}
	if approve {
		status = lookup.DeviceApproved
		sets = append(sets, Set(lookup.OAuthDeviceUserID, userID), Set(lookup.OAuthDeviceSessionToken, sessionToken))
	}
	sets = append(sets, Set(lookup.OAuthDeviceStatus, string(status)))
	var n int64
	err := o.do(func(q Querier) error {
		var err error
		n, err = o.Update(lookup.EntityOAuthDeviceCodes).Set(sets...).
			Where(Eq(lookup.OAuthDeviceUserCode, strings.ToUpper(userCode)),
				Eq(lookup.OAuthDeviceStatus, string(lookup.DevicePending)),
				Gt(lookup.OAuthDeviceExpiresAt, o.Now())).Exec(ctx, q)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to decide device code: %w", err)
	}
	if n == 0 {
		return lookup.ErrNotFound
	}
	return nil
}

// DevicePoll implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DevicePoll(ctx context.Context, deviceHash string) (*lookup.DeviceCode, error) {
	var out *lookup.DeviceCode
	var outErr error
	err := o.tx(ctx, func(q Querier) error {
		var d lookup.DeviceCode
		var scopes, polled any
		var status string
		var userID sql.NullInt64
		var session sql.NullString
		var exp time.Time
		err := o.From(lookup.EntityOAuthDeviceCodes).
			Cols(lookup.OAuthDeviceUserCode, lookup.OAuthDeviceClientID, lookup.OAuthDeviceScopes, lookup.OAuthDeviceStatus,
				lookup.OAuthDeviceUserID, lookup.OAuthDeviceSessionToken, lookup.OAuthDeviceInterval,
				lookup.OAuthDeviceExpiresAt, lookup.OAuthDeviceLastPolledAt).
			Where(Eq(lookup.OAuthDeviceHash, deviceHash)).
			QueryRow(ctx, q, &d.UserCode, &d.ClientID, &scopes, &status, &userID, &session, &d.Interval, o.timeDest(&exp), &polled)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				outErr = lookup.ErrDeviceExpired
				return nil
			}
			return err
		}
		now := o.Now()
		del := func() error {
			_, err := o.Delete(lookup.EntityOAuthDeviceCodes).Where(Eq(lookup.OAuthDeviceHash, deviceHash)).Exec(ctx, q)
			return err
		}
		if !exp.After(now) {
			outErr = lookup.ErrDeviceExpired
			return del()
		}
		if last, ok := o.optTime(polled); ok && now.Sub(last) < time.Duration(d.Interval)*time.Second {
			outErr = lookup.ErrDeviceSlowDown
		}
		if _, err := o.Update(lookup.EntityOAuthDeviceCodes).Set(Set(lookup.OAuthDeviceLastPolledAt, now)).
			Where(Eq(lookup.OAuthDeviceHash, deviceHash)).Exec(ctx, q); err != nil {
			return err
		}
		if outErr != nil {
			return nil
		}
		switch lookup.DeviceStatus(status) {
		case lookup.DeviceDenied:
			outErr = lookup.ErrDeviceDenied
			return del()
		case lookup.DeviceApproved:
			d.DeviceHash = deviceHash
			d.Status = lookup.DeviceApproved
			d.UserID = int(userID.Int64)
			d.SessionToken = session.String
			d.ExpiresAt = exp
			_ = o.d.DecodeJSON(scopes, &d.Scopes)
			out = &d
			return del()
		}
		outErr = lookup.ErrDevicePending
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll device code: %w", err)
	}
	return out, outErr
}

// SavePushedRequest implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SavePushedRequest(ctx context.Context, r lookup.PushedRequest) error {
	params, err := o.jsonArg(r.Params)
	if err != nil {
		return err
	}
	return o.do(func(q Querier) error {
		return o.Insert(lookup.EntityOAuthPARRequests).Set(
			Set(lookup.OAuthPARRequestURI, r.RequestURI),
			Set(lookup.OAuthPARClientID, r.ClientID),
			Set(lookup.OAuthPARParams, params),
			Set(lookup.OAuthPARCreatedAt, o.Now()),
			Set(lookup.OAuthPARExpiresAt, r.ExpiresAt),
		).Exec(ctx, q)
	})
}

// ConsumePushedRequest implements lookup.OAuthGrantStore.
func (o *OAuthGrants) ConsumePushedRequest(ctx context.Context, requestURI string) (*lookup.PushedRequest, error) {
	r := &lookup.PushedRequest{RequestURI: requestURI}
	var params any
	var exp time.Time
	err := o.tx(ctx, func(q Querier) error {
		if err := o.From(lookup.EntityOAuthPARRequests).
			Cols(lookup.OAuthPARClientID, lookup.OAuthPARParams, lookup.OAuthPARExpiresAt).
			Where(Eq(lookup.OAuthPARRequestURI, requestURI), Gt(lookup.OAuthPARExpiresAt, o.Now())).
			QueryRow(ctx, q, &r.ClientID, &params, o.timeDest(&exp)); err != nil {
			return err
		}
		n, err := o.Delete(lookup.EntityOAuthPARRequests).Where(Eq(lookup.OAuthPARRequestURI, requestURI)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, lookup.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume pushed request: %w", err)
	}
	r.ExpiresAt = exp
	_ = o.d.DecodeJSON(params, &r.Params)
	return r, nil
}

// SeenJTI implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SeenJTI(ctx context.Context, key string, expires time.Time) (bool, error) {
	seen := false
	err := o.tx(ctx, func(q Querier) error {
		if _, err := o.Delete(lookup.EntityOAuthJTI).Where(Lt(lookup.OAuthJTIExpiresAt, o.Now())).Exec(ctx, q); err != nil {
			return err
		}
		exists, err := o.From(lookup.EntityOAuthJTI).Cols(lookup.OAuthJTIKey).Where(Eq(lookup.OAuthJTIKey, key)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			seen = true
			return nil
		}
		return o.Insert(lookup.EntityOAuthJTI).Set(
			Set(lookup.OAuthJTIKey, key), Set(lookup.OAuthJTIExpiresAt, expires)).Exec(ctx, q)
	})
	if err != nil {
		// A concurrent insert of the same key violates the unique index: that is a replay.
		if ok, qerr := o.keyExists(ctx, key); qerr == nil && ok {
			return true, nil
		}
		return false, fmt.Errorf("failed to record jti: %w", err)
	}
	return seen, nil
}

func (o *OAuthGrants) keyExists(ctx context.Context, key string) (bool, error) {
	var ok bool
	err := o.do(func(q Querier) error {
		var err error
		ok, err = o.From(lookup.EntityOAuthJTI).Cols(lookup.OAuthJTIKey).Where(Eq(lookup.OAuthJTIKey, key)).Exists(ctx, q)
		return err
	})
	return ok, err
}
