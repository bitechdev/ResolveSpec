package funcspec

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// HookType defines the type of hook to execute
type HookType string

const (
	// Query operation hooks (for SqlQuery - single record)
	BeforeQuery HookType = "before_query"
	AfterQuery  HookType = "after_query"

	// Query list operation hooks (for SqlQueryList - multiple records)
	BeforeQueryList HookType = "before_query_list"
	AfterQueryList  HookType = "after_query_list"

	// SQL execution hooks (just before SQL is executed)
	BeforeSQLExec HookType = "before_sql_exec"
	AfterSQLExec  HookType = "after_sql_exec"

	// Response hooks (before response is sent)
	BeforeResponse HookType = "before_response"
)

// HookContext contains all the data available to a hook
type HookContext struct {
	Context context.Context
	Handler *Handler // Reference to the handler for accessing database
	Request *http.Request
	Writer  http.ResponseWriter

	// SQL query and variables
	SQLQuery    string                 // The SQL query being executed (can be modified by hooks)
	Variables   map[string]interface{} // Variables extracted from request
	InputVars   []string               // Input variable placeholders found in query
	MetaInfo    map[string]interface{} // Metadata about the request
	PropQry     map[string]string      // Property query parameters

	// User context
	UserContext *security.UserContext

	// Pagination and filtering (for list queries)
	SortColumns string
	Limit       int
	Offset      int

	// Results
	Result      interface{} // Query result (single record or list)
	Total       int64       // Total count (for list queries)
	Error       error       // Error if operation failed
	ComplexAPI  bool        // Whether complex API response format is requested
	NoCount     bool        // Whether count query should be skipped
	BlankParams bool        // Whether blank parameters should be removed
	AllowFilter bool        // Whether filtering is allowed

	// Allow hooks to abort the operation
	Abort        bool   // If set to true, the operation will be aborted
	AbortMessage string // Message to return if aborted
	AbortCode    int    // HTTP status code if aborted
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
	logger.Info("Registered funcspec hook for %s (total: %d)", hookType, len(r.hooks[hookType]))
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
		return nil
	}

	logger.Debug("Executing %d funcspec hook(s) for %s", len(hooks), hookType)

	for i, hook := range hooks {
		if err := hook(ctx); err != nil {
			logger.Error("Funcspec hook %d for %s failed: %v", i+1, hookType, err)
			return fmt.Errorf("hook execution failed: %w", err)
		}

		// Check if hook requested abort
		if ctx.Abort {
			logger.Warn("Funcspec hook %d for %s requested abort: %s", i+1, hookType, ctx.AbortMessage)
			return fmt.Errorf("operation aborted by hook: %s", ctx.AbortMessage)
		}
	}

	return nil
}

// Clear removes all hooks for the specified type
func (r *HookRegistry) Clear(hookType HookType) {
	delete(r.hooks, hookType)
	logger.Info("Cleared all funcspec hooks for %s", hookType)
}

// ClearAll removes all registered hooks
func (r *HookRegistry) ClearAll() {
	r.hooks = make(map[HookType][]HookFunc)
	logger.Info("Cleared all funcspec hooks")
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
