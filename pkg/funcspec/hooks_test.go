package funcspec

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// TestNewHookRegistry tests hook registry creation
func TestNewHookRegistry(t *testing.T) {
	registry := NewHookRegistry()

	if registry == nil {
		t.Fatal("Expected registry to be created, got nil")
	}

	if registry.hooks == nil {
		t.Error("Expected hooks map to be initialized")
	}
}

// TestRegisterHook tests registering a single hook
func TestRegisterHook(t *testing.T) {
	registry := NewHookRegistry()

	hookCalled := false
	testHook := func(ctx *HookContext) error {
		hookCalled = true
		return nil
	}

	registry.Register(BeforeQuery, testHook)

	if !registry.HasHooks(BeforeQuery) {
		t.Error("Expected hook to be registered")
	}

	if registry.Count(BeforeQuery) != 1 {
		t.Errorf("Expected 1 hook, got %d", registry.Count(BeforeQuery))
	}

	// Execute the hook
	ctx := &HookContext{}
	err := registry.Execute(BeforeQuery, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	if !hookCalled {
		t.Error("Expected hook to be called")
	}
}

// TestRegisterMultipleHooks tests registering multiple hooks for same type
func TestRegisterMultipleHooks(t *testing.T) {
	registry := NewHookRegistry()

	callOrder := []int{}

	hook1 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 1)
		return nil
	}

	hook2 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 2)
		return nil
	}

	hook3 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 3)
		return nil
	}

	registry.Register(BeforeQuery, hook1)
	registry.Register(BeforeQuery, hook2)
	registry.Register(BeforeQuery, hook3)

	if registry.Count(BeforeQuery) != 3 {
		t.Errorf("Expected 3 hooks, got %d", registry.Count(BeforeQuery))
	}

	// Execute hooks
	ctx := &HookContext{}
	err := registry.Execute(BeforeQuery, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	// Verify hooks were called in order
	if len(callOrder) != 3 {
		t.Errorf("Expected 3 hooks to be called, got %d", len(callOrder))
	}

	for i, expected := range []int{1, 2, 3} {
		if callOrder[i] != expected {
			t.Errorf("Expected hook %d at position %d, got %d", expected, i, callOrder[i])
		}
	}
}

// TestRegisterMultipleHookTypes tests registering a hook for multiple types
func TestRegisterMultipleHookTypes(t *testing.T) {
	registry := NewHookRegistry()

	callCount := 0
	testHook := func(ctx *HookContext) error {
		callCount++
		return nil
	}

	hookTypes := []HookType{BeforeQuery, AfterQuery, BeforeSQLExec}
	registry.RegisterMultiple(hookTypes, testHook)

	// Verify hook is registered for all types
	for _, hookType := range hookTypes {
		if !registry.HasHooks(hookType) {
			t.Errorf("Expected hook to be registered for %s", hookType)
		}

		if registry.Count(hookType) != 1 {
			t.Errorf("Expected 1 hook for %s, got %d", hookType, registry.Count(hookType))
		}
	}

	// Execute each hook type
	ctx := &HookContext{}
	for _, hookType := range hookTypes {
		if err := registry.Execute(hookType, ctx); err != nil {
			t.Errorf("Hook execution failed for %s: %v", hookType, err)
		}
	}

	if callCount != 3 {
		t.Errorf("Expected hook to be called 3 times, got %d", callCount)
	}
}

// TestHookError tests hook error handling
func TestHookError(t *testing.T) {
	registry := NewHookRegistry()

	expectedError := fmt.Errorf("test error")
	errorHook := func(ctx *HookContext) error {
		return expectedError
	}

	registry.Register(BeforeQuery, errorHook)

	ctx := &HookContext{}
	err := registry.Execute(BeforeQuery, ctx)

	if err == nil {
		t.Error("Expected error from hook, got nil")
	}

	if err.Error() != fmt.Sprintf("hook execution failed: %v", expectedError) {
		t.Errorf("Expected error message to contain hook error, got: %v", err)
	}
}

// TestHookAbort tests hook abort functionality
func TestHookAbort(t *testing.T) {
	registry := NewHookRegistry()

	abortHook := func(ctx *HookContext) error {
		ctx.Abort = true
		ctx.AbortMessage = "Operation aborted by hook"
		ctx.AbortCode = 403
		return nil
	}

	registry.Register(BeforeQuery, abortHook)

	ctx := &HookContext{}
	err := registry.Execute(BeforeQuery, ctx)

	if err == nil {
		t.Error("Expected error when hook aborts, got nil")
	}

	if !ctx.Abort {
		t.Error("Expected Abort to be true")
	}

	if ctx.AbortMessage != "Operation aborted by hook" {
		t.Errorf("Expected abort message, got: %s", ctx.AbortMessage)
	}

	if ctx.AbortCode != 403 {
		t.Errorf("Expected abort code 403, got: %d", ctx.AbortCode)
	}
}

