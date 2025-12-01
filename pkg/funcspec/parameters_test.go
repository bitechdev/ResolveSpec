package funcspec

import (
	"strings"
	"testing"
)

// TestParseParameters tests the comprehensive parameter parsing
func TestParseParameters(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name        string
		queryParams map[string]string
		headers     map[string]string
		validate    func(t *testing.T, params *RequestParameters)
	}{
		{
			name: "Parse field selection",
			headers: map[string]string{
				"X-Select-Fields":     "id,name,email",
				"X-Not-Select-Fields": "password,ssn",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if len(params.SelectFields) != 3 {
					t.Errorf("Expected 3 select fields, got %d", len(params.SelectFields))
				}
				if len(params.NotSelectFields) != 2 {
					t.Errorf("Expected 2 not-select fields, got %d", len(params.NotSelectFields))
				}
			},
		},
		{
			name: "Parse distinct flag",
			headers: map[string]string{
				"X-Distinct": "true",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if !params.Distinct {
					t.Error("Expected Distinct to be true")
				}
			},
		},
		{
			name: "Parse field filters",
			headers: map[string]string{
				"X-FieldFilter-Status": "active",
				"X-FieldFilter-Type":   "admin",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if len(params.FieldFilters) != 2 {
					t.Errorf("Expected 2 field filters, got %d", len(params.FieldFilters))
				}
				if params.FieldFilters["status"] != "active" {
					t.Errorf("Expected status filter=active, got %s", params.FieldFilters["status"])
				}
			},
		},
		{
			name: "Parse search filters",
			headers: map[string]string{
				"X-SearchFilter-Name":  "john",
				"X-SearchFilter-Email": "test",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if len(params.SearchFilters) != 2 {
					t.Errorf("Expected 2 search filters, got %d", len(params.SearchFilters))
				}
			},
		},
		{
			name: "Parse sort columns",
			queryParams: map[string]string{
				"sort": "-created_at,name",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.SortColumns != "-created_at,name" {
					t.Errorf("Expected sort columns=-created_at,name, got %s", params.SortColumns)
				}
			},
		},
		{
			name: "Parse limit and offset",
			queryParams: map[string]string{
				"limit":  "100",
				"offset": "50",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.Limit != 100 {
					t.Errorf("Expected limit=100, got %d", params.Limit)
				}
				if params.Offset != 50 {
					t.Errorf("Expected offset=50, got %d", params.Offset)
				}
			},
		},
		{
			name: "Parse skip count",
			headers: map[string]string{
				"X-SkipCount": "true",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if !params.SkipCount {
					t.Error("Expected SkipCount to be true")
				}
			},
		},
		{
			name: "Parse response format - syncfusion",
			headers: map[string]string{
				"X-Syncfusion": "true",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.ResponseFormat != "syncfusion" {
					t.Errorf("Expected ResponseFormat=syncfusion, got %s", params.ResponseFormat)
				}
				if !params.ComplexAPI {
					t.Error("Expected ComplexAPI to be true for syncfusion format")
				}
			},
		},
		{
			name: "Parse response format - detail",
			headers: map[string]string{
				"X-DetailAPI": "true",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.ResponseFormat != "detail" {
					t.Errorf("Expected ResponseFormat=detail, got %s", params.ResponseFormat)
				}
			},
		},
		{
			name: "Parse simple API",
			headers: map[string]string{
				"X-SimpleAPI": "true",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.ResponseFormat != "simple" {
					t.Errorf("Expected ResponseFormat=simple, got %s", params.ResponseFormat)
				}
				if params.ComplexAPI {
					t.Error("Expected ComplexAPI to be false for simple API")
				}
			},
		},
		{
			name: "Parse custom SQL WHERE",
			headers: map[string]string{
				"X-Custom-SQL-W": "status = 'active' AND deleted = false",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if params.CustomSQLWhere == "" {
					t.Error("Expected CustomSQLWhere to be set")
				}
			},
		},
		{
			name: "Parse search operators - AND",
			headers: map[string]string{
				"X-SearchOp-Eq-Name":  "john",
				"X-SearchOp-Gt-Age":   "18",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if len(params.SearchOps) != 2 {
					t.Errorf("Expected 2 search operators, got %d", len(params.SearchOps))
				}
				if op, exists := params.SearchOps["name"]; exists {
					if op.Operator != "eq" {
						t.Errorf("Expected operator=eq for name, got %s", op.Operator)
					}
					if op.Logic != "AND" {
						t.Errorf("Expected logic=AND, got %s", op.Logic)
					}
				} else {
					t.Error("Expected name search operator to exist")
				}
			},
		},
		{
			name: "Parse search operators - OR",
			headers: map[string]string{
				"X-SearchOr-Like-Description": "test",
			},
			validate: func(t *testing.T, params *RequestParameters) {
				if op, exists := params.SearchOps["description"]; exists {
					if op.Logic != "OR" {
						t.Errorf("Expected logic=OR, got %s", op.Logic)
					}
				} else {
					t.Error("Expected description search operator to exist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestRequest("GET", "/test", tt.queryParams, tt.headers, nil)
			params := handler.ParseParameters(req)

			if tt.validate != nil {
				tt.validate(t, params)
			}
		})
	}
}

// TestBuildFilterCondition tests the filter condition builder
func TestBuildFilterCondition(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name      string
		colName   string
		operator  FilterOperator
		expected  string
	}{
		{
			name:    "Equals operator - numeric",
			colName: "age",
			operator: FilterOperator{
				Operator: "eq",
				Value:    "25",
				Logic:    "AND",
			},
			expected: "age = 25",
		},
		{
			name:    "Equals operator - string",
			colName: "name",
			operator: FilterOperator{
				Operator: "eq",
				Value:    "john",
				Logic:    "AND",
			},
			expected: "name = 'john'",
		},
		{
			name:    "Not equals operator",
			colName: "status",
			operator: FilterOperator{
				Operator: "neq",
				Value:    "inactive",
				Logic:    "AND",
			},
			expected: "status != 'inactive'",
		},
		{
			name:    "Greater than operator",
			colName: "age",
			operator: FilterOperator{
				Operator: "gt",
				Value:    "18",
				Logic:    "AND",
			},
			expected: "age > 18",
		},
		{
			name:    "Less than operator",
			colName: "price",
			operator: FilterOperator{
				Operator: "lt",
				Value:    "100",
				Logic:    "AND",
			},
			expected: "price < 100",
		},
		{
			name:    "Contains operator",
			colName: "description",
			operator: FilterOperator{
				Operator: "contains",
				Value:    "test",
				Logic:    "AND",
			},
			expected: "description ILIKE '%test%'",
		},
		{
			name:    "Starts with operator",
			colName: "name",
			operator: FilterOperator{
				Operator: "startswith",
				Value:    "john",
				Logic:    "AND",
			},
			expected: "name ILIKE 'john%'",
		},
		{
			name:    "Ends with operator",
			colName: "email",
			operator: FilterOperator{
				Operator: "endswith",
				Value:    "@example.com",
				Logic:    "AND",
			},
			expected: "email ILIKE '%@example.com'",
		},
		{
			name:    "Between operator",
			colName: "age",
			operator: FilterOperator{
				Operator: "between",
				Value:    "18,65",
				Logic:    "AND",
			},
			expected: "age > 18 AND age < 65",
		},
		{
			name:    "IN operator",
			colName: "status",
			operator: FilterOperator{
				Operator: "in",
				Value:    "active,pending,approved",
				Logic:    "AND",
			},
			expected: "status IN ('active', 'pending', 'approved')",
		},
		{
			name:    "IS NULL operator",
			colName: "deleted_at",
			operator: FilterOperator{
				Operator: "null",
				Value:    "",
				Logic:    "AND",
			},
			expected: "(deleted_at IS NULL OR deleted_at = '')",
		},
		{
			name:    "IS NOT NULL operator",
			colName: "created_at",
			operator: FilterOperator{
				Operator: "notnull",
				Value:    "",
				Logic:    "AND",
			},
			expected: "(created_at IS NOT NULL AND created_at != '')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.buildFilterCondition(tt.colName, tt.operator)
			if result != tt.expected {
				t.Errorf("Expected: %s\nGot: %s", tt.expected, result)
			}
		})
	}
}

