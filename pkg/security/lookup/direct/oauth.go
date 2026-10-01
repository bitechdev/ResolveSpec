package direct

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// nullIfEmpty keeps optional TEXT columns (e.g. client_secret_hash of a public client) NULL
// rather than "".
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// OAuthClients implements lookup.OAuthClientStore. Array columns (redirect_uris, grant_types,
// allowed_scopes, scopes) are JSON through the dialect.
type OAuthClients struct{ *Base }

var _ lookup.OAuthClientStore = (*OAuthClients)(nil)

// NewOAuthClients creates the direct OAuthClientStore.
func NewOAuthClients(b *Base) *OAuthClients { return &OAuthClients{Base: b} }

// RegisterClient implements lookup.OAuthClientStore.
func (o *OAuthClients) RegisterClient(ctx context.Context, client *sectypes.OAuthServerClient) (*sectypes.OAuthServerClient, error) {
	grantTypes := client.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	allowedScopes := client.AllowedScopes
	if len(allowedScopes) == 0 {
		allowedScopes = []string{"openid", "profile", "email"}
	}
	authMethod := client.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	redirects, err := o.d.EncodeJSON(client.RedirectURIs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal redirect_uris: %w", err)
	}
	if redirects == nil { // the column is NOT NULL
		redirects = "[]"
	}
	grants, err := o.d.EncodeJSON(grantTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal grant_types: %w", err)
	}
	scopes, err := o.d.EncodeJSON(allowedScopes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal allowed_scopes: %w", err)
	}

	err = o.do(func(q Querier) error {
		return o.Insert(lookup.EntityOAuthClients).Set(
			Set(lookup.OAuthClientsClientID, client.ClientID),
			Set(lookup.OAuthClientsRedirectURIs, redirects),
			Set(lookup.OAuthClientsClientName, client.ClientName),
			Set(lookup.OAuthClientsGrantTypes, grants),
			Set(lookup.OAuthClientsAllowedScopes, scopes),
			Set(lookup.OAuthClientsClientSecretHash, nullIfEmpty(client.ClientSecretHash)),
			Set(lookup.OAuthClientsTokenEndpointAuthMethod, authMethod),
			Set(lookup.OAuthClientsIsActive, true),
			Set(lookup.OAuthClientsCreatedAt, o.Now()),
		).Exec(ctx, q)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	return &sectypes.OAuthServerClient{
		ClientID:                client.ClientID,
		RedirectURIs:            client.RedirectURIs,
		ClientName:              client.ClientName,
		GrantTypes:              grantTypes,
		AllowedScopes:           allowedScopes,
		ClientSecretHash:        client.ClientSecretHash,
		TokenEndpointAuthMethod: authMethod,
	}, nil
}

// GetClient implements lookup.OAuthClientStore.
func (o *OAuthClients) GetClient(ctx context.Context, clientID string) (*sectypes.OAuthServerClient, error) {
	var redirects, grants, scopes any
	var name, secret, method sql.NullString
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityOAuthClients).
			Cols(lookup.OAuthClientsRedirectURIs, lookup.OAuthClientsClientName, lookup.OAuthClientsGrantTypes,
				lookup.OAuthClientsAllowedScopes, lookup.OAuthClientsClientSecretHash, lookup.OAuthClientsTokenEndpointAuthMethod).
			Where(Eq(lookup.OAuthClientsClientID, clientID), Eq(lookup.OAuthClientsIsActive, true)).
			QueryRow(ctx, q, &redirects, &name, &grants, &scopes, &secret, &method)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("client not found")
		}
		return nil, fmt.Errorf("failed to get client: %w", err)
	}
	res := &sectypes.OAuthServerClient{
		ClientID:                clientID,
		ClientName:              name.String,
		ClientSecretHash:        secret.String,
		TokenEndpointAuthMethod: method.String,
	}
	_ = o.d.DecodeJSON(redirects, &res.RedirectURIs)
	_ = o.d.DecodeJSON(grants, &res.GrantTypes)
	_ = o.d.DecodeJSON(scopes, &res.AllowedScopes)
	return res, nil
}

