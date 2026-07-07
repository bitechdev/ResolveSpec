package security

import (
	"reflect"
	"testing"
)

func TestDefaultTableNames_AllFieldsNonEmpty(t *testing.T) {
	names := DefaultTableNames()
	v := reflect.ValueOf(names).Elem()
	typ := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		if field.String() == "" {
			t.Errorf("DefaultTableNames().%s is empty", typ.Field(i).Name)
		}
	}
}

func TestMergeTableNames_PartialOverride(t *testing.T) {
	base := DefaultTableNames()
	override := &TableNames{Users: "custom_users", OAuthCodes: "custom_oauth_codes"}

	merged := MergeTableNames(base, override)

	if merged.Users != "custom_users" {
		t.Errorf("MergeTableNames().Users = %q, want %q", merged.Users, "custom_users")
	}
	if merged.OAuthCodes != "custom_oauth_codes" {
		t.Errorf("MergeTableNames().OAuthCodes = %q, want %q", merged.OAuthCodes, "custom_oauth_codes")
	}
	if merged.UserSessions != "user_sessions" {
		t.Errorf("MergeTableNames().UserSessions = %q, want default", merged.UserSessions)
	}
}

func TestMergeTableNames_NilOverride(t *testing.T) {
	base := DefaultTableNames()
	merged := MergeTableNames(base, nil)

	if merged == base {
		t.Error("MergeTableNames with nil override should return a copy, not the same pointer")
	}
	if *merged != *base {
		t.Errorf("MergeTableNames(base, nil) = %+v, want %+v", merged, base)
	}
}

func TestMergeTableNames_DoesNotMutateBase(t *testing.T) {
	base := DefaultTableNames()
	original := base.Users

	override := &TableNames{Users: "custom_users"}
	_ = MergeTableNames(base, override)

	if base.Users != original {
		t.Errorf("MergeTableNames mutated base: Users = %q, want %q", base.Users, original)
	}
}

func TestValidateTableNames_Valid(t *testing.T) {
	if err := ValidateTableNames(DefaultTableNames()); err != nil {
		t.Errorf("ValidateTableNames(defaults) error = %v", err)
	}
}

func TestValidateTableNames_Invalid(t *testing.T) {
	names := DefaultTableNames()
	names.Users = "users; DROP TABLE users; --"

	if err := ValidateTableNames(names); err == nil {
		t.Error("ValidateTableNames should reject names with invalid characters")
	}
}

func TestResolveTableNames_NoOverride(t *testing.T) {
	names := resolveTableNames(nil)
	if names.Users != "users" {
		t.Errorf("resolveTableNames(nil).Users = %q, want default", names.Users)
	}
}

func TestResolveTableNames_WithOverride(t *testing.T) {
	names := resolveTableNames(&TableNames{Users: "custom_users"})
	if names.Users != "custom_users" {
		t.Errorf("resolveTableNames().Users = %q, want %q", names.Users, "custom_users")
	}
	if names.UserSessions != "user_sessions" {
		t.Errorf("resolveTableNames().UserSessions = %q, want default", names.UserSessions)
	}
}

func TestDefaultKeyStoreTableNames(t *testing.T) {
	names := DefaultKeyStoreTableNames()
	if names.UserKeys != "user_keys" {
		t.Errorf("DefaultKeyStoreTableNames().UserKeys = %q, want %q", names.UserKeys, "user_keys")
	}
}

func TestMergeKeyStoreTableNames_PartialOverride(t *testing.T) {
	base := DefaultKeyStoreTableNames()
	merged := MergeKeyStoreTableNames(base, &KeyStoreTableNames{UserKeys: "custom_keys"})
	if merged.UserKeys != "custom_keys" {
		t.Errorf("MergeKeyStoreTableNames().UserKeys = %q, want %q", merged.UserKeys, "custom_keys")
	}
}

func TestMergeKeyStoreTableNames_NilOverride(t *testing.T) {
	base := DefaultKeyStoreTableNames()
	merged := MergeKeyStoreTableNames(base, nil)
	if merged == base {
		t.Error("MergeKeyStoreTableNames with nil override should return a copy, not the same pointer")
	}
	if *merged != *base {
		t.Errorf("MergeKeyStoreTableNames(base, nil) = %+v, want %+v", merged, base)
	}
}

func TestValidateKeyStoreTableNames_Invalid(t *testing.T) {
	names := &KeyStoreTableNames{UserKeys: "bad name!"}
	if err := ValidateKeyStoreTableNames(names); err == nil {
		t.Error("ValidateKeyStoreTableNames should reject names with invalid characters")
	}
}

func TestValidateKeyStoreTableNames_Valid(t *testing.T) {
	if err := ValidateKeyStoreTableNames(DefaultKeyStoreTableNames()); err != nil {
		t.Errorf("ValidateKeyStoreTableNames(defaults) error = %v", err)
	}
}
