package resolvemcp

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
	BeforeHandle HookType = "before_handle"

	BeforeRead HookType = "before_read"
	AfterRead  HookType = "after_read"

	BeforeCreate HookType = "before_create"
	AfterCreate  HookType = "after_create"

	BeforeUpdate HookType = "before_update"
	AfterUpdate  HookType = "after_update"

	BeforeDelete HookType = "before_delete"
	AfterDelete  HookType = "after_delete"
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

// HookFunc is the signature for hook functions
type HookFunc func(*HookContext) error

// HookRegistry manages all registered hooks
type HookRegistry struct {
	hooks map[HookType][]HookFunc
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookType][]HookFunc),
	}
}

func (r *HookRegistry) Register(hookType HookType, hook HookFunc) {
	if r.hooks == nil {
		r.hooks = make(map[HookType][]HookFunc)
	}
	r.hooks[hookType] = append(r.hooks[hookType], hook)
	logger.Info("Registered resolvemcp hook for %s (total: %d)", hookType, len(r.hooks[hookType]))
}

func (r *HookRegistry) RegisterMultiple(hookTypes []HookType, hook HookFunc) {
	for _, hookType := range hookTypes {
		r.Register(hookType, hook)
	}
}

func (r *HookRegistry) Execute(hookType HookType, ctx *HookContext) error {
	hooks, exists := r.hooks[hookType]
	if !exists || len(hooks) == 0 {
		return nil
	}

	logger.Debug("Executing %d resolvemcp hook(s) for %s", len(hooks), hookType)

	for i, hook := range hooks {
		if err := hook(ctx); err != nil {
			logger.Error("resolvemcp hook %d for %s failed: %v", i+1, hookType, err)
			return fmt.Errorf("hook execution failed: %w", err)
		}

		if ctx.Abort {
			logger.Warn("resolvemcp hook %d for %s requested abort: %s", i+1, hookType, ctx.AbortMessage)
			return fmt.Errorf("operation aborted by hook: %s", ctx.AbortMessage)
		}
	}

	return nil
}

func (r *HookRegistry) Clear(hookType HookType) {
	delete(r.hooks, hookType)
}

func (r *HookRegistry) ClearAll() {
	r.hooks = make(map[HookType][]HookFunc)
}

func (r *HookRegistry) HasHooks(hookType HookType) bool {
	hooks, exists := r.hooks[hookType]
	return exists && len(hooks) > 0
}
