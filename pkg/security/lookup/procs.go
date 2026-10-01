package lookup

import (
	"fmt"
	"reflect"
)

// ProcNames holds the stored procedure (function) names used by the procedure backend.
// It replaces security.SQLNames and security.KeyStoreSQLNames. Zero fields mean "default"
// when merged with DefaultProcNames.
type ProcNames struct {

	// Auth procedures (DatabaseAuthenticator)
	Login         string // default: "resolvespec_login"
	Register      string // default: "resolvespec_register"
	Logout        string // default: "resolvespec_logout"
	Session       string // default: "resolvespec_session"
	SessionUpdate string // default: "resolvespec_session_update"
	RefreshToken  string // default: "resolvespec_refresh_token"
	LoginAPIKey   string // default: "resolvespec_login_api_key"

	// JWT procedures (JWTAuthenticator)
	JWTLogin  string // default: "resolvespec_jwt_login"
	JWTLogout string // default: "resolvespec_jwt_logout"

	// Security policy procedures
	ColumnSecurity string // default: "resolvespec_column_security"
	RowSecurity    string // default: "resolvespec_row_security"

	// TOTP procedures (DatabaseTwoFactorProvider)
	TOTPEnable             string // default: "resolvespec_totp_enable"
	TOTPDisable            string // default: "resolvespec_totp_disable"
	TOTPGetStatus          string // default: "resolvespec_totp_get_status"
	TOTPGetSecret          string // default: "resolvespec_totp_get_secret"
	TOTPRegenerateBackup   string // default: "resolvespec_totp_regenerate_backup_codes"
	TOTPValidateBackupCode string // default: "resolvespec_totp_validate_backup_code"

	// Passkey procedures (DatabasePasskeyProvider)
	PasskeyStoreCredential    string // default: "resolvespec_passkey_store_credential"
	PasskeyGetCredsByUsername string // default: "resolvespec_passkey_get_credentials_by_username"
	PasskeyGetCredential      string // default: "resolvespec_passkey_get_credential"
	PasskeyUpdateCounter      string // default: "resolvespec_passkey_update_counter"
	PasskeyGetUserCredentials string // default: "resolvespec_passkey_get_user_credentials"
	PasskeyDeleteCredential   string // default: "resolvespec_passkey_delete_credential"
	PasskeyUpdateName         string // default: "resolvespec_passkey_update_name"
	PasskeyLogin              string // default: "resolvespec_passkey_login"

	// Password reset procedures (DatabaseAuthenticator)
	PasswordResetRequest  string // default: "resolvespec_password_reset_request"
	PasswordResetComplete string // default: "resolvespec_password_reset"

	// OAuth2 procedures (DatabaseAuthenticator OAuth2 methods)
	OAuthGetOrCreateUser    string // default: "resolvespec_oauth_getorcreateuser"
	OAuthCreateSession      string // default: "resolvespec_oauth_createsession"
	OAuthGetRefreshToken    string // default: "resolvespec_oauth_getrefreshtoken"
	OAuthUpdateRefreshToken string // default: "resolvespec_oauth_updaterefreshtoken"
	OAuthGetUser            string // default: "resolvespec_oauth_getuser"

	// OAuth2 server procedures (OAuthServer persistence)
	OAuthRegisterClient string // default: "resolvespec_oauth_register_client"
	OAuthGetClient      string // default: "resolvespec_oauth_get_client"
	OAuthSaveCode       string // default: "resolvespec_oauth_save_code"
	OAuthExchangeCode   string // default: "resolvespec_oauth_exchange_code"
	OAuthIntrospect     string // default: "resolvespec_oauth_introspect"
	OAuthRevoke         string // default: "resolvespec_oauth_revoke"

	// Keystore procedures (KeyStore)
	KeystoreGetUserKeys string // default: "resolvespec_keystore_get_user_keys"
	KeystoreCreateKey   string // default: "resolvespec_keystore_create_key"
	KeystoreDeleteKey   string // default: "resolvespec_keystore_delete_key"
	KeystoreValidateKey string // default: "resolvespec_keystore_validate_key"
}

