package security

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/bitechdev/ResolveSpec/pkg/cache"
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
		req := LoginRequest{Username: "test", Password: "pass"}

		_, err := auth.Login(ctx, req)
		if err == nil {
			t.Fatal("expected error for unsupported login")
		}
	})

	t.Run("logout always succeeds", func(t *testing.T) {
		ctx := context.Background()
		req := LogoutRequest{Token: "token", UserID: 1}

		err := auth.Logout(ctx, req)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

// Test parseRoles helper
func TestParseRoles(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single role",
			input:    "admin",
			expected: []string{"admin"},
		},
		{
			name:     "multiple roles",
			input:    "admin,user,moderator",
			expected: []string{"admin", "user", "moderator"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRoles(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d roles, got %d", len(tt.expected), len(result))
				return
			}
			for i, role := range tt.expected {
				if result[i] != role {
					t.Errorf("expected role[%d] = %s, got %s", i, role, result[i])
				}
			}
		})
	}
}

// Test parseIntHeader helper
func TestParseIntHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	t.Run("valid int header", func(t *testing.T) {
		req.Header.Set("X-Test-Int", "42")
		result := parseIntHeader(req, "X-Test-Int", 0)
		if result != 42 {
			t.Errorf("expected 42, got %d", result)
		}
	})

	t.Run("missing header returns default", func(t *testing.T) {
		result := parseIntHeader(req, "X-Missing", 99)
		if result != 99 {
			t.Errorf("expected default 99, got %d", result)
		}
	})

	t.Run("invalid int returns default", func(t *testing.T) {
		req.Header.Set("X-Invalid-Int", "not-a-number")
		result := parseIntHeader(req, "X-Invalid-Int", 10)
		if result != 10 {
			t.Errorf("expected default 10, got %d", result)
		}
	})
}

