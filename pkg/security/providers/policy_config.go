package providers

import (
	"context"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// ConfigColumnSecurityProvider provides static column security configuration
type ConfigColumnSecurityProvider struct {
	rules map[string][]sectypes.ColumnSecurity
}

func NewConfigColumnSecurityProvider(rules map[string][]sectypes.ColumnSecurity) *ConfigColumnSecurityProvider {
	return &ConfigColumnSecurityProvider{rules: rules}
}

func (p *ConfigColumnSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]sectypes.ColumnSecurity, error) {
	key := fmt.Sprintf("%s.%s", schema, table)
	rules, ok := p.rules[key]
	if !ok {
		return []sectypes.ColumnSecurity{}, nil
	}
	return rules, nil
}

// ConfigRowSecurityProvider provides static row security configuration
type ConfigRowSecurityProvider struct {
	templates map[string]string
	blocked   map[string]bool
}

func NewConfigRowSecurityProvider(templates map[string]string, blocked map[string]bool) *ConfigRowSecurityProvider {
	return &ConfigRowSecurityProvider{
		templates: templates,
		blocked:   blocked,
	}
}

func (p *ConfigRowSecurityProvider) GetRowSecurity(ctx context.Context, userRef any, schema, table string) (sectypes.RowSecurity, error) {
	key := fmt.Sprintf("%s.%s", schema, table)

	if p.blocked[key] {
		return sectypes.RowSecurity{
			Schema:    schema,
			Tablename: table,
			UserID:    userRef,
			HasBlock:  true,
		}, nil
	}

	template := p.templates[key]
	return sectypes.RowSecurity{
		Schema:    schema,
		Tablename: table,
		UserID:    userRef,
		Template:  template,
		HasBlock:  false,
	}, nil
}
