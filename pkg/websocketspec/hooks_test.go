package websocketspec

import (
	"context"
	"errors"
	"testing"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHookType_Constants(t *testing.T) {
	assert.Equal(t, HookType("before_read"), BeforeRead)
	assert.Equal(t, HookType("after_read"), AfterRead)
	assert.Equal(t, HookType("before_create"), BeforeCreate)
	assert.Equal(t, HookType("after_create"), AfterCreate)
	assert.Equal(t, HookType("before_update"), BeforeUpdate)
	assert.Equal(t, HookType("after_update"), AfterUpdate)
	assert.Equal(t, HookType("before_delete"), BeforeDelete)
	assert.Equal(t, HookType("after_delete"), AfterDelete)
	assert.Equal(t, HookType("before_subscribe"), BeforeSubscribe)
	assert.Equal(t, HookType("after_subscribe"), AfterSubscribe)
	assert.Equal(t, HookType("before_unsubscribe"), BeforeUnsubscribe)
	assert.Equal(t, HookType("after_unsubscribe"), AfterUnsubscribe)
	assert.Equal(t, HookType("before_connect"), BeforeConnect)
	assert.Equal(t, HookType("after_connect"), AfterConnect)
	assert.Equal(t, HookType("before_disconnect"), BeforeDisconnect)
	assert.Equal(t, HookType("after_disconnect"), AfterDisconnect)
}

func TestNewHookRegistry(t *testing.T) {
	hr := NewHookRegistry()
	assert.NotNil(t, hr)
	assert.NotNil(t, hr.hooks)
	assert.Empty(t, hr.hooks)
}

func TestHookRegistry_Register(t *testing.T) {
	hr := NewHookRegistry()

	hookCalled := false
	hook := func(ctx *HookContext) error {
		hookCalled = true
		return nil
	}

	hr.Register(BeforeRead, hook)

	// Verify hook was registered
	assert.True(t, hr.HasHooks(BeforeRead))

	// Execute hook
	ctx := &HookContext{Context: context.Background()}
	err := hr.Execute(BeforeRead, ctx)
	require.NoError(t, err)
	assert.True(t, hookCalled)
}

func TestHookRegistry_Register_MultipleHooks(t *testing.T) {
	hr := NewHookRegistry()

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

	hr.Register(BeforeRead, hook1)
	hr.Register(BeforeRead, hook2)
	hr.Register(BeforeRead, hook3)

	// Execute hooks
	ctx := &HookContext{Context: context.Background()}
	err := hr.Execute(BeforeRead, ctx)
	require.NoError(t, err)

	// Verify hooks were called in order
	assert.Equal(t, []int{1, 2, 3}, callOrder)
}

func TestHookRegistry_RegisterBefore(t *testing.T) {
	hr := NewHookRegistry()

	tests := []struct {
		operation OperationType
		hookType  HookType
	}{
		{OperationRead, BeforeRead},
		{OperationCreate, BeforeCreate},
		{OperationUpdate, BeforeUpdate},
		{OperationDelete, BeforeDelete},
		{OperationSubscribe, BeforeSubscribe},
		{OperationUnsubscribe, BeforeUnsubscribe},
	}

	for _, tt := range tests {
		t.Run(string(tt.operation), func(t *testing.T) {
			hookCalled := false
			hook := func(ctx *HookContext) error {
				hookCalled = true
				return nil
			}

			hr.RegisterBefore(tt.operation, hook)
			assert.True(t, hr.HasHooks(tt.hookType))

			ctx := &HookContext{Context: context.Background()}
			err := hr.Execute(tt.hookType, ctx)
			require.NoError(t, err)
			assert.True(t, hookCalled)

			// Clean up for next test
			hr.Clear(tt.hookType)
		})
	}
}

func TestHookRegistry_RegisterAfter(t *testing.T) {
	hr := NewHookRegistry()

	tests := []struct {
		operation OperationType
		hookType  HookType
	}{
		{OperationRead, AfterRead},
		{OperationCreate, AfterCreate},
		{OperationUpdate, AfterUpdate},
		{OperationDelete, AfterDelete},
		{OperationSubscribe, AfterSubscribe},
		{OperationUnsubscribe, AfterUnsubscribe},
	}

	for _, tt := range tests {
		t.Run(string(tt.operation), func(t *testing.T) {
			hookCalled := false
			hook := func(ctx *HookContext) error {
				hookCalled = true
				return nil
			}

			hr.RegisterAfter(tt.operation, hook)
			assert.True(t, hr.HasHooks(tt.hookType))

			ctx := &HookContext{Context: context.Background()}
			err := hr.Execute(tt.hookType, ctx)
			require.NoError(t, err)
			assert.True(t, hookCalled)

			// Clean up for next test
			hr.Clear(tt.hookType)
		})
	}
}

func TestHookRegistry_Execute_NoHooks(t *testing.T) {
	hr := NewHookRegistry()

	ctx := &HookContext{Context: context.Background()}
	err := hr.Execute(BeforeRead, ctx)

	// Should not error when no hooks registered
	assert.NoError(t, err)
}

func TestHookRegistry_Execute_HookReturnsError(t *testing.T) {
	hr := NewHookRegistry()

	expectedErr := errors.New("hook error")
	hook := func(ctx *HookContext) error {
		return expectedErr
	}

	hr.Register(BeforeRead, hook)

	ctx := &HookContext{Context: context.Background()}
	err := hr.Execute(BeforeRead, ctx)

	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestHookRegistry_Execute_FirstHookErrors(t *testing.T) {
	hr := NewHookRegistry()

	hook1Called := false
	hook2Called := false

	hook1 := func(ctx *HookContext) error {
		hook1Called = true
		return errors.New("hook1 error")
	}
	hook2 := func(ctx *HookContext) error {
		hook2Called = true
		return nil
	}

	hr.Register(BeforeRead, hook1)
	hr.Register(BeforeRead, hook2)

	ctx := &HookContext{Context: context.Background()}
	err := hr.Execute(BeforeRead, ctx)

	assert.Error(t, err)
	assert.True(t, hook1Called)
	assert.False(t, hook2Called) // Should not be called after first error
}

func TestHookRegistry_HasHooks(t *testing.T) {
	hr := NewHookRegistry()

	assert.False(t, hr.HasHooks(BeforeRead))

	hr.Register(BeforeRead, func(ctx *HookContext) error { return nil })

	assert.True(t, hr.HasHooks(BeforeRead))
	assert.False(t, hr.HasHooks(AfterRead))
}

func TestHookRegistry_Clear(t *testing.T) {
	hr := NewHookRegistry()

	hr.Register(BeforeRead, func(ctx *HookContext) error { return nil })
	hr.Register(BeforeRead, func(ctx *HookContext) error { return nil })
	assert.True(t, hr.HasHooks(BeforeRead))

	hr.Clear(BeforeRead)
	assert.False(t, hr.HasHooks(BeforeRead))
}

func TestHookRegistry_ClearAll(t *testing.T) {
	hr := NewHookRegistry()

	hr.Register(BeforeRead, func(ctx *HookContext) error { return nil })
	hr.Register(AfterRead, func(ctx *HookContext) error { return nil })
	hr.Register(BeforeCreate, func(ctx *HookContext) error { return nil })

	assert.True(t, hr.HasHooks(BeforeRead))
	assert.True(t, hr.HasHooks(AfterRead))
	assert.True(t, hr.HasHooks(BeforeCreate))

	hr.ClearAll()

	assert.False(t, hr.HasHooks(BeforeRead))
	assert.False(t, hr.HasHooks(AfterRead))
	assert.False(t, hr.HasHooks(BeforeCreate))
}

func TestHookContext_Structure(t *testing.T) {
	ctx := &HookContext{
		Context:   context.Background(),
		Schema:    "public",
		Entity:    "users",
		TableName: "public.users",
		ID:        "123",
		Data: map[string]interface{}{
			"name": "John",
		},
		Options: &common.RequestOptions{
			Filters: []common.FilterOption{
				{Column: "status", Operator: "eq", Value: "active"},
			},
		},
		Metadata: map[string]interface{}{
			"user_id": 456,
		},
	}

	assert.NotNil(t, ctx.Context)
	assert.Equal(t, "public", ctx.Schema)
	assert.Equal(t, "users", ctx.Entity)
	assert.Equal(t, "public.users", ctx.TableName)
	assert.Equal(t, "123", ctx.ID)
	assert.NotNil(t, ctx.Data)
	assert.NotNil(t, ctx.Options)
	assert.NotNil(t, ctx.Metadata)
}

func TestHookContext_ModifyData(t *testing.T) {
	hr := NewHookRegistry()

	// Hook that modifies data
	hook := func(ctx *HookContext) error {
		if data, ok := ctx.Data.(map[string]interface{}); ok {
			data["modified"] = true
		}
		return nil
	}

	hr.Register(BeforeCreate, hook)

	ctx := &HookContext{
		Context: context.Background(),
		Data: map[string]interface{}{
			"name": "John",
		},
	}

	err := hr.Execute(BeforeCreate, ctx)
	require.NoError(t, err)

	// Verify data was modified
	data := ctx.Data.(map[string]interface{})
	assert.True(t, data["modified"].(bool))
}

func TestHookContext_ModifyOptions(t *testing.T) {
	hr := NewHookRegistry()

	// Hook that adds a filter
	hook := func(ctx *HookContext) error {
		if ctx.Options == nil {
			ctx.Options = &common.RequestOptions{}
		}
		ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
			Column:   "user_id",
			Operator: "eq",
			Value:    123,
		})
		return nil
	}

	hr.Register(BeforeRead, hook)

	ctx := &HookContext{
		Context: context.Background(),
		Options: &common.RequestOptions{},
	}

	err := hr.Execute(BeforeRead, ctx)
	require.NoError(t, err)

	// Verify filter was added
	assert.Len(t, ctx.Options.Filters, 1)
	assert.Equal(t, "user_id", ctx.Options.Filters[0].Column)
}

