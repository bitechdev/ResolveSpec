package direct

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestPasskeyLifecycle(t *testing.T) {
	ctx := context.Background()
	a, _ := newAuth(t, AuthOptions{})
	reg := registerUser(t, a, "pat")
	uid := reg.User.UserID
	p := NewPasskey(a.Base)

	rec := lookup.PasskeyCredentialRecord{UserID: uid, CredentialID: b64("cred1"), PublicKey: b64("pk"), AttestationType: "none",
		Transports: []string{"usb", "nfc"}, Name: "Key 1"}
	id, err := p.Store(ctx, rec)
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	if _, err := p.Store(ctx, rec); err == nil || err.Error() != "credential already exists" {
		t.Fatalf("dup: %v", err)
	}
	rec.CredentialID, rec.UserID = b64("cred2"), 999
	if _, err := p.Store(ctx, rec); err == nil || err.Error() != "user not found" {
		t.Fatalf("no user: %v", err)
	}
	rec.UserID, rec.Name = uid, "Key 2"
	if _, err := p.Store(ctx, rec); err != nil {
		t.Fatal(err)
	}

	owner, count, err := p.Get(ctx, b64("cred1"))
	if err != nil || owner != uid || count != 0 {
		t.Fatalf("%d %d %v", owner, count, err)
	}
	if _, _, err := p.Get(ctx, b64("zzz")); err == nil || err.Error() != "credential not found" {
		t.Fatalf("got %v", err)
	}

	if clone, err := p.UpdateCounter(ctx, b64("cred1"), 5); err != nil || clone {
		t.Fatalf("%v %v", clone, err)
	}
	if clone, err := p.UpdateCounter(ctx, b64("cred1"), 5); err != nil || !clone {
		t.Fatalf("replayed counter must flag clone: %v %v", clone, err)
	}
	if _, count, _ = p.Get(ctx, b64("cred1")); count != 5 {
		t.Fatalf("counter changed on clone: %d", count)
	}
	if _, err := p.UpdateCounter(ctx, b64("missing"), 1); err == nil {
		t.Fatal("expected not found")
	}

	list, err := p.List(ctx, uid)
	if err != nil || len(list) != 2 {
		t.Fatalf("%d %v", len(list), err)
	}
	for _, c := range list {
		if string(c.CredentialID) == "cred1" {
			if !c.CloneWarning || c.SignCount != 5 || len(c.Transports) != 2 || c.Name != "Key 1" {
				t.Fatalf("%+v", c)
			}
		}
	}

	if err := p.Rename(ctx, uid, b64("cred1"), "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := p.Rename(ctx, uid+1, b64("cred1"), "x"); err == nil {
		t.Fatal("renamed another user's credential")
	}

	gotID, refs, err := p.ByUsername(ctx, "pat")
	if err != nil || gotID != uid || len(refs) != 2 {
		t.Fatalf("%d %+v %v", gotID, refs, err)
	}
	if _, _, err := p.ByUsername(ctx, "ghost"); err == nil || err.Error() != "user not found" {
		t.Fatalf("got %v", err)
	}

	resp, err := p.Login(ctx, uid, map[string]any{"ip_address": "1.1.1.1"})
	if err != nil || resp.User.UserName != "pat" || resp.ExpiresIn != 86400 {
		t.Fatalf("%+v %v", resp, err)
	}
	if _, err := a.Session(ctx, resp.Token, ""); err != nil {
		t.Fatal(err)
	}

	if err := p.Delete(ctx, uid+1, b64("cred1")); err == nil {
		t.Fatal("deleted another user's credential")
	}
	if err := p.Delete(ctx, uid, b64("cred1")); err != nil {
		t.Fatal(err)
	}
	if err := p.Delete(ctx, uid, b64("cred1")); err == nil || err.Error() != "credential not found" {
		t.Fatalf("got %v", err)
	}
}

func TestTOTPLifecycle(t *testing.T) {
	ctx := context.Background()
	a, _ := newAuth(t, AuthOptions{})
	uid := registerUser(t, a, "tom").User.UserID
	s := NewTOTP(a.Base)

	if on, err := s.Status(ctx, uid); err != nil || on {
		t.Fatalf("%v %v", on, err)
	}
	if _, err := s.Secret(ctx, uid); err == nil || err.Error() != "TOTP not enabled for user" {
		t.Fatalf("got %v", err)
	}
	if err := s.RegenerateBackupCodes(ctx, uid, []string{"h"}); err == nil {
		t.Fatal("regenerate without 2FA")
	}
	if err := s.Enable(ctx, 999, "S", nil); err == nil || err.Error() != "user not found" {
		t.Fatalf("got %v", err)
	}

	if err := s.Enable(ctx, uid, "SECRET", []string{"h1", "h2"}); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.Status(ctx, uid); !on {
		t.Fatal("not enabled")
	}
	if sec, err := s.Secret(ctx, uid); err != nil || sec != "SECRET" {
		t.Fatalf("%q %v", sec, err)
	}

	if ok, err := s.ValidateBackupCode(ctx, uid, "h1"); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := s.ValidateBackupCode(ctx, uid, "h1"); err == nil || err.Error() != "backup code already used" {
		t.Fatalf("reuse: %v", err)
	}
	if ok, err := s.ValidateBackupCode(ctx, uid, "nope"); err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	if err := s.RegenerateBackupCodes(ctx, uid, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ValidateBackupCode(ctx, uid, "h2"); ok {
		t.Fatal("old code survived regenerate")
	}
	if ok, _ := s.ValidateBackupCode(ctx, uid, "n1"); !ok {
		t.Fatal("new code rejected")
	}

	if err := s.Disable(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.Status(ctx, uid); on {
		t.Fatal("still enabled")
	}
	if ok, _ := s.ValidateBackupCode(ctx, uid, "n1"); ok {
		t.Fatal("codes survived disable")
	}
}
