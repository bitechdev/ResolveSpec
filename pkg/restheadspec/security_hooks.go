package restheadspec

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

	// Hook 2: BeforeScan - Apply row-level security filters
	handler.Hooks().Register(BeforeScan, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.ApplyRowSecurity(secCtx, securityList)
	})

	// Hook 3: AfterRead - Apply column-level security (masking)
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.ApplyColumnSecurity(secCtx, securityList)
	})

	// Hook 4 (Optional): Audit logging
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.LogDataAccess(secCtx)
	})

	logger.Info("Security hooks registered for restheadspec handler")
}

// securityContext adapts restheadspec.HookContext to security.SecurityContext interface
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

func (s *securityContext) GetQuery() interface{} {
	return s.ctx.Query
}

func (s *securityContext) SetQuery(query interface{}) {
	s.ctx.Query = query
}

func (s *securityContext) GetResult() interface{} {
	return s.ctx.Result
}

func (s *securityContext) SetResult(result interface{}) {
	s.ctx.Result = result
}
