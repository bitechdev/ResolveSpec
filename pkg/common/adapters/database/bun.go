package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/uptrace/bun"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// BunAdapter adapts Bun to work with our Database interface
// This demonstrates how the abstraction works with different ORMs
type BunAdapter struct {
	db *bun.DB
}

// NewBunAdapter creates a new Bun adapter
func NewBunAdapter(db *bun.DB) *BunAdapter {
	return &BunAdapter{db: db}
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

// BunSelectQuery implements SelectQuery for Bun
type BunSelectQuery struct {
	query            *bun.SelectQuery
	db               bun.IDB // Store DB connection for count queries
	hasModel         bool    // Track if Model() was called
	schema           string  // Separated schema name
	tableName        string  // Just the table name, without schema
	tableAlias       string
	deferredPreloads []deferredPreload // Preloads to execute as separate queries
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
	b.query = b.query.Where(query, args...)
	return b
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
	// Check if this relation chain would create problematic long aliases
	relationParts := strings.Split(relation, ".")
	aliasChain := strings.ToLower(strings.Join(relationParts, "__"))

	// PostgreSQL's identifier limit is 63 characters
	const postgresIdentifierLimit = 63
	const safeAliasLimit = 35 // Leave room for column names

	// If the alias chain is too long, defer this preload to be executed as a separate query
	if len(aliasChain) > safeAliasLimit {
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

func (b *BunSelectQuery) Order(order string) common.SelectQuery {
	b.query = b.query.Order(order)
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
	}

	return nil
}

func (b *BunSelectQuery) ScanModel(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("BunSelectQuery.ScanModel", r)
		}
	}()
	if b.query.GetModel() == nil {
		return fmt.Errorf("model is nil")
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

	// Get the interface value to pass to Bun
	parentValue := parentField.Interface()

	// Load the child relation on the parent record
	// This uses a shorter alias since we're only loading "Child", not "Parent.Child"
	return b.db.NewSelect().
		Model(parentValue).
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
