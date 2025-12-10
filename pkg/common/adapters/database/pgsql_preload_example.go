package database

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Example models for demonstrating preload functionality

// Author model - has many Posts
type Author struct {
	ID    int     `db:"id"`
	Name  string  `db:"name"`
	Email string  `db:"email"`
	Posts []*Post `bun:"rel:has-many,join:id=author_id"`
}

func (a Author) TableName() string {
	return "authors"
}

// Post model - belongs to Author, has many Comments
type Post struct {
	ID       int        `db:"id"`
	Title    string     `db:"title"`
	Content  string     `db:"content"`
	AuthorID int        `db:"author_id"`
	Author   *Author    `bun:"rel:belongs-to,join:author_id=id"`
	Comments []*Comment `bun:"rel:has-many,join:id=post_id"`
}

func (p Post) TableName() string {
	return "posts"
}

// Comment model - belongs to Post
type Comment struct {
	ID      int    `db:"id"`
	Content string `db:"content"`
	PostID  int    `db:"post_id"`
	Post    *Post  `bun:"rel:belongs-to,join:post_id=id"`
}

func (c Comment) TableName() string {
	return "comments"
}

// ExamplePreload demonstrates the Preload functionality
func ExamplePreload() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	// Example 1: Simple Preload (uses subquery for has-many)
	var authors []*Author
	err = adapter.NewSelect().
		Model(&Author{}).
		Table("authors").
		Preload("Posts"). // Load all posts for each author
		Scan(ctx, &authors)
	if err != nil {
		return err
	}

	// Now authors[i].Posts will be populated with their posts

	return nil
}

// ExamplePreloadRelation demonstrates smart PreloadRelation with auto-detection
func ExamplePreloadRelation() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	// Example 1: PreloadRelation auto-detects has-many (uses subquery)
	var authors []*Author
	err = adapter.NewSelect().
		Model(&Author{}).
		Table("authors").
		PreloadRelation("Posts", func(q common.SelectQuery) common.SelectQuery {
			return q.Where("published = ?", true).Order("created_at DESC")
		}).
		Where("active = ?", true).
		Scan(ctx, &authors)
	if err != nil {
		return err
	}

	// Example 2: PreloadRelation auto-detects belongs-to (uses JOIN)
	var posts []*Post
	err = adapter.NewSelect().
		Model(&Post{}).
		Table("posts").
		PreloadRelation("Author"). // Will use JOIN because it's belongs-to
		Scan(ctx, &posts)
	if err != nil {
		return err
	}

	// Example 3: Nested preloads
	err = adapter.NewSelect().
		Model(&Author{}).
		Table("authors").
		PreloadRelation("Posts", func(q common.SelectQuery) common.SelectQuery {
			// First load posts, then preload comments for each post
			return q.Limit(10)
		}).
		Scan(ctx, &authors)
	if err != nil {
		return err
	}

	// Manually load nested relationships (two-level preloading)
	for _, author := range authors {
		if author.Posts != nil {
			for _, post := range author.Posts {
				var comments []*Comment
				err := adapter.NewSelect().
					Table("comments").
					Where("post_id = ?", post.ID).
					Scan(ctx, &comments)
				if err == nil {
					post.Comments = comments
				}
			}
		}
	}

	return nil
}

// ExampleJoinRelation demonstrates explicit JOIN loading
func ExampleJoinRelation() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	// Example 1: Force JOIN for belongs-to relationship
	var posts []*Post
	err = adapter.NewSelect().
		Model(&Post{}).
		Table("posts").
		JoinRelation("Author", func(q common.SelectQuery) common.SelectQuery {
			return q.Where("active = ?", true)
		}).
		Scan(ctx, &posts)
	if err != nil {
		return err
	}

	// Example 2: Multiple JOINs
	err = adapter.NewSelect().
		Model(&Post{}).
		Table("posts p").
		Column("p.*", "a.name as author_name", "a.email as author_email").
		LeftJoin("authors a ON a.id = p.author_id").
		Where("p.published = ?", true).
		Scan(ctx, &posts)

	return err
}

// ExampleScanModel demonstrates ScanModel with struct destinations
func ExampleScanModel() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	ctx := context.Background()

	// Example 1: Scan single struct
	author := Author{}
	err = adapter.NewSelect().
		Model(&author).
		Table("authors").
		Where("id = ?", 1).
		ScanModel(ctx) // ScanModel automatically uses the model set with Model()

	if err != nil {
		return err
	}

	// Example 2: Scan slice of structs
	authors := []*Author{}
	err = adapter.NewSelect().
		Model(&authors).
		Table("authors").
		Where("active = ?", true).
		Limit(10).
		ScanModel(ctx)

	return err
}

// ExampleCompleteWorkflow demonstrates a complete workflow with preloading
func ExampleCompleteWorkflow() error {
	dsn := "postgres://username:password@localhost:5432/dbname?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	adapter := NewPgSQLAdapter(db)
	adapter.EnableQueryDebug() // Enable query logging
	ctx := context.Background()

	// Step 1: Create an author
	author := &Author{
		Name:  "John Doe",
		Email: "john@example.com",
	}

	result, err := adapter.NewInsert().
		Table("authors").
		Value("name", author.Name).
		Value("email", author.Email).
		Returning("id").
		Exec(ctx)
	if err != nil {
		return err
	}

	_ = result

	// Step 2: Load author with all their posts
	var loadedAuthor Author
	err = adapter.NewSelect().
		Model(&loadedAuthor).
		Table("authors").
		PreloadRelation("Posts", func(q common.SelectQuery) common.SelectQuery {
			return q.Order("created_at DESC").Limit(5)
		}).
		Where("id = ?", 1).
		ScanModel(ctx)
	if err != nil {
		return err
	}

	// Step 3: Update author name
	_, err = adapter.NewUpdate().
		Table("authors").
		Set("name", "Jane Doe").
		Where("id = ?", 1).
		Exec(ctx)

	return err
}
