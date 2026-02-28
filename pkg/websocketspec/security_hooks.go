package websocketspec

import (
	"context"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// RegisterSecurityHooks registers all security-related hooks with the handler
func RegisterSecurityHooks(handler *Handler, securityList *security.SecurityList) {
	// Hook 1: BeforeRead - Load security rules
	handler.Hooks().Register(BeforeRead, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.LoadSecurityRules(secCtx, securityList)
	})

	// Hook 2: AfterRead - Apply column-level security (masking)
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.ApplyColumnSecurity(secCtx, securityList)
	})

	// Hook 3 (Optional): Audit logging
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.LogDataAccess(secCtx)
	})

	// Hook 4: BeforeUpdate - enforce CanUpdate rule from context/registry
	handler.Hooks().Register(BeforeUpdate, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.CheckModelUpdateAllowed(secCtx)
	})

	// Hook 5: BeforeDelete - enforce CanDelete rule from context/registry
	handler.Hooks().Register(BeforeDelete, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.CheckModelDeleteAllowed(secCtx)
	})

	logger.Info("Security hooks registered for websocketspec handler")
}

// securityContext adapts websocketspec.HookContext to security.SecurityContext interface
type securityContext struct {
	ctx *HookContext
}

func newSecurityContext(ctx *HookContext) security.SecurityContext {
	return &securityContext{ctx: ctx}
}

func (s *securityContext) GetContext() context.Context {
	return s.ctx.Context
}

func (s *securityContext) GetUserID() (int, bool) {
	return security.GetUserID(s.ctx.Context)
}

func (s *securityContext) GetSchema() string {
	return s.ctx.Schema
}

func (s *securityContext) GetEntity() string {
	return s.ctx.Entity
}

func (s *securityContext) GetModel() interface{} {
	return s.ctx.Model
}

// GetQuery retrieves a stored query from hook metadata (websocketspec has no Query field)
func (s *securityContext) GetQuery() interface{} {
	if s.ctx.Metadata == nil {
		return nil
	}
	return s.ctx.Metadata["query"]
}

// SetQuery stores the query in hook metadata
func (s *securityContext) SetQuery(query interface{}) {
	if s.ctx.Metadata == nil {
		s.ctx.Metadata = make(map[string]interface{})
	}
	s.ctx.Metadata["query"] = query
}

func (s *securityContext) GetResult() interface{} {
	return s.ctx.Result
}

func (s *securityContext) SetResult(result interface{}) {
	s.ctx.Result = result
}
