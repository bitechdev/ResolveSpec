package funcspec

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// MockDatabase implements common.Database interface for testing
type MockDatabase struct {
	QueryFunc          func(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	ExecFunc           func(ctx context.Context, query string, args ...interface{}) (common.Result, error)
	RunInTransactionFunc func(ctx context.Context, fn func(common.Database) error) error
}

func (m *MockDatabase) NewSelect() common.SelectQuery {
	return nil
}

func (m *MockDatabase) NewInsert() common.InsertQuery {
	return nil
}

func (m *MockDatabase) NewUpdate() common.UpdateQuery {
	return nil
}

func (m *MockDatabase) NewDelete() common.DeleteQuery {
	return nil
}

func (m *MockDatabase) Exec(ctx context.Context, query string, args ...interface{}) (common.Result, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, query, args...)
	}
	return &MockResult{rows: 0}, nil
}

func (m *MockDatabase) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, dest, query, args...)
	}
	return nil
}

func (m *MockDatabase) BeginTx(ctx context.Context) (common.Database, error) {
	return m, nil
}

func (m *MockDatabase) CommitTx(ctx context.Context) error {
	return nil
}

func (m *MockDatabase) RollbackTx(ctx context.Context) error {
	return nil
}

func (m *MockDatabase) RunInTransaction(ctx context.Context, fn func(common.Database) error) error {
	if m.RunInTransactionFunc != nil {
		return m.RunInTransactionFunc(ctx, fn)
	}
	return fn(m)
}

// MockResult implements common.Result interface for testing
type MockResult struct {
	rows int64
	id   int64
}

func (m *MockResult) RowsAffected() int64 {
	return m.rows
}

func (m *MockResult) LastInsertId() (int64, error) {
	return m.id, nil
}

// Helper function to create a test request with user context
func createTestRequest(method, path string, queryParams map[string]string, headers map[string]string, body []byte) *http.Request {
	u, _ := url.Parse(path)
	if queryParams != nil {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req := httptest.NewRequest(method, u.String(), bodyReader)

	if headers != nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	// Add user context
	userCtx := &security.UserContext{
		UserID:    1,
		UserName:  "testuser",
		SessionID: "test-session-123",
	}
	ctx := context.WithValue(req.Context(), security.UserContextKey, userCtx)
	req = req.WithContext(ctx)

	return req
}

// TestNewHandler tests handler creation
func TestNewHandler(t *testing.T) {
	db := &MockDatabase{}
	handler := NewHandler(db)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	if handler.db != db {
		t.Error("Expected handler to have the provided database")
	}

	if handler.hooks == nil {
		t.Error("Expected handler to have a hook registry")
	}
}

// TestHandlerHooks tests the Hooks method
func TestHandlerHooks(t *testing.T) {
	handler := NewHandler(&MockDatabase{})
	hooks := handler.Hooks()

	if hooks == nil {
		t.Fatal("Expected hooks registry to be non-nil")
	}

	// Should return the same instance
	hooks2 := handler.Hooks()
	if hooks != hooks2 {
		t.Error("Expected Hooks() to return the same registry instance")
	}
}

// TestExtractInputVariables tests the extractInputVariables function
func TestExtractInputVariables(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name          string
		sqlQuery      string
		expectedVars  []string
	}{
		{
			name:         "No variables",
			sqlQuery:     "SELECT * FROM users",
			expectedVars: []string{},
		},
		{
			name:         "Single variable",
			sqlQuery:     "SELECT * FROM users WHERE id = [user_id]",
			expectedVars: []string{"[user_id]"},
		},
		{
			name:         "Multiple variables",
			sqlQuery:     "SELECT * FROM users WHERE id = [user_id] AND name = [user_name]",
			expectedVars: []string{"[user_id]", "[user_name]"},
		},
		{
			name:         "Nested brackets",
			sqlQuery:     "SELECT * FROM users WHERE data::jsonb @> '[field]'::jsonb AND id = [user_id]",
			expectedVars: []string{"[field]", "[user_id]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputvars := make([]string, 0)
			result := handler.extractInputVariables(tt.sqlQuery, &inputvars)

			if result != tt.sqlQuery {
				t.Errorf("Expected SQL query to be unchanged, got %s", result)
			}

			if len(inputvars) != len(tt.expectedVars) {
				t.Errorf("Expected %d variables, got %d: %v", len(tt.expectedVars), len(inputvars), inputvars)
				return
			}

			for i, expected := range tt.expectedVars {
				if inputvars[i] != expected {
					t.Errorf("Expected variable %d to be %s, got %s", i, expected, inputvars[i])
				}
			}
		})
	}
}

