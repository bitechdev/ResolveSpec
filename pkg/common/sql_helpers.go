package common

import (
	"fmt"
	"regexp"
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

// validateWhereClauseSecurity checks for dangerous SQL statements in WHERE clauses
// Returns an error if any dangerous keywords are found
func validateWhereClauseSecurity(where string) error {
	if where == "" {
		return nil
	}

	lowerWhere := strings.ToLower(where)

	// List of dangerous SQL keywords that should never appear in WHERE clauses
	dangerousKeywords := []string{
		"delete ", "delete\t", "delete\n", "delete;",
		"update ", "update\t", "update\n", "update;",
		"truncate ", "truncate\t", "truncate\n", "truncate;",
		"drop ", "drop\t", "drop\n", "drop;",
		"alter ", "alter\t", "alter\n", "alter;",
		"create ", "create\t", "create\n", "create;",
		"insert ", "insert\t", "insert\n", "insert;",
		"grant ", "grant\t", "grant\n", "grant;",
		"revoke ", "revoke\t", "revoke\n", "revoke;",
		"exec ", "exec\t", "exec\n", "exec;",
		"execute ", "execute\t", "execute\n", "execute;",
		";delete", ";update", ";truncate", ";drop", ";alter", ";create", ";insert",
	}

	for _, keyword := range dangerousKeywords {
		if strings.Contains(lowerWhere, keyword) {
			logger.Error("Dangerous SQL keyword detected in WHERE clause: %s", strings.TrimSpace(keyword))
			return fmt.Errorf("dangerous SQL keyword detected in WHERE clause: %s", strings.TrimSpace(keyword))
		}
	}

	return nil
}

// SanitizeWhereClause removes trivial conditions and fixes incorrect table prefixes
// This function should be used everywhere a WHERE statement is sent to ensure clean, efficient SQL
//
// Parameters:
//   - where: The WHERE clause string to sanitize
//   - tableName: The correct table/relation name to use when fixing incorrect prefixes
//   - options: Optional RequestOptions containing preload relations that should be allowed as valid prefixes
//
// Returns:
//   - The sanitized WHERE clause with trivial conditions removed and incorrect prefixes fixed
//   - An empty string if all conditions were trivial or the input was empty
//
// Note: This function will NOT add prefixes to unprefixed columns. It will only fix
// incorrect prefixes (e.g., wrong_table.column -> correct_table.column), unless the
// prefix matches a preloaded relation name, in which case it's left unchanged.
//
// IMPORTANT: Outer parentheses are preserved if the clause contains top-level OR operators
// to prevent OR logic from escaping and affecting the entire query incorrectly.
func SanitizeWhereClause(where string, tableName string, options ...*RequestOptions) string {
	if where == "" {
		return ""
	}

	where = strings.TrimSpace(where)

	// Validate that the WHERE clause doesn't contain dangerous SQL statements
	if err := validateWhereClauseSecurity(where); err != nil {
		logger.Debug("Security validation failed for WHERE clause: %v", err)
		return ""
	}

	// Check if the original clause has outer parentheses and contains OR operators
	// If so, we need to preserve the outer parentheses to prevent OR logic from escaping
	hasOuterParens := false
	if len(where) > 0 && where[0] == '(' && where[len(where)-1] == ')' {
		_, hasOuterParens = stripOneMatchingOuterParen(where)
	}

	// Strip outer parentheses and re-trim for processing
	whereWithoutParens := stripOuterParentheses(where)
	shouldPreserveParens := hasOuterParens && containsTopLevelOR(whereWithoutParens)

	// Use the stripped version for processing
	where = whereWithoutParens

	// Get valid columns from the model if tableName is provided
	var validColumns map[string]bool
	if tableName != "" {
		validColumns = getValidColumnsForTable(tableName)
	}

	// Build a set of allowed table prefixes (main table + preloaded relations)
	allowedPrefixes := make(map[string]bool)
	if tableName != "" {
		allowedPrefixes[tableName] = true
	}

	// Add preload relation names as allowed prefixes
	if len(options) > 0 && options[0] != nil {
		for pi := range options[0].Preload {
			if options[0].Preload[pi].Relation != "" {
				allowedPrefixes[options[0].Preload[pi].Relation] = true
				logger.Debug("Added preload relation '%s' as allowed table prefix", options[0].Preload[pi].Relation)
			}
		}

		// Add join aliases as allowed prefixes
		for _, alias := range options[0].JoinAliases {
			if alias != "" {
				allowedPrefixes[alias] = true
				logger.Debug("Added join alias '%s' as allowed table prefix", alias)
			}
		}
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

		// If tableName is provided and the condition HAS a table prefix, check if it's correct
		if tableName != "" && hasTablePrefix(condToCheck) {
			// Extract the current prefix and column name
			currentPrefix, columnName := extractTableAndColumn(condToCheck)

			if currentPrefix != "" && columnName != "" {
				// Check if the prefix is allowed (main table or preload relation)
				if !allowedPrefixes[currentPrefix] {
					// Prefix is not in the allowed list - only fix if it's a valid column in the main table
					if validColumns == nil || isValidColumn(columnName, validColumns) {
						// Replace the incorrect prefix with the correct main table name
						oldRef := currentPrefix + "." + columnName
						newRef := tableName + "." + columnName
						cond = strings.Replace(cond, oldRef, newRef, 1)
						logger.Debug("Fixed incorrect table prefix in condition: '%s' -> '%s'", oldRef, newRef)
					} else {
						logger.Debug("Skipping prefix fix for '%s.%s' - not a valid column in main table (might be preload relation)", currentPrefix, columnName)
					}
				}
			}
		}
		// Note: We no longer add prefixes to unqualified columns here.
		// Use AddTablePrefixToColumns() separately if you need to add prefixes.

		validConditions = append(validConditions, cond)
	}

	if len(validConditions) == 0 {
		return ""
	}

	result := strings.Join(validConditions, " AND ")

	// If the original clause had outer parentheses and contains OR operators,
	// restore the outer parentheses to prevent OR logic from escaping
	if shouldPreserveParens {
		result = "(" + result + ")"
		logger.Debug("Preserved outer parentheses for OR conditions: '%s'", result)
	}

	if result != where && !shouldPreserveParens {
		logger.Debug("Sanitized WHERE clause: '%s' -> '%s'", where, result)
	}

	return result
}

// stripOuterParentheses removes matching outer parentheses from a string
// It handles nested parentheses correctly
func stripOuterParentheses(s string) string {
	s = strings.TrimSpace(s)

	for {
		stripped, wasStripped := stripOneMatchingOuterParen(s)
		if !wasStripped {
			return s
		}
		s = stripped
	}
}

// stripOneOuterParentheses removes only one level of matching outer parentheses from a string
// Unlike stripOuterParentheses, this only strips once, preserving nested parentheses
func stripOneOuterParentheses(s string) string {
	stripped, _ := stripOneMatchingOuterParen(strings.TrimSpace(s))
	return stripped
}

// stripOneMatchingOuterParen is a helper that strips one matching pair of outer parentheses
// Returns the stripped string and a boolean indicating if stripping occurred
func stripOneMatchingOuterParen(s string) (string, bool) {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s, false
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
				return s, false
			}
		}
	}

	if !matched {
		return s, false
	}

	// Strip the outer parentheses
	return strings.TrimSpace(s[1 : len(s)-1]), true
}

