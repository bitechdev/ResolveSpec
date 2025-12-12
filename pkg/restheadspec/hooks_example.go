package restheadspec

import (
	"fmt"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
)

// This file contains example implementations showing how to use hooks
// These are just examples - you can implement hooks as needed for your application

// ExampleLoggingHook logs before and after operations
func ExampleLoggingHook(hookType HookType) HookFunc {
	return func(ctx *HookContext) error {
		logger.Info("[%s] Operation: %s.%s, ID: %s", hookType, ctx.Schema, ctx.Entity, ctx.ID)
		if ctx.Data != nil {
			logger.Debug("[%s] Data: %+v", hookType, ctx.Data)
		}
		if ctx.Result != nil {
			logger.Debug("[%s] Result: %+v", hookType, ctx.Result)
		}
		return nil
	}
}

// ExampleValidationHook validates data before create/update operations
func ExampleValidationHook(ctx *HookContext) error {
	// Example: Ensure certain fields are present
	if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
		// Check for required fields
		requiredFields := []string{"name"} // Add your required fields here
		for _, field := range requiredFields {
			if _, exists := dataMap[field]; !exists {
				return fmt.Errorf("required field missing: %s", field)
			}
		}
	}
	return nil
}

// ExampleAuthorizationHook checks if the user has permission to perform the operation
func ExampleAuthorizationHook(ctx *HookContext) error {
	// Example: Check user permissions from context
	// userID, ok := ctx.Context.Value("user_id").(string)
	// if !ok {
	// 	return fmt.Errorf("unauthorized: no user in context")
	// }

	// You can access the handler's database or registry if needed
	// For example, to check permissions in the database:
	// query := ctx.Handler.db.NewSelect().Table("permissions")...

	// Add your authorization logic here
	logger.Debug("Authorization check for %s.%s", ctx.Schema, ctx.Entity)
	return nil
}

// ExampleDataTransformHook modifies data before create/update
func ExampleDataTransformHook(ctx *HookContext) error {
	if dataMap, ok := ctx.Data.(map[string]interface{}); ok {
		// Example: Add a timestamp or user ID
		// dataMap["updated_at"] = time.Now()
		// dataMap["updated_by"] = ctx.Context.Value("user_id")

		// Update the context with modified data
		ctx.Data = dataMap
		logger.Debug("Data transformed for %s.%s", ctx.Schema, ctx.Entity)
	}
	return nil
}

// ExampleAuditLogHook creates audit log entries for operations
func ExampleAuditLogHook(hookType HookType) HookFunc {
	return func(ctx *HookContext) error {
		// Example: Log to audit system
		auditEntry := map[string]interface{}{
			"operation":  hookType,
			"schema":     ctx.Schema,
			"entity":     ctx.Entity,
			"table_name": ctx.TableName,
			"id":         ctx.ID,
		}

		if ctx.Error != nil {
			auditEntry["error"] = ctx.Error.Error()
		}

		logger.Info("Audit log: %+v", auditEntry)

		// In a real application, you would save this to a database using the handler
		// Example:
		// query := ctx.Handler.db.NewInsert().Table("audit_logs").Model(&auditEntry)
		// if _, err := query.Exec(ctx.Context); err != nil {
		// 	logger.Error("Failed to save audit log: %v", err)
		// }

		return nil
	}
}

// ExampleCacheInvalidationHook invalidates cache after create/update/delete
func ExampleCacheInvalidationHook(ctx *HookContext) error {
	// Example: Invalidate cache for the entity
	cacheKey := fmt.Sprintf("%s.%s", ctx.Schema, ctx.Entity)
	logger.Info("Invalidating cache for: %s", cacheKey)

	// Add your cache invalidation logic here
	// cache.Delete(cacheKey)

	return nil
}

// ExampleFilterSensitiveDataHook removes sensitive data from responses
func ExampleFilterSensitiveDataHook(ctx *HookContext) error {
	// Example: Remove password fields from results
	// This would be called in AfterRead hooks
	logger.Debug("Filtering sensitive data for %s.%s", ctx.Schema, ctx.Entity)

	// Add your data filtering logic here
	// You would iterate through ctx.Result and remove sensitive fields

	return nil
}

