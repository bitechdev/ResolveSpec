// +build !integration

package restheadspec

import (
	"fmt"
	"reflect"
	"testing"
)

// Test models for nested CRUD operations
type TestUser struct {
	ID    int64      `json:"id" bun:"id,pk,autoincrement"`
	Name  string     `json:"name"`
	Posts []TestPost `json:"posts" gorm:"foreignKey:UserID"`
}

type TestPost struct {
	ID       int64         `json:"id" bun:"id,pk,autoincrement"`
	UserID   int64         `json:"user_id"`
	Title    string        `json:"title"`
	Comments []TestComment `json:"comments" gorm:"foreignKey:PostID"`
}

type TestComment struct {
	ID      int64  `json:"id" bun:"id,pk,autoincrement"`
	PostID  int64  `json:"post_id"`
	Content string `json:"content"`
}

func (TestUser) TableName() string    { return "users" }
func (TestPost) TableName() string    { return "posts" }
func (TestComment) TableName() string { return "comments" }

// Test extractNestedRelations function
func TestExtractNestedRelations(t *testing.T) {
	// Create handler
	registry := &mockRegistry{
		models: map[string]interface{}{
			"users":    TestUser{},
			"posts":    TestPost{},
			"comments": TestComment{},
		},
	}
	handler := NewHandler(nil, registry)

	tests := []struct {
		name               string
		data               map[string]interface{}
		model              interface{}
		expectedCleanCount int
		expectedRelCount   int
	}{
		{
			name: "User with posts",
			data: map[string]interface{}{
				"name": "John Doe",
				"posts": []map[string]interface{}{
					{"title": "Post 1"},
				},
			},
			model:              TestUser{},
			expectedCleanCount: 1, // name
			expectedRelCount:   1, // posts
		},
		{
			name: "Post with comments",
			data: map[string]interface{}{
				"title": "Test Post",
				"comments": []map[string]interface{}{
					{"content": "Comment 1"},
					{"content": "Comment 2"},
				},
			},
			model:              TestPost{},
			expectedCleanCount: 1, // title
			expectedRelCount:   1, // comments
		},
		{
			name: "User with nested posts and comments",
			data: map[string]interface{}{
				"name": "Jane Doe",
				"posts": []map[string]interface{}{
					{
						"title": "Post 1",
						"comments": []map[string]interface{}{
							{"content": "Comment 1"},
						},
					},
				},
			},
			model:              TestUser{},
			expectedCleanCount: 1, // name
			expectedRelCount:   1, // posts (which contains nested comments)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanedData, relations, err := handler.extractNestedRelations(tt.data, tt.model)
			if err != nil {
				t.Errorf("extractNestedRelations() error = %v", err)
				return
			}

			if len(cleanedData) != tt.expectedCleanCount {
				t.Errorf("Expected %d cleaned fields, got %d: %+v", tt.expectedCleanCount, len(cleanedData), cleanedData)
			}

			if len(relations) != tt.expectedRelCount {
				t.Errorf("Expected %d relation fields, got %d: %+v", tt.expectedRelCount, len(relations), relations)
			}

			t.Logf("Cleaned data: %+v", cleanedData)
			t.Logf("Relations: %+v", relations)
		})
	}
}

