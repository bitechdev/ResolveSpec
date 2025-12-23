package database

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/bitechdev/ResolveSpec/pkg/common"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"
)

// PgSQLAdapter adapts standard database/sql to work with our Database interface
// This provides a lightweight PostgreSQL adapter without ORM overhead
type PgSQLAdapter struct {
	db *sql.DB
}

// NewPgSQLAdapter creates a new PostgreSQL adapter
func NewPgSQLAdapter(db *sql.DB) *PgSQLAdapter {
	return &PgSQLAdapter{db: db}
}

// EnableQueryDebug enables query debugging for development
func (p *PgSQLAdapter) EnableQueryDebug() {
	logger.Info("PgSQL query debug mode - logging enabled via logger")
}

func (p *PgSQLAdapter) NewSelect() common.SelectQuery {
	return &PgSQLSelectQuery{
		db:      p.db,
		columns: []string{"*"},
		args:    make([]interface{}, 0),
	}
}

func (p *PgSQLAdapter) NewInsert() common.InsertQuery {
	return &PgSQLInsertQuery{
		db:     p.db,
		values: make(map[string]interface{}),
	}
}

func (p *PgSQLAdapter) NewUpdate() common.UpdateQuery {
	return &PgSQLUpdateQuery{
		db:           p.db,
		sets:         make(map[string]interface{}),
		args:         make([]interface{}, 0),
		whereClauses: make([]string, 0),
	}
}

func (p *PgSQLAdapter) NewDelete() common.DeleteQuery {
	return &PgSQLDeleteQuery{
		db:           p.db,
		args:         make([]interface{}, 0),
		whereClauses: make([]string, 0),
	}
}

func (p *PgSQLAdapter) Exec(ctx context.Context, query string, args ...interface{}) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLAdapter.Exec", r)
		}
	}()
	logger.Debug("PgSQL Exec: %s [args: %v]", query, args)
	result, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		logger.Error("PgSQL Exec failed: %v", err)
		return nil, err
	}
	return &PgSQLResult{result: result}, nil
}

func (p *PgSQLAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLAdapter.Query", r)
		}
	}()
	logger.Debug("PgSQL Query: %s [args: %v]", query, args)
	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error("PgSQL Query failed: %v", err)
		return err
	}
	defer rows.Close()

	return scanRows(rows, dest)
}

func (p *PgSQLAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &PgSQLTxAdapter{tx: tx}, nil
}

func (p *PgSQLAdapter) CommitTx(ctx context.Context) error {
	return fmt.Errorf("CommitTx should be called on transaction adapter")
}

func (p *PgSQLAdapter) RollbackTx(ctx context.Context) error {
	return fmt.Errorf("RollbackTx should be called on transaction adapter")
}

func (p *PgSQLAdapter) RunInTransaction(ctx context.Context, fn func(common.Database) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLAdapter.RunInTransaction", r)
		}
	}()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	adapter := &PgSQLTxAdapter{tx: tx}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		} else if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	return fn(adapter)
}

func (p *PgSQLAdapter) GetUnderlyingDB() interface{} {
	return p.db
}

// preloadConfig represents a relationship to be preloaded
type preloadConfig struct {
	relation   string
	conditions []interface{}
	applyFuncs []func(common.SelectQuery) common.SelectQuery
	useJoin    bool
}

// relationMetadata contains information about a relationship
type relationMetadata struct {
	fieldName    string
	relationType reflection.RelationType
	foreignKey   string
	targetTable  string
	targetKey    string
}

// PgSQLSelectQuery implements SelectQuery for PostgreSQL
type PgSQLSelectQuery struct {
	db            *sql.DB
	tx            *sql.Tx
	model         interface{}
	tableName     string
	tableAlias    string
	columns       []string
	columnExprs   []string
	whereClauses  []string
	orClauses     []string
	joins         []string
	orderBy       []string
	groupBy       []string
	havingClauses []string
	limit         int
	offset        int
	args          []interface{}
	paramCounter  int
	preloads      []preloadConfig
}

func (p *PgSQLSelectQuery) Model(model interface{}) common.SelectQuery {
	p.model = model
	if provider, ok := model.(common.TableNameProvider); ok {
		p.tableName = provider.TableName()
	}
	if provider, ok := model.(common.TableAliasProvider); ok {
		p.tableAlias = provider.TableAlias()
	}
	return p
}

func (p *PgSQLSelectQuery) Table(table string) common.SelectQuery {
	p.tableName = table
	return p
}

