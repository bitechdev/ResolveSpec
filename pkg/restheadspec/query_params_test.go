package restheadspec

import (
	"net/http"
	"testing"
)

// MockRequest implements common.Request interface for testing
type MockRequest struct {
	headers     map[string]string
	queryParams map[string]string
}

func (m *MockRequest) Method() string {
	return "GET"
}

func (m *MockRequest) URL() string {
	return "http://example.com/test"
}

func (m *MockRequest) Header(key string) string {
	return m.headers[key]
}

func (m *MockRequest) AllHeaders() map[string]string {
	return m.headers
}

func (m *MockRequest) Body() ([]byte, error) {
	return nil, nil
}

func (m *MockRequest) PathParam(key string) string {
	return ""
}

func (m *MockRequest) QueryParam(key string) string {
	return m.queryParams[key]
}

func (m *MockRequest) AllQueryParams() map[string]string {
	return m.queryParams
}

func (m *MockRequest) UnderlyingRequest() *http.Request {
	// For testing purposes, return nil
	// In real scenarios, you might want to construct a proper http.Request
	return nil
}

func TestParseOptionsFromQueryParams(t *testing.T) {
	handler := NewHandler(nil, nil)

	tests := []struct {
		name        string
		queryParams map[string]string
		headers     map[string]string
		validate    func(t *testing.T, options ExtendedRequestOptions)
	}{
		{
			name: "Parse custom SQL WHERE from query params",
			queryParams: map[string]string{
				"x-custom-sql-w-1": `("v_webui_clients".clientstatus = 0 or "v_webui_clients".clientstatus is null)`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if options.CustomSQLWhere == "" {
					t.Error("Expected CustomSQLWhere to be set from query param")
				}
				expected := `("v_webui_clients".clientstatus = 0 or "v_webui_clients".clientstatus is null)`
				if options.CustomSQLWhere != expected {
					t.Errorf("Expected CustomSQLWhere=%q, got %q", expected, options.CustomSQLWhere)
				}
			},
		},
		{
			name: "Parse sort from query params",
			queryParams: map[string]string{
				"x-sort": "-applicationdate,name",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.Sort) != 2 {
					t.Errorf("Expected 2 sort options, got %d", len(options.Sort))
					return
				}
				if options.Sort[0].Column != "applicationdate" || options.Sort[0].Direction != "DESC" {
					t.Errorf("Expected first sort: applicationdate DESC, got %s %s", options.Sort[0].Column, options.Sort[0].Direction)
				}
				if options.Sort[1].Column != "name" || options.Sort[1].Direction != "ASC" {
					t.Errorf("Expected second sort: name ASC, got %s %s", options.Sort[1].Column, options.Sort[1].Direction)
				}
			},
		},
		{
			name: "Parse limit and offset from query params",
			queryParams: map[string]string{
				"x-limit":  "100",
				"x-offset": "50",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if options.Limit == nil || *options.Limit != 100 {
					t.Errorf("Expected limit=100, got %v", options.Limit)
				}
				if options.Offset == nil || *options.Offset != 50 {
					t.Errorf("Expected offset=50, got %v", options.Offset)
				}
			},
		},
		{
			name: "Parse field filters from query params",
			queryParams: map[string]string{
				"x-fieldfilter-status": "active",
				"x-fieldfilter-type":   "user",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.Filters) != 2 {
					t.Errorf("Expected 2 filters, got %d", len(options.Filters))
					return
				}
				// Check that filters were created
				foundStatus := false
				foundType := false
				for _, filter := range options.Filters {
					if filter.Column == "status" && filter.Value == "active" && filter.Operator == "eq" {
						foundStatus = true
					}
					if filter.Column == "type" && filter.Value == "user" && filter.Operator == "eq" {
						foundType = true
					}
				}
				if !foundStatus {
					t.Error("Expected status filter not found")
				}
				if !foundType {
					t.Error("Expected type filter not found")
				}
			},
		},
		{
			name: "Parse select fields from query params",
			queryParams: map[string]string{
				"x-select-fields": "id,name,email",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.Columns) != 3 {
					t.Errorf("Expected 3 columns, got %d", len(options.Columns))
					return
				}
				expected := []string{"id", "name", "email"}
				for i, col := range expected {
					if i >= len(options.Columns) || options.Columns[i] != col {
						t.Errorf("Expected column[%d]=%s, got %v", i, col, options.Columns)
					}
				}
			},
		},
		{
			name: "Parse preload from query params",
			queryParams: map[string]string{
				"x-preload": "posts:title,content|comments",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.Preload) != 2 {
					t.Errorf("Expected 2 preload options, got %d", len(options.Preload))
					return
				}
				// Check first preload (posts with columns)
				if options.Preload[0].Relation != "posts" {
					t.Errorf("Expected first preload relation=posts, got %s", options.Preload[0].Relation)
				}
				if len(options.Preload[0].Columns) != 2 {
					t.Errorf("Expected 2 columns for posts preload, got %d", len(options.Preload[0].Columns))
				}
				// Check second preload (comments without columns)
				if options.Preload[1].Relation != "comments" {
					t.Errorf("Expected second preload relation=comments, got %s", options.Preload[1].Relation)
				}
			},
		},
		{
			name: "Query params take precedence over headers",
			queryParams: map[string]string{
				"x-limit": "100",
			},
			headers: map[string]string{
				"X-Limit": "50",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if options.Limit == nil || *options.Limit != 100 {
					t.Errorf("Expected query param limit=100 to override header, got %v", options.Limit)
				}
			},
		},
		{
			name: "Parse search operators from query params",
			queryParams: map[string]string{
				"x-searchop-contains-name": "john",
				"x-searchop-gt-age":        "18",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.Filters) != 2 {
					t.Errorf("Expected 2 filters, got %d", len(options.Filters))
					return
				}
				// Check for ILIKE filter
				foundContains := false
				foundGt := false
				for _, filter := range options.Filters {
					if filter.Column == "name" && filter.Operator == "ilike" {
						foundContains = true
					}
					if filter.Column == "age" && filter.Operator == "gt" && filter.Value == "18" {
						foundGt = true
					}
				}
				if !foundContains {
					t.Error("Expected contains filter not found")
				}
				if !foundGt {
					t.Error("Expected gt filter not found")
				}
			},
		},
		{
			name: "Parse complex example with multiple params",
			queryParams: map[string]string{
				"x-custom-sql-w-1":     `("v_webui_clients".clientstatus = 0)`,
				"x-sort":               "-applicationdate",
				"x-limit":              "100",
				"x-select-fields":      "id,name,status",
				"x-fieldfilter-active": "true",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				// Validate CustomSQLWhere
				if options.CustomSQLWhere == "" {
					t.Error("Expected CustomSQLWhere to be set")
				}
				// Validate Sort
				if len(options.Sort) != 1 || options.Sort[0].Column != "applicationdate" || options.Sort[0].Direction != "DESC" {
					t.Errorf("Expected sort by applicationdate DESC, got %v", options.Sort)
				}
				// Validate Limit
				if options.Limit == nil || *options.Limit != 100 {
					t.Errorf("Expected limit=100, got %v", options.Limit)
				}
				// Validate Columns
				if len(options.Columns) != 3 {
					t.Errorf("Expected 3 columns, got %d", len(options.Columns))
				}
				// Validate Filters
				if len(options.Filters) < 1 {
					t.Error("Expected at least 1 filter")
				}
			},
		},
		{
			name: "Parse distinct flag from query params",
			queryParams: map[string]string{
				"x-distinct": "true",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if !options.Distinct {
					t.Error("Expected Distinct to be true")
				}
			},
		},
		{
			name: "Parse skip count flag from query params",
			queryParams: map[string]string{
				"x-skipcount": "true",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if !options.SkipCount {
					t.Error("Expected SkipCount to be true")
				}
			},
		},
		{
			name: "Parse response format from query params",
			queryParams: map[string]string{
				"x-syncfusion": "true",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if options.ResponseFormat != "syncfusion" {
					t.Errorf("Expected ResponseFormat=syncfusion, got %s", options.ResponseFormat)
				}
			},
		},
		{
			name: "Parse custom SQL OR from query params",
			queryParams: map[string]string{
				"x-custom-sql-or": `("field1" = 'value1' OR "field2" = 'value2')`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if options.CustomSQLOr == "" {
					t.Error("Expected CustomSQLOr to be set")
				}
				expected := `("field1" = 'value1' OR "field2" = 'value2')`
				if options.CustomSQLOr != expected {
					t.Errorf("Expected CustomSQLOr=%q, got %q", expected, options.CustomSQLOr)
				}
			},
		},
		{
			name: "Parse custom SQL JOIN from query params",
			queryParams: map[string]string{
				"x-custom-sql-join": `LEFT JOIN departments d ON d.id = employees.department_id`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.CustomSQLJoin) == 0 {
					t.Error("Expected CustomSQLJoin to be set")
					return
				}
				if len(options.CustomSQLJoin) != 1 {
					t.Errorf("Expected 1 custom SQL join, got %d", len(options.CustomSQLJoin))
					return
				}
				expected := `LEFT JOIN departments d ON d.id = employees.department_id`
				if options.CustomSQLJoin[0] != expected {
					t.Errorf("Expected CustomSQLJoin[0]=%q, got %q", expected, options.CustomSQLJoin[0])
				}
			},
		},
		{
			name: "Parse multiple custom SQL JOINs from query params",
			queryParams: map[string]string{
				"x-custom-sql-join": `LEFT JOIN departments d ON d.id = e.dept_id | INNER JOIN roles r ON r.id = e.role_id`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.CustomSQLJoin) != 2 {
					t.Errorf("Expected 2 custom SQL joins, got %d", len(options.CustomSQLJoin))
					return
				}
				expected1 := `LEFT JOIN departments d ON d.id = e.dept_id`
				expected2 := `INNER JOIN roles r ON r.id = e.role_id`
				if options.CustomSQLJoin[0] != expected1 {
					t.Errorf("Expected CustomSQLJoin[0]=%q, got %q", expected1, options.CustomSQLJoin[0])
				}
				if options.CustomSQLJoin[1] != expected2 {
					t.Errorf("Expected CustomSQLJoin[1]=%q, got %q", expected2, options.CustomSQLJoin[1])
				}
			},
		},
		{
			name: "Parse custom SQL JOIN from headers",
			headers: map[string]string{
				"X-Custom-SQL-Join": `LEFT JOIN users u ON u.id = posts.user_id`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.CustomSQLJoin) == 0 {
					t.Error("Expected CustomSQLJoin to be set from header")
					return
				}
				expected := `LEFT JOIN users u ON u.id = posts.user_id`
				if options.CustomSQLJoin[0] != expected {
					t.Errorf("Expected CustomSQLJoin[0]=%q, got %q", expected, options.CustomSQLJoin[0])
				}
			},
		},
		{
			name: "Extract aliases from custom SQL JOIN",
			queryParams: map[string]string{
				"x-custom-sql-join": `LEFT JOIN departments d ON d.id = employees.department_id`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.JoinAliases) == 0 {
					t.Error("Expected JoinAliases to be extracted")
					return
				}
				if len(options.JoinAliases) != 1 {
					t.Errorf("Expected 1 join alias, got %d", len(options.JoinAliases))
					return
				}
				if options.JoinAliases[0] != "d" {
					t.Errorf("Expected join alias 'd', got %q", options.JoinAliases[0])
				}
				// Also check that it's in the embedded RequestOptions
				if len(options.RequestOptions.JoinAliases) != 1 || options.RequestOptions.JoinAliases[0] != "d" {
					t.Error("Expected join alias to also be in RequestOptions.JoinAliases")
				}
			},
		},
		{
			name: "Extract multiple aliases from multiple custom SQL JOINs",
			queryParams: map[string]string{
				"x-custom-sql-join": `LEFT JOIN departments d ON d.id = e.dept_id | INNER JOIN roles AS r ON r.id = e.role_id`,
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				if len(options.JoinAliases) != 2 {
					t.Errorf("Expected 2 join aliases, got %d", len(options.JoinAliases))
					return
				}
				expectedAliases := []string{"d", "r"}
				for i, expected := range expectedAliases {
					if options.JoinAliases[i] != expected {
						t.Errorf("Expected join alias[%d]=%q, got %q", i, expected, options.JoinAliases[i])
					}
				}
			},
		},
		{
			name: "Custom JOIN with sort on joined table",
			queryParams: map[string]string{
				"x-custom-sql-join": `LEFT JOIN departments d ON d.id = employees.department_id`,
				"x-sort":            "d.name,employees.id",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				// Verify join was added
				if len(options.CustomSQLJoin) != 1 {
					t.Errorf("Expected 1 custom SQL join, got %d", len(options.CustomSQLJoin))
					return
				}
				// Verify alias was extracted
				if len(options.JoinAliases) != 1 || options.JoinAliases[0] != "d" {
					t.Error("Expected join alias 'd' to be extracted")
					return
				}
				// Verify sort was parsed
				if len(options.Sort) != 2 {
					t.Errorf("Expected 2 sort options, got %d", len(options.Sort))
					return
				}
				if options.Sort[0].Column != "d.name" {
					t.Errorf("Expected first sort column 'd.name', got %q", options.Sort[0].Column)
				}
				if options.Sort[1].Column != "employees.id" {
					t.Errorf("Expected second sort column 'employees.id', got %q", options.Sort[1].Column)
				}
			},
		},
		{
			name: "Custom JOIN with filter on joined table",
			queryParams: map[string]string{
				"x-custom-sql-join":    `LEFT JOIN departments d ON d.id = employees.department_id`,
				"x-searchop-eq-d.name": "Engineering",
			},
			validate: func(t *testing.T, options ExtendedRequestOptions) {
				// Verify join was added
				if len(options.CustomSQLJoin) != 1 {
					t.Error("Expected 1 custom SQL join")
					return
				}
				// Verify alias was extracted
				if len(options.JoinAliases) != 1 || options.JoinAliases[0] != "d" {
					t.Error("Expected join alias 'd' to be extracted")
					return
				}
				// Verify filter was parsed
				if len(options.Filters) != 1 {
					t.Errorf("Expected 1 filter, got %d", len(options.Filters))
					return
				}
				if options.Filters[0].Column != "d.name" {
					t.Errorf("Expected filter column 'd.name', got %q", options.Filters[0].Column)
				}
				if options.Filters[0].Operator != "eq" {
					t.Errorf("Expected filter operator 'eq', got %q", options.Filters[0].Operator)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock request
			req := &MockRequest{
				headers:     tt.headers,
				queryParams: tt.queryParams,
			}
			if req.headers == nil {
				req.headers = make(map[string]string)
			}
			if req.queryParams == nil {
				req.queryParams = make(map[string]string)
			}

			// Parse options
			options := handler.parseOptionsFromHeaders(req, nil)

			// Validate
			tt.validate(t, options)
		})
	}
}

