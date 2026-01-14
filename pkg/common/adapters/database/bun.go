package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// QueryDebugHook is a Bun query hook that logs all SQL queries including preloads
type QueryDebugHook struct{}

func (h *QueryDebugHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

func (h *QueryDebugHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	query := event.Query
	duration := time.Since(event.StartTime)

	if event.Err != nil {
		logger.Error("SQL Query Failed [%s]: %s. Error: %v", duration, query, event.Err)
	} else {
		logger.Debug("SQL Query Success [%s]: %s", duration, query)
	}
}

// debugScanIntoStruct attempts to scan rows into a struct with detailed field-level logging
// This helps identify which specific field is causing scanning issues
func debugScanIntoStruct(rows interface{}, dest interface{}) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}

	v = v.Elem()
	if v.Kind() != reflect.Struct && v.Kind() != reflect.Slice {
		return fmt.Errorf("dest must be pointer to struct or slice")
	}

	// Log the type being scanned into
	typeName := v.Type().String()
	logger.Debug("Debug scan into type: %s (kind: %s)", typeName, v.Kind())

	// Handle slice types - inspect the element type
	var structType reflect.Type
	if v.Kind() == reflect.Slice {
		elemType := v.Type().Elem()
		logger.Debug("  Slice element type: %s", elemType)

		// If slice of pointers, get the underlying type
		if elemType.Kind() == reflect.Ptr {
			structType = elemType.Elem()
		} else {
			structType = elemType
		}
	} else if v.Kind() == reflect.Struct {
		structType = v.Type()
	}

	// If we have a struct type, log all its fields
	if structType != nil && structType.Kind() == reflect.Struct {
		logger.Debug("  Struct %s has %d fields:", structType.Name(), structType.NumField())
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)

			// Log embedded fields specially
			if field.Anonymous {
				logger.Debug("    [%d] EMBEDDED: %s (type: %s, kind: %s, bun:%q)",
					i, field.Name, field.Type, field.Type.Kind(), field.Tag.Get("bun"))
			} else {
				bunTag := field.Tag.Get("bun")
				if bunTag == "" {
					bunTag = "(no tag)"
				}
				logger.Debug("    [%d] %s (type: %s, kind: %s, bun:%q)",
					i, field.Name, field.Type, field.Type.Kind(), bunTag)
			}
		}
	}

	return nil
}

// BunAdapter adapts Bun to work with our Database interface
// This demonstrates how the abstraction works with different ORMs
type BunAdapter struct {
	db *bun.DB
}

// NewBunAdapter creates a new Bun adapter
func NewBunAdapter(db *bun.DB) *BunAdapter {
	return &BunAdapter{db: db}
}

// EnableQueryDebug enables query debugging which logs all SQL queries including preloads
// This is useful for debugging preload queries that may be failing
func (b *BunAdapter) EnableQueryDebug() {
	b.db.AddQueryHook(&QueryDebugHook{})
	logger.Info("Bun query debug mode enabled - all SQL queries will be logged")
}

// EnableDetailedScanDebug enables verbose logging of scan operations
// WARNING: This generates a LOT of log output. Use only for debugging specific issues.
func (b *BunAdapter) EnableDetailedScanDebug() {
	logger.Info("Detailed scan debugging enabled - will log all field scanning operations")
	// This is a flag that can be checked in scan operations
	// Implementation would require modifying the scan logic
}

// DisableQueryDebug removes all query hooks
func (b *BunAdapter) DisableQueryDebug() {
	// Create a new DB without hooks
	// Note: Bun doesn't have a RemoveQueryHook, so we'd need to track hooks manually
	logger.Info("To disable query debug, recreate the BunAdapter without adding the hook")
}

func (b *BunAdapter) NewSelect() common.SelectQuery {
	return &BunSelectQuery{
		query: b.db.NewSelect(),
		db:    b.db,
	}
}

func (b *BunAdapter) NewInsert() common.InsertQuery {
	return &BunInsertQuery{query: b.db.NewInsert()}
}

func (b *BunAdapter) NewUpdate() common.UpdateQuery {
	return &BunUpdateQuery{query: b.db.NewUpdate()}
}

func (b *BunAdapter) NewDelete() common.DeleteQuery {
	return &BunDeleteQuery{query: b.db.NewDelete()}
}

func (b *BunAdapter) Exec(ctx context.Context, query string, args ...interface{}) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunAdapter.Exec", r)
		}
	}()
	result, err := b.db.ExecContext(ctx, query, args...)
	return &BunResult{result: result}, err
}

func (b *BunAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunAdapter.Query", r)
		}
	}()
	return b.db.NewRaw(query, args...).Scan(ctx, dest)
}

func (b *BunAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	// For Bun, we'll return a special wrapper that holds the transaction
	return &BunTxAdapter{tx: tx}, nil
}

func (b *BunAdapter) CommitTx(ctx context.Context) error {
	// For Bun, we need to handle this differently
	// This is a simplified implementation
	return nil
}

func (b *BunAdapter) RollbackTx(ctx context.Context) error {
	// For Bun, we need to handle this differently
	// This is a simplified implementation
	return nil
}