// SaveCode implements lookup.OAuthClientStore.
func (o *OAuthClients) SaveCode(ctx context.Context, code *sectypes.OAuthCode) error {
	scopes, err := o.d.EncodeJSON(code.Scopes)
	if err != nil {
		return fmt.Errorf("failed to marshal scopes: %w", err)
	}
	method := code.CodeChallengeMethod
	if method == "" {
		method = "S256"
	}
	return o.do(func(q Querier) error {
		return o.Insert(lookup.EntityOAuthCodes).Set(
			Set(lookup.OAuthCodesCode, code.Code),
			Set(lookup.OAuthCodesClientID, code.ClientID),
			Set(lookup.OAuthCodesRedirectURI, code.RedirectURI),
			Set(lookup.OAuthCodesClientState, code.ClientState),
			Set(lookup.OAuthCodesCodeChallenge, code.CodeChallenge),
			Set(lookup.OAuthCodesCodeChallengeMethod, method),
			Set(lookup.OAuthCodesSessionToken, code.SessionToken),
			Set(lookup.OAuthCodesRefreshToken, code.RefreshToken),
			Set(lookup.OAuthCodesScopes, scopes),
			Set(lookup.OAuthCodesExpiresAt, code.ExpiresAt),
			Set(lookup.OAuthCodesCreatedAt, o.Now()),
		).Exec(ctx, q)
	})
}

// ExchangeCode implements lookup.OAuthClientStore: the code is consumed in a transaction and
// only the caller whose delete removes the row gets it, so a code cannot be redeemed twice.
func (o *OAuthClients) ExchangeCode(ctx context.Context, code string) (*sectypes.OAuthCode, error) {
	var res sectypes.OAuthCode
	var state, refresh sql.NullString
	var scopes any
	err := o.tx(ctx, func(q Querier) error {
		err := o.From(lookup.EntityOAuthCodes).
			Cols(lookup.OAuthCodesClientID, lookup.OAuthCodesRedirectURI, lookup.OAuthCodesClientState,
				lookup.OAuthCodesCodeChallenge, lookup.OAuthCodesCodeChallengeMethod, lookup.OAuthCodesSessionToken,
				lookup.OAuthCodesRefreshToken, lookup.OAuthCodesScopes).
			Where(Eq(lookup.OAuthCodesCode, code), Gt(lookup.OAuthCodesExpiresAt, o.Now())).
			QueryRow(ctx, q, &res.ClientID, &res.RedirectURI, &state, &res.CodeChallenge, &res.CodeChallengeMethod,
				&res.SessionToken, &refresh, &scopes)
		if err != nil {
			return err
		}
		n, err := o.Delete(lookup.EntityOAuthCodes).Where(Eq(lookup.OAuthCodesCode, code)).Exec(ctx, q)
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("invalid or expired code")
		}
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	res.Code = code
	res.ClientState = state.String
	res.RefreshToken = refresh.String
	_ = o.d.DecodeJSON(scopes, &res.Scopes)
	return &res, nil
}

// Introspect implements lookup.OAuthClientStore (RFC 7662). An unknown or expired token is
// {active:false}, not an error.
func (o *OAuthClients) Introspect(ctx context.Context, token string) (*sectypes.OAuthTokenInfo, error) {
	var info sectypes.OAuthTokenInfo
	var userID int
	var username, email, roles sql.NullString
	var level sql.NullInt64
	var exp, iat time.Time
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityUserSessions).
			Cols(lookup.UsersID, lookup.UsersUsername, lookup.UsersEmail, lookup.UsersUserLevel, lookup.UsersRoles,
				lookup.SessionsExpiresAt, lookup.SessionsCreatedAt).
			Join(lookup.EntityUsers, EqCol(lookup.UsersID, lookup.SessionsUserID)).
			Where(Eq(lookup.SessionsToken, token), Gt(lookup.SessionsExpiresAt, o.Now()), Eq(lookup.UsersIsActive, true)).
			QueryRow(ctx, q, &userID, &username, &email, &level, &roles, o.timeDest(&exp), o.timeDest(&iat))
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &sectypes.OAuthTokenInfo{Active: false}, nil
		}
		return nil, fmt.Errorf("failed to introspect token: %w", err)
	}
	info.Active = true
	info.Sub = fmt.Sprintf("%d", userID)
	info.Username = username.String
	info.Email = email.String
	info.UserLevel = int(level.Int64)
	info.Roles = ParseRoles(roles.String)
	if !exp.IsZero() {
		info.Exp = exp.Unix()
	}
	if !iat.IsZero() {
		info.Iat = iat.Unix()
	}
	return &info, nil
}

