package security

import (
	"reflect"
	"testing"
)

func TestDefaultSQLNames_AllFieldsNonEmpty(t *testing.T) {
	names := DefaultSQLNames()
	v := reflect.ValueOf(names).Elem()
	typ := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		if field.String() == "" {
			t.Errorf("DefaultSQLNames().%s is empty", typ.Field(i).Name)
		}
	}
}

func TestMergeSQLNames_PartialOverride(t *testing.T) {
	base := DefaultSQLNames()
	override := &SQLNames{
		Login:       "custom_login",
		TOTPEnable:  "custom_totp_enable",
		PasskeyLogin: "custom_passkey_login",
	}

	merged := MergeSQLNames(base, override)

	if merged.Login != "custom_login" {
		t.Errorf("MergeSQLNames().Login = %q, want %q", merged.Login, "custom_login")
	}
	if merged.TOTPEnable != "custom_totp_enable" {
		t.Errorf("MergeSQLNames().TOTPEnable = %q, want %q", merged.TOTPEnable, "custom_totp_enable")
	}
	if merged.PasskeyLogin != "custom_passkey_login" {
		t.Errorf("MergeSQLNames().PasskeyLogin = %q, want %q", merged.PasskeyLogin, "custom_passkey_login")
	}
	// Non-overridden fields should retain defaults
	if merged.Logout != "resolvespec_logout" {
		t.Errorf("MergeSQLNames().Logout = %q, want %q", merged.Logout, "resolvespec_logout")
	}
	if merged.Session != "resolvespec_session" {
		t.Errorf("MergeSQLNames().Session = %q, want %q", merged.Session, "resolvespec_session")
	}
}

func TestMergeSQLNames_NilOverride(t *testing.T) {
	base := DefaultSQLNames()
	merged := MergeSQLNames(base, nil)

	// Should be a copy, not the same pointer
	if merged == base {
		t.Error("MergeSQLNames with nil override should return a copy, not the same pointer")
	}

	// All values should match
	v1 := reflect.ValueOf(base).Elem()
	v2 := reflect.ValueOf(merged).Elem()
	typ := v1.Type()

	for i := 0; i < v1.NumField(); i++ {
		f1 := v1.Field(i)
		f2 := v2.Field(i)
		if f1.Kind() != reflect.String {
			continue
		}
		if f1.String() != f2.String() {
			t.Errorf("MergeSQLNames(base, nil).%s = %q, want %q", typ.Field(i).Name, f2.String(), f1.String())
		}
	}
}

func TestMergeSQLNames_DoesNotMutateBase(t *testing.T) {
	base := DefaultSQLNames()
	originalLogin := base.Login

	override := &SQLNames{Login: "custom_login"}
	_ = MergeSQLNames(base, override)

	if base.Login != originalLogin {
		t.Errorf("MergeSQLNames mutated base: Login = %q, want %q", base.Login, originalLogin)
	}
}

func TestMergeSQLNames_AllFieldsMerged(t *testing.T) {
	base := DefaultSQLNames()
	override := &SQLNames{}
	v := reflect.ValueOf(override).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			v.Field(i).SetString("custom_sentinel")
		}
	}

	merged := MergeSQLNames(base, override)
	mv := reflect.ValueOf(merged).Elem()
	typ := mv.Type()
	for i := 0; i < mv.NumField(); i++ {
		if mv.Field(i).Kind() != reflect.String {
			continue
		}
		if mv.Field(i).String() != "custom_sentinel" {
			t.Errorf("MergeSQLNames did not merge field %s", typ.Field(i).Name)
		}
	}
}

func TestValidateSQLNames_Valid(t *testing.T) {
	names := DefaultSQLNames()
	if err := ValidateSQLNames(names); err != nil {
		t.Errorf("ValidateSQLNames(defaults) error = %v", err)
	}
}

func TestValidateSQLNames_Invalid(t *testing.T) {
	names := DefaultSQLNames()
	names.Login = "resolvespec_login; DROP TABLE users; --"

	err := ValidateSQLNames(names)
	if err == nil {
		t.Error("ValidateSQLNames should reject names with invalid characters")
	}
}

func TestResolveSQLNames_NoOverride(t *testing.T) {
	names := resolveSQLNames()
	if names.Login != "resolvespec_login" {
		t.Errorf("resolveSQLNames().Login = %q, want default", names.Login)
	}
}

func TestResolveSQLNames_WithOverride(t *testing.T) {
	names := resolveSQLNames(&SQLNames{Login: "custom_login"})
	if names.Login != "custom_login" {
		t.Errorf("resolveSQLNames().Login = %q, want %q", names.Login, "custom_login")
	}
	if names.Logout != "resolvespec_logout" {
		t.Errorf("resolveSQLNames().Logout = %q, want default", names.Logout)
	}
}