func (b *BunAdapter) RunInTransaction(ctx context.Context, fn func(common.Database) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunAdapter.RunInTransaction", r)
		}
	}()
	return b.db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		// Create adapter with transaction
		adapter := &BunTxAdapter{tx: tx}
		return fn(adapter)
	})
}

func (b *BunAdapter) GetUnderlyingDB() interface{} {
	return b.db
}

// BunSelectQuery implements SelectQuery for Bun
type BunSelectQuery struct {
	query            *bun.SelectQuery
	db               bun.IDB // Store DB connection for count queries
	hasModel         bool    // Track if Model() was called
	schema           string  // Separated schema name
	tableName        string  // Just the table name, without schema
	tableAlias       string
	deferredPreloads []deferredPreload // Preloads to execute as separate queries
	inJoinContext    bool              // Track if we're in a JOIN relation context
	joinTableAlias   string            // Alias to use for JOIN conditions
	skipAutoDetect   bool              // Skip auto-detection to prevent circular calls
}

// deferredPreload represents a preload that will be executed as a separate query
// to avoid PostgreSQL identifier length limits
type deferredPreload struct {
	relation string
	apply    []func(common.SelectQuery) common.SelectQuery
}

func (b *BunSelectQuery) Model(model interface{}) common.SelectQuery {
	b.query = b.query.Model(model)
	b.hasModel = true // Mark that we have a model

	// Try to get table name from model if it implements TableNameProvider
	if provider, ok := model.(common.TableNameProvider); ok {
		fullTableName := provider.TableName()
		// Check if the table name contains schema (e.g., "schema.table")
		b.schema, b.tableName = parseTableName(fullTableName)
	}

	if provider, ok := model.(common.TableAliasProvider); ok {
		b.tableAlias = provider.TableAlias()
	}

	return b
}

func (b *BunSelectQuery) Table(table string) common.SelectQuery {
	b.query = b.query.Table(table)
	// Check if the table name contains schema (e.g., "schema.table")
	b.schema, b.tableName = parseTableName(table)
	return b
}

func (b *BunSelectQuery) Column(columns ...string) common.SelectQuery {
	b.query = b.query.Column(columns...)
	return b
}

func (b *BunSelectQuery) ColumnExpr(query string, args ...interface{}) common.SelectQuery {
	if len(args) > 0 {
		b.query = b.query.ColumnExpr(query, args)
	} else {
		b.query = b.query.ColumnExpr(query)
	}
	return b
}

func (b *BunSelectQuery) Where(query string, args ...interface{}) common.SelectQuery {
	// If we're in a JOIN context, add table prefix to unqualified columns
	if b.inJoinContext && b.joinTableAlias != "" {
		query = addTablePrefix(query, b.joinTableAlias)
	} else if b.tableAlias != "" && b.tableName != "" {
		// If we have a table alias defined, check if the query references a different alias
		// This can happen in preloads where the user expects a certain alias but Bun generates another
		query = normalizeTableAlias(query, b.tableAlias, b.tableName)
	}
	b.query = b.query.Where(query, args...)
	return b
}

// addTablePrefix adds a table prefix to unqualified column references
// This is used in JOIN contexts where conditions must reference the joined table
func addTablePrefix(query, tableAlias string) string {
	if tableAlias == "" || query == "" {
		return query
	}

	// Split on spaces and parentheses to find column references
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '(' || r == ')' || r == ','
	})

	modified := query
	for _, part := range parts {
		// Check if this looks like an unqualified column reference
		// (no dot, and likely a column name before an operator)
		if !strings.Contains(part, ".") {
			// Extract potential column name (before = or other operators)
			for _, op := range []string{"=", "!=", "<>", ">", ">=", "<", "<=", " LIKE ", " IN ", " IS "} {
				if strings.Contains(part, op) {
					colName := strings.Split(part, op)[0]
					colName = strings.TrimSpace(colName)
					if colName != "" && !isOperatorOrKeyword(colName) {
						// Add table prefix
						prefixed := tableAlias + "." + colName + strings.TrimPrefix(part, colName)
						modified = strings.ReplaceAll(modified, part, prefixed)
						logger.Debug("Adding table prefix '%s' to column '%s' in JOIN condition", tableAlias, colName)
					}
					break
				}
			}
		}
	}

	return modified
}

// isOperatorOrKeyword checks if a string is likely an operator or SQL keyword
func isOperatorOrKeyword(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	keywords := []string{"AND", "OR", "NOT", "IN", "IS", "NULL", "TRUE", "FALSE", "LIKE", "BETWEEN"}
	for _, kw := range keywords {
		if s == kw {
			return true
		}
	}
	return false
}

// isAcronymMatch checks if prefix is an acronym of tableName
// For example, "apil" matches "apiproviderlink" because each letter appears in sequence
func isAcronymMatch(prefix, tableName string) bool {
	if len(prefix) == 0 || len(tableName) == 0 {
		return false
	}

	prefixIdx := 0
	for i := 0; i < len(tableName) && prefixIdx < len(prefix); i++ {
		if tableName[i] == prefix[prefixIdx] {
			prefixIdx++
		}
	}

	// All characters of prefix were found in sequence in tableName
	return prefixIdx == len(prefix)
}

