package database

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// GormAdapter adapts GORM to work with our Database interface
type GormAdapter struct {
	db *gorm.DB
}

// NewGormAdapter creates a new GORM adapter
func NewGormAdapter(db *gorm.DB) *GormAdapter {
	return &GormAdapter{db: db}
}

// EnableQueryDebug enables query debugging which logs all SQL queries including preloads
// This is useful for debugging preload queries that may be failing
func (g *GormAdapter) EnableQueryDebug() *GormAdapter {
	g.db = g.db.Debug()
	logger.Info("GORM query debug mode enabled - all SQL queries will be logged")
	return g
}

// DisableQueryDebug disables query debugging
func (g *GormAdapter) DisableQueryDebug() *GormAdapter {
	// GORM's Debug() creates a new session, so we need to get the base DB
	// This is a simplified implementation
	logger.Info("GORM debug mode - create a new adapter without Debug() to disable")
	return g
}

func (g *GormAdapter) NewSelect() common.SelectQuery {
	return &GormSelectQuery{db: g.db}
}

func (g *GormAdapter) NewInsert() common.InsertQuery {
	return &GormInsertQuery{db: g.db}
}

func (g *GormAdapter) NewUpdate() common.UpdateQuery {
	return &GormUpdateQuery{db: g.db}
}

func (g *GormAdapter) NewDelete() common.DeleteQuery {
	return &GormDeleteQuery{db: g.db}
}

func (g *GormAdapter) Exec(ctx context.Context, query string, args ...interface{}) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormAdapter.Exec", r)
		}
	}()
	result := g.db.WithContext(ctx).Exec(query, args...)
	return &GormResult{result: result}, result.Error
}

func (g *GormAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormAdapter.Query", r)
		}
	}()
	return g.db.WithContext(ctx).Raw(query, args...).Find(dest).Error
}

func (g *GormAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	tx := g.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &GormAdapter{db: tx}, nil
}

func (g *GormAdapter) CommitTx(ctx context.Context) error {
	return g.db.WithContext(ctx).Commit().Error
}

func (g *GormAdapter) RollbackTx(ctx context.Context) error {
	return g.db.WithContext(ctx).Rollback().Error
}

func (g *GormAdapter) RunInTransaction(ctx context.Context, fn func(common.Database) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormAdapter.RunInTransaction", r)
		}
	}()
	return g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		adapter := &GormAdapter{db: tx}
		return fn(adapter)
	})
}

func (g *GormAdapter) GetUnderlyingDB() interface{} {
	return g.db
}

// GormSelectQuery implements SelectQuery for GORM
type GormSelectQuery struct {
	db             *gorm.DB
	schema         string // Separated schema name
	tableName      string // Just the table name, without schema
	tableAlias     string
	inJoinContext  bool   // Track if we're in a JOIN relation context
	joinTableAlias string // Alias to use for JOIN conditions
}

func (g *GormSelectQuery) Model(model interface{}) common.SelectQuery {
	g.db = g.db.Model(model)

	// Try to get table name from model if it implements TableNameProvider
	if provider, ok := model.(common.TableNameProvider); ok {
		fullTableName := provider.TableName()
		// Check if the table name contains schema (e.g., "schema.table")
		g.schema, g.tableName = parseTableName(fullTableName)
	}

	if provider, ok := model.(common.TableAliasProvider); ok {
		g.tableAlias = provider.TableAlias()
	}

	return g
}

func (g *GormSelectQuery) Table(table string) common.SelectQuery {
	g.db = g.db.Table(table)
	// Check if the table name contains schema (e.g., "schema.table")
	g.schema, g.tableName = parseTableName(table)

	return g
}

func (g *GormSelectQuery) Column(columns ...string) common.SelectQuery {
	g.db = g.db.Select(columns)
	return g
}

func (g *GormSelectQuery) ColumnExpr(query string, args ...interface{}) common.SelectQuery {
	if len(args) > 0 {
		g.db = g.db.Select(query, args...)
	} else {
		g.db = g.db.Select(query)
	}

	return g
}

