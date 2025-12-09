package common

import (
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// ValidateAndFixPreloadWhere validates and normalizes WHERE clauses for preloads
//
// NOTE: For preload queries, table aliases from the parent query are not valid since
// the preload executes as a separate query with its own table alias. This function
// now simply validates basic syntax without requiring or adding prefixes.
// The actual alias normalization happens in the database adapter layer.
//
// Returns the WHERE clause and an error if it contains obviously invalid syntax.
func ValidateAndFixPreloadWhere(where string, relationName string) (string, error) {
	if where == "" {
		return where, nil
	}

	where = strings.TrimSpace(where)

	// Just do basic validation - don't require or add prefixes
	// The database adapter will handle alias normalization

	// Check if the WHERE clause contains any qualified column references
	// If it does, log a debug message but don't fail - let the adapter handle it
	if strings.Contains(where, ".") {
		logger.Debug("Preload WHERE clause for '%s' contains qualified column references: '%s'. "+
			"Note: In preload context, table aliases from parent query are not available. "+
			"The database adapter will normalize aliases automatically.", relationName, where)
	}

	// Validate that it's not empty or just whitespace
	if where == "" {
		return where, nil
	}

	// Return the WHERE clause as-is
	// The BunSelectQuery.Where() method will handle alias normalization via normalizeTableAlias()
	return where, nil
}

// IsSQLExpression checks if a condition is a SQL expression that shouldn't be prefixed
func IsSQLExpression(cond string) bool {
	// Common SQL literals and expressions
	sqlLiterals := []string{"true", "false", "null", "1=1", "1 = 1", "0=0", "0 = 0"}
	for _, literal := range sqlLiterals {
		if cond == literal {
			return true
		}
	}
	return false
}

// IsTrivialCondition checks if a condition is trivial and always evaluates to true
// These conditions should be removed from WHERE clauses as they have no filtering effect
func IsTrivialCondition(cond string) bool {
	cond = strings.TrimSpace(cond)
	lowerCond := strings.ToLower(cond)

	// Conditions that always evaluate to true
	trivialConditions := []string{
		"1=1", "1 = 1", "1= 1", "1 =1",
		"true", "true = true", "true=true", "true= true", "true =true",
		"0=0", "0 = 0", "0= 0", "0 =0",
	}

	for _, trivial := range trivialConditions {
		if lowerCond == trivial {
			return true
		}
	}

	return false
}

// SanitizeWhereClause removes trivial conditions and optionally prefixes table/relation names to columns
// This function should be used everywhere a WHERE statement is sent to ensure clean, efficient SQL
//
// Parameters:
//   - where: The WHERE clause string to sanitize
//   - tableName: Optional table/relation name to prefix to column references (empty string to skip prefixing)
//
// Returns:
//   - The sanitized WHERE clause with trivial conditions removed and columns optionally prefixed
//   - An empty string if all conditions were trivial or the input was empty
func SanitizeWhereClause(where string, tableName string) string {
	if where == "" {
		return ""
	}

	where = strings.TrimSpace(where)

	// Strip outer parentheses and re-trim
	where = stripOuterParentheses(where)

	// Get valid columns from the model if tableName is provided
	var validColumns map[string]bool
	if tableName != "" {
		validColumns = getValidColumnsForTable(tableName)
	}

	// Split by AND to handle multiple conditions
	conditions := splitByAND(where)

	validConditions := make([]string, 0, len(conditions))

	for _, cond := range conditions {
		cond = strings.TrimSpace(cond)
		if cond == "" {
			continue
		}

		// Strip parentheses from the condition before checking
		condToCheck := stripOuterParentheses(cond)

		// Skip trivial conditions that always evaluate to true
		if IsTrivialCondition(condToCheck) {
			logger.Debug("Removing trivial condition: '%s'", cond)
			continue
		}

		// If tableName is provided and the condition doesn't already have a table prefix,
		// attempt to add it
		if tableName != "" && !hasTablePrefix(condToCheck) {
			// Check if this is a SQL expression/literal that shouldn't be prefixed
			if !IsSQLExpression(strings.ToLower(condToCheck)) {
				// Extract the column name and prefix it
				columnName := ExtractColumnName(condToCheck)
				if columnName != "" {
					// Only prefix if this is a valid column in the model
					// If we don't have model info (validColumns is nil), prefix anyway for backward compatibility
					if validColumns == nil || isValidColumn(columnName, validColumns) {
						// Replace in the original condition (without stripped parens)
						cond = strings.Replace(cond, columnName, tableName+"."+columnName, 1)
						logger.Debug("Prefixed column in condition: '%s'", cond)
					} else {
						logger.Debug("Skipping prefix for '%s' - not a valid column in model", columnName)
					}
				}
			}
		}

		validConditions = append(validConditions, cond)
	}

	if len(validConditions) == 0 {
		return ""
	}

	result := strings.Join(validConditions, " AND ")

	if result != where {
		logger.Debug("Sanitized WHERE clause: '%s' -> '%s'", where, result)
	}

	return result
}

// stripOuterParentheses removes matching outer parentheses from a string
// It handles nested parentheses correctly
func stripOuterParentheses(s string) string {
	s = strings.TrimSpace(s)

	for {
		if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
			return s
		}

		// Check if these parentheses match (i.e., they're the outermost pair)
		depth := 0
		matched := false
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i == len(s)-1 {
					matched = true
				} else if depth == 0 {
					// Found a closing paren before the end, so outer parens don't match
					return s
				}
			}
		}

		if !matched {
			return s
		}

		// Strip the outer parentheses and continue
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
}

