package mqttspec

import (
	"context"
	"net/http"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// RegisterSecurityHooks registers all security-related hooks with the MQTT handler
func RegisterSecurityHooks(handler *Handler, securityList *security.SecurityList) {
	// Hook 0: BeforeHandle - enforce auth after model resolution
	handler.Hooks().Register(BeforeHandle, func(hookCtx *HookContext) error {
		if err := security.CheckModelAuthAllowed(newSecurityContext(hookCtx), hookCtx.Operation); err != nil {
			hookCtx.Abort = true
			hookCtx.AbortMessage = err.Error()
			hookCtx.AbortCode = http.StatusUnauthorized
			return err
		}
		return nil
	})

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

	// Hook 5: BeforeUpdate - enforce CanUpdate rule from context/registry
	handler.Hooks().Register(BeforeUpdate, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.CheckModelUpdateAllowed(secCtx)
	})

	// Hook 6: BeforeDelete - enforce CanDelete rule from context/registry
	handler.Hooks().Register(BeforeDelete, func(hookCtx *HookContext) error {
		secCtx := newSecurityContext(hookCtx)
		return security.CheckModelDeleteAllowed(secCtx)
	})

	logger.Info("Security hooks registered for mqttspec handler")
}

// securityContext adapts mqttspec.HookContext to security.SecurityContext interface
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

// GetQuery retrieves a stored query from hook metadata
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