// Revoke implements lookup.OAuthClientStore (RFC 7009): the session is deleted; an unknown
// token is not an error.
func (o *OAuthClients) Revoke(ctx context.Context, token string) error {
	return o.do(func(q Querier) error {
		_, err := o.Delete(lookup.EntityUserSessions).Where(Eq(lookup.SessionsToken, token)).Exec(ctx, q)
		return err
	})
}

// OAuthUsers implements lookup.OAuthUserStore.
type OAuthUsers struct{ *Base }

var _ lookup.OAuthUserStore = (*OAuthUsers)(nil)

// NewOAuthUsers creates the direct OAuthUserStore.
func NewOAuthUsers(b *Base) *OAuthUsers { return &OAuthUsers{Base: b} }

// GetOrCreateUser implements lookup.OAuthUserStore: select by email, then update or insert,
// in one transaction (no upsert).
func (o *OAuthUsers) GetOrCreateUser(ctx context.Context, user *sectypes.UserContext, provider string) (int, error) {
	roles := strings.Join(user.Roles, ",")
	var userID int
	err := o.tx(ctx, func(q Querier) error {
		now := o.Now()
		var remoteID, authProvider sql.NullString
		err := o.From(lookup.EntityUsers).Cols(lookup.UsersID, lookup.UsersRemoteID, lookup.UsersAuthProvider).
			Where(Eq(lookup.UsersEmail, user.Email)).QueryRow(ctx, q, &userID, &remoteID, &authProvider)
		if err == nil {
			// remote_id and auth_provider are only filled when still unset.
			sets := []Assignment{Set(lookup.UsersLastLoginAt, now), Set(lookup.UsersUpdatedAt, now)}
			if !remoteID.Valid {
				sets = append(sets, Set(lookup.UsersRemoteID, user.RemoteID))
			}
			if !authProvider.Valid {
				sets = append(sets, Set(lookup.UsersAuthProvider, provider))
			}
			_, err := o.Update(lookup.EntityUsers).Set(sets...).Where(Eq(lookup.UsersID, userID)).Exec(ctx, q)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := o.Insert(lookup.EntityUsers).Set(
			Set(lookup.UsersUsername, user.UserName),
			Set(lookup.UsersEmail, user.Email),
			Set(lookup.UsersPassword, nil),
			Set(lookup.UsersUserLevel, user.UserLevel),
			Set(lookup.UsersRoles, roles),
			Set(lookup.UsersIsActive, true),
			Set(lookup.UsersCreatedAt, now),
			Set(lookup.UsersUpdatedAt, now),
			Set(lookup.UsersLastLoginAt, now),
			Set(lookup.UsersRemoteID, user.RemoteID),
			Set(lookup.UsersAuthProvider, provider),
		).ExecID(ctx, q, lookup.UsersID)
		userID = int(id)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get or create user: %w", err)
	}
	return userID, nil
}

// CreateSession implements lookup.OAuthUserStore: insert, or update when the token exists.
func (o *OAuthUsers) CreateSession(ctx context.Context, s lookup.OAuthSession) error {
	return o.tx(ctx, func(q Querier) error {
		now := o.Now()
		exists, err := o.From(lookup.EntityUserSessions).Cols(lookup.SessionsID).Where(Eq(lookup.SessionsToken, s.SessionToken)).Exists(ctx, q)
		if err != nil {
			return err
		}
		if exists {
			_, err := o.Update(lookup.EntityUserSessions).Set(
				Set(lookup.SessionsAccessToken, s.AccessToken),
				Set(lookup.SessionsRefreshToken, s.RefreshToken),
				Set(lookup.SessionsTokenType, s.TokenType),
				Set(lookup.SessionsExpiresAt, s.ExpiresAt),
				Set(lookup.SessionsLastActivityAt, now),
			).Where(Eq(lookup.SessionsToken, s.SessionToken)).Exec(ctx, q)
			return err
		}
		return o.Insert(lookup.EntityUserSessions).Set(
			Set(lookup.SessionsToken, s.SessionToken),
			Set(lookup.SessionsUserID, s.UserID),
			Set(lookup.SessionsExpiresAt, s.ExpiresAt),
			Set(lookup.SessionsCreatedAt, now),
			Set(lookup.SessionsLastActivityAt, now),
			Set(lookup.SessionsAccessToken, s.AccessToken),
			Set(lookup.SessionsRefreshToken, s.RefreshToken),
			Set(lookup.SessionsTokenType, s.TokenType),
			Set(lookup.SessionsAuthProvider, s.Provider),
		).Exec(ctx, q)
	})
}

// GetByRefreshToken implements lookup.OAuthUserStore.
func (o *OAuthUsers) GetByRefreshToken(ctx context.Context, refreshToken string) (*lookup.OAuthRefreshSession, error) {
	var s lookup.OAuthRefreshSession
	var access, tokenType sql.NullString
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityUserSessions).
			Cols(lookup.SessionsUserID, lookup.SessionsAccessToken, lookup.SessionsTokenType, lookup.SessionsExpiresAt).
			Where(Eq(lookup.SessionsRefreshToken, refreshToken), Gt(lookup.SessionsExpiresAt, o.Now())).
			QueryRow(ctx, q, &s.UserID, &access, &tokenType, o.timeDest(&s.Expiry))
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("refresh token not found or expired")
		}
		return nil, fmt.Errorf("failed to get session by refresh token: %w", err)
	}
	s.AccessToken = access.String
	s.TokenType = tokenType.String
	return &s, nil
}

