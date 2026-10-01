package providers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Test HeaderAuthenticator
func TestHeaderAuthenticator(t *testing.T) {
	auth := NewHeaderAuthenticator()

	t.Run("successful authentication", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-User-ID", "123")
		req.Header.Set("X-User-Name", "testuser")
		req.Header.Set("X-User-Level", "5")
		req.Header.Set("X-Session-ID", "session123")
		req.Header.Set("X-Remote-ID", "remote456")
		req.Header.Set("X-User-Email", "test@example.com")
		req.Header.Set("X-User-Roles", "admin,user")

		userCtx, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if userCtx.UserID != 123 {
			t.Errorf("expected UserID 123, got %d", userCtx.UserID)
		}
		if userCtx.UserName != "testuser" {
			t.Errorf("expected UserName testuser, got %s", userCtx.UserName)
		}
		if userCtx.UserLevel != 5 {
			t.Errorf("expected UserLevel 5, got %d", userCtx.UserLevel)
		}
		if userCtx.SessionID != "session123" {
			t.Errorf("expected SessionID session123, got %s", userCtx.SessionID)
		}
		if userCtx.Email != "test@example.com" {
			t.Errorf("expected Email test@example.com, got %s", userCtx.Email)
		}
		if len(userCtx.Roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(userCtx.Roles))
		}
	})

	t.Run("missing user ID header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-User-Name", "testuser")

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error when X-User-ID is missing")
		}
	})

	t.Run("invalid user ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-User-ID", "invalid")

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error with invalid user ID")
		}
	})

	t.Run("login not supported", func(t *testing.T) {
		ctx := context.Background()
		req := sectypes.LoginRequest{Username: "test", Password: "pass"}

		_, err := auth.Login(ctx, req)
		if err == nil {
			t.Fatal("expected error for unsupported login")
		}
	})

	t.Run("logout always succeeds", func(t *testing.T) {
		ctx := context.Background()
		req := sectypes.LogoutRequest{Token: "token", UserID: 1}

		err := auth.Logout(ctx, req)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

// Test ConfigColumnSecurityProvider
func TestConfigColumnSecurityProvider(t *testing.T) {
	rules := map[string][]sectypes.ColumnSecurity{
		"public.users": {
			{
				Schema:     "public",
				Tablename:  "users",
				Path:       []string{"email"},
				Accesstype: "mask",
			},
		},
	}

	provider := NewConfigColumnSecurityProvider(rules)
	ctx := context.Background()

	t.Run("get existing rules", func(t *testing.T) {
		result, err := provider.GetColumnSecurity(ctx, 1, "public", "users")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result) != 1 {
			t.Errorf("expected 1 rule, got %d", len(result))
		}
	})

	t.Run("get non-existent rules returns empty", func(t *testing.T) {
		result, err := provider.GetColumnSecurity(ctx, 1, "public", "orders")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result) != 0 {
			t.Errorf("expected 0 rules, got %d", len(result))
		}
	})
}

// Test ConfigRowSecurityProvider
func TestConfigRowSecurityProvider(t *testing.T) {
	templates := map[string]string{
		"public.orders": "user_id = {UserID}",
	}
	blocked := map[string]bool{
		"public.secrets": true,
	}

	provider := NewConfigRowSecurityProvider(templates, blocked)
	ctx := context.Background()

	t.Run("get template for allowed table", func(t *testing.T) {
		result, err := provider.GetRowSecurity(ctx, 1, "public", "orders")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if result.Template != "user_id = {UserID}" {
			t.Errorf("expected template 'user_id = {UserID}', got %s", result.Template)
		}
		if result.HasBlock {
			t.Error("expected HasBlock to be false")
		}
	})

	t.Run("get blocked table", func(t *testing.T) {
		result, err := provider.GetRowSecurity(ctx, 1, "public", "secrets")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !result.HasBlock {
			t.Error("expected HasBlock to be true")
		}
	})

	t.Run("get non-existent table returns empty template", func(t *testing.T) {
		result, err := provider.GetRowSecurity(ctx, 1, "public", "unknown")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if result.Template != "" {
			t.Errorf("expected empty template, got %s", result.Template)
		}
		if result.HasBlock {
			t.Error("expected HasBlock to be false")
		}
	})
}
