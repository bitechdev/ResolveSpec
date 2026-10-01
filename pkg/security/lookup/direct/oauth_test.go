package direct

import (
	"context"
	"testing"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

func TestOAuthClientAndCodeFlow(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	b := newTestBase(t, db, nil)
	c := NewOAuthClients(b)

	reg, err := c.RegisterClient(ctx, &sectypes.OAuthServerClient{ClientID: "cid", RedirectURIs: []string{"https://a/cb"}, ClientName: "App"})
	if err != nil || reg.TokenEndpointAuthMethod != "none" || len(reg.GrantTypes) != 1 || len(reg.AllowedScopes) != 3 {
		t.Fatalf("%+v %v", reg, err)
	}
	got, err := c.GetClient(ctx, "cid")
	if err != nil || got.ClientName != "App" || got.RedirectURIs[0] != "https://a/cb" || got.ClientSecretHash != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := c.GetClient(ctx, "nope"); err == nil || err.Error() != "client not found" {
		t.Fatalf("got %v", err)
	}
	_, _ = db.Exec(`UPDATE oauth_clients SET is_active = 0`)
	if _, err := c.GetClient(ctx, "cid"); err == nil {
		t.Fatal("inactive client returned")
	}

	code := &sectypes.OAuthCode{Code: "c1", ClientID: "cid", RedirectURI: "https://a/cb", CodeChallenge: "ch",
		SessionToken: "st", Scopes: []string{"openid"}, ExpiresAt: time.Now().Add(time.Minute)}
	if err := c.SaveCode(ctx, code); err != nil {
		t.Fatal(err)
	}
	ex, err := c.ExchangeCode(ctx, "c1")
	if err != nil || ex.Code != "c1" || ex.CodeChallengeMethod != "S256" || ex.SessionToken != "st" || len(ex.Scopes) != 1 {
		t.Fatalf("%+v %v", ex, err)
	}
	if _, err := c.ExchangeCode(ctx, "c1"); err == nil || err.Error() != "invalid or expired code" {
		t.Fatalf("code reused: %v", err)
	}
	code.Code, code.ExpiresAt = "c2", time.Now().Add(-time.Minute)
	_ = c.SaveCode(ctx, code)
	if _, err := c.ExchangeCode(ctx, "c2"); err == nil {
		t.Fatal("expired code exchanged")
	}
}

func TestOAuthIntrospectRevoke(t *testing.T) {
	ctx := context.Background()
	a, db := newAuth(t, AuthOptions{})
	reg := registerUser(t, a, "oli")
	_, _ = db.Exec(`UPDATE users SET roles='r1,r2', user_level=3`)
	c := NewOAuthClients(a.Base)

	info, err := c.Introspect(ctx, reg.Token)
	if err != nil || !info.Active || info.Username != "oli" || info.UserLevel != 3 || len(info.Roles) != 2 || info.Exp == 0 || info.Iat == 0 || info.Sub != "1" {
		t.Fatalf("%+v %v", info, err)
	}
	if err := c.Revoke(ctx, reg.Token); err != nil {
		t.Fatal(err)
	}
	if info, err := c.Introspect(ctx, reg.Token); err != nil || info.Active {
		t.Fatalf("%+v %v", info, err)
	}
	if err := c.Revoke(ctx, "unknown"); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthUsers(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	o := NewOAuthUsers(newTestBase(t, db, nil))

	id, err := o.GetOrCreateUser(ctx, &sectypes.UserContext{UserName: "gh", Email: "g@x.io", RemoteID: "r-1", Roles: []string{"a"}}, "github")
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	// second login: same user; existing remote_id/auth_provider are kept
	id2, err := o.GetOrCreateUser(ctx, &sectypes.UserContext{UserName: "gh", Email: "g@x.io", RemoteID: "r-2"}, "google")
	if err != nil || id2 != id {
		t.Fatalf("%d %v", id2, err)
	}
	var remote, prov string
	_ = db.QueryRow(`SELECT remote_id, auth_provider FROM users WHERE id=?`, id).Scan(&remote, &prov)
	if remote != "r-1" || prov != "github" {
		t.Fatalf("overwrote: %q %q", remote, prov)
	}

	exp := time.Now().Add(time.Hour)
	s := lookup.OAuthSession{SessionToken: "s1", UserID: id, AccessToken: "a1", RefreshToken: "r1", TokenType: "Bearer", ExpiresAt: exp, Provider: "github"}
	if err := o.CreateSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.AccessToken = "a1b" // same token: updated, not duplicated
	if err := o.CreateSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_sessions`).Scan(&n)
	if n != 1 {
		t.Fatalf("sessions: %d", n)
	}

	ref, err := o.GetByRefreshToken(ctx, "r1")
	if err != nil || ref.UserID != id || ref.AccessToken != "a1b" || ref.TokenType != "Bearer" || ref.Expiry.IsZero() {
		t.Fatalf("%+v %v", ref, err)
	}
	if _, err := o.GetByRefreshToken(ctx, "zzz"); err == nil {
		t.Fatal("unknown refresh token accepted")
	}
	if err := o.UpdateRefreshToken(ctx, id, "r1", "s2", "a2", "r2", exp); err != nil {
		t.Fatal(err)
	}
	if err := o.UpdateRefreshToken(ctx, id, "r1", "s3", "a3", "r3", exp); err == nil || err.Error() != "session not found" {
		t.Fatalf("got %v", err)
	}
	u, err := o.GetUser(ctx, id)
	if err != nil || u.UserName != "gh" || u.UserID != id {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := o.GetUser(ctx, 999); err == nil || err.Error() != "user not found" {
		t.Fatalf("got %v", err)
	}
}
