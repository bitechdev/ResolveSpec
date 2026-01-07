package restheadspec

import (
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestParseModelName(t *testing.T) {
	tests := []struct {
		name           string
		fullName       string
		expectedSchema string
		expectedEntity string
	}{
		{
			name:           "Model with schema",
			fullName:       "public.users",
			expectedSchema: "public",
			expectedEntity: "users",
		},
		{
			name:           "Model without schema",
			fullName:       "users",
			expectedSchema: "",
			expectedEntity: "users",
		},
		{
			name:           "Model with custom schema",
			fullName:       "myschema.products",
			expectedSchema: "myschema",
			expectedEntity: "products",
		},
		{
			name:           "Empty string",
			fullName:       "",
			expectedSchema: "",
			expectedEntity: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, entity := parseModelName(tt.fullName)
			if schema != tt.expectedSchema {
				t.Errorf("Expected schema '%s', got '%s'", tt.expectedSchema, schema)
			}
			if entity != tt.expectedEntity {
				t.Errorf("Expected entity '%s', got '%s'", tt.expectedEntity, entity)
			}
		})
	}
}

func TestBuildRoutePath(t *testing.T) {
	tests := []struct {
		name         string
		schema       string
		entity       string
		expectedPath string
	}{
		{
			name:         "With schema",
			schema:       "public",
			entity:       "users",
			expectedPath: "/public/users",
		},
		{
			name:         "Without schema",
			schema:       "",
			entity:       "users",
			expectedPath: "/users",
		},
		{
			name:         "Custom schema",
			schema:       "admin",
			entity:       "logs",
			expectedPath: "/admin/logs",
		},
		{
			name:         "Empty entity with schema",
			schema:       "public",
			entity:       "",
			expectedPath: "/public/",
		},
		{
			name:         "Both empty",
			schema:       "",
			entity:       "",
			expectedPath: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := buildRoutePath(tt.schema, tt.entity)
			if path != tt.expectedPath {
				t.Errorf("Expected path '%s', got '%s'", tt.expectedPath, path)
			}
		})
	}
}

func TestNewStandardMuxRouter(t *testing.T) {
	router := NewStandardMuxRouter()
	if router == nil {
		t.Error("Expected router to be created, got nil")
	}
}

func TestNewStandardBunRouter(t *testing.T) {
	router := NewStandardBunRouter()
	if router == nil {
		t.Error("Expected router to be created, got nil")
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
			name:     "Extract existing key",
			tag:      "json:name;validate:required",
			key:      "json",
			expected: "name",
		},
		{
			name:     "Extract key with spaces",
			tag:      "json:name ; validate:required",
			key:      "validate",
			expected: "required",
		},
		{
			name:     "Extract key at end",
			tag:      "json:name;validate:required;db:column_name",
			key:      "db",
			expected: "column_name",
		},
		{
			name:     "Extract key at beginning",
			tag:      "primary:true;json:id;db:user_id",
			key:      "primary",
			expected: "true",
		},
		{
			name:     "Key not found",
			tag:      "json:name;validate:required",
			key:      "db",
			expected: "",
		},
		{
			name:     "Empty tag",
			tag:      "",
			key:      "json",
			expected: "",
		},
		{
			name:     "Single key-value pair",
			tag:      "json:name",
			key:      "json",
			expected: "name",
		},
		{
			name:     "Key with empty value",
			tag:      "json:;validate:required",
			key:      "json",
			expected: "",
		},
		{
			name:     "Key with complex value",
			tag:      "json:user_name,omitempty;validate:required,min=3",
			key:      "json",
			expected: "user_name,omitempty",
		},
		{
			name:     "Multiple semicolons",
			tag:      "json:name;;validate:required",
			key:      "validate",
			expected: "required",
		},
		{
			name:     "BUN Tag",
			tag:      "rel:has-many,join:rid_hub=rid_hub_child",
			key:      "join",
			expected: "rid_hub=rid_hub_child",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := common.ExtractTagValue(tt.tag, tt.key)
			if result != tt.expected {
				t.Errorf("ExtractTagValue(%q, %q) = %q; want %q", tt.tag, tt.key, result, tt.expected)
			}
		})
	}
}