func TestHookContext_UseMetadata(t *testing.T) {
	hr := NewHookRegistry()

	// Hook that stores data in metadata
	hook := func(ctx *HookContext) error {
		ctx.Metadata["processed"] = true
		ctx.Metadata["timestamp"] = "2024-01-01"
		return nil
	}

	hr.Register(BeforeCreate, hook)

	ctx := &HookContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}

	err := hr.Execute(BeforeCreate, ctx)
	require.NoError(t, err)

	// Verify metadata was set
	assert.True(t, ctx.Metadata["processed"].(bool))
	assert.Equal(t, "2024-01-01", ctx.Metadata["timestamp"])
}

func TestHookRegistry_Authentication_Example(t *testing.T) {
	hr := NewHookRegistry()

	// Authentication hook
	authHook := func(ctx *HookContext) error {
		// Simulate getting user from connection metadata
		userID := 123
		ctx.Metadata["user_id"] = userID
		return nil
	}

	// Authorization hook that uses auth data
	authzHook := func(ctx *HookContext) error {
		userID, ok := ctx.Metadata["user_id"]
		if !ok {
			return errors.New("unauthorized: not authenticated")
		}

		// Add filter to only show user's own records
		if ctx.Options == nil {
			ctx.Options = &common.RequestOptions{}
		}
		ctx.Options.Filters = append(ctx.Options.Filters, common.FilterOption{
			Column:   "user_id",
			Operator: "eq",
			Value:    userID,
		})

		return nil
	}

	hr.Register(BeforeConnect, authHook)
	hr.Register(BeforeRead, authzHook)

	// Simulate connection
	ctx1 := &HookContext{
		Context:  context.Background(),
		Metadata: make(map[string]interface{}),
	}
	err := hr.Execute(BeforeConnect, ctx1)
	require.NoError(t, err)
	assert.Equal(t, 123, ctx1.Metadata["user_id"])

	// Simulate read with authorization
	ctx2 := &HookContext{
		Context:  context.Background(),
		Metadata: map[string]interface{}{"user_id": 123},
		Options:  &common.RequestOptions{},
	}
	err = hr.Execute(BeforeRead, ctx2)
	require.NoError(t, err)
	assert.Len(t, ctx2.Options.Filters, 1)
	assert.Equal(t, "user_id", ctx2.Options.Filters[0].Column)
}

