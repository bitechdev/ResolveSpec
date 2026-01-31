package restheadspec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestDecodeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "String without encoding prefix",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decodeHeaderValue(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestAddXFilesPreload_WithSqlJoins(t *testing.T) {
	handler := &Handler{}
	options := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Preload: make([]common.PreloadOption, 0),
		},
	}

	// Create an XFiles with SqlJoins
	xfile := &XFiles{
		TableName: "users",
		SqlJoins: []string{
			"LEFT JOIN departments d ON d.id = users.department_id",
			"INNER JOIN roles r ON r.id = users.role_id",
		},
		FilterFields: []struct {
			Field    string `json:"field"`
			Value    string `json:"value"`
			Operator string `json:"operator"`
		}{
			{Field: "d.active", Value: "true", Operator: "eq"},
			{Field: "r.name", Value: "admin", Operator: "eq"},
		},
	}

	// Add the XFiles preload
	handler.addXFilesPreload(xfile, options, "")

	// Verify that a preload was added
	if len(options.Preload) != 1 {
		t.Fatalf("Expected 1 preload, got %d", len(options.Preload))
	}

	preload := options.Preload[0]

	// Verify relation name
	if preload.Relation != "users" {
		t.Errorf("Expected relation 'users', got '%s'", preload.Relation)
	}

	// Verify SqlJoins were transferred
	if len(preload.SqlJoins) != 2 {
		t.Fatalf("Expected 2 SQL joins, got %d", len(preload.SqlJoins))
	}

	// Verify JoinAliases were extracted
	if len(preload.JoinAliases) != 2 {
		t.Fatalf("Expected 2 join aliases, got %d", len(preload.JoinAliases))
	}

	// Verify the aliases are correct
	expectedAliases := []string{"d", "r"}
	for i, expected := range expectedAliases {
		if preload.JoinAliases[i] != expected {
			t.Errorf("Expected alias '%s', got '%s'", expected, preload.JoinAliases[i])
		}
	}

	// Verify filters were added
	if len(preload.Filters) != 2 {
		t.Fatalf("Expected 2 filters, got %d", len(preload.Filters))
	}

	// Verify filter columns reference joined tables
	if preload.Filters[0].Column != "d.active" {
		t.Errorf("Expected filter column 'd.active', got '%s'", preload.Filters[0].Column)
	}
	if preload.Filters[1].Column != "r.name" {
		t.Errorf("Expected filter column 'r.name', got '%s'", preload.Filters[1].Column)
	}
}

func TestExtractJoinAlias(t *testing.T) {
	tests := []struct {
		name        string
		joinClause  string
		expected    string
	}{
		{
			name:       "LEFT JOIN with alias",
			joinClause: "LEFT JOIN departments d ON d.id = users.department_id",
			expected:   "d",
		},
		{
			name:       "INNER JOIN with AS keyword",
			joinClause: "INNER JOIN users AS u ON u.id = orders.user_id",
			expected:   "u",
		},
		{
			name:       "JOIN without alias",
			joinClause: "JOIN roles ON roles.id = users.role_id",
			expected:   "",
		},
		{
			name:       "Complex join with multiple conditions",
			joinClause: "LEFT OUTER JOIN products p ON p.id = items.product_id AND p.active = true",
			expected:   "p",
		},
		{
			name:       "Invalid join (no ON clause)",
			joinClause: "LEFT JOIN departments",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJoinAlias(tt.joinClause)
			if result != tt.expected {
				t.Errorf("Expected alias '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

// Note: The following functions are unexported (lowercase) and cannot be tested directly:
// - parseSelectFields
// - parseFieldFilter
// - mapSearchOperator
// - parseCommaSeparated
// - parseSorting
// These are tested indirectly through parseOptionsFromHeaders in query_params_test.go
