package providers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// HeaderAuthenticator provides simple header-based authentication
// Expects: X-User-ID, X-User-Name, X-User-Level, X-Session-ID, X-Remote-ID, X-User-Roles, X-User-Email
type HeaderAuthenticator struct{}

func NewHeaderAuthenticator() *HeaderAuthenticator {
	return &HeaderAuthenticator{}
}

func (a *HeaderAuthenticator) Login(ctx context.Context, req sectypes.LoginRequest) (*sectypes.LoginResponse, error) {
	return nil, fmt.Errorf("header authentication does not support login")
}

func (a *HeaderAuthenticator) LoginWithCookie(ctx context.Context, req sectypes.LoginRequest, w http.ResponseWriter) (*sectypes.LoginResponse, error) {
	return a.Login(ctx, req)
}

func (a *HeaderAuthenticator) Logout(ctx context.Context, req sectypes.LogoutRequest) error {
	return nil
}

func (a *HeaderAuthenticator) LogoutWithCookie(ctx context.Context, req sectypes.LogoutRequest, w http.ResponseWriter) error {
	return a.Logout(ctx, req)
}

func (a *HeaderAuthenticator) Authenticate(r *http.Request) (*sectypes.UserContext, error) {
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return nil, fmt.Errorf("X-User-ID header required")
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	return &sectypes.UserContext{
		UserID:    userID,
		UserName:  r.Header.Get("X-User-Name"),
		UserLevel: parseIntHeader(r, "X-User-Level", 0),
		SessionID: r.Header.Get("X-Session-ID"),
		RemoteID:  r.Header.Get("X-Remote-ID"),
		Email:     r.Header.Get("X-User-Email"),
		Roles:     parseRoles(r.Header.Get("X-User-Roles")),
	}, nil
}

func parseRoles(rolesStr string) []string {
	if rolesStr == "" {
		return []string{}
	}
	return strings.Split(rolesStr, ",")
}

func parseIntHeader(r *http.Request, key string, defaultVal int) int {
	val := r.Header.Get(key)
	if val == "" {
		return defaultVal
	}
	intVal, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return intVal
}
