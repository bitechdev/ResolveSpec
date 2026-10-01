package lookup

import (
	"context"
	"errors"
	"time"
)

// Errors returned by OAuthGrantStore. Callers compare with errors.Is.
var (
	// ErrRefreshInvalid: the refresh token is unknown, expired or revoked.
	ErrRefreshInvalid = errors.New("invalid refresh token")
	// ErrRefreshReused: a refresh token that was already rotated was presented again. The
	// store has revoked the whole token family.
	ErrRefreshReused = errors.New("refresh token reuse detected")
	// ErrDevicePending, ErrDeviceSlowDown, ErrDeviceDenied and ErrDeviceExpired are the RFC 8628
	// polling outcomes other than success.
	ErrDevicePending  = errors.New("authorization pending")
	ErrDeviceSlowDown = errors.New("slow down")
	ErrDeviceDenied   = errors.New("access denied")
	ErrDeviceExpired  = errors.New("device code expired")
	// ErrNotFound: the requested record does not exist or has expired.
	ErrNotFound = errors.New("not found")
)

// Consent records that a user allowed a client to act with Scopes.
type Consent struct {
	UserID    int       `json:"user_id"`
	ClientID  string    `json:"client_id"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RefreshToken is a server-managed refresh token. Only the SHA-256 hash of the raw token
// is stored.
type RefreshToken struct {
	TokenHash    string         `json:"token_hash"`
	FamilyID     string         `json:"family_id"`
	ClientID     string         `json:"client_id"`
	UserID       int            `json:"user_id"`
	SessionToken string         `json:"session_token"`
	Scopes       []string       `json:"scopes,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"` // nonce, auth_time, acr, sid, dpop_jkt, resource
	ExpiresAt    time.Time      `json:"expires_at"`
}

// DeviceStatus is the state of an RFC 8628 device authorization.
type DeviceStatus string

const (
	DevicePending  DeviceStatus = "pending"
	DeviceApproved DeviceStatus = "approved"
	DeviceDenied   DeviceStatus = "denied"
)

// DeviceCode is a pending RFC 8628 device authorization. DeviceHash is the SHA-256 hash of the
// device_code returned to the device; UserCode is stored as typed by the user (normalised).
type DeviceCode struct {
	DeviceHash   string       `json:"device_hash"`
	UserCode     string       `json:"user_code"`
	ClientID     string       `json:"client_id"`
	Scopes       []string     `json:"scopes,omitempty"`
	Status       DeviceStatus `json:"status"`
	UserID       int          `json:"user_id,omitempty"`
	SessionToken string       `json:"session_token,omitempty"`
	Interval     int          `json:"interval"` // minimum seconds between polls
	ExpiresAt    time.Time    `json:"expires_at"`
}

// PushedRequest is an RFC 9126 pushed authorization request.
type PushedRequest struct {
	RequestURI string            `json:"request_uri"`
	ClientID   string            `json:"client_id"`
	Params     map[string]string `json:"params"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

// OAuthGrantStore persists the OAuth2 authorization server state that is not a client, a code
// or a session: consents, refresh tokens, device codes, pushed requests and the replay cache.
type OAuthGrantStore interface {
	// SaveConsent replaces the consent of (UserID, ClientID).
	SaveConsent(ctx context.Context, c Consent) error
	// GetConsent returns the unexpired consent or ErrNotFound.
	GetConsent(ctx context.Context, userID int, clientID string) (*Consent, error)
	RevokeConsent(ctx context.Context, userID int, clientID string) error

	SaveRefresh(ctx context.Context, t RefreshToken) error
	// RotateRefresh atomically consumes the token with hash oldHash and stores next in the same
	// family. It returns the consumed token. An unknown, expired or revoked token is
	// ErrRefreshInvalid. A token that was already consumed revokes its family and returns the
	// token together with ErrRefreshReused so the caller can end the session.
	RotateRefresh(ctx context.Context, oldHash string, next RefreshToken) (*RefreshToken, error)
	// PeekRefresh returns the token without consuming it. Unknown, expired and revoked tokens are
	// ErrRefreshInvalid; an already rotated token is returned so RotateRefresh can report its reuse.
	PeekRefresh(ctx context.Context, hash string) (*RefreshToken, error)
	RevokeRefreshFamily(ctx context.Context, familyID string) error
	// RevokeRefreshBySession revokes every refresh token bound to a session token.
	RevokeRefreshBySession(ctx context.Context, sessionToken string) error

	CreateDevice(ctx context.Context, d DeviceCode) error
	// DeviceByUserCode returns the unexpired pending device authorization or ErrNotFound.
	DeviceByUserCode(ctx context.Context, userCode string) (*DeviceCode, error)
	// DeviceDecide approves or denies the device authorization of userCode.
	DeviceDecide(ctx context.Context, userCode string, approve bool, userID int, sessionToken string) error
	// DevicePoll implements the token endpoint side: it enforces the poll interval and returns
	// one of ErrDevicePending, ErrDeviceSlowDown, ErrDeviceDenied, ErrDeviceExpired or, once
	// approved, the record (consumed: it cannot be polled again).
	DevicePoll(ctx context.Context, deviceHash string) (*DeviceCode, error)

	SavePushedRequest(ctx context.Context, r PushedRequest) error
	// ConsumePushedRequest returns and deletes the request or ErrNotFound.
	ConsumePushedRequest(ctx context.Context, requestURI string) (*PushedRequest, error)

	// SeenJTI records key until expires and reports whether it was already recorded. It is the
	// replay cache for DPoP proofs and client assertions.
	SeenJTI(ctx context.Context, key string, expires time.Time) (bool, error)
}
