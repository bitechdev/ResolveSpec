package backends

import (
	"context"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Routers: each store method asks the chooser which backend serves that operation.

type authRouter struct {
	c            *chooser
	proc, direct lookup.AuthStore
}

var _ lookup.AuthStore = (*authRouter)(nil)

func (r *authRouter) Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpLogin, r.c.procs.Login, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Login(ctx, req)
}

func (r *authRouter) Register(ctx context.Context, req sectypes.RegisterRequest) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpRegister, r.c.procs.Register, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Register(ctx, req)
}

func (r *authRouter) Logout(ctx context.Context, req sectypes.LogoutRequest) error {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpLogout, r.c.procs.Logout, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Logout(ctx, req)
}

func (r *authRouter) Session(ctx context.Context, token, reference string) (*sectypes.UserContext, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpSession, r.c.procs.Session, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Session(ctx, token, reference)
}

func (r *authRouter) TouchSession(ctx context.Context, token string, user *sectypes.UserContext) error {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpTouchSession, r.c.procs.SessionUpdate, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.TouchSession(ctx, token, user)
}

func (r *authRouter) Refresh(ctx context.Context, refreshToken string) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpRefresh, r.c.procs.RefreshToken, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Refresh(ctx, refreshToken)
}

func (r *authRouter) LoginAPIKey(ctx context.Context, rawKey string, claims map[string]any) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpLoginAPIKey, r.c.procs.LoginAPIKey, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.LoginAPIKey(ctx, rawKey, claims)
}

func (r *authRouter) JWTLogin(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpJWTLogin, r.c.procs.JWTLogin, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.JWTLogin(ctx, req)
}

func (r *authRouter) JWTLogout(ctx context.Context, req sectypes.LogoutRequest) error {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpJWTLogout, r.c.procs.JWTLogout, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.JWTLogout(ctx, req)
}

func (r *authRouter) ResetRequest(ctx context.Context, req sectypes.PasswordResetRequest) (*sectypes.PasswordResetResponse, error) {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpResetRequest, r.c.procs.PasswordResetRequest, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.ResetRequest(ctx, req)
}