// EnsureOuterParentheses ensures that a SQL clause is wrapped in parentheses
// to prevent OR logic from escaping. It checks if the clause already has
// matching outer parentheses and only adds them if they don't exist.
//
// This is particularly important for OR conditions and complex filters where
// the absence of parentheses could cause the logic to escape and affect
// the entire query incorrectly.
//
// Parameters:
//   - clause: The SQL clause to check and potentially wrap
//
// Returns:
//   - The clause with guaranteed outer parentheses, or empty string if input is empty
func EnsureOuterParentheses(clause string) string {
	if clause == "" {
		return ""
	}

	clause = strings.TrimSpace(clause)
	if clause == "" {
		return ""
	}

	// Check if the clause already has matching outer parentheses
	_, hasOuterParens := stripOneMatchingOuterParen(clause)

	// If it already has matching outer parentheses, return as-is
	if hasOuterParens {
		return clause
	}

	// Otherwise, wrap it in parentheses
	return "(" + clause + ")"
}

// containsTopLevelOR checks if a SQL clause contains OR operators at the top level
// (i.e., not inside parentheses or subqueries). This is used to determine if
// outer parentheses should be preserved to prevent OR logic from escaping.
func containsTopLevelOR(clause string) bool {
	if clause == "" {
		return false
	}

	depth := 0
	inSingleQuote := false
	inDoubleQuote := false
	lowerClause := strings.ToLower(clause)

	for i := 0; i < len(clause); i++ {
		ch := clause[i]

		// Track quote state
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}

		// Skip if inside quotes
		if inSingleQuote || inDoubleQuote {
			continue
		}

		// Track parenthesis depth
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		}

		// Only check for OR at depth 0 (not inside parentheses)
		if depth == 0 && i+4 <= len(clause) {
			// Check for " OR " (case-insensitive)
			substring := lowerClause[i : i+4]
			if substring == " or " {
				return true
			}
		}
	}

	return false
}

