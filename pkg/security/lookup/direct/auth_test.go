package direct

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

func newAuth(t *testing.T, opts AuthOptions) (*Auth, *sql.DB) {
	db := newTestDB(t)
	return NewAuth(newTestBase(t, db, nil), opts), db
}

func registerUser(t *testing.T, a *Auth, name string) *sectypes.LoginResponse {
	t.Helper()
	resp, err := a.Register(context.Background(), sectypes.RegisterRequest{Username: name, Email: name + "@x.io", Password: "pw-" + name})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestRegisterLoginSessionFlow(t *testing.T) {
	ctx := context.Background()
	a, db := newAuth(t, AuthOptions{})

	reg, err := a.Register(ctx, sectypes.RegisterRequest{
		Username: "ann", Email: "ann@x.io", Password: "secret",
		UserLevel: 99, Roles: []string{"admin"}, // must be ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.User.UserLevel != 0 || len(reg.User.Roles) != 0 {
		t.Fatalf("register honoured privileges: %+v", reg.User)
	}
	var stored string
	if err := db.QueryRow(`SELECT password FROM users WHERE username='ann'`).Scan(&stored); err != nil || !strings.HasPrefix(stored, "$2") {
		t.Fatalf("password not bcrypt: %q %v", stored, err)
	}

	if _, err := a.Register(ctx, sectypes.RegisterRequest{Username: "ann", Email: "other@x.io", Password: "x"}); !errors.Is(err, lookup.ErrUsernameExists) {
		t.Fatalf("got %v", err)
	}
	if _, err := a.Register(ctx, sectypes.RegisterRequest{Username: "bob", Email: "ann@x.io", Password: "x"}); !errors.Is(err, lookup.ErrEmailExists) {
		t.Fatalf("got %v", err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("failed register left a row: %d", n)
	}

	login, err := a.Login(ctx, sectypes.LoginRequest{Username: "ann", Password: "secret", Claims: map[string]any{"ip_address": "1.2.3.4", "user_agent": "ua"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(login.Token, "sess_") || login.ExpiresIn != 86400 || login.User.Email != "ann@x.io" {
		t.Fatalf("login: %+v", login)
	}
	var ip string
	_ = db.QueryRow(`SELECT ip_address FROM user_sessions WHERE session_token=?`, login.Token).Scan(&ip)
	if ip != "1.2.3.4" {
		t.Fatalf("ip %q", ip)
	}

	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: "ann", Password: "wrong"}); err == nil || err.Error() != "invalid credentials" {
		t.Fatalf("got %v", err)
	}
	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: "nobody", Password: "x"}); err == nil || err.Error() != "invalid credentials" {
		t.Fatalf("got %v", err)
	}

	u, err := a.Session(ctx, login.Token, "authenticate")
	if err != nil || u.UserName != "ann" || u.SessionID != login.Token {
		t.Fatalf("session: %+v %v", u, err)
	}
	if err := a.TouchSession(ctx, login.Token, u); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Session(ctx, "nope", "authenticate"); err == nil || err.Error() != "invalid or expired session" {
		t.Fatalf("got %v", err)
	}

	ref, err := a.Refresh(ctx, login.Token)
	if err != nil || ref.Token == login.Token {
		t.Fatalf("refresh: %+v %v", ref, err)
	}
	if _, err := a.Session(ctx, login.Token, ""); err == nil {
		t.Fatal("old session still valid after refresh")
	}
	if _, err := a.Refresh(ctx, login.Token); err == nil || err.Error() != "invalid or expired refresh token" {
		t.Fatalf("got %v", err)
	}

	if err := a.Logout(ctx, sectypes.LogoutRequest{Token: "Bearer " + ref.Token, UserID: ref.User.UserID}); err != nil {
		t.Fatal(err)
	}
	if err := a.Logout(ctx, sectypes.LogoutRequest{Token: ref.Token, UserID: ref.User.UserID}); err == nil || err.Error() != "session not found" {
		t.Fatalf("got %v", err)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	ctx := context.Background()
	a, _ := newAuth(t, AuthOptions{})
	resp := registerUser(t, a, "eve")
	a.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	if _, err := a.Session(ctx, resp.Token, ""); err == nil {
		t.Fatal("expired session accepted")
	}
}

func TestLegacyPasswordUpgradeIsOptIn(t *testing.T) {
	ctx := context.Background()
	for _, upgrade := range []bool{false, true} {
		a, db := newAuth(t, AuthOptions{UpgradePasswordHash: upgrade})
		_, err := db.Exec(`INSERT INTO users (username, email, password, user_level, roles, is_active) VALUES ('old','o@x.io','clear',1,'a,b',1)`)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := a.Login(ctx, sectypes.LoginRequest{Username: "old", Password: "clear"})
		if err != nil || len(resp.User.Roles) != 2 {
			t.Fatalf("login: %+v %v", resp, err)
		}
		var stored string
		_ = db.QueryRow(`SELECT password FROM users WHERE username='old'`).Scan(&stored)
		if got := strings.HasPrefix(stored, "$2"); got != upgrade {
			t.Fatalf("upgrade=%v stored=%q", upgrade, stored)
		}
	}
}

func TestInactiveUserCannotLogin(t *testing.T) {
	ctx := context.Background()
	a, db := newAuth(t, AuthOptions{})
	resp := registerUser(t, a, "ian")
	_, _ = db.Exec(`UPDATE users SET is_active = 0`)
	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: "ian", Password: "pw-ian"}); err == nil {
		t.Fatal("inactive login accepted")
	}
	if _, err := a.Session(ctx, resp.Token, ""); err == nil {
		t.Fatal("inactive session accepted")
	}
}

func TestLoginAPIKey(t *testing.T) {
	ctx := context.Background()
	a, db := newAuth(t, AuthOptions{})
	reg := registerUser(t, a, "kim")
	insert := func(raw, typ string, active int, expires any) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO user_keys (user_id, key_type, key_hash, name, is_active, expires_at) VALUES (?,?,?,?,?,?)`,
			reg.User.UserID, typ, sectypes.HashKey(raw), "k", active, expires)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("good", "header_api", 1, nil)
	insert("generic", "api", 1, nil)
	insert("jwt", "jwt_secret", 1, nil)
	insert("off", "api", 0, nil)
	insert("old", "api", 1, time.Now().UTC().Add(-time.Hour))

	for _, k := range []string{"good", "generic"} {
		resp, err := a.LoginAPIKey(ctx, k, map[string]any{"ip_address": "9.9.9.9"})
		if err != nil || resp.User.UserName != "kim" || !strings.HasPrefix(resp.Token, "sess_") {
			t.Fatalf("%s: %+v %v", k, resp, err)
		}
		if _, err := a.Session(ctx, resp.Token, ""); err != nil {
			t.Fatal(err)
		}
	}
	var used sql.NullString
	_ = db.QueryRow(`SELECT last_used_at FROM user_keys WHERE key_hash = ?`, sectypes.HashKey("good")).Scan(&used)
	if !used.Valid {
		t.Fatal("last_used_at not stamped")
	}
	for _, k := range []string{"", "missing", "jwt", "off", "old"} {
		if _, err := a.LoginAPIKey(ctx, k, nil); !errors.Is(err, lookup.ErrInvalidAPIKey) {
			t.Fatalf("%q: got %v", k, err)
		}
	}
	_, _ = db.Exec(`UPDATE users SET is_active = 0`)
	if _, err := a.LoginAPIKey(ctx, "good", nil); !errors.Is(err, lookup.ErrInvalidAPIKey) {
		t.Fatalf("inactive user: got %v", err)
	}
}

func TestPasswordReset(t *testing.T) {
	ctx := context.Background()
	a, _ := newAuth(t, AuthOptions{})
	reg := registerUser(t, a, "rae")

	empty, err := a.ResetRequest(ctx, sectypes.PasswordResetRequest{Email: "none@x.io"})
	if err != nil || empty.Token != "" {
		t.Fatalf("enumeration leak: %+v %v", empty, err)
	}
	if _, err := a.ResetRequest(ctx, sectypes.PasswordResetRequest{}); err == nil {
		t.Fatal("expected error")
	}

	r1, err := a.ResetRequest(ctx, sectypes.PasswordResetRequest{Email: "rae@x.io"})
	if err != nil || r1.Token == "" {
		t.Fatal(err)
	}
	r2, _ := a.ResetRequest(ctx, sectypes.PasswordResetRequest{Username: "rae"})
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: r1.Token, NewPassword: "n"}); err == nil {
		t.Fatal("superseded token accepted")
	}
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: r2.Token, NewPassword: "newpw"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: r2.Token, NewPassword: "again"}); err == nil {
		t.Fatal("token reused")
	}
	if _, err := a.Session(ctx, reg.Token, ""); err == nil {
		t.Fatal("sessions survived reset")
	}
	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: "rae", Password: "newpw"}); err != nil {
		t.Fatal(err)
	}
}

func TestJWTLoginLogout(t *testing.T) {
	ctx := context.Background()
	a, db := newAuth(t, AuthOptions{})
	reg := registerUser(t, a, "jay")
	resp, err := a.JWTLogin(ctx, sectypes.LoginRequest{Username: "jay", Password: "pw-jay"})
	if err != nil || !strings.HasPrefix(resp.Token, "token_") {
		t.Fatalf("%+v %v", resp, err)
	}
	if err := a.JWTLogout(ctx, sectypes.LogoutRequest{Token: "tok", UserID: reg.User.UserID}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM token_blacklist WHERE token='tok'`).Scan(&n)
	if n != 1 {
		t.Fatal("token not blacklisted")
	}
}

func TestCustomSchemaNames(t *testing.T) {
	db := newTestDB(t, `
CREATE TABLE app_users (uid INTEGER PRIMARY KEY AUTOINCREMENT, login TEXT, email TEXT, password TEXT,
  user_level INTEGER, roles TEXT, is_active INTEGER, created_at DATETIME, updated_at DATETIME,
  last_login_at DATETIME, program_user_id INTEGER, program_user_table TEXT, remote_id TEXT, auth_provider TEXT,
  totp_secret TEXT, totp_enabled INTEGER, totp_enabled_at DATETIME);`)
	schema := lookup.Schema{lookup.EntityUsers: {Name: "app_users", Columns: map[string]string{"id": "uid", "username": "login"}}}
	a := NewAuth(newTestBase(t, db, schema), AuthOptions{})
	ctx := context.Background()
	if _, err := a.Register(ctx, sectypes.RegisterRequest{Username: "zed", Email: "z@x.io", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: "zed", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	var login string
	if err := db.QueryRow(`SELECT login FROM app_users`).Scan(&login); err != nil || login != "zed" {
		t.Fatalf("%q %v", login, err)
	}
}
