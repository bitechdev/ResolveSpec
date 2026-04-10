package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// GormAdapter adapts GORM to work with our Database interface
type GormAdapter struct {
	dbMu           sync.RWMutex
	db             *gorm.DB
	dbFactory      func() (*gorm.DB, error)
	driverName     string
	metricsEnabled bool
}

// NewGormAdapter creates a new GORM adapter
func NewGormAdapter(db *gorm.DB) *GormAdapter {
	adapter := &GormAdapter{db: db, metricsEnabled: true}
	// Initialize driver name
	adapter.driverName = adapter.DriverName()
	return adapter
}

// WithDBFactory configures a factory used to reopen the database connection if it is closed.
func (g *GormAdapter) WithDBFactory(factory func() (*gorm.DB, error)) *GormAdapter {
	g.dbFactory = factory
	return g
}

// SetMetricsEnabled enables or disables query metrics for this adapter.
func (g *GormAdapter) SetMetricsEnabled(enabled bool) *GormAdapter {
	g.metricsEnabled = enabled
	return g
}

// EnableMetrics enables query metrics for this adapter.
func (g *GormAdapter) EnableMetrics() *GormAdapter {
	return g.SetMetricsEnabled(true)
}

// DisableMetrics disables query metrics for this adapter.
func (g *GormAdapter) DisableMetrics() *GormAdapter {
	return g.SetMetricsEnabled(false)
}

func (g *GormAdapter) getDB() *gorm.DB {
	g.dbMu.RLock()
	defer g.dbMu.RUnlock()
	return g.db
}

func (g *GormAdapter) reconnectDB(targets ...*gorm.DB) error {
	if g.dbFactory == nil {
		return fmt.Errorf("no db factory configured for reconnect")
	}

	freshDB, err := g.dbFactory()
	if err != nil {
		return err
	}

	g.dbMu.Lock()
	previous := g.db
	g.db = freshDB
	g.driverName = normalizeGormDriverName(freshDB)
	g.dbMu.Unlock()

	if previous != nil {
		syncGormConnPool(previous, freshDB)
	}

	for _, target := range targets {
		if target != nil && target != previous {
			syncGormConnPool(target, freshDB)
		}
	}

	return nil
}

func syncGormConnPool(target, fresh *gorm.DB) {
	if target == nil || fresh == nil {
		return
	}

	if target.Config != nil && fresh.Config != nil {
		target.ConnPool = fresh.ConnPool
	}

	if target.Statement != nil {
		if fresh.Statement != nil && fresh.Statement.ConnPool != nil {
			target.Statement.ConnPool = fresh.Statement.ConnPool
		} else if fresh.Config != nil {
			target.Statement.ConnPool = fresh.ConnPool
		}
		target.Statement.DB = target
	}
}

// EnableQueryDebug enables query debugging which logs all SQL queries including preloads
// This is useful for debugging preload queries that may be failing
func (g *GormAdapter) EnableQueryDebug() *GormAdapter {
	g.dbMu.Lock()
	g.db = g.db.Debug()
	g.dbMu.Unlock()
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
	return &GormSelectQuery{db: g.getDB(), driverName: g.driverName, reconnect: g.reconnectDB, metricsEnabled: g.metricsEnabled}
}

func (g *GormAdapter) NewInsert() common.InsertQuery {
	return &GormInsertQuery{db: g.getDB(), reconnect: g.reconnectDB, metricsEnabled: g.metricsEnabled}
}

func (g *GormAdapter) NewUpdate() common.UpdateQuery {
	return &GormUpdateQuery{db: g.getDB(), reconnect: g.reconnectDB, metricsEnabled: g.metricsEnabled}
}

func (g *GormAdapter) NewDelete() common.DeleteQuery {
	return &GormDeleteQuery{db: g.getDB(), reconnect: g.reconnectDB, metricsEnabled: g.metricsEnabled}
}

