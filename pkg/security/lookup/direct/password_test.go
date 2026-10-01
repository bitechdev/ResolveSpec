package direct

import "testing"

func TestVerifyPasswordEdgeCases(t *testing.T) {
	h, _ := HashPassword("pw")
	if ok, _ := VerifyPassword(h, "pw"); !ok {
		t.Error("bcrypt match failed")
	}
	if ok, _ := VerifyPassword("", "pw"); ok {
		t.Error("empty stored must not match")
	}
	if ok, _ := VerifyPassword("pw", ""); ok {
		t.Error("empty supplied must not match")
	}
	if _, err := HashPassword(string(make([]byte, 73))); err == nil {
		t.Error("73-byte password must be rejected")
	}
}
