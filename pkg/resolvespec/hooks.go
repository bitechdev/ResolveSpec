package resolvespec

import (
	"context"
	"fmt"
	"sync"
	"time"

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

	// Scan/Execute operation hooks (for query building)
	BeforeScan HookType = "before_scan"

	// BeforeOp fires immediately before every SQL operation (read, create, update, delete, scan).
	// Unlike BeforeHandle, which fires once before operation dispatch, BeforeOp fires at each
	// individual SQL-operation hook point, so it runs once per statement executed.
	BeforeOp HookType = "before_op"
)

// HookContext contains all the data available to a hook
type HookContext struct {
	Context context.Context
	Handler *Handler // Reference to the handler for accessing database, registry, etc.
	Schema  string
	Entity  string
	Model   interface{}
	Options common.RequestOptions
	Writer  common.ResponseWriter
	Request common.Request

	// Operation being dispatched (e.g. "read", "create", "update", "delete")
	Operation string

	// Operation-specific fields
	ID     string
	Data   interface{} // For create/update operations
	Result interface{} // For after hooks
	Error  error       // For after hooks

	// Query chain - allows hooks to modify the query before execution
	Query common.SelectQuery

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
	mutex sync.RWMutex
}

// NewHookRegistry creates a new hook registry
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

// hookLockRetryAttempts/hookLockRetryDelay bound how long the try-lock
// helpers below will spin before giving up, so a contended mutex can
// never hang a caller.
const (
	hookLockRetryAttempts = 20
	hookLockRetryDelay    = 1 * time.Millisecond
)

// tryLock attempts to acquire the write lock, retrying briefly. Returns
// false if it could not be acquired within the bound.
func (r *HookRegistry) tryLock() bool {
	for i := 0; i < hookLockRetryAttempts; i++ {
		if r.mutex.TryLock() {
			return true
		}
		time.Sleep(hookLockRetryDelay)
	}
	return false
}

// tryRLock attempts to acquire the read lock, retrying briefly. Returns
// false if it could not be acquired within the bound.
func (r *HookRegistry) tryRLock() bool {
	for i := 0; i < hookLockRetryAttempts; i++ {
		if r.mutex.TryRLock() {
			return true
		}
		time.Sleep(hookLockRetryDelay)
	}
	return false
}

// Register adds a new hook for the specified hook type
func (r *HookRegistry) Register(hookType HookType, hook HookFunc) {
	if !r.tryLock() {
		logger.Error("Failed to register resolvespec hook for %s: registry locked", hookType)
		return
	}
	defer r.mutex.Unlock()

	if r.hooks == nil {
		r.hooks = make(map[HookType][]HookFunc)
	}
	r.hooks[hookType] = append(r.hooks[hookType], hook)
	logger.Info("Registered resolvespec hook for %s (total: %d)", hookType, len(r.hooks[hookType]))
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
	if !r.tryRLock() {
		return fmt.Errorf("hook execution failed: registry locked")
	}
	hooks := append([]HookFunc(nil), r.hooks[hookType]...)
	r.mutex.RUnlock()

	if len(hooks) == 0 {
		return nil
	}

	logger.Debug("Executing %d resolvespec hook(s) for %s", len(hooks), hookType)

	for i, hook := range hooks {
		if err := hook(ctx); err != nil {
			logger.Error("Resolvespec hook %d for %s failed: %v", i+1, hookType, err)
			return fmt.Errorf("hook execution failed: %w", err)
		}

		// Check if hook requested abort
		if ctx.Abort {
			logger.Warn("Resolvespec hook %d for %s requested abort: %s", i+1, hookType, ctx.AbortMessage)
			return fmt.Errorf("operation aborted by hook: %s", ctx.AbortMessage)
		}
	}

	return nil
}

// ExecuteBeforeOp executes the BeforeOp hook followed by the given SQL-operation hook
// (BeforeRead, BeforeCreate, BeforeUpdate, BeforeDelete, or BeforeScan). BeforeOp always
// runs first so it can observe/veto every SQL operation regardless of type.
func (r *HookRegistry) ExecuteBeforeOp(hookType HookType, ctx *HookContext) error {
	if err := r.Execute(BeforeOp, ctx); err != nil {
		return err
	}
	return r.Execute(hookType, ctx)
}

// Clear removes all hooks for the specified type
func (r *HookRegistry) Clear(hookType HookType) {
	if !r.tryLock() {
		logger.Error("Failed to clear resolvespec hooks for %s: registry locked", hookType)
		return
	}
	defer r.mutex.Unlock()

	delete(r.hooks, hookType)
	logger.Info("Cleared all resolvespec hooks for %s", hookType)
}

// ClearAll removes all registered hooks
func (r *HookRegistry) ClearAll() {
	if !r.tryLock() {
		logger.Error("Failed to clear all resolvespec hooks: registry locked")
		return
	}
	defer r.mutex.Unlock()

	r.hooks = make(map[HookType][]HookFunc)
	logger.Info("Cleared all resolvespec hooks")
}

// Count returns the number of hooks registered for a specific type
func (r *HookRegistry) Count(hookType HookType) int {
	if !r.tryRLock() {
		return 0
	}
	defer r.mutex.RUnlock()

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
	if !r.tryRLock() {
		return nil
	}
	defer r.mutex.RUnlock()

	types := make([]HookType, 0, len(r.hooks))
	for hookType := range r.hooks {
		types = append(types, hookType)
	}
	return types
}