func (g *GormAdapter) Exec(ctx context.Context, query string, args ...interface{}) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormAdapter.Exec", r)
		}
	}()
	startedAt := time.Now()
	operation, schema, entity, table := metricTargetFromRawQuery(query, g.driverName)
	run := func() *gorm.DB {
		return g.getDB().WithContext(ctx).Exec(query, args...)
	}
	result := run()
	if isDBClosed(result.Error) {
		if reconnErr := g.reconnectDB(); reconnErr == nil {
			result = run()
		}
	}
	recordQueryMetrics(g.metricsEnabled, operation, schema, entity, table, startedAt, result.Error)
	return &GormResult{result: result}, result.Error
}

func (g *GormAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormAdapter.Query", r)
		}
	}()
	startedAt := time.Now()
	operation, schema, entity, table := metricTargetFromRawQuery(query, g.driverName)
	run := func() error {
		return g.getDB().WithContext(ctx).Raw(query, args...).Find(dest).Error
	}
	err = run()
	if isDBClosed(err) {
		if reconnErr := g.reconnectDB(); reconnErr == nil {
			err = run()
		}
	}
	recordQueryMetrics(g.metricsEnabled, operation, schema, entity, table, startedAt, err)
	return err
}

func (g *GormAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	run := func() *gorm.DB {
		return g.getDB().WithContext(ctx).Begin()
	}
	tx := run()
	if isDBClosed(tx.Error) {
		if reconnErr := g.reconnectDB(); reconnErr == nil {
			tx = run()
		}
	}
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &GormAdapter{db: tx, dbFactory: g.dbFactory, driverName: g.driverName, metricsEnabled: g.metricsEnabled}, nil
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
	run := func() error {
		return g.getDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			adapter := &GormAdapter{db: tx, dbFactory: g.dbFactory, driverName: g.driverName, metricsEnabled: g.metricsEnabled}
			return fn(adapter)
		})
	}
	err = run()
	if isDBClosed(err) {
		if reconnErr := g.reconnectDB(); reconnErr == nil {
			err = run()
		}
	}
	return err
}

func (g *GormAdapter) GetUnderlyingDB() interface{} {
	return g.getDB()
}

func (g *GormAdapter) DriverName() string {
	return normalizeGormDriverName(g.getDB())
}

func normalizeGormDriverName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return ""
	}
	// Normalize GORM's dialector name to match the project's canonical vocabulary.
	// GORM returns "sqlserver" for MSSQL; the rest of the project uses "mssql".
	// GORM returns "sqlite" or "sqlite3" for SQLite; we normalize to "sqlite".
	switch name := db.Name(); name {
	case "sqlserver":
		return "mssql"
	case "sqlite3":
		return "sqlite"
	default:
		return name
	}
}

// GormSelectQuery implements SelectQuery for GORM
type GormSelectQuery struct {
	db             *gorm.DB
	reconnect      func(...*gorm.DB) error
	schema         string // Separated schema name
	tableName      string // Just the table name, without schema
	entity         string
	tableAlias     string
	driverName     string // Database driver name (postgres, sqlite, mssql)
	inJoinContext  bool   // Track if we're in a JOIN relation context
	joinTableAlias string // Alias to use for JOIN conditions
	metricsEnabled bool
}

func (g *GormSelectQuery) Model(model interface{}) common.SelectQuery {
	g.db = g.db.Model(model)
	g.schema, g.tableName = schemaAndTableFromModel(model, g.driverName)
	g.entity = entityNameFromModel(model, g.tableName)

	if provider, ok := model.(common.TableAliasProvider); ok {
		g.tableAlias = provider.TableAlias()
	}

	return g
}