// normalizeTableAlias replaces table alias prefixes in SQL conditions
// This handles cases where a user references a table alias that doesn't match
// what Bun generates (common in preload contexts)
func normalizeTableAlias(query, expectedAlias, tableName string) string {
	// Pattern: <word>.<column> where <word> might be an incorrect alias
	// We'll look for patterns like "APIL.column" and either:
	// 1. Remove the alias prefix if it's clearly meant for this table
	// 2. Leave it alone if it might be referring to another table (JOIN/preload)

	// Split on spaces and parentheses to find qualified references
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '(' || r == ')' || r == ','
	})

	modified := query
	for _, part := range parts {
		// Check if this looks like a qualified column reference
		if dotIndex := strings.Index(part, "."); dotIndex > 0 {
			prefix := part[:dotIndex]
			column := part[dotIndex+1:]

			// Check if the prefix matches our expected alias or table name (case-insensitive)
			if strings.EqualFold(prefix, expectedAlias) ||
				strings.EqualFold(prefix, tableName) ||
				strings.EqualFold(prefix, strings.ToLower(tableName)) {
				// Prefix matches current table, it's safe but redundant - leave it
				continue
			}

			// Check if the prefix could plausibly be an alias/acronym for this table
			// Only strip if we're confident it's meant for this table
			// For example: "APIL" could be an acronym for "apiproviderlink"
			prefixLower := strings.ToLower(prefix)
			tableNameLower := strings.ToLower(tableName)

			// Check if prefix is a substring of table name
			isSubstring := strings.Contains(tableNameLower, prefixLower) && len(prefixLower) > 2

			// Check if prefix is an acronym of table name
			// e.g., "APIL" matches "ApiProviderLink" (A-p-I-providerL-ink)
			isAcronym := false
			if !isSubstring && len(prefixLower) > 2 {
				isAcronym = isAcronymMatch(prefixLower, tableNameLower)
			}

			if isSubstring || isAcronym {
				// This looks like it could be an alias for this table - strip it
				logger.Debug("Stripping plausible alias '%s' from WHERE condition, keeping just '%s'", prefix, column)
				// Replace the qualified reference with just the column name
				modified = strings.ReplaceAll(modified, part, column)
			} else {
				// Prefix doesn't match the current table at all
				// It's likely referring to a different table (JOIN/preload)
				// DON'T strip it - leave the qualified reference as-is
				logger.Debug("Keeping qualified reference '%s' - prefix '%s' doesn't match current table '%s'", part, prefix, tableName)
			}
		}
	}

	return modified
}

func (b *BunSelectQuery) WhereOr(query string, args ...interface{}) common.SelectQuery {
	b.query = b.query.WhereOr(query, args...)
	return b
}

func (b *BunSelectQuery) Join(query string, args ...interface{}) common.SelectQuery {
	// Extract optional prefix from args
	// If the last arg is a string that looks like a table prefix, use it
	var prefix string
	sqlArgs := args

	if len(args) > 0 {
		if lastArg, ok := args[len(args)-1].(string); ok && len(lastArg) < 50 && !strings.Contains(lastArg, " ") {
			// Likely a prefix, not a SQL parameter
			prefix = lastArg
			sqlArgs = args[:len(args)-1]
		}
	}

	// If no prefix provided, use the table name as prefix (already separated from schema)
	if prefix == "" && b.tableName != "" {
		prefix = b.tableName
	}

	// If prefix is provided, add it as an alias in the join
	// Bun expects: "JOIN table AS alias ON condition"
	joinClause := query
	if prefix != "" && !strings.Contains(strings.ToUpper(query), " AS ") {
		// If query doesn't already have AS, check if it's a simple table name
		parts := strings.Fields(query)
		if len(parts) > 0 && !strings.HasPrefix(strings.ToUpper(parts[0]), "JOIN") {
			// Simple table name, add prefix: "table AS prefix"
			joinClause = fmt.Sprintf("%s AS %s", parts[0], prefix)
			if len(parts) > 1 {
				// Has ON clause: "table ON ..." becomes "table AS prefix ON ..."
				joinClause += " " + strings.Join(parts[1:], " ")
			}
		}
	}

	b.query = b.query.Join(joinClause, sqlArgs...)
	return b
}

