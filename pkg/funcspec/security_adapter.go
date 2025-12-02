package funcspec

import (
	"context"

	"github.com/bitechdev/ResolveSpec/pkg/security"
)

// RegisterSecurityHooks registers security hooks for funcspec handlers
// Note: funcspec operates on SQL queries directly, so row-level security is not directly applicable
// We provide audit logging for data access tracking
func RegisterSecurityHooks(handler *Handler, securityList *security.SecurityList) {
	// Hook 1: BeforeQueryList - Audit logging before query list execution
	handler.Hooks().Register(BeforeQueryList, func(hookCtx *HookContext) error {
		secCtx := newFuncSpecSecurityContext(hookCtx)
		return security.LogDataAccess(secCtx)
	})

	// Hook 2: BeforeQuery - Audit logging before single query execution
	handler.Hooks().Register(BeforeQuery, func(hookCtx *HookContext) error {
		secCtx := newFuncSpecSecurityContext(hookCtx)
		return security.LogDataAccess(secCtx)
	})

	// Note: Row-level security and column masking are challenging in funcspec
	// because the SQL query is fully user-defined. Security should be implemented
	// at the SQL function level or through database policies (RLS).
}

// funcSpecSecurityContext adapts funcspec.HookContext to security.SecurityContext interface
type funcSpecSecurityContext struct {
	ctx *HookContext
}

func newFuncSpecSecurityContext(ctx *HookContext) security.SecurityContext {
	return &funcSpecSecurityContext{ctx: ctx}
}

func (f *funcSpecSecurityContext) GetContext() context.Context {
	return f.ctx.Context
}

func (f *funcSpecSecurityContext) GetUserID() (int, bool) {
	if f.ctx.UserContext == nil {
		return 0, false
	}
	return int(f.ctx.UserContext.UserID), true
}

func (f *funcSpecSecurityContext) GetSchema() string {
	// funcspec doesn't have a schema concept, extract from SQL query or use default
	return "public"
}

func (f *funcSpecSecurityContext) GetEntity() string {
	// funcspec doesn't have an entity concept, could parse from SQL or use a placeholder
	return "sql_query"
}

func (f *funcSpecSecurityContext) GetModel() interface{} {
	// funcspec doesn't use models in the same way as restheadspec
	return nil
}

func (f *funcSpecSecurityContext) GetQuery() interface{} {
	// In funcspec, the query is a string, not a query builder object
	return f.ctx.SQLQuery
}

func (f *funcSpecSecurityContext) SetQuery(query interface{}) {
	// In funcspec, we could modify the SQL string, but this should be done cautiously
	if sqlQuery, ok := query.(string); ok {
		f.ctx.SQLQuery = sqlQuery
	}
}

func (f *funcSpecSecurityContext) GetResult() interface{} {
	return f.ctx.Result
}

func (f *funcSpecSecurityContext) SetResult(result interface{}) {
	f.ctx.Result = result
}
