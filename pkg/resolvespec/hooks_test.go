package resolvespec

import (
	"context"
	"fmt"
	"testing"
)

func TestHookRegistry(t *testing.T) {
	registry := NewHookRegistry()

	// Test registering a hook
	called := false
	hook := func(ctx *HookContext) error {
		called = true
		return nil
	}

	registry.Register(BeforeRead, hook)

	if registry.Count(BeforeRead) != 1 {
		t.Errorf("Expected 1 hook, got %d", registry.Count(BeforeRead))
	}

	// Test executing a hook
	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	err := registry.Execute(BeforeRead, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	if !called {
		t.Error("Hook was not called")
	}
}

func TestHookExecutionOrder(t *testing.T) {
	registry := NewHookRegistry()

	order := []int{}

	hook1 := func(ctx *HookContext) error {
		order = append(order, 1)
		return nil
	}

	hook2 := func(ctx *HookContext) error {
		order = append(order, 2)
		return nil
	}

	hook3 := func(ctx *HookContext) error {
		order = append(order, 3)
		return nil
	}

	registry.Register(BeforeCreate, hook1)
	registry.Register(BeforeCreate, hook2)
	registry.Register(BeforeCreate, hook3)

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	err := registry.Execute(BeforeCreate, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	if len(order) != 3 {
		t.Errorf("Expected 3 hooks to be called, got %d", len(order))
	}

	if order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("Hooks executed in wrong order: %v", order)
	}
}

func TestHookError(t *testing.T) {
	registry := NewHookRegistry()

	executed := []string{}

	hook1 := func(ctx *HookContext) error {
		executed = append(executed, "hook1")
		return nil
	}

	hook2 := func(ctx *HookContext) error {
		executed = append(executed, "hook2")
		return fmt.Errorf("hook2 error")
	}

	hook3 := func(ctx *HookContext) error {
		executed = append(executed, "hook3")
		return nil
	}

	registry.Register(BeforeUpdate, hook1)
	registry.Register(BeforeUpdate, hook2)
	registry.Register(BeforeUpdate, hook3)

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	err := registry.Execute(BeforeUpdate, ctx)
	if err == nil {
		t.Error("Expected error from hook execution")
	}

	if len(executed) != 2 {
		t.Errorf("Expected only 2 hooks to be executed, got %d", len(executed))
	}

	if executed[0] != "hook1" || executed[1] != "hook2" {
		t.Errorf("Unexpected execution order: %v", executed)
	}
}

func TestHookDataModification(t *testing.T) {
	registry := NewHookRegistry()

	modifyHook := func(ctx *HookContext) error {
		if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
			dataMap["modified"] = true
			ctx.Data = dataMap
		}
		return nil
	}

	registry.Register(BeforeCreate, modifyHook)

	data := map[string]interface{}{
		"name": "test",
	}

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
		Data:    data,
	}

	err := registry.Execute(BeforeCreate, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	modifiedData := ctx.Data.(map[string]interface{})
	if !modifiedData["modified"].(bool) {
		t.Error("Data was not modified by hook")
	}
}

func TestRegisterMultiple(t *testing.T) {
	registry := NewHookRegistry()

	called := 0
	hook := func(ctx *HookContext) error {
		called++
		return nil
	}

	registry.RegisterMultiple([]HookType{
		BeforeRead,
		BeforeCreate,
		BeforeUpdate,
	}, hook)

	if registry.Count(BeforeRead) != 1 {
		t.Error("Hook not registered for BeforeRead")
	}
	if registry.Count(BeforeCreate) != 1 {
		t.Error("Hook not registered for BeforeCreate")
	}
	if registry.Count(BeforeUpdate) != 1 {
		t.Error("Hook not registered for BeforeUpdate")
	}

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	registry.Execute(BeforeRead, ctx)
	registry.Execute(BeforeCreate, ctx)
	registry.Execute(BeforeUpdate, ctx)

	if called != 3 {
		t.Errorf("Expected hook to be called 3 times, got %d", called)
	}
}

