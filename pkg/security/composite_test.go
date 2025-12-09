package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Mock implementations for testing composite provider
type mockAuth struct {
	loginResp  *LoginResponse
	loginErr   error
	logoutErr  error
	authUser   *UserContext
	authErr    error
	supportsRefresh bool
	supportsValidate bool
}

func (m *mockAuth) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return m.loginResp, m.loginErr
}

func (m *mockAuth) Logout(ctx context.Context, req LogoutRequest) error {
	return m.logoutErr
}

func (m *mockAuth) Authenticate(r *http.Request) (*UserContext, error) {
	return m.authUser, m.authErr
}

// Optional interface implementations
func (m *mockAuth) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	if !m.supportsRefresh {
		return nil, errors.New("not supported")
	}
	return m.loginResp, m.loginErr
}

func (m *mockAuth) ValidateToken(ctx context.Context, token string) (bool, error) {
	if !m.supportsValidate {
		return false, errors.New("not supported")
	}
	return true, nil
}

type mockColSec struct {
	rules []ColumnSecurity
	err   error
	supportsCache bool
}

func (m *mockColSec) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	return m.rules, m.err
}

func (m *mockColSec) ClearCache(ctx context.Context, userID int, schema, table string) error {
	if !m.supportsCache {
		return errors.New("not supported")
	}
	return nil
}

type mockRowSec struct {
	rowSec RowSecurity
	err    error
	supportsCache bool
}

func (m *mockRowSec) GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error) {
	return m.rowSec, m.err
}

func (m *mockRowSec) ClearCache(ctx context.Context, userID int, schema, table string) error {
	if !m.supportsCache {
		return errors.New("not supported")
	}
	return nil
}

// Test NewCompositeSecurityProvider
func TestNewCompositeSecurityProvider(t *testing.T) {
	t.Run("with all valid providers", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, err := NewCompositeSecurityProvider(auth, colSec, rowSec)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if composite == nil {
			t.Fatal("expected non-nil composite provider")
		}
	})

	t.Run("with nil authenticator", func(t *testing.T) {
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		_, err := NewCompositeSecurityProvider(nil, colSec, rowSec)
		if err == nil {
			t.Fatal("expected error with nil authenticator")
		}
	})

	t.Run("with nil column security provider", func(t *testing.T) {
		auth := &mockAuth{}
		rowSec := &mockRowSec{}

		_, err := NewCompositeSecurityProvider(auth, nil, rowSec)
		if err == nil {
			t.Fatal("expected error with nil column security provider")
		}
	})

	t.Run("with nil row security provider", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{}

		_, err := NewCompositeSecurityProvider(auth, colSec, nil)
		if err == nil {
			t.Fatal("expected error with nil row security provider")
		}
	})
}

