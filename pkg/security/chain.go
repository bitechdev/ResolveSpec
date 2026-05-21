package security

import (
	"context"
	"fmt"
	"net/http"
)

// ChainAuthenticator tries each authenticator in order, returning the first success.
// Login and Logout are delegated to the primary authenticator.
type ChainAuthenticator struct {
	authenticators []Authenticator
}

// NewChainAuthenticator creates a ChainAuthenticator from the given authenticators.
// At least one authenticator is required; the first is treated as primary for Login/Logout.
func NewChainAuthenticator(primary Authenticator, rest ...Authenticator) *ChainAuthenticator {
	return &ChainAuthenticator{
		authenticators: append([]Authenticator{primary}, rest...),
	}
}

func (c *ChainAuthenticator) Authenticate(r *http.Request) (*UserContext, error) {
	var lastErr error
	for _, a := range c.authenticators {
		if uc, err := a.Authenticate(r); err == nil {
			return uc, nil
		} else {
			lastErr = err
		}
	}
	return nil, fmt.Errorf("all authenticators failed; last error: %w", lastErr)
}

func (c *ChainAuthenticator) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return c.authenticators[0].Login(ctx, req)
}

func (c *ChainAuthenticator) Logout(ctx context.Context, req LogoutRequest) error {
	return c.authenticators[0].Logout(ctx, req)
}
