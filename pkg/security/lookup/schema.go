package lookup

import (
	"fmt"
	"regexp"
	"sort"
)

var (
	identRe          = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	qualifiedIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)
)

func validIdent(s string) bool          { return identRe.MatchString(s) }
func validQualifiedIdent(s string) bool { return qualifiedIdentRe.MatchString(s) }

// Entity identifies one table the direct backend reads or writes.
type Entity string

const (
	EntityUsers                  Entity = "users"
	EntityUserSessions           Entity = "user_sessions"
	EntityTokenBlacklist         Entity = "token_blacklist"
	EntityUserTOTPBackupCodes    Entity = "user_totp_backup_codes"
	EntityUserPasskeyCredentials Entity = "user_passkey_credentials" //nolint:gosec // table name, not a credential
	EntityUserPasswordResets     Entity = "user_password_resets"
	EntityOAuthClients           Entity = "oauth_clients"
	EntityOAuthCodes             Entity = "oauth_codes"
	EntityUserKeys               Entity = "user_keys"
	EntitySecGroupMembers        Entity = "sec_group_members"
	EntitySecColumnRules         Entity = "sec_column_rules"
	EntitySecRowRules            Entity = "sec_row_rules"
)

// Column is a typed key naming one logical column of an entity. The physical column
// name is looked up in the Schema, so every column of every entity is configurable.
type Column struct {
	Entity Entity
	Name   string
}

func (c Column) String() string { return string(c.Entity) + "." + c.Name }

func col(e Entity, name string) Column { return Column{Entity: e, Name: name} }

