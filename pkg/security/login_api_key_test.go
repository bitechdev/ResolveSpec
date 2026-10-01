package security

import (
	"context"
	"errors"
	"testing"
)

type fakeAPIKeyAuth struct {
	Authenticator
	key string
}

func (f *fakeAPIKeyAuth) LoginWithAPIKey(_ context.Context, rawKey string, _ map[string]any) (*LoginResponse, error) {
	if rawKey != f.key {
		return nil, errInvalidAPIKey
	}
	return &LoginResponse{Token: "tok"}, nil
}

func TestChainLoginWithAPIKey(t *testing.T) {
	ctx := context.Background()
	chain := NewChainAuthenticator(&fakeAPIKeyAuth{key: "a"}, &fakeAPIKeyAuth{key: "b"})
	for _, k := range []string{"a", "b"} {
		if resp, err := chain.LoginWithAPIKey(ctx, k, nil); err != nil || resp.Token != "tok" {
			t.Errorf("chain LoginWithAPIKey(%q) = %v, %v", k, resp, err)
		}
	}
	if _, err := chain.LoginWithAPIKey(ctx, "bad", nil); !errors.Is(err, errInvalidAPIKey) {
		t.Errorf("chain bad key error = %v, want errInvalidAPIKey", err)
	}
}

func TestDatabaseAuthenticatorLoginWithAPIKey_EmptyKey(t *testing.T) {
	auth := NewDatabaseAuthenticatorWithOptions(newDirectTestDB(t), DatabaseAuthenticatorOptions{})
	if _, err := auth.LoginWithAPIKey(context.Background(), "", nil); !errors.Is(err, errInvalidAPIKey) {
		t.Errorf("empty key error = %v, want errInvalidAPIKey", err)
	}
}