// splitByAND splits a WHERE clause by AND operators (case-insensitive)
// This is parenthesis-aware and won't split on AND operators inside subqueries
func splitByAND(where string) []string {
	conditions := []string{}
	currentCondition := strings.Builder{}
	depth := 0 // Track parenthesis depth
	i := 0

	for i < len(where) {
		ch := where[i]

		// Track parenthesis depth
		if ch == '(' {
			depth++
			currentCondition.WriteByte(ch)
			i++
			continue
		} else if ch == ')' {
			depth--
			currentCondition.WriteByte(ch)
			i++
			continue
		}

		// Only look for AND operators at depth 0 (not inside parentheses)
		if depth == 0 {
			// Check if we're at an AND operator (case-insensitive)
			// We need at least " AND " (5 chars) or " and " (5 chars)
			if i+5 <= len(where) {
				substring := where[i : i+5]
				lowerSubstring := strings.ToLower(substring)

				if lowerSubstring == " and " {
					// Found an AND operator at the top level
					// Add the current condition to the list
					conditions = append(conditions, currentCondition.String())
					currentCondition.Reset()
					// Skip past the AND operator
					i += 5
					continue
				}
			}
		}

		// Not an AND operator or we're inside parentheses, just add the character
		currentCondition.WriteByte(ch)
		i++
	}

	// Add the last condition
	if currentCondition.Len() > 0 {
		conditions = append(conditions, currentCondition.String())
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

// extractTableAndColumn extracts the table prefix and column name from a qualified reference
// For example: "users.status = 'active'" returns ("users", "status")
// Returns empty strings if no table prefix is found
// This function is parenthesis-aware and will only look for operators outside of subqueries
func extractTableAndColumn(cond string) (table string, column string) {
	// Common SQL operators to find the column reference
	operators := []string{" = ", " != ", " <> ", " > ", " >= ", " < ", " <= ", " LIKE ", " like ", " IN ", " in ", " IS ", " is "}

	var columnRef string

	// Find the column reference (left side of the operator)
	// We need to find the first operator that appears OUTSIDE of parentheses
	minIdx := -1

	for _, op := range operators {
		idx := findOperatorOutsideParentheses(cond, op)
		if idx > 0 && (minIdx == -1 || idx < minIdx) {
			minIdx = idx
		}
	}

	if minIdx > 0 {
		columnRef = strings.TrimSpace(cond[:minIdx])
	}

	// If no operator found, the whole condition might be the column reference
	if columnRef == "" {
		parts := strings.Fields(cond)
		if len(parts) > 0 {
			columnRef = parts[0]
		}
	}

	if columnRef == "" {
		return "", ""
	}

	// Remove any quotes
	columnRef = strings.Trim(columnRef, "`\"'")

	// Check if there's a function call (contains opening parenthesis)
	openParenIdx := strings.Index(columnRef, "(")

	if openParenIdx >= 0 {
		// There's a function call - find the FIRST dot after the opening paren
		// This handles cases like: ifblnk(users.status, orders.status) - extracts users.status
		dotIdx := strings.Index(columnRef[openParenIdx:], ".")
		if dotIdx > 0 {
			dotIdx += openParenIdx // Adjust to absolute position

			// Extract table name (between paren and dot)
			// Find the last opening paren before this dot
			lastOpenParen := strings.LastIndex(columnRef[:dotIdx], "(")
			table = columnRef[lastOpenParen+1 : dotIdx]

			// Find the column name - it ends at comma, closing paren, whitespace, or end of string
			columnStart := dotIdx + 1
			columnEnd := len(columnRef)

			for i := columnStart; i < len(columnRef); i++ {
				ch := columnRef[i]
				if ch == ',' || ch == ')' || ch == ' ' || ch == '\t' {
					columnEnd = i
					break
				}
			}

			column = columnRef[columnStart:columnEnd]

			// Remove quotes from table and column if present
			table = strings.Trim(table, "`\"'")
			column = strings.Trim(column, "`\"'")

			return table, column
		}
	}

	// No function call - check if it contains a dot (qualified reference)
	// Use LastIndex to handle schema.table.column properly
	if dotIdx := strings.LastIndex(columnRef, "."); dotIdx > 0 {
		table = columnRef[:dotIdx]
		column = columnRef[dotIdx+1:]

		// Remove quotes from table and column if present
		table = strings.Trim(table, "`\"'")
		column = strings.Trim(column, "`\"'")

		return table, column
	}

	return "", ""
}

// Unused: extractUnqualifiedColumnName extracts the column name from an unqualified condition
// For example: "rid_parentmastertaskitem is null" returns "rid_parentmastertaskitem"
// "status = 'active'" returns "status"
// nolint:unused
func extractUnqualifiedColumnName(cond string) string {
	// Common SQL operators
	operators := []string{" = ", " != ", " <> ", " > ", " >= ", " < ", " <= ", " LIKE ", " like ", " IN ", " in ", " IS ", " is ", " NOT ", " not "}

	// Find the column reference (left side of the operator)
	minIdx := -1
	for _, op := range operators {
		idx := strings.Index(cond, op)
		if idx > 0 && (minIdx == -1 || idx < minIdx) {
			minIdx = idx
		}
	}

	var columnRef string
	if minIdx > 0 {
		columnRef = strings.TrimSpace(cond[:minIdx])
	} else {
		// No operator found, might be a single column reference
		parts := strings.Fields(cond)
		if len(parts) > 0 {
			columnRef = parts[0]
		}
	}

	if columnRef == "" {
		return ""
	}

	// Remove any quotes
	columnRef = strings.Trim(columnRef, "`\"'")

	// Return empty if it contains a dot (already qualified) or function call
	if strings.Contains(columnRef, ".") || strings.Contains(columnRef, "(") {
		return ""
	}

	return columnRef
}

// qualifyColumnInCondition replaces an unqualified column name with a qualified one in a condition
// Uses word boundaries to avoid partial matches
// For example: qualifyColumnInCondition("rid_item is null", "rid_item", "table.rid_item")
// returns "table.rid_item is null"
func qualifyColumnInCondition(cond, oldRef, newRef string) string {
	// Use word boundary matching with Go's supported regex syntax
	// \b matches word boundaries
	escapedOld := regexp.QuoteMeta(oldRef)
	pattern := `\b` + escapedOld + `\b`

	re, err := regexp.Compile(pattern)
	if err != nil {
		// If regex fails, fall back to simple string replacement
		logger.Debug("Failed to compile regex for column qualification, using simple replace: %v", err)
		return strings.Replace(cond, oldRef, newRef, 1)
	}

	// Only replace if the match is not preceded by a dot (to avoid replacing already qualified columns)
	result := cond
	matches := re.FindAllStringIndex(cond, -1)

	// Process matches in reverse order to maintain correct indices
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		start := match[0]

		// Check if preceded by a dot (already qualified)
		if start > 0 && cond[start-1] == '.' {
			continue
		}

		// Replace this occurrence
		result = result[:start] + newRef + result[match[1]:]
	}

	return result
}

// findOperatorOutsideParentheses finds the first occurrence of an operator outside of parentheses
// Returns the index of the operator, or -1 if not found or only found inside parentheses
func findOperatorOutsideParentheses(s string, operator string) int {
	depth := 0
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		// Track quote state (operators inside quotes should be ignored)
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}

		// Skip if we're inside quotes
		if inSingleQuote || inDoubleQuote {
			continue
		}

		// Track parenthesis depth
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		}

		// Only look for the operator when we're outside parentheses (depth == 0)
		if depth == 0 {
			// Check if the operator starts at this position
			if i+len(operator) <= len(s) {
				if s[i:i+len(operator)] == operator {
					return i
				}
			}
		}
	}

	return -1
}

