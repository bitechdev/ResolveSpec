package common

import (
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

func TestSanitizeWhereClause(t *testing.T) {
	tests := []struct {
		name      string
		where     string
		tableName string
		expected  string
	}{
		{
			name:      "trivial conditions in parentheses",
			where:     "(true AND true AND true)",
			tableName: "mastertask",
			expected:  "",
		},
		{
			name:      "trivial conditions without parentheses",
			where:     "true AND true AND true",
			tableName: "mastertask",
			expected:  "",
		},
		{
			name:      "single trivial condition",
			where:     "true",
			tableName: "mastertask",
			expected:  "",
		},
		{
			name:      "valid condition with parentheses - prefix added to prevent ambiguity",
			where:     "(status = 'active')",
			tableName: "users",
			expected:  "users.status = 'active'",
		},
		{
			name:      "mixed trivial and valid conditions - prefix added",
			where:     "true AND status = 'active' AND 1=1",
			tableName: "users",
			expected:  "users.status = 'active'",
		},
		{
			name:      "condition with correct table prefix - unchanged",
			where:     "users.status = 'active'",
			tableName: "users",
			expected:  "users.status = 'active'",
		},
		{
			name:      "condition with incorrect table prefix - fixed",
			where:     "wrong_table.status = 'active'",
			tableName: "users",
			expected:  "users.status = 'active'",
		},
		{
			name:      "multiple conditions with incorrect prefix - fixed",
			where:     "wrong_table.status = 'active' AND wrong_table.age > 18",
			tableName: "users",
			expected:  "users.status = 'active' AND users.age > 18",
		},
		{
			name:      "multiple valid conditions without prefix - prefixes added",
			where:     "status = 'active' AND age > 18",
			tableName: "users",
			expected:  "users.status = 'active' AND users.age > 18",
		},
		{
			name:      "no table name provided",
			where:     "status = 'active'",
			tableName: "",
			expected:  "status = 'active'",
		},
		{
			name:      "empty where clause",
			where:     "",
			tableName: "users",
			expected:  "",
		},
		{
			name:      "mixed correct and incorrect prefixes",
			where:     "users.status = 'active' AND wrong_table.age > 18",
			tableName: "users",
			expected:  "users.status = 'active' AND users.age > 18",
		},
		{
			name:      "mixed case AND operators",
			where:     "status = 'active' AND age > 18 and name = 'John'",
			tableName: "users",
			expected:  "users.status = 'active' AND users.age > 18 AND users.name = 'John'",
		},
		{
			name:      "subquery with ORDER BY and LIMIT - allowed",
			where:     "id IN (SELECT id FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10)",
			tableName: "users",
			expected:  "users.id IN (SELECT users.id FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10)",
		},
		{
			name:      "dangerous DELETE keyword - blocked",
			where:     "status = 'active'; DELETE FROM users",
			tableName: "users",
			expected:  "",
		},
		{
			name:      "dangerous UPDATE keyword - blocked",
			where:     "1=1; UPDATE users SET admin = true",
			tableName: "users",
			expected:  "",
		},
		{
			name:      "dangerous TRUNCATE keyword - blocked",
			where:     "status = 'active' OR TRUNCATE TABLE users",
			tableName: "users",
			expected:  "",
		},
		{
			name:      "dangerous DROP keyword - blocked",
			where:     "status = 'active'; DROP TABLE users",
			tableName: "users",
			expected:  "",
		},
		{
			name:      "subquery with table alias should not be modified",
			where:     "apiprovider.rid_apiprovider in (select l.rid_apiprovider from core.apiproviderlink l where l.rid_hub = 2576)",
			tableName: "apiprovider",
			expected:  "apiprovider.rid_apiprovider in (select l.rid_apiprovider from core.apiproviderlink l where l.rid_hub = 2576)",
		},
		{
			name:      "complex subquery with AND and multiple operators",
			where:     "apiprovider.type in ('softphone') AND (apiprovider.rid_apiprovider in (select l.rid_apiprovider from core.apiproviderlink l where l.rid_hub = 2576))",
			tableName: "apiprovider",
			expected:  "apiprovider.type in ('softphone') AND (apiprovider.rid_apiprovider in (select l.rid_apiprovider from core.apiproviderlink l where l.rid_hub = 2576))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First add table prefixes to unqualified columns
			prefixedWhere := AddTablePrefixToColumns(tt.where, tt.tableName)
			// Then sanitize the where clause
			result := SanitizeWhereClause(prefixedWhere, tt.tableName)
			if result != tt.expected {
				t.Errorf("SanitizeWhereClause(%q, %q) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}

func TestStripOuterParentheses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single level parentheses",
			input:    "(true)",
			expected: "true",
		},
		{
			name:     "multiple levels",
			input:    "((true))",
			expected: "true",
		},
		{
			name:     "no parentheses",
			input:    "true",
			expected: "true",
		},
		{
			name:     "mismatched parentheses",
			input:    "(true",
			expected: "(true",
		},
		{
			name:     "complex expression",
			input:    "(a AND b)",
			expected: "a AND b",
		},
		{
			name:     "nested but not outer",
			input:    "(a AND (b OR c)) AND d",
			expected: "(a AND (b OR c)) AND d",
		},
		{
			name:     "with spaces",
			input:    "  ( true )  ",
			expected: "true",
		},
		{
			name:     "complex sub query",
			input:    "(a = 1 AND b = 2 or c = 3 and (select s from generate_series(1,10) s where s < 10 and s > 0 offset 2 limit 1) = 3)",
			expected: "a = 1 AND b = 2 or c = 3 and (select s from generate_series(1,10) s where s < 10 and s > 0 offset 2 limit 1) = 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripOuterParentheses(tt.input)
			if result != tt.expected {
				t.Errorf("stripOuterParentheses(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsTrivialCondition(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"true", "true", true},
		{"true with spaces", "  true  ", true},
		{"TRUE uppercase", "TRUE", true},
		{"1=1", "1=1", true},
		{"1 = 1", "1 = 1", true},
		{"true = true", "true = true", true},
		{"valid condition", "status = 'active'", false},
		{"false", "false", false},
		{"column name", "is_active", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTrivialCondition(tt.input)
			if result != tt.expected {
				t.Errorf("IsTrivialCondition(%q) = %v; want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractTableAndColumn(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedTable string
		expectedCol   string
	}{
		{
			name:          "qualified column with equals",
			input:         "users.status = 'active'",
			expectedTable: "users",
			expectedCol:   "status",
		},
		{
			name:          "qualified column with greater than",
			input:         "users.age > 18",
			expectedTable: "users",
			expectedCol:   "age",
		},
		{
			name:          "qualified column with LIKE",
			input:         "users.name LIKE '%john%'",
			expectedTable: "users",
			expectedCol:   "name",
		},
		{
			name:          "qualified column with IN",
			input:         "users.status IN ('active', 'pending')",
			expectedTable: "users",
			expectedCol:   "status",
		},
		{
			name:          "unqualified column",
			input:         "status = 'active'",
			expectedTable: "",
			expectedCol:   "",
		},
		{
			name:          "qualified with backticks",
			input:         "`users`.`status` = 'active'",
			expectedTable: "users",
			expectedCol:   "status",
		},
		{
			name:          "schema.table.column reference",
			input:         "public.users.status = 'active'",
			expectedTable: "public.users",
			expectedCol:   "status",
		},
		{
			name:          "empty string",
			input:         "",
			expectedTable: "",
			expectedCol:   "",
		},
		{
			name:          "function call with table.column - ifblnk",
			input:         "ifblnk(users.status,0) in (1,2,3,4)",
			expectedTable: "users",
			expectedCol:   "status",
		},
		{
			name:          "function call with table.column - coalesce",
			input:         "coalesce(users.age, 0) = 25",
			expectedTable: "users",
			expectedCol:   "age",
		},
		{
			name:          "nested function calls",
			input:         "upper(trim(users.name)) = 'JOHN'",
			expectedTable: "users",
			expectedCol:   "name",
		},
		{
			name:          "function with multiple args and table.column",
			input:         "substring(users.email, 1, 5) = 'admin'",
			expectedTable: "users",
			expectedCol:   "email",
		},
		{
			name:          "cast function with table.column",
			input:         "cast(orders.total as decimal) > 100",
			expectedTable: "orders",
			expectedCol:   "total",
		},
		{
			name:          "complex nested functions",
			input:         "coalesce(nullif(users.status, ''), 'default') = 'active'",
			expectedTable: "users",
			expectedCol:   "status",
		},
		{
			name:          "function with multiple table.column refs (extracts first)",
			input:         "greatest(users.created_at, users.updated_at) > '2024-01-01'",
			expectedTable: "users",
			expectedCol:   "created_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, col := extractTableAndColumn(tt.input)
			if table != tt.expectedTable || col != tt.expectedCol {
				t.Errorf("extractTableAndColumn(%q) = (%q, %q); want (%q, %q)",
					tt.input, table, col, tt.expectedTable, tt.expectedCol)
			}
		})
	}
}

func TestSanitizeWhereClauseWithPreloads(t *testing.T) {
	tests := []struct {
		name      string
		where     string
		tableName string
		options   *RequestOptions
		expected  string
		addPrefix bool
	}{
		{
			name:      "preload relation prefix is preserved",
			where:     "Department.name = 'Engineering'",
			tableName: "users",
			options: &RequestOptions{
				Preload: []PreloadOption{
					{Relation: "Department"},
				},
			},
			expected: "Department.name = 'Engineering'",
		},
		{
			name:      "multiple preload relations - all preserved",
			where:     "Department.name = 'Engineering' AND Manager.status = 'active'",
			tableName: "users",
			options: &RequestOptions{
				Preload: []PreloadOption{
					{Relation: "Department"},
					{Relation: "Manager"},
				},
			},
			expected: "Department.name = 'Engineering' AND Manager.status = 'active'",
		},
		{
			name:      "mix of main table and preload relation",
			where:     "users.status = 'active' AND Department.name = 'Engineering'",
			tableName: "users",
			options: &RequestOptions{
				Preload: []PreloadOption{
					{Relation: "Department"},
				},
			},
			expected: "users.status = 'active' AND Department.name = 'Engineering'",
		},
		{
			name:      "incorrect prefix fixed when not a preload relation",
			where:     "wrong_table.status = 'active' AND Department.name = 'Engineering'",
			tableName: "users",
			options: &RequestOptions{
				Preload: []PreloadOption{
					{Relation: "Department"},
				},
			},
			expected: "users.status = 'active' AND Department.name = 'Engineering'",
		},

		{
			name:      "Function Call with correct table prefix - unchanged",
			where:     "ifblnk(users.status,0) in (1,2,3,4)",
			tableName: "users",
			options:   nil,
			expected:  "ifblnk(users.status,0) in (1,2,3,4)",
		},
		{
			name:      "no options provided - works as before",
			where:     "wrong_table.status = 'active'",
			tableName: "users",
			options:   nil,
			expected:  "users.status = 'active'",
		},
		{
			name:      "empty preload list - works as before",
			where:     "wrong_table.status = 'active'",
			tableName: "users",
			options:   &RequestOptions{Preload: []PreloadOption{}},
			expected:  "users.status = 'active'",
		},

		{
			name:      "complex where clause with subquery and preload",
			where:     `("mastertaskitem"."rid_mastertask" IN (6, 173, 157, 172, 174, 171, 170, 169, 167, 168, 166, 145, 161, 164, 146, 160, 147, 159, 148, 150, 152, 175, 151, 8, 153, 149, 155, 154, 165)) AND (rid_parentmastertaskitem is null)`,
			tableName: "mastertaskitem",
			options:   nil,
			expected:  `("mastertaskitem"."rid_mastertask" IN (6, 173, 157, 172, 174, 171, 170, 169, 167, 168, 166, 145, 161, 164, 146, 160, 147, 159, 148, 150, 152, 175, 151, 8, 153, 149, 155, 154, 165)) AND (mastertaskitem.rid_parentmastertaskitem is null)`,
			addPrefix: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			prefixedWhere := tt.where
			if tt.addPrefix {
				// First add table prefixes to unqualified columns
				prefixedWhere = AddTablePrefixToColumns(tt.where, tt.tableName)
			}
			// Then sanitize the where clause
			if tt.options != nil {
				result = SanitizeWhereClause(prefixedWhere, tt.tableName, tt.options)
			} else {
				result = SanitizeWhereClause(prefixedWhere, tt.tableName)
			}
			if result != tt.expected {
				t.Errorf("SanitizeWhereClause(%q, %q, options) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}

// Test model for model-aware sanitization tests
type MasterTask struct {
	ID     int    `bun:"id,pk"`
	Name   string `bun:"name"`
	Status string `bun:"status"`
	UserID int    `bun:"user_id"`
}

func TestSplitByAND(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "uppercase AND",
			input:    "status = 'active' AND age > 18",
			expected: []string{"status = 'active'", "age > 18"},
		},
		{
			name:     "lowercase and",
			input:    "status = 'active' and age > 18",
			expected: []string{"status = 'active'", "age > 18"},
		},
		{
			name:     "mixed case AND",
			input:    "status = 'active' AND age > 18 and name = 'John'",
			expected: []string{"status = 'active'", "age > 18", "name = 'John'"},
		},
		{
			name:     "single condition",
			input:    "status = 'active'",
			expected: []string{"status = 'active'"},
		},
		{
			name:     "multiple uppercase AND",
			input:    "a = 1 AND b = 2 AND c = 3",
			expected: []string{"a = 1", "b = 2", "c = 3"},
		},
		{
			name:     "multiple case subquery",
			input:    "a = 1 AND b = 2 AND c = 3 and (select s from generate_series(1,10) s where s < 10 and s > 0 offset 2 limit 1) = 3",
			expected: []string{"a = 1", "b = 2", "c = 3", "(select s from generate_series(1,10) s where s < 10 and s > 0 offset 2 limit 1) = 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitByAND(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("splitByAND(%q) returned %d conditions; want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i := range result {
				if strings.TrimSpace(result[i]) != strings.TrimSpace(tt.expected[i]) {
					t.Errorf("splitByAND(%q)[%d] = %q; want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestValidateWhereClauseSecurity(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "safe WHERE clause",
			input:       "status = 'active' AND age > 18",
			expectError: false,
		},
		{
			name:        "safe subquery",
			input:       "id IN (SELECT id FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10)",
			expectError: false,
		},
		{
			name:        "DELETE keyword",
			input:       "status = 'active'; DELETE FROM users",
			expectError: true,
		},
		{
			name:        "UPDATE keyword",
			input:       "1=1; UPDATE users SET admin = true",
			expectError: true,
		},
		{
			name:        "TRUNCATE keyword",
			input:       "status = 'active' OR TRUNCATE TABLE users",
			expectError: true,
		},
		{
			name:        "DROP keyword",
			input:       "status = 'active'; DROP TABLE users",
			expectError: true,
		},
		{
			name:        "INSERT keyword",
			input:       "status = 'active'; INSERT INTO users (name) VALUES ('hacker')",
			expectError: true,
		},
		{
			name:        "ALTER keyword",
			input:       "1=1; ALTER TABLE users ADD COLUMN is_admin BOOLEAN",
			expectError: true,
		},
		{
			name:        "CREATE keyword",
			input:       "1=1; CREATE TABLE malicious (id INT)",
			expectError: true,
		},
		{
			name:        "empty clause",
			input:       "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWhereClauseSecurity(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("validateWhereClauseSecurity(%q) expected error but got none", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("validateWhereClauseSecurity(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestSanitizeWhereClauseWithModel(t *testing.T) {
	// Register the test model
	err := modelregistry.RegisterModel(MasterTask{}, "mastertask")
	if err != nil {
		// Model might already be registered, ignore error
		t.Logf("Model registration returned: %v", err)
	}

	tests := []struct {
		name      string
		where     string
		tableName string
		expected  string
	}{
		{
			name:      "valid column without prefix - no prefix added",
			where:     "status = 'active'",
			tableName: "mastertask",
			expected:  "status = 'active'",
		},
		{
			name:      "multiple valid columns without prefix - no prefix added",
			where:     "status = 'active' AND user_id = 123",
			tableName: "mastertask",
			expected:  "status = 'active' AND user_id = 123",
		},
		{
			name:      "incorrect table prefix on valid column - fixed",
			where:     "wrong_table.status = 'active'",
			tableName: "mastertask",
			expected:  "mastertask.status = 'active'",
		},
		{
			name:      "incorrect prefix on invalid column - not fixed",
			where:     "wrong_table.invalid_column = 'value'",
			tableName: "mastertask",
			expected:  "wrong_table.invalid_column = 'value'",
		},
		{
			name:      "mix of valid and trivial conditions",
			where:     "true AND status = 'active' AND 1=1",
			tableName: "mastertask",
			expected:  "status = 'active'",
		},
		{
			name:      "parentheses with valid column - no prefix added",
			where:     "(status = 'active')",
			tableName: "mastertask",
			expected:  "status = 'active'",
		},
		{
			name:      "correct prefix - unchanged",
			where:     "mastertask.status = 'active'",
			tableName: "mastertask",
			expected:  "mastertask.status = 'active'",
		},
		{
			name:      "multiple conditions with mixed prefixes",
			where:     "mastertask.status = 'active' AND wrong_table.user_id = 123",
			tableName: "mastertask",
			expected:  "mastertask.status = 'active' AND mastertask.user_id = 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeWhereClause(tt.where, tt.tableName)
			if result != tt.expected {
				t.Errorf("SanitizeWhereClause(%q, %q) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}

func TestEnsureOuterParentheses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no parentheses",
			input:    "status = 'active'",
			expected: "(status = 'active')",
		},
		{
			name:     "already has outer parentheses",
			input:    "(status = 'active')",
			expected: "(status = 'active')",
		},
		{
			name:     "OR condition without parentheses",
			input:    "status = 'active' OR status = 'pending'",
			expected: "(status = 'active' OR status = 'pending')",
		},
		{
			name:     "OR condition with parentheses",
			input:    "(status = 'active' OR status = 'pending')",
			expected: "(status = 'active' OR status = 'pending')",
		},
		{
			name:     "complex condition with nested parentheses",
			input:    "(status = 'active' OR status = 'pending') AND (age > 18)",
			expected: "((status = 'active' OR status = 'pending') AND (age > 18))",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "mismatched parentheses - adds outer ones",
			input:    "(status = 'active' OR status = 'pending'",
			expected: "((status = 'active' OR status = 'pending')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnsureOuterParentheses(tt.input)
			if result != tt.expected {
				t.Errorf("EnsureOuterParentheses(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContainsTopLevelOR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "no OR operator",
			input:    "status = 'active' AND age > 18",
			expected: false,
		},
		{
			name:     "top-level OR",
			input:    "status = 'active' OR status = 'pending'",
			expected: true,
		},
		{
			name:     "OR inside parentheses",
			input:    "age > 18 AND (status = 'active' OR status = 'pending')",
			expected: false,
		},
		{
			name:     "OR in subquery",
			input:    "id IN (SELECT id FROM users WHERE status = 'active' OR status = 'pending')",
			expected: false,
		},
		{
			name:     "OR inside quotes",
			input:    "comment = 'this OR that'",
			expected: false,
		},
		{
			name:     "mixed - top-level OR and nested OR",
			input:    "name = 'test' OR (status = 'active' OR status = 'pending')",
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "lowercase or",
			input:    "status = 'active' or status = 'pending'",
			expected: true,
		},
		{
			name:     "uppercase OR",
			input:    "status = 'active' OR status = 'pending'",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsTopLevelOR(tt.input)
			if result != tt.expected {
				t.Errorf("containsTopLevelOR(%q) = %v; want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeWhereClause_PreservesParenthesesWithOR(t *testing.T) {
	tests := []struct {
		name      string
		where     string
		tableName string
		expected  string
	}{
		{
			name:      "OR condition with outer parentheses - preserved",
			where:     "(status = 'active' OR status = 'pending')",
			tableName: "users",
			expected:  "(users.status = 'active' OR users.status = 'pending')",
		},
		{
			name:      "AND condition with outer parentheses - stripped (no OR)",
			where:     "(status = 'active' AND age > 18)",
			tableName: "users",
			expected:  "users.status = 'active' AND users.age > 18",
		},
		{
			name:      "complex OR with nested conditions",
			where:     "((status = 'active' OR status = 'pending') AND age > 18)",
			tableName: "users",
			// Outer parens are stripped, but inner parens with OR are preserved
			expected: "(users.status = 'active' OR users.status = 'pending') AND users.age > 18",
		},
		{
			name:      "OR without outer parentheses - no parentheses added by SanitizeWhereClause",
			where:     "status = 'active' OR status = 'pending'",
			tableName: "users",
			expected:  "users.status = 'active' OR users.status = 'pending'",
		},
		{
			name:      "simple OR with parentheses - preserved",
			where:     "(users.status = 'active' OR users.status = 'pending')",
			tableName: "users",
			// Already has correct prefixes, parentheses preserved
			expected: "(users.status = 'active' OR users.status = 'pending')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefixedWhere := AddTablePrefixToColumns(tt.where, tt.tableName)
			result := SanitizeWhereClause(prefixedWhere, tt.tableName)
			if result != tt.expected {
				t.Errorf("SanitizeWhereClause(%q, %q) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
			}
		})
	}
}

func TestAddTablePrefixToColumns_ComplexConditions(t *testing.T) {
tests := []struct {
name      string
where     string
tableName string
expected  string
}{
{
name:      "Parentheses with true AND condition - should not prefix true",
where:     "(true AND status = 'active')",
tableName: "mastertask",
expected:  "(true AND mastertask.status = 'active')",
},
{
name:      "Parentheses with multiple conditions including true",
where:     "(true AND status = 'active' AND id > 5)",
tableName: "mastertask",
expected:  "(true AND mastertask.status = 'active' AND mastertask.id > 5)",
},
{
name:      "Nested parentheses with true",
where:     "((true AND status = 'active'))",
tableName: "mastertask",
expected:  "((true AND mastertask.status = 'active'))",
},
{
name:      "Mixed: false AND valid conditions",
where:     "(false AND name = 'test')",
tableName: "mastertask",
expected:  "(false AND mastertask.name = 'test')",
},
{
name:      "Mixed: null AND valid conditions",
where:     "(null AND status = 'active')",
tableName: "mastertask",
expected:  "(null AND mastertask.status = 'active')",
},
{
name:      "Multiple true conditions in parentheses",
where:     "(true AND true AND status = 'active')",
tableName: "mastertask",
expected:  "(true AND true AND mastertask.status = 'active')",
},
{
name:      "Simple true without parens - should not prefix",
where:     "true",
tableName: "mastertask",
expected:  "true",
},
{
name:      "Simple condition without parens - should prefix",
where:     "status = 'active'",
tableName: "mastertask",
expected:  "mastertask.status = 'active'",
},
{
name:      "Unregistered table with true - should not prefix true",
where:     "(true AND status = 'active')",
tableName: "unregistered_table",
expected:  "(true AND unregistered_table.status = 'active')",
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
result := AddTablePrefixToColumns(tt.where, tt.tableName)
if result != tt.expected {
t.Errorf("AddTablePrefixToColumns(%q, %q) = %q; want %q", tt.where, tt.tableName, result, tt.expected)
}
})
}
}