func (b *BunSelectQuery) LeftJoin(query string, args ...interface{}) common.SelectQuery {
	// Extract optional prefix from args
	var prefix string
	sqlArgs := args

	if len(args) > 0 {
		if lastArg, ok := args[len(args)-1].(string); ok && len(lastArg) < 50 && !strings.Contains(lastArg, " ") {
			prefix = lastArg
			sqlArgs = args[:len(args)-1]
		}
	}

	// If no prefix provided, use the table name as prefix (already separated from schema)
	if prefix == "" && b.tableName != "" {
		prefix = b.tableName
	}

	// Construct LEFT JOIN with prefix
	joinClause := query
	if prefix != "" && !strings.Contains(strings.ToUpper(query), " AS ") {
		parts := strings.Fields(query)
		if len(parts) > 0 && !strings.HasPrefix(strings.ToUpper(parts[0]), "LEFT") && !strings.HasPrefix(strings.ToUpper(parts[0]), "JOIN") {
			joinClause = fmt.Sprintf("%s AS %s", parts[0], prefix)
			if len(parts) > 1 {
				joinClause += " " + strings.Join(parts[1:], " ")
			}
		}
	}

	b.query = b.query.Join("LEFT JOIN "+joinClause, sqlArgs...)
	return b
}

func (b *BunSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
	// Bun uses Relation() method for preloading
	// For now, we'll just pass the relation name without conditions
	// TODO: Implement proper condition handling for Bun
	b.query = b.query.Relation(relation)
	return b
}

// // shortenAliasForPostgres shortens a table/relation alias if it would exceed PostgreSQL's 63-char limit
// // when combined with typical column names
// func shortenAliasForPostgres(relationPath string) (string, bool) {
// 	// Convert relation path to the alias format Bun uses: dots become double underscores
// 	// Also convert to lowercase and use snake_case as Bun does
// 	parts := strings.Split(relationPath, ".")
// 	alias := strings.ToLower(strings.Join(parts, "__"))

// 	// PostgreSQL truncates identifiers to 63 chars
// 	// If the alias + typical column name would exceed this, we need to shorten
// 	// Reserve at least 30 chars for column names (e.g., "__rid_mastertype_hubtype")
// 	const maxAliasLength = 30

// 	if len(alias) > maxAliasLength {
// 		// Create a shortened alias using a hash of the original
// 		hash := md5.Sum([]byte(alias))
// 		hashStr := hex.EncodeToString(hash[:])[:8]

// 		// Keep first few chars of original for readability + hash
// 		prefixLen := maxAliasLength - 9 // 9 = 1 underscore + 8 hash chars
// 		if prefixLen > len(alias) {
// 			prefixLen = len(alias)
// 		}

// 		shortened := alias[:prefixLen] + "_" + hashStr
// 		logger.Debug("Shortened alias '%s' (%d chars) to '%s' (%d chars) to avoid PostgreSQL 63-char limit",
// 			alias, len(alias), shortened, len(shortened))
// 		return shortened, true
// 	}

// 	return alias, false
// }

// // estimateColumnAliasLength estimates the length of a column alias in a nested preload
// // Bun creates aliases like: relationChain__columnName
// func estimateColumnAliasLength(relationPath string, columnName string) int {
// 	relationParts := strings.Split(relationPath, ".")
// 	aliasChain := strings.ToLower(strings.Join(relationParts, "__"))
// 	// Bun adds "__" between alias and column name
// 	return len(aliasChain) + 2 + len(columnName)
// }

