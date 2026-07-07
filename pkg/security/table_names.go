package security

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrDirectModeUnsupported is returned by Direct-mode operations that have no
// portable equivalent because they depend on an external schema this package
// does not own (e.g. core.secaccess / core.hub_link for column/row security).
// Callers needing that functionality must run against Postgres with the
// resolvespec_column_security / resolvespec_row_security stored procedures
// installed (ModeProcedure or ModeAuto with the procedures present).
var ErrDirectModeUnsupported = errors.New("direct mode does not support column/row security; requires the resolvespec_column_security/resolvespec_row_security stored procedures")

// TableNames defines all configurable table names used by Direct-mode SQL
// in the security package. Override individual fields to remap to custom
// table names. Use DefaultTableNames() for baseline defaults, and
// MergeTableNames() to apply partial overrides.
type TableNames struct {
	Users                  string // default: "users"
	UserSessions           string // default: "user_sessions"
	TokenBlacklist         string // default: "token_blacklist"
	UserTOTPBackupCodes    string // default: "user_totp_backup_codes"
	UserPasskeyCredentials string // default: "user_passkey_credentials"
	UserPasswordResets     string // default: "user_password_resets"
	OAuthClients           string // default: "oauth_clients"
	OAuthCodes             string // default: "oauth_codes"
}

// DefaultTableNames returns a TableNames with all default table names.
func DefaultTableNames() *TableNames {
	return &TableNames{
		Users:                  "users",
		UserSessions:           "user_sessions",
		TokenBlacklist:         "token_blacklist",
		UserTOTPBackupCodes:    "user_totp_backup_codes",
		UserPasskeyCredentials: "user_passkey_credentials",
		UserPasswordResets:     "user_password_resets",
		OAuthClients:           "oauth_clients",
		OAuthCodes:             "oauth_codes",
	}
}

// MergeTableNames returns a copy of base with any non-empty fields from override applied.
// If override is nil, a copy of base is returned.
func MergeTableNames(base, override *TableNames) *TableNames {
	if override == nil {
		copied := *base
		return &copied
	}
	merged := *base
	if override.Users != "" {
		merged.Users = override.Users
	}
	if override.UserSessions != "" {
		merged.UserSessions = override.UserSessions
	}
	if override.TokenBlacklist != "" {
		merged.TokenBlacklist = override.TokenBlacklist
	}
	if override.UserTOTPBackupCodes != "" {
		merged.UserTOTPBackupCodes = override.UserTOTPBackupCodes
	}
	if override.UserPasskeyCredentials != "" {
		merged.UserPasskeyCredentials = override.UserPasskeyCredentials
	}
	if override.UserPasswordResets != "" {
		merged.UserPasswordResets = override.UserPasswordResets
	}
	if override.OAuthClients != "" {
		merged.OAuthClients = override.OAuthClients
	}
	if override.OAuthCodes != "" {
		merged.OAuthCodes = override.OAuthCodes
	}
	return &merged
}

// ValidateTableNames checks that all non-empty fields in names are valid SQL identifiers.
func ValidateTableNames(names *TableNames) error {
	v := reflect.ValueOf(names).Elem()
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		val := field.String()
		if val != "" && !validSQLIdentifier.MatchString(val) {
			return fmt.Errorf("TableNames.%s contains invalid characters: %q", typ.Field(i).Name, val)
		}
	}
	return nil
}

// resolveTableNames merges an optional override with defaults.
func resolveTableNames(override *TableNames) *TableNames {
	return MergeTableNames(DefaultTableNames(), override)
}