func (g *GormSelectQuery) Table(table string) common.SelectQuery {
	g.db = g.db.Table(table)
	// Check if the table name contains schema (e.g., "schema.table")
	// For SQLite, this will convert "schema.table" to "schema_table"
	g.schema, g.tableName = parseTableName(table, g.driverName)
	if g.entity == "" {
		g.entity = cleanMetricIdentifier(g.tableName)
	}

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
			db:             db,
			reconnect:      g.reconnect,
			driverName:     g.driverName,
			metricsEnabled: g.metricsEnabled,
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
			reconnect:      g.reconnect,
			driverName:     g.driverName,
			inJoinContext:  true,                      // Mark as JOIN context
			joinTableAlias: strings.ToLower(relation), // Use relation name as alias
			metricsEnabled: g.metricsEnabled,
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

func (g *GormSelectQuery) OrderExpr(order string, args ...interface{}) common.SelectQuery {
	// GORM's Order can handle expressions directly
	g.db = g.db.Order(gorm.Expr(order, args...))
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
	startedAt := time.Now()
	run := func() error {
		return g.db.WithContext(ctx).Find(dest).Error
	}
	err = run()
	if isDBClosed(err) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			err = run()
		}
	}
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Find(dest)
		})
		logger.Error("GormSelectQuery.Scan failed. SQL: %s. Error: %v", sqlStr, err)
	}
	recordQueryMetrics(g.metricsEnabled, "SELECT", g.schema, g.entity, g.tableName, startedAt, err)
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
	startedAt := time.Now()
	run := func() error {
		return g.db.WithContext(ctx).Find(g.db.Statement.Model).Error
	}
	err = run()
	if isDBClosed(err) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			err = run()
		}
	}
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Find(g.db.Statement.Model)
		})
		logger.Error("GormSelectQuery.ScanModel failed. SQL: %s. Error: %v", sqlStr, err)
	}
	recordQueryMetrics(g.metricsEnabled, "SELECT", g.schema, g.entity, g.tableName, startedAt, err)
	return err
}

func (g *GormSelectQuery) Count(ctx context.Context) (count int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.Count", r)
			count = 0
		}
	}()
	startedAt := time.Now()
	var count64 int64
	run := func() error {
		return g.db.WithContext(ctx).Count(&count64).Error
	}
	err = run()
	if isDBClosed(err) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			err = run()
		}
	}
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Count(&count64)
		})
		logger.Error("GormSelectQuery.Count failed. SQL: %s. Error: %v", sqlStr, err)
	}
	recordQueryMetrics(g.metricsEnabled, "COUNT", g.schema, g.entity, g.tableName, startedAt, err)
	return int(count64), err
}

func (g *GormSelectQuery) Exists(ctx context.Context) (exists bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("GormSelectQuery.Exists", r)
			exists = false
		}
	}()
	startedAt := time.Now()
	var count int64
	run := func() error {
		return g.db.WithContext(ctx).Limit(1).Count(&count).Error
	}
	err = run()
	if isDBClosed(err) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			err = run()
		}
	}
	if err != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Limit(1).Count(&count)
		})
		logger.Error("GormSelectQuery.Exists failed. SQL: %s. Error: %v", sqlStr, err)
	}
	recordQueryMetrics(g.metricsEnabled, "EXISTS", g.schema, g.entity, g.tableName, startedAt, err)
	return count > 0, err
}

// GormInsertQuery implements InsertQuery for GORM
type GormInsertQuery struct {
	db             *gorm.DB
	reconnect      func(...*gorm.DB) error
	model          interface{}
	values         map[string]interface{}
	schema         string
	tableName      string
	entity         string
	metricsEnabled bool
}

func (g *GormInsertQuery) Model(model interface{}) common.InsertQuery {
	g.model = model
	g.db = g.db.Model(model)
	g.schema, g.tableName = schemaAndTableFromModel(model, g.db.Name())
	g.entity = entityNameFromModel(model, g.tableName)
	return g
}

