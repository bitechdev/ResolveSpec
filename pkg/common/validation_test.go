package common

import (
	"strings"
	"testing"
)

// TestModel represents a sample model for testing
type TestModel struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	Name      string `json:"name" gorm:"column:name"`
	Email     string `json:"email" bun:"email"`
	Age       int    `json:"age"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
}

func TestNewColumnValidator(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	if validator == nil {
		t.Fatal("Expected validator to be created")
	}

	if len(validator.validColumns) == 0 {
		t.Fatal("Expected validator to have valid columns")
	}

	// Check that expected columns are present
	expectedColumns := []string{"id", "name", "email", "age", "is_active", "created_at"}
	for _, col := range expectedColumns {
		if !validator.validColumns[col] {
			t.Errorf("Expected column '%s' to be valid", col)
		}
	}
}

func TestValidateColumn(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	tests := []struct {
		name        string
		column      string
		shouldError bool
	}{
		{"Valid column - id", "id", false},
		{"Valid column - name", "name", false},
		{"Valid column - email", "email", false},
		{"Valid column - uppercase", "ID", false}, // Case insensitive
		{"Invalid column", "invalid_column", true},
		{"CQL prefixed - should be valid", "cqlComputedField", false},
		{"CQL prefixed uppercase - should be valid", "CQLComputedField", false},
		{"Empty column", "", false}, // Empty columns are allowed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateColumn(tt.column)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for column '%s', got nil", tt.column)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for column '%s', got: %v", tt.column, err)
			}
		})
	}
}

func TestValidateColumns(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	tests := []struct {
		name        string
		columns     []string
		shouldError bool
	}{
		{"All valid columns", []string{"id", "name", "email"}, false},
		{"One invalid column", []string{"id", "invalid_col", "name"}, true},
		{"All invalid columns", []string{"bad1", "bad2"}, true},
		{"With CQL prefix", []string{"id", "cqlComputed", "name"}, false},
		{"Empty list", []string{}, false},
		{"Nil list", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateColumns(tt.columns)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for columns %v, got nil", tt.columns)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for columns %v, got: %v", tt.columns, err)
			}
		})
	}
}

func TestValidateRequestOptions(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	tests := []struct {
		name        string
		options     RequestOptions
		shouldError bool
		errorMsg    string
	}{
		{
			name: "Valid options with columns",
			options: RequestOptions{
				Columns: []string{"id", "name"},
				Filters: []FilterOption{
					{Column: "name", Operator: "eq", Value: "test"},
				},
				Sort: []SortOption{
					{Column: "id", Direction: "ASC"},
				},
			},
			shouldError: false,
		},
		{
			name: "Invalid column in Columns",
			options: RequestOptions{
				Columns: []string{"id", "invalid_column"},
			},
			shouldError: true,
			errorMsg:    "select columns",
		},
		{
			name: "Invalid column in Filters",
			options: RequestOptions{
				Filters: []FilterOption{
					{Column: "invalid_col", Operator: "eq", Value: "test"},
				},
			},
			shouldError: true,
			errorMsg:    "filter",
		},
		{
			name: "Invalid column in Sort",
			options: RequestOptions{
				Sort: []SortOption{
					{Column: "invalid_col", Direction: "ASC"},
				},
			},
			shouldError: true,
			errorMsg:    "sort",
		},
		{
			name: "Valid CQL prefixed columns",
			options: RequestOptions{
				Columns: []string{"id", "cqlComputedField"},
				Filters: []FilterOption{
					{Column: "cqlCustomFilter", Operator: "eq", Value: "test"},
				},
			},
			shouldError: false,
		},
		{
			name: "Invalid column in Preload",
			options: RequestOptions{
				Preload: []PreloadOption{
					{
						Relation: "SomeRelation",
						Columns:  []string{"id", "invalid_col"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "preload",
		},
		{
			name: "Valid preload with valid columns",
			options: RequestOptions{
				Preload: []PreloadOption{
					{
						Relation: "SomeRelation",
						Columns:  []string{"id", "name"},
					},
				},
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateRequestOptions(tt.options)
			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func TestGetValidColumns(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	columns := validator.GetValidColumns()
	if len(columns) == 0 {
		t.Error("Expected to get valid columns, got empty list")
	}

	// Should have at least the columns from TestModel
	if len(columns) < 6 {
		t.Errorf("Expected at least 6 columns, got %d", len(columns))
	}
}

// Test with Bun tags specifically
type BunModel struct {
	ID    int64  `bun:"id,pk"`
	Name  string `bun:"name"`
	Email string `bun:"user_email"`
}

func TestBunTagSupport(t *testing.T) {
	model := BunModel{}
	validator := NewColumnValidator(model)

	// Test that bun tags are properly recognized
	tests := []struct {
		column      string
		shouldError bool
	}{
		{"id", false},
		{"name", false},
		{"user_email", false}, // Bun tag specifies this name
		{"email", true},       // JSON tag would be "email", but bun tag says "user_email"
	}

	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			err := validator.ValidateColumn(tt.column)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error for column '%s'", tt.column)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for column '%s', got: %v", tt.column, err)
			}
		})
	}
}

func TestFilterValidColumns(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	tests := []struct {
		name           string
		input          []string
		expectedOutput []string
	}{
		{
			name:           "All valid columns",
			input:          []string{"id", "name", "email"},
			expectedOutput: []string{"id", "name", "email"},
		},
		{
			name:           "Mix of valid and invalid",
			input:          []string{"id", "invalid_col", "name", "bad_col", "email"},
			expectedOutput: []string{"id", "name", "email"},
		},
		{
			name:           "All invalid columns",
			input:          []string{"bad1", "bad2"},
			expectedOutput: []string{},
		},
		{
			name:           "With CQL prefix (should pass)",
			input:          []string{"id", "cqlComputed", "name"},
			expectedOutput: []string{"id", "cqlComputed", "name"},
		},
		{
			name:           "Empty input",
			input:          []string{},
			expectedOutput: []string{},
		},
		{
			name:           "Nil input",
			input:          nil,
			expectedOutput: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.FilterValidColumns(tt.input)
			if len(result) != len(tt.expectedOutput) {
				t.Errorf("Expected %d columns, got %d", len(tt.expectedOutput), len(result))
			}
			for i, col := range result {
				if col != tt.expectedOutput[i] {
					t.Errorf("At index %d: expected %s, got %s", i, tt.expectedOutput[i], col)
				}
			}
		})
	}
}

func TestFilterRequestOptions(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	options := RequestOptions{
		Columns:     []string{"id", "name", "invalid_col"},
		OmitColumns: []string{"email", "bad_col"},
		Filters: []FilterOption{
			{Column: "name", Operator: "eq", Value: "test"},
			{Column: "invalid_col", Operator: "eq", Value: "test"},
		},
		Sort: []SortOption{
			{Column: "id", Direction: "ASC"},
			{Column: "bad_col", Direction: "DESC"},
		},
	}

	filtered := validator.FilterRequestOptions(options)

	// Check Columns
	if len(filtered.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(filtered.Columns))
	}
	if filtered.Columns[0] != "id" || filtered.Columns[1] != "name" {
		t.Errorf("Expected columns [id, name], got %v", filtered.Columns)
	}

	// Check OmitColumns
	if len(filtered.OmitColumns) != 1 {
		t.Errorf("Expected 1 omit column, got %d", len(filtered.OmitColumns))
	}
	if filtered.OmitColumns[0] != "email" {
		t.Errorf("Expected omit column [email], got %v", filtered.OmitColumns)
	}

	// Check Filters
	if len(filtered.Filters) != 1 {
		t.Errorf("Expected 1 filter, got %d", len(filtered.Filters))
	}
	if filtered.Filters[0].Column != "name" {
		t.Errorf("Expected filter column 'name', got %s", filtered.Filters[0].Column)
	}

	// Check Sort
	if len(filtered.Sort) != 1 {
		t.Errorf("Expected 1 sort, got %d", len(filtered.Sort))
	}
	if filtered.Sort[0].Column != "id" {
		t.Errorf("Expected sort column 'id', got %s", filtered.Sort[0].Column)
	}
}

func TestFilterRequestOptions_ClearsJoinAliases(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	options := RequestOptions{
		Columns: []string{"id", "name"},
		// Set JoinAliases - this should be cleared by FilterRequestOptions
		JoinAliases: []string{"d", "u", "r"},
	}

	filtered := validator.FilterRequestOptions(options)

	// Verify that JoinAliases was cleared (internal field should not persist)
	if filtered.JoinAliases != nil {
		t.Errorf("Expected JoinAliases to be nil after filtering, got %v", filtered.JoinAliases)
	}

	// Verify that other fields are still properly filtered
	if len(filtered.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(filtered.Columns))
	}
}

func TestIsSafeSortExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		shouldPass bool
	}{
		// Safe expressions
		{"Valid subquery", "(SELECT MAX(price) FROM products)", true},
		{"Valid CASE expression", "(CASE WHEN status = 'active' THEN 1 ELSE 0 END)", true},
		{"Valid aggregate", "(COUNT(*) OVER (PARTITION BY category))", true},
		{"Valid function", "(COALESCE(discount, 0))", true},

		// Dangerous expressions - SQL injection attempts
		{"DROP TABLE attempt", "(id); DROP TABLE users; --", false},
		{"DELETE attempt", "(id WHERE 1=1); DELETE FROM users; --", false},
		{"INSERT attempt", "(id); INSERT INTO admin VALUES ('hacker'); --", false},
		{"UPDATE attempt", "(id); UPDATE users SET role='admin'; --", false},
		{"EXEC attempt", "(id); EXEC sp_executesql 'DROP TABLE users'; --", false},
		{"XP_ stored proc", "(id); xp_cmdshell 'dir'; --", false},

		// Comment injection
		{"SQL comment dash", "(id) -- malicious comment", false},
		{"SQL comment block start", "(id) /* comment", false},
		{"SQL comment block end", "(id) comment */", false},

		// Semicolon attempts
		{"Semicolon separator", "(id); SELECT * FROM passwords", false},

		// Empty/invalid
		{"Empty string", "", false},
		{"Just brackets", "()", true}, // Empty but technically valid structure
		{"No brackets", "id", false},  // Must have brackets for expressions
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSafeSortExpression(tt.expression)
			if result != tt.shouldPass {
				t.Errorf("IsSafeSortExpression(%q) = %v, want %v", tt.expression, result, tt.shouldPass)
			}
		})
	}
}

func TestFilterRequestOptions_WithSortExpressions(t *testing.T) {
	model := TestModel{}
	validator := NewColumnValidator(model)

	options := RequestOptions{
		Sort: []SortOption{
			{Column: "id", Direction: "ASC"},                                    // Valid column
			{Column: "(SELECT MAX(age) FROM users)", Direction: "DESC"},         // Safe expression
			{Column: "name", Direction: "ASC"},                                  // Valid column
			{Column: "(id); DROP TABLE users; --", Direction: "DESC"},          // Dangerous expression
			{Column: "invalid_col", Direction: "ASC"},                           // Invalid column
			{Column: "(CASE WHEN age > 18 THEN 1 ELSE 0 END)", Direction: "ASC"}, // Safe expression
		},
	}

	filtered := validator.FilterRequestOptions(options)

	// Should keep: id, safe expression, name, another safe expression
	// Should remove: dangerous expression, invalid column
	expectedCount := 4
	if len(filtered.Sort) != expectedCount {
		t.Errorf("Expected %d sort options, got %d", expectedCount, len(filtered.Sort))
	}

	// Verify the kept options
	if filtered.Sort[0].Column != "id" {
		t.Errorf("Expected first sort to be 'id', got '%s'", filtered.Sort[0].Column)
	}
	if filtered.Sort[1].Column != "(SELECT MAX(age) FROM users)" {
		t.Errorf("Expected second sort to be safe expression, got '%s'", filtered.Sort[1].Column)
	}
	if filtered.Sort[2].Column != "name" {
		t.Errorf("Expected third sort to be 'name', got '%s'", filtered.Sort[2].Column)
	}
}