// TestHookChainWithError tests that hook chain stops on first error
func TestHookChainWithError(t *testing.T) {
	registry := NewHookRegistry()

	callOrder := []int{}

	hook1 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 1)
		return nil
	}

	hook2 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 2)
		return fmt.Errorf("error in hook 2")
	}

	hook3 := func(ctx *HookContext) error {
		callOrder = append(callOrder, 3)
		return nil
	}

	registry.Register(BeforeQuery, hook1)
	registry.Register(BeforeQuery, hook2)
	registry.Register(BeforeQuery, hook3)

	ctx := &HookContext{}
	err := registry.Execute(BeforeQuery, ctx)

	if err == nil {
		t.Error("Expected error from hook chain")
	}

	// Only first two hooks should have been called
	if len(callOrder) != 2 {
		t.Errorf("Expected 2 hooks to be called, got %d", len(callOrder))
	}

	if callOrder[0] != 1 || callOrder[1] != 2 {
		t.Errorf("Expected hooks 1 and 2 to be called, got: %v", callOrder)
	}
}

// TestClearHooks tests clearing hooks
func TestClearHooks(t *testing.T) {
	registry := NewHookRegistry()

	testHook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeQuery, testHook)
	registry.Register(AfterQuery, testHook)

	if !registry.HasHooks(BeforeQuery) {
		t.Error("Expected BeforeQuery hook to be registered")
	}

	registry.Clear(BeforeQuery)

	if registry.HasHooks(BeforeQuery) {
		t.Error("Expected BeforeQuery hooks to be cleared")
	}

	if !registry.HasHooks(AfterQuery) {
		t.Error("Expected AfterQuery hook to still be registered")
	}
}

// TestClearAllHooks tests clearing all hooks
func TestClearAllHooks(t *testing.T) {
	registry := NewHookRegistry()

	testHook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeQuery, testHook)
	registry.Register(AfterQuery, testHook)
	registry.Register(BeforeSQLExec, testHook)

	registry.ClearAll()

	if registry.HasHooks(BeforeQuery) || registry.HasHooks(AfterQuery) || registry.HasHooks(BeforeSQLExec) {
		t.Error("Expected all hooks to be cleared")
	}
}

// TestGetAllHookTypes tests getting all registered hook types
func TestGetAllHookTypes(t *testing.T) {
	registry := NewHookRegistry()

	testHook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeQuery, testHook)
	registry.Register(AfterQuery, testHook)

	types := registry.GetAllHookTypes()

	if len(types) != 2 {
		t.Errorf("Expected 2 hook types, got %d", len(types))
	}

	// Verify the types are present
	foundBefore := false
	foundAfter := false
	for _, hookType := range types {
		if hookType == BeforeQuery {
			foundBefore = true
		}
		if hookType == AfterQuery {
			foundAfter = true
		}
	}

	if !foundBefore || !foundAfter {
		t.Error("Expected both BeforeQuery and AfterQuery hook types")
	}
}

// TestHookContextModification tests that hooks can modify the context
func TestHookContextModification(t *testing.T) {
	registry := NewHookRegistry()

	// Hook that modifies SQL query
	modifyHook := func(ctx *HookContext) error {
		ctx.SQLQuery = "SELECT * FROM modified_table"
		ctx.Variables["new_var"] = "new_value"
		return nil
	}

	registry.Register(BeforeQuery, modifyHook)

	ctx := &HookContext{
		SQLQuery:  "SELECT * FROM original_table",
		Variables: make(map[string]interface{}),
	}

	err := registry.Execute(BeforeQuery, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	if ctx.SQLQuery != "SELECT * FROM modified_table" {
		t.Errorf("Expected SQL query to be modified, got: %s", ctx.SQLQuery)
	}

	if ctx.Variables["new_var"] != "new_value" {
		t.Errorf("Expected variable to be added, got: %v", ctx.Variables)
	}
}

// TestExampleHooks tests the example hooks
func TestExampleLoggingHook(t *testing.T) {
	ctx := &HookContext{
		Context:  context.Background(),
		SQLQuery: "SELECT * FROM test",
		UserContext: &security.UserContext{
			UserName: "testuser",
		},
	}

	err := ExampleLoggingHook(ctx)
	if err != nil {
		t.Errorf("ExampleLoggingHook failed: %v", err)
	}
}

func TestExampleSecurityHook(t *testing.T) {
	tests := []struct {
		name        string
		sqlQuery    string
		userID      int
		shouldAbort bool
	}{
		{
			name:        "Admin accessing sensitive table",
			sqlQuery:    "SELECT * FROM sensitive_table",
			userID:      1,
			shouldAbort: false,
		},
		{
			name:        "Non-admin accessing sensitive table",
			sqlQuery:    "SELECT * FROM sensitive_table",
			userID:      2,
			shouldAbort: true,
		},
		{
			name:        "Non-admin accessing normal table",
			sqlQuery:    "SELECT * FROM users",
			userID:      2,
			shouldAbort: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &HookContext{
				Context:  context.Background(),
				SQLQuery: tt.sqlQuery,
				UserContext: &security.UserContext{
					UserID: tt.userID,
				},
			}

			_ = ExampleSecurityHook(ctx)

			if tt.shouldAbort {
				if !ctx.Abort {
					t.Error("Expected security hook to abort operation")
				}
				if ctx.AbortCode != 403 {
					t.Errorf("Expected abort code 403, got %d", ctx.AbortCode)
				}
			} else {
				if ctx.Abort {
					t.Error("Expected security hook not to abort operation")
				}
			}
		})
	}
}