// isValidColumn checks if a column name exists in the valid columns map
// Handles case-insensitive comparison
func isValidColumn(columnName string, validColumns map[string]bool) bool {
	if validColumns == nil {
		return true // No model info, assume valid
	}
	return validColumns[strings.ToLower(columnName)]
}

// AddTablePrefixToColumns adds table prefix to unqualified column references in a WHERE clause.
// This function only prefixes simple column references and skips:
//   - Columns already having a table prefix (containing a dot)
//   - Columns inside function calls or expressions (inside parentheses)
//   - Columns inside subqueries
//   - Columns that don't exist in the table (validation via model registry)
//
// Examples:
//   - "status = 'active'" -> "users.status = 'active'" (if status exists in users table)
//   - "COALESCE(status, 'default') = 'active'" -> unchanged (status inside function)
//   - "users.status = 'active'" -> unchanged (already has prefix)
//   - "(status = 'active')" -> "(users.status = 'active')" (grouping parens are OK)
//   - "invalid_col = 'value'" -> unchanged (if invalid_col doesn't exist in table)
//
// Parameters:
//   - where: The WHERE clause to process
//   - tableName: The table name to use as prefix
//
// Returns:
//   - The WHERE clause with table prefixes added to appropriate and valid columns
func AddTablePrefixToColumns(where string, tableName string) string {
	if where == "" || tableName == "" {
		return where
	}

	where = strings.TrimSpace(where)

	// Get valid columns from the model registry for validation
	validColumns := getValidColumnsForTable(tableName)

	// Split by AND to handle multiple conditions (parenthesis-aware)
	conditions := splitByAND(where)
	prefixedConditions := make([]string, 0, len(conditions))

	for _, cond := range conditions {
		cond = strings.TrimSpace(cond)
		if cond == "" {
			continue
		}

		// Process this condition to add table prefix if appropriate
		processedCond := addPrefixToSingleCondition(cond, tableName, validColumns)
		prefixedConditions = append(prefixedConditions, processedCond)
	}

	if len(prefixedConditions) == 0 {
		return ""
	}

	return strings.Join(prefixedConditions, " AND ")
}

