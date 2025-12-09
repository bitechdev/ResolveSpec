package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Test SkipAuth
func TestSkipAuth(t *testing.T) {
	ctx := context.Background()
	ctxWithSkip := SkipAuth(ctx)

	skip, ok := ctxWithSkip.Value(SkipAuthKey).(bool)
	if !ok {
		t.Fatal("expected skip auth value to be set")
	}
	if !skip {
		t.Error("expected skip auth to be true")
	}
}

// Test OptionalAuth
func TestOptionalAuth(t *testing.T) {
	ctx := context.Background()
	ctxWithOptional := OptionalAuth(ctx)

	optional, ok := ctxWithOptional.Value(OptionalAuthKey).(bool)
	if !ok {
		t.Fatal("expected optional auth value to be set")
	}
	if !optional {
		t.Error("expected optional auth to be true")
	}
}

// Test createGuestContext
func TestCreateGuestContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	guestCtx := createGuestContext(req)

	if guestCtx.UserID != 0 {
		t.Errorf("expected guest UserID 0, got %d", guestCtx.UserID)
	}
	if guestCtx.UserName != "guest" {
		t.Errorf("expected guest UserName, got %s", guestCtx.UserName)
	}
	if len(guestCtx.Roles) != 1 || guestCtx.Roles[0] != "guest" {
		t.Error("expected guest role")
	}
}

// Test setUserContext
func TestSetUserContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	userCtx := &UserContext{
		UserID:     123,
		UserName:   "testuser",
		UserLevel:  5,
		SessionID:  "session123",
		SessionRID: 456,
		RemoteID:   "remote789",
		Email:      "test@example.com",
		Roles:      []string{"admin", "user"},
		Meta:       map[string]any{"key": "value"},
	}

	newReq := setUserContext(req, userCtx)
	ctx := newReq.Context()

	// Check all values are set in context
	if userID, ok := ctx.Value(UserIDKey).(int); !ok || userID != 123 {
		t.Errorf("expected UserID 123, got %v", userID)
	}
	if userName, ok := ctx.Value(UserNameKey).(string); !ok || userName != "testuser" {
		t.Errorf("expected UserName testuser, got %v", userName)
	}
	if userLevel, ok := ctx.Value(UserLevelKey).(int); !ok || userLevel != 5 {
		t.Errorf("expected UserLevel 5, got %v", userLevel)
	}
	if sessionID, ok := ctx.Value(SessionIDKey).(string); !ok || sessionID != "session123" {
		t.Errorf("expected SessionID session123, got %v", sessionID)
	}
	if email, ok := ctx.Value(UserEmailKey).(string); !ok || email != "test@example.com" {
		t.Errorf("expected Email test@example.com, got %v", email)
	}

	// Check UserContext is set
	if storedUserCtx, ok := ctx.Value(UserContextKey).(*UserContext); !ok {
		t.Error("expected UserContext to be set")
	} else if storedUserCtx.UserID != 123 {
		t.Errorf("expected stored UserContext UserID 123, got %d", storedUserCtx.UserID)
	}
}

