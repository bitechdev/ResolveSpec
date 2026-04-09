package security

import "fmt"

// KeyStoreSQLNames holds the configurable stored procedure names used by DatabaseKeyStore.
// Use DefaultKeyStoreSQLNames() for defaults and MergeKeyStoreSQLNames() for partial overrides.
type KeyStoreSQLNames struct {
	GetUserKeys string // default: "resolvespec_keystore_get_user_keys"
	CreateKey   string // default: "resolvespec_keystore_create_key"
	DeleteKey   string // default: "resolvespec_keystore_delete_key"
	ValidateKey string // default: "resolvespec_keystore_validate_key"
}

// DefaultKeyStoreSQLNames returns a KeyStoreSQLNames with all default resolvespec_keystore_* values.
func DefaultKeyStoreSQLNames() *KeyStoreSQLNames {
	return &KeyStoreSQLNames{
		GetUserKeys: "resolvespec_keystore_get_user_keys",
		CreateKey:   "resolvespec_keystore_create_key",
		DeleteKey:   "resolvespec_keystore_delete_key",
		ValidateKey: "resolvespec_keystore_validate_key",
	}
}

// MergeKeyStoreSQLNames returns a copy of base with any non-empty fields from override applied.
// If override is nil, a copy of base is returned.
func MergeKeyStoreSQLNames(base, override *KeyStoreSQLNames) *KeyStoreSQLNames {
	if override == nil {
		copied := *base
		return &copied
	}
	merged := *base
	if override.GetUserKeys != "" {
		merged.GetUserKeys = override.GetUserKeys
	}
	if override.CreateKey != "" {
		merged.CreateKey = override.CreateKey
	}
	if override.DeleteKey != "" {
		merged.DeleteKey = override.DeleteKey
	}
	if override.ValidateKey != "" {
		merged.ValidateKey = override.ValidateKey
	}
	return &merged
}

// ValidateKeyStoreSQLNames checks that all non-empty procedure names are valid SQL identifiers.
func ValidateKeyStoreSQLNames(names *KeyStoreSQLNames) error {
	fields := map[string]string{
		"GetUserKeys": names.GetUserKeys,
		"CreateKey":   names.CreateKey,
		"DeleteKey":   names.DeleteKey,
		"ValidateKey": names.ValidateKey,
	}
	for field, val := range fields {
		if val != "" && !validSQLIdentifier.MatchString(val) {
			return fmt.Errorf("KeyStoreSQLNames.%s contains invalid characters: %q", field, val)
		}
	}
	return nil
}
