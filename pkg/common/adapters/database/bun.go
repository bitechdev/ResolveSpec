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
	query          *bun.SelectQuery
	db             bun.IDB // Store DB connection for count queries
	hasModel       bool    // Track if Model() was called
	schema         string  // Separated schema name
	tableName      string  // Just the table name, without schema
	tableAlias     string
	inJoinContext  bool                                                     // Track if we're in a JOIN relation context
	joinTableAlias string                                                   // Alias to use for JOIN conditions
	skipAutoDetect bool                                                     // Skip auto-detection to prevent circular calls
	customPreloads map[string][]func(common.SelectQuery) common.SelectQuery // Relations to load with custom implementation
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

func (b *BunSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// Check if this relation will likely cause alias truncation FIRST
	// PostgreSQL has a 63-character limit on identifiers
	willTruncate := checkAliasLength(relation)

	if willTruncate {
		logger.Warn("Preload relation '%s' would generate aliases exceeding PostgreSQL's 63-char limit", relation)
		logger.Info("Using custom preload implementation with separate queries for relation '%s'", relation)

		// Store this relation for custom post-processing after the main query
		// We'll load it manually with separate queries to avoid JOIN aliases
		if b.customPreloads == nil {
			b.customPreloads = make(map[string][]func(common.SelectQuery) common.SelectQuery)
		}
		b.customPreloads[relation] = apply

		// Return without calling Bun's Relation() - we'll handle it ourselves
		return b
	}

	// Auto-detect relationship type and choose optimal loading strategy
	// Skip auto-detection if flag is set (prevents circular calls from JoinRelation)
	if !b.skipAutoDetect {
		model := b.query.GetModel()
		if model != nil && model.Value() != nil {
			relType := reflection.GetRelationType(model.Value(), relation)

			// Log the detected relationship type
			logger.Debug("PreloadRelation '%s' detected as: %s", relation, relType)

			if relType.ShouldUseJoin() {
				// If this is a belongs-to or has-one relation that won't exceed limits, use JOIN for better performance
				logger.Info("Using JOIN strategy for %s relation '%s'", relType, relation)
				return b.JoinRelation(relation, apply...)
			}

			// For has-many, many-to-many, or unknown: use separate query (safer default)
			if relType == reflection.RelationHasMany || relType == reflection.RelationManyToMany {
				logger.Debug("Using separate query for %s relation '%s'", relType, relation)
			}
		}
	}

	// Use Bun's native Relation() for preloading
	// Note: For relations that would cause truncation, skipAutoDetect is set to true
	// to prevent our auto-detection from adding JOIN optimization
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
				logger.Debug("Preload relation '%s' using table alias: %s", relation, wrapper.tableAlias)
			}
		}

		// Start with the interface value (not pointer)
		current := common.SelectQuery(wrapper)

		// Apply each function in sequence
		for _, fn := range apply {
			if fn != nil {
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

// checkIfRelationAlreadyLoaded checks if a relation is already populated on parent records
// Returns the collection of related records if already loaded
func checkIfRelationAlreadyLoaded(parents reflect.Value, relationName string) (reflect.Value, bool) {
	if parents.Len() == 0 {
		return reflect.Value{}, false
	}

	// Get the first parent to check the relation field
	firstParent := parents.Index(0)
	if firstParent.Kind() == reflect.Ptr {
		firstParent = firstParent.Elem()
	}

	// Find the relation field
	relationField := firstParent.FieldByName(relationName)
	if !relationField.IsValid() {
		return reflect.Value{}, false
	}

	// Check if it's a slice (has-many)
	if relationField.Kind() == reflect.Slice {
		// Check if any parent has a non-empty slice
		for i := 0; i < parents.Len(); i++ {
			parent := parents.Index(i)
			if parent.Kind() == reflect.Ptr {
				parent = parent.Elem()
			}
			field := parent.FieldByName(relationName)
			if field.IsValid() && !field.IsNil() && field.Len() > 0 {
				// Already loaded! Collect all related records from all parents
				allRelated := reflect.MakeSlice(field.Type(), 0, field.Len()*parents.Len())
				for j := 0; j < parents.Len(); j++ {
					p := parents.Index(j)
					if p.Kind() == reflect.Ptr {
						p = p.Elem()
					}
					f := p.FieldByName(relationName)
					if f.IsValid() && !f.IsNil() {
						for k := 0; k < f.Len(); k++ {
							allRelated = reflect.Append(allRelated, f.Index(k))
						}
					}
				}
				return allRelated, true
			}
		}
	} else if relationField.Kind() == reflect.Ptr {
		// Check if it's a pointer (has-one/belongs-to)
		if !relationField.IsNil() {
			// Already loaded! Collect all related records from all parents
			relatedType := relationField.Type()
			allRelated := reflect.MakeSlice(reflect.SliceOf(relatedType), 0, parents.Len())
			for j := 0; j < parents.Len(); j++ {
				p := parents.Index(j)
				if p.Kind() == reflect.Ptr {
					p = p.Elem()
				}
				f := p.FieldByName(relationName)
				if f.IsValid() && !f.IsNil() {
					allRelated = reflect.Append(allRelated, f)
				}
			}
			return allRelated, true
		}
	}

	return reflect.Value{}, false
}

// loadCustomPreloads loads relations that would cause alias truncation using separate queries
func (b *BunSelectQuery) loadCustomPreloads(ctx context.Context) error {
	model := b.query.GetModel()
	if model == nil || model.Value() == nil {
		return fmt.Errorf("no model to load preloads for")
	}

	// Get the actual data from the model
	modelValue := reflect.ValueOf(model.Value())
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	// We only handle slices of records for now
	if modelValue.Kind() != reflect.Slice {
		logger.Warn("Custom preloads only support slice models currently, got: %v", modelValue.Kind())
		return nil
	}

	if modelValue.Len() == 0 {
		logger.Debug("No records to load preloads for")
		return nil
	}

	// For each custom preload relation
	for relation, applyFuncs := range b.customPreloads {
		logger.Info("Loading custom preload for relation: %s", relation)

		// Parse the relation path (e.g., "MTL.MAL.DEF" -> ["MTL", "MAL", "DEF"])
		relationParts := strings.Split(relation, ".")

		// Start with the parent records
		currentRecords := modelValue

		// Load each level of the relation
		for i, relationPart := range relationParts {
			isLastPart := i == len(relationParts)-1

			logger.Debug("Loading relation part [%d/%d]: %s", i+1, len(relationParts), relationPart)

			// Check if this level is already loaded by Bun (avoid duplicates)
			existingRecords, alreadyLoaded := checkIfRelationAlreadyLoaded(currentRecords, relationPart)
			if alreadyLoaded && existingRecords.IsValid() && existingRecords.Len() > 0 {
				logger.Info("Relation '%s' already loaded by Bun, using existing %d records", relationPart, existingRecords.Len())
				currentRecords = existingRecords
				continue
			}

			// Load this level and get the loaded records for the next level
			loadedRecords, err := b.loadRelationLevel(ctx, currentRecords, relationPart, isLastPart, applyFuncs)
			if err != nil {
				return fmt.Errorf("failed to load relation %s (part %s): %w", relation, relationPart, err)
			}

			// For nested relations, use the loaded records as parents for the next level
			if !isLastPart && loadedRecords.IsValid() && loadedRecords.Len() > 0 {
				logger.Debug("Collected %d records for next level", loadedRecords.Len())
				currentRecords = loadedRecords
			} else if !isLastPart {
				logger.Debug("No records loaded at level %s, stopping nested preload", relationPart)
				break
			}
		}
	}

	return nil
}

// loadRelationLevel loads a single level of a relation for a set of parent records
// Returns the loaded records (for use as parents in nested preloads) and any error
func (b *BunSelectQuery) loadRelationLevel(ctx context.Context, parentRecords reflect.Value, relationName string, isLast bool, applyFuncs []func(common.SelectQuery) common.SelectQuery) (reflect.Value, error) {
	if parentRecords.Len() == 0 {
		return reflect.Value{}, nil
	}

	// Get the first record to inspect the struct type
	firstRecord := parentRecords.Index(0)
	if firstRecord.Kind() == reflect.Ptr {
		firstRecord = firstRecord.Elem()
	}

	if firstRecord.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("expected struct, got %v", firstRecord.Kind())
	}

	parentType := firstRecord.Type()

	// Find the relation field in the struct
	structField, found := parentType.FieldByName(relationName)
	if !found {
		return reflect.Value{}, fmt.Errorf("relation field %s not found in struct %s", relationName, parentType.Name())
	}

	// Parse the bun tag to get relation info
	bunTag := structField.Tag.Get("bun")
	logger.Debug("Relation %s bun tag: %s", relationName, bunTag)

	relInfo, err := parseRelationTag(bunTag)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to parse relation tag for %s: %w", relationName, err)
	}

	logger.Debug("Parsed relation: type=%s, join=%s", relInfo.relType, relInfo.joinCondition)

	// Extract foreign key values from parent records
	fkValues, err := extractForeignKeyValues(parentRecords, relInfo.localKey)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to extract FK values: %w", err)
	}

	if len(fkValues) == 0 {
		logger.Debug("No foreign key values to load for relation %s", relationName)
		return reflect.Value{}, nil
	}

	logger.Debug("Loading %d related records for %s (FK values: %v)", len(fkValues), relationName, fkValues)

	// Get the related model type
	relatedType := structField.Type
	isSlice := relatedType.Kind() == reflect.Slice
	if isSlice {
		relatedType = relatedType.Elem()
	}
	if relatedType.Kind() == reflect.Ptr {
		relatedType = relatedType.Elem()
	}

	// Create a slice to hold the results
	resultsSlice := reflect.MakeSlice(reflect.SliceOf(reflect.PointerTo(relatedType)), 0, len(fkValues))
	resultsPtr := reflect.New(resultsSlice.Type())
	resultsPtr.Elem().Set(resultsSlice)

	// Build and execute the query
	query := b.db.NewSelect().Model(resultsPtr.Interface())

	// Apply WHERE clause: foreign_key IN (values...)
	query = query.Where(fmt.Sprintf("%s IN (?)", relInfo.foreignKey), bun.In(fkValues))

	// Apply user's functions (if any)
	if isLast && len(applyFuncs) > 0 {
		wrapper := &BunSelectQuery{query: query, db: b.db}
		for _, fn := range applyFuncs {
			if fn != nil {
				wrapper = fn(wrapper).(*BunSelectQuery)
				query = wrapper.query
			}
		}
	}

	// Execute the query
	err = query.Scan(ctx)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load related records: %w", err)
	}

	loadedRecords := resultsPtr.Elem()
	logger.Info("Loaded %d related records for relation %s", loadedRecords.Len(), relationName)

	// Associate loaded records back to parent records
	err = associateRelatedRecords(parentRecords, loadedRecords, relationName, relInfo, isSlice)
	if err != nil {
		return reflect.Value{}, err
	}

	// Return the loaded records for use in nested preloads
	return loadedRecords, nil
}