func TestExampleResultFilterHook(t *testing.T) {
	tests := []struct {
		name     string
		userID   int
		result   interface{}
		validate func(t *testing.T, result interface{})
	}{
		{
			name:   "Admin user - no filtering",
			userID: 1,
			result: map[string]interface{}{
				"id":       1,
				"name":     "Test",
				"password": "secret",
			},
			validate: func(t *testing.T, result interface{}) {
				m := result.(map[string]interface{})
				if _, exists := m["password"]; !exists {
					t.Error("Expected password field to remain for admin")
				}
			},
		},
		{
			name:   "Regular user - sensitive fields removed",
			userID: 2,
			result: map[string]interface{}{
				"id":       1,
				"name":     "Test",
				"password": "secret",
				"ssn":      "123-45-6789",
			},
			validate: func(t *testing.T, result interface{}) {
				m := result.(map[string]interface{})
				if _, exists := m["password"]; exists {
					t.Error("Expected password field to be removed")
				}
				if _, exists := m["ssn"]; exists {
					t.Error("Expected ssn field to be removed")
				}
				if _, exists := m["name"]; !exists {
					t.Error("Expected name field to remain")
				}
			},
		},
		{
			name:   "Regular user - list results filtered",
			userID: 2,
			result: []map[string]interface{}{
				{"id": 1, "name": "User 1", "password": "secret1"},
				{"id": 2, "name": "User 2", "password": "secret2"},
			},
			validate: func(t *testing.T, result interface{}) {
				list := result.([]map[string]interface{})
				for _, m := range list {
					if _, exists := m["password"]; exists {
						t.Error("Expected password field to be removed from list")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &HookContext{
				Context: context.Background(),
				Result:  tt.result,
				UserContext: &security.UserContext{
					UserID: tt.userID,
				},
			}

			err := ExampleResultFilterHook(ctx)
			if err != nil {
				t.Errorf("Hook failed: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, ctx.Result)
			}
		})
	}
}

func TestExampleAuditHook(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ctx := &HookContext{
		Context: context.Background(),
		Request: req,
		UserContext: &security.UserContext{
			UserID:   123,
			UserName: "testuser",
		},
	}

	err := ExampleAuditHook(ctx)
	if err != nil {
		t.Errorf("ExampleAuditHook failed: %v", err)
	}
}

func TestExampleErrorHandlingHook(t *testing.T) {
	ctx := &HookContext{
		Context:  context.Background(),
		SQLQuery: "SELECT * FROM test",
		Error:    fmt.Errorf("test error"),
		UserContext: &security.UserContext{
			UserName: "testuser",
		},
	}

	err := ExampleErrorHandlingHook(ctx)
	if err != nil {
		t.Errorf("ExampleErrorHandlingHook failed: %v", err)
	}
}

// TestHookIntegrationWithHandler tests hooks integrated with the handler
func TestHookIntegrationWithHandler(t *testing.T) {
	db := &MockDatabase{
		RunInTransactionFunc: func(ctx context.Context, fn func(common.Database) error) error {
			queryDB := &MockDatabase{
				QueryFunc: func(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
					rows := dest.(*[]map[string]interface{})
					*rows = []map[string]interface{}{
						{"id": float64(1), "name": "Test User"},
					}
					return nil
				},
			}
			return fn(queryDB)
		},
	}

	handler := NewHandler(db)

	// Register a hook that modifies the SQL query
	hookCalled := false
	handler.Hooks().Register(BeforeSQLExec, func(ctx *HookContext) error {
		hookCalled = true
		// Verify we can access context data
		if ctx.SQLQuery == "" {
			t.Error("Expected SQL query to be set")
		}
		if ctx.UserContext == nil {
			t.Error("Expected user context to be set")
		}
		return nil
	})

	// Execute a query
	req := createTestRequest("GET", "/test", nil, nil, nil)
	w := httptest.NewRecorder()

	handlerFunc := handler.SqlQuery("SELECT * FROM users WHERE id = 1", SqlQueryOptions{})
	handlerFunc(w, req)

	if !hookCalled {
		t.Error("Expected hook to be called during query execution")
	}

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}