// DefaultProcNames returns the default resolvespec_* procedure names.
func DefaultProcNames() ProcNames {
	return ProcNames{ //nolint:gosec // G101: false positive: identifiers, not credentials
		Login:                     "resolvespec_login",
		Register:                  "resolvespec_register",
		Logout:                    "resolvespec_logout",
		Session:                   "resolvespec_session",
		SessionUpdate:             "resolvespec_session_update",
		RefreshToken:              "resolvespec_refresh_token",
		LoginAPIKey:               "resolvespec_login_api_key",
		JWTLogin:                  "resolvespec_jwt_login",
		JWTLogout:                 "resolvespec_jwt_logout",
		ColumnSecurity:            "resolvespec_column_security",
		RowSecurity:               "resolvespec_row_security",
		TOTPEnable:                "resolvespec_totp_enable",
		TOTPDisable:               "resolvespec_totp_disable",
		TOTPGetStatus:             "resolvespec_totp_get_status",
		TOTPGetSecret:             "resolvespec_totp_get_secret",
		TOTPRegenerateBackup:      "resolvespec_totp_regenerate_backup_codes",
		TOTPValidateBackupCode:    "resolvespec_totp_validate_backup_code",
		PasskeyStoreCredential:    "resolvespec_passkey_store_credential",
		PasskeyGetCredsByUsername: "resolvespec_passkey_get_credentials_by_username",
		PasskeyGetCredential:      "resolvespec_passkey_get_credential",
		PasskeyUpdateCounter:      "resolvespec_passkey_update_counter",
		PasskeyGetUserCredentials: "resolvespec_passkey_get_user_credentials",
		PasskeyDeleteCredential:   "resolvespec_passkey_delete_credential",
		PasskeyUpdateName:         "resolvespec_passkey_update_name",
		PasskeyLogin:              "resolvespec_passkey_login",
		PasswordResetRequest:      "resolvespec_password_reset_request",
		PasswordResetComplete:     "resolvespec_password_reset",
		OAuthGetOrCreateUser:      "resolvespec_oauth_getorcreateuser",
		OAuthCreateSession:        "resolvespec_oauth_createsession",
		OAuthGetRefreshToken:      "resolvespec_oauth_getrefreshtoken",
		OAuthUpdateRefreshToken:   "resolvespec_oauth_updaterefreshtoken",
		OAuthGetUser:              "resolvespec_oauth_getuser",
		OAuthRegisterClient:       "resolvespec_oauth_register_client",
		OAuthGetClient:            "resolvespec_oauth_get_client",
		OAuthSaveCode:             "resolvespec_oauth_save_code",
		OAuthExchangeCode:         "resolvespec_oauth_exchange_code",
		OAuthIntrospect:           "resolvespec_oauth_introspect",
		OAuthRevoke:               "resolvespec_oauth_revoke",
		KeystoreGetUserKeys:       "resolvespec_keystore_get_user_keys",
		KeystoreCreateKey:         "resolvespec_keystore_create_key",
		KeystoreDeleteKey:         "resolvespec_keystore_delete_key",
		KeystoreValidateKey:       "resolvespec_keystore_validate_key",
	}
}

// Merge returns a copy of p with every non-empty field of override applied.
func (p ProcNames) Merge(override ProcNames) ProcNames {
	merged := p
	mv, ov := reflect.ValueOf(&merged).Elem(), reflect.ValueOf(override)
	for i := 0; i < ov.NumField(); i++ {
		if v := ov.Field(i).String(); v != "" {
			mv.Field(i).SetString(v)
		}
	}
	return merged
}

// Validate checks that every name is a safe (optionally schema-qualified) identifier.
func (p ProcNames) Validate() error {
	v, t := reflect.ValueOf(p), reflect.TypeOf(p)
	for i := 0; i < v.NumField(); i++ {
		if name := v.Field(i).String(); !validQualifiedIdent(name) {
			return fmt.Errorf("lookup: invalid procedure name %q for %s", name, t.Field(i).Name)
		}
	}
	return nil
}
