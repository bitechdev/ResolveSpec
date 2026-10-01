package lookup

import "fmt"

// Dialect names understood by Config.Dialect. The dialect package (step 2) owns the
// implementations; the names are defined here so Config can be validated without it.
const (
	DialectPostgres = "postgres"
	DialectSQLite   = "sqlite"
	DialectMySQL    = "mysql"
	DialectMSSQL    = "mssql"
)

// Mode selects how a store talks to the database.
type Mode string

const (
	// ModeDefault (the zero value) resolves to ModeProcedure on Postgres and ModeDirect elsewhere.
	ModeDefault Mode = ""
	// ModeProcedure always calls the configured stored procedure; a missing procedure is an error.
	ModeProcedure Mode = "procedure"
	// ModeDirect always works on the tables through the dialect builder.
	ModeDirect Mode = "direct"
	// ModeAuto probes the procedure once per operation on Postgres (cached) and uses it when
	// present, otherwise direct. Other dialects resolve to ModeDirect.
	ModeAuto Mode = "auto"
)

func (m Mode) valid() bool {
	switch m {
	case ModeDefault, ModeProcedure, ModeDirect, ModeAuto:
		return true
	}
	return false
}

// Op names one store operation so its mode can be overridden individually.
type Op string

const (
	OpLogin         Op = "login"
	OpRegister      Op = "register"
	OpLogout        Op = "logout"
	OpSession       Op = "session"
	OpTouchSession  Op = "touch_session"
	OpRefresh       Op = "refresh"
	OpLoginAPIKey   Op = "login_api_key" //nolint:gosec // operation name, not a credential
	OpJWTLogin      Op = "jwt_login"
	OpJWTLogout     Op = "jwt_logout"
	OpResetRequest  Op = "reset_request"
	OpResetComplete Op = "reset_complete"

	OpKeyCreate   Op = "key_create"
	OpKeyList     Op = "key_list"
	OpKeyDelete   Op = "key_delete"
	OpKeyValidate Op = "key_validate"

	OpOAuthRegisterClient Op = "oauth_register_client"
	OpOAuthGetClient      Op = "oauth_get_client"
	OpOAuthSaveCode       Op = "oauth_save_code"
	OpOAuthExchangeCode   Op = "oauth_exchange_code"
	OpOAuthIntrospect     Op = "oauth_introspect"
	OpOAuthRevoke         Op = "oauth_revoke"
	OpOAuthUpdateClient   Op = "oauth_update_client"
	OpOAuthDeleteClient   Op = "oauth_delete_client"

	OpOAuthGetOrCreateUser    Op = "oauth_get_or_create_user"
	OpOAuthCreateSession      Op = "oauth_create_session"
	OpOAuthGetRefreshToken    Op = "oauth_get_refresh_token"    //nolint:gosec // operation name, not a credential
	OpOAuthUpdateRefreshToken Op = "oauth_update_refresh_token" //nolint:gosec // operation name, not a credential
	OpOAuthGetUser            Op = "oauth_get_user"

	OpOAuthSaveConsent         Op = "oauth_save_consent"
	OpOAuthGetConsent          Op = "oauth_get_consent"
	OpOAuthRevokeConsent       Op = "oauth_revoke_consent"
	OpOAuthSaveRefresh         Op = "oauth_save_refresh"           //nolint:gosec // operation name, not a credential
	OpOAuthRotateRefresh       Op = "oauth_rotate_refresh"         //nolint:gosec // operation name, not a credential
	OpOAuthPeekRefresh         Op = "oauth_peek_refresh"           //nolint:gosec // operation name, not a credential
	OpOAuthRevokeRefreshFamily Op = "oauth_revoke_refresh_family"  //nolint:gosec // operation name, not a credential
	OpOAuthRevokeRefreshByUser Op = "oauth_revoke_refresh_session" //nolint:gosec // operation name, not a credential
	OpOAuthCreateDevice        Op = "oauth_create_device"
	OpOAuthDeviceByUserCode    Op = "oauth_device_by_user_code"
	OpOAuthDeviceDecide        Op = "oauth_device_decide"
	OpOAuthDevicePoll          Op = "oauth_device_poll"
	OpOAuthSavePAR             Op = "oauth_save_par"
	OpOAuthConsumePAR          Op = "oauth_consume_par"
	OpOAuthSeenJTI             Op = "oauth_seen_jti"

	OpPasskeyStore         Op = "passkey_store"
	OpPasskeyGet           Op = "passkey_get"
	OpPasskeyUpdateCounter Op = "passkey_update_counter"
	OpPasskeyList          Op = "passkey_list"
	OpPasskeyDelete        Op = "passkey_delete"
	OpPasskeyRename        Op = "passkey_rename"
	OpPasskeyByUsername    Op = "passkey_by_username" //nolint:gosec // operation name, not a credential
	OpPasskeyLogin         Op = "passkey_login"

	OpTOTPEnable             Op = "totp_enable"
	OpTOTPDisable            Op = "totp_disable"
	OpTOTPStatus             Op = "totp_status"
	OpTOTPSecret             Op = "totp_secret"
	OpTOTPRegenerateBackup   Op = "totp_regenerate_backup"
	OpTOTPValidateBackupCode Op = "totp_validate_backup_code"

	OpColumnSecurity Op = "column_security"
	OpRowSecurity    Op = "row_security"
)