func (p *PgSQLSelectQuery) Column(columns ...string) common.SelectQuery {
	if len(p.columns) == 1 && p.columns[0] == "*" {
		p.columns = make([]string, 0)
	}
	p.columns = append(p.columns, columns...)
	return p
}

func (p *PgSQLSelectQuery) ColumnExpr(query string, args ...interface{}) common.SelectQuery {
	p.columnExprs = append(p.columnExprs, query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) Where(query string, args ...interface{}) common.SelectQuery {
	// Replace ? placeholders with $1, $2, etc.
	query = p.replacePlaceholders(query, len(args))
	p.whereClauses = append(p.whereClauses, query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) WhereOr(query string, args ...interface{}) common.SelectQuery {
	query = p.replacePlaceholders(query, len(args))
	p.orClauses = append(p.orClauses, query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) Join(query string, args ...interface{}) common.SelectQuery {
	query = p.replacePlaceholders(query, len(args))
	p.joins = append(p.joins, "JOIN "+query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) LeftJoin(query string, args ...interface{}) common.SelectQuery {
	query = p.replacePlaceholders(query, len(args))
	p.joins = append(p.joins, "LEFT JOIN "+query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) Preload(relation string, conditions ...interface{}) common.SelectQuery {
	p.preloads = append(p.preloads, preloadConfig{
		relation:   relation,
		conditions: conditions,
		useJoin:    false, // Always use subquery for simple Preload
	})
	return p
}

func (p *PgSQLSelectQuery) PreloadRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// Auto-detect relationship type and choose optimal loading strategy
	var useJoin bool
	if p.model != nil {
		relType := reflection.GetRelationType(p.model, relation)
		useJoin = relType.ShouldUseJoin()
		logger.Debug("PreloadRelation '%s' detected as: %s (useJoin: %v)", relation, relType, useJoin)
	}

	p.preloads = append(p.preloads, preloadConfig{
		relation:   relation,
		applyFuncs: apply,
		useJoin:    useJoin,
	})
	return p
}

func (p *PgSQLSelectQuery) JoinRelation(relation string, apply ...func(common.SelectQuery) common.SelectQuery) common.SelectQuery {
	// Force JOIN loading
	logger.Debug("JoinRelation '%s' - forcing JOIN strategy", relation)
	p.preloads = append(p.preloads, preloadConfig{
		relation:   relation,
		applyFuncs: apply,
		useJoin:    true, // Force JOIN
	})
	return p
}

func (p *PgSQLSelectQuery) Order(order string) common.SelectQuery {
	p.orderBy = append(p.orderBy, order)
	return p
}

func (p *PgSQLSelectQuery) OrderExpr(order string, args ...interface{}) common.SelectQuery {
	// For PgSQL, expressions are passed directly without quoting
	// If there are args, we would need to format them, but for now just append the expression
	p.orderBy = append(p.orderBy, order)
	return p
}

func (p *PgSQLSelectQuery) Limit(n int) common.SelectQuery {
	p.limit = n
	return p
}

func (p *PgSQLSelectQuery) Offset(n int) common.SelectQuery {
	p.offset = n
	return p
}

func (p *PgSQLSelectQuery) Group(group string) common.SelectQuery {
	p.groupBy = append(p.groupBy, group)
	return p
}

func (p *PgSQLSelectQuery) Having(having string, args ...interface{}) common.SelectQuery {
	having = p.replacePlaceholders(having, len(args))
	p.havingClauses = append(p.havingClauses, having)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLSelectQuery) buildSQL() string {
	var sb strings.Builder

	// SELECT clause
	sb.WriteString("SELECT ")
	if len(p.columns) > 0 || len(p.columnExprs) > 0 {
		allCols := make([]string, 0)
		allCols = append(allCols, p.columns...)
		allCols = append(allCols, p.columnExprs...)
		sb.WriteString(strings.Join(allCols, ", "))
	} else {
		sb.WriteString("*")
	}

	// FROM clause
	if p.tableName != "" {
		sb.WriteString(" FROM ")
		sb.WriteString(p.tableName)
		if p.tableAlias != "" {
			sb.WriteString(" AS ")
			sb.WriteString(p.tableAlias)
		}
	}

	// JOIN clauses
	if len(p.joins) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(p.joins, " "))
	}

	// WHERE clause
	if len(p.whereClauses) > 0 || len(p.orClauses) > 0 {
		sb.WriteString(" WHERE ")
		conditions := make([]string, 0)

		if len(p.whereClauses) > 0 {
			conditions = append(conditions, "("+strings.Join(p.whereClauses, " AND ")+")")
		}
		if len(p.orClauses) > 0 {
			conditions = append(conditions, "("+strings.Join(p.orClauses, " OR ")+")")
		}

		sb.WriteString(strings.Join(conditions, " AND "))
	}

	// GROUP BY clause
	if len(p.groupBy) > 0 {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(strings.Join(p.groupBy, ", "))
	}

	// HAVING clause
	if len(p.havingClauses) > 0 {
		sb.WriteString(" HAVING ")
		sb.WriteString(strings.Join(p.havingClauses, " AND "))
	}

	// ORDER BY clause
	if len(p.orderBy) > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(strings.Join(p.orderBy, ", "))
	}

	// LIMIT clause
	if p.limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", p.limit))
	}

	// OFFSET clause
	if p.offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", p.offset))
	}

	return sb.String()
}