// Logical columns. The names are the default physical column names.
var (
	UsersID               = col(EntityUsers, "id")
	UsersUsername         = col(EntityUsers, "username")
	UsersEmail            = col(EntityUsers, "email")
	UsersPassword         = col(EntityUsers, "password")
	UsersUserLevel        = col(EntityUsers, "user_level")
	UsersRoles            = col(EntityUsers, "roles")
	UsersIsActive         = col(EntityUsers, "is_active")
	UsersCreatedAt        = col(EntityUsers, "created_at")
	UsersUpdatedAt        = col(EntityUsers, "updated_at")
	UsersLastLoginAt      = col(EntityUsers, "last_login_at")
	UsersProgramUserID    = col(EntityUsers, "program_user_id")
	UsersProgramUserTable = col(EntityUsers, "program_user_table")
	UsersRemoteID         = col(EntityUsers, "remote_id")
	UsersAuthProvider     = col(EntityUsers, "auth_provider")
	UsersTOTPSecret       = col(EntityUsers, "totp_secret")
	UsersTOTPEnabled      = col(EntityUsers, "totp_enabled")
	UsersTOTPEnabledAt    = col(EntityUsers, "totp_enabled_at")

	SessionsID             = col(EntityUserSessions, "id")
	SessionsToken          = col(EntityUserSessions, "session_token")
	SessionsUserID         = col(EntityUserSessions, "user_id")
	SessionsExpiresAt      = col(EntityUserSessions, "expires_at")
	SessionsCreatedAt      = col(EntityUserSessions, "created_at")
	SessionsLastActivityAt = col(EntityUserSessions, "last_activity_at")
	SessionsIPAddress      = col(EntityUserSessions, "ip_address")
	SessionsUserAgent      = col(EntityUserSessions, "user_agent")
	SessionsAccessToken    = col(EntityUserSessions, "access_token")
	SessionsRefreshToken   = col(EntityUserSessions, "refresh_token")
	SessionsTokenType      = col(EntityUserSessions, "token_type")
	SessionsAuthProvider   = col(EntityUserSessions, "auth_provider")

	BlacklistID        = col(EntityTokenBlacklist, "id")
	BlacklistToken     = col(EntityTokenBlacklist, "token")
	BlacklistUserID    = col(EntityTokenBlacklist, "user_id")
	BlacklistExpiresAt = col(EntityTokenBlacklist, "expires_at")
	BlacklistCreatedAt = col(EntityTokenBlacklist, "created_at")

	BackupCodesID        = col(EntityUserTOTPBackupCodes, "id")
	BackupCodesUserID    = col(EntityUserTOTPBackupCodes, "user_id")
	BackupCodesCodeHash  = col(EntityUserTOTPBackupCodes, "code_hash")
	BackupCodesUsed      = col(EntityUserTOTPBackupCodes, "used")
	BackupCodesUsedAt    = col(EntityUserTOTPBackupCodes, "used_at")
	BackupCodesCreatedAt = col(EntityUserTOTPBackupCodes, "created_at")

	PasskeyID              = col(EntityUserPasskeyCredentials, "id")
	PasskeyUserID          = col(EntityUserPasskeyCredentials, "user_id")
	PasskeyCredentialID    = col(EntityUserPasskeyCredentials, "credential_id")
	PasskeyPublicKey       = col(EntityUserPasskeyCredentials, "public_key")
	PasskeyAttestationType = col(EntityUserPasskeyCredentials, "attestation_type")
	PasskeyAAGUID          = col(EntityUserPasskeyCredentials, "aaguid")
	PasskeySignCount       = col(EntityUserPasskeyCredentials, "sign_count")
	PasskeyCloneWarning    = col(EntityUserPasskeyCredentials, "clone_warning")
	PasskeyTransports      = col(EntityUserPasskeyCredentials, "transports")
	PasskeyBackupEligible  = col(EntityUserPasskeyCredentials, "backup_eligible")
	PasskeyBackupState     = col(EntityUserPasskeyCredentials, "backup_state")
	PasskeyName            = col(EntityUserPasskeyCredentials, "name")
	PasskeyCreatedAt       = col(EntityUserPasskeyCredentials, "created_at")
	PasskeyLastUsedAt      = col(EntityUserPasskeyCredentials, "last_used_at")

	ResetsID        = col(EntityUserPasswordResets, "id")
	ResetsUserID    = col(EntityUserPasswordResets, "user_id")
	ResetsTokenHash = col(EntityUserPasswordResets, "token_hash")
	ResetsExpiresAt = col(EntityUserPasswordResets, "expires_at")
	ResetsCreatedAt = col(EntityUserPasswordResets, "created_at")
	ResetsUsed      = col(EntityUserPasswordResets, "used")
	ResetsUsedAt    = col(EntityUserPasswordResets, "used_at")

	OAuthClientsID                      = col(EntityOAuthClients, "id")
	OAuthClientsClientID                = col(EntityOAuthClients, "client_id")
	OAuthClientsRedirectURIs            = col(EntityOAuthClients, "redirect_uris")
	OAuthClientsClientName              = col(EntityOAuthClients, "client_name")
	OAuthClientsGrantTypes              = col(EntityOAuthClients, "grant_types")
	OAuthClientsAllowedScopes           = col(EntityOAuthClients, "allowed_scopes")
	OAuthClientsClientSecretHash        = col(EntityOAuthClients, "client_secret_hash")
	OAuthClientsTokenEndpointAuthMethod = col(EntityOAuthClients, "token_endpoint_auth_method")
	OAuthClientsIsActive                = col(EntityOAuthClients, "is_active")
	OAuthClientsCreatedAt               = col(EntityOAuthClients, "created_at")

	OAuthCodesID                  = col(EntityOAuthCodes, "id")
	OAuthCodesCode                = col(EntityOAuthCodes, "code")
	OAuthCodesClientID            = col(EntityOAuthCodes, "client_id")
	OAuthCodesRedirectURI         = col(EntityOAuthCodes, "redirect_uri")
	OAuthCodesClientState         = col(EntityOAuthCodes, "client_state")
	OAuthCodesCodeChallenge       = col(EntityOAuthCodes, "code_challenge")
	OAuthCodesCodeChallengeMethod = col(EntityOAuthCodes, "code_challenge_method")
	OAuthCodesSessionToken        = col(EntityOAuthCodes, "session_token")
	OAuthCodesRefreshToken        = col(EntityOAuthCodes, "refresh_token")
	OAuthCodesScopes              = col(EntityOAuthCodes, "scopes")
	OAuthCodesExpiresAt           = col(EntityOAuthCodes, "expires_at")
	OAuthCodesCreatedAt           = col(EntityOAuthCodes, "created_at")

	KeysID         = col(EntityUserKeys, "id")
	KeysUserID     = col(EntityUserKeys, "user_id")
	KeysKeyType    = col(EntityUserKeys, "key_type")
	KeysKeyHash    = col(EntityUserKeys, "key_hash")
	KeysName       = col(EntityUserKeys, "name")
	KeysScopes     = col(EntityUserKeys, "scopes")
	KeysMeta       = col(EntityUserKeys, "meta")
	KeysExpiresAt  = col(EntityUserKeys, "expires_at")
	KeysCreatedAt  = col(EntityUserKeys, "created_at")
	KeysLastUsedAt = col(EntityUserKeys, "last_used_at")
	KeysIsActive   = col(EntityUserKeys, "is_active")

	GroupMembersGroupID = col(EntitySecGroupMembers, "group_id")
	GroupMembersUserID  = col(EntitySecGroupMembers, "user_id")

	ColRulesID           = col(EntitySecColumnRules, "id")
	ColRulesUserID       = col(EntitySecColumnRules, "user_id")
	ColRulesGroupID      = col(EntitySecColumnRules, "group_id")
	ColRulesSchemaName   = col(EntitySecColumnRules, "schema_name")
	ColRulesTableName    = col(EntitySecColumnRules, "table_name")
	ColRulesColumnPath   = col(EntitySecColumnRules, "column_path")
	ColRulesAccessType   = col(EntitySecColumnRules, "access_type")
	ColRulesMaskStart    = col(EntitySecColumnRules, "mask_start")
	ColRulesMaskEnd      = col(EntitySecColumnRules, "mask_end")
	ColRulesMaskInvert   = col(EntitySecColumnRules, "mask_invert")
	ColRulesMaskChar     = col(EntitySecColumnRules, "mask_char")
	ColRulesExtraFilters = col(EntitySecColumnRules, "extra_filters")
	ColRulesIsActive     = col(EntitySecColumnRules, "is_active")

	RowRulesID         = col(EntitySecRowRules, "id")
	RowRulesUserID     = col(EntitySecRowRules, "user_id")
	RowRulesGroupID    = col(EntitySecRowRules, "group_id")
	RowRulesSchemaName = col(EntitySecRowRules, "schema_name")
	RowRulesTableName  = col(EntitySecRowRules, "table_name")
	RowRulesTemplate   = col(EntitySecRowRules, "template")
	RowRulesHasBlock   = col(EntitySecRowRules, "has_block")
	RowRulesIsActive   = col(EntitySecRowRules, "is_active")
)

