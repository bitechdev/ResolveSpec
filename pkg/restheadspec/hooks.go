package restheadspec

import (
	"context"
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// HookType defines the type of hook to execute
type HookType string

const (
	// BeforeHandle fires after model resolution, before operation dispatch.
	// Use this for auth checks that need model rules and user context simultaneously.
	BeforeHandle HookType = "before_handle"

	// Read operation hooks
	BeforeRead HookType = "before_read"
	AfterRead  HookType = "after_read"

	// Create operation hooks
	BeforeCreate HookType = "before_create"
	AfterCreate  HookType = "after_create"

	// Update operation hooks
	BeforeUpdate HookType = "before_update"
	AfterUpdate  HookType = "after_update"

	// Delete operation hooks
	BeforeDelete HookType = "before_delete"
	AfterDelete  HookType = "after_delete"

	// Scan/Execute operation hooks
	BeforeScan HookType = "before_scan"
)

// HookContext contains all the data available to a hook
type HookContext struct {
	Context   context.Context
	Handler   *Handler // Reference to the handler for accessing database, registry, etc.
	Schema    string
	Entity    string
	TableName string
	Model     interface{}
	Options   ExtendedRequestOptions

	// Operation being dispatched (e.g. "read", "create", "update", "delete")
	Operation string

	// Operation-specific fields
	ID          string
	Data        interface{} // For create/update operations
	Result      interface{} // For after hooks
	Error       error       // For after hooks
	QueryFilter string      // For read operations

	// Query chain - allows hooks to modify the query before execution
	// Can be SelectQuery, InsertQuery, UpdateQuery, or DeleteQuery
	Query interface{}

	// Response writer - allows hooks to modify response
	Writer common.ResponseWriter

	// Request - the original HTTP request
	Request common.Request

	// Allow hooks to abort the operation
	Abort        bool   // If set to true, the operation will be aborted
	AbortMessage string // Message to return if aborted
	AbortCode    int    // HTTP status code if aborted

	// Tx provides access to the database/transaction for executing additional SQL
	// This allows hooks to run custom queries in addition to the main Query chain
	Tx common.Database
}

// HookFunc is the signature for hook functions
// It receives a HookContext and can modify it or return an error
// If an error is returned, the operation will be aborted
type HookFunc func(*HookContext) error

// HookRegistry manages all registered hooks
type HookRegistry struct {
	hooks map[HookType][]HookFunc
}

// NewHookRegistry creates a new hook registry
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

// Register adds a new hook for the specified hook type
func (r *HookRegistry) Register(hookType HookType, hook HookFunc) {
	if r.hooks == nil {
		r.hooks = make(map[HookType][]HookFunc)
	}
	r.hooks[hookType] = append(r.hooks[hookType], hook)
	logger.Info("Registered hook for %s (total: %d)", hookType, len(r.hooks[hookType]))
}

// RegisterMultiple registers a hook for multiple hook types
func (r *HookRegistry) RegisterMultiple(hookTypes []HookType, hook HookFunc) {
	for _, hookType := range hookTypes {
		r.Register(hookType, hook)
	}
}

// Execute runs all hooks for the specified type in order
// If any hook returns an error, execution stops and the error is returned
func (r *HookRegistry) Execute(hookType HookType, ctx *HookContext) error {
	hooks, exists := r.hooks[hookType]
	if !exists || len(hooks) == 0 {
		// logger.Debug("No hooks registered for %s", hookType)
		return nil
	}

	logger.Debug("Executing %d hook(s) for %s", len(hooks), hookType)

	for i, hook := range hooks {
		if err := hook(ctx); err != nil {
			logger.Error("Hook %d for %s failed: %v", i+1, hookType, err)
			return fmt.Errorf("hook execution failed: %w", err)
		}

		// Check if hook requested abort
		if ctx.Abort {
			logger.Warn("Hook %d for %s requested abort: %s", i+1, hookType, ctx.AbortMessage)
			return fmt.Errorf("operation aborted by hook: %s", ctx.AbortMessage)
		}
	}

	// logger.Debug("All hooks for %s executed successfully", hookType)
	return nil
}

// Clear removes all hooks for the specified type
func (r *HookRegistry) Clear(hookType HookType) {
	delete(r.hooks, hookType)
	logger.Info("Cleared all hooks for %s", hookType)
}

// ClearAll removes all registered hooks
func (r *HookRegistry) ClearAll() {
	r.hooks = make(map[HookType][]HookFunc)
	logger.Info("Cleared all hooks")
}

// Count returns the number of hooks registered for a specific type
func (r *HookRegistry) Count(hookType HookType) int {
	if hooks, exists := r.hooks[hookType]; exists {
		return len(hooks)
	}
	return 0
}

// HasHooks returns true if there are any hooks registered for the specified type
func (r *HookRegistry) HasHooks(hookType HookType) bool {
	return r.Count(hookType) > 0
}

// GetAllHookTypes returns all hook types that have registered hooks
func (r *HookRegistry) GetAllHookTypes() []HookType {
	types := make([]HookType, 0, len(r.hooks))
	for hookType := range r.hooks {
		types = append(types, hookType)
	}
	return types
}
