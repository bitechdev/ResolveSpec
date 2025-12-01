package funcspec

import (
	"fmt"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// Example hook functions demonstrating various use cases

// ExampleLoggingHook logs all SQL queries before execution
func ExampleLoggingHook(ctx *HookContext) error {
	logger.Info("Executing SQL query for user %s: %s", ctx.UserContext.UserName, ctx.SQLQuery)
	return nil
}

// ExampleSecurityHook validates user permissions before executing queries
func ExampleSecurityHook(ctx *HookContext) error {
	// Example: Block queries that try to access sensitive tables
	if strings.Contains(strings.ToLower(ctx.SQLQuery), "sensitive_table") {
		if ctx.UserContext.UserID != 1 { // Only admin can access
			ctx.Abort = true
			ctx.AbortCode = 403
			ctx.AbortMessage = "Access denied: insufficient permissions"
			return fmt.Errorf("access denied to sensitive_table")
		}
	}
	return nil
}

// ExampleQueryModificationHook modifies SQL queries to add user-specific filtering
func ExampleQueryModificationHook(ctx *HookContext) error {
	// Example: Automatically add user_id filter for non-admin users
	if ctx.UserContext.UserID != 1 { // Not admin
		// Add WHERE clause to filter by user_id
		if !strings.Contains(strings.ToLower(ctx.SQLQuery), "where") {
			ctx.SQLQuery = fmt.Sprintf("%s WHERE user_id = %d", ctx.SQLQuery, ctx.UserContext.UserID)
		} else {
			ctx.SQLQuery = strings.Replace(
				ctx.SQLQuery,
				"WHERE",
				fmt.Sprintf("WHERE user_id = %d AND", ctx.UserContext.UserID),
				1,
			)
		}
		logger.Debug("Modified query for user %d: %s", ctx.UserContext.UserID, ctx.SQLQuery)
	}
	return nil
}

// ExampleResultFilterHook filters results after query execution
func ExampleResultFilterHook(ctx *HookContext) error {
	// Example: Remove sensitive fields from results for non-admin users
	if ctx.UserContext.UserID != 1 { // Not admin
		switch result := ctx.Result.(type) {
		case []map[string]interface{}:
			// Filter list results
			for i := range result {
				delete(result[i], "password")
				delete(result[i], "ssn")
				delete(result[i], "credit_card")
			}
		case map[string]interface{}:
			// Filter single record
			delete(result, "password")
			delete(result, "ssn")
			delete(result, "credit_card")
		}
	}
	return nil
}

// ExampleAuditHook logs all queries and results for audit purposes
func ExampleAuditHook(ctx *HookContext) error {
	// Log to audit table or external system
	logger.Info("AUDIT: User %s (%d) executed query from %s",
		ctx.UserContext.UserName,
		ctx.UserContext.UserID,
		ctx.Request.RemoteAddr,
	)

	// In a real implementation, you might:
	// - Insert into an audit log table
	// - Send to a logging service
	// - Write to a file

	return nil
}

// ExampleCacheHook implements simple response caching
func ExampleCacheHook(ctx *HookContext) error {
	// This is a simplified example - real caching would use a proper cache store
	// Check if we have a cached result for this query
	// cacheKey := fmt.Sprintf("%s:%s", ctx.UserContext.UserName, ctx.SQLQuery)
	// if cachedResult := checkCache(cacheKey); cachedResult != nil {
	//     ctx.Result = cachedResult
	//     ctx.Abort = true // Skip query execution
	//     ctx.AbortMessage = "Serving from cache"
	// }
	return nil
}

// ExampleErrorHandlingHook provides custom error handling
func ExampleErrorHandlingHook(ctx *HookContext) error {
	if ctx.Error != nil {
		// Log error with context
		logger.Error("Query failed for user %s: %v\nQuery: %s",
			ctx.UserContext.UserName,
			ctx.Error,
			ctx.SQLQuery,
		)

		// You could send notifications, update metrics, etc.
	}
	return nil
}

// Example of registering hooks:
//
// func SetupHooks(handler *Handler) {
//     hooks := handler.Hooks()
//
//     // Register security hook before query execution
//     hooks.Register(BeforeQuery, ExampleSecurityHook)
//     hooks.Register(BeforeQueryList, ExampleSecurityHook)
//
//     // Register logging hook before SQL execution
//     hooks.Register(BeforeSQLExec, ExampleLoggingHook)
//
//     // Register result filtering after query
//     hooks.Register(AfterQuery, ExampleResultFilterHook)
//     hooks.Register(AfterQueryList, ExampleResultFilterHook)
//
//     // Register audit hook after execution
//     hooks.RegisterMultiple([]HookType{AfterQuery, AfterQueryList}, ExampleAuditHook)
// }