// TestApplyFilters tests the filter application to SQL queries
func TestApplyFilters(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name         string
		sqlQuery     string
		params       *RequestParameters
		expectedSQL  string
		shouldContain []string
	}{
		{
			name:     "Apply field filter",
			sqlQuery: "SELECT * FROM users",
			params: &RequestParameters{
				FieldFilters: map[string]string{
					"status": "active",
				},
			},
			shouldContain: []string{"WHERE", "status"},
		},
		{
			name:     "Apply search filter",
			sqlQuery: "SELECT * FROM users",
			params: &RequestParameters{
				SearchFilters: map[string]string{
					"name": "john",
				},
			},
			shouldContain: []string{"WHERE", "name", "ILIKE"},
		},
		{
			name:     "Apply search operators",
			sqlQuery: "SELECT * FROM users",
			params: &RequestParameters{
				SearchOps: map[string]FilterOperator{
					"age": {
						Operator: "gt",
						Value:    "18",
						Logic:    "AND",
					},
				},
			},
			shouldContain: []string{"WHERE", "age", ">", "18"},
		},
		{
			name:     "Apply custom SQL WHERE",
			sqlQuery: "SELECT * FROM users",
			params: &RequestParameters{
				CustomSQLWhere: "deleted = false",
			},
			shouldContain: []string{"WHERE", "deleted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.ApplyFilters(tt.sqlQuery, tt.params)

			for _, expected := range tt.shouldContain {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected SQL to contain %q, got: %s", expected, result)
				}
			}
		})
	}
}