func (p *PgSQLSelectQuery) replacePlaceholders(query string, argCount int) string {
	result := query
	for i := 0; i < argCount; i++ {
		p.paramCounter++
		placeholder := fmt.Sprintf("$%d", p.paramCounter)
		result = strings.Replace(result, "?", placeholder, 1)
	}
	return result
}

func (p *PgSQLSelectQuery) Scan(ctx context.Context, dest interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLSelectQuery.Scan", r)
		}
	}()

	// Apply preloads that use JOINs
	p.applyJoinPreloads()

	query := p.buildSQL()
	logger.Debug("PgSQL SELECT: %s [args: %v]", query, p.args)

	var rows *sql.Rows
	if p.tx != nil {
		rows, err = p.tx.QueryContext(ctx, query, p.args...)
	} else {
		rows, err = p.db.QueryContext(ctx, query, p.args...)
	}

	if err != nil {
		logger.Error("PgSQL SELECT failed: %v", err)
		return err
	}
	defer rows.Close()

	err = scanRows(rows, dest)
	if err != nil {
		return err
	}

	// Apply preloads that use separate queries
	return p.applySubqueryPreloads(ctx, dest)
}

func (p *PgSQLSelectQuery) ScanModel(ctx context.Context) error {
	if p.model == nil {
		return fmt.Errorf("ScanModel requires Model() to be set before scanning")
	}
	return p.Scan(ctx, p.model)
}

func (p *PgSQLSelectQuery) Count(ctx context.Context) (count int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLSelectQuery.Count", r)
			count = 0
		}
	}()

	// Build a COUNT query
	var sb strings.Builder
	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(p.tableName)

	if len(p.joins) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(p.joins, " "))
	}

	if len(p.whereClauses) > 0 || len(p.orClauses) > 0 {
		sb.WriteString(" WHERE ")
		conditions := make([]string, 0)

		if len(p.whereClauses) > 0 {
			conditions = append(conditions, "("+strings.Join(p.whereClauses, " AND ")+")")
		}
		if len(p.orClauses) > 0 {
			conditions = append(conditions, "("+strings.Join(p.orClauses, " OR ")+")")
		}

		sb.WriteString(strings.Join(conditions, " AND "))
	}

	query := sb.String()
	logger.Debug("PgSQL COUNT: %s [args: %v]", query, p.args)

	var row *sql.Row
	if p.tx != nil {
		row = p.tx.QueryRowContext(ctx, query, p.args...)
	} else {
		row = p.db.QueryRowContext(ctx, query, p.args...)
	}

	err = row.Scan(&count)
	if err != nil {
		logger.Error("PgSQL COUNT failed: %v", err)
	}
	return count, err
}

func (p *PgSQLSelectQuery) Exists(ctx context.Context) (exists bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLSelectQuery.Exists", r)
			exists = false
		}
	}()

	count, err := p.Count(ctx)
	return count > 0, err
}

// PgSQLInsertQuery implements InsertQuery for PostgreSQL
type PgSQLInsertQuery struct {
	db        *sql.DB
	tx        *sql.Tx
	tableName string
	values    map[string]interface{}
	returning []string
}

func (p *PgSQLInsertQuery) Model(model interface{}) common.InsertQuery {
	if provider, ok := model.(common.TableNameProvider); ok {
		p.tableName = provider.TableName()
	}
	// Extract values from model using reflection
	// This is a simplified implementation
	return p
}

func (p *PgSQLInsertQuery) Table(table string) common.InsertQuery {
	p.tableName = table
	return p
}

func (p *PgSQLInsertQuery) Value(column string, value interface{}) common.InsertQuery {
	p.values[column] = value
	return p
}

func (p *PgSQLInsertQuery) OnConflict(action string) common.InsertQuery {
	logger.Warn("OnConflict not yet implemented in PgSQL adapter")
	return p
}

func (p *PgSQLInsertQuery) Returning(columns ...string) common.InsertQuery {
	p.returning = columns
	return p
}