func (b *BunSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// Auto-detect relationship type and choose optimal loading strategy
	// Get the model from the query if available
	// Skip auto-detection if flag is set (prevents circular calls from JoinRelation)
	if !b.skipAutoDetect {
		model := b.query.GetModel()
		if model != nil && model.Value() != nil {
			relType := reflection.GetRelationType(model.Value(), relation)

			// Log the detected relationship type
			logger.Debug("PreloadRelation '%s' detected as: %s", relation, relType)

			// If this is a belongs-to or has-one relation, use JOIN for better performance
			if relType.ShouldUseJoin() {
				logger.Info("Using JOIN strategy for %s relation '%s'", relType, relation)
				return b.JoinRelation(relation, apply...)
			}

			// For has-many, many-to-many, or unknown: use separate query (safer default)
			if relType == reflection.RelationHasMany || relType == reflection.RelationManyToMany {
				logger.Debug("Using separate query for %s relation '%s'", relType, relation)
			}
		}
	}

	// Check if this relation chain would create problematic long aliases
	relationParts := strings.Split(relation, ".")
	aliasChain := strings.ToLower(strings.Join(relationParts, "__"))

	// PostgreSQL's identifier limit is 63 characters
	const postgresIdentifierLimit = 63
	const safeAliasLimit = 35 // Leave room for column names

	// If the alias chain is too long, defer this preload to be executed as a separate query
	if len(relationParts) > 1 && len(aliasChain) > safeAliasLimit {
		logger.Info("Preload relation '%s' creates long alias chain '%s' (%d chars). "+
			"Using separate query to avoid PostgreSQL %d-char identifier limit.",
			relation, aliasChain, len(aliasChain), postgresIdentifierLimit)

		// For nested preloads (e.g., "Parent.Child"), split into separate preloads
		// This avoids the long concatenated alias
		if len(relationParts) > 1 {
			// Load first level normally: "Parent"
			firstLevel := relationParts[0]
			remainingPath := strings.Join(relationParts[1:], ".")

			logger.Info("Splitting nested preload: loading '%s' first, then '%s' separately",
				firstLevel, remainingPath)

			// Apply the first level preload normally
			b.query = b.query.Relation(firstLevel)

			// Store the remaining nested preload to be executed after the main query
			b.deferredPreloads = append(b.deferredPreloads, deferredPreload{
				relation: relation,
				apply:    apply,
			})

			return b
		}

		// Single level but still too long - just warn and continue
		logger.Warn("Single-level preload '%s' has a very long name (%d chars). "+
			"Consider renaming the field to avoid potential issues.",
			relation, len(aliasChain))
	}

	// Normal preload handling
	b.query = b.query.Relation(relation, func(sq *bun.SelectQuery) *bun.SelectQuery {
		defer func() {
			if r := recover(); r != nil {
				err := logger.HandlePanic("BunSelectQuery.PreloadRelation", r)
				if err != nil {
					return
				}
			}
		}()
		if len(apply) == 0 {
			return sq
		}

		// Wrap the incoming *bun.SelectQuery in our adapter
		wrapper := &BunSelectQuery{
			query: sq,
			db:    b.db,
		}

		// Try to extract table name and alias from the preload model
		if model := sq.GetModel(); model != nil && model.Value() != nil {
			modelValue := model.Value()

			// Extract table name if model implements TableNameProvider
			if provider, ok := modelValue.(common.TableNameProvider); ok {
				fullTableName := provider.TableName()
				wrapper.schema, wrapper.tableName = parseTableName(fullTableName)
			}

			// Extract table alias if model implements TableAliasProvider
			if provider, ok := modelValue.(common.TableAliasProvider); ok {
				wrapper.tableAlias = provider.TableAlias()
				// Apply the alias to the Bun query so conditions can reference it
				if wrapper.tableAlias != "" {
					// Note: Bun's Relation() already sets up the table, but we can add
					// the alias explicitly if needed
					logger.Debug("Preload relation '%s' using table alias: %s", relation, wrapper.tableAlias)
				}
			}
		}

		// Start with the interface value (not pointer)
		current := common.SelectQuery(wrapper)

		// Apply each function in sequence
		for _, fn := range apply {
			if fn != nil {
				// Pass &current (pointer to interface variable), fn modifies and returns new interface value
				modified := fn(current)
				current = modified
			}
		}

		// Extract the final *bun.SelectQuery
		if finalBun, ok := current.(*BunSelectQuery); ok {
			return finalBun.query
		}

		return sq // fallback
	})
	return b
}

func (b *BunSelectQuery) JoinRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// JoinRelation uses a LEFT JOIN instead of a separate query
	// This is more efficient for many-to-one or one-to-one relationships

	logger.Debug("JoinRelation '%s' - Using JOIN strategy with automatic WHERE prefix addition", relation)

	// Wrap the apply functions to automatically add table prefix to WHERE conditions
	wrappedApply := make([]func(common.SelectQuery) common.SelectQuery, 0, len(apply))
	for _, fn := range apply {
		if fn != nil {
			wrappedFn := func(originalFn func(common.SelectQuery) common.SelectQuery) func(common.SelectQuery) common.SelectQuery {
				return func(q common.SelectQuery) common.SelectQuery {
					// Create a special wrapper that adds prefixes to WHERE conditions
					if bunQuery, ok := q.(*BunSelectQuery); ok {
						// Mark this query as being in JOIN context
						bunQuery.inJoinContext = true
						bunQuery.joinTableAlias = strings.ToLower(relation)
					}
					return originalFn(q)
				}
			}(fn)
			wrappedApply = append(wrappedApply, wrappedFn)
		}
	}

	// Use PreloadRelation with the wrapped functions
	// Bun's Relation() will use JOIN for belongs-to and has-one relations
	// CRITICAL: Set skipAutoDetect flag to prevent circular call
	// (PreloadRelation would detect belongs-to and call JoinRelation again)
	b.skipAutoDetect = true
	defer func() { b.skipAutoDetect = false }()
	return b.PreloadRelation(relation, wrappedApply...)
}

func (b *BunSelectQuery) Order(order string) common.SelectQuery {
	b.query = b.query.Order(order)
	return b
}

func (b *BunSelectQuery) OrderExpr(order string, args ...interface{}) common.SelectQuery {
	b.query = b.query.OrderExpr(order, args...)
	return b
}

func (b *BunSelectQuery) Limit(n int) common.SelectQuery {
	b.query = b.query.Limit(n)
	return b
}

func (b *BunSelectQuery) Offset(n int) common.SelectQuery {
	b.query = b.query.Offset(n)
	return b
}

func (b *BunSelectQuery) Group(group string) common.SelectQuery {
	b.query = b.query.Group(group)
	return b
}

func (b *BunSelectQuery) Having(having string, args ...interface{}) common.SelectQuery {
	b.query = b.query.Having(having, args...)
	return b
}