// allColumns lists every logical column; it defines the default schema.
var allColumns = []Column{
	UsersID, UsersUsername, UsersEmail, UsersPassword, UsersUserLevel, UsersRoles, UsersIsActive,
	UsersCreatedAt, UsersUpdatedAt, UsersLastLoginAt, UsersProgramUserID, UsersProgramUserTable,
	UsersRemoteID, UsersAuthProvider, UsersTOTPSecret, UsersTOTPEnabled, UsersTOTPEnabledAt,
	SessionsID, SessionsToken, SessionsUserID, SessionsExpiresAt, SessionsCreatedAt, SessionsLastActivityAt,
	SessionsIPAddress, SessionsUserAgent, SessionsAccessToken, SessionsRefreshToken, SessionsTokenType, SessionsAuthProvider,
	BlacklistID, BlacklistToken, BlacklistUserID, BlacklistExpiresAt, BlacklistCreatedAt,
	BackupCodesID, BackupCodesUserID, BackupCodesCodeHash, BackupCodesUsed, BackupCodesUsedAt, BackupCodesCreatedAt,
	PasskeyID, PasskeyUserID, PasskeyCredentialID, PasskeyPublicKey, PasskeyAttestationType, PasskeyAAGUID,
	PasskeySignCount, PasskeyCloneWarning, PasskeyTransports, PasskeyBackupEligible, PasskeyBackupState,
	PasskeyName, PasskeyCreatedAt, PasskeyLastUsedAt,
	ResetsID, ResetsUserID, ResetsTokenHash, ResetsExpiresAt, ResetsCreatedAt, ResetsUsed, ResetsUsedAt,
	OAuthClientsID, OAuthClientsClientID, OAuthClientsRedirectURIs, OAuthClientsClientName, OAuthClientsGrantTypes,
	OAuthClientsAllowedScopes, OAuthClientsClientSecretHash, OAuthClientsTokenEndpointAuthMethod,
	OAuthClientsIsActive, OAuthClientsCreatedAt,
	OAuthCodesID, OAuthCodesCode, OAuthCodesClientID, OAuthCodesRedirectURI, OAuthCodesClientState,
	OAuthCodesCodeChallenge, OAuthCodesCodeChallengeMethod, OAuthCodesSessionToken, OAuthCodesRefreshToken,
	OAuthCodesScopes, OAuthCodesExpiresAt, OAuthCodesCreatedAt,
	KeysID, KeysUserID, KeysKeyType, KeysKeyHash, KeysName, KeysScopes, KeysMeta, KeysExpiresAt,
	KeysCreatedAt, KeysLastUsedAt, KeysIsActive,
	GroupMembersGroupID, GroupMembersUserID,
	ColRulesID, ColRulesUserID, ColRulesGroupID, ColRulesSchemaName, ColRulesTableName, ColRulesColumnPath,
	ColRulesAccessType, ColRulesMaskStart, ColRulesMaskEnd, ColRulesMaskInvert, ColRulesMaskChar,
	ColRulesExtraFilters, ColRulesIsActive,
	RowRulesID, RowRulesUserID, RowRulesGroupID, RowRulesSchemaName, RowRulesTableName, RowRulesTemplate,
	RowRulesHasBlock, RowRulesIsActive,
}