// TestApplyDistinct tests DISTINCT application
func TestApplyDistinct(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name        string
		sqlQuery    string
		distinct    bool
		shouldHave  string
	}{
		{
			name:       "Apply DISTINCT",
			sqlQuery:   "SELECT id, name FROM users",
			distinct:   true,
			shouldHave: "SELECT DISTINCT",
		},
		{
			name:       "Do not apply DISTINCT",
			sqlQuery:   "SELECT id, name FROM users",
			distinct:   false,
			shouldHave: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &RequestParameters{Distinct: tt.distinct}
			result := handler.ApplyDistinct(tt.sqlQuery, params)

			if tt.shouldHave != "" {
				if !strings.Contains(result, tt.shouldHave) {
					t.Errorf("Expected SQL to contain %q, got: %s", tt.shouldHave, result)
				}
			} else {
				// Should not have DISTINCT when not requested
				if strings.Contains(result, "DISTINCT") && !tt.distinct {
					t.Errorf("SQL should not contain DISTINCT when not requested: %s", result)
				}
			}
		})
	}
}

// TestParseCommaSeparated tests comma-separated value parsing
func TestParseCommaSeparated(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Simple comma-separated",
			input:    "id,name,email",
			expected: []string{"id", "name", "email"},
		},
		{
			name:     "With spaces",
			input:    "id, name, email",
			expected: []string{"id", "name", "email"},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "Single value",
			input:    "id",
			expected: []string{"id"},
		},
		{
			name:     "With extra commas",
			input:    "id,,name,,email",
			expected: []string{"id", "name", "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.parseCommaSeparated(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d values, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("Expected value %d to be %s, got %s", i, expected, result[i])
				}
			}
		})
	}
}

// TestSqlQryWhereOr tests OR WHERE clause manipulation
func TestSqlQryWhereOr(t *testing.T) {
	tests := []struct {
		name          string
		sqlQuery      string
		condition     string
		shouldContain []string
	}{
		{
			name:          "Add WHERE with OR to query without WHERE",
			sqlQuery:      "SELECT * FROM users",
			condition:     "status = 'inactive'",
			shouldContain: []string{"WHERE", "status = 'inactive'"},
		},
		{
			name:          "Add OR to query with existing WHERE",
			sqlQuery:      "SELECT * FROM users WHERE id > 0",
			condition:     "status = 'inactive'",
			shouldContain: []string{"WHERE", "OR", "(status = 'inactive')"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sqlQryWhereOr(tt.sqlQuery, tt.condition)

			for _, expected := range tt.shouldContain {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected SQL to contain %q, got: %s", expected, result)
				}
			}
		})
	}
}