func (b *BunSelectQuery) Scan(ctx context.Context, dest interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunSelectQuery.Scan", r)
		}
	}()
	if dest == nil {
		return fmt.Errorf("destination cannot be nil")
	}

	// Execute the main query first
	err = b.query.Scan(ctx, dest)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunSelectQuery.Scan failed. SQL: %s. Error: %v", sqlStr, err)
		return err
	}

	// Execute any deferred preloads
	if len(b.deferredPreloads) > 0 {
		err = b.executeDeferredPreloads(ctx, dest)
		if err != nil {
			logger.Warn("Failed to execute deferred preloads: %v", err)
			// Don't fail the whole query, just log the warning
		}
		// Clear deferred preloads to prevent re-execution
		b.deferredPreloads = nil
	}

	return nil
}

func (b *BunSelectQuery) ScanModel(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Enhanced panic recovery with model information
			model := b.query.GetModel()
			var modelInfo string
			if model != nil && model.Value() != nil {
				modelValue := model.Value()
				modelInfo = fmt.Sprintf("Model type: %T", modelValue)

				// Try to get the model's underlying struct type
				v := reflect.ValueOf(modelValue)
				if v.Kind() == reflect.Ptr {
					v = v.Elem()
				}
				if v.Kind() == reflect.Slice {
					if v.Type().Elem().Kind() == reflect.Ptr {
						modelInfo += fmt.Sprintf(", Slice of: %s", v.Type().Elem().Elem().Name())
					} else {
						modelInfo += fmt.Sprintf(", Slice of: %s", v.Type().Elem().Name())
					}
				} else if v.Kind() == reflect.Struct {
					modelInfo += fmt.Sprintf(", Struct: %s", v.Type().Name())
				}
			}

			sqlStr := b.query.String()
			logger.Error("Panic in BunSelectQuery.ScanModel: %v. %s. SQL: %s", r, modelInfo, sqlStr)
			err = logger.HandlePanic("BunSelectQuery.ScanModel", r)
		}
	}()
	if b.query.GetModel() == nil {
		return fmt.Errorf("model is nil")
	}

	// Optional: Enable detailed field-level debugging (set to true to debug)
	const enableDetailedDebug = true
	if enableDetailedDebug {
		model := b.query.GetModel()
		if model != nil && model.Value() != nil {
			if err := debugScanIntoStruct(nil, model.Value()); err != nil {
				logger.Warn("Debug scan inspection failed: %v", err)
			}
		}
	}

	// Execute the main query first
	err = b.query.Scan(ctx)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunSelectQuery.ScanModel failed. SQL: %s. Error: %v", sqlStr, err)
		return err
	}

	// Execute any deferred preloads
	if len(b.deferredPreloads) > 0 {
		model := b.query.GetModel()
		err = b.executeDeferredPreloads(ctx, model.Value())
		if err != nil {
			logger.Warn("Failed to execute deferred preloads: %v", err)
			// Don't fail the whole query, just log the warning
		}
		// Clear deferred preloads to prevent re-execution
		b.deferredPreloads = nil
	}

	return nil
}

// executeDeferredPreloads executes preloads that were deferred to avoid PostgreSQL identifier length limits
func (b *BunSelectQuery) executeDeferredPreloads(ctx context.Context, dest interface{}) error {
	if len(b.deferredPreloads) == 0 {
		return nil
	}

	for _, dp := range b.deferredPreloads {
		err := b.executeSingleDeferredPreload(ctx, dest, dp)
		if err != nil {
			return fmt.Errorf("failed to execute deferred preload '%s': %w", dp.relation, err)
		}
	}

	return nil
}

// executeSingleDeferredPreload executes a single deferred preload
// For a relation like "Parent.Child", it:
// 1. Finds all loaded Parent records in dest
// 2. Loads Child records for those Parents using a separate query (loading only "Child", not "Parent.Child")
// 3. Bun automatically assigns the Child records to the appropriate Parent.Child field
func (b *BunSelectQuery) executeSingleDeferredPreload(ctx context.Context, dest interface{}, dp deferredPreload) error {
	relationParts := strings.Split(dp.relation, ".")
	if len(relationParts) < 2 {
		return fmt.Errorf("deferred preload must be nested (e.g., 'Parent.Child'), got: %s", dp.relation)
	}

	// The parent relation that was already loaded
	parentRelation := relationParts[0]
	// The child relation we need to load
	childRelation := strings.Join(relationParts[1:], ".")

	logger.Debug("Executing deferred preload: loading '%s' on already-loaded '%s'", childRelation, parentRelation)

	// Use reflection to access the parent relation field(s) in the loaded records
	// Then load the child relation for those parent records
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() == reflect.Ptr {
		destValue = destValue.Elem()
	}

	// Handle both slice and single record
	if destValue.Kind() == reflect.Slice {
		// Iterate through each record in the slice
		for i := 0; i < destValue.Len(); i++ {
			record := destValue.Index(i)
			if err := b.loadChildRelationForRecord(ctx, record, parentRelation, childRelation, dp.apply); err != nil {
				logger.Warn("Failed to load child relation '%s' for record %d: %v", childRelation, i, err)
				// Continue with other records
			}
		}
	} else {
		// Single record
		if err := b.loadChildRelationForRecord(ctx, destValue, parentRelation, childRelation, dp.apply); err != nil {
			return fmt.Errorf("failed to load child relation '%s': %w", childRelation, err)
		}
	}

	return nil
}