// UpdateRefreshToken implements lookup.OAuthUserStore.
func (o *OAuthUsers) UpdateRefreshToken(ctx context.Context, userID int, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken string, expiresAt time.Time) error {
	var rows int64
	err := o.do(func(q Querier) error {
		var err error
		rows, err = o.Update(lookup.EntityUserSessions).Set(
			Set(lookup.SessionsToken, newSessionToken),
			Set(lookup.SessionsAccessToken, newAccessToken),
			Set(lookup.SessionsRefreshToken, newRefreshToken),
			Set(lookup.SessionsExpiresAt, expiresAt),
			Set(lookup.SessionsLastActivityAt, o.Now()),
		).Where(Eq(lookup.SessionsUserID, userID), Eq(lookup.SessionsRefreshToken, oldRefreshToken)).Exec(ctx, q)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session not found")
	}
	return nil
}

// GetUser implements lookup.OAuthUserStore.
func (o *OAuthUsers) GetUser(ctx context.Context, userID int) (*sectypes.UserContext, error) {
	var u userRow
	err := o.do(func(q Querier) error {
		return o.From(lookup.EntityUsers).
			Cols(lookup.UsersUsername, lookup.UsersEmail, lookup.UsersUserLevel, lookup.UsersRoles,
				lookup.UsersProgramUserID, lookup.UsersProgramUserTable).
			Where(Eq(lookup.UsersID, userID), Eq(lookup.UsersIsActive, true)).
			QueryRow(ctx, q, &u.username, &u.email, &u.userLevel, &u.roles, &u.programUserID, &u.programUserTable)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user data: %w", err)
	}
	u.id = userID
	return u.context(""), nil
}
