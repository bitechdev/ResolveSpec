package database

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Example demonstrates how to use the PgSQL adapter
func ExamplePgSQLAdapter() error {
	// Connect to PostgreSQL database
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create the PgSQL adapter
	adapter := NewPgSQLAdapter(db)

	// Enable query debugging (optional)
	adapter.EnableQueryDebug()

	ctx := context.Background()

	// Example 1: Simple SELECT query
	var results []map[string]interface{}
	err = adapter.NewSelect().
		Table("users").
		Where("age > ?", 18).
		Order("created_at DESC").
		Limit(10).
		Scan(ctx, &results)
	if err != nil {
		return fmt.Errorf("select failed: %w", err)
	}

	// Example 2: INSERT query
	result, err := adapter.NewInsert().
		Table("users").
		Value("name", "John Doe").
		Value("email", "john@example.com").
		Value("age", 25).
		Returning("id").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert failed: %w", err)
	}
	fmt.Printf("Rows affected: %d\n", result.RowsAffected())

	// Example 3: UPDATE query
	result, err = adapter.NewUpdate().
		Table("users").
		Set("name", "Jane Doe").
		Where("id = ?", 1).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	fmt.Printf("Rows updated: %d\n", result.RowsAffected())

	// Example 4: DELETE query
	result, err = adapter.NewDelete().
		Table("users").
		Where("age < ?", 18).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	fmt.Printf("Rows deleted: %d\n", result.RowsAffected())

	// Example 5: Using transactions
	err = adapter.RunInTransaction(ctx, func(tx common.Database) error {
		// Insert a new user
		_, err := tx.NewInsert().
			Table("users").
			Value("name", "Transaction User").
			Value("email", "tx@example.com").
			Exec(ctx)
		if err != nil {
			return err
		}

		// Update another user
		_, err = tx.NewUpdate().
			Table("users").
			Set("verified", true).
			Where("email = ?", "tx@example.com").
			Exec(ctx)
		if err != nil {
			return err
		}

		// Both operations succeed or both rollback
		return nil
	})
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}

	// Example 6: JOIN query
	err = adapter.NewSelect().
		Table("users u").
		Column("u.id", "u.name", "p.title as post_title").
		LeftJoin("posts p ON p.user_id = u.id").
		Where("u.active = ?", true).
		Scan(ctx, &results)
	if err != nil {
		return fmt.Errorf("join query failed: %w", err)
	}

	// Example 7: Aggregation query
	count, err := adapter.NewSelect().
		Table("users").
		Where("active = ?", true).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count failed: %w", err)
	}
	fmt.Printf("Active users: %d\n", count)

	// Example 8: Raw SQL execution
	_, err = adapter.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)")
	if err != nil {
		return fmt.Errorf("raw exec failed: %w", err)
	}

	// Example 9: Raw SQL query
	var users []map[string]interface{}
	err = adapter.Query(ctx, &users, "SELECT * FROM users WHERE age > $1 LIMIT $2", 18, 10)
	if err != nil {
		return fmt.Errorf("raw query failed: %w", err)
	}

	return nil
}

// User is an example model
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// TableName implements common.TableNameProvider
func (u User) TableName() string {
	return "users"
}

// ExampleWithModel demonstrates using models with the PgSQL adapter
func ExampleWithModel() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	// Use model with adapter
	user := User{}
	err = adapter.NewSelect().
		Model(&user).
		Where("id = ?", 1).
		Scan(ctx, &user)

	return err
}