// Table maps one entity to a physical table and its columns.
type Table struct {
	// Schema optionally qualifies the table (schema.table). Empty = unqualified.
	Schema string
	// Name is the physical table name. Empty = default (the entity name).
	Name string
	// Columns maps logical column name -> physical column name. Missing = default.
	Columns map[string]string
}

// Schema maps every entity to its physical table and columns. The zero value is
// valid and means "all defaults"; use DefaultSchema for the explicit baseline.
type Schema map[Entity]Table

// DefaultSchema returns the baseline schema: every entity and column under its default name.
func DefaultSchema() Schema {
	s := Schema{}
	for _, c := range allColumns {
		t, ok := s[c.Entity]
		if !ok {
			t = Table{Name: string(c.Entity), Columns: map[string]string{}}
		}
		t.Columns[c.Name] = c.Name
		s[c.Entity] = t
	}
	return s
}

// Merge returns a copy of s with every non-empty field of override applied.
// Unknown entities or columns in override are kept so Validate can report them.
func (s Schema) Merge(override Schema) Schema {
	merged := Schema{}
	for e, t := range s {
		merged[e] = cloneTable(t)
	}
	for e, ot := range override {
		t, ok := merged[e]
		if !ok {
			merged[e] = cloneTable(ot)
			continue
		}
		if ot.Schema != "" {
			t.Schema = ot.Schema
		}
		if ot.Name != "" {
			t.Name = ot.Name
		}
		for k, v := range ot.Columns {
			if v != "" {
				t.Columns[k] = v
			}
		}
		merged[e] = t
	}
	return merged
}

func cloneTable(t Table) Table {
	c := t
	c.Columns = make(map[string]string, len(t.Columns))
	for k, v := range t.Columns {
		c.Columns[k] = v
	}
	return c
}

// Validate checks that the schema only names known entities and columns and that
// every identifier is safe. It is meant to run on the merged (default + override) schema.
func (s Schema) Validate() error {
	known := map[Column]bool{}
	for _, c := range allColumns {
		known[c] = true
	}
	entities := make([]string, 0, len(s))
	for e := range s {
		entities = append(entities, string(e))
	}
	sort.Strings(entities)
	for _, en := range entities {
		e := Entity(en)
		t := s[e]
		if firstKnownColumn(e) == "" {
			return fmt.Errorf("lookup: unknown entity %q", e)
		}
		if !validQualifiedIdent(t.Name) {
			return fmt.Errorf("lookup: invalid table name %q for %s", t.Name, e)
		}
		if t.Schema != "" && !validIdent(t.Schema) {
			return fmt.Errorf("lookup: invalid schema name %q for %s", t.Schema, e)
		}
		names := make([]string, 0, len(t.Columns))
		for n := range t.Columns {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if !known[Column{Entity: e, Name: n}] {
				return fmt.Errorf("lookup: unknown column %q for %s", n, e)
			}
			if !validIdent(t.Columns[n]) {
				return fmt.Errorf("lookup: invalid column name %q for %s.%s", t.Columns[n], e, n)
			}
		}
	}
	return nil
}

func firstKnownColumn(e Entity) string {
	for _, c := range allColumns {
		if c.Entity == e {
			return c.Name
		}
	}
	return ""
}

// TableName returns the physical table name of an entity (unqualified, unquoted).
func (s Schema) TableName(e Entity) string {
	if t, ok := s[e]; ok && t.Name != "" {
		return t.Name
	}
	return string(e)
}

// SchemaName returns the optional schema qualifier of an entity.
func (s Schema) SchemaName(e Entity) string { return s[e].Schema }

// Col returns the physical column name for a logical column (unquoted).
func (s Schema) Col(c Column) string {
	if t, ok := s[c.Entity]; ok {
		if n := t.Columns[c.Name]; n != "" {
			return n
		}
	}
	return c.Name
}

// FirstColumn returns the name of the first logical column of an entity (used by the
// direct backend for existence checks).
func FirstColumn(e Entity) string { return firstKnownColumn(e) }
