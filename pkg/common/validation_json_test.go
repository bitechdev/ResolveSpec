package common

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/reflection"
	"github.com/bitechdev/ResolveSpec/pkg/spectypes"
)

func TestExtractSourceColumn(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple column name",
			input:    "columna",
			expected: "columna",
		},
		{
			name:     "column with ->> operator",
			input:    "columna->>'val'",
			expected: "columna",
		},
		{
			name:     "column with -> operator",
			input:    "columna->'key'",
			expected: "columna",
		},
		{
			name:     "column with table prefix and ->> operator",
			input:    "table.columna->>'val'",
			expected: "table.columna",
		},
		{
			name:     "column with table prefix and -> operator",
			input:    "table.columna->'key'",
			expected: "table.columna",
		},
		{
			name:     "complex JSON path with ->>",
			input:    "data->>'nested'->>'value'",
			expected: "data",
		},
		{
			name:     "column with spaces before operator",
			input:    "columna ->>'val'",
			expected: "columna",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := reflection.ExtractSourceColumn(tc.input)
			if result != tc.expected {
				t.Errorf("reflection.ExtractSourceColumn(%q) = %q; want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestValidateColumnWithJSONOperators(t *testing.T) {
	// Create a test model
	type TestModel struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Data     string `json:"data"` // JSON column
		Metadata string `json:"metadata"`
	}

	validator := NewColumnValidator(TestModel{})

	testCases := []struct {
		name      string
		column    string
		shouldErr bool
	}{
		{
			name:      "simple valid column",
			column:    "name",
			shouldErr: false,
		},
		{
			name:      "valid column with ->> operator",
			column:    "data->>'field'",
			shouldErr: false,
		},
		{
			name:      "valid column with -> operator",
			column:    "metadata->'key'",
			shouldErr: false,
		},
		{
			name:      "invalid column",
			column:    "invalid_column",
			shouldErr: true,
		},
		{
			name:      "invalid column with ->> operator",
			column:    "invalid_column->>'field'",
			shouldErr: true,
		},
		{
			name:      "cql prefixed column (always valid)",
			column:    "cql_computed",
			shouldErr: false,
		},
		{
			name:      "empty column",
			column:    "",
			shouldErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateColumn(tc.column)
			if tc.shouldErr && err == nil {
				t.Errorf("ValidateColumn(%q) expected error, got nil", tc.column)
			}
			if !tc.shouldErr && err != nil {
				t.Errorf("ValidateColumn(%q) expected no error, got %v", tc.column, err)
			}
		})
	}
}

func TestValidateColumn_JSONPathsAndDottedShorthand(t *testing.T) {
	type Model struct {
		ID   int64              `json:"id"`
		Name string             `json:"name"`
		Data spectypes.SqlJSONB `json:"data"`
	}
	v := NewColumnValidator(Model{})

	valid := []string{
		"data->>'city'",
		"data->'addr'->>'city'",
		"data#>>'{addr,city}'",
		"data.addr.city",      // dotted shorthand, base is JSON -> allowed
		"(data->>'age')::int", // cast + paren
	}
	for _, c := range valid {
		if err := v.ValidateColumn(c); err != nil {
			t.Errorf("ValidateColumn(%q) = %v, want nil", c, err)
		}
	}

	invalid := []string{
		"nope->>'city'", // base column does not exist
		"name.first",    // dotted shorthand but 'name' is not a JSON column
	}
	for _, c := range invalid {
		if err := v.ValidateColumn(c); err == nil {
			t.Errorf("ValidateColumn(%q) = nil, want error", c)
		}
	}
}
