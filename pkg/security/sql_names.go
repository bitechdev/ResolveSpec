package security

import (
	"fmt"
	"reflect"
	"regexp"
)

var validSQLIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// SQLNames defines all configurable SQL stored procedure and table names
// used by the security package. Override individual fields to remap
// to custom database objects. Use DefaultSQLNames() for baseline defaults,
// and MergeSQLNames() to apply partial overrides.
type SQLNames struct {
	// Auth procedures (DatabaseAuthenticator)
	Login         string // default: "resolvespec_login"
	Register      string // default: "resolvespec_register"
	Logout        string // default: "resolvespec_logout"
	Session       string // default: "resolvespec_session"
	SessionUpdate string // default: "resolvespec_session_update"
	RefreshToken  string // default: "resolvespec_refresh_token"

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

	// OAuth2 procedures (DatabaseAuthenticator OAuth2 methods)
	OAuthGetOrCreateUser    string // default: "resolvespec_oauth_getorcreateuser"
	OAuthCreateSession      string // default: "resolvespec_oauth_createsession"
	OAuthGetRefreshToken    string // default: "resolvespec_oauth_getrefreshtoken"
	OAuthUpdateRefreshToken string // default: "resolvespec_oauth_updaterefreshtoken"
	OAuthGetUser            string // default: "resolvespec_oauth_getuser"

}

// DefaultSQLNames returns an SQLNames with all default resolvespec_* values.
func DefaultSQLNames() *SQLNames {
	return &SQLNames{
		Login:         "resolvespec_login",
		Register:      "resolvespec_register",
		Logout:        "resolvespec_logout",
		Session:       "resolvespec_session",
		SessionUpdate: "resolvespec_session_update",
		RefreshToken:  "resolvespec_refresh_token",

		JWTLogin:  "resolvespec_jwt_login",
		JWTLogout: "resolvespec_jwt_logout",

		ColumnSecurity: "resolvespec_column_security",
		RowSecurity:    "resolvespec_row_security",

		TOTPEnable:             "resolvespec_totp_enable",
		TOTPDisable:            "resolvespec_totp_disable",
		TOTPGetStatus:          "resolvespec_totp_get_status",
		TOTPGetSecret:          "resolvespec_totp_get_secret",
		TOTPRegenerateBackup:   "resolvespec_totp_regenerate_backup_codes",
		TOTPValidateBackupCode: "resolvespec_totp_validate_backup_code",

		PasskeyStoreCredential:    "resolvespec_passkey_store_credential",
		PasskeyGetCredsByUsername: "resolvespec_passkey_get_credentials_by_username",
		PasskeyGetCredential:      "resolvespec_passkey_get_credential",
		PasskeyUpdateCounter:      "resolvespec_passkey_update_counter",
		PasskeyGetUserCredentials: "resolvespec_passkey_get_user_credentials",
		PasskeyDeleteCredential:   "resolvespec_passkey_delete_credential",
		PasskeyUpdateName:         "resolvespec_passkey_update_name",
		PasskeyLogin:              "resolvespec_passkey_login",

		OAuthGetOrCreateUser:    "resolvespec_oauth_getorcreateuser",
		OAuthCreateSession:      "resolvespec_oauth_createsession",
		OAuthGetRefreshToken:    "resolvespec_oauth_getrefreshtoken",
		OAuthUpdateRefreshToken: "resolvespec_oauth_updaterefreshtoken",
		OAuthGetUser:            "resolvespec_oauth_getuser",
	}
}