// loadChildRelationForRecord loads a child relation for a single parent record
func (b *BunSelectQuery) loadChildRelationForRecord(ctx context.Context, record reflect.Value, parentRelation, childRelation string, apply []func(common.SelectQuery) common.SelectQuery) error {
	// Ensure we're working with the actual struct value, not a pointer
	if record.Kind() == reflect.Ptr {
		record = record.Elem()
	}

	// Get the parent relation field
	parentField := record.FieldByName(parentRelation)
	if !parentField.IsValid() {
		// Parent relation field doesn't exist
		logger.Debug("Parent relation field '%s' not found in record", parentRelation)
		return nil
	}

	// Check if the parent field is nil (for pointer fields)
	if parentField.Kind() == reflect.Ptr && parentField.IsNil() {
		// Parent relation not loaded or nil, skip
		logger.Debug("Parent relation field '%s' is nil, skipping child preload", parentRelation)
		return nil
	}

	// Get a pointer to the parent field so Bun can modify it
	// CRITICAL: We need to pass a pointer, not a value, so that when Bun
	// loads the child records and appends them to the slice, the changes
	// are reflected in the original struct field.
	var parentPtr interface{}
	if parentField.Kind() == reflect.Ptr {
		// Field is already a pointer (e.g., Parent *Parent), use as-is
		parentPtr = parentField.Interface()
	} else {
		// Field is a value (e.g., Comments []Comment), get its address
		if parentField.CanAddr() {
			parentPtr = parentField.Addr().Interface()
		} else {
			return fmt.Errorf("cannot get address of field '%s'", parentRelation)
		}
	}

	// Load the child relation on the parent record
	// This uses a shorter alias since we're only loading "Child", not "Parent.Child"
	// CRITICAL: Use WherePK() to ensure we only load children for THIS specific parent
	// record, not the first parent in the database table.
	return b.db.NewSelect().
		Model(parentPtr).
		WherePK().
		Relation(childRelation, func(sq *bun.SelectQuery) *bun.SelectQuery {
			// Apply any custom query modifications
			if len(apply) > 0 {
				wrapper := &BunSelectQuery{query: sq, db: b.db}
				current := common.SelectQuery(wrapper)
				for _, fn := range apply {
					if fn != nil {
						current = fn(current)
					}
				}
				if finalBun, ok := current.(*BunSelectQuery); ok {
					return finalBun.query
				}
			}
			return sq
		}).
		Scan(ctx)
}

func (b *BunSelectQuery) Count(ctx context.Context) (count int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunSelectQuery.Count", r)
			count = 0
		}
	}()
	// If Model() was set, use bun's native Count() which works properly
	if b.hasModel {
		count, err := b.query.Count(ctx)
		if err != nil {
			// Log SQL string for debugging
			sqlStr := b.query.String()
			logger.Error("BunSelectQuery.Count failed. SQL: %s. Error: %v", sqlStr, err)
		}
		return count, err
	}

	// Otherwise, wrap as subquery to avoid "Model(nil)" error
	// This is needed when only Table() is set without a model
	countQuery := b.db.NewSelect().
		TableExpr("(?) AS subquery", b.query).
		ColumnExpr("COUNT(*)")
	err = countQuery.Scan(ctx, &count)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := countQuery.String()
		logger.Error("BunSelectQuery.Count (subquery) failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return count, err
}

func (b *BunSelectQuery) Exists(ctx context.Context) (exists bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunSelectQuery.Exists", r)
			exists = false
		}
	}()
	exists, err = b.query.Exists(ctx)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunSelectQuery.Exists failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return exists, err
}

// BunInsertQuery implements InsertQuery for Bun
type BunInsertQuery struct {
	query    *bun.InsertQuery
	values   map[string]interface{}
	hasModel bool
}

func (b *BunInsertQuery) Model(model interface{}) common.InsertQuery {
	b.query = b.query.Model(model)
	b.hasModel = true
	return b
}

func (b *BunInsertQuery) Table(table string) common.InsertQuery {
	if b.hasModel {
		return b
	}
	b.query = b.query.Table(table)
	return b
}

func (b *BunInsertQuery) Value(column string, value interface{}) common.InsertQuery {
	if b.values == nil {
		b.values = make(map[string]interface{})
	}
	b.values[column] = value
	return b
}

func (b *BunInsertQuery) OnConflict(action string) common.InsertQuery {
	b.query = b.query.On(action)
	return b
}

func (b *BunInsertQuery) Returning(columns ...string) common.InsertQuery {
	if len(columns) > 0 {
		b.query = b.query.Returning(columns[0])
	}
	return b
}

func (b *BunInsertQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunInsertQuery.Exec", r)
		}
	}()
	if len(b.values) > 0 {
		if !b.hasModel {
			// If no model was set, use the values map as the model
			// Bun can insert map[string]interface{} directly
			b.query = b.query.Model(&b.values)
		} else {
			// If model was set, use Value() to add individual values
			for k, v := range b.values {
				b.query = b.query.Value(k, "?", v)
			}
		}
	}
	result, err := b.query.Exec(ctx)
	return &BunResult{result: result}, err
}