// EffectiveMode resolves the mode for one operation: a per-operation override wins over
// Config.Mode, and ModeDefault is replaced by the dialect default. The result is
// ModeProcedure, ModeDirect or ModeAuto; ModeAuto only survives on Postgres, where the
// caller must probe the procedure. dialect is the resolved dialect name.
func (c Config) EffectiveMode(op Op, dialect string) (Mode, error) {
	m := c.Mode
	if o, ok := c.Overrides[op]; ok && o != ModeDefault {
		m = o
	}
	if !m.valid() {
		return "", fmt.Errorf("lookup: invalid mode %q for %s", m, op)
	}
	pg := dialect == DialectPostgres
	switch m {
	case ModeDefault:
		if pg {
			return ModeProcedure, nil
		}
		return ModeDirect, nil
	case ModeAuto:
		if pg {
			return ModeAuto, nil
		}
		return ModeDirect, nil
	case ModeProcedure:
		if !pg {
			return "", fmt.Errorf("lookup: procedure mode for %s requires the postgres dialect, got %q", op, dialect)
		}
	}
	return m, nil
}

// AllOps lists every operation, so callers can resolve or validate modes up front.
func AllOps() []Op {
	return []Op{
		OpLogin,
		OpRegister,
		OpLogout,
		OpSession,
		OpTouchSession,
		OpRefresh,
		OpLoginAPIKey,
		OpJWTLogin,
		OpJWTLogout,
		OpResetRequest,
		OpResetComplete,
		OpKeyCreate,
		OpKeyList,
		OpKeyDelete,
		OpKeyValidate,
		OpOAuthRegisterClient,
		OpOAuthGetClient,
		OpOAuthSaveCode,
		OpOAuthExchangeCode,
		OpOAuthIntrospect,
		OpOAuthRevoke,
		OpOAuthUpdateClient,
		OpOAuthDeleteClient,
		OpOAuthGetOrCreateUser,
		OpOAuthCreateSession,
		OpOAuthGetRefreshToken,
		OpOAuthUpdateRefreshToken,
		OpOAuthGetUser,
		OpOAuthSaveConsent,
		OpOAuthGetConsent,
		OpOAuthRevokeConsent,
		OpOAuthSaveRefresh,
		OpOAuthRotateRefresh,
		OpOAuthPeekRefresh,
		OpOAuthRevokeRefreshFamily,
		OpOAuthRevokeRefreshByUser,
		OpOAuthCreateDevice,
		OpOAuthDeviceByUserCode,
		OpOAuthDeviceDecide,
		OpOAuthDevicePoll,
		OpOAuthSavePAR,
		OpOAuthConsumePAR,
		OpOAuthSeenJTI,
		OpPasskeyStore,
		OpPasskeyGet,
		OpPasskeyUpdateCounter,
		OpPasskeyList,
		OpPasskeyDelete,
		OpPasskeyRename,
		OpPasskeyByUsername,
		OpPasskeyLogin,
		OpTOTPEnable,
		OpTOTPDisable,
		OpTOTPStatus,
		OpTOTPSecret,
		OpTOTPRegenerateBackup,
		OpTOTPValidateBackupCode,
		OpColumnSecurity,
		OpRowSecurity,
	}
}