// relationInfo holds parsed relation metadata
type relationInfo struct {
	relType       string // has-one, has-many, belongs-to
	localKey      string // Key in parent table
	foreignKey    string // Key in related table
	joinCondition string // Full join condition
}

// parseRelationTag parses the bun:"rel:..." tag
func parseRelationTag(tag string) (*relationInfo, error) {
	info := &relationInfo{}

	// Parse tag like: rel:has-one,join:rid_mastertaskitem=rid_mastertaskitem
	parts := strings.Split(tag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "rel:") {
			info.relType = strings.TrimPrefix(part, "rel:")
		} else if strings.HasPrefix(part, "join:") {
			info.joinCondition = strings.TrimPrefix(part, "join:")
			// Parse join: local_key=foreign_key
			joinParts := strings.Split(info.joinCondition, "=")
			if len(joinParts) == 2 {
				info.localKey = strings.TrimSpace(joinParts[0])
				info.foreignKey = strings.TrimSpace(joinParts[1])
			}
		}
	}

	if info.relType == "" || info.localKey == "" || info.foreignKey == "" {
		return nil, fmt.Errorf("incomplete relation tag: %s", tag)
	}

	return info, nil
}

// extractForeignKeyValues collects FK values from parent records
func extractForeignKeyValues(records reflect.Value, fkFieldName string) ([]interface{}, error) {
	values := make([]interface{}, 0, records.Len())
	seenValues := make(map[interface{}]bool)

	for i := 0; i < records.Len(); i++ {
		record := records.Index(i)
		if record.Kind() == reflect.Ptr {
			record = record.Elem()
		}

		// Find the FK field - try both exact name and capitalized version
		fkField := record.FieldByName(fkFieldName)
		if !fkField.IsValid() {
			// Try capitalized version
			fkField = record.FieldByName(strings.ToUpper(fkFieldName[:1]) + fkFieldName[1:])
		}
		if !fkField.IsValid() {
			// Try finding by json tag
			for j := 0; j < record.NumField(); j++ {
				field := record.Type().Field(j)
				jsonTag := field.Tag.Get("json")
				bunTag := field.Tag.Get("bun")
				if strings.HasPrefix(jsonTag, fkFieldName) || strings.Contains(bunTag, fkFieldName) {
					fkField = record.Field(j)
					break
				}
			}
		}

		if !fkField.IsValid() {
			continue // Skip records without FK
		}

		// Extract the value
		var value interface{}
		if fkField.CanInterface() {
			value = fkField.Interface()

			// Handle SqlNull types
			if nullType, ok := value.(interface{ IsNull() bool }); ok {
				if nullType.IsNull() {
					continue
				}
			}

			// Handle types with Int64() method
			if int64er, ok := value.(interface{ Int64() int64 }); ok {
				value = int64er.Int64()
			}

			// Deduplicate
			if !seenValues[value] {
				values = append(values, value)
				seenValues[value] = true
			}
		}
	}

	return values, nil
}