// ExampleRelatedDataHook fetches related data using the handler's database
func ExampleRelatedDataHook(ctx *HookContext) error {
	// Example: Fetch related data after reading the main entity
	// This hook demonstrates using ctx.Handler to access the database

	if ctx.Entity == "users" && ctx.Result != nil {
		// Example: Fetch user's recent activity
		// userID := ... extract from ctx.Result

		// Use the handler's database to query related data
		// query := ctx.Handler.db.NewSelect().Table("user_activity").Where("user_id = ?", userID)
		// var activities []Activity
		// if err := query.Scan(ctx.Context, &activities); err != nil {
		// 	logger.Error("Failed to fetch user activities: %v", err)
		// 	return err
		// }

		// Optionally modify the result to include the related data
		// if resultMap, ok := ctx.Result.(map[string]interface{}); ok {
		// 	resultMap["recent_activities"] = activities
		// }

		logger.Debug("Fetched related data for user entity")
	}

	return nil
}

// ExampleTxHook demonstrates using the Tx field to execute additional SQL queries
// The Tx field provides access to the database/transaction for custom queries
func ExampleTxHook(ctx *HookContext) error {
	// Example: Execute additional SQL operations alongside the main query
	// This is useful for maintaining data consistency, updating related records, etc.

	if ctx.Entity == "orders" && ctx.Data != nil {
		// Example: Update inventory when an order is created
		// Extract product ID and quantity from the order data
		// dataMap, ok := ctx.Data.(map[string]interface{})
		// if !ok {
		// 	return fmt.Errorf("invalid data format")
		// }
		// productID := dataMap["product_id"]
		// quantity := dataMap["quantity"]

		// Use ctx.Tx to execute additional SQL queries
		// The Tx field contains the same database/transaction as the main operation
		// If inside a transaction, your queries will be part of the same transaction
		// query := ctx.Tx.NewUpdate().
		// 	Table("inventory").
		// 	Set("quantity = quantity - ?", quantity).
		// 	Where("product_id = ?", productID)
		//
		// if _, err := query.Exec(ctx.Context); err != nil {
		// 	logger.Error("Failed to update inventory: %v", err)
		// 	return fmt.Errorf("failed to update inventory: %w", err)
		// }

		// You can also execute raw SQL using ctx.Tx
		// var result []map[string]interface{}
		// err := ctx.Tx.Query(ctx.Context, &result,
		// 	"INSERT INTO order_history (order_id, status) VALUES (?, ?)",
		// 	orderID, "pending")
		// if err != nil {
		// 	return fmt.Errorf("failed to insert order history: %w", err)
		// }

		logger.Debug("Executed additional SQL for order entity")
	}

	return nil
}

// SetupExampleHooks demonstrates how to register hooks on a handler
func SetupExampleHooks(handler *Handler) {
	hooks := handler.Hooks()

	// Register logging hooks for all operations
	hooks.Register(BeforeRead, ExampleLoggingHook(BeforeRead))
	hooks.Register(AfterRead, ExampleLoggingHook(AfterRead))
	hooks.Register(BeforeCreate, ExampleLoggingHook(BeforeCreate))
	hooks.Register(AfterCreate, ExampleLoggingHook(AfterCreate))
	hooks.Register(BeforeUpdate, ExampleLoggingHook(BeforeUpdate))
	hooks.Register(AfterUpdate, ExampleLoggingHook(AfterUpdate))
	hooks.Register(BeforeDelete, ExampleLoggingHook(BeforeDelete))
	hooks.Register(AfterDelete, ExampleLoggingHook(AfterDelete))

	// Register validation hooks for create/update
	hooks.Register(BeforeCreate, ExampleValidationHook)
	hooks.Register(BeforeUpdate, ExampleValidationHook)

	// Register authorization hooks for all operations
	hooks.RegisterMultiple([]HookType{
		BeforeRead, BeforeCreate, BeforeUpdate, BeforeDelete,
	}, ExampleAuthorizationHook)

	// Register data transform hook for create/update
	hooks.Register(BeforeCreate, ExampleDataTransformHook)
	hooks.Register(BeforeUpdate, ExampleDataTransformHook)

	// Register audit log hooks for after operations
	hooks.Register(AfterCreate, ExampleAuditLogHook(AfterCreate))
	hooks.Register(AfterUpdate, ExampleAuditLogHook(AfterUpdate))
	hooks.Register(AfterDelete, ExampleAuditLogHook(AfterDelete))

	// Register cache invalidation for after operations
	hooks.Register(AfterCreate, ExampleCacheInvalidationHook)
	hooks.Register(AfterUpdate, ExampleCacheInvalidationHook)
	hooks.Register(AfterDelete, ExampleCacheInvalidationHook)

	// Register sensitive data filtering for read operations
	hooks.Register(AfterRead, ExampleFilterSensitiveDataHook)

	// Register related data fetching for read operations
	hooks.Register(AfterRead, ExampleRelatedDataHook)

	logger.Info("Example hooks registered successfully")
}
