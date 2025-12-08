package resolvespec

import (
	"testing"
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
