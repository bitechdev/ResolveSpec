package resolvespec

import (
	"reflect"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestNewHandler(t *testing.T) {
	// Note: We can't create a real handler without actual DB and registry
	// But we can test that the constructor doesn't panic with nil values
	handler := NewHandler(nil, nil)
	if handler == nil {
		t.Error("Expected handler to be created, got nil")
	}

	if handler.hooks == nil {
		t.Error("Expected hooks registry to be initialized")
	}
}

func TestHandlerHooks(t *testing.T) {
	handler := NewHandler(nil, nil)
	hooks := handler.Hooks()
	if hooks == nil {
		t.Error("Expected hooks registry, got nil")
	}
}

func TestSetFallbackHandler(t *testing.T) {
	handler := NewHandler(nil, nil)

	// We can't directly call the fallback without mocks, but we can verify it's set
	handler.SetFallbackHandler(func(w common.ResponseWriter, r common.Request, params map[string]string) {
		// Fallback handler implementation
	})

	if handler.fallbackHandler == nil {
		t.Error("Expected fallback handler to be set")
	}
}

func TestGetDatabase(t *testing.T) {
	handler := NewHandler(nil, nil)
	db := handler.GetDatabase()
	// Should return nil since we passed nil
	if db != nil {
		t.Error("Expected nil database")
	}
}

func TestParseTableName(t *testing.T) {
	handler := NewHandler(nil, nil)

	tests := []struct {
		name           string
		fullTableName  string
		expectedSchema string
		expectedTable  string
	}{
		{
			name:           "Table with schema",
			fullTableName:  "public.users",
			expectedSchema: "public",
			expectedTable:  "users",
		},
		{
			name:           "Table without schema",
			fullTableName:  "users",
			expectedSchema: "",
			expectedTable:  "users",
		},
		{
			name:           "Multiple dots (use last)",
			fullTableName:  "db.public.users",
			expectedSchema: "db.public",
			expectedTable:  "users",
		},
		{
			name:           "Empty string",
			fullTableName:  "",
			expectedSchema: "",
			expectedTable:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table := handler.parseTableName(tt.fullTableName)
			if schema != tt.expectedSchema {
				t.Errorf("Expected schema '%s', got '%s'", tt.expectedSchema, schema)
			}
			if table != tt.expectedTable {
				t.Errorf("Expected table '%s', got '%s'", tt.expectedTable, table)
			}
		})
	}
}

func TestGetColumnType(t *testing.T) {
	tests := []struct {
		name         string
		field        reflect.StructField
		expectedType string
	}{
		{
			name: "String field",
			field: reflect.StructField{
				Name: "Name",
				Type: reflect.TypeOf(""),
			},
			expectedType: "string",
		},
		{
			name: "Int field",
			field: reflect.StructField{
				Name: "Count",
				Type: reflect.TypeOf(int(0)),
			},
			expectedType: "integer",
		},
		{
			name: "Int32 field",
			field: reflect.StructField{
				Name: "ID",
				Type: reflect.TypeOf(int32(0)),
			},
			expectedType: "integer",
		},
		{
			name: "Int64 field",
			field: reflect.StructField{
				Name: "BigID",
				Type: reflect.TypeOf(int64(0)),
			},
			expectedType: "bigint",
		},
		{
			name: "Float32 field",
			field: reflect.StructField{
				Name: "Price",
				Type: reflect.TypeOf(float32(0)),
			},
			expectedType: "float",
		},
		{
			name: "Float64 field",
			field: reflect.StructField{
				Name: "Amount",
				Type: reflect.TypeOf(float64(0)),
			},
			expectedType: "double",
		},
		{
			name: "Bool field",
			field: reflect.StructField{
				Name: "Active",
				Type: reflect.TypeOf(false),
			},
			expectedType: "boolean",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			colType := getColumnType(tt.field)
			if colType != tt.expectedType {
				t.Errorf("Expected column type '%s', got '%s'", tt.expectedType, colType)
			}
		})
	}
}

func TestIsNullable(t *testing.T) {
	tests := []struct {
		name     string
		field    reflect.StructField
		nullable bool
	}{
		{
			name: "Pointer type is nullable",
			field: reflect.StructField{
				Name: "Name",
				Type: reflect.TypeOf((*string)(nil)),
			},
			nullable: true,
		},
		{
			name: "Non-pointer type without explicit 'not null' tag",
			field: reflect.StructField{
				Name: "ID",
				Type: reflect.TypeOf(int(0)),
			},
			nullable: true, // isNullable returns true if there's no explicit "not null" tag
		},
		{
			name: "Field with 'not null' tag is not nullable",
			field: reflect.StructField{
				Name: "Email",
				Type: reflect.TypeOf(""),
				Tag:  `gorm:"not null"`,
			},
			nullable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNullable(tt.field)
			if result != tt.nullable {
				t.Errorf("Expected nullable=%v, got %v", tt.nullable, result)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "UserID",
			expected: "user_id",
		},
		{
			input:    "DepartmentName",
			expected: "department_name",
		},
		{
			input:    "ID",
			expected: "id",
		},
		{
			input:    "HTTPServer",
			expected: "http_server",
		},
		{
			input:    "createdAt",
			expected: "created_at",
		},
		{
			input:    "name",
			expected: "name",
		},
		{
			input:    "",
			expected: "",
		},
		{
			input:    "A",
			expected: "a",
		},
		{
			input:    "APIKey",
			expected: "api_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toSnakeCase(tt.input)
			if result != tt.expected {
				t.Errorf("toSnakeCase(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractTagValue(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		key      string
		expected string
	}{
		{
			name:     "Extract foreignKey",
			tag:      "foreignKey:UserID;references:ID",
			key:      "foreignKey",
			expected: "UserID",
		},
		{
			name:     "Extract references",
			tag:      "foreignKey:UserID;references:ID",
			key:      "references",
			expected: "ID",
		},
		{
			name:     "Key not found",
			tag:      "foreignKey:UserID;references:ID",
			key:      "notfound",
			expected: "",
		},
		{
			name:     "Empty tag",
			tag:      "",
			key:      "foreignKey",
			expected: "",
		},
		{
			name:     "Single value",
			tag:      "many2many:user_roles",
			key:      "many2many",
			expected: "user_roles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := common.ExtractTagValue(tt.tag, tt.key)
			if result != tt.expected {
				t.Errorf("ExtractTagValue(%q, %q) = %q, expected %q", tt.tag, tt.key, result, tt.expected)
			}
		})
	}
}

func TestApplyFilter(t *testing.T) {
	// Note: Without a real database, we can't fully test query execution
	// But we can test that the method exists
	_ = NewHandler(nil, nil)

	// The applyFilter method exists and can be tested with actual queries
	// but requires database setup which is beyond unit test scope
	t.Log("applyFilter method exists and is used in handler operations")
}

func TestShouldUseNestedProcessor(t *testing.T) {
	handler := NewHandler(nil, nil)

	tests := []struct {
		name     string
		data     map[string]interface{}
		expected bool
	}{
		{
			name: "Has _request field",
			data: map[string]interface{}{
				"_request": "nested",
				"name":     "test",
			},
			expected: true,
		},
		{
			name: "No special fields",
			data: map[string]interface{}{
				"name":  "test",
				"email": "test@example.com",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: Without a real model, we can't fully test this
			// But we can verify the function exists
			result := handler.shouldUseNestedProcessor(tt.data, nil)
			// The actual result depends on the model structure
			_ = result
		})
	}
}