func (g *GormSelectQuery) Where(query string, args ...interface{}) common.SelectQuery {
	// If we're in a JOIN context, add table prefix to unqualified columns
	if g.inJoinContext && g.joinTableAlias != "" {
		query = addTablePrefixGorm(query, g.joinTableAlias)
	}
	g.db = g.db.Where(query, args...)
	return g
}

// addTablePrefixGorm adds a table prefix to unqualified column references (GORM version)
func addTablePrefixGorm(query, tableAlias string) string {
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
		if !strings.Contains(part, ".") {
			// Extract potential column name (before = or other operators)
			for _, op := range []string{"=", "!=", "<>", ">", ">=", "<", "<=", " LIKE ", " IN ", " IS "} {
				if strings.Contains(part, op) {
					colName := strings.Split(part, op)[0]
					colName = strings.TrimSpace(colName)
					if colName != "" && !isOperatorOrKeywordGorm(colName) {
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

// isOperatorOrKeywordGorm checks if a string is likely an operator or SQL keyword (GORM version)
func isOperatorOrKeywordGorm(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	keywords := []string{"AND", "OR", "NOT", "IN", "IS", "NULL", "TRUE", "FALSE", "LIKE", "BETWEEN"}
	for _, kw := range keywords {
		if s == kw {
			return true
		}
	}
	return false
}

func (g *GormSelectQuery) WhereOr(query string, args ...interface{}) common.SelectQuery {
	g.db = g.db.Or(query, args...)
	return g
}

func (g *GormSelectQuery) Join(query string, args ...interface{}) common.SelectQuery {
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
	if prefix == "" && g.tableName != "" {
		prefix = g.tableName
	}

	// If prefix is provided, add it as an alias in the join
	// GORM expects: "JOIN table AS alias ON condition"
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

	g.db = g.db.Joins(joinClause, sqlArgs...)
	return g
}

func (g *GormSelectQuery) LeftJoin(query string, args ...interface{}) common.SelectQuery {
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
	if prefix == "" && g.tableName != "" {
		prefix = g.tableName
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

	g.db = g.db.Joins("LEFT JOIN "+joinClause, sqlArgs...)
	return g
}

func (g *GormSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
	g.db = g.db.Preload(relation, conditions...)
	return g
}

func (g *GormSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// Auto-detect relationship type and choose optimal loading strategy
	// Get the model from GORM's statement if available
	if g.db.Statement != nil && g.db.Statement.Model != nil {
		relType := reflection.GetRelationType(g.db.Statement.Model, relation)

		// Log the detected relationship type
		logger.Debug("PreloadRelation '%s' detected as: %s", relation, relType)

		// If this is a belongs-to or has-one relation, use JOIN for better performance
		if relType.ShouldUseJoin() {
			logger.Info("Using JOIN strategy for %s relation '%s'", relType, relation)
			return g.JoinRelation(relation, apply...)
		}

		// For has-many, many-to-many, or unknown: use separate query (safer default)
		if relType == reflection.RelationHasMany || relType == reflection.RelationManyToMany {
			logger.Debug("Using separate query for %s relation '%s'", relType, relation)
		}
	}

	// Use GORM's Preload (separate query strategy)
	g.db = g.db.Preload(relation, func(db *gorm.DB) *gorm.DB {
		if len(apply) == 0 {
			return db
		}

		wrapper := &GormSelectQuery{
			db: db,
		}

		current := common.SelectQuery(wrapper)

		for _, fn := range apply {
			if fn != nil {

				modified := fn(current)
				current = modified
			}
		}

		if finalBun, ok := current.(*GormSelectQuery); ok {
			return finalBun.db
		}

		return db // fallback
	})

	return g
}

func (g *GormSelectQuery) JoinRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// JoinRelation uses a JOIN instead of a separate preload query
	// This is more efficient for many-to-one or one-to-one relationships
	// as it avoids additional round trips to the database

	// GORM's Joins() method forces a JOIN for the preload
	logger.Debug("JoinRelation '%s' - Using GORM Joins() with automatic WHERE prefix addition", relation)

	g.db = g.db.Joins(relation, func(db *gorm.DB) *gorm.DB {
		if len(apply) == 0 {
			return db
		}

		wrapper := &GormSelectQuery{
			db:             db,
			inJoinContext:  true,                      // Mark as JOIN context
			joinTableAlias: strings.ToLower(relation), // Use relation name as alias
		}
		current := common.SelectQuery(wrapper)

		for _, fn := range apply {
			if fn != nil {
				current = fn(current)
			}
		}

		if finalGorm, ok := current.(*GormSelectQuery); ok {
			return finalGorm.db
		}

		return db
	})

	return g
}

func (g *GormSelectQuery) Order(order string) common.SelectQuery {
	g.db = g.db.Order(order)
	return g
}

func (g *GormSelectQuery) Limit(n int) common.SelectQuery {
	g.db = g.db.Limit(n)
	return g
}

func (g *GormSelectQuery) Offset(n int) common.SelectQuery {
	g.db = g.db.Offset(n)
	return g
}

func (g *GormSelectQuery) Group(group string) common.SelectQuery {
	g.db = g.db.Group(group)
	return g
}

func (g *GormSelectQuery) Having(having string, args ...interface{}) common.SelectQuery {
	g.db = g.db.Having(having, args...)
	return g
}

func (g *GormSelectQuery) Scan(ctx context.Context, dest interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.Scan", r)
		}
	}()
	err = g.db.WithContext(ctx).Find(dest).Error
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Find(dest)
		})
		logger.Error("GormSelectQuery.Scan failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return err
}

func (g *GormSelectQuery) ScanModel(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.ScanModel", r)
		}
	}()
	if g.db.Statement.Model == nil {
		return fmt.Errorf("ScanModel requires Model() to be set before scanning")
	}
	err = g.db.WithContext(ctx).Find(g.db.Statement.Model).Error
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Find(g.db.Statement.Model)
		})
		logger.Error("GormSelectQuery.ScanModel failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return err
}