// TestValidSQL tests the SQL sanitization function
func TestValidSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		mode     string
		expected string
	}{
		{
			name:     "Column name with valid characters",
			input:    "user_id",
			mode:     "colname",
			expected: "user_id",
		},
		{
			name:     "Column name with dots (table.column)",
			input:    "users.user_id",
			mode:     "colname",
			expected: "users.user_id",
		},
		{
			name:     "Column name with SQL injection attempt",
			input:    "id'; DROP TABLE users--",
			mode:     "colname",
			expected: "idDROPTABLEusers",
		},
		{
			name:     "Column value with single quotes",
			input:    "O'Brien",
			mode:     "colvalue",
			expected: "O''Brien",
		},
		{
			name:     "Select with dangerous keywords",
			input:    "name, email; DROP TABLE users",
			mode:     "select",
			expected: "name, email TABLE users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidSQL(tt.input, tt.mode)
			if result != tt.expected {
				t.Errorf("ValidSQL(%q, %q) = %q, expected %q", tt.input, tt.mode, result, tt.expected)
			}
		})
	}
}

// TestIsNumeric tests the IsNumeric function
func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123", true},
		{"123.45", true},
		{"-123", true},
		{"-123.45", true},
		{"0", true},
		{"abc", false},
		{"12.34.56", false},
		{"", false},
		{"123abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("IsNumeric(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSqlQryWhere tests the WHERE clause manipulation
func TestSqlQryWhere(t *testing.T) {
	tests := []struct {
		name      string
		sqlQuery  string
		condition string
		expected  string
	}{
		{
			name:      "Add WHERE to query without WHERE",
			sqlQuery:  "SELECT * FROM users",
			condition: "status = 'active'",
			expected:  "SELECT * FROM users WHERE status = 'active' ",
		},
		{
			name:      "Add AND to query with existing WHERE",
			sqlQuery:  "SELECT * FROM users WHERE id > 0",
			condition: "status = 'active'",
			expected:  "SELECT * FROM users WHERE id > 0 AND status = 'active' ",
		},
		{
			name:      "Add WHERE before ORDER BY",
			sqlQuery:  "SELECT * FROM users ORDER BY name",
			condition: "status = 'active'",
			expected:  "SELECT * FROM users WHERE status = 'active'  ORDER BY name",
		},
		{
			name:      "Add WHERE before GROUP BY",
			sqlQuery:  "SELECT COUNT(*) FROM users GROUP BY department",
			condition: "status = 'active'",
			expected:  "SELECT COUNT(*) FROM users WHERE status = 'active'  GROUP BY department",
		},
		{
			name:      "Add WHERE before LIMIT",
			sqlQuery:  "SELECT * FROM users LIMIT 10",
			condition: "status = 'active'",
			expected:  "SELECT * FROM users WHERE status = 'active'  LIMIT 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sqlQryWhere(tt.sqlQuery, tt.condition)
			if result != tt.expected {
				t.Errorf("sqlQryWhere() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestGetIPAddress tests IP address extraction
func TestGetIPAddress(t *testing.T) {
	tests := []struct {
		name      string
		setupReq  func() *http.Request
		expected  string
	}{
		{
			name: "X-Forwarded-For header",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Forwarded-For", "192.168.1.100, 10.0.0.1")
				return req
			},
			expected: "192.168.1.100",
		},
		{
			name: "X-Real-IP header",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Real-IP", "192.168.1.200")
				return req
			},
			expected: "192.168.1.200",
		},
		{
			name: "RemoteAddr fallback",
			setupReq: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.RemoteAddr = "192.168.1.1:12345"
				return req
			},
			expected: "192.168.1.1:12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupReq()
			result := getIPAddress(req)
			if result != tt.expected {
				t.Errorf("getIPAddress() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestParsePaginationParams tests pagination parameter parsing
func TestParsePaginationParams(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name           string
		queryParams    map[string]string
		expectedSort   string
		expectedLimit  int
		expectedOffset int
	}{
		{
			name:           "No parameters - defaults",
			queryParams:    map[string]string{},
			expectedSort:   "",
			expectedLimit:  20,
			expectedOffset: 0,
		},
		{
			name: "All parameters provided",
			queryParams: map[string]string{
				"sort":   "name,-created_at",
				"limit":  "100",
				"offset": "50",
			},
			expectedSort:   "name,-created_at",
			expectedLimit:  100,
			expectedOffset: 50,
		},
		{
			name: "Invalid limit - use default",
			queryParams: map[string]string{
				"limit": "invalid",
			},
			expectedSort:   "",
			expectedLimit:  20,
			expectedOffset: 0,
		},
		{
			name: "Negative offset - use default",
			queryParams: map[string]string{
				"offset": "-10",
			},
			expectedSort:   "",
			expectedLimit:  20,
			expectedOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestRequest("GET", "/test", tt.queryParams, nil, nil)
			sort, limit, offset := handler.parsePaginationParams(req)

			if sort != tt.expectedSort {
				t.Errorf("Expected sort=%q, got %q", tt.expectedSort, sort)
			}
			if limit != tt.expectedLimit {
				t.Errorf("Expected limit=%d, got %d", tt.expectedLimit, limit)
			}
			if offset != tt.expectedOffset {
				t.Errorf("Expected offset=%d, got %d", tt.expectedOffset, offset)
			}
		})
	}
}

// TestSqlQuery tests the SqlQuery handler for single record queries
func TestSqlQuery(t *testing.T) {
	tests := []struct {
		name           string
		sqlQuery       string
		blankParams    bool
		queryParams    map[string]string
		headers        map[string]string
		setupDB        func() *MockDatabase
		expectedStatus int
		validateResp   func(t *testing.T, body []byte)
	}{
		{
			name:        "Basic query - returns single record",
			sqlQuery:    "SELECT * FROM users WHERE id = 1",
			blankParams: false,
			setupDB: func() *MockDatabase {
				return &MockDatabase{
					RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
						db := &MockDatabase{
							QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
								rows := dest.(*[]map[string]interface{})
								*rows = []map[string]interface{}{
									{"id": float64(1), "name": "Test User", "email": "test@example.com"},
								}
								return nil
							},
						}
						return fn(db)
					},
				}
			},
			expectedStatus: 200,
			validateResp: func(t *testing.T, body []byte) {
				var result map[string]interface{}
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if result["name"] != "Test User" {
					t.Errorf("Expected name='Test User', got %v", result["name"])
				}
			},
		},
		{
			name:        "Query with no results",
			sqlQuery:    "SELECT * FROM users WHERE id = 999",
			blankParams: false,
			setupDB: func() *MockDatabase {
				return &MockDatabase{
					RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
						db := &MockDatabase{
							QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
								// Return empty array
								return nil
							},
						}
						return fn(db)
					},
				}
			},
			expectedStatus: 200,
			validateResp: func(t *testing.T, body []byte) {
				var result map[string]interface{}
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if len(result) != 0 {
					t.Errorf("Expected empty result, got %v", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.setupDB()
			handler := NewHandler(db)

			req := createTestRequest("GET", "/test", tt.queryParams, tt.headers, nil)
			w := httptest.NewRecorder()

			handlerFunc := handler.SqlQuery(tt.sqlQuery, tt.blankParams)
			handlerFunc(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.validateResp != nil {
				tt.validateResp(t, w.Body.Bytes())
			}
		})
	}
}

// TestSqlQueryList tests the SqlQueryList handler for list queries
func TestSqlQueryList(t *testing.T) {
	tests := []struct {
		name           string
		sqlQuery       string
		noCount        bool
		blankParams    bool
		allowFilter    bool
		queryParams    map[string]string
		headers        map[string]string
		setupDB        func() *MockDatabase
		expectedStatus int
		validateResp   func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:        "Basic list query",
			sqlQuery:    "SELECT * FROM users",
			noCount:     false,
			blankParams: false,
			allowFilter: false,
			setupDB: func() *MockDatabase {
				return &MockDatabase{
					RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
						callCount := 0
						db := &MockDatabase{
							QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
								callCount++
								if strings.Contains(query, "COUNT") {
									// Count query
									countResult := dest.(*struct{ Count int64 })
									countResult.Count = 2
								} else {
									// Main query
									rows := dest.(*[]map[string]interface{})
									*rows = []map[string]interface{}{
										{"id": float64(1), "name": "User 1"},
										{"id": float64(2), "name": "User 2"},
									}
								}
								return nil
							},
						}
						return fn(db)
					},
				}
			},
			expectedStatus: 200,
			validateResp: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result []map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if len(result) != 2 {
					t.Errorf("Expected 2 results, got %d", len(result))
				}

				// Check Content-Range header
				contentRange := w.Header().Get("Content-Range")
				if !strings.Contains(contentRange, "2") {
					t.Errorf("Expected Content-Range to contain total count, got: %s", contentRange)
				}
			},
		},
		{
			name:        "List query with noCount",
			sqlQuery:    "SELECT * FROM users",
			noCount:     true,
			blankParams: false,
			allowFilter: false,
			setupDB: func() *MockDatabase {
				return &MockDatabase{
					RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
						db := &MockDatabase{
							QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
								if strings.Contains(query, "COUNT") {
									t.Error("Count query should not be executed when noCount is true")
								}
								rows := dest.(*[]map[string]interface{})
								*rows = []map[string]interface{}{
									{"id": float64(1), "name": "User 1"},
								}
								return nil
							},
						}
						return fn(db)
					},
				}
			},
			expectedStatus: 200,
			validateResp: func(t *testing.T, w *httptest.ResponseRecorder) {
				var result []map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}
				if len(result) != 1 {
					t.Errorf("Expected 1 result, got %d", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := tt.setupDB()
			handler := NewHandler(db)

			req := createTestRequest("GET", "/test", tt.queryParams, tt.headers, nil)
			w := httptest.NewRecorder()

			handlerFunc := handler.SqlQueryList(tt.sqlQuery, tt.noCount, tt.blankParams, tt.allowFilter)
			handlerFunc(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			if tt.validateResp != nil {
				tt.validateResp(t, w)
			}
		})
	}
}

// TestMergeQueryParams tests query parameter merging
func TestMergeQueryParams(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name          string
		sqlQuery      string
		queryParams   map[string]string
		allowFilter   bool
		expectedQuery string
		checkVars     func(t *testing.T, vars map[string]interface{})
	}{
		{
			name:        "Replace placeholder with parameter",
			sqlQuery:    "SELECT * FROM users WHERE id = [user_id]",
			queryParams: map[string]string{"p-user_id": "123"},
			allowFilter: false,
			checkVars: func(t *testing.T, vars map[string]interface{}) {
				if vars["p-user_id"] != "123" {
					t.Errorf("Expected p-user_id=123, got %v", vars["p-user_id"])
				}
			},
		},
		{
			name:        "Add filter when allowed",
			sqlQuery:    "SELECT * FROM users",
			queryParams: map[string]string{"status": "active"},
			allowFilter: true,
			checkVars: func(t *testing.T, vars map[string]interface{}) {
				if vars["status"] != "active" {
					t.Errorf("Expected status=active, got %v", vars["status"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestRequest("GET", "/test", tt.queryParams, nil, nil)
			variables := make(map[string]interface{})
			propQry := make(map[string]string)

			result := handler.mergeQueryParams(req, tt.sqlQuery, variables, tt.allowFilter, propQry)

			if result == "" {
				t.Error("Expected non-empty SQL query result")
			}

			if tt.checkVars != nil {
				tt.checkVars(t, variables)
			}
		})
	}
}

// TestMergeHeaderParams tests header parameter merging
func TestMergeHeaderParams(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	tests := []struct {
		name          string
		sqlQuery      string
		headers       map[string]string
		expectedQuery string
		checkVars     func(t *testing.T, vars map[string]interface{})
	}{
		{
			name:     "Field filter header",
			sqlQuery: "SELECT * FROM users",
			headers:  map[string]string{"X-FieldFilter-Status": "1"},
			checkVars: func(t *testing.T, vars map[string]interface{}) {
				if vars["x-fieldfilter-status"] != "1" {
					t.Errorf("Expected x-fieldfilter-status=1, got %v", vars["x-fieldfilter-status"])
				}
			},
		},
		{
			name:     "Search filter header",
			sqlQuery: "SELECT * FROM users",
			headers:  map[string]string{"X-SearchFilter-Name": "john"},
			checkVars: func(t *testing.T, vars map[string]interface{}) {
				if vars["x-searchfilter-name"] != "john" {
					t.Errorf("Expected x-searchfilter-name=john, got %v", vars["x-searchfilter-name"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestRequest("GET", "/test", nil, tt.headers, nil)
			variables := make(map[string]interface{})
			propQry := make(map[string]string)
			complexAPI := false

			result := handler.mergeHeaderParams(req, tt.sqlQuery, variables, propQry, &complexAPI)

			if result == "" {
				t.Error("Expected non-empty SQL query result")
			}

			if tt.checkVars != nil {
				tt.checkVars(t, variables)
			}
		})
	}
}

// TestReplaceMetaVariables tests meta variable replacement
func TestReplaceMetaVariables(t *testing.T) {
	handler := NewHandler(&MockDatabase{})

	userCtx := &security.UserContext{
		UserID:    123,
		UserName:  "testuser",
		SessionID: "456",
	}

	metainfo := map[string]interface{}{
		"ipaddress": "192.168.1.1",
		"url":       "/api/test",
	}

	variables := map[string]interface{}{
		"param1": "value1",
	}

	tests := []struct {
		name          string
		sqlQuery      string
		expectedCheck func(result string) bool
	}{
		{
			name:     "Replace [rid_user]",
			sqlQuery: "SELECT * FROM users WHERE created_by = [rid_user]",
			expectedCheck: func(result string) bool {
				return strings.Contains(result, "123")
			},
		},
		{
			name:     "Replace [user]",
			sqlQuery: "SELECT * FROM audit WHERE username = [user]",
			expectedCheck: func(result string) bool {
				return strings.Contains(result, "'testuser'")
			},
		},
		{
			name:     "Replace [rid_session]",
			sqlQuery: "SELECT * FROM sessions WHERE session_id = [rid_session]",
			expectedCheck: func(result string) bool {
				return strings.Contains(result, "456")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createTestRequest("GET", "/test", nil, nil, nil)
			result := handler.replaceMetaVariables(tt.sqlQuery, req, userCtx, metainfo, variables)

			if !tt.expectedCheck(result) {
				t.Errorf("Meta variable replacement failed. Query: %s", result)
			}
		})
	}
}

// TestGetReplacementForBlankParam tests the blank parameter replacement logic
func TestGetReplacementForBlankParam(t *testing.T) {
	tests := []struct {
		name     string
		sqlQuery string
		param    string
		expected string
	}{
		{
			name:     "Parameter in single quotes",
			sqlQuery: "SELECT * FROM users WHERE name = '[username]'",
			param:    "[username]",
			expected: "",
		},
		{
			name:     "Parameter in dollar quotes",
			sqlQuery: "SELECT * FROM users WHERE data = $[jsondata]$",
			param:    "[jsondata]",
			expected: "",
		},
		{
			name:     "Parameter not in quotes",
			sqlQuery: "SELECT * FROM users WHERE id = [user_id]",
			param:    "[user_id]",
			expected: "NULL",
		},
		{
			name:     "Parameter not in quotes with AND",
			sqlQuery: "SELECT * FROM users WHERE id = [user_id] AND status = 1",
			param:    "[user_id]",
			expected: "NULL",
		},
		{
			name:     "Parameter in mixed quote context - before quote",
			sqlQuery: "SELECT * FROM users WHERE id = [user_id] AND name = 'test'",
			param:    "[user_id]",
			expected: "NULL",
		},
		{
			name:     "Parameter in mixed quote context - in quotes",
			sqlQuery: "SELECT * FROM users WHERE name = '[username]' AND id = 1",
			param:    "[username]",
			expected: "",
		},
		{
			name:     "Parameter with dollar quote tag",
			sqlQuery: "SELECT * FROM users WHERE body = $tag$[content]$tag$",
			param:    "[content]",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getReplacementForBlankParam(tt.sqlQuery, tt.param)
			if result != tt.expected {
				t.Errorf("Expected replacement '%s', got '%s' for query: %s", tt.expected, result, tt.sqlQuery)
			}
		})
	}
}