func (p *PgSQLInsertQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLInsertQuery.Exec", r)
		}
	}()

	if len(p.values) == 0 {
		return nil, fmt.Errorf("no values to insert")
	}

	columns := make([]string, 0, len(p.values))
	placeholders := make([]string, 0, len(p.values))
	args := make([]interface{}, 0, len(p.values))

	i := 1
	for col, val := range p.values {
		columns = append(columns, col)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		args = append(args, val)
		i++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		p.tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	if len(p.returning) > 0 {
		query += " RETURNING " + strings.Join(p.returning, ", ")
	}

	logger.Debug("PgSQL INSERT: %s [args: %v]", query, args)

	var result sql.Result
	if p.tx != nil {
		result, err = p.tx.ExecContext(ctx, query, args...)
	} else {
		result, err = p.db.ExecContext(ctx, query, args...)
	}

	if err != nil {
		logger.Error("PgSQL INSERT failed: %v", err)
		return nil, err
	}

	return &PgSQLResult{result: result}, nil
}

// PgSQLUpdateQuery implements UpdateQuery for PostgreSQL
type PgSQLUpdateQuery struct {
	db           *sql.DB
	tx           *sql.Tx
	tableName    string
	model        interface{}
	sets         map[string]interface{}
	whereClauses []string
	args         []interface{}
	paramCounter int
	returning    []string
}

func (p *PgSQLUpdateQuery) Model(model interface{}) common.UpdateQuery {
	p.model = model
	if provider, ok := model.(common.TableNameProvider); ok {
		p.tableName = provider.TableName()
	}
	return p
}

func (p *PgSQLUpdateQuery) Table(table string) common.UpdateQuery {
	p.tableName = table
	if p.model == nil {
		model, err := modelregistry.GetModelByName(table)
		if err == nil {
			p.model = model
		}
	}
	return p
}

func (p *PgSQLUpdateQuery) Set(column string, value interface{}) common.UpdateQuery {
	if p.model != nil && !reflection.IsColumnWritable(p.model, column) {
		return p
	}
	p.sets[column] = value
	return p
}

func (p *PgSQLUpdateQuery) SetMap(values map[string]interface{}) common.UpdateQuery {
	pkName := ""
	if p.model != nil {
		pkName = reflection.GetPrimaryKeyName(p.model)
	}

	for column, value := range values {
		if pkName != "" && column == pkName {
			continue
		}
		if p.model != nil && !reflection.IsColumnWritable(p.model, column) {
			continue
		}
		p.sets[column] = value
	}
	return p
}

func (p *PgSQLUpdateQuery) Where(query string, args ...interface{}) common.UpdateQuery {
	query = p.replacePlaceholders(query, len(args))
	p.whereClauses = append(p.whereClauses, query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLUpdateQuery) Returning(columns ...string) common.UpdateQuery {
	p.returning = columns
	return p
}

func (p *PgSQLUpdateQuery) replacePlaceholders(query string, argCount int) string {
	result := query
	for i := 0; i < argCount; i++ {
		p.paramCounter++
		placeholder := fmt.Sprintf("$%d", p.paramCounter)
		result = strings.Replace(result, "?", placeholder, 1)
	}
	return result
}

func (p *PgSQLUpdateQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLUpdateQuery.Exec", r)
		}
	}()

	if len(p.sets) == 0 {
		return nil, fmt.Errorf("no values to update")
	}

	setClauses := make([]string, 0, len(p.sets))
	setArgs := make([]interface{}, 0, len(p.sets))

	// SET parameters start at $1
	i := 1
	for col, val := range p.sets {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		setArgs = append(setArgs, val)
		i++
	}

	query := fmt.Sprintf("UPDATE %s SET %s",
		p.tableName,
		strings.Join(setClauses, ", "))

	// Update WHERE clause parameter numbers to continue after SET parameters
	if len(p.whereClauses) > 0 {
		updatedWhereClauses := make([]string, 0, len(p.whereClauses))
		for _, whereClause := range p.whereClauses {
			// Find and replace parameter placeholders
			updatedClause := whereClause
			paramNum := i
			// Count how many parameters are in this WHERE clause
			placeholderCount := strings.Count(whereClause, "$")
			for j := 0; j < placeholderCount; j++ {
				oldParam := fmt.Sprintf("$%d", j+1)
				newParam := fmt.Sprintf("$%d", paramNum)
				updatedClause = strings.Replace(updatedClause, oldParam, newParam, 1)
				paramNum++
			}
			updatedWhereClauses = append(updatedWhereClauses, updatedClause)
			i = paramNum
		}
		p.whereClauses = updatedWhereClauses
	}

	// All arguments: SET values first, then WHERE values
	// Create a new slice to avoid modifying setArgs
	allArgs := make([]interface{}, len(setArgs)+len(p.args))
	copy(allArgs, setArgs)
	copy(allArgs[len(setArgs):], p.args)

	if len(p.whereClauses) > 0 {
		query += " WHERE " + strings.Join(p.whereClauses, " AND ")
	}

	if len(p.returning) > 0 {
		query += " RETURNING " + strings.Join(p.returning, ", ")
	}

	logger.Debug("PgSQL UPDATE: %s [args: %v]", query, allArgs)

	var result sql.Result
	if p.tx != nil {
		result, err = p.tx.ExecContext(ctx, query, allArgs...)
	} else {
		result, err = p.db.ExecContext(ctx, query, allArgs...)
	}

	if err != nil {
		logger.Error("PgSQL UPDATE failed: %v", err)
		return nil, err
	}

	return &PgSQLResult{result: result}, nil
}