// splitByAND splits a WHERE clause by AND operators (case-insensitive)
// This is a simple split that doesn't handle nested parentheses or complex expressions
func splitByAND(where string) []string {
	// First try uppercase AND
	conditions := strings.Split(where, " AND ")

	// If we didn't split on uppercase, try lowercase
	if len(conditions) == 1 {
		conditions = strings.Split(where, " and ")
	}

	// If we still didn't split, try mixed case
	if len(conditions) == 1 {
		conditions = strings.Split(where, " And ")
	}

	return conditions
}

// hasTablePrefix checks if a condition already has a table/relation prefix (contains a dot)
func hasTablePrefix(cond string) bool {
	// Look for patterns like "table.column" or "`table`.`column`" or "\"table\".\"column\""
	return strings.Contains(cond, ".")
}

// ExtractColumnName extracts the column name from a WHERE condition
// For example: "status = 'active'" returns "status"
func ExtractColumnName(cond string) string {
	// Common SQL operators
	operators := []string{" = ", " != ", " <> ", " > ", " >= ", " < ", " <= ", " LIKE ", " like ", " IN ", " in ", " IS ", " is "}

	for _, op := range operators {
		if idx := strings.Index(cond, op); idx > 0 {
			columnName := strings.TrimSpace(cond[:idx])
			// Remove quotes if present
			columnName = strings.Trim(columnName, "`\"'")
			return columnName
		}
	}

	// If no operator found, check if it's a simple identifier (for boolean columns)
	parts := strings.Fields(cond)
	if len(parts) > 0 {
		columnName := strings.Trim(parts[0], "`\"'")
		// Check if it's a valid identifier (not a SQL keyword)
		if !IsSQLKeyword(strings.ToLower(columnName)) {
			return columnName
		}
	}

	return ""
}

// IsSQLKeyword checks if a string is a SQL keyword that shouldn't be treated as a column name
func IsSQLKeyword(word string) bool {
	keywords := []string{"select", "from", "where", "and", "or", "not", "in", "is", "null", "true", "false", "like", "between", "exists"}
	for _, kw := range keywords {
		if word == kw {
			return true
		}
	}
	return false
}

// getValidColumnsForTable retrieves the valid SQL columns for a table from the model registry
// Returns a map of column names for fast lookup, or nil if the model is not found
func getValidColumnsForTable(tableName string) map[string]bool {
	// Try to get the model from the registry
	model, err := modelregistry.GetModelByName(tableName)
	if err != nil {
		// Model not found, return nil to indicate we should use fallback behavior
		return nil
	}

	// Get SQL columns from the model
	columns := reflection.GetSQLModelColumns(model)
	if len(columns) == 0 {
		// No columns found, return nil
		return nil
	}

	// Build a map for fast lookup
	columnMap := make(map[string]bool, len(columns))
	for _, col := range columns {
		columnMap[strings.ToLower(col)] = true
	}

	return columnMap
}

// isValidColumn checks if a column name exists in the valid columns map
// Handles case-insensitive comparison
func isValidColumn(columnName string, validColumns map[string]bool) bool {
	if validColumns == nil {
		return true // No model info, assume valid
	}
	return validColumns[strings.ToLower(columnName)]
}