func TestClearHooks(t *testing.T) {
	registry := NewHookRegistry()

	hook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeRead, hook)
	registry.Register(BeforeCreate, hook)

	if registry.Count(BeforeRead) != 1 {
		t.Error("Hook not registered")
	}

	registry.Clear(BeforeRead)

	if registry.Count(BeforeRead) != 0 {
		t.Error("Hook not cleared")
	}

	if registry.Count(BeforeCreate) != 1 {
		t.Error("Wrong hook was cleared")
	}
}

func TestClearAllHooks(t *testing.T) {
	registry := NewHookRegistry()

	hook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeRead, hook)
	registry.Register(BeforeCreate, hook)
	registry.Register(BeforeUpdate, hook)

	registry.ClearAll()

	if registry.Count(BeforeRead) != 0 || registry.Count(BeforeCreate) != 0 || registry.Count(BeforeUpdate) != 0 {
		t.Error("Not all hooks were cleared")
	}
}

func TestHasHooks(t *testing.T) {
	registry := NewHookRegistry()

	if registry.HasHooks(BeforeRead) {
		t.Error("Should not have hooks initially")
	}

	hook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeRead, hook)

	if !registry.HasHooks(BeforeRead) {
		t.Error("Should have hooks after registration")
	}
}

func TestGetAllHookTypes(t *testing.T) {
	registry := NewHookRegistry()

	hook := func(ctx *HookContext) error {
		return nil
	}

	registry.Register(BeforeRead, hook)
	registry.Register(BeforeCreate, hook)
	registry.Register(AfterUpdate, hook)

	types := registry.GetAllHookTypes()

	if len(types) != 3 {
		t.Errorf("Expected 3 hook types, got %d", len(types))
	}

	// Verify all expected types are present
	expectedTypes := map[HookType]bool{
		BeforeRead:   true,
		BeforeCreate: true,
		AfterUpdate:  true,
	}

	for _, hookType := range types {
		if !expectedTypes[hookType] {
			t.Errorf("Unexpected hook type: %s", hookType)
		}
	}
}

func TestHookContextHandler(t *testing.T) {
	registry := NewHookRegistry()

	var capturedHandler *Handler

	hook := func(ctx *HookContext) error {
		if ctx.Handler == nil {
			return fmt.Errorf("handler is nil in hook context")
		}
		capturedHandler = ctx.Handler
		return nil
	}

	registry.Register(BeforeRead, hook)

	handler := &Handler{
		hooks: registry,
	}

	ctx := &HookContext{
		Context: context.Background(),
		Handler: handler,
		Schema:  "test",
		Entity:  "users",
	}

	err := registry.Execute(BeforeRead, ctx)
	if err != nil {
		t.Errorf("Hook execution failed: %v", err)
	}

	if capturedHandler == nil {
		t.Error("Handler was not captured from hook context")
	}

	if capturedHandler != handler {
		t.Error("Captured handler does not match original handler")
	}
}

func TestHookAbort(t *testing.T) {
	registry := NewHookRegistry()

	abortHook := func(ctx *HookContext) error {
		ctx.Abort = true
		ctx.AbortMessage = "Operation aborted by hook"
		ctx.AbortCode = 403
		return nil
	}

	registry.Register(BeforeCreate, abortHook)

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	err := registry.Execute(BeforeCreate, ctx)
	if err == nil {
		t.Error("Expected error when hook sets Abort=true")
	}

	if err.Error() != "operation aborted by hook: Operation aborted by hook" {
		t.Errorf("Expected abort error message, got: %v", err)
	}
}

func TestHookTypes(t *testing.T) {
	// Test all hook type constants
	hookTypes := []HookType{
		BeforeRead,
		AfterRead,
		BeforeCreate,
		AfterCreate,
		BeforeUpdate,
		AfterUpdate,
		BeforeDelete,
		AfterDelete,
		BeforeScan,
	}

	for _, hookType := range hookTypes {
		if string(hookType) == "" {
			t.Errorf("Hook type should not be empty: %v", hookType)
		}
	}
}

func TestExecuteWithNoHooks(t *testing.T) {
	registry := NewHookRegistry()

	ctx := &HookContext{
		Context: context.Background(),
		Schema:  "test",
		Entity:  "users",
	}

	// Executing with no registered hooks should not cause an error
	err := registry.Execute(BeforeRead, ctx)
	if err != nil {
		t.Errorf("Execute should not fail with no hooks, got: %v", err)
	}
}