// PgSQLDeleteQuery implements DeleteQuery for PostgreSQL
type PgSQLDeleteQuery struct {
	db           *sql.DB
	tx           *sql.Tx
	tableName    string
	whereClauses []string
	args         []interface{}
	paramCounter int
}

func (p *PgSQLDeleteQuery) Model(model interface{}) common.DeleteQuery {
	if provider, ok := model.(common.TableNameProvider); ok {
		p.tableName = provider.TableName()
	}
	return p
}

func (p *PgSQLDeleteQuery) Table(table string) common.DeleteQuery {
	p.tableName = table
	return p
}

func (p *PgSQLDeleteQuery) Where(query string, args ...interface{}) common.DeleteQuery {
	query = p.replacePlaceholders(query, len(args))
	p.whereClauses = append(p.whereClauses, query)
	p.args = append(p.args, args...)
	return p
}

func (p *PgSQLDeleteQuery) replacePlaceholders(query string, argCount int) string {
	result := query
	for i := 0; i < argCount; i++ {
		p.paramCounter++
		placeholder := fmt.Sprintf("$%d", p.paramCounter)
		result = strings.Replace(result, "?", placeholder, 1)
	}
	return result
}

func (p *PgSQLDeleteQuery) Exec(ctx context.Context) (res common.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.HandlePanic("PgSQLDeleteQuery.Exec", r)
		}
	}()

	query := fmt.Sprintf("DELETE FROM %s", p.tableName)

	if len(p.whereClauses) > 0 {
		query += " WHERE " + strings.Join(p.whereClauses, " AND ")
	}

	logger.Debug("PgSQL DELETE: %s [args: %v]", query, p.args)

	var result sql.Result
	if p.tx != nil {
		result, err = p.tx.ExecContext(ctx, query, p.args...)
	} else {
		result, err = p.db.ExecContext(ctx, query, p.args...)
	}

	if err != nil {
		logger.Error("PgSQL DELETE failed: %v", err)
		return nil, err
	}

	return &PgSQLResult{result: result}, nil
}

// PgSQLResult implements Result for PostgreSQL
type PgSQLResult struct {
	result sql.Result
}

func (p *PgSQLResult) RowsAffected() int64 {
	if p.result == nil {
		return 0
	}
	rows, _ := p.result.RowsAffected()
	return rows
}

func (p *PgSQLResult) LastInsertId() (int64, error) {
	if p.result == nil {
		return 0, nil
	}
	return p.result.LastInsertId()
}

// PgSQLTxAdapter wraps a PostgreSQL transaction
type PgSQLTxAdapter struct {
	tx *sql.Tx
}

func (p *PgSQLTxAdapter) NewSelect() common.SelectQuery {
	return &PgSQLSelectQuery{
		tx:      p.tx,
		columns: []string{"*"},
		args:    make([]interface{}, 0),
	}
}

func (p *PgSQLTxAdapter) NewInsert() common.InsertQuery {
	return &PgSQLInsertQuery{
		tx:     p.tx,
		values: make(map[string]interface{}),
	}
}

func (p *PgSQLTxAdapter) NewUpdate() common.UpdateQuery {
	return &PgSQLUpdateQuery{
		tx:           p.tx,
		sets:         make(map[string]interface{}),
		args:         make([]interface{}, 0),
		whereClauses: make([]string, 0),
	}
}

func (p *PgSQLTxAdapter) NewDelete() common.DeleteQuery {
	return &PgSQLDeleteQuery{
		tx:           p.tx,
		args:         make([]interface{}, 0),
		whereClauses: make([]string, 0),
	}
}

