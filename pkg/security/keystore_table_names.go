package security

import "fmt"

// KeyStoreTableNames holds the configurable table name used by DatabaseKeyStore
// in Direct mode. Use DefaultKeyStoreTableNames() for defaults and
// MergeKeyStoreTableNames() for partial overrides.
type KeyStoreTableNames struct {
	UserKeys string // default: "user_keys"
}

// DefaultKeyStoreTableNames returns a KeyStoreTableNames with default table names.
func DefaultKeyStoreTableNames() *KeyStoreTableNames {
	return &KeyStoreTableNames{
		UserKeys: "user_keys",
	}
}

// MergeKeyStoreTableNames returns a copy of base with any non-empty fields from override applied.
// If override is nil, a copy of base is returned.
func MergeKeyStoreTableNames(base, override *KeyStoreTableNames) *KeyStoreTableNames {
	if override == nil {
		copied := *base
		return &copied
	}
	merged := *base
	if override.UserKeys != "" {
		merged.UserKeys = override.UserKeys
	}
	return &merged
}

// ValidateKeyStoreTableNames checks that all non-empty table names are valid SQL identifiers.
func ValidateKeyStoreTableNames(names *KeyStoreTableNames) error {
	if names.UserKeys != "" && !validSQLIdentifier.MatchString(names.UserKeys) {
		return fmt.Errorf("KeyStoreTableNames.UserKeys contains invalid characters: %q", names.UserKeys)
	}
	return nil
}

// resolveKeyStoreTableNames merges an optional override with defaults.
func resolveKeyStoreTableNames(override *KeyStoreTableNames) *KeyStoreTableNames {
	return MergeKeyStoreTableNames(DefaultKeyStoreTableNames(), override)
}
