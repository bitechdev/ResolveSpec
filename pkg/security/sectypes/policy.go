package sectypes

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

type ColumnSecurity struct {
	Schema       string            `json:"schema"`
	Tablename    string            `json:"tablename"`
	Path         []string          `json:"path"`
	ExtraFilters map[string]string `json:"extra_filters"`
	UserID       int               `json:"user_id"`
	Accesstype   string            `json:"accesstype"`
	MaskStart    int               `json:"mask_start"`
	MaskEnd      int               `json:"mask_end"`
	MaskInvert   bool              `json:"mask_invert"`
	MaskChar     string            `json:"mask_char"`
	Control      string            `json:"control"`
	ID           int               `json:"id"`
}

type RowSecurity struct {
	Schema    string `json:"schema"`
	Tablename string `json:"tablename"`
	Template  string `json:"template"`
	HasBlock  bool   `json:"has_block"`
	// UserID is the opaque user reference the security rules were loaded for.
	// It may be an int, a string/UUID, or a *UserContext, depending on what the
	// RowSecurityProvider/SecurityContext.GetUserRef implementation returns.
	UserID any `json:"user_id"`
}

// safeIdentRe matches an unquoted SQL identifier. Identifiers substituted into a
// row-security template must match it; anything else is rejected.
var safeIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// userIDScalar reduces the opaque user reference to a scalar that is safe to
// bind as a query argument. A *UserContext is reduced to its UserID; other
// structured values are rejected rather than stringified into SQL.
func userIDScalar(ref any) (any, error) {
	switch v := ref.(type) {
	case nil:
		return nil, fmt.Errorf("row security: no user reference")
	case *UserContext:
		if v == nil {
			return nil, fmt.Errorf("row security: nil user context")
		}
		return v.UserID, nil
	case UserContext:
		return v.UserID, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v, nil
	case string:
		return v, nil
	default:
		return nil, fmt.Errorf("row security: unsupported user reference type %T", ref)
	}
}

// GetTemplate expands the row-security template into a WHERE clause and its
// bind arguments. {PrimaryKeyName}, {TableName} and {SchemaName} are validated
// identifiers substituted in place; every {UserID} becomes a `?` placeholder
// with the user reference bound as an argument, so user data never reaches the
// SQL text.
func (m *RowSecurity) GetTemplate(pPrimaryKeyName string, pModelType reflect.Type) (clause string, args []any, err error) {
	str := m.Template

	for placeholder, ident := range map[string]string{
		"{PrimaryKeyName}": pPrimaryKeyName,
		"{TableName}":      m.Tablename,
		"{SchemaName}":     m.Schema,
	} {
		if !strings.Contains(str, placeholder) {
			continue
		}
		if !safeIdentRe.MatchString(ident) {
			return "", nil, fmt.Errorf("row security: invalid identifier %q for %s", ident, placeholder)
		}
		str = strings.ReplaceAll(str, placeholder, ident)
	}

	n := strings.Count(str, "{UserID}")
	if n == 0 {
		return str, nil, nil
	}
	uid, err := userIDScalar(m.UserID)
	if err != nil {
		return "", nil, err
	}
	args = make([]any, n)
	for i := range args {
		args[i] = uid
	}
	return strings.ReplaceAll(str, "{UserID}", "?"), args, nil
}