func TestQueryParamsWithURLEncoding(t *testing.T) {
	handler := NewHandler(nil, nil)

	// Test with URL-encoded query parameter (like the user's example)
	req := &MockRequest{
		headers: make(map[string]string),
		queryParams: map[string]string{
			// URL-encoded version of the SQL WHERE clause
			"x-custom-sql-w-1": `("v_webui_clients".clientstatus = 0 or "v_webui_clients".clientstatus is null) and ("v_webui_clients".inactive = 0 or "v_webui_clients".inactive is null)`,
		},
	}

	options := handler.parseOptionsFromHeaders(req, nil)

	if options.CustomSQLWhere == "" {
		t.Error("Expected CustomSQLWhere to be set from URL-encoded query param")
	}

	// The SQL should contain the expected conditions
	if !contains(options.CustomSQLWhere, "clientstatus") {
		t.Error("Expected CustomSQLWhere to contain 'clientstatus'")
	}
	if !contains(options.CustomSQLWhere, "inactive") {
		t.Error("Expected CustomSQLWhere to contain 'inactive'")
	}
}

func TestHeadersAndQueryParamsCombined(t *testing.T) {
	handler := NewHandler(nil, nil)

	// Test that headers and query params can work together
	req := &MockRequest{
		headers: map[string]string{
			"X-Select-Fields": "id,name",
			"X-Limit":         "50",
		},
		queryParams: map[string]string{
			"x-sort":   "-created_at",
			"x-offset": "10",
			// This should override the header value
			"x-limit": "100",
		},
	}

	options := handler.parseOptionsFromHeaders(req, nil)

	// Verify columns from header
	if len(options.Columns) != 2 {
		t.Errorf("Expected 2 columns from header, got %d", len(options.Columns))
	}

	// Verify sort from query param
	if len(options.Sort) != 1 || options.Sort[0].Column != "created_at" {
		t.Errorf("Expected sort from query param, got %v", options.Sort)
	}

	// Verify offset from query param
	if options.Offset == nil || *options.Offset != 10 {
		t.Errorf("Expected offset=10 from query param, got %v", options.Offset)
	}

	// Verify limit from query param (should override header)
	if options.Limit == nil {
		t.Error("Expected limit to be set from query param")
	} else if *options.Limit != 100 {
		t.Errorf("Expected limit=100 from query param (overriding header), got %d", *options.Limit)
	}
}

