package sectypes

import "time"

// OAuthServerClient is a persisted RFC 7591 registered OAuth2 client.
type OAuthServerClient struct {
	ClientID                string   `json:"client_id"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	AllowedScopes           []string `json:"allowed_scopes,omitempty"`
	ClientSecretHash        string   `json:"client_secret_hash,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

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
}

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
}