func (g *GormInsertQuery) Table(table string) common.InsertQuery {
	g.db = g.db.Table(table)
	g.schema, g.tableName = parseTableName(table, g.db.Name())
	if g.entity == "" {
		g.entity = cleanMetricIdentifier(g.tableName)
	}
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
	startedAt := time.Now()
	run := func() *gorm.DB {
		switch {
		case g.model != nil:
			return g.db.WithContext(ctx).Create(g.model)
		case g.values != nil:
			return g.db.WithContext(ctx).Create(g.values)
		default:
			return g.db.WithContext(ctx).Create(map[string]interface{}{})
		}
	}
	result := run()
	if isDBClosed(result.Error) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			result = run()
		}
	}
	recordQueryMetrics(g.metricsEnabled, "INSERT", g.schema, g.entity, g.tableName, startedAt, result.Error)
	return &GormResult{result: result}, result.Error
}

// GormUpdateQuery implements UpdateQuery for GORM
type GormUpdateQuery struct {
	db             *gorm.DB
	reconnect      func(...*gorm.DB) error
	model          interface{}
	updates        interface{}
	schema         string
	tableName      string
	entity         string
	metricsEnabled bool
}

func (g *GormUpdateQuery) Model(model interface{}) common.UpdateQuery {
	g.model = model
	g.db = g.db.Model(model)
	g.schema, g.tableName = schemaAndTableFromModel(model, g.db.Name())
	g.entity = entityNameFromModel(model, g.tableName)
	return g
}

func (g *GormUpdateQuery) Table(table string) common.UpdateQuery {
	g.db = g.db.Table(table)
	g.schema, g.tableName = parseTableName(table, g.db.Name())
	if g.entity == "" {
		g.entity = cleanMetricIdentifier(g.tableName)
	}
	if g.model == nil {
		// Try to get table name from table string if model is not set
		model, err := modelregistry.GetModelByName(table)
		if err == nil {
			g.model = model
			g.entity = entityNameFromModel(model, g.tableName)
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
	startedAt := time.Now()
	run := func() *gorm.DB {
		return g.db.WithContext(ctx).Updates(g.updates)
	}
	result := run()
	if isDBClosed(result.Error) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			result = run()
		}
	}
	if result.Error != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Updates(g.updates)
		})
		logger.Error("GormUpdateQuery.Exec failed. SQL: %s. Error: %v", sqlStr, result.Error)
	}
	recordQueryMetrics(g.metricsEnabled, "UPDATE", g.schema, g.entity, g.tableName, startedAt, result.Error)
	return &GormResult{result: result}, result.Error
}

// GormDeleteQuery implements DeleteQuery for GORM
type GormDeleteQuery struct {
	db             *gorm.DB
	reconnect      func(...*gorm.DB) error
	model          interface{}
	schema         string
	tableName      string
	entity         string
	metricsEnabled bool
}

func (g *GormDeleteQuery) Model(model interface{}) common.DeleteQuery {
	g.model = model
	g.db = g.db.Model(model)
	g.schema, g.tableName = schemaAndTableFromModel(model, g.db.Name())
	g.entity = entityNameFromModel(model, g.tableName)
	return g
}

func (g *GormDeleteQuery) Table(table string) common.DeleteQuery {
	g.db = g.db.Table(table)
	g.schema, g.tableName = parseTableName(table, g.db.Name())
	if g.entity == "" {
		g.entity = cleanMetricIdentifier(g.tableName)
	}
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
	startedAt := time.Now()
	run := func() *gorm.DB {
		return g.db.WithContext(ctx).Delete(g.model)
	}
	result := run()
	if isDBClosed(result.Error) && g.reconnect != nil {
		if reconnErr := g.reconnect(g.db); reconnErr == nil {
			result = run()
		}
	}
	if result.Error != nil {
		// Log SQL string for debugging
		sqlStr := g.db.ToSQL(func(tx *gorm.DB) *gorm.DB {
			return tx.Delete(g.model)
		})
		logger.Error("GormDeleteQuery.Exec failed. SQL: %s. Error: %v", sqlStr, result.Error)
	}
	recordQueryMetrics(g.metricsEnabled, "DELETE", g.schema, g.entity, g.tableName, startedAt, result.Error)
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