// TestCustomJoinAliasExtraction tests the extractJoinAlias helper function
func TestCustomJoinAliasExtraction(t *testing.T) {
	tests := []struct {
		name     string
		join     string
		expected string
	}{
		{
			name:     "LEFT JOIN with alias",
			join:     "LEFT JOIN departments d ON d.id = employees.department_id",
			expected: "d",
		},
		{
			name:     "INNER JOIN with AS keyword",
			join:     "INNER JOIN users AS u ON u.id = posts.user_id",
			expected: "u",
		},
		{
			name:     "Simple JOIN with alias",
			join:     "JOIN roles r ON r.id = user_roles.role_id",
			expected: "r",
		},
		{
			name:     "JOIN without alias (just table name)",
			join:     "JOIN departments ON departments.id = employees.dept_id",
			expected: "",
		},
		{
			name:     "RIGHT JOIN with alias",
			join:     "RIGHT JOIN orders o ON o.customer_id = customers.id",
			expected: "o",
		},
		{
			name:     "FULL OUTER JOIN with AS",
			join:     "FULL OUTER JOIN products AS p ON p.id = order_items.product_id",
			expected: "p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJoinAlias(tt.join)
			if result != tt.expected {
				t.Errorf("extractJoinAlias(%q) = %q, want %q", tt.join, result, tt.expected)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