// addPrefixToSingleCondition adds table prefix to a single condition if appropriate
// Returns the condition unchanged if:
//   - The condition is a SQL literal/expression (true, false, null, 1=1, etc.)
//   - The column reference is inside a function call
//   - The column already has a table prefix
//   - No valid column reference is found
//   - The column doesn't exist in the table (when validColumns is provided)
func addPrefixToSingleCondition(cond string, tableName string, validColumns map[string]bool) string {
	// Strip one level of outer grouping parentheses to get to the actual condition
	strippedCond := stripOneOuterParentheses(cond)

	// Skip SQL literals and trivial conditions (true, false, null, 1=1, etc.)
	if IsSQLExpression(strippedCond) || IsTrivialCondition(strippedCond) {
		logger.Debug("Skipping SQL literal/trivial condition: '%s'", strippedCond)
		return cond
	}

	// After stripping outer parentheses, check if there are multiple AND-separated conditions
	// at the top level. If so, split and process each separately to avoid incorrectly
	// treating "true AND status" as a single column name.
	subConditions := splitByAND(strippedCond)
	if len(subConditions) > 1 {
		// Multiple conditions found - process each separately
		logger.Debug("Found %d sub-conditions after stripping parentheses, processing separately", len(subConditions))
		processedConditions := make([]string, 0, len(subConditions))
		for _, subCond := range subConditions {
			// Recursively process each sub-condition
			processed := addPrefixToSingleCondition(subCond, tableName, validColumns)
			processedConditions = append(processedConditions, processed)
		}
		result := strings.Join(processedConditions, " AND ")
		// Preserve original outer parentheses if they existed
		if cond != strippedCond {
			result = "(" + result + ")"
		}
		return result
	}

	// If we stripped parentheses and still have more parentheses, recursively process
	if cond != strippedCond && strings.HasPrefix(strippedCond, "(") && strings.HasSuffix(strippedCond, ")") {
		// Recursively handle nested parentheses
		processed := addPrefixToSingleCondition(strippedCond, tableName, validColumns)
		return "(" + processed + ")"
	}

	// Extract the left side of the comparison (before the operator)
	columnRef := extractLeftSideOfComparison(strippedCond)
	if columnRef == "" {
		return cond
	}

	// Skip if it already has a prefix (contains a dot)
	if strings.Contains(columnRef, ".") {
		logger.Debug("Skipping column '%s' - already has table prefix", columnRef)
		return cond
	}

	// Skip if it's a function call or expression (contains parentheses)
	if strings.Contains(columnRef, "(") {
		logger.Debug("Skipping column reference '%s' - inside function or expression", columnRef)
		return cond
	}

	// Validate that the column exists in the table (if we have column info)
	if !isValidColumn(columnRef, validColumns) {
		logger.Debug("Skipping column '%s' - not found in table '%s'", columnRef, tableName)
		return cond
	}

	// It's a simple unqualified column reference that exists in the table - add the table prefix
	newRef := tableName + "." + columnRef
	result := qualifyColumnInCondition(cond, columnRef, newRef)
	logger.Debug("Added table prefix to column: '%s' -> '%s'", columnRef, newRef)

	return result
}

