package resolvemcp

import (
	"context"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// RegisterSecurityHooks wires the security package's access-control layer into the
// resolvemcp handler. Call it once after creating the handler, before registering models.
//
// The following controls are applied:
//   - Per-entity operation rules (CanRead, CanCreate, CanUpdate, CanDelete, CanPublic*)
//     stored via RegisterModelWithRules / SetModelRules.
//   - Row-level security: WHERE clause injected per user from the SecurityList provider.
//   - Column-level security: sensitive columns masked/hidden in read results.
//   - Audit logging after each read.
func RegisterSecurityHooks(handler *Handler, securityList *security.SecurityList) {
	// BeforeHandle: enforce model-level operation rules (auth check).
	handler.Hooks().Register(BeforeHandle, func(hookCtx *HookContext) error {
		if err := security.CheckModelAuthAllowed(newSecurityContext(hookCtx), hookCtx.Operation); err != nil {
			hookCtx.Abort = true
			hookCtx.AbortMessage = err.Error()
			hookCtx.AbortCode = http.StatusUnauthorized
			return err
		}
		return nil
	})

	// BeforeRead (1st): load RLS + CLS rules from the provider into SecurityList.
	handler.Hooks().Register(BeforeRead, func(hookCtx *HookContext) error {
		return security.LoadSecurityRules(newSecurityContext(hookCtx), securityList)
	})

	// BeforeRead (2nd): apply row-level security — injects a WHERE clause into the query.
	// resolvemcp has no separate BeforeScan hook; the query is available in BeforeRead.
	handler.Hooks().Register(BeforeRead, func(hookCtx *HookContext) error {
		return security.ApplyRowSecurity(newSecurityContext(hookCtx), securityList)
	})

	// AfterRead (1st): apply column-level security — mask/hide columns in the result.
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		return security.ApplyColumnSecurity(newSecurityContext(hookCtx), securityList)
	})

	// AfterRead (2nd): audit log.
	handler.Hooks().Register(AfterRead, func(hookCtx *HookContext) error {
		return security.LogDataAccess(newSecurityContext(hookCtx))
	})

	// BeforeUpdate: enforce CanUpdate rule.
	handler.Hooks().Register(BeforeUpdate, func(hookCtx *HookContext) error {
		return security.CheckModelUpdateAllowed(newSecurityContext(hookCtx))
	})

	// BeforeDelete: enforce CanDelete rule.
	handler.Hooks().Register(BeforeDelete, func(hookCtx *HookContext) error {
		return security.CheckModelDeleteAllowed(newSecurityContext(hookCtx))
	})

	logger.Info("Security hooks registered for resolvemcp handler")
}

// --------------------------------------------------------------------------
// securityContext — adapts resolvemcp.HookContext to security.SecurityContext
// --------------------------------------------------------------------------

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
	if q, ok := query.(common.SelectQuery); ok {
		s.ctx.Query = q
	}
}

func (s *securityContext) GetResult() interface{} {
	return s.ctx.Result
}

func (s *securityContext) SetResult(result interface{}) {
	s.ctx.Result = result
}
