package restheadspec

import (
	"strings"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

func TestGetCursorFilter_Forward(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{
				{Column: "created_at", Direction: "DESC"},
				{Column: "id", Direction: "ASC"},
			},
		},
	}
	opts.CursorForward = "123"

	tableName := "posts"
	pkName := "id"
	modelColumns := []string{"id", "title", "created_at", "user_id"}

	filter, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err != nil {
		t.Fatalf("GetCursorFilter failed: %v", err)
	}

	if filter == "" {
		t.Fatal("Expected non-empty cursor filter")
	}

	// Verify filter contains EXISTS subquery
	if !strings.Contains(filter, "EXISTS") {
		t.Errorf("Filter should contain EXISTS subquery, got: %s", filter)
	}

	// Verify filter references the cursor ID
	if !strings.Contains(filter, "123") {
		t.Errorf("Filter should reference cursor ID 123, got: %s", filter)
	}

	// Verify filter contains the table name
	if !strings.Contains(filter, tableName) {
		t.Errorf("Filter should reference table name %s, got: %s", tableName, filter)
	}

	// Verify filter contains primary key
	if !strings.Contains(filter, pkName) {
		t.Errorf("Filter should reference primary key %s, got: %s", pkName, filter)
	}

	t.Logf("Generated cursor filter: %s", filter)
}

func TestGetCursorFilter_Backward(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{
				{Column: "created_at", Direction: "DESC"},
				{Column: "id", Direction: "ASC"},
			},
		},
	}
	opts.CursorBackward = "456"

	tableName := "posts"
	pkName := "id"
	modelColumns := []string{"id", "title", "created_at", "user_id"}

	filter, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err != nil {
		t.Fatalf("GetCursorFilter failed: %v", err)
	}

	if filter == "" {
		t.Fatal("Expected non-empty cursor filter")
	}

	// Verify filter contains cursor ID
	if !strings.Contains(filter, "456") {
		t.Errorf("Filter should reference cursor ID 456, got: %s", filter)
	}

	// For backward cursor, sort direction should be reversed
	// This is handled internally by the GetCursorFilter method
	t.Logf("Generated backward cursor filter: %s", filter)
}

func TestGetCursorFilter_NoCursor(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{
				{Column: "created_at", Direction: "DESC"},
			},
		},
	}
	// No cursor set

	tableName := "posts"
	pkName := "id"
	modelColumns := []string{"id", "title", "created_at"}

	_, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err == nil {
		t.Error("Expected error when no cursor is provided")
	}

	if !strings.Contains(err.Error(), "no cursor provided") {
		t.Errorf("Expected 'no cursor provided' error, got: %v", err)
	}
}

func TestGetCursorFilter_NoSort(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{},
		},
	}
	opts.CursorForward = "123"

	tableName := "posts"
	pkName := "id"
	modelColumns := []string{"id", "title"}

	_, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err == nil {
		t.Error("Expected error when no sort columns are defined")
	}

	if !strings.Contains(err.Error(), "no sort columns") {
		t.Errorf("Expected 'no sort columns' error, got: %v", err)
	}
}

func TestGetCursorFilter_MultiColumnSort(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{
				{Column: "priority", Direction: "DESC"},
				{Column: "created_at", Direction: "DESC"},
				{Column: "id", Direction: "ASC"},
			},
		},
	}
	opts.CursorForward = "789"

	tableName := "tasks"
	pkName := "id"
	modelColumns := []string{"id", "title", "priority", "created_at"}

	filter, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err != nil {
		t.Fatalf("GetCursorFilter failed: %v", err)
	}

	// Verify filter contains priority column
	if !strings.Contains(filter, "priority") {
		t.Errorf("Filter should reference priority column, got: %s", filter)
	}

	// Verify filter contains created_at column
	if !strings.Contains(filter, "created_at") {
		t.Errorf("Filter should reference created_at column, got: %s", filter)
	}

	t.Logf("Generated multi-column cursor filter: %s", filter)
}

func TestGetCursorFilter_WithSchemaPrefix(t *testing.T) {
	opts := &ExtendedRequestOptions{
		RequestOptions: common.RequestOptions{
			Sort: []common.SortOption{
				{Column: "name", Direction: "ASC"},
			},
		},
	}
	opts.CursorForward = "100"

	tableName := "public.users"
	pkName := "id"
	modelColumns := []string{"id", "name", "email"}

	filter, err := opts.GetCursorFilter(tableName, pkName, modelColumns, nil)
	if err != nil {
		t.Fatalf("GetCursorFilter failed: %v", err)
	}

	// Should handle schema prefix properly
	if !strings.Contains(filter, "users") {
		t.Errorf("Filter should reference table name users, got: %s", filter)
	}

	t.Logf("Generated cursor filter with schema: %s", filter)
}

func TestGetActiveCursor(t *testing.T) {
	tests := []struct {
		name              string
		cursorForward     string
		cursorBackward    string
		expectedID        string
		expectedDirection CursorDirection
	}{
		{
			name:              "Forward cursor only",
			cursorForward:     "123",
			cursorBackward:    "",
			expectedID:        "123",
			expectedDirection: CursorForward,
		},
		{
			name:              "Backward cursor only",
			cursorForward:     "",
			cursorBackward:    "456",
			expectedID:        "456",
			expectedDirection: CursorBackward,
		},
		{
			name:              "Both cursors - forward takes precedence",
			cursorForward:     "123",
			cursorBackward:    "456",
			expectedID:        "123",
			expectedDirection: CursorForward,
		},
		{
			name:              "No cursors",
			cursorForward:     "",
			cursorBackward:    "",
			expectedID:        "",
			expectedDirection: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &ExtendedRequestOptions{}
			opts.CursorForward = tt.cursorForward
			opts.CursorBackward = tt.cursorBackward

			id, direction := opts.getActiveCursor()

			if id != tt.expectedID {
				t.Errorf("Expected cursor ID %q, got %q", tt.expectedID, id)
			}

			if direction != tt.expectedDirection {
				t.Errorf("Expected direction %d, got %d", tt.expectedDirection, direction)
			}
		})
	}
}

func TestCleanSortField(t *testing.T) {
	opts := &ExtendedRequestOptions{}

	tests := []struct {
		input    string
		expected string
	}{
		{"created_at desc", "created_at"},
		{"name asc", "name"},
		{"priority desc nulls last", "priority"},
		{"id asc nulls first", "id"},
		{"title", "title"},
		{"updated_at DESC", "updated_at"},
		{"  status  asc  ", "status"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := opts.cleanSortField(tt.input)
			if result != tt.expected {
				t.Errorf("cleanSortField(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildPriorityChain(t *testing.T) {
	clauses := []string{
		"cursor_select.priority > posts.priority",
		"cursor_select.created_at > posts.created_at",
		"cursor_select.id < posts.id",
	}

	result := buildPriorityChain(clauses)

	// Should build OR-AND chain for cursor comparison
	if !strings.Contains(result, "OR") {
		t.Error("Priority chain should contain OR operators")
	}

	if !strings.Contains(result, "AND") {
		t.Error("Priority chain should contain AND operators for composite conditions")
	}

	// First clause should appear standalone
	if !strings.Contains(result, clauses[0]) {
		t.Errorf("Priority chain should contain first clause: %s", clauses[0])
	}

	t.Logf("Built priority chain: %s", result)
}