func (p *PgSQLTxAdapter) Exec(ctx context.Context, query string, args ...interface{}) (common.Result, error) {
	logger.Debug("PgSQL Tx Exec: %s [args: %v]", query, args)
	result, err := p.tx.ExecContext(ctx, query, args...)
	if err != nil {
		logger.Error("PgSQL Tx Exec failed: %v", err)
		return nil, err
	}
	return &PgSQLResult{result: result}, nil
}

func (p *PgSQLTxAdapter) Query(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	logger.Debug("PgSQL Tx Query: %s [args: %v]", query, args)
	rows, err := p.tx.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error("PgSQL Tx Query failed: %v", err)
		return err
	}
	defer rows.Close()

	return scanRows(rows, dest)
}

func (p *PgSQLTxAdapter) BeginTx(ctx context.Context) (common.Database, error) {
	return nil, fmt.Errorf("nested transactions not supported")
}

func (p *PgSQLTxAdapter) CommitTx(ctx context.Context) error {
	return p.tx.Commit()
}

func (p *PgSQLTxAdapter) RollbackTx(ctx context.Context) error {
	return p.tx.Rollback()
}

func (p *PgSQLTxAdapter) RunInTransaction(ctx context.Context, fn func(common.Database) error) error {
	return fn(p)
}

func (p *PgSQLTxAdapter) GetUnderlyingDB() interface{} {
	return p.tx
}

// applyJoinPreloads adds JOINs for relationships that should use JOIN strategy
func (p *PgSQLSelectQuery) applyJoinPreloads() {
	for _, preload := range p.preloads {
		if !preload.useJoin {
			continue
		}

		// Build JOIN based on relationship metadata
		meta := p.getRelationMetadata(preload.relation)
		if meta == nil {
			logger.Warn("Cannot determine relationship metadata for '%s'", preload.relation)
			continue
		}

		// Build the JOIN clause
		relationAlias := strings.ToLower(preload.relation)
		joinClause := fmt.Sprintf("%s AS %s ON %s.%s = %s.%s",
			meta.targetTable,
			relationAlias,
			p.tableAlias,
			meta.foreignKey,
			relationAlias,
			meta.targetKey,
		)

		logger.Debug("Adding LEFT JOIN for relation '%s': %s", preload.relation, joinClause)
		p.joins = append(p.joins, "LEFT JOIN "+joinClause)

		// Apply any custom conditions through applyFuncs
		// Note: These would need to be integrated into the WHERE clause
		// For simplicity, we're logging a warning if custom conditions are present
		if len(preload.applyFuncs) > 0 {
			logger.Warn("Custom conditions in JoinRelation not yet fully implemented")
		}
	}
}

// applySubqueryPreloads executes separate queries for has-many and many-to-many relationships
func (p *PgSQLSelectQuery) applySubqueryPreloads(ctx context.Context, dest interface{}) error {
	// Get all preloads that don't use JOIN
	subqueryPreloads := make([]preloadConfig, 0)
	for _, preload := range p.preloads {
		if !preload.useJoin {
			subqueryPreloads = append(subqueryPreloads, preload)
		}
	}

	if len(subqueryPreloads) == 0 {
		return nil
	}

	// Use reflection to process the destination
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}

	destValue = destValue.Elem()

	// Handle slice of structs
	if destValue.Kind() == reflect.Slice {
		for i := 0; i < destValue.Len(); i++ {
			elem := destValue.Index(i)
			if err := p.loadPreloadsForRecord(ctx, elem, subqueryPreloads); err != nil {
				logger.Warn("Failed to load preloads for record %d: %v", i, err)
			}
		}
		return nil
	}

	// Handle single struct
	if destValue.Kind() == reflect.Struct {
		return p.loadPreloadsForRecord(ctx, destValue, subqueryPreloads)
	}

	return nil
}

// loadPreloadsForRecord loads all preload relationships for a single record
func (p *PgSQLSelectQuery) loadPreloadsForRecord(ctx context.Context, record reflect.Value, preloads []preloadConfig) error {
	if record.Kind() == reflect.Ptr {
		if record.IsNil() {
			return nil
		}
		record = record.Elem()
	}

	for _, preload := range preloads {
		field := record.FieldByName(preload.relation)
		if !field.IsValid() || !field.CanSet() {
			logger.Warn("Field '%s' not found or cannot be set", preload.relation)
			continue
		}

		meta := p.getRelationMetadataFromField(record.Type(), preload.relation)
		if meta == nil {
			logger.Warn("Cannot determine relationship metadata for '%s'", preload.relation)
			continue
		}

		// Get the foreign key value from the parent record
		fkField := record.FieldByName(meta.foreignKey)
		if !fkField.IsValid() {
			logger.Warn("Foreign key field '%s' not found", meta.foreignKey)
			continue
		}

		fkValue := fkField.Interface()

		// Build and execute the preload query
		err := p.executePreloadQuery(ctx, field, meta, fkValue, preload)
		if err != nil {
			logger.Warn("Failed to execute preload query for '%s': %v", preload.relation, err)
		}
	}

	return nil
}

