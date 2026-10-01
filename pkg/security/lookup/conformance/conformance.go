// Package conformance is the shared behavioural suite every lookup backend must pass.
// It only uses the store interfaces, so the same cases run against the direct backend on
// every dialect and against the procedure backend on Postgres. Error messages are not
// asserted (backends word them differently), only whether an operation succeeds or fails
// and the values it returns.
//
// The suite names everything it creates with Env.Prefix and never assumes empty tables, so
// it can run against a shared database. Env.Cleanup, when set, removes the prefixed rows.
package conformance

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/lookup/dialect"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Env is one backend under test.
type Env struct {
	Provider *lookup.Provider
	// DB and Dialect are used only to seed policy rules, which have no store method.
	DB      *sql.DB
	Dialect dialect.Dialect
	// Prefix makes every created name unique to this run.
	Prefix string
	// Cleanup removes rows whose names start with Prefix. Optional.
	Cleanup func(t *testing.T)
}

// Run executes the suite.
func Run(t *testing.T, env Env) {
	if env.Cleanup != nil {
		t.Cleanup(func() { env.Cleanup(t) })
	}
	s := &suite{Env: env}
	t.Run("AuthSessionLifecycle", s.authSessionLifecycle)
	t.Run("AuthRejectsBadCredentials", s.authRejectsBadCredentials)
	t.Run("RegisterIgnoresPrivileges", s.registerIgnoresPrivileges)
	t.Run("RegisterRejectsDuplicates", s.registerRejectsDuplicates)
	t.Run("PasswordReset", s.passwordReset)
	t.Run("JWT", s.jwt)
	t.Run("Keys", s.keys)
	t.Run("LoginAPIKey", s.loginAPIKey)
	t.Run("OAuthClientAndCodes", s.oauthClientAndCodes)
	t.Run("OAuthIntrospectRevoke", s.oauthIntrospectRevoke)
	t.Run("OAuthUsers", s.oauthUsers)
	t.Run("Passkey", s.passkey)
	t.Run("TOTP", s.totp)
	t.Run("Policy", s.policy)
}

type suite struct{ Env }

var ctx = context.Background()

func (s *suite) name(n string) string { return s.Prefix + n }

func (s *suite) register(t *testing.T, n string) *sectypes.LoginResponse {
	t.Helper()
	resp, err := s.Provider.Auth.Register(ctx, sectypes.RegisterRequest{
		Username: s.name(n), Email: s.name(n) + "@example.test", Password: "pw-" + n,
	})
	if err != nil {
		t.Fatalf("register %s: %v", n, err)
	}
	if resp == nil || resp.User == nil || resp.Token == "" || resp.User.UserID == 0 {
		t.Fatalf("register %s: incomplete response %+v", n, resp)
	}
	return resp
}

func rejected(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", what)
	}
}

// notOK asserts an operation did not validate: it either failed or returned false.
func notOK(t *testing.T, what string, ok bool, err error) {
	t.Helper()
	if err == nil && ok {
		t.Fatalf("%s: accepted", what)
	}
}

