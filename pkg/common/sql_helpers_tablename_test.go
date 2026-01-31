package common

import (
	"testing"
)

// TestSanitizeWhereClause_WithTableName tests that table prefixes in WHERE clauses
// are correctly handled when the tableName parameter matches the prefix
func TestSanitizeWhereClause_WithTableName(t *testing.T) {
	tests := []struct {
		name      string
		where     string
		tableName string
		options   *RequestOptions
		expected  string
	}{
		{
			name:      "Correct table prefix should not be changed",
			where:     "mastertaskitem.rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			options:   nil,
			expected:  "mastertaskitem.rid_parentmastertaskitem is null",
		},
		{
			name:      "Wrong table prefix should be fixed",
			where:     "wrong_table.rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			options:   nil,
			expected:  "mastertaskitem.rid_parentmastertaskitem is null",
		},
		{
			name:      "Relation name should not replace correct table prefix",
			where:     "mastertaskitem.rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			options: &RequestOptions{
				Preload: []PreloadOption{
					{
						Relation:  "MTL.MAL.MAL_RID_PARENTMASTERTASKITEM",
						TableName: "mastertaskitem",
					},
				},
			},
			expected: "mastertaskitem.rid_parentmastertaskitem is null",
		},
		{
			name:      "Unqualified column should remain unqualified",
			where:     "rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			options:   nil,
			expected:  "rid_parentmastertaskitem is null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeWhereClause(tt.where, tt.tableName, tt.options)
			if result != tt.expected {
				t.Errorf("SanitizeWhereClause(%q, %q) = %q, want %q",
					tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}

// TestAddTablePrefixToColumns_WithTableName tests that table prefixes
// are correctly added to unqualified columns
func TestAddTablePrefixToColumns_WithTableName(t *testing.T) {
	tests := []struct {
		name      string
		where     string
		tableName string
		expected  string
	}{
		{
			name:      "Add prefix to unqualified column",
			where:     "rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			expected:  "mastertaskitem.rid_parentmastertaskitem is null",
		},
		{
			name:      "Don't change already qualified column",
			where:     "mastertaskitem.rid_parentmastertaskitem is null",
			tableName: "mastertaskitem",
			expected:  "mastertaskitem.rid_parentmastertaskitem is null",
		},
		{
			name:      "Don't change qualified column with different table",
			where:     "other_table.rid_something is null",
			tableName: "mastertaskitem",
			expected:  "other_table.rid_something is null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AddTablePrefixToColumns(tt.where, tt.tableName)
			if result != tt.expected {
				t.Errorf("AddTablePrefixToColumns(%q, %q) = %q, want %q",
					tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}