// BunUpdateQuery implements UpdateQuery for Bun
type BunUpdateQuery struct {
	query *bun.UpdateQuery
	model interface{}
}

func (b *BunUpdateQuery) Model(model interface{}) common.UpdateQuery {
	b.query = b.query.Model(model)
	b.model = model
	return b
}

func (b *BunUpdateQuery) Table(table string) common.UpdateQuery {
	b.query = b.query.Table(table)
	if b.model == nil {
		// Try to get table name from table string if model is not set

		model, err := modelregistry.GetModelByName(table)
		if err == nil {
			b.model = model
		}
	}
	return b
}

func (b *BunUpdateQuery) Set(column string, value interface{}) common.UpdateQuery {
	// Validate column is writable if model is set
	if b.model != nil && !reflection.IsColumnWritable(b.model, column) {
		// Skip scan-only columns
		return b
	}
	b.query = b.query.Set(column+" = ?", value)
	return b
}

func (b *BunUpdateQuery) SetMap(values map[string]interface{}) common.UpdateQuery {
	pkName := reflection.GetPrimaryKeyName(b.model)
	for column, value := range values {
		// Validate column is writable if model is set
		if b.model != nil && !reflection.IsColumnWritable(b.model, column) {
			// Skip scan-only columns
			continue
		}
		if pkName != "" && column == pkName {
			// Skip primary key updates
			continue
		}
		b.query = b.query.Set(column+" = ?", value)
	}
	return b
}

func (b *BunUpdateQuery) Where(query string, args ...interface{}) common.UpdateQuery {
	b.query = b.query.Where(query, args...)
	return b
}

func (b *BunUpdateQuery) Returning(columns ...string) common.UpdateQuery {
	if len(columns) > 0 {
		b.query = b.query.Returning(columns[0])
	}
	return b
}

func (b *BunUpdateQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunUpdateQuery.Exec", r)
		}
	}()
	result, err := b.query.Exec(ctx)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunUpdateQuery.Exec failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return &BunResult{result: result}, err
}

// BunDeleteQuery implements DeleteQuery for Bun
type BunDeleteQuery struct {
	query *bun.DeleteQuery
}

func (b *BunDeleteQuery) Model(model interface{}) common.DeleteQuery {
	b.query = b.query.Model(model)
	return b
}

func (b *BunDeleteQuery) Table(table string) common.DeleteQuery {
	b.query = b.query.Table(table)
	return b
}

func (b *BunDeleteQuery) Where(query string, args ...interface{}) common.DeleteQuery {
	b.query = b.query.Where(query, args...)
	return b
}

func (b *BunDeleteQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunDeleteQuery.Exec", r)
		}
	}()
	result, err := b.query.Exec(ctx)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunDeleteQuery.Exec failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return &BunResult{result: result}, err
}

// BunResult implements Result for Bun
type BunResult struct {
	result sql.Result
}

func (b *BunResult) RowsAffected() int64 {
	if b.result == nil {
		return 0
	}
	rows, _ := b.result.RowsAffected()
	return rows
}

func (b *BunResult) LastInsertId() (int64, error) {
	if b.result == nil {
		return 0, nil
	}
	return b.result.LastInsertId()
}

// BunTxAdapter wraps a Bun transaction to implement the Database interface
type BunTxAdapter struct {
	tx bun.Tx
}

func (b *BunTxAdapter) NewSelect() common.SelectQuery {
	return &BunSelectQuery{
		query: b.tx.NewSelect(),
		db:    b.tx,
	}
}

func (b *BunTxAdapter) NewInsert() common.InsertQuery {
	return &BunInsertQuery{query: b.tx.NewInsert()}
}

func (b *BunTxAdapter) NewUpdate() common.UpdateQuery {
	return &BunUpdateQuery{query: b.tx.NewUpdate()}
}

func (b *BunTxAdapter) NewDelete() common.DeleteQuery {
	return &BunDeleteQuery{query: b.tx.NewDelete()}
}

func (b *BunTxAdapter) Exec(ctx context.Context, query string, args ...interface{}) (common.Result, error) {
	result, err := b.tx.ExecContext(ctx, query, args...)
	return &BunResult{result: result}, err
}

func (b *BunTxAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return b.tx.NewRaw(query, args...).Scan(ctx, dest)
}

func (b *BunTxAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	return nil, fmt.Errorf("nested transactions not supported")
}

func (b *BunTxAdapter) CommitTx(ctx context.Context) error {
	return b.tx.Commit()
}

func (b *BunTxAdapter) RollbackTx(ctx context.Context) error {
	return b.tx.Rollback()
}

func (b *BunTxAdapter) RunInTransaction(ctx context.Context, fn func(common.Database) error) error {
	return fn(b) // Already in transaction
}

func (b *BunTxAdapter) GetUnderlyingDB() interface{} {
	return b.tx
}