// Test DatabaseAuthenticator caching
func TestDatabaseAuthenticatorCaching(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	// Create a test cache instance
	cacheProvider := cache.NewMemoryProvider(&cache.Options{
		DefaultTTL: 1 * time.Minute,
		MaxSize:    1000,
	})
	testCache := cache.NewCache(cacheProvider)

	// Create authenticator with short cache TTL for testing
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{
		CacheTTL: 100 * time.Millisecond,
		Cache:    testCache,
	})

	t.Run("cache hit avoids database call", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer cached-token-123")

		// First call - should hit database
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":1,"user_name":"testuser","session_id":"cached-token-123"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("cached-token-123", "authenticate").
			WillReturnRows(rows)

		userCtx1, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("first authenticate failed: %v", err)
		}
		if userCtx1.UserID != 1 {
			t.Errorf("expected UserID 1, got %d", userCtx1.UserID)
		}

		// Second call - should use cache, no database call expected
		userCtx2, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("second authenticate failed: %v", err)
		}
		if userCtx2.UserID != 1 {
			t.Errorf("expected UserID 1, got %d", userCtx2.UserID)
		}

		// Verify no unexpected database calls
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("cache expiration triggers database call", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer expire-token-456")

		// First call - populate cache
		rows1 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":2,"user_name":"expireuser","session_id":"expire-token-456"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("expire-token-456", "authenticate").
			WillReturnRows(rows1)

		_, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("first authenticate failed: %v", err)
		}

		// Wait for cache to expire
		time.Sleep(150 * time.Millisecond)

		// Second call - cache expired, should hit database again
		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":2,"user_name":"expireuser","session_id":"expire-token-456"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("expire-token-456", "authenticate").
			WillReturnRows(rows2)

		_, err = auth.Authenticate(req)
		if err != nil {
			t.Fatalf("second authenticate after expiration failed: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("logout clears cache", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer logout-token-789")

		// First call - populate cache
		rows1 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":3,"user_name":"logoutuser","session_id":"logout-token-789"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("logout-token-789", "authenticate").
			WillReturnRows(rows1)

		_, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("authenticate failed: %v", err)
		}

		// Logout - should clear cache
		logoutRows := sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(true, nil, nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_data::text FROM resolvespec_logout`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(logoutRows)

		err = auth.Logout(context.Background(), LogoutRequest{
			Token:  "logout-token-789",
			UserID: 3,
		})
		if err != nil {
			t.Fatalf("logout failed: %v", err)
		}

		// Next authenticate should hit database again since cache was cleared
		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":3,"user_name":"logoutuser","session_id":"logout-token-789"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("logout-token-789", "authenticate").
			WillReturnRows(rows2)

		_, err = auth.Authenticate(req)
		if err != nil {
			t.Fatalf("authenticate after logout failed: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("manual cache clear", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer manual-clear-token")

		// Populate cache
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":4,"user_name":"clearuser","session_id":"manual-clear-token"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("manual-clear-token", "authenticate").
			WillReturnRows(rows)

		_, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("authenticate failed: %v", err)
		}

		// Manually clear cache
		auth.ClearCache("manual-clear-token")

		// Next call should hit database
		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":4,"user_name":"clearuser","session_id":"manual-clear-token"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("manual-clear-token", "authenticate").
			WillReturnRows(rows2)

		_, err = auth.Authenticate(req)
		if err != nil {
			t.Fatalf("authenticate after cache clear failed: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("clear user cache", func(t *testing.T) {
		// Populate cache with multiple tokens for the same user
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.Header.Set("Authorization", "Bearer user-token-1")

		rows1 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":5,"user_name":"multiuser","session_id":"user-token-1"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("user-token-1", "authenticate").
			WillReturnRows(rows1)

		_, err := auth.Authenticate(req1)
		if err != nil {
			t.Fatalf("first authenticate failed: %v", err)
		}

		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("Authorization", "Bearer user-token-2")

		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":5,"user_name":"multiuser","session_id":"user-token-2"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("user-token-2", "authenticate").
			WillReturnRows(rows2)

		_, err = auth.Authenticate(req2)
		if err != nil {
			t.Fatalf("second authenticate failed: %v", err)
		}

		// Clear all cache entries for user 5
		auth.ClearUserCache(5)

		// Both tokens should now require database calls
		rows3 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":5,"user_name":"multiuser","session_id":"user-token-1"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("user-token-1", "authenticate").
			WillReturnRows(rows3)

		_, err = auth.Authenticate(req1)
		if err != nil {
			t.Fatalf("authenticate after user cache clear failed: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test DatabaseAuthenticator
func TestDatabaseAuthenticator(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	auth := NewDatabaseAuthenticator(db)

	t.Run("successful login", func(t *testing.T) {
		ctx := context.Background()
		req := LoginRequest{
			Username: "testuser",
			Password: "password123",
		}

		// Mock the stored procedure call
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(true, nil, `{"token":"abc123","user":{"user_id":1,"user_name":"testuser"},"expires_in":86400}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_data::text FROM resolvespec_login`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(rows)

		resp, err := auth.Login(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.Token != "abc123" {
			t.Errorf("expected token abc123, got %s", resp.Token)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("failed login", func(t *testing.T) {
		ctx := context.Background()
		req := LoginRequest{
			Username: "testuser",
			Password: "wrongpass",
		}

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(false, "Invalid credentials", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_data::text FROM resolvespec_login`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(rows)

		_, err := auth.Login(ctx, req)
		if err == nil {
			t.Fatal("expected error for failed login")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("successful logout", func(t *testing.T) {
		ctx := context.Background()
		req := LogoutRequest{
			Token:  "abc123",
			UserID: 1,
		}

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_data"}).
			AddRow(true, nil, nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_data::text FROM resolvespec_logout`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(rows)

		err := auth.Logout(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate with bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token-123")

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":1,"user_name":"testuser","session_id":"test-token-123"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("test-token-123", "authenticate").
			WillReturnRows(rows)

		userCtx, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if userCtx.UserID != 1 {
			t.Errorf("expected UserID 1, got %d", userCtx.UserID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate with cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "session_token",
			Value: "cookie-token-456",
		})

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":2,"user_name":"cookieuser","session_id":"cookie-token-456"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("cookie-token-456", "cookie").
			WillReturnRows(rows)

		userCtx, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if userCtx.UserID != 2 {
			t.Errorf("expected UserID 2, got %d", userCtx.UserID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate missing token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error when token is missing")
		}
	})

	t.Run("authenticate with multiple comma-separated tokens", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Token invalid-token, Token valid-token-123")

		// First token fails
		rows1 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(false, "Invalid token", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("invalid-token", "authenticate").
			WillReturnRows(rows1)

		// Second token succeeds
		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":3,"user_name":"multitoken","session_id":"valid-token-123"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("valid-token-123", "authenticate").
			WillReturnRows(rows2)

		userCtx, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if userCtx.UserID != 3 {
			t.Errorf("expected UserID 3, got %d", userCtx.UserID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate with duplicate tokens", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Token 968CA5AE-4F83-4D55-A3C6-51AE4410E03A, Token 968CA5AE-4F83-4D55-A3C6-51AE4410E03A")

		// First token succeeds
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":4,"user_name":"duplicateuser","session_id":"968CA5AE-4F83-4D55-A3C6-51AE4410E03A"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("968CA5AE-4F83-4D55-A3C6-51AE4410E03A", "authenticate").
			WillReturnRows(rows)

		userCtx, err := auth.Authenticate(req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if userCtx.UserID != 4 {
			t.Errorf("expected UserID 4, got %d", userCtx.UserID)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate with all tokens failing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Token bad-token-1, Token bad-token-2")

		// First token fails
		rows1 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(false, "Invalid token", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("bad-token-1", "authenticate").
			WillReturnRows(rows1)

		// Second token also fails
		rows2 := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(false, "Invalid token", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs("bad-token-2", "authenticate").
			WillReturnRows(rows2)

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error when all tokens fail")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test DatabaseAuthenticator RefreshToken
func TestDatabaseAuthenticatorRefreshToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	auth := NewDatabaseAuthenticator(db)
	ctx := context.Background()

	t.Run("successful token refresh", func(t *testing.T) {
		refreshToken := "refresh-token-123"

		// First call to validate refresh token
		sessionRows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":1,"user_name":"testuser"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs(refreshToken, "refresh").
			WillReturnRows(sessionRows)

		// Second call to generate new token
		refreshRows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, `{"user_id":1,"user_name":"testuser","session_id":"new-token-456"}`)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_refresh_token`).
			WithArgs(refreshToken, sqlmock.AnyArg()).
			WillReturnRows(refreshRows)

		resp, err := auth.RefreshToken(ctx, refreshToken)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.Token != "new-token-456" {
			t.Errorf("expected token new-token-456, got %s", resp.Token)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		refreshToken := "invalid-token"

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(false, "Invalid refresh token", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_user::text FROM resolvespec_session`).
			WithArgs(refreshToken, "refresh").
			WillReturnRows(rows)

		_, err := auth.RefreshToken(ctx, refreshToken)
		if err == nil {
			t.Fatal("expected error for invalid refresh token")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test JWTAuthenticator
func TestJWTAuthenticator(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	auth := NewJWTAuthenticator("secret-key", db)

	t.Run("successful login", func(t *testing.T) {
		ctx := context.Background()
		req := LoginRequest{
			Username: "testuser",
			Password: "password123",
		}

		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user"}).
			AddRow(true, nil, []byte(`{"id":1,"username":"testuser","email":"test@example.com","user_level":5,"roles":"admin,user"}`))

		mock.ExpectQuery(`SELECT p_success, p_error, p_user FROM resolvespec_jwt_login`).
			WithArgs("testuser", "password123").
			WillReturnRows(rows)

		resp, err := auth.Login(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if resp.User.UserID != 1 {
			t.Errorf("expected UserID 1, got %d", resp.User.UserID)
		}
		if resp.User.UserName != "testuser" {
			t.Errorf("expected UserName testuser, got %s", resp.User.UserName)
		}
		if len(resp.User.Roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(resp.User.Roles))
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("authenticate returns not implemented", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error for unimplemented JWT parsing")
		}
	})

	t.Run("authenticate missing bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)

		_, err := auth.Authenticate(req)
		if err == nil {
			t.Fatal("expected error when authorization header is missing")
		}
	})

	t.Run("successful logout", func(t *testing.T) {
		ctx := context.Background()
		req := LogoutRequest{
			Token:  "token123",
			UserID: 1,
		}

		rows := sqlmock.NewRows([]string{"p_success", "p_error"}).
			AddRow(true, nil)

		mock.ExpectQuery(`SELECT p_success, p_error FROM resolvespec_jwt_logout`).
			WithArgs("token123", 1).
			WillReturnRows(rows)

		err := auth.Logout(ctx, req)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test DatabaseColumnSecurityProvider
func TestDatabaseColumnSecurityProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabaseColumnSecurityProvider(db)
	ctx := context.Background()

	t.Run("load column security successfully", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_rules"}).
			AddRow(true, nil, []byte(`[{"control":"public.users.email","accesstype":"mask","jsonvalue":""}]`))

		mock.ExpectQuery(`SELECT p_success, p_error, p_rules FROM resolvespec_column_security`).
			WithArgs(1, "public", "users").
			WillReturnRows(rows)

		rules, err := provider.GetColumnSecurity(ctx, 1, "public", "users")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(rules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(rules))
		}
		if rules[0].Accesstype != "mask" {
			t.Errorf("expected accesstype mask, got %s", rules[0].Accesstype)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("failed to load column security", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_rules"}).
			AddRow(false, "No security rules found", nil)

		mock.ExpectQuery(`SELECT p_success, p_error, p_rules FROM resolvespec_column_security`).
			WithArgs(1, "public", "orders").
			WillReturnRows(rows)

		_, err := provider.GetColumnSecurity(ctx, 1, "public", "orders")
		if err == nil {
			t.Fatal("expected error when loading fails")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test DatabaseRowSecurityProvider
func TestDatabaseRowSecurityProvider(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabaseRowSecurityProvider(db)
	ctx := context.Background()

	t.Run("load row security successfully", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"p_template", "p_block"}).
			AddRow("user_id = {UserID}", false)

		mock.ExpectQuery(`SELECT p_template, p_block FROM resolvespec_row_security`).
			WithArgs("public", "orders", 1).
			WillReturnRows(rows)

		rowSec, err := provider.GetRowSecurity(ctx, 1, "public", "orders")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if rowSec.Template != "user_id = {UserID}" {
			t.Errorf("expected template 'user_id = {UserID}', got %s", rowSec.Template)
		}
		if rowSec.HasBlock {
			t.Error("expected HasBlock to be false")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		mock.ExpectQuery(`SELECT p_template, p_block FROM resolvespec_row_security`).
			WithArgs("public", "blocked_table", 1).
			WillReturnError(sql.ErrNoRows)

		_, err := provider.GetRowSecurity(ctx, 1, "public", "blocked_table")
		if err == nil {
			t.Fatal("expected error when query fails")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

// Test ConfigColumnSecurityProvider
func TestConfigColumnSecurityProvider(t *testing.T) {
	rules := map[string][]ColumnSecurity{
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
