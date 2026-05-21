package security

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubAuthenticator is a configurable Authenticator for testing.
type stubAuthenticator struct {
	userCtx *UserContext
	err     error
}

func (s *stubAuthenticator) Authenticate(_ *http.Request) (*UserContext, error) {
	return s.userCtx, s.err
}

func (s *stubAuthenticator) Login(_ context.Context, _ LoginRequest) (*LoginResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &LoginResponse{Token: "tok"}, nil
}

func (s *stubAuthenticator) Logout(_ context.Context, _ LogoutRequest) error {
	return s.err
}

func TestChainAuthenticator_Authenticate(t *testing.T) {
	successCtx := &UserContext{UserID: 42, UserName: "alice"}
	failStub := &stubAuthenticator{err: fmt.Errorf("no token")}
	okStub := &stubAuthenticator{userCtx: successCtx}

	t.Run("primary succeeds", func(t *testing.T) {
		chain := NewChainAuthenticator(okStub, failStub)
		req := httptest.NewRequest("GET", "/", nil)

		uc, err := chain.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if uc.UserID != 42 {
			t.Errorf("expected UserID 42, got %d", uc.UserID)
		}
	})

	t.Run("primary fails, secondary succeeds", func(t *testing.T) {
		chain := NewChainAuthenticator(failStub, okStub)
		req := httptest.NewRequest("GET", "/", nil)

		uc, err := chain.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if uc.UserID != 42 {
			t.Errorf("expected UserID 42, got %d", uc.UserID)
		}
	})

	t.Run("all fail", func(t *testing.T) {
		chain := NewChainAuthenticator(failStub, failStub)
		req := httptest.NewRequest("GET", "/", nil)

		_, err := chain.Authenticate(req)
		if err == nil {
			t.Fatal("expected error when all authenticators fail")
		}
	})

	t.Run("three in chain, first two fail", func(t *testing.T) {
		chain := NewChainAuthenticator(failStub, failStub, okStub)
		req := httptest.NewRequest("GET", "/", nil)

		uc, err := chain.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if uc.UserName != "alice" {
			t.Errorf("expected UserName alice, got %s", uc.UserName)
		}
	})
}

func TestChainAuthenticator_LoginLogout(t *testing.T) {
	primary := &stubAuthenticator{userCtx: &UserContext{UserID: 1}}
	secondary := &stubAuthenticator{userCtx: &UserContext{UserID: 2}}
	chain := NewChainAuthenticator(primary, secondary)
	ctx := context.Background()

	t.Run("login delegates to primary", func(t *testing.T) {
		resp, err := chain.Login(ctx, LoginRequest{Username: "u", Password: "p"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Token != "tok" {
			t.Errorf("expected token from primary, got %s", resp.Token)
		}
	})

	t.Run("logout delegates to primary", func(t *testing.T) {
		if err := chain.Logout(ctx, LogoutRequest{}); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("login error from primary is returned", func(t *testing.T) {
		failPrimary := &stubAuthenticator{err: fmt.Errorf("db down")}
		chain2 := NewChainAuthenticator(failPrimary, secondary)
		_, err := chain2.Login(ctx, LoginRequest{})
		if err == nil {
			t.Fatal("expected error from primary login failure")
		}
	})
}
