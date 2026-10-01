package procedure

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

func newMock(t *testing.T) (*DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewDB(db, nil, nil), mock
}

func q(s string) string { return regexp.QuoteMeta(s) }

func TestAuthLoginCallsProcedureWithJSON(t *testing.T) {
	run, mock := newMock(t)
	procs := lookup.DefaultProcNames()
	procs.Login = "custom_login"
	a := NewAuth(run, procs)

	mock.ExpectQuery(q("SELECT p_success, p_error, p_data::text FROM custom_login($1::jsonb)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(true, nil, `{"token":"sess_1","user":{"user_id":7,"user_name":"bob"}}`))

	resp, err := a.Login(context.Background(), sectypes.LoginRequest{Username: "bob", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Token != "sess_1" || resp.User == nil || resp.User.UserID != 7 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthLoginFailureUsesProcedureMessage(t *testing.T) {
	run, mock := newMock(t)
	a := NewAuth(run, lookup.DefaultProcNames())

	mock.ExpectQuery("resolvespec_login").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(false, "bad credentials", nil))
	if _, err := a.Login(context.Background(), sectypes.LoginRequest{}); err == nil || err.Error() != "bad credentials" {
		t.Fatalf("got %v", err)
	}

	mock.ExpectQuery("resolvespec_login").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(false, nil, nil))
	if _, err := a.Login(context.Background(), sectypes.LoginRequest{}); err == nil || err.Error() == "" {
		t.Fatalf("expected default error, got %v", err)
	}
}

func TestRunnerReconnectsOnClosedDB(t *testing.T) {
	first, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	reconnected := false
	run := NewDB(first, func() (*sql.DB, error) { return second, nil }, func() { reconnected = true })
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))

	err = run.Run(func(db *sql.DB) error {
		var x int
		return db.QueryRow("SELECT 1").Scan(&x)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reconnected || run.Get() != second {
		t.Fatal("expected reconnect to the new handle")
	}
}

func TestRunnerNoFactoryReturnsClosedError(t *testing.T) {
	db, _, _ := sqlmock.New()
	_ = db.Close()
	err := NewDB(db, nil, nil).Run(func(db *sql.DB) error { return db.QueryRow("SELECT 1").Scan(new(int)) })
	if !IsClosed(err) {
		t.Fatalf("got %v", err)
	}
}

func TestPasskeyGetDecodesCredentialID(t *testing.T) {
	run, mock := newMock(t)
	p := NewPasskey(run, lookup.DefaultProcNames())
	raw := []byte{1, 2, 3, 4}

	mock.ExpectQuery("resolvespec_passkey_get_credential").WithArgs(raw).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_credential"}).
			AddRow(true, nil, `{"user_id":9,"sign_count":4}`))
	uid, count, err := p.Get(context.Background(), base64.StdEncoding.EncodeToString(raw))
	if err != nil || uid != 9 || count != 4 {
		t.Fatalf("got %d %d %v", uid, count, err)
	}
}

func TestPasskeyInvalidBase64(t *testing.T) {
	run, _ := newMock(t)
	p := NewPasskey(run, lookup.DefaultProcNames())
	if _, _, err := p.Get(context.Background(), "***"); err == nil {
		t.Fatal("expected error")
	}
	if err := p.Delete(context.Background(), 1, "***"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPasskeyUpdateCounterReportsCloneWarning(t *testing.T) {
	run, mock := newMock(t)
	p := NewPasskey(run, lookup.DefaultProcNames())
	id := base64.StdEncoding.EncodeToString([]byte("abc"))

	mock.ExpectQuery("resolvespec_passkey_update_counter").WithArgs([]byte("abc"), uint32(5)).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_clone_warning"}).AddRow(true, nil, true))
	warn, err := p.UpdateCounter(context.Background(), id, 5)
	if err != nil || !warn {
		t.Fatalf("got %v %v", warn, err)
	}

	mock.ExpectQuery("resolvespec_passkey_update_counter").WillReturnError(errors.New("boom"))
	if _, err := p.UpdateCounter(context.Background(), id, 6); err == nil {
		t.Fatal("expected error")
	}
}

func TestPasskeyByUsername(t *testing.T) {
	run, mock := newMock(t)
	p := NewPasskey(run, lookup.DefaultProcNames())
	mock.ExpectQuery("resolvespec_passkey_get_credentials_by_username").WithArgs("bob").
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_user_id", "p_credentials"}).
			AddRow(true, nil, 3, `[{"credential_id":"YWJj","transports":["usb"]}]`))
	uid, refs, err := p.ByUsername(context.Background(), "bob")
	if err != nil || uid != 3 || len(refs) != 1 || refs[0].CredentialID != "YWJj" || refs[0].Transports[0] != "usb" {
		t.Fatalf("got %d %+v %v", uid, refs, err)
	}
}

func TestOAuthUsersGetOrCreateUser(t *testing.T) {
	run, mock := newMock(t)
	o := NewOAuthUsers(run, lookup.DefaultProcNames())

	mock.ExpectQuery("resolvespec_oauth_getorcreateuser").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_user_id"}).AddRow(true, nil, 11))
	id, err := o.GetOrCreateUser(context.Background(), &sectypes.UserContext{UserName: "u", Email: "e"}, "github")
	if err != nil || id != 11 {
		t.Fatalf("got %d %v", id, err)
	}

	mock.ExpectQuery("resolvespec_oauth_getorcreateuser").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_user_id"}).AddRow(true, nil, nil))
	if _, err := o.GetOrCreateUser(context.Background(), &sectypes.UserContext{}, "github"); err == nil || err.Error() != "user ID not returned" {
		t.Fatalf("got %v", err)
	}
}

func TestOAuthUsersRefreshRoundTrip(t *testing.T) {
	run, mock := newMock(t)
	o := NewOAuthUsers(run, lookup.DefaultProcNames())

	mock.ExpectQuery("resolvespec_oauth_getrefreshtoken").WithArgs("r1").
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(true, nil, `{"user_id":2,"access_token":"a","token_type":"Bearer","expiry":"2030-01-01T00:00:00Z"}`))
	s, err := o.GetByRefreshToken(context.Background(), "r1")
	if err != nil || s.UserID != 2 || s.AccessToken != "a" {
		t.Fatalf("got %+v %v", s, err)
	}

	mock.ExpectQuery("resolvespec_oauth_updaterefreshtoken").WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error"}).AddRow(false, "session not found"))
	err = o.UpdateRefreshToken(context.Background(), 2, "r1", "s2", "a2", "r2", time.Now())
	if err == nil || err.Error() != "session not found" {
		t.Fatalf("got %v", err)
	}
}

func TestOAuthClientsExchangeCodeSetsCode(t *testing.T) {
	run, mock := newMock(t)
	c := NewOAuthClients(run, lookup.DefaultProcNames())
	mock.ExpectQuery("resolvespec_oauth_exchange_code").WithArgs("abc").
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(true, nil, `{"client_id":"cid"}`))
	code, err := c.ExchangeCode(context.Background(), "abc")
	if err != nil || code.Code != "abc" || code.ClientID != "cid" {
		t.Fatalf("got %+v %v", code, err)
	}

	mock.ExpectQuery("resolvespec_oauth_exchange_code").WithArgs("zzz").
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).AddRow(false, nil, nil))
	if _, err := c.ExchangeCode(context.Background(), "zzz"); err == nil || err.Error() != "invalid or expired code" {
		t.Fatalf("got %v", err)
	}
}

func TestOAuthClientsRevoke(t *testing.T) {
	run, mock := newMock(t)
	c := NewOAuthClients(run, lookup.DefaultProcNames())
	mock.ExpectQuery("resolvespec_oauth_revoke").WithArgs("t").
		WillReturnRows(sqlmock.NewRows([]string{"p_success", "p_error"}).AddRow(true, nil))
	if err := c.Revoke(context.Background(), "t"); err != nil {
		t.Fatal(err)
	}
}