func (s *suite) authSessionLifecycle(t *testing.T) {
	a := s.Provider.Auth
	reg := s.register(t, "life")

	login, err := a.Login(ctx, sectypes.LoginRequest{Username: s.name("life"), Password: "pw-life",
		Claims: map[string]any{"ip_address": "10.0.0.1", "user_agent": "conformance"}})
	if err != nil || login.Token == "" || login.User.UserName != s.name("life") {
		t.Fatalf("login: %+v %v", login, err)
	}
	if login.Token == reg.Token {
		t.Fatal("login reused the registration session")
	}

	u, err := a.Session(ctx, login.Token, "authenticate")
	if err != nil || u.UserName != s.name("life") || u.UserID != reg.User.UserID {
		t.Fatalf("session: %+v %v", u, err)
	}
	if err := a.TouchSession(ctx, login.Token, u); err != nil {
		t.Fatalf("touch: %v", err)
	}
	_, err = a.Session(ctx, s.name("no-such-token"), "authenticate")
	rejected(t, "unknown session", err)

	ref, err := a.Refresh(ctx, login.Token)
	if err != nil || ref.Token == "" || ref.Token == login.Token {
		t.Fatalf("refresh: %+v %v", ref, err)
	}
	_, err = a.Session(ctx, login.Token, "")
	rejected(t, "session after refresh", err)
	_, err = a.Refresh(ctx, login.Token)
	rejected(t, "second refresh of the same token", err)

	if err := a.Logout(ctx, sectypes.LogoutRequest{Token: ref.Token, UserID: ref.User.UserID}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	_, err = a.Session(ctx, ref.Token, "")
	rejected(t, "session after logout", err)
}

func (s *suite) authRejectsBadCredentials(t *testing.T) {
	a := s.Provider.Auth
	s.register(t, "creds")
	_, err := a.Login(ctx, sectypes.LoginRequest{Username: s.name("creds"), Password: "wrong"})
	rejected(t, "wrong password", err)
	_, err = a.Login(ctx, sectypes.LoginRequest{Username: s.name("creds")})
	rejected(t, "empty password", err)
	_, err = a.Login(ctx, sectypes.LoginRequest{Username: s.name("nobody"), Password: "pw"})
	rejected(t, "unknown user", err)
}

func (s *suite) registerIgnoresPrivileges(t *testing.T) {
	resp, err := s.Provider.Auth.Register(ctx, sectypes.RegisterRequest{
		Username: s.name("priv"), Email: s.name("priv") + "@example.test", Password: "x",
		UserLevel: 99, Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.User.UserLevel != 0 || len(resp.User.Roles) != 0 {
		t.Fatalf("client-supplied privileges honoured: %+v", resp.User)
	}
}

func (s *suite) registerRejectsDuplicates(t *testing.T) {
	s.register(t, "dup")
	_, err := s.Provider.Auth.Register(ctx, sectypes.RegisterRequest{Username: s.name("dup"), Email: s.name("dup2") + "@example.test", Password: "x"})
	rejected(t, "duplicate username", err)
	_, err = s.Provider.Auth.Register(ctx, sectypes.RegisterRequest{Username: s.name("dup2"), Email: s.name("dup") + "@example.test", Password: "x"})
	rejected(t, "duplicate email", err)
}

func (s *suite) passwordReset(t *testing.T) {
	a := s.Provider.Auth
	reg := s.register(t, "reset")

	if r, err := a.ResetRequest(ctx, sectypes.PasswordResetRequest{Email: s.name("nobody") + "@example.test"}); err != nil || (r != nil && r.Token != "") {
		t.Fatalf("unknown email must succeed without a token (user enumeration): %+v %v", r, err)
	}
	req, err := a.ResetRequest(ctx, sectypes.PasswordResetRequest{Email: s.name("reset") + "@example.test"})
	if err != nil || req == nil || req.Token == "" {
		t.Fatalf("reset request: %+v %v", req, err)
	}
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: "bogus", NewPassword: "x"}); err == nil {
		t.Fatal("bogus reset token accepted")
	}
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: req.Token, NewPassword: "new-pw"}); err != nil {
		t.Fatalf("reset complete: %v", err)
	}
	if err := a.ResetComplete(ctx, sectypes.PasswordResetCompleteRequest{Token: req.Token, NewPassword: "again"}); err == nil {
		t.Fatal("reset token reused")
	}
	_, err = a.Session(ctx, reg.Token, "")
	rejected(t, "session surviving a password reset", err)
	if _, err := a.Login(ctx, sectypes.LoginRequest{Username: s.name("reset"), Password: "new-pw"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	_, err = a.Login(ctx, sectypes.LoginRequest{Username: s.name("reset"), Password: "pw-reset"})
	rejected(t, "old password after reset", err)
}

func (s *suite) jwt(t *testing.T) {
	a := s.Provider.Auth
	reg := s.register(t, "jwt")
	resp, err := a.JWTLogin(ctx, sectypes.LoginRequest{Username: s.name("jwt"), Password: "pw-jwt"})
	if err != nil || resp.Token == "" || resp.User.UserID != reg.User.UserID {
		t.Fatalf("jwt login: %+v %v", resp, err)
	}
	_, err = a.JWTLogin(ctx, sectypes.LoginRequest{Username: s.name("jwt"), Password: "bad"})
	rejected(t, "jwt login with wrong password", err)
	if err := a.JWTLogout(ctx, sectypes.LogoutRequest{Token: s.name("jwt-tok"), UserID: reg.User.UserID}); err != nil {
		t.Fatalf("jwt logout: %v", err)
	}
}

func (s *suite) createKey(t *testing.T, uid int, typ sectypes.KeyType, raw string, exp *time.Time) *sectypes.UserKey {
	t.Helper()
	k, err := s.Provider.Keys.Create(ctx, sectypes.CreateKeyRequest{UserID: uid, KeyType: typ, Name: s.name("key"),
		Scopes: []string{"read"}, ExpiresAt: exp}, sectypes.HashKey(raw))
	if err != nil || k == nil || k.ID == 0 {
		t.Fatalf("create key: %+v %v", k, err)
	}
	return k
}

func (s *suite) keys(t *testing.T) {
	k := s.Provider.Keys
	uid := s.register(t, "keys").User.UserID
	raw := s.name("raw-keys")
	created := s.createKey(t, uid, sectypes.KeyTypeHeaderAPI, raw, nil)
	s.createKey(t, uid, sectypes.KeyTypeJWTSecret, s.name("raw-keys-jwt"), nil)
	s.createKey(t, uid, sectypes.KeyTypeGenericAPI, s.name("raw-keys-old"), ptr(time.Now().Add(-time.Hour)))

	all, err := k.List(ctx, uid, "")
	if err != nil || len(all) != 2 {
		t.Fatalf("list must hide expired keys: %d %v", len(all), err)
	}
	one, err := k.List(ctx, uid, sectypes.KeyTypeHeaderAPI)
	if err != nil || len(one) != 1 || one[0].ID != created.ID || len(one[0].Scopes) != 1 {
		t.Fatalf("typed list: %+v %v", one, err)
	}

	got, err := k.Validate(ctx, sectypes.HashKey(raw), sectypes.KeyTypeHeaderAPI)
	if err != nil || got.UserID != uid {
		t.Fatalf("validate: %+v %v", got, err)
	}
	_, err = k.Validate(ctx, sectypes.HashKey(raw), sectypes.KeyTypeGenericAPI)
	rejected(t, "wrong key type", err)
	_, err = k.Validate(ctx, sectypes.HashKey(s.name("raw-keys-old")), "")
	rejected(t, "expired key", err)
	_, err = k.Validate(ctx, sectypes.HashKey(s.name("unknown")), "")
	rejected(t, "unknown key", err)

	_, err = k.Delete(ctx, uid+1_000_000, created.ID)
	rejected(t, "deleting another user's key", err)
	if _, err := k.Delete(ctx, uid, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = k.Delete(ctx, uid, created.ID)
	rejected(t, "deleting twice", err)
	_, err = k.Validate(ctx, sectypes.HashKey(raw), "")
	rejected(t, "deleted key", err)
}

func (s *suite) loginAPIKey(t *testing.T) {
	a := s.Provider.Auth
	uid := s.register(t, "apikey").User.UserID
	good, generic, jwtKey, off, old := s.name("ak-good"), s.name("ak-generic"), s.name("ak-jwt"), s.name("ak-off"), s.name("ak-old")
	s.createKey(t, uid, sectypes.KeyTypeHeaderAPI, good, nil)
	s.createKey(t, uid, sectypes.KeyTypeGenericAPI, generic, nil)
	s.createKey(t, uid, sectypes.KeyTypeJWTSecret, jwtKey, nil)
	inactive := s.createKey(t, uid, sectypes.KeyTypeGenericAPI, off, nil)
	if _, err := s.Provider.Keys.Delete(ctx, uid, inactive.ID); err != nil {
		t.Fatal(err)
	}
	s.createKey(t, uid, sectypes.KeyTypeGenericAPI, old, ptr(time.Now().Add(-time.Hour)))

	for _, raw := range []string{good, generic} {
		resp, err := a.LoginAPIKey(ctx, raw, map[string]any{"ip_address": "10.0.0.2"})
		if err != nil || resp.User.UserName != s.name("apikey") || resp.Token == "" {
			t.Fatalf("api key login: %+v %v", resp, err)
		}
		if _, err := a.Session(ctx, resp.Token, ""); err != nil {
			t.Fatalf("session from api key login: %v", err)
		}
	}
	for _, raw := range []string{"", s.name("ak-missing"), jwtKey, off, old} {
		_, err := a.LoginAPIKey(ctx, raw, nil)
		if !errors.Is(err, lookup.ErrInvalidAPIKey) {
			t.Fatalf("key %q: want ErrInvalidAPIKey, got %v", raw, err)
		}
	}
}

func (s *suite) oauthClientAndCodes(t *testing.T) {
	c := s.Provider.OAuthClient
	cid := s.name("client")
	reg, err := c.RegisterClient(ctx, &sectypes.OAuthServerClient{ClientID: cid, RedirectURIs: []string{"https://app.example.test/cb"}, ClientName: "App"})
	if err != nil || reg.ClientID != cid {
		t.Fatalf("register client: %+v %v", reg, err)
	}
	got, err := c.GetClient(ctx, cid)
	if err != nil || got.ClientName != "App" || len(got.RedirectURIs) != 1 || got.RedirectURIs[0] != "https://app.example.test/cb" {
		t.Fatalf("get client: %+v %v", got, err)
	}
	_, err = c.GetClient(ctx, s.name("no-client"))
	rejected(t, "unknown client", err)

	code := &sectypes.OAuthCode{Code: s.name("code1"), ClientID: cid, RedirectURI: "https://app.example.test/cb",
		CodeChallenge: "challenge", SessionToken: s.name("sess"), Scopes: []string{"openid"}, ExpiresAt: time.Now().Add(time.Minute)}
	if err := c.SaveCode(ctx, code); err != nil {
		t.Fatalf("save code: %v", err)
	}
	ex, err := c.ExchangeCode(ctx, code.Code)
	if err != nil || ex.Code != code.Code || ex.ClientID != cid || ex.SessionToken != code.SessionToken || len(ex.Scopes) != 1 {
		t.Fatalf("exchange: %+v %v", ex, err)
	}
	_, err = c.ExchangeCode(ctx, code.Code)
	rejected(t, "code reuse", err)

	expired := *code
	expired.Code, expired.ExpiresAt = s.name("code2"), time.Now().Add(-time.Minute)
	if err := c.SaveCode(ctx, &expired); err != nil {
		t.Fatal(err)
	}
	_, err = c.ExchangeCode(ctx, expired.Code)
	rejected(t, "expired code", err)
}

func (s *suite) oauthIntrospectRevoke(t *testing.T) {
	c := s.Provider.OAuthClient
	reg := s.register(t, "intro")
	info, err := c.Introspect(ctx, reg.Token)
	if err != nil || !info.Active || info.Username != s.name("intro") {
		t.Fatalf("introspect: %+v %v", info, err)
	}
	if err := c.Revoke(ctx, reg.Token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if info, err := c.Introspect(ctx, reg.Token); err != nil || info.Active {
		t.Fatalf("revoked token still active: %+v %v", info, err)
	}
	if err := c.Revoke(ctx, s.name("unknown-token")); err != nil {
		t.Fatalf("revoking an unknown token must succeed (RFC 7009): %v", err)
	}
	if info, err := c.Introspect(ctx, s.name("unknown-token")); err != nil || info.Active {
		t.Fatalf("unknown token: %+v %v", info, err)
	}
}

func (s *suite) oauthUsers(t *testing.T) {
	o := s.Provider.OAuthUser
	id, err := o.GetOrCreateUser(ctx, &sectypes.UserContext{UserName: s.name("gh"), Email: s.name("gh") + "@example.test", RemoteID: s.name("remote-1")}, "github")
	if err != nil || id == 0 {
		t.Fatalf("get or create: %d %v", id, err)
	}
	again, err := o.GetOrCreateUser(ctx, &sectypes.UserContext{UserName: s.name("gh"), Email: s.name("gh") + "@example.test", RemoteID: s.name("remote-1")}, "github")
	if err != nil || again != id {
		t.Fatalf("second login must return the same user: %d %v", again, err)
	}

	exp := time.Now().Add(time.Hour)
	sess := lookup.OAuthSession{SessionToken: s.name("os1"), UserID: id, AccessToken: "a1", RefreshToken: s.name("or1"), TokenType: "Bearer", ExpiresAt: exp, Provider: "github"}
	if err := o.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	ref, err := o.GetByRefreshToken(ctx, sess.RefreshToken)
	if err != nil || ref.UserID != id || ref.AccessToken != "a1" {
		t.Fatalf("by refresh token: %+v %v", ref, err)
	}
	_, err = o.GetByRefreshToken(ctx, s.name("or-missing"))
	rejected(t, "unknown refresh token", err)
	if err := o.UpdateRefreshToken(ctx, id, sess.RefreshToken, s.name("os2"), "a2", s.name("or2"), exp); err != nil {
		t.Fatalf("update refresh token: %v", err)
	}
	if _, err := o.GetByRefreshToken(ctx, s.name("or2")); err != nil {
		t.Fatalf("rotated refresh token not found: %v", err)
	}
	u, err := o.GetUser(ctx, id)
	if err != nil || u.UserName != s.name("gh") {
		t.Fatalf("get user: %+v %v", u, err)
	}
	_, err = o.GetUser(ctx, id+1_000_000)
	rejected(t, "unknown user", err)
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func (s *suite) passkey(t *testing.T) {
	p := s.Provider.Passkey
	reg := s.register(t, "pk")
	uid := reg.User.UserID
	c1, c2 := b64(s.name("cred1")), b64(s.name("cred2"))

	rec := lookup.PasskeyCredentialRecord{UserID: uid, CredentialID: c1, PublicKey: b64("pubkey"), AttestationType: "none",
		Transports: []string{"usb", "nfc"}, Name: "Key 1"}
	if id, err := p.Store(ctx, rec); err != nil || id == 0 {
		t.Fatalf("store: %d %v", id, err)
	}
	_, err := p.Store(ctx, rec)
	rejected(t, "duplicate credential", err)
	rec.CredentialID, rec.Name = c2, "Key 2"
	if _, err := p.Store(ctx, rec); err != nil {
		t.Fatal(err)
	}

	owner, count, err := p.Get(ctx, c1)
	if err != nil || owner != uid || count != 0 {
		t.Fatalf("get: %d %d %v", owner, count, err)
	}
	_, _, err = p.Get(ctx, b64(s.name("missing")))
	rejected(t, "unknown credential", err)

	if clone, err := p.UpdateCounter(ctx, c1, 5); err != nil || clone {
		t.Fatalf("advance counter: clone=%v %v", clone, err)
	}
	if clone, err := p.UpdateCounter(ctx, c1, 5); err != nil || !clone {
		t.Fatalf("replayed counter must raise a clone warning: clone=%v %v", clone, err)
	}

	list, err := p.List(ctx, uid)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if err := p.Rename(ctx, uid, c1, "Renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	rejected(t, "renaming another user's credential", p.Rename(ctx, uid+1_000_000, c1, "x"))

	gotID, refs, err := p.ByUsername(ctx, s.name("pk"))
	if err != nil || gotID != uid || len(refs) != 2 {
		t.Fatalf("by username: %d %+v %v", gotID, refs, err)
	}
	_, _, err = p.ByUsername(ctx, s.name("ghost"))
	rejected(t, "unknown username", err)

	resp, err := p.Login(ctx, uid, map[string]any{"ip_address": "10.0.0.3"})
	if err != nil || resp.Token == "" || resp.User.UserName != s.name("pk") {
		t.Fatalf("passkey login: %+v %v", resp, err)
	}
	if _, err := s.Provider.Auth.Session(ctx, resp.Token, ""); err != nil {
		t.Fatalf("session from passkey login: %v", err)
	}

	rejected(t, "deleting another user's credential", p.Delete(ctx, uid+1_000_000, c1))
	if err := p.Delete(ctx, uid, c1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rejected(t, "deleting twice", p.Delete(ctx, uid, c1))
}

func (s *suite) totp(t *testing.T) {
	st := s.Provider.TOTP
	uid := s.register(t, "totp").User.UserID

	if on, err := st.Status(ctx, uid); err != nil || on {
		t.Fatalf("initial status: %v %v", on, err)
	}
	_, err := st.Secret(ctx, uid)
	rejected(t, "secret without 2FA", err)

	if err := st.Enable(ctx, uid, "SECRET", []string{s.name("h1"), s.name("h2")}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if on, _ := st.Status(ctx, uid); !on {
		t.Fatal("not enabled")
	}
	if sec, err := st.Secret(ctx, uid); err != nil || sec != "SECRET" {
		t.Fatalf("secret: %q %v", sec, err)
	}

	if ok, err := st.ValidateBackupCode(ctx, uid, s.name("h1")); err != nil || !ok {
		t.Fatalf("backup code: %v %v", ok, err)
	}
	ok, err := st.ValidateBackupCode(ctx, uid, s.name("h1"))
	notOK(t, "backup code reuse", ok, err)
	ok, err = st.ValidateBackupCode(ctx, uid, s.name("nope"))
	notOK(t, "unknown backup code", ok, err)

	if err := st.RegenerateBackupCodes(ctx, uid, []string{s.name("n1")}); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	ok, err = st.ValidateBackupCode(ctx, uid, s.name("h2"))
	notOK(t, "old backup code after regenerate", ok, err)
	if ok, err := st.ValidateBackupCode(ctx, uid, s.name("n1")); err != nil || !ok {
		t.Fatalf("new backup code: %v %v", ok, err)
	}

	if err := st.Disable(ctx, uid); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if on, _ := st.Status(ctx, uid); on {
		t.Fatal("still enabled after disable")
	}
}

// seed inserts one row with dialect placeholders. Values are bound, booleans converted.
func (s *suite) seed(t *testing.T, table string, cols []string, vals ...any) {
	t.Helper()
	ph := make([]string, len(vals))
	args := make([]any, len(vals))
	for i, v := range vals {
		ph[i] = s.Dialect.Placeholder(i + 1)
		if b, ok := v.(bool); ok {
			v = s.Dialect.Bool(b)
		}
		args[i] = v
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(ph, ", ")) //nolint:gosec // test seeding with fixed table names
	if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func (s *suite) policy(t *testing.T) {
	p := s.Provider.Policy
	u1 := s.register(t, "pol1").User.UserID
	u2 := s.register(t, "pol2").User.UserID
	group := 7_000_000 + u1
	schema, users, orders, secret := s.name("pub"), "Users", "orders", "secret"

	s.seed(t, "sec_group_members", []string{"group_id", "user_id"}, group, u1)
	colCols := []string{"user_id", "group_id", "schema_name", "table_name", "column_path", "access_type", "is_active"}
	s.seed(t, "sec_column_rules", colCols, u1, nil, schema, users, "email", "mask", true)
	s.seed(t, "sec_column_rules", colCols, nil, group, schema, strings.ToLower(users), "profile.ssn", "hide", true)
	s.seed(t, "sec_column_rules", colCols, u2, nil, schema, strings.ToLower(users), "other", "hide", true)
	s.seed(t, "sec_column_rules", colCols, u1, nil, schema, strings.ToLower(users), "inactive", "hide", false)
	s.seed(t, "sec_column_rules", colCols, u1, nil, schema, orders, "x", "hide", true)
	s.seed(t, "sec_column_rules", colCols, u1, nil, schema, "users_archive", "y", "hide", true)

	rules, err := p.ColumnSecurity(ctx, u1, schema, "users")
	if err != nil || len(rules) != 2 {
		t.Fatalf("column rules (user + group, exact table, active only): %d %v %+v", len(rules), err, rules)
	}
	paths := map[string]bool{}
	for i := range rules {
		paths[strings.Join(rules[i].Path, ".")] = true
	}
	if !paths["email"] || !paths["profile.ssn"] {
		t.Fatalf("paths: %v", paths)
	}
	if r, err := p.ColumnSecurity(ctx, u2, schema, "users"); err != nil || len(r) != 1 {
		t.Fatalf("other user's rules: %d %v", len(r), err)
	}
	if r, err := p.ColumnSecurity(ctx, u1+u2+1_000_000, schema, "users"); err != nil || len(r) != 0 {
		t.Fatalf("no rules must be empty, not an error: %d %v", len(r), err)
	}

	rowCols := []string{"user_id", "group_id", "schema_name", "table_name", "template", "has_block", "is_active"}
	s.seed(t, "sec_row_rules", rowCols, u1, nil, schema, orders, "owner_id = {UserID}", false, true)
	s.seed(t, "sec_row_rules", rowCols, nil, group, schema, orders, "region = 1", false, true)
	s.seed(t, "sec_row_rules", rowCols, nil, group, schema, orders, "ignored = 1", false, false)
	s.seed(t, "sec_row_rules", rowCols, u2, nil, schema, secret, nil, true, true)
	s.seed(t, "sec_row_rules", rowCols, nil, group, schema, secret, "x = 1", false, true)

	rs, err := p.RowSecurity(ctx, u1, schema, orders)
	if err != nil || rs.HasBlock || !strings.Contains(rs.Template, "owner_id = {UserID}") || !strings.Contains(rs.Template, "region = 1") || strings.Contains(rs.Template, "ignored") {
		t.Fatalf("row template: %+v %v", rs, err)
	}
	if rs, err := p.RowSecurity(ctx, u2, schema, secret); err != nil || !rs.HasBlock {
		t.Fatalf("blocking rule must win: %+v %v", rs, err)
	}
	if rs, err := p.RowSecurity(ctx, u1+u2+1_000_000, schema, orders); err != nil || rs.HasBlock || rs.Template != "" {
		t.Fatalf("no rules: %+v %v", rs, err)
	}
	if _, err := p.RowSecurity(ctx, "not-a-number", schema, orders); err == nil {
		t.Fatal("non-numeric user reference accepted (must fail closed)")
	}
}

func ptr[T any](v T) *T { return &v }