// Test NewAuthMiddleware
func TestNewAuthMiddleware(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("successful authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		middleware := NewAuthMiddleware(secList)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check user context is set
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1 in context, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		middleware(handler).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("failed authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		middleware := NewAuthMiddleware(secList)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		middleware(handler).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})

	t.Run("skip authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie, // Would fail normally
		}
		secList, _ := NewSecurityList(provider)

		middleware := NewAuthMiddleware(secList)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Should have guest context
			if uid, ok := GetUserID(r.Context()); !ok || uid != 0 {
				t.Errorf("expected guest UserID 0, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req = req.WithContext(SkipAuth(req.Context()))
		w := httptest.NewRecorder()

		middleware(handler).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("optional authentication with success", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		middleware := NewAuthMiddleware(secList)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req = req.WithContext(OptionalAuth(req.Context()))
		w := httptest.NewRecorder()

		middleware(handler).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("optional authentication with failure", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		middleware := NewAuthMiddleware(secList)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Should have guest context
			if uid, ok := GetUserID(r.Context()); !ok || uid != 0 {
				t.Errorf("expected guest UserID 0, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req = req.WithContext(OptionalAuth(req.Context()))
		w := httptest.NewRecorder()

		middleware(handler).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200 with guest, got %d", w.Code)
		}
	})
}

// Test NewAuthHandler
func TestNewAuthHandler(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("successful authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		handler := NewAuthHandler(secList, nextHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("failed authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		})

		handler := NewAuthHandler(secList, nextHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

// Test NewOptionalAuthHandler
func TestNewOptionalAuthHandler(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("successful authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		})

		handler := NewOptionalAuthHandler(secList, nextHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("failed authentication falls back to guest", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 0 {
				t.Errorf("expected guest UserID 0, got %v", uid)
			}
			if userName, ok := GetUserName(r.Context()); !ok || userName != "guest" {
				t.Errorf("expected guest UserName, got %v", userName)
			}
			w.WriteHeader(http.StatusOK)
		})

		handler := NewOptionalAuthHandler(secList, nextHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

// Test SetSecurityMiddleware
func TestSetSecurityMiddleware(t *testing.T) {
	provider := &mockSecurityProvider{}
	secList, _ := NewSecurityList(provider)

	middleware := SetSecurityMiddleware(secList)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check security list is in context
		if list, ok := GetSecurityList(r.Context()); !ok {
			t.Error("expected security list to be set")
		} else if list == nil {
			t.Error("expected non-nil security list")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	middleware(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// Test WithAuth
func TestWithAuth(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("successful authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		}

		wrapped := WithAuth(handlerFunc, secList)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("failed authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}

		wrapped := WithAuth(handlerFunc, secList)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", w.Code)
		}
	})
}

// Test WithOptionalAuth
func TestWithOptionalAuth(t *testing.T) {
	userCtx := &UserContext{
		UserID:   1,
		UserName: "testuser",
	}

	t.Run("successful authentication", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authUser: userCtx,
		}
		secList, _ := NewSecurityList(provider)

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 1 {
				t.Errorf("expected UserID 1, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		}

		wrapped := WithOptionalAuth(handlerFunc, secList)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("failed authentication falls back to guest", func(t *testing.T) {
		provider := &mockSecurityProvider{
			authError: http.ErrNoCookie,
		}
		secList, _ := NewSecurityList(provider)

		handlerFunc := func(w http.ResponseWriter, r *http.Request) {
			if uid, ok := GetUserID(r.Context()); !ok || uid != 0 {
				t.Errorf("expected guest UserID 0, got %v", uid)
			}
			w.WriteHeader(http.StatusOK)
		}

		wrapped := WithOptionalAuth(handlerFunc, secList)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

// Test WithSecurityContext
func TestWithSecurityContext(t *testing.T) {
	provider := &mockSecurityProvider{}
	secList, _ := NewSecurityList(provider)

	handlerFunc := func(w http.ResponseWriter, r *http.Request) {
		if list, ok := GetSecurityList(r.Context()); !ok {
			t.Error("expected security list in context")
		} else if list == nil {
			t.Error("expected non-nil security list")
		}
		w.WriteHeader(http.StatusOK)
	}

	wrapped := WithSecurityContext(handlerFunc, secList)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// Test GetUserContext and other context getters
func TestContextGetters(t *testing.T) {
	userCtx := &UserContext{
		UserID:     123,
		UserName:   "testuser",
		UserLevel:  5,
		SessionID:  "session123",
		SessionRID: 456,
		RemoteID:   "remote789",
		Email:      "test@example.com",
		Roles:      []string{"admin", "user"},
		Meta:       map[string]any{"key": "value"},
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req = setUserContext(req, userCtx)
	ctx := req.Context()

	t.Run("GetUserContext", func(t *testing.T) {
		user, ok := GetUserContext(ctx)
		if !ok {
			t.Fatal("expected user context to be found")
		}
		if user.UserID != 123 {
			t.Errorf("expected UserID 123, got %d", user.UserID)
		}
	})

	t.Run("GetUserID", func(t *testing.T) {
		userID, ok := GetUserID(ctx)
		if !ok {
			t.Fatal("expected UserID to be found")
		}
		if userID != 123 {
			t.Errorf("expected UserID 123, got %d", userID)
		}
	})

	t.Run("GetUserName", func(t *testing.T) {
		userName, ok := GetUserName(ctx)
		if !ok {
			t.Fatal("expected UserName to be found")
		}
		if userName != "testuser" {
			t.Errorf("expected UserName testuser, got %s", userName)
		}
	})

	t.Run("GetUserLevel", func(t *testing.T) {
		userLevel, ok := GetUserLevel(ctx)
		if !ok {
			t.Fatal("expected UserLevel to be found")
		}
		if userLevel != 5 {
			t.Errorf("expected UserLevel 5, got %d", userLevel)
		}
	})

	t.Run("GetSessionID", func(t *testing.T) {
		sessionID, ok := GetSessionID(ctx)
		if !ok {
			t.Fatal("expected SessionID to be found")
		}
		if sessionID != "session123" {
			t.Errorf("expected SessionID session123, got %s", sessionID)
		}
	})

	t.Run("GetRemoteID", func(t *testing.T) {
		remoteID, ok := GetRemoteID(ctx)
		if !ok {
			t.Fatal("expected RemoteID to be found")
		}
		if remoteID != "remote789" {
			t.Errorf("expected RemoteID remote789, got %s", remoteID)
		}
	})

	t.Run("GetUserRoles", func(t *testing.T) {
		roles, ok := GetUserRoles(ctx)
		if !ok {
			t.Fatal("expected Roles to be found")
		}
		if len(roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(roles))
		}
	})

	t.Run("GetUserEmail", func(t *testing.T) {
		email, ok := GetUserEmail(ctx)
		if !ok {
			t.Fatal("expected Email to be found")
		}
		if email != "test@example.com" {
			t.Errorf("expected Email test@example.com, got %s", email)
		}
	})

	t.Run("GetUserMeta", func(t *testing.T) {
		meta, ok := GetUserMeta(ctx)
		if !ok {
			t.Fatal("expected Meta to be found")
		}
		if meta["key"] != "value" {
			t.Errorf("expected meta key=value, got %v", meta["key"])
		}
	})
}

// Test GetSessionRID
func TestGetSessionRID(t *testing.T) {
	t.Run("valid session RID", func(t *testing.T) {
		ctx := context.Background()
		ctx = context.WithValue(ctx, SessionRIDKey, "789")

		rid, ok := GetSessionRID(ctx)
		if !ok {
			t.Fatal("expected SessionRID to be found")
		}
		if rid != 789 {
			t.Errorf("expected SessionRID 789, got %d", rid)
		}
	})

	t.Run("invalid session RID", func(t *testing.T) {
		ctx := context.Background()
		ctx = context.WithValue(ctx, SessionRIDKey, "invalid")

		_, ok := GetSessionRID(ctx)
		if ok {
			t.Error("expected SessionRID parsing to fail")
		}
	})

	t.Run("missing session RID", func(t *testing.T) {
		ctx := context.Background()

		_, ok := GetSessionRID(ctx)
		if ok {
			t.Error("expected SessionRID to not be found")
		}
	})
}