// associateRelatedRecords associates loaded records back to parents
func associateRelatedRecords(parents, related reflect.Value, fieldName string, relInfo *relationInfo, isSlice bool) error {
	logger.Debug("Associating %d related records to %d parents for field '%s'", related.Len(), parents.Len(), fieldName)

	// Build a map: foreignKey -> related record(s)
	relatedMap := make(map[interface{}][]reflect.Value)

	for i := 0; i < related.Len(); i++ {
		relRecord := related.Index(i)
		relRecordElem := relRecord
		if relRecordElem.Kind() == reflect.Ptr {
			relRecordElem = relRecordElem.Elem()
		}

		// Get the foreign key value from the related record - try multiple variations
		fkField := findFieldByName(relRecordElem, relInfo.foreignKey)
		if !fkField.IsValid() {
			logger.Warn("Could not find FK field '%s' in related record type %s", relInfo.foreignKey, relRecordElem.Type().Name())
			continue
		}

		fkValue := extractFieldValue(fkField)
		if fkValue == nil {
			continue
		}

		relatedMap[fkValue] = append(relatedMap[fkValue], related.Index(i))
	}

	logger.Debug("Built related map with %d unique FK values", len(relatedMap))

	// Associate with parents
	associatedCount := 0
	for i := 0; i < parents.Len(); i++ {
		parentPtr := parents.Index(i)
		parent := parentPtr
		if parent.Kind() == reflect.Ptr {
			parent = parent.Elem()
		}

		// Get the local key value from parent
		localField := findFieldByName(parent, relInfo.localKey)
		if !localField.IsValid() {
			logger.Warn("Could not find local key field '%s' in parent type %s", relInfo.localKey, parent.Type().Name())
			continue
		}

		localValue := extractFieldValue(localField)
		if localValue == nil {
			continue
		}

		// Find matching related records
		matches := relatedMap[localValue]
		if len(matches) == 0 {
			continue
		}

		// Set the relation field - IMPORTANT: use the pointer, not the elem
		relationField := parent.FieldByName(fieldName)
		if !relationField.IsValid() {
			logger.Warn("Relation field '%s' not found in parent type %s", fieldName, parent.Type().Name())
			continue
		}

		if !relationField.CanSet() {
			logger.Warn("Relation field '%s' cannot be set (unexported?)", fieldName)
			continue
		}

		if isSlice {
			// For has-many: replace entire slice (don't append to avoid duplicates)
			newSlice := reflect.MakeSlice(relationField.Type(), 0, len(matches))
			for _, match := range matches {
				newSlice = reflect.Append(newSlice, match)
			}
			relationField.Set(newSlice)
			associatedCount += len(matches)
			logger.Debug("Set has-many field '%s' with %d records for parent %d", fieldName, len(matches), i)
		} else {
			// For has-one/belongs-to: only set if not already set (avoid duplicates)
			if relationField.IsNil() {
				relationField.Set(matches[0])
				associatedCount++
				logger.Debug("Set has-one field '%s' for parent %d", fieldName, i)
			} else {
				logger.Debug("Skipping has-one field '%s' for parent %d (already set)", fieldName, i)
			}
		}
	}

	logger.Info("Associated %d related records to %d parents for field '%s'", associatedCount, parents.Len(), fieldName)
	return nil
}

