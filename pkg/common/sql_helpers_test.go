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
			name:      "valid condition with parentheses - no prefix added",
			where:     "(status = 'active')",
			tableName: "users",
			expected:  "status = 'active'",
		},
		{
			name:      "mixed trivial and valid conditions - no prefix added",
			where:     "true AND status = 'active' AND 1=1",
			tableName: "users",
			expected:  "status = 'active'",
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
			name:      "multiple valid conditions without prefix - no prefix added",
			where:     "status = 'active' AND age > 18",
			tableName: "users",
			expected:  "status = 'active' AND age > 18",
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
			expected:  "status = 'active' AND age > 18 AND name = 'John'",
		},
		{
			name:      "subquery with ORDER BY and LIMIT - allowed",
			where:     "id IN (SELECT id FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10)",
			tableName: "users",
			expected:  "id IN (SELECT id FROM users WHERE status = 'active' ORDER BY created_at DESC LIMIT 10)",
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
			result := SanitizeWhereClause(tt.where, tt.tableName)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.options != nil {
				result = SanitizeWhereClause(tt.where, tt.tableName, tt.options)
			} else {
				result = SanitizeWhereClause(tt.where, tt.tableName)
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