// MergeSQLNames returns a copy of base with any non-empty fields from override applied.
// If override is nil, a copy of base is returned.
func MergeSQLNames(base, override *SQLNames) *SQLNames {
	if override == nil {
		copied := *base
		return &copied
	}
	merged := *base
	if override.Login != "" {
		merged.Login = override.Login
	}
	if override.Register != "" {
		merged.Register = override.Register
	}
	if override.Logout != "" {
		merged.Logout = override.Logout
	}
	if override.Session != "" {
		merged.Session = override.Session
	}
	if override.SessionUpdate != "" {
		merged.SessionUpdate = override.SessionUpdate
	}
	if override.RefreshToken != "" {
		merged.RefreshToken = override.RefreshToken
	}
	if override.JWTLogin != "" {
		merged.JWTLogin = override.JWTLogin
	}
	if override.JWTLogout != "" {
		merged.JWTLogout = override.JWTLogout
	}
	if override.ColumnSecurity != "" {
		merged.ColumnSecurity = override.ColumnSecurity
	}
	if override.RowSecurity != "" {
		merged.RowSecurity = override.RowSecurity
	}
	if override.TOTPEnable != "" {
		merged.TOTPEnable = override.TOTPEnable
	}
	if override.TOTPDisable != "" {
		merged.TOTPDisable = override.TOTPDisable
	}
	if override.TOTPGetStatus != "" {
		merged.TOTPGetStatus = override.TOTPGetStatus
	}
	if override.TOTPGetSecret != "" {
		merged.TOTPGetSecret = override.TOTPGetSecret
	}
	if override.TOTPRegenerateBackup != "" {
		merged.TOTPRegenerateBackup = override.TOTPRegenerateBackup
	}
	if override.TOTPValidateBackupCode != "" {
		merged.TOTPValidateBackupCode = override.TOTPValidateBackupCode
	}
	if override.PasskeyStoreCredential != "" {
		merged.PasskeyStoreCredential = override.PasskeyStoreCredential
	}
	if override.PasskeyGetCredsByUsername != "" {
		merged.PasskeyGetCredsByUsername = override.PasskeyGetCredsByUsername
	}
	if override.PasskeyGetCredential != "" {
		merged.PasskeyGetCredential = override.PasskeyGetCredential
	}
	if override.PasskeyUpdateCounter != "" {
		merged.PasskeyUpdateCounter = override.PasskeyUpdateCounter
	}
	if override.PasskeyGetUserCredentials != "" {
		merged.PasskeyGetUserCredentials = override.PasskeyGetUserCredentials
	}
	if override.PasskeyDeleteCredential != "" {
		merged.PasskeyDeleteCredential = override.PasskeyDeleteCredential
	}
	if override.PasskeyUpdateName != "" {
		merged.PasskeyUpdateName = override.PasskeyUpdateName
	}
	if override.PasskeyLogin != "" {
		merged.PasskeyLogin = override.PasskeyLogin
	}
	if override.OAuthGetOrCreateUser != "" {
		merged.OAuthGetOrCreateUser = override.OAuthGetOrCreateUser
	}
	if override.OAuthCreateSession != "" {
		merged.OAuthCreateSession = override.OAuthCreateSession
	}
	if override.OAuthGetRefreshToken != "" {
		merged.OAuthGetRefreshToken = override.OAuthGetRefreshToken
	}
	if override.OAuthUpdateRefreshToken != "" {
		merged.OAuthUpdateRefreshToken = override.OAuthUpdateRefreshToken
	}
	if override.OAuthGetUser != "" {
		merged.OAuthGetUser = override.OAuthGetUser
	}
	return &merged
}

// ValidateSQLNames checks that all non-empty fields in names are valid SQL identifiers.
// Returns an error if any field contains invalid characters.
func ValidateSQLNames(names *SQLNames) error {
	v := reflect.ValueOf(names).Elem()
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		val := field.String()
		if val != "" && !validSQLIdentifier.MatchString(val) {
			return fmt.Errorf("SQLNames.%s contains invalid characters: %q", typ.Field(i).Name, val)
		}
	}
	return nil
}

// resolveSQLNames merges an optional override with defaults.
// Used by constructors that accept variadic *SQLNames.
func resolveSQLNames(override ...*SQLNames) *SQLNames {
	if len(override) > 0 && override[0] != nil {
		return MergeSQLNames(DefaultSQLNames(), override[0])
	}
	return DefaultSQLNames()
}