// findFieldByName finds a struct field by name, trying multiple variations
func findFieldByName(v reflect.Value, name string) reflect.Value {
	// Try exact name
	field := v.FieldByName(name)
	if field.IsValid() {
		return field
	}

	// Try with capital first letter
	if len(name) > 0 {
		capital := strings.ToUpper(name[0:1]) + name[1:]
		field = v.FieldByName(capital)
		if field.IsValid() {
			return field
		}
	}

	// Try searching by json or bun tag
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		jsonTag := f.Tag.Get("json")
		bunTag := f.Tag.Get("bun")

		// Check json tag
		if strings.HasPrefix(jsonTag, name+",") || jsonTag == name {
			return v.Field(i)
		}

		// Check bun tag for column name
		if strings.Contains(bunTag, name+",") || strings.Contains(bunTag, name+":") {
			return v.Field(i)
		}
	}

	return reflect.Value{}
}

// extractFieldValue extracts the value from a field, handling SqlNull types
func extractFieldValue(field reflect.Value) interface{} {
	if !field.CanInterface() {
		return nil
	}

	value := field.Interface()

	// Handle SqlNull types
	if nullType, ok := value.(interface{ IsNull() bool }); ok {
		if nullType.IsNull() {
			return nil
		}
	}

	// Handle types with Int64() method
	if int64er, ok := value.(interface{ Int64() int64 }); ok {
		return int64er.Int64()
	}

	// Handle types with String() method for comparison
	if stringer, ok := value.(interface{ String() string }); ok {
		return stringer.String()
	}

	return value
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

	err = b.query.Scan(ctx, dest)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunSelectQuery.Scan failed. SQL: %s. Error: %v", sqlStr, err)
		return err
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

	err = b.query.Scan(ctx)
	if err != nil {
		// Log SQL string for debugging
		sqlStr := b.query.String()
		logger.Error("BunSelectQuery.ScanModel failed. SQL: %s. Error: %v", sqlStr, err)
		return err
	}

	// After main query, load custom preloads using separate queries
	if len(b.customPreloads) > 0 {
		logger.Info("Loading %d custom preload(s) with separate queries", len(b.customPreloads))
		if err := b.loadCustomPreloads(ctx); err != nil {
			logger.Error("Failed to load custom preloads: %v", err)
			return err
		}
	}

	return nil
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
