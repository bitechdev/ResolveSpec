package security

import (
	"context"
	"fmt"
	"net/http"
)

// CompositeSecurityProvider combines multiple security providers
// Allows separating authentication, column security, and row security concerns
type CompositeSecurityProvider struct {
	auth    Authenticator
	colSec  ColumnSecurityProvider
	rowSec  RowSecurityProvider
}

// NewCompositeSecurityProvider creates a composite provider
// All parameters are required
func NewCompositeSecurityProvider(
	auth Authenticator,
	colSec ColumnSecurityProvider,
	rowSec RowSecurityProvider,
) *CompositeSecurityProvider {
	if auth == nil {
		panic("authenticator cannot be nil")
	}
	if colSec == nil {
		panic("column security provider cannot be nil")
	}
	if rowSec == nil {
		panic("row security provider cannot be nil")
	}

	return &CompositeSecurityProvider{
		auth:   auth,
		colSec: colSec,
		rowSec: rowSec,
	}
}

// Login delegates to the authenticator
func (c *CompositeSecurityProvider) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	return c.auth.Login(ctx, req)
}

// Logout delegates to the authenticator
func (c *CompositeSecurityProvider) Logout(ctx context.Context, req LogoutRequest) error {
	return c.auth.Logout(ctx, req)
}

// Authenticate delegates to the authenticator
func (c *CompositeSecurityProvider) Authenticate(r *http.Request) (*UserContext, error) {
	return c.auth.Authenticate(r)
}

// GetColumnSecurity delegates to the column security provider
func (c *CompositeSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	return c.colSec.GetColumnSecurity(ctx, userID, schema, table)
}

// GetRowSecurity delegates to the row security provider
func (c *CompositeSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error) {
	return c.rowSec.GetRowSecurity(ctx, userID, schema, table)
}

// Optional interface implementations (if wrapped providers support them)

// RefreshToken implements Refreshable if the authenticator supports it
func (c *CompositeSecurityProvider) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	if refreshable, ok := c.auth.(Refreshable); ok {
		return refreshable.RefreshToken(ctx, refreshToken)
	}
	return nil, fmt.Errorf("authenticator does not support token refresh")
}

// ValidateToken implements Validatable if the authenticator supports it
func (c *CompositeSecurityProvider) ValidateToken(ctx context.Context, token string) (bool, error) {
	if validatable, ok := c.auth.(Validatable); ok {
		return validatable.ValidateToken(ctx, token)
	}
	return false, fmt.Errorf("authenticator does not support token validation")
}

// ClearCache implements Cacheable if any provider supports it
func (c *CompositeSecurityProvider) ClearCache(ctx context.Context, userID int, schema, table string) error {
	var errs []error

	if cacheable, ok := c.colSec.(Cacheable); ok {
		if err := cacheable.ClearCache(ctx, userID, schema, table); err != nil {
			errs = append(errs, fmt.Errorf("column security cache clear failed: %w", err))
		}
	}

	if cacheable, ok := c.rowSec.(Cacheable); ok {
		if err := cacheable.ClearCache(ctx, userID, schema, table); err != nil {
			errs = append(errs, fmt.Errorf("row security cache clear failed: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cache clear errors: %v", errs)
	}

	return nil
}