// Test shouldUseNestedProcessor function
func TestShouldUseNestedProcessor(t *testing.T) {
	registry := &mockRegistry{
		models: map[string]interface{}{
			"users": TestUser{},
			"posts": TestPost{},
		},
	}
	handler := NewHandler(nil, registry)

	tests := []struct {
		name     string
		data     map[string]interface{}
		model    interface{}
		expected bool
	}{
		{
			name: "Data with simple nested posts (no further nesting)",
			data: map[string]interface{}{
				"name": "John",
				"posts": []map[string]interface{}{
					{"title": "Post 1"},
				},
			},
			model:    TestUser{},
			expected: false, // Simple one-level nesting doesn't require nested processor
		},
		{
			name: "Data with deeply nested relations",
			data: map[string]interface{}{
				"name": "John",
				"posts": []map[string]interface{}{
					{
						"title": "Post 1",
						"comments": []map[string]interface{}{
							{"content": "Comment 1"},
						},
					},
				},
			},
			model:    TestUser{},
			expected: true, // Multi-level nesting requires nested processor
		},
		{
			name: "Data without nested relations",
			data: map[string]interface{}{
				"name": "John",
			},
			model:    TestUser{},
			expected: false,
		},
		{
			name: "Data with _request field",
			data: map[string]interface{}{
				"_request": "insert",
				"name":     "John",
			},
			model:    TestUser{},
			expected: true,
		},
		{
			name: "Nested data with _request field",
			data: map[string]interface{}{
				"name": "John",
				"posts": []map[string]interface{}{
					{
						"_request": "insert",
						"title":    "Post 1",
					},
				},
			},
			model:    TestUser{},
			expected: true, // _request at nested level requires nested processor
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.shouldUseNestedProcessor(tt.data, tt.model)
			if result != tt.expected {
				t.Errorf("shouldUseNestedProcessor() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// Test normalizeToSlice function
func TestNormalizeToSlice(t *testing.T) {
	registry := &mockRegistry{}
	handler := NewHandler(nil, registry)

	tests := []struct {
		name     string
		input    interface{}
		expected int // expected slice length
	}{
		{
			name:     "Single object",
			input:    map[string]interface{}{"name": "John"},
			expected: 1,
		},
		{
			name: "Slice of objects",
			input: []map[string]interface{}{
				{"name": "John"},
				{"name": "Jane"},
			},
			expected: 2,
		},
		{
			name: "Array of interfaces",
			input: []interface{}{
				map[string]interface{}{"name": "John"},
				map[string]interface{}{"name": "Jane"},
				map[string]interface{}{"name": "Bob"},
			},
			expected: 3,
		},
		{
			name:     "Nil input",
			input:    nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.normalizeToSlice(tt.input)
			if len(result) != tt.expected {
				t.Errorf("normalizeToSlice() returned slice of length %d, expected %d", len(result), tt.expected)
			}
		})
	}
}

// Test GetRelationshipInfo function
func TestGetRelationshipInfo(t *testing.T) {
	registry := &mockRegistry{}
	handler := NewHandler(nil, registry)

	tests := []struct {
		name         string
		modelType    reflect.Type
		relationName string
		expectNil    bool
	}{
		{
			name:         "User posts relation",
			modelType:    reflect.TypeOf(TestUser{}),
			relationName: "posts",
			expectNil:    false,
		},
		{
			name:         "Post comments relation",
			modelType:    reflect.TypeOf(TestPost{}),
			relationName: "comments",
			expectNil:    false,
		},
		{
			name:         "Non-existent relation",
			modelType:    reflect.TypeOf(TestUser{}),
			relationName: "nonexistent",
			expectNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.GetRelationshipInfo(tt.modelType, tt.relationName)
			if tt.expectNil && result != nil {
				t.Errorf("Expected nil, got %+v", result)
			}
			if !tt.expectNil && result == nil {
				t.Errorf("Expected non-nil relationship info")
			}
			if result != nil {
				t.Logf("Relationship info: FieldName=%s, JSONName=%s, RelationType=%s, ForeignKey=%s",
					result.FieldName, result.JSONName, result.RelationType, result.ForeignKey)
			}
		})
	}
}

// Mock registry for testing
type mockRegistry struct {
	models map[string]interface{}
}

func (m *mockRegistry) Register(name string, model interface{}) {
	m.RegisterModel(name, model)
}

func (m *mockRegistry) RegisterModel(name string, model interface{}) error {
	if m.models == nil {
		m.models = make(map[string]interface{})
	}
	m.models[name] = model
	return nil
}

func (m *mockRegistry) GetModelByEntity(schema, entity string) (interface{}, error) {
	if model, ok := m.models[entity]; ok {
		return model, nil
	}
	return nil, fmt.Errorf("model not found: %s", entity)
}

func (m *mockRegistry) GetModelByName(name string) (interface{}, error) {
	if model, ok := m.models[name]; ok {
		return model, nil
	}
	return nil, fmt.Errorf("model not found: %s", name)
}

func (m *mockRegistry) GetModel(name string) (interface{}, error) {
	return m.GetModelByName(name)
}

func (m *mockRegistry) HasModel(schema, entity string) bool {
	_, ok := m.models[entity]
	return ok
}

func (m *mockRegistry) ListModels() []string {
	models := make([]string, 0, len(m.models))
	for name := range m.models {
		models = append(models, name)
	}
	return models
}

func (m *mockRegistry) GetAllModels() map[string]interface{} {
	return m.models
}

// TestMultiLevelRelationExtraction tests extracting deeply nested relations
func TestMultiLevelRelationExtraction(t *testing.T) {
	registry := &mockRegistry{
		models: map[string]interface{}{
			"users":    TestUser{},
			"posts":    TestPost{},
			"comments": TestComment{},
		},
	}
	handler := NewHandler(nil, registry)

	// Test data with 3 levels: User -> Posts -> Comments
	testData := map[string]interface{}{
		"name": "John Doe",
		"posts": []map[string]interface{}{
			{
				"title": "First Post",
				"comments": []map[string]interface{}{
					{"content": "Great post!"},
					{"content": "Thanks for sharing!"},
				},
			},
			{
				"title": "Second Post",
				"comments": []map[string]interface{}{
					{"content": "Interesting read"},
				},
			},
		},
	}

	// Extract relations from user
	cleanedData, relations, err := handler.extractNestedRelations(testData, TestUser{})
	if err != nil {
		t.Fatalf("Failed to extract relations: %v", err)
	}

	// Verify user data is cleaned
	if len(cleanedData) != 1 || cleanedData["name"] != "John Doe" {
		t.Errorf("Expected cleaned data to contain only name, got: %+v", cleanedData)
	}

	// Verify posts relation was extracted
	if len(relations) != 1 {
		t.Errorf("Expected 1 relation (posts), got %d", len(relations))
	}

	posts, ok := relations["posts"]
	if !ok {
		t.Fatal("Expected posts relation to be extracted")
	}

	// Verify posts is a slice with 2 items
	postsSlice, ok := posts.([]map[string]interface{})
	if !ok {
		t.Fatalf("Expected posts to be []map[string]interface{}, got %T", posts)
	}

	if len(postsSlice) != 2 {
		t.Errorf("Expected 2 posts, got %d", len(postsSlice))
	}

	// Verify first post has comments
	if _, hasComments := postsSlice[0]["comments"]; !hasComments {
		t.Error("Expected first post to have comments")
	}

	t.Logf("Successfully extracted multi-level nested relations")
	t.Logf("Cleaned data: %+v", cleanedData)
	t.Logf("Relations: %d posts with nested comments", len(postsSlice))
}