// extractLeftSideOfComparison extracts the left side of a comparison operator from a condition.
// This is used to identify the column reference that may need a table prefix.
//
// Examples:
//   - "status = 'active'" returns "status"
//   - "COALESCE(status, 'default') = 'active'" returns "COALESCE(status, 'default')"
//   - "priority > 5" returns "priority"
//
// Returns empty string if no operator is found.
func extractLeftSideOfComparison(cond string) string {
	operators := []string{" = ", " != ", " <> ", " > ", " >= ", " < ", " <= ", " LIKE ", " like ", " IN ", " in ", " IS ", " is ", " NOT ", " not "}

	// Find the first operator outside of parentheses and quotes
	minIdx := -1
	for _, op := range operators {
		idx := findOperatorOutsideParentheses(cond, op)
		if idx > 0 && (minIdx == -1 || idx < minIdx) {
			minIdx = idx
		}
	}

	if minIdx > 0 {
		leftSide := strings.TrimSpace(cond[:minIdx])
		// Remove any surrounding quotes
		leftSide = strings.Trim(leftSide, "`\"'")
		return leftSide
	}

	// No operator found - might be a boolean column
	parts := strings.Fields(cond)
	if len(parts) > 0 {
		columnRef := strings.Trim(parts[0], "`\"'")
		// Make sure it's not a SQL keyword
		if !IsSQLKeyword(strings.ToLower(columnRef)) {
			return columnRef
		}
	}

	return ""
}
