package security

import (
	"fmt"
	"reflect"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

// RegisterSecurityHooks registers all security-related hooks with the handler
func RegisterSecurityHooks(handler *restheadspec.Handler, securityList *SecurityList) {

	// Hook 1: BeforeRead - Load security rules
	handler.Hooks().Register(restheadspec.BeforeRead, func(hookCtx *restheadspec.HookContext) error {
		return LoadSecurityRules(hookCtx, securityList)
	})

	// Hook 2: BeforeScan - Apply row-level security filters
	handler.Hooks().Register(restheadspec.BeforeScan, func(hookCtx *restheadspec.HookContext) error {
		return ApplyRowSecurity(hookCtx, securityList)
	})

	// Hook 3: AfterRead - Apply column-level security (masking)
	handler.Hooks().Register(restheadspec.AfterRead, func(hookCtx *restheadspec.HookContext) error {
		return ApplyColumnSecurity(hookCtx, securityList)
	})

	// Hook 4 (Optional): Audit logging
	handler.Hooks().Register(restheadspec.AfterRead, LogDataAccess)
}

// LoadSecurityRules loads security configuration for the user and entity
func LoadSecurityRules(hookCtx *restheadspec.HookContext, securityList *SecurityList) error {
	// Extract user ID from context
	userID, ok := GetUserID(hookCtx.Context)
	if !ok {
		logger.Warn("No user ID in context for security check")
		return fmt.Errorf("authentication required")
	}

	schema := hookCtx.Schema
	tablename := hookCtx.Entity

	logger.Debug("Loading security rules for user=%d, schema=%s, table=%s", userID, schema, tablename)

	// Load column security rules using the provider
	err := securityList.LoadColumnSecurity(hookCtx.Context, userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load column security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}

	// Load row security rules using the provider
	_, err = securityList.LoadRowSecurity(hookCtx.Context, userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load row security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}

	return nil
}

// ApplyRowSecurity applies row-level security filters to the query
func ApplyRowSecurity(hookCtx *restheadspec.HookContext, securityList *SecurityList) error {
	userID, ok := GetUserID(hookCtx.Context)
	if !ok {
		return nil // No user context, skip
	}

	schema := hookCtx.Schema
	tablename := hookCtx.Entity

	// Get row security template
	rowSec, err := securityList.GetRowSecurityTemplate(userID, schema, tablename)
	if err != nil {
		// No row security defined, allow query to proceed
		logger.Debug("No row security for %s.%s@%d: %v", schema, tablename, userID, err)
		return nil
	}

	// Check if user has a blocking rule
	if rowSec.HasBlock {
		logger.Warn("User %d blocked from accessing %s.%s", userID, schema, tablename)
		return fmt.Errorf("access denied to %s", tablename)
	}

	// If there's a security template, apply it as a WHERE clause
	if rowSec.Template != "" {
		// Get primary key name from model
		modelType := reflect.TypeOf(hookCtx.Model)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}

		// Find primary key field
		pkName := "id" // default
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			if tag := field.Tag.Get("bun"); tag != "" {
				// Check for primary key tag
				if contains(tag, "pk") || contains(tag, "primary_key") {
					if sqlName := extractSQLName(tag); sqlName != "" {
						pkName = sqlName
					}
					break
				}
			}
		}

		// Generate the WHERE clause from template
		whereClause := rowSec.GetTemplate(pkName, modelType)

		logger.Info("Applying row security filter for user %d on %s.%s: %s",
			userID, schema, tablename, whereClause)

		// Apply the WHERE clause to the query
		// The query is in hookCtx.Query
		if selectQuery, ok := hookCtx.Query.(interface {
			Where(string, ...interface{}) interface{}
		}); ok {
			hookCtx.Query = selectQuery.Where(whereClause)
		} else {
			logger.Error("Unable to apply WHERE clause - query doesn't support Where method")
		}
	}

	return nil
}

// ApplyColumnSecurity applies column-level security (masking/hiding) to results
func ApplyColumnSecurity(hookCtx *restheadspec.HookContext, securityList *SecurityList) error {
	userID, ok := GetUserID(hookCtx.Context)
	if !ok {
		return nil // No user context, skip
	}

	schema := hookCtx.Schema
	tablename := hookCtx.Entity

	// Get result data
	result := hookCtx.Result
	if result == nil {
		return nil
	}

	logger.Debug("Applying column security for user=%d, schema=%s, table=%s", userID, schema, tablename)

	// Get model type
	modelType := reflect.TypeOf(hookCtx.Model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// Apply column security masking
	resultValue := reflect.ValueOf(result)
	if resultValue.Kind() == reflect.Ptr {
		resultValue = resultValue.Elem()
	}

	maskedResult, err := securityList.ApplyColumnSecurity(resultValue, modelType, userID, schema, tablename)
	if err != nil {
		logger.Warn("Column security error: %v", err)
		// Don't fail the request, just log the issue
		return nil
	}

	// Update the result with masked data
	if maskedResult.IsValid() && maskedResult.CanInterface() {
		hookCtx.Result = maskedResult.Interface()
	}

	return nil
}

// LogDataAccess logs all data access for audit purposes
func LogDataAccess(hookCtx *restheadspec.HookContext) error {
	userID, _ := GetUserID(hookCtx.Context)

	logger.Info("AUDIT: User %d accessed %s.%s with filters: %+v",
		userID,
		hookCtx.Schema,
		hookCtx.Entity,
		hookCtx.Options.Filters,
	)

	// TODO: Write to audit log table or external audit service
	// auditLog := AuditLog{
	//     UserID:    userID,
	//     Schema:    hookCtx.Schema,
	//     Entity:    hookCtx.Entity,
	//     Action:    "READ",
	//     Timestamp: time.Now(),
	//     Filters:   hookCtx.Options.Filters,
	// }
	// db.Create(&auditLog)

	return nil
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && s[len(s)-len(substr):] == substr
}

func extractSQLName(tag string) string {
	// Simple parser for "column:name" or just "name"
	// This is a simplified version
	parts := splitTag(tag, ',')
	for _, part := range parts {
		if part != "" && !contains(part, ":") {
			return part
		}
		if contains(part, "column:") {
			return part[7:] // Skip "column:"
		}
	}
	return ""
}

func splitTag(tag string, sep rune) []string {
	var parts []string
	var current string
	for _, ch := range tag {
		if ch == sep {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