func (r *authRouter) ResetComplete(ctx context.Context, req sectypes.PasswordResetCompleteRequest) error {
	st, err := pick[lookup.AuthStore](r.c, ctx, lookup.OpResetComplete, r.c.procs.PasswordResetComplete, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.ResetComplete(ctx, req)
}

type keysRouter struct {
	c            *chooser
	proc, direct lookup.KeyStore
}

var _ lookup.KeyStore = (*keysRouter)(nil)

func (r *keysRouter) Create(ctx context.Context, req sectypes.CreateKeyRequest, keyHash string) (*sectypes.UserKey, error) {
	st, err := pick[lookup.KeyStore](r.c, ctx, lookup.OpKeyCreate, r.c.procs.KeystoreCreateKey, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Create(ctx, req, keyHash)
}

func (r *keysRouter) List(ctx context.Context, userID int, keyType sectypes.KeyType) ([]sectypes.UserKey, error) {
	st, err := pick[lookup.KeyStore](r.c, ctx, lookup.OpKeyList, r.c.procs.KeystoreGetUserKeys, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, userID, keyType)
}

func (r *keysRouter) Delete(ctx context.Context, userID int, keyID int64) (string, error) {
	st, err := pick[lookup.KeyStore](r.c, ctx, lookup.OpKeyDelete, r.c.procs.KeystoreDeleteKey, r.proc, r.direct)
	if err != nil {
		return "", err
	}
	return st.Delete(ctx, userID, keyID)
}

func (r *keysRouter) Validate(ctx context.Context, keyHash string, keyType sectypes.KeyType) (*sectypes.UserKey, error) {
	st, err := pick[lookup.KeyStore](r.c, ctx, lookup.OpKeyValidate, r.c.procs.KeystoreValidateKey, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Validate(ctx, keyHash, keyType)
}

type oauthClientRouter struct {
	c            *chooser
	proc, direct lookup.OAuthClientStore
}

var _ lookup.OAuthClientStore = (*oauthClientRouter)(nil)

func (r *oauthClientRouter) RegisterClient(ctx context.Context, client *sectypes.OAuthServerClient) (*sectypes.OAuthServerClient, error) {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthRegisterClient, r.c.procs.OAuthRegisterClient, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.RegisterClient(ctx, client)
}

func (r *oauthClientRouter) GetClient(ctx context.Context, clientID string) (*sectypes.OAuthServerClient, error) {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthGetClient, r.c.procs.OAuthGetClient, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.GetClient(ctx, clientID)
}

func (r *oauthClientRouter) SaveCode(ctx context.Context, code *sectypes.OAuthCode) error {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthSaveCode, r.c.procs.OAuthSaveCode, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.SaveCode(ctx, code)
}

func (r *oauthClientRouter) ExchangeCode(ctx context.Context, code string) (*sectypes.OAuthCode, error) {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthExchangeCode, r.c.procs.OAuthExchangeCode, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.ExchangeCode(ctx, code)
}

func (r *oauthClientRouter) Introspect(ctx context.Context, token string) (*sectypes.OAuthTokenInfo, error) {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthIntrospect, r.c.procs.OAuthIntrospect, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Introspect(ctx, token)
}

func (r *oauthClientRouter) Revoke(ctx context.Context, token string) error {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthRevoke, r.c.procs.OAuthRevoke, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Revoke(ctx, token)
}

func (r *oauthClientRouter) UpdateClient(ctx context.Context, client *sectypes.OAuthServerClient) error {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthUpdateClient, r.c.procs.OAuthUpdateClient, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.UpdateClient(ctx, client)
}

func (r *oauthClientRouter) DeleteClient(ctx context.Context, clientID string) error {
	st, err := pick[lookup.OAuthClientStore](r.c, ctx, lookup.OpOAuthDeleteClient, r.c.procs.OAuthDeleteClient, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.DeleteClient(ctx, clientID)
}

type oauthUserRouter struct {
	c            *chooser
	proc, direct lookup.OAuthUserStore
}

var _ lookup.OAuthUserStore = (*oauthUserRouter)(nil)

func (r *oauthUserRouter) GetOrCreateUser(ctx context.Context, user *sectypes.UserContext, provider string) (int, error) {
	st, err := pick[lookup.OAuthUserStore](r.c, ctx, lookup.OpOAuthGetOrCreateUser, r.c.procs.OAuthGetOrCreateUser, r.proc, r.direct)
	if err != nil {
		return 0, err
	}
	return st.GetOrCreateUser(ctx, user, provider)
}

func (r *oauthUserRouter) CreateSession(ctx context.Context, session lookup.OAuthSession) error {
	st, err := pick[lookup.OAuthUserStore](r.c, ctx, lookup.OpOAuthCreateSession, r.c.procs.OAuthCreateSession, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.CreateSession(ctx, session)
}

func (r *oauthUserRouter) GetByRefreshToken(ctx context.Context, refreshToken string) (*lookup.OAuthRefreshSession, error) {
	st, err := pick[lookup.OAuthUserStore](r.c, ctx, lookup.OpOAuthGetRefreshToken, r.c.procs.OAuthGetRefreshToken, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.GetByRefreshToken(ctx, refreshToken)
}

func (r *oauthUserRouter) UpdateRefreshToken(ctx context.Context, userID int, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken string, expiresAt time.Time) error {
	st, err := pick[lookup.OAuthUserStore](r.c, ctx, lookup.OpOAuthUpdateRefreshToken, r.c.procs.OAuthUpdateRefreshToken, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.UpdateRefreshToken(ctx, userID, oldRefreshToken, newSessionToken, newAccessToken, newRefreshToken, expiresAt)
}

func (r *oauthUserRouter) GetUser(ctx context.Context, userID int) (*sectypes.UserContext, error) {
	st, err := pick[lookup.OAuthUserStore](r.c, ctx, lookup.OpOAuthGetUser, r.c.procs.OAuthGetUser, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.GetUser(ctx, userID)
}

type passkeyRouter struct {
	c            *chooser
	proc, direct lookup.PasskeyStore
}

var _ lookup.PasskeyStore = (*passkeyRouter)(nil)

func (r *passkeyRouter) Store(ctx context.Context, rec lookup.PasskeyCredentialRecord) (int64, error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyStore, r.c.procs.PasskeyStoreCredential, r.proc, r.direct)
	if err != nil {
		return 0, err
	}
	return st.Store(ctx, rec)
}

func (r *passkeyRouter) Get(ctx context.Context, credentialID string) (userID int, signCount uint32, err error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyGet, r.c.procs.PasskeyGetCredential, r.proc, r.direct)
	if err != nil {
		return 0, 0, err
	}
	return st.Get(ctx, credentialID)
}

func (r *passkeyRouter) UpdateCounter(ctx context.Context, credentialID string, newCounter uint32) (bool, error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyUpdateCounter, r.c.procs.PasskeyUpdateCounter, r.proc, r.direct)
	if err != nil {
		return false, err
	}
	return st.UpdateCounter(ctx, credentialID, newCounter)
}

func (r *passkeyRouter) List(ctx context.Context, userID int) ([]sectypes.PasskeyCredential, error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyList, r.c.procs.PasskeyGetUserCredentials, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, userID)
}

func (r *passkeyRouter) Delete(ctx context.Context, userID int, credentialID string) error {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyDelete, r.c.procs.PasskeyDeleteCredential, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Delete(ctx, userID, credentialID)
}

func (r *passkeyRouter) Rename(ctx context.Context, userID int, credentialID, name string) error {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyRename, r.c.procs.PasskeyUpdateName, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Rename(ctx, userID, credentialID, name)
}

func (r *passkeyRouter) ByUsername(ctx context.Context, username string) (int, []lookup.PasskeyCredentialRef, error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyByUsername, r.c.procs.PasskeyGetCredsByUsername, r.proc, r.direct)
	if err != nil {
		return 0, nil, err
	}
	return st.ByUsername(ctx, username)
}

func (r *passkeyRouter) Login(ctx context.Context, userID int, claims map[string]any) (*sectypes.LoginResponse, error) {
	st, err := pick[lookup.PasskeyStore](r.c, ctx, lookup.OpPasskeyLogin, r.c.procs.PasskeyLogin, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.Login(ctx, userID, claims)
}

type totpRouter struct {
	c            *chooser
	proc, direct lookup.TOTPStore
}

var _ lookup.TOTPStore = (*totpRouter)(nil)

func (r *totpRouter) Enable(ctx context.Context, userID int, secret string, hashedCodes []string) error {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPEnable, r.c.procs.TOTPEnable, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Enable(ctx, userID, secret, hashedCodes)
}

func (r *totpRouter) Disable(ctx context.Context, userID int) error {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPDisable, r.c.procs.TOTPDisable, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.Disable(ctx, userID)
}

func (r *totpRouter) Status(ctx context.Context, userID int) (bool, error) {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPStatus, r.c.procs.TOTPGetStatus, r.proc, r.direct)
	if err != nil {
		return false, err
	}
	return st.Status(ctx, userID)
}

func (r *totpRouter) Secret(ctx context.Context, userID int) (string, error) {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPSecret, r.c.procs.TOTPGetSecret, r.proc, r.direct)
	if err != nil {
		return "", err
	}
	return st.Secret(ctx, userID)
}

func (r *totpRouter) RegenerateBackupCodes(ctx context.Context, userID int, hashedCodes []string) error {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPRegenerateBackup, r.c.procs.TOTPRegenerateBackup, r.proc, r.direct)
	if err != nil {
		return err
	}
	return st.RegenerateBackupCodes(ctx, userID, hashedCodes)
}

func (r *totpRouter) ValidateBackupCode(ctx context.Context, userID int, codeHash string) (bool, error) {
	st, err := pick[lookup.TOTPStore](r.c, ctx, lookup.OpTOTPValidateBackupCode, r.c.procs.TOTPValidateBackupCode, r.proc, r.direct)
	if err != nil {
		return false, err
	}
	return st.ValidateBackupCode(ctx, userID, codeHash)
}

type policyRouter struct {
	c            *chooser
	proc, direct lookup.PolicyStore
}

var _ lookup.PolicyStore = (*policyRouter)(nil)

func (r *policyRouter) ColumnSecurity(ctx context.Context, userID int, schema, table string) ([]sectypes.ColumnSecurity, error) {
	st, err := pick[lookup.PolicyStore](r.c, ctx, lookup.OpColumnSecurity, r.c.procs.ColumnSecurity, r.proc, r.direct)
	if err != nil {
		return nil, err
	}
	return st.ColumnSecurity(ctx, userID, schema, table)
}

func (r *policyRouter) RowSecurity(ctx context.Context, userRef any, schema, table string) (sectypes.RowSecurity, error) {
	st, err := pick[lookup.PolicyStore](r.c, ctx, lookup.OpRowSecurity, r.c.procs.RowSecurity, r.proc, r.direct)
	if err != nil {
		return sectypes.RowSecurity{}, err
	}
	return st.RowSecurity(ctx, userRef, schema, table)
}

type oauthGrantRouter struct {
	c            *chooser
	proc, direct lookup.OAuthGrantStore
}

var _ lookup.OAuthGrantStore = (*oauthGrantRouter)(nil)

func (r *oauthGrantRouter) pick(ctx context.Context, op lookup.Op, proc string) (lookup.OAuthGrantStore, error) {
	return pick[lookup.OAuthGrantStore](r.c, ctx, op, proc, r.proc, r.direct)
}

func (r *oauthGrantRouter) SaveConsent(ctx context.Context, c lookup.Consent) error {
	st, err := r.pick(ctx, lookup.OpOAuthSaveConsent, r.c.procs.OAuthSaveConsent)
	if err != nil {
		return err
	}
	return st.SaveConsent(ctx, c)
}

func (r *oauthGrantRouter) GetConsent(ctx context.Context, userID int, clientID string) (*lookup.Consent, error) {
	st, err := r.pick(ctx, lookup.OpOAuthGetConsent, r.c.procs.OAuthGetConsent)
	if err != nil {
		return nil, err
	}
	return st.GetConsent(ctx, userID, clientID)
}

func (r *oauthGrantRouter) RevokeConsent(ctx context.Context, userID int, clientID string) error {
	st, err := r.pick(ctx, lookup.OpOAuthRevokeConsent, r.c.procs.OAuthRevokeConsent)
	if err != nil {
		return err
	}
	return st.RevokeConsent(ctx, userID, clientID)
}

func (r *oauthGrantRouter) SaveRefresh(ctx context.Context, t lookup.RefreshToken) error {
	st, err := r.pick(ctx, lookup.OpOAuthSaveRefresh, r.c.procs.OAuthSaveRefresh)
	if err != nil {
		return err
	}
	return st.SaveRefresh(ctx, t)
}

func (r *oauthGrantRouter) RotateRefresh(ctx context.Context, oldHash string, next lookup.RefreshToken) (*lookup.RefreshToken, error) {
	st, err := r.pick(ctx, lookup.OpOAuthRotateRefresh, r.c.procs.OAuthRotateRefresh)
	if err != nil {
		return nil, err
	}
	return st.RotateRefresh(ctx, oldHash, next)
}

func (r *oauthGrantRouter) PeekRefresh(ctx context.Context, hash string) (*lookup.RefreshToken, error) {
	st, err := r.pick(ctx, lookup.OpOAuthPeekRefresh, r.c.procs.OAuthPeekRefresh)
	if err != nil {
		return nil, err
	}
	return st.PeekRefresh(ctx, hash)
}

func (r *oauthGrantRouter) RevokeRefreshFamily(ctx context.Context, familyID string) error {
	st, err := r.pick(ctx, lookup.OpOAuthRevokeRefreshFamily, r.c.procs.OAuthRevokeRefreshFamily)
	if err != nil {
		return err
	}
	return st.RevokeRefreshFamily(ctx, familyID)
}

func (r *oauthGrantRouter) RevokeRefreshBySession(ctx context.Context, sessionToken string) error {
	st, err := r.pick(ctx, lookup.OpOAuthRevokeRefreshByUser, r.c.procs.OAuthRevokeRefreshByUser)
	if err != nil {
		return err
	}
	return st.RevokeRefreshBySession(ctx, sessionToken)
}

func (r *oauthGrantRouter) CreateDevice(ctx context.Context, d lookup.DeviceCode) error {
	st, err := r.pick(ctx, lookup.OpOAuthCreateDevice, r.c.procs.OAuthCreateDevice)
	if err != nil {
		return err
	}
	return st.CreateDevice(ctx, d)
}

func (r *oauthGrantRouter) DeviceByUserCode(ctx context.Context, userCode string) (*lookup.DeviceCode, error) {
	st, err := r.pick(ctx, lookup.OpOAuthDeviceByUserCode, r.c.procs.OAuthDeviceByUserCode)
	if err != nil {
		return nil, err
	}
	return st.DeviceByUserCode(ctx, userCode)
}

func (r *oauthGrantRouter) DeviceDecide(ctx context.Context, userCode string, approve bool, userID int, sessionToken string) error {
	st, err := r.pick(ctx, lookup.OpOAuthDeviceDecide, r.c.procs.OAuthDeviceDecide)
	if err != nil {
		return err
	}
	return st.DeviceDecide(ctx, userCode, approve, userID, sessionToken)
}

func (r *oauthGrantRouter) DevicePoll(ctx context.Context, deviceHash string) (*lookup.DeviceCode, error) {
	st, err := r.pick(ctx, lookup.OpOAuthDevicePoll, r.c.procs.OAuthDevicePoll)
	if err != nil {
		return nil, err
	}
	return st.DevicePoll(ctx, deviceHash)
}

func (r *oauthGrantRouter) SavePushedRequest(ctx context.Context, req lookup.PushedRequest) error {
	st, err := r.pick(ctx, lookup.OpOAuthSavePAR, r.c.procs.OAuthSavePAR)
	if err != nil {
		return err
	}
	return st.SavePushedRequest(ctx, req)
}

func (r *oauthGrantRouter) ConsumePushedRequest(ctx context.Context, requestURI string) (*lookup.PushedRequest, error) {
	st, err := r.pick(ctx, lookup.OpOAuthConsumePAR, r.c.procs.OAuthConsumePAR)
	if err != nil {
		return nil, err
	}
	return st.ConsumePushedRequest(ctx, requestURI)
}

func (r *oauthGrantRouter) SeenJTI(ctx context.Context, key string, expires time.Time) (bool, error) {
	st, err := r.pick(ctx, lookup.OpOAuthSeenJTI, r.c.procs.OAuthSeenJTI)
	if err != nil {
		return false, err
	}
	return st.SeenJTI(ctx, key, expires)
}
