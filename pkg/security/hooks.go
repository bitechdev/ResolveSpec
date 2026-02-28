package security

import (
	"context"
	"fmt"
	"reflect"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// SecurityContext is a generic interface that any spec can implement to integrate with security features
// This interface abstracts the common security context needs across different specs
type SecurityContext interface {
	GetContext() context.Context
	GetUserID() (int, bool)
	GetSchema() string
	GetEntity() string
	GetModel() interface{}
	GetQuery() interface{}
	SetQuery(interface{})
	GetResult() interface{}
	SetResult(interface{})
}

// loadSecurityRules loads security configuration for the user and entity (generic version)
func loadSecurityRules(secCtx SecurityContext, securityList *SecurityList) error {
	// Extract user ID from context
	userID, ok := secCtx.GetUserID()
	if !ok {
		logger.Warn("No user ID in context for security check")
		return nil
	}

	schema := secCtx.GetSchema()
	tablename := secCtx.GetEntity()

	logger.Debug("Loading security rules for user=%d, schema=%s, table=%s", userID, schema, tablename)

	// Load column security rules using the provider
	err := securityList.LoadColumnSecurity(secCtx.GetContext(), userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load column security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}

	// Load row security rules using the provider
	_, err = securityList.LoadRowSecurity(secCtx.GetContext(), userID, schema, tablename, false)
	if err != nil {
		logger.Warn("Failed to load row security: %v", err)
		// Don't fail the request if no security rules exist
		// return err
	}

	return nil
}

// applyRowSecurity applies row-level security filters to the query (generic version)
func applyRowSecurity(secCtx SecurityContext, securityList *SecurityList) error {
	userID, ok := secCtx.GetUserID()
	if !ok {
		return nil // No user context, skip
	}

	schema := secCtx.GetSchema()
	tablename := secCtx.GetEntity()

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
		model := secCtx.GetModel()
		if model == nil {
			logger.Debug("No model available for row security on %s.%s", schema, tablename)
			return nil
		}

		// Get primary key name from model
		modelType := reflect.TypeOf(model)
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
		query := secCtx.GetQuery()
		if selectQuery, ok := query.(interface {
			Where(string, ...interface{}) interface{}
		}); ok {
			secCtx.SetQuery(selectQuery.Where(whereClause))
		} else {
			logger.Debug("Query doesn't support Where method, skipping row security")
		}
	}

	return nil
}

// applyColumnSecurity applies column-level security (masking/hiding) to results (generic version)
func applyColumnSecurity(secCtx SecurityContext, securityList *SecurityList) error {
	userID, ok := secCtx.GetUserID()
	if !ok {
		return nil // No user context, skip
	}

	schema := secCtx.GetSchema()
	tablename := secCtx.GetEntity()

	// Get result data
	result := secCtx.GetResult()
	if result == nil {
		return nil
	}

	logger.Debug("Applying column security for user=%d, schema=%s, table=%s", userID, schema, tablename)

	model := secCtx.GetModel()
	if model == nil {
		logger.Debug("No model available for column security on %s.%s", schema, tablename)
		return nil
	}

	// Get model type
	modelType := reflect.TypeOf(model)
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
		secCtx.SetResult(maskedResult.Interface())
	}

	return nil
}

// logDataAccess logs all data access for audit purposes (generic version)
func logDataAccess(secCtx SecurityContext) error {
	userID, _ := secCtx.GetUserID()

	logger.Info("AUDIT: User %d accessed %s.%s",
		userID,
		secCtx.GetSchema(),
		secCtx.GetEntity(),
	)

	// TODO: Write to audit log table or external audit service
	// auditLog := AuditLog{
	//     UserID:    userID,
	//     Schema:    secCtx.GetSchema(),
	//     Entity:    secCtx.GetEntity(),
	//     Action:    "READ",
	//     Timestamp: time.Now(),
	// }
	// db.Create(&auditLog)

	return nil
}

// LogDataAccess is a public wrapper for logDataAccess that accepts a SecurityContext
// This allows other packages to use the audit logging functionality
func LogDataAccess(secCtx SecurityContext) error {
	return logDataAccess(secCtx)
}

// LoadSecurityRules is a public wrapper for loadSecurityRules that accepts a SecurityContext
// This allows other packages to load security rules using the generic interface
func LoadSecurityRules(secCtx SecurityContext, securityList *SecurityList) error {
	return loadSecurityRules(secCtx, securityList)
}

// ApplyRowSecurity is a public wrapper for applyRowSecurity that accepts a SecurityContext
// This allows other packages to apply row-level security using the generic interface
func ApplyRowSecurity(secCtx SecurityContext, securityList *SecurityList) error {
	return applyRowSecurity(secCtx, securityList)
}

// ApplyColumnSecurity is a public wrapper for applyColumnSecurity that accepts a SecurityContext
// This allows other packages to apply column-level security using the generic interface
func ApplyColumnSecurity(secCtx SecurityContext, securityList *SecurityList) error {
	return applyColumnSecurity(secCtx, securityList)
}

// checkModelUpdateAllowed returns an error if CanUpdate is false for the model.
// Rules are read from context (set by NewModelAuthMiddleware) with a fallback to the model registry.
func checkModelUpdateAllowed(secCtx SecurityContext) error {
	rules, ok := GetModelRulesFromContext(secCtx.GetContext())
	if !ok {
		schema := secCtx.GetSchema()
		entity := secCtx.GetEntity()
		var err error
		if schema != "" {
			rules, err = modelregistry.GetModelRulesByName(fmt.Sprintf("%s.%s", schema, entity))
		}
		if err != nil || schema == "" {
			rules, err = modelregistry.GetModelRulesByName(entity)
		}
		if err != nil {
			return nil // model not registered, allow by default
		}
	}
	if !rules.CanUpdate {
		return fmt.Errorf("update not allowed for %s", secCtx.GetEntity())
	}
	return nil
}

// checkModelDeleteAllowed returns an error if CanDelete is false for the model.
// Rules are read from context (set by NewModelAuthMiddleware) with a fallback to the model registry.
func checkModelDeleteAllowed(secCtx SecurityContext) error {
	rules, ok := GetModelRulesFromContext(secCtx.GetContext())
	if !ok {
		schema := secCtx.GetSchema()
		entity := secCtx.GetEntity()
		var err error
		if schema != "" {
			rules, err = modelregistry.GetModelRulesByName(fmt.Sprintf("%s.%s", schema, entity))
		}
		if err != nil || schema == "" {
			rules, err = modelregistry.GetModelRulesByName(entity)
		}
		if err != nil {
			return nil // model not registered, allow by default
		}
	}
	if !rules.CanDelete {
		return fmt.Errorf("delete not allowed for %s", secCtx.GetEntity())
	}
	return nil
}

// CheckModelUpdateAllowed is the public wrapper for checkModelUpdateAllowed.
func CheckModelUpdateAllowed(secCtx SecurityContext) error {
	return checkModelUpdateAllowed(secCtx)
}

// CheckModelDeleteAllowed is the public wrapper for checkModelDeleteAllowed.
func CheckModelDeleteAllowed(secCtx SecurityContext) error {
	return checkModelDeleteAllowed(secCtx)
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
