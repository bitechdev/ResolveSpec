package database

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Test models
type TestUser struct {
	ID    int    `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
	Age   int    `db:"age"`
}

func (u TestUser) TableName() string {
	return "users"
}

type TestPost struct {
	ID       int        `db:"id"`
	Title    string     `db:"title"`
	Content  string     `db:"content"`
	UserID   int        `db:"user_id"`
	User     *TestUser  `bun:"rel:belongs-to,join:user_id=id"`
	Comments []TestComment `bun:"rel:has-many,join:id=post_id"`
}

func (p TestPost) TableName() string {
	return "posts"
}

type TestComment struct {
	ID      int    `db:"id"`
	Content string `db:"content"`
	PostID  int    `db:"post_id"`
}

func (c TestComment) TableName() string {
	return "comments"
}

// TestNewPgSQLAdapter tests adapter creation
func TestNewPgSQLAdapter(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	assert.NotNil(t, adapter)
	assert.Equal(t, db, adapter.db)
}

// TestPgSQLSelectQuery_BuildSQL tests SQL query building
func TestPgSQLSelectQuery_BuildSQL(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*PgSQLSelectQuery)
		expected string
	}{
		{
			name: "simple select",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
			},
			expected: "SELECT * FROM users",
		},
		{
			name: "select with columns",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
				q.columns = []string{"id", "name", "email"}
			},
			expected: "SELECT id, name, email FROM users",
		},
		{
			name: "select with where",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
				q.whereClauses = []string{"age > $1"}
				q.args = []interface{}{18}
			},
			expected: "SELECT * FROM users WHERE (age > $1)",
		},
		{
			name: "select with order and limit",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
				q.orderBy = []string{"created_at DESC"}
				q.limit = 10
				q.offset = 5
			},
			expected: "SELECT * FROM users ORDER BY created_at DESC LIMIT 10 OFFSET 5",
		},
		{
			name: "select with join",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
				q.joins = []string{"LEFT JOIN posts ON posts.user_id = users.id"}
			},
			expected: "SELECT * FROM users LEFT JOIN posts ON posts.user_id = users.id",
		},
		{
			name: "select with group and having",
			setup: func(q *PgSQLSelectQuery) {
				q.tableName = "users"
				q.groupBy = []string{"country"}
				q.havingClauses = []string{"COUNT(*) > $1"}
				q.args = []interface{}{5}
			},
			expected: "SELECT * FROM users GROUP BY country HAVING COUNT(*) > $1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &PgSQLSelectQuery{
				columns: []string{"*"},
			}
			tt.setup(q)
			sql := q.buildSQL()
			assert.Equal(t, tt.expected, sql)
		})
	}
}

// TestPgSQLSelectQuery_ReplacePlaceholders tests placeholder replacement
func TestPgSQLSelectQuery_ReplacePlaceholders(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		argCount     int
		paramCounter int
		expected     string
	}{
		{
			name:         "single placeholder",
			query:        "age > ?",
			argCount:     1,
			paramCounter: 0,
			expected:     "age > $1",
		},
		{
			name:         "multiple placeholders",
			query:        "age > ? AND status = ?",
			argCount:     2,
			paramCounter: 0,
			expected:     "age > $1 AND status = $2",
		},
		{
			name:         "with existing counter",
			query:        "name = ?",
			argCount:     1,
			paramCounter: 5,
			expected:     "name = $6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &PgSQLSelectQuery{paramCounter: tt.paramCounter}
			result := q.replacePlaceholders(tt.query, tt.argCount)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestPgSQLSelectQuery_Chaining tests method chaining
func TestPgSQLSelectQuery_Chaining(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	query := adapter.NewSelect().
		Table("users").
		Column("id", "name").
		Where("age > ?", 18).
		Order("name ASC").
		Limit(10).
		Offset(5)

	pgQuery := query.(*PgSQLSelectQuery)
	assert.Equal(t, "users", pgQuery.tableName)
	assert.Equal(t, []string{"id", "name"}, pgQuery.columns)
	assert.Len(t, pgQuery.whereClauses, 1)
	assert.Equal(t, []string{"name ASC"}, pgQuery.orderBy)
	assert.Equal(t, 10, pgQuery.limit)
	assert.Equal(t, 5, pgQuery.offset)
}

// TestPgSQLSelectQuery_Model tests model setting
func TestPgSQLSelectQuery_Model(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	user := &TestUser{}
	query := adapter.NewSelect().Model(user)

	pgQuery := query.(*PgSQLSelectQuery)
	assert.Equal(t, "users", pgQuery.tableName)
	assert.Equal(t, user, pgQuery.model)
}

// TestScanRowsToStructSlice tests scanning rows into struct slice
func TestScanRowsToStructSlice(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "age"}).
		AddRow(1, "John Doe", "john@example.com", 25).
		AddRow(2, "Jane Smith", "jane@example.com", 30)

	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	var users []TestUser
	err = adapter.NewSelect().
		Table("users").
		Scan(ctx, &users)

	require.NoError(t, err)
	assert.Len(t, users, 2)
	assert.Equal(t, "John Doe", users[0].Name)
	assert.Equal(t, "jane@example.com", users[1].Email)
	assert.Equal(t, 30, users[1].Age)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestScanRowsToStructSlicePointers tests scanning rows into pointer slice
func TestScanRowsToStructSlicePointers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "age"}).
		AddRow(1, "John Doe", "john@example.com", 25)

	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	var users []*TestUser
	err = adapter.NewSelect().
		Table("users").
		Scan(ctx, &users)

	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.NotNil(t, users[0])
	assert.Equal(t, "John Doe", users[0].Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestScanRowsToSingleStruct tests scanning a single row
func TestScanRowsToSingleStruct(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "age"}).
		AddRow(1, "John Doe", "john@example.com", 25)

	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	var user TestUser
	err = adapter.NewSelect().
		Table("users").
		Where("id = ?", 1).
		Scan(ctx, &user)

	require.NoError(t, err)
	assert.Equal(t, 1, user.ID)
	assert.Equal(t, "John Doe", user.Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestScanRowsToMapSlice tests scanning into map slice
func TestScanRowsToMapSlice(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email"}).
		AddRow(1, "John Doe", "john@example.com").
		AddRow(2, "Jane Smith", "jane@example.com")

	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	var results []map[string]interface{}
	err = adapter.NewSelect().
		Table("users").
		Scan(ctx, &results)

	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, int64(1), results[0]["id"])
	assert.Equal(t, "John Doe", results[0]["name"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLInsertQuery_Exec tests insert query execution
func TestPgSQLInsertQuery_Exec(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("INSERT INTO users").
		WithArgs("John Doe", "john@example.com", 25).
		WillReturnResult(sqlmock.NewResult(1, 1))

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	result, err := adapter.NewInsert().
		Table("users").
		Value("name", "John Doe").
		Value("email", "john@example.com").
		Value("age", 25).
		Exec(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(1), result.RowsAffected())

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLUpdateQuery_Exec tests update query execution
func TestPgSQLUpdateQuery_Exec(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Note: Args order is SET values first, then WHERE values
	mock.ExpectExec("UPDATE users SET name = \\$1 WHERE id = \\$2").
		WithArgs("Jane Doe", 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	result, err := adapter.NewUpdate().
		Table("users").
		Set("name", "Jane Doe").
		Where("id = ?", 1).
		Exec(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(1), result.RowsAffected())

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLDeleteQuery_Exec tests delete query execution
func TestPgSQLDeleteQuery_Exec(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("DELETE FROM users WHERE id = \\$1").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	result, err := adapter.NewDelete().
		Table("users").
		Where("id = ?", 1).
		Exec(ctx)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, int64(1), result.RowsAffected())

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLSelectQuery_Count tests count query
func TestPgSQLSelectQuery_Count(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"count"}).AddRow(42)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	count, err := adapter.NewSelect().
		Table("users").
		Count(ctx)

	require.NoError(t, err)
	assert.Equal(t, 42, count)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLSelectQuery_Exists tests exists query
func TestPgSQLSelectQuery_Exists(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	exists, err := adapter.NewSelect().
		Table("users").
		Where("email = ?", "john@example.com").
		Exists(ctx)

	require.NoError(t, err)
	assert.True(t, exists)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLAdapter_Transaction tests transaction handling
func TestPgSQLAdapter_Transaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	err = adapter.RunInTransaction(ctx, func(tx common.Database) error {
		_, err := tx.NewInsert().
			Table("users").
			Value("name", "John").
			Exec(ctx)
		return err
	})

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPgSQLAdapter_TransactionRollback tests transaction rollback
func TestPgSQLAdapter_TransactionRollback(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO users").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	err = adapter.RunInTransaction(ctx, func(tx common.Database) error {
		_, err := tx.NewInsert().
			Table("users").
			Value("name", "John").
			Exec(ctx)
		return err
	})

	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBuildFieldMap tests field mapping construction
func TestBuildFieldMap(t *testing.T) {
	userType := reflect.TypeOf(TestUser{})
	fieldMap := buildFieldMap(userType, nil)

	assert.NotEmpty(t, fieldMap)

	// Check that fields are mapped
	assert.Contains(t, fieldMap, "id")
	assert.Contains(t, fieldMap, "name")
	assert.Contains(t, fieldMap, "email")
	assert.Contains(t, fieldMap, "age")

	// Check field info
	idInfo := fieldMap["id"]
	assert.Equal(t, "ID", idInfo.Name)
}

// TestGetRelationMetadata tests relationship metadata extraction
func TestGetRelationMetadata(t *testing.T) {
	q := &PgSQLSelectQuery{
		model: &TestPost{},
	}

	// Test belongs-to relationship
	meta := q.getRelationMetadata("User")
	assert.NotNil(t, meta)
	assert.Equal(t, "User", meta.fieldName)

	// Test has-many relationship
	meta = q.getRelationMetadata("Comments")
	assert.NotNil(t, meta)
	assert.Equal(t, "Comments", meta.fieldName)
}

// TestPreloadConfiguration tests preload configuration
func TestPreloadConfiguration(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	adapter := NewPgSQLAdapter(db)

	// Test Preload
	query := adapter.NewSelect().
		Model(&TestPost{}).
		Table("posts").
		Preload("User")

	pgQuery := query.(*PgSQLSelectQuery)
	assert.Len(t, pgQuery.preloads, 1)
	assert.Equal(t, "User", pgQuery.preloads[0].relation)
	assert.False(t, pgQuery.preloads[0].useJoin)

	// Test PreloadRelation
	query = adapter.NewSelect().
		Model(&TestPost{}).
		Table("posts").
		PreloadRelation("Comments")

	pgQuery = query.(*PgSQLSelectQuery)
	assert.Len(t, pgQuery.preloads, 1)
	assert.Equal(t, "Comments", pgQuery.preloads[0].relation)

	// Test JoinRelation
	query = adapter.NewSelect().
		Model(&TestPost{}).
		Table("posts").
		JoinRelation("User")

	pgQuery = query.(*PgSQLSelectQuery)
	assert.Len(t, pgQuery.preloads, 1)
	assert.Equal(t, "User", pgQuery.preloads[0].relation)
	assert.True(t, pgQuery.preloads[0].useJoin)
}

// TestScanModel tests ScanModel functionality
func TestScanModel(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "name", "email", "age"}).
		AddRow(1, "John Doe", "john@example.com", 25)

	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	user := &TestUser{}
	err = adapter.NewSelect().
		Model(user).
		Table("users").
		Where("id = ?", 1).
		ScanModel(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, user.ID)
	assert.Equal(t, "John Doe", user.Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRawSQL tests raw SQL execution
func TestRawSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Test Exec
	mock.ExpectExec("CREATE TABLE test").WillReturnResult(sqlmock.NewResult(0, 0))

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	_, err = adapter.Exec(ctx, "CREATE TABLE test (id INT)")
	require.NoError(t, err)

	// Test Query
	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Test")
	mock.ExpectQuery("SELECT (.+) FROM test").WillReturnRows(rows)

	var results []map[string]interface{}
	err = adapter.Query(ctx, &results, "SELECT * FROM test WHERE id = $1", 1)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	assert.NoError(t, mock.ExpectationsWereMet())
}