func (g *GormSelectQuery) Count(ctx context.Context) (count int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.Count", r)
			count = 0
		}
	}()
	var count64 int64
	err = g.db.WithContext(ctx).Count(&count64).Error
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Count(&count64)
		})
		logger.Error("GormSelectQuery.Count failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return int(count64), err
}

func (g *GormSelectQuery) Exists(ctx context.Context) (exists bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.Exists", r)
			exists = false
		}
	}()
	var count int64
	err = g.db.WithContext(ctx).Limit(1).Count(&count).Error
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Limit(1).Count(&count)
		})
		logger.Error("GormSelectQuery.Exists failed. SQL: %s. Error: %v", sqlStr, err)
	}
	return count > 0, err
}

// GormInsertQuery implements InsertQuery for GORM
type GormInsertQuery struct {
	db     *gorm.DB
	model  interface{}
	values map[string]interface{}
}

func (g *GormInsertQuery) Model(model interface{}) common.InsertQuery {
	g.model = model
	g.db = g.db.Model(model)
	return g
}

func (g *GormInsertQuery) Table(table string) common.InsertQuery {
	g.db = g.db.Table(table)
	return g
}

func (g *GormInsertQuery) Value(column string, value interface{}) common.InsertQuery {
	if g.values == nil {
		g.values = make(map[string]interface{})
	}
	g.values[column] = value
	return g
}

func (g *GormInsertQuery) OnConflict(action string) common.InsertQuery {
	// GORM handles conflicts differently, this would need specific implementation
	return g
}

func (g *GormInsertQuery) Returning(columns ...string) common.InsertQuery {
	// GORM doesn't have explicit RETURNING, but updates the model
	return g
}