// executePreloadQuery executes a query to load a relationship
func (p *PgSQLSelectQuery) executePreloadQuery(ctx context.Context, field reflect.Value, meta *relationMetadata, fkValue interface{}, preload preloadConfig) error {
	// Create a new select query for the related table
	var db common.Database
	if p.tx != nil {
		db = &PgSQLTxAdapter{tx: p.tx}
	} else {
		db = &PgSQLAdapter{db: p.db}
	}

	query := db.NewSelect().
		Table(meta.targetTable).
		Where(fmt.Sprintf("%s = ?", meta.targetKey), fkValue)

	// Apply custom functions
	for _, applyFunc := range preload.applyFuncs {
		if applyFunc != nil {
			query = applyFunc(query)
		}
	}

	// Determine if this is a slice (has-many) or single struct (belongs-to/has-one)
	if field.Kind() == reflect.Slice {
		// Create a new slice to hold results
		sliceType := field.Type()
		results := reflect.New(sliceType).Elem()

		// Execute query
		err := query.Scan(ctx, results.Addr().Interface())
		if err != nil {
			return err
		}

		// Set the field
		field.Set(results)
	} else {
		// Single struct - create a pointer if needed
		var target reflect.Value
		if field.Kind() == reflect.Ptr {
			target = reflect.New(field.Type().Elem())
		} else {
			target = reflect.New(field.Type())
		}

		// Execute query with LIMIT 1
		err := query.Limit(1).Scan(ctx, target.Interface())
		if err != nil && err != sql.ErrNoRows {
			return err
		}

		// Set the field
		if field.Kind() == reflect.Ptr {
			field.Set(target)
		} else {
			field.Set(target.Elem())
		}
	}

	return nil
}

// getRelationMetadata extracts relationship metadata from the model
func (p *PgSQLSelectQuery) getRelationMetadata(fieldName string) *relationMetadata {
	if p.model == nil {
		return nil
	}

	modelType := reflect.TypeOf(p.model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	return p.getRelationMetadataFromField(modelType, fieldName)
}

// getRelationMetadataFromField extracts relationship metadata from a type
func (p *PgSQLSelectQuery) getRelationMetadataFromField(modelType reflect.Type, fieldName string) *relationMetadata {
	if modelType.Kind() != reflect.Struct {
		return nil
	}

	field, found := modelType.FieldByName(fieldName)
	if !found {
		return nil
	}

	meta := &relationMetadata{
		fieldName:    fieldName,
		relationType: reflection.GetRelationType(reflect.New(modelType).Interface(), fieldName),
	}

	// Parse struct tags to get foreign key and target table
	bunTag := field.Tag.Get("bun")
	if bunTag != "" {
		// Parse bun tags: rel:has-many,join:user_id=id
		parts := strings.Split(bunTag, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "join:") {
				// Parse join condition: join:user_id=id
				joinSpec := strings.TrimPrefix(part, "join:")
				if strings.Contains(joinSpec, "=") {
					keys := strings.Split(joinSpec, "=")
					if len(keys) == 2 {
						meta.foreignKey = strings.TrimSpace(keys[0])
						meta.targetKey = strings.TrimSpace(keys[1])
					}
				}
			}
		}
	}

	// Try to determine target table from field type
	fieldType := field.Type
	if fieldType.Kind() == reflect.Slice {
		fieldType = fieldType.Elem()
	}
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}

	if fieldType.Kind() == reflect.Struct {
		// Try to get table name from the related model
		relatedModel := reflect.New(fieldType).Interface()
		if provider, ok := relatedModel.(common.TableNameProvider); ok {
			meta.targetTable = provider.TableName()
		}
	}

	// Set defaults if not found
	if meta.foreignKey == "" {
		meta.foreignKey = "id"
	}
	if meta.targetKey == "" {
		meta.targetKey = "id"
	}

	return meta
}