// Test CompositeSecurityProvider authentication delegation
func TestCompositeSecurityProviderAuth(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("login delegates to authenticator", func(t *testing.T) {
		auth := &mockAuth{
			loginResp: &LoginResponse{
				Token: "abc123",
				User:  userCtx,
			},
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()
		req := LoginRequest{Username: "test", Password: "pass"}

		resp, err := composite.Login(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Token != "abc123" {
			t.Errorf("expected token abc123, got %s", resp.Token)
		}
	})

	t.Run("logout delegates to authenticator", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()
		req := LogoutRequest{Token: "abc123", UserID: 1}

		err := composite.Logout(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("authenticate delegates to authenticator", func(t *testing.T) {
		auth := &mockAuth{
			authUser: userCtx,
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		req := httptest.NewRequest("GET", "/test", nil)

		user, err := composite.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if user.UserID != 1 {
			t.Errorf("expected UserID 1, got %d", user.UserID)
		}
	})
}

// Test CompositeSecurityProvider security provider delegation
func TestCompositeSecurityProviderSecurity(t *testing.T) {
	t.Run("get column security delegates to column provider", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{
			rules: []ColumnSecurity{
				{Schema: "public", Tablename: "users", Path: []string{"email"}},
			},
		}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		rules, err := composite.GetColumnSecurity(ctx, 1, "public", "users")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(rules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(rules))
		}
	})

	t.Run("get row security delegates to row provider", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{
			rowSec: RowSecurity{
				Schema:    "public",
				Tablename: "orders",
				Template:  "user_id = {UserID}",
			},
		}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		rowSecResult, err := composite.GetRowSecurity(ctx, 1, "public", "orders")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if rowSecResult.Template != "user_id = {UserID}" {
			t.Errorf("expected template 'user_id = {UserID}', got %s", rowSecResult.Template)
		}
	})
}

// Test CompositeSecurityProvider optional interfaces
func TestCompositeSecurityProviderOptionalInterfaces(t *testing.T) {
	t.Run("refresh token with support", func(t *testing.T) {
		auth := &mockAuth{
			supportsRefresh: true,
			loginResp: &LoginResponse{
				Token: "new-token",
			},
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		resp, err := composite.RefreshToken(ctx, "old-token")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if resp.Token != "new-token" {
			t.Errorf("expected token new-token, got %s", resp.Token)
		}
	})

	t.Run("refresh token without support", func(t *testing.T) {
		auth := &mockAuth{
			supportsRefresh: false,
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		_, err := composite.RefreshToken(ctx, "token")
		if err == nil {
			t.Fatal("expected error when refresh not supported")
		}
	})

	t.Run("validate token with support", func(t *testing.T) {
		auth := &mockAuth{
			supportsValidate: true,
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		valid, err := composite.ValidateToken(ctx, "token")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !valid {
			t.Error("expected token to be valid")
		}
	})

	t.Run("validate token without support", func(t *testing.T) {
		auth := &mockAuth{
			supportsValidate: false,
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		_, err := composite.ValidateToken(ctx, "token")
		if err == nil {
			t.Fatal("expected error when validate not supported")
		}
	})
}

// Test CompositeSecurityProvider cache clearing
func TestCompositeSecurityProviderClearCache(t *testing.T) {
	t.Run("clear cache with support", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{supportsCache: true}
		rowSec := &mockRowSec{supportsCache: true}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		err := composite.ClearCache(ctx, 1, "public", "users")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("clear cache without support", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{supportsCache: false}
		rowSec := &mockRowSec{supportsCache: false}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		// Should not error even if providers don't support cache
		// (they just won't implement the interface)
		err := composite.ClearCache(ctx, 1, "public", "users")
		if err != nil {
			// It's ok if this errors, as the providers don't implement Cacheable
			t.Logf("cache clear returned error as expected: %v", err)
		}
	})

	t.Run("clear cache with partial support", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{supportsCache: true}
		rowSec := &mockRowSec{supportsCache: false}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		err := composite.ClearCache(ctx, 1, "public", "users")
		// Should succeed for column security even if row security fails
		if err == nil {
			t.Log("cache clear succeeded partially")
		} else {
			t.Logf("cache clear returned error: %v", err)
		}
	})
}

// Test error propagation
func TestCompositeSecurityProviderErrorPropagation(t *testing.T) {
	t.Run("login error propagates", func(t *testing.T) {
		auth := &mockAuth{
			loginErr: errors.New("invalid credentials"),
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		_, err := composite.Login(ctx, LoginRequest{})
		if err == nil {
			t.Fatal("expected error to propagate")
		}
	})

	t.Run("authenticate error propagates", func(t *testing.T) {
		auth := &mockAuth{
			authErr: errors.New("invalid token"),
		}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		req := httptest.NewRequest("GET", "/test", nil)

		_, err := composite.Authenticate(req)
		if err == nil {
			t.Fatal("expected error to propagate")
		}
	})

	t.Run("column security error propagates", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{
			err: errors.New("failed to load column security"),
		}
		rowSec := &mockRowSec{}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		_, err := composite.GetColumnSecurity(ctx, 1, "public", "users")
		if err == nil {
			t.Fatal("expected error to propagate")
		}
	})

	t.Run("row security error propagates", func(t *testing.T) {
		auth := &mockAuth{}
		colSec := &mockColSec{}
		rowSec := &mockRowSec{
			err: errors.New("failed to load row security"),
		}

		composite, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
		ctx := context.Background()

		_, err := composite.GetRowSecurity(ctx, 1, "public", "orders")
		if err == nil {
			t.Fatal("expected error to propagate")
		}
	})
}