func TestHookRegistry_Validation_Example(t *testing.T) {
	hr := NewHookRegistry()

	// Validation hook
	validationHook := func(ctx *HookContext) error {
		data, ok := ctx.Data.(map[string]interface{})
		if !ok {
			return errors.New("invalid data format")
		}

		if ctx.Entity == "users" {
			email, hasEmail := data["email"]
			if !hasEmail || email == "" {
				return errors.New("validation error: email is required")
			}

			name, hasName := data["name"]
			if !hasName || name == "" {
				return errors.New("validation error: name is required")
			}
		}

		return nil
	}

	hr.Register(BeforeCreate, validationHook)

	// Test with valid data
	ctx1 := &HookContext{
		Context: context.Background(),
		Entity:  "users",
		Data: map[string]interface{}{
			"name":  "John Doe",
			"email": "john@example.com",
		},
	}
	err := hr.Execute(BeforeCreate, ctx1)
	assert.NoError(t, err)

	// Test with missing email
	ctx2 := &HookContext{
		Context: context.Background(),
		Entity:  "users",
		Data: map[string]interface{}{
			"name": "John Doe",
		},
	}
	err = hr.Execute(BeforeCreate, ctx2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "email is required")

	// Test with missing name
	ctx3 := &HookContext{
		Context: context.Background(),
		Entity:  "users",
		Data: map[string]interface{}{
			"email": "john@example.com",
		},
	}
	err = hr.Execute(BeforeCreate, ctx3)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestHookRegistry_Logging_Example(t *testing.T) {
	hr := NewHookRegistry()

	logEntries := []string{}

	// Logging hook for create operations
	loggingHook := func(ctx *HookContext) error {
		logEntries = append(logEntries, "Created record in "+ctx.Entity)
		return nil
	}

	hr.Register(AfterCreate, loggingHook)

	ctx := &HookContext{
		Context: context.Background(),
		Entity:  "users",
		Result:  map[string]interface{}{"id": 1, "name": "John"},
	}

	err := hr.Execute(AfterCreate, ctx)
	require.NoError(t, err)
	assert.Len(t, logEntries, 1)
	assert.Equal(t, "Created record in users", logEntries[0])
}

func TestHookRegistry_ConcurrentExecution(t *testing.T) {
	hr := NewHookRegistry()

	// This test verifies that concurrent hook executions don't cause race conditions
	// Run with: go test -race

	counter := 0
	hook := func(ctx *HookContext) error {
		counter++
		return nil
	}

	hr.Register(BeforeRead, hook)

	done := make(chan bool)

	// Execute hooks concurrently
	for i := 0; i < 10; i++ {
		go func() {
			ctx := &HookContext{Context: context.Background()}
			hr.Execute(BeforeRead, ctx)
			done <- true
		}()
	}

	// Wait for all executions
	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 10, counter)
}