func (g *GormInsertQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormInsertQuery.Exec", r)
		}
	}()
	var result *gorm.DB
	switch {
	case g.model != nil:
		result = g.db.WithContext(ctx).Create(g.model)
	case g.values != nil:
		result = g.db.WithContext(ctx).Create(g.values)
	default:
		result = g.db.WithContext(ctx).Create(map[string]interface{}{})
	}
	return &GormResult{result: result}, result.Error
}

// GormUpdateQuery implements UpdateQuery for GORM
type GormUpdateQuery struct {
	db      *gorm.DB
	model   interface{}
	updates interface{}
}

func (g *GormUpdateQuery) Model(model interface{}) common.UpdateQuery {
	g.model = model
	g.db = g.db.Model(model)
	return g
}

func (g *GormUpdateQuery) Table(table string) common.UpdateQuery {
	g.db = g.db.Table(table)
	if g.model == nil {
		// Try to get table name from table string if model is not set
		model, err := modelregistry.GetModelByName(table)
		if err == nil {
			g.model = model
		}
	}
	return g
}

func (g *GormUpdateQuery) Set(column string, value interface{}) common.UpdateQuery {
	// Validate column is writable if model is set
	if g.model != nil && !reflection.IsColumnWritable(g.model, column) {
		// Skip read-only columns
		return g
	}

	if g.updates == nil {
		g.updates = make(map[string]interface{})
	}
	if updates, ok := g.updates.(map[string]interface{}); ok {
		updates[column] = value
	}
	return g
}

func (g *GormUpdateQuery) SetMap(values map[string]interface{}) common.UpdateQuery {

	// Filter out read-only columns if model is set
	if g.model != nil {
		pkName := reflection.GetPrimaryKeyName(g.model)
		filteredValues := make(map[string]interface{})
		for column, value := range values {
			if pkName != "" && column == pkName {
				// Skip primary key updates
				continue
			}
			if reflection.IsColumnWritable(g.model, column) {
				filteredValues[column] = value
			}

		}
		g.updates = filteredValues
	} else {
		g.updates = values
	}
	return g
}

func (g *GormUpdateQuery) Where(query string, args ...interface{}) common.UpdateQuery {
	g.db = g.db.Where(query, args...)
	return g
}

func (g *GormUpdateQuery) Returning(columns ...string) common.UpdateQuery {
	// GORM doesn't have explicit RETURNING
	return g
}

func (g *GormUpdateQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormUpdateQuery.Exec", r)
		}
	}()
	result := g.db.WithContext(ctx).Updates(g.updates)
	if result.Error != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Updates(g.updates)
		})
		logger.Error("GormUpdateQuery.Exec failed. SQL: %s. Error: %v", sqlStr, result.Error)
	}
	return &GormResult{result: result}, result.Error
}

// GormDeleteQuery implements DeleteQuery for GORM
type GormDeleteQuery struct {
	db    *gorm.DB
	model interface{}
}

func (g *GormDeleteQuery) Model(model interface{}) common.DeleteQuery {
	g.model = model
	g.db = g.db.Model(model)
	return g
}

func (g *GormDeleteQuery) Table(table string) common.DeleteQuery {
	g.db = g.db.Table(table)
	return g
}

func (g *GormDeleteQuery) Where(query string, args ...interface{}) common.DeleteQuery {
	g.db = g.db.Where(query, args...)
	return g
}

func (g *GormDeleteQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormDeleteQuery.Exec", r)
		}
	}()
	result := g.db.WithContext(ctx).Delete(g.model)
	if result.Error != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Delete(g.model)
		})
		logger.Error("GormDeleteQuery.Exec failed. SQL: %s. Error: %v", sqlStr, result.Error)
	}
	return &GormResult{result: result}, result.Error
}

// GormResult implements Result for GORM
type GormResult struct {
	result *gorm.DB
}

func (g *GormResult) RowsAffected() int64 {
	return g.result.RowsAffected
}

func (g *GormResult) LastInsertId() (int64, error) {
	// GORM doesn't directly provide last insert ID, would need specific implementation
	return 0, nil
}
