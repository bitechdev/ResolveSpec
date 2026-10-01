package procedure

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

// OAuthGrants implements lookup.OAuthGrantStore with the resolvespec_oauth_* grant procedures.
// Every procedure takes one jsonb request and returns (p_success, p_error, p_data). A failure
// that maps to a lookup sentinel carries a stable code in p_error (see the grantErrors table).
type OAuthGrants struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.OAuthGrantStore = (*OAuthGrants)(nil)

// NewOAuthGrants creates the procedure-backed OAuthGrantStore.
func NewOAuthGrants(run Runner, procs lookup.ProcNames) *OAuthGrants {
	return &OAuthGrants{run: run, procs: procs}
}

// grantErrors maps the codes a grant procedure puts in p_error to the lookup sentinels.
var grantErrors = map[string]error{
	"not_found":       lookup.ErrNotFound,
	"refresh_invalid": lookup.ErrRefreshInvalid,
	"refresh_reused":  lookup.ErrRefreshReused,
	"device_pending":  lookup.ErrDevicePending,
	"device_slowdown": lookup.ErrDeviceSlowDown,
	"device_denied":   lookup.ErrDeviceDenied,
	"device_expired":  lookup.ErrDeviceExpired,
}

// call runs proc with the JSON-encoded request. The returned data is the p_data of the
// procedure, also when it reports a failure (rotate returns the reused token that way).
func (o *OAuthGrants) call(ctx context.Context, proc string, req any) (data []byte, err error) {
	input, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	var success bool
	var errMsg sql.NullString
	err = o.run.Run(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT p_success, p_error, p_data::text
		FROM %s($1::jsonb)
	`, proc), input).Scan(&success, &errMsg, &data)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", proc, err)
	}
	if success {
		return data, nil
	}
	if e, ok := grantErrors[errMsg.String]; ok {
		return data, e
	}
	return data, failure(errMsg, proc+" failed")
}

// SaveConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SaveConsent(ctx context.Context, c lookup.Consent) error {
	_, err := o.call(ctx, o.procs.OAuthSaveConsent, c)
	return err
}

// GetConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) GetConsent(ctx context.Context, userID int, clientID string) (*lookup.Consent, error) {
	data, err := o.call(ctx, o.procs.OAuthGetConsent, map[string]any{"user_id": userID, "client_id": clientID})
	if err != nil {
		return nil, err
	}
	var c lookup.Consent
	if err := json.Unmarshal(normalizeTimes(data), &c); err != nil {
		return nil, fmt.Errorf("failed to parse consent: %w", err)
	}
	return &c, nil
}

// RevokeConsent implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeConsent(ctx context.Context, userID int, clientID string) error {
	_, err := o.call(ctx, o.procs.OAuthRevokeConsent, map[string]any{"user_id": userID, "client_id": clientID})
	return err
}

// SaveRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SaveRefresh(ctx context.Context, t lookup.RefreshToken) error {
	_, err := o.call(ctx, o.procs.OAuthSaveRefresh, t)
	return err
}

func parseRefresh(data []byte) (*lookup.RefreshToken, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var t lookup.RefreshToken
	if err := json.Unmarshal(normalizeTimes(data), &t); err != nil {
		return nil, fmt.Errorf("failed to parse refresh token: %w", err)
	}
	return &t, nil
}

// RotateRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RotateRefresh(ctx context.Context, oldHash string, next lookup.RefreshToken) (*lookup.RefreshToken, error) {
	data, err := o.call(ctx, o.procs.OAuthRotateRefresh, map[string]any{"old_hash": oldHash, "next": next})
	if err != nil && err != lookup.ErrRefreshReused { //nolint:errorlint // sentinel returned unwrapped by call
		return nil, err
	}
	t, perr := parseRefresh(data)
	if perr != nil {
		return nil, perr
	}
	return t, err
}

// PeekRefresh implements lookup.OAuthGrantStore.
func (o *OAuthGrants) PeekRefresh(ctx context.Context, hash string) (*lookup.RefreshToken, error) {
	data, err := o.call(ctx, o.procs.OAuthPeekRefresh, map[string]any{"token_hash": hash})
	if err != nil {
		return nil, err
	}
	return parseRefresh(data)
}

// RevokeRefreshFamily implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeRefreshFamily(ctx context.Context, familyID string) error {
	_, err := o.call(ctx, o.procs.OAuthRevokeRefreshFamily, map[string]any{"family_id": familyID})
	return err
}

// RevokeRefreshBySession implements lookup.OAuthGrantStore.
func (o *OAuthGrants) RevokeRefreshBySession(ctx context.Context, sessionToken string) error {
	_, err := o.call(ctx, o.procs.OAuthRevokeRefreshByUser, map[string]any{"session_token": sessionToken})
	return err
}

// CreateDevice implements lookup.OAuthGrantStore.
func (o *OAuthGrants) CreateDevice(ctx context.Context, d lookup.DeviceCode) error {
	if d.Status == "" {
		d.Status = lookup.DevicePending
	}
	_, err := o.call(ctx, o.procs.OAuthCreateDevice, d)
	return err
}

func parseDevice(data []byte) (*lookup.DeviceCode, error) {
	var d lookup.DeviceCode
	if err := json.Unmarshal(normalizeTimes(data), &d); err != nil {
		return nil, fmt.Errorf("failed to parse device code: %w", err)
	}
	return &d, nil
}

// DeviceByUserCode implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DeviceByUserCode(ctx context.Context, userCode string) (*lookup.DeviceCode, error) {
	data, err := o.call(ctx, o.procs.OAuthDeviceByUserCode, map[string]any{"user_code": userCode})
	if err != nil {
		return nil, err
	}
	return parseDevice(data)
}

// DeviceDecide implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DeviceDecide(ctx context.Context, userCode string, approve bool, userID int, sessionToken string) error {
	_, err := o.call(ctx, o.procs.OAuthDeviceDecide, map[string]any{
		"user_code": userCode, "approve": approve, "user_id": userID, "session_token": sessionToken,
	})
	return err
}

// DevicePoll implements lookup.OAuthGrantStore.
func (o *OAuthGrants) DevicePoll(ctx context.Context, deviceHash string) (*lookup.DeviceCode, error) {
	data, err := o.call(ctx, o.procs.OAuthDevicePoll, map[string]any{"device_hash": deviceHash})
	if err != nil {
		return nil, err
	}
	return parseDevice(data)
}

// SavePushedRequest implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SavePushedRequest(ctx context.Context, r lookup.PushedRequest) error {
	_, err := o.call(ctx, o.procs.OAuthSavePAR, r)
	return err
}

// ConsumePushedRequest implements lookup.OAuthGrantStore.
func (o *OAuthGrants) ConsumePushedRequest(ctx context.Context, requestURI string) (*lookup.PushedRequest, error) {
	data, err := o.call(ctx, o.procs.OAuthConsumePAR, map[string]any{"request_uri": requestURI})
	if err != nil {
		return nil, err
	}
	var r lookup.PushedRequest
	if err := json.Unmarshal(normalizeTimes(data), &r); err != nil {
		return nil, fmt.Errorf("failed to parse pushed request: %w", err)
	}
	return &r, nil
}

// SeenJTI implements lookup.OAuthGrantStore.
func (o *OAuthGrants) SeenJTI(ctx context.Context, key string, expires time.Time) (bool, error) {
	data, err := o.call(ctx, o.procs.OAuthSeenJTI, map[string]any{"key": key, "expires_at": expires})
	if err != nil {
		return false, err
	}
	var out struct {
		Seen bool `json:"seen"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return false, fmt.Errorf("failed to parse jti result: %w", err)
	}
	return out.Seen, nil
}
