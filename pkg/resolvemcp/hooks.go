package resolvemcp

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// HookType defines the type of hook to execute
type HookType string

const (
	// BeforeHandle fires after model resolution, before operation dispatch.
	BeforeHandle HookType = "before_handle"

	BeforeRead HookType = "before_read"
	AfterRead  HookType = "after_read"

	BeforeCreate HookType = "before_create"
	AfterCreate  HookType = "after_create"

	// BeforeScan fires on update and delete, with hookCtx.Query set to the select that loads the
	// target row. Hooks that narrow the query (row security) run here; a row the query does
	// not return is reported as not found and never written.
	BeforeScan HookType = "before_scan"

	BeforeUpdate HookType = "before_update"
	AfterUpdate  HookType = "after_update"

	BeforeDelete HookType = "before_delete"
	AfterDelete  HookType = "after_delete"

	// BeforeCall and AfterCall fire inside the transaction of a call_function call.
	// hookCtx.Entity is the function name, Data the validated arguments (BeforeCall may
	// replace them) and Result the function's result (AfterCall).
	BeforeCall HookType = "before_call"
	AfterCall  HookType = "after_call"

	// OnTxBegin fires once, first, inside every transaction the handler opens
	// (including the second short transaction for post-commit work). hookCtx.Tx is
	// the transaction; use it to stamp transaction-local state such as RLS
	// settings. An error or abort rolls the transaction back.
	OnTxBegin HookType = common.TxHookName
)

// HookContext contains all the data available to a hook
type HookContext struct {
	Context      context.Context
	Handler      *Handler
	Schema       string
	Entity       string
	Model        interface{}
	Options      common.RequestOptions
	Operation    string
	ID           string
	Data         interface{}
	Result       interface{}
	Error        error
	Query        common.SelectQuery
	Abort        bool
	AbortMessage string
	AbortCode    int
	Tx           common.Database
}

// SetTx points the context at the transaction in use (common.TxContext).
func (c *HookContext) SetTx(tx common.Database) { c.Tx = tx }

// HookFunc is the signature for hook functions
type HookFunc func(*HookContext) error

// HookRegistry manages all registered hooks
type HookRegistry struct {
	mu    sync.RWMutex
	hooks map[HookType][]HookFunc
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

func (r *HookRegistry) Register(hookType HookType, hook HookFunc) {
	r.mu.Lock()
	if r.hooks == nil {
		r.hooks = make(map[HookType][]HookFunc)
	}
	r.hooks[hookType] = append(r.hooks[hookType], hook)
	total := len(r.hooks[hookType])
	r.mu.Unlock()
	logger.Info("Registered resolvemcp hook for %s (total: %d)", hookType, total)
}

func (r *HookRegistry) RegisterMultiple(hookTypes []HookType, hook HookFunc) {
	for _, hookType := range hookTypes {
		r.Register(hookType, hook)
	}
}

func (r *HookRegistry) Execute(hookType HookType, ctx *HookContext) error {
	// Append-only slices: a snapshot of the slice header is safe to iterate without the lock.
	r.mu.RLock()
	hooks := r.hooks[hookType]
	r.mu.RUnlock()
	if len(hooks) == 0 {
		return nil
	}

	logger.Debug("Executing %d resolvemcp hook(s) for %s", len(hooks), hookType)

	for i, hook := range hooks {
		if err := runHook(hook, ctx); err != nil {
			logger.Error("resolvemcp hook %d for %s failed: %v", i+1, hookType, err)
			return fmt.Errorf("hook execution failed: %w", err)
		}

		if ctx.Abort {
			logger.Warn("resolvemcp hook %d for %s requested abort: %s", i+1, hookType, ctx.AbortMessage)
			return fmt.Errorf("operation aborted by hook: %w", NewClientError(CodeForbidden, ctx.AbortMessage))
		}
	}

	return nil
}

// runHook calls hook and turns a panic into an error, so a faulty hook fails the request
// instead of unwinding through the transaction machinery. The stack is logged, not returned.
func runHook(hook HookFunc, ctx *HookContext) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("resolvemcp hook panic: %v\n%s", r, debug.Stack())
			err = errInternal
		}
	}()
	return hook(ctx)
}

func (r *HookRegistry) Clear(hookType HookType) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hooks, hookType)
}

func (r *HookRegistry) ClearAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = make(map[HookType][]HookFunc)
}

func (r *HookRegistry) HasHooks(hookType HookType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hooks[hookType]) > 0
}
