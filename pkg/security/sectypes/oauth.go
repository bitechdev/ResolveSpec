package sectypes

import (
	"encoding/json"
	"time"
)

// OAuthServerClient is a persisted RFC 7591 registered OAuth2 client.
type OAuthServerClient struct {
	ClientID                string   `json:"client_id"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	AllowedScopes           []string `json:"allowed_scopes,omitempty"`
	ClientSecretHash        string   `json:"client_secret_hash,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`

	// The fields below are stored together in the oauth_clients.metadata JSON column, so a
	// new field never needs a schema change. See SplitJSON / MergeJSON.

	ResponseTypes               []string        `json:"response_types,omitempty"`
	ClientURI                   string          `json:"client_uri,omitempty"`
	LogoURI                     string          `json:"logo_uri,omitempty"`
	Contacts                    []string        `json:"contacts,omitempty"`
	PostLogoutRedirectURIs      []string        `json:"post_logout_redirect_uris,omitempty"`
	BackchannelLogoutURI        string          `json:"backchannel_logout_uri,omitempty"`
	JWKS                        json.RawMessage `json:"jwks,omitempty"`
	JWKSURI                     string          `json:"jwks_uri,omitempty"`
	IDTokenSignedResponseAlg    string          `json:"id_token_signed_response_alg,omitempty"`
	UserinfoSignedResponseAlg   string          `json:"userinfo_signed_response_alg,omitempty"`
	TokenEndpointAuthSigningAlg string          `json:"token_endpoint_auth_signing_alg,omitempty"`
	RequireConsent              bool            `json:"require_consent,omitempty"`
	FirstParty                  bool            `json:"first_party,omitempty"`
	RequirePAR                  bool            `json:"require_pushed_authorization_requests,omitempty"`
	DPoPBoundAccessTokens       bool            `json:"dpop_bound_access_tokens,omitempty"`
	RegistrationAccessTokenHash string          `json:"registration_access_token_hash,omitempty"`
	ClientSecretExpiresAt       int64           `json:"client_secret_expires_at,omitempty"`
	ClientIDIssuedAt            int64           `json:"client_id_issued_at,omitempty"`
}

// oauthClientColumns are the keys stored in their own oauth_clients columns; every other
// key of the JSON form is stored in the metadata column.
var oauthClientColumns = []string{
	"client_id", "redirect_uris", "client_name", "grant_types", "allowed_scopes",
	"client_secret_hash", "token_endpoint_auth_method",
}

// oauthCodeColumns are the keys stored in their own oauth_codes columns.
var oauthCodeColumns = []string{
	"code", "client_id", "redirect_uri", "client_state", "code_challenge", "code_challenge_method",
	"session_token", "refresh_token", "scopes", "expires_at",
}

// SplitJSON returns the JSON form of v without the keys in columns. It is the value stored in
// a metadata/extra column; an empty object is returned as "".
func SplitJSON(v any, columns []string) (string, error) {
	raw, err := json.Marshal(v) //nolint:gosec // G117: client secret hash and tokens are intentionally stored
	if err != nil {
		return "", err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	for _, c := range columns {
		delete(m, c)
	}
	if len(m) == 0 {
		return "", nil
	}
	out, err := json.Marshal(m)
	return string(out), err
}

// MergeJSON applies a metadata/extra JSON document onto dst. Empty input is a no-op.
func MergeJSON(dst any, data string) error {
	if data == "" || data == "null" {
		return nil
	}
	return json.Unmarshal([]byte(data), dst)
}

// ClientMetadataJSON returns the value of the oauth_clients.metadata column.
func (c *OAuthServerClient) ClientMetadataJSON() (string, error) {
	return SplitJSON(c, oauthClientColumns)
}

// ApplyClientMetadata merges the oauth_clients.metadata column into c.
func (c *OAuthServerClient) ApplyClientMetadata(data string) error { return MergeJSON(c, data) }

// OAuthCode is a short-lived authorization code.
type OAuthCode struct {
	Code                string    `json:"code"`
	ClientID            string    `json:"client_id"`
	RedirectURI         string    `json:"redirect_uri"`
	ClientState         string    `json:"client_state,omitempty"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	SessionToken        string    `json:"session_token"`
	RefreshToken        string    `json:"refresh_token,omitempty"`
	Scopes              []string  `json:"scopes,omitempty"`
	ExpiresAt           time.Time `json:"expires_at"`

	// Stored together in the oauth_codes.extra JSON column.

	UserID       int            `json:"user_id,omitempty"`
	Nonce        string         `json:"nonce,omitempty"`
	AuthTime     int64          `json:"auth_time,omitempty"`
	ACR          string         `json:"acr,omitempty"`
	AMR          []string       `json:"amr,omitempty"`
	SessionID    string         `json:"sid,omitempty"`
	Claims       map[string]any `json:"claims,omitempty"`
	Resource     []string       `json:"resource,omitempty"`
	DPoPJKT      string         `json:"dpop_jkt,omitempty"`
	ResponseType string         `json:"response_type,omitempty"`
	ConsentedAt  int64          `json:"consented_at,omitempty"`
}

// CodeExtraJSON returns the value of the oauth_codes.extra column.
func (c *OAuthCode) CodeExtraJSON() (string, error) { return SplitJSON(c, oauthCodeColumns) }

// ApplyCodeExtra merges the oauth_codes.extra column into c.
func (c *OAuthCode) ApplyCodeExtra(data string) error { return MergeJSON(c, data) }

// OAuthTokenInfo is the RFC 7662 token introspection response.
type OAuthTokenInfo struct {
	Active    bool     `json:"active"`
	Sub       string   `json:"sub,omitempty"`
	Username  string   `json:"username,omitempty"`
	Email     string   `json:"email,omitempty"`
	UserLevel int      `json:"user_level,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Exp       int64    `json:"exp,omitempty"`
	Iat       int64    `json:"iat,omitempty"`

	// Filled in by the OAuth server, not by the stores.
	Scope     string         `json:"scope,omitempty"`
	ClientID  string         `json:"client_id,omitempty"`
	TokenType string         `json:"token_type,omitempty"`
	Iss       string         `json:"iss,omitempty"`
	Aud       []string       `json:"aud,omitempty"`
	Jti       string         `json:"jti,omitempty"`
	Cnf       map[string]any `json:"cnf,omitempty"`
}
