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
	// OnTxBegin: stamp transaction-local settings (e.g. RLS GUCs) before any SQL.
	// Looked up per call so SetTxSettings may come after registration.
	handler.Hooks().Register(OnTxBegin, func(hookCtx *HookContext) error {
		return security.StampTxSettings(newSecurityContext(hookCtx), securityList, hookCtx.Tx)
	})

	// BeforeHandle: enforce model-level operation rules (auth check).
	handler.Hooks().Register(BeforeHandle, func(hookCtx *HookContext) error {
		if err := security.CheckModelAuthAllowed(newSecurityContext(hookCtx), hookCtx.Operation); err != nil {
			hookCtx.Abort = true
			hookCtx.AbortMessage = err.Error()
			hookCtx.AbortCode = http.StatusUnauthorized
			return NewClientError(CodeForbidden, err.Error())
		}
		return nil
	})

	// BeforeHandle: preload column rules for writes before the handler opens its
	// transaction; the write hooks below only read the cache.
	handler.Hooks().Register(BeforeHandle, func(hookCtx *HookContext) error {
		return security.PreloadSecurityRules(newSecurityContext(hookCtx), securityList, hookCtx.Operation)
	})

	// BeforeCreate/BeforeUpdate: drop columns hidden or masked for the user from the
	// write payload, so they cannot be inserted or updated.
	handler.Hooks().Register(BeforeCreate, func(hookCtx *HookContext) error {
		return security.ApplyWriteColumnSecurity(newSecurityContext(hookCtx), securityList)
	})
	handler.Hooks().Register(BeforeUpdate, func(hookCtx *HookContext) error {
		return security.ApplyWriteColumnSecurity(newSecurityContext(hookCtx), securityList)
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

	// BeforeScan: row-level security on the row an update or delete targets. A row the user
	// cannot see is "not found" and is never written.
	handler.Hooks().Register(BeforeScan, func(hookCtx *HookContext) error {
		if err := security.LoadSecurityRules(newSecurityContext(hookCtx), securityList); err != nil {
			return err
		}
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

	// BeforeCreate: enforce CanCreate rule.
	handler.Hooks().Register(BeforeCreate, func(hookCtx *HookContext) error {
		return forbidden(security.CheckModelCreateAllowed(newSecurityContext(hookCtx)))
	})

	// BeforeUpdate: enforce CanUpdate rule.
	handler.Hooks().Register(BeforeUpdate, func(hookCtx *HookContext) error {
		return forbidden(security.CheckModelUpdateAllowed(newSecurityContext(hookCtx)))
	})

	// BeforeDelete: enforce CanDelete rule.
	handler.Hooks().Register(BeforeDelete, func(hookCtx *HookContext) error {
		return forbidden(security.CheckModelDeleteAllowed(newSecurityContext(hookCtx)))
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

// GetUserRef returns an opaque user identifier for row security lookups.
// It prefers the full *security.UserContext (so providers can read JWT claims,
// e.g. a UUID subject) and falls back to the int user ID.
func (s *securityContext) GetUserRef() (any, bool) {
	if userCtx, ok := security.GetUserContext(s.ctx.Context); ok {
		return userCtx, true
	}
	userID, ok := security.GetUserID(s.ctx.Context)
	return userID, ok
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

func (s *securityContext) GetData() interface{} {
	return s.ctx.Data
}

func (s *securityContext) SetData(data interface{}) {
	s.ctx.Data = data
}

func (s *securityContext) GetResult() interface{} {
	return s.ctx.Result
}

func (s *securityContext) SetResult(result interface{}) {
	s.ctx.Result = result
}

// forbidden marks a rule denial as safe to show the client; nil passes through.
func forbidden(err error) error {
	if err == nil {
		return nil
	}
	return NewClientError(CodeForbidden, err.Error())
}