// scanRows scans database rows into the destination using reflection
func scanRows(rows *sql.Rows, dest interface{}) error {
	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Get destination type
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return fmt.Errorf("dest must be a pointer")
	}

	destValue = destValue.Elem()

	// Handle map slice: []map[string]interface{}
	if destValue.Type() == reflect.TypeOf([]map[string]interface{}{}) {
		return scanRowsToMapSlice(rows, columns, destValue)
	}

	// Handle struct slice: []MyStruct or []*MyStruct
	if destValue.Kind() == reflect.Slice {
		return scanRowsToStructSlice(rows, columns, destValue)
	}

	// Handle single struct: MyStruct or *MyStruct
	if destValue.Kind() == reflect.Struct {
		return scanRowsToSingleStruct(rows, columns, destValue)
	}

	return fmt.Errorf("unsupported destination type: %T", dest)
}

// scanRowsToMapSlice scans rows into []map[string]interface{}
func scanRowsToMapSlice(rows *sql.Rows, columns []string, destValue reflect.Value) error {
	results := make([]map[string]interface{}, 0)

	for rows.Next() {
		// Create holders for values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		err := rows.Scan(valuePtrs...)
		if err != nil {
			return err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	destValue.Set(reflect.ValueOf(results))
	return rows.Err()
}

// scanRowsToStructSlice scans rows into a slice of structs
func scanRowsToStructSlice(rows *sql.Rows, columns []string, destValue reflect.Value) error {
	elemType := destValue.Type().Elem()
	isPtr := elemType.Kind() == reflect.Ptr

	if isPtr {
		elemType = elemType.Elem()
	}

	if elemType.Kind() != reflect.Struct {
		return fmt.Errorf("slice element must be a struct, got %v", elemType.Kind())
	}

	// Build column-to-field mapping
	fieldMap := buildFieldMap(elemType, columns)

	for rows.Next() {
		// Create a new instance of the struct
		elemValue := reflect.New(elemType).Elem()

		// Create scan targets
		scanTargets := make([]interface{}, len(columns))
		for i, col := range columns {
			if fieldInfo, ok := fieldMap[col]; ok {
				field := elemValue.FieldByIndex(fieldInfo.Index)
				if field.CanSet() {
					scanTargets[i] = field.Addr().Interface()
					continue
				}
			}
			// Use a dummy variable for unmapped columns
			var dummy interface{}
			scanTargets[i] = &dummy
		}

		err := rows.Scan(scanTargets...)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		// Append to slice
		if isPtr {
			destValue.Set(reflect.Append(destValue, elemValue.Addr()))
		} else {
			destValue.Set(reflect.Append(destValue, elemValue))
		}
	}

	return rows.Err()
}

// scanRowsToSingleStruct scans a single row into a struct
func scanRowsToSingleStruct(rows *sql.Rows, columns []string, destValue reflect.Value) error {
	if !rows.Next() {
		return sql.ErrNoRows
	}

	// Build column-to-field mapping
	fieldMap := buildFieldMap(destValue.Type(), columns)

	// Create scan targets
	scanTargets := make([]interface{}, len(columns))
	for i, col := range columns {
		if fieldInfo, ok := fieldMap[col]; ok {
			field := destValue.FieldByIndex(fieldInfo.Index)
			if field.CanSet() {
				scanTargets[i] = field.Addr().Interface()
				continue
			}
		}
		// Use a dummy variable for unmapped columns
		var dummy interface{}
		scanTargets[i] = &dummy
	}

	err := rows.Scan(scanTargets...)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return rows.Err()
}

// fieldInfo holds information about a struct field
type fieldInfo struct {
	Index []int
	Name  string
}

// buildFieldMap creates a mapping from column names to struct fields
func buildFieldMap(structType reflect.Type, _ []string) map[string]fieldInfo {
	fieldMap := make(map[string]fieldInfo)

	// Iterate through struct fields
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get column name from struct tag or field name
		colName := field.Name

		// Check for bun tag
		if bunTag := field.Tag.Get("bun"); bunTag != "" {
			parts := strings.Split(bunTag, ",")
			if len(parts) > 0 && parts[0] != "" && parts[0] != "-" {
				colName = parts[0]
			}
		}

		// Check for db tag (common convention)
		if dbTag := field.Tag.Get("db"); dbTag != "" && dbTag != "-" {
			colName = dbTag
		}

		// Convert to lowercase for case-insensitive matching
		colNameLower := strings.ToLower(colName)

		fieldMap[colNameLower] = fieldInfo{
			Index: field.Index,
			Name:  field.Name,
		}

		// Also map by exact field name
		fieldMap[strings.ToLower(field.Name)] = fieldInfo{
			Index: field.Index,
			Name:  field.Name,
		}
	}

	return fieldMap
}
