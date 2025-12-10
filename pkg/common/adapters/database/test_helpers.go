package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHelper provides utilities for database testing
type TestHelper struct {
	DB      *sql.DB
	Adapter *PgSQLAdapter
	t       *testing.T
}

// NewTestHelper creates a new test helper
func NewTestHelper(t *testing.T, db *sql.DB) *TestHelper {
	return &TestHelper{
		DB:      db,
		Adapter: NewPgSQLAdapter(db),
		t:       t,
	}
}

// CleanupTables truncates all test tables
func (h *TestHelper) CleanupTables() {
	ctx := context.Background()
	tables := []string{"comments", "posts", "users"}

	for _, table := range tables {
		_, err := h.DB.ExecContext(ctx, "TRUNCATE TABLE "+table+" CASCADE")
		require.NoError(h.t, err)
	}
}

// InsertUser inserts a test user and returns the ID
func (h *TestHelper) InsertUser(name, email string, age int) int {
	ctx := context.Background()
	result, err := h.Adapter.NewInsert().
		Table("users").
		Value("name", name).
		Value("email", email).
		Value("age", age).
		Exec(ctx)

	require.NoError(h.t, err)
	id, _ := result.LastInsertId()
	return int(id)
}

// InsertPost inserts a test post and returns the ID
func (h *TestHelper) InsertPost(userID int, title, content string, published bool) int {
	ctx := context.Background()
	result, err := h.Adapter.NewInsert().
		Table("posts").
		Value("user_id", userID).
		Value("title", title).
		Value("content", content).
		Value("published", published).
		Exec(ctx)

	require.NoError(h.t, err)
	id, _ := result.LastInsertId()
	return int(id)
}

// InsertComment inserts a test comment and returns the ID
func (h *TestHelper) InsertComment(postID int, content string) int {
	ctx := context.Background()
	result, err := h.Adapter.NewInsert().
		Table("comments").
		Value("post_id", postID).
		Value("content", content).
		Exec(ctx)

	require.NoError(h.t, err)
	id, _ := result.LastInsertId()
	return int(id)
}

// AssertUserExists checks if a user exists by email
func (h *TestHelper) AssertUserExists(email string) {
	ctx := context.Background()
	exists, err := h.Adapter.NewSelect().
		Table("users").
		Where("email = ?", email).
		Exists(ctx)

	require.NoError(h.t, err)
	require.True(h.t, exists, "User with email %s should exist", email)
}

// AssertUserCount asserts the number of users
func (h *TestHelper) AssertUserCount(expected int) {
	ctx := context.Background()
	count, err := h.Adapter.NewSelect().
		Table("users").
		Count(ctx)

	require.NoError(h.t, err)
	require.Equal(h.t, expected, count)
}

// GetUserByEmail retrieves a user by email
func (h *TestHelper) GetUserByEmail(email string) map[string]interface{} {
	ctx := context.Background()
	var results []map[string]interface{}
	err := h.Adapter.NewSelect().
		Table("users").
		Where("email = ?", email).
		Scan(ctx, &results)

	require.NoError(h.t, err)
	require.Len(h.t, results, 1, "Expected exactly one user with email %s", email)
	return results[0]
}

// BeginTestTransaction starts a transaction for testing
func (h *TestHelper) BeginTestTransaction() (*PgSQLTxAdapter, func()) {
	ctx := context.Background()
	tx, err := h.DB.BeginTx(ctx, nil)
	require.NoError(h.t, err)

	adapter := &PgSQLTxAdapter{tx: tx}
	cleanup := func() {
		tx.Rollback()
	}

	return adapter, cleanup
}
