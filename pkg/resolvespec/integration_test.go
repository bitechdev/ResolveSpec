// +build integration

package resolvespec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/bitechdev/ResolveSpec/pkg/common"
)

// Test models
type TestUser struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	Email     string    `gorm:"uniqueIndex;not null" json:"email"`
	Age       int       `json:"age"`
	Active    bool      `gorm:"default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	Posts     []TestPost `gorm:"foreignKey:UserID" json:"posts,omitempty"`
}

func (TestUser) TableName() string {
	return "test_users"
}

type TestPost struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null" json:"user_id"`
	Title     string    `gorm:"not null" json:"title"`
	Content   string    `json:"content"`
	Published bool      `gorm:"default:false" json:"published"`
	CreatedAt time.Time `json:"created_at"`
	User      *TestUser  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Comments  []TestComment `gorm:"foreignKey:PostID" json:"comments,omitempty"`
}

func (TestPost) TableName() string {
	return "test_posts"
}

type TestComment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	PostID    uint      `gorm:"not null" json:"post_id"`
	Content   string    `gorm:"not null" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	Post      *TestPost  `gorm:"foreignKey:PostID" json:"post,omitempty"`
}

func (TestComment) TableName() string {
	return "test_comments"
}

// Test helper functions
func setupTestDB(t *testing.T) *gorm.DB {
	// Get connection string from environment or use default
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost user=postgres password=postgres dbname=resolvespec_test port=5434 sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("Skipping integration test: database not available: %v", err)
		return nil
	}

	// Run migrations
	err = db.AutoMigrate(&TestUser{}, &TestPost{}, &TestComment{})
	if err != nil {
		t.Skipf("Skipping integration test: failed to migrate database: %v", err)
		return nil
	}

	return db
}

func cleanupTestDB(t *testing.T, db *gorm.DB) {
	// Clean up test data
	db.Exec("TRUNCATE TABLE test_comments CASCADE")
	db.Exec("TRUNCATE TABLE test_posts CASCADE")
	db.Exec("TRUNCATE TABLE test_users CASCADE")
}

func createTestData(t *testing.T, db *gorm.DB) {
	users := []TestUser{
		{Name: "John Doe", Email: "john@example.com", Age: 30, Active: true},
		{Name: "Jane Smith", Email: "jane@example.com", Age: 25, Active: true},
		{Name: "Bob Johnson", Email: "bob@example.com", Age: 35, Active: false},
	}

	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
	}

	posts := []TestPost{
		{UserID: users[0].ID, Title: "First Post", Content: "Hello World", Published: true},
		{UserID: users[0].ID, Title: "Second Post", Content: "More content", Published: true},
		{UserID: users[1].ID, Title: "Jane's Post", Content: "Jane's content", Published: false},
	}

	for i := range posts {
		if err := db.Create(&posts[i]).Error; err != nil {
			t.Fatalf("Failed to create test post: %v", err)
		}
	}

	comments := []TestComment{
		{PostID: posts[0].ID, Content: "Great post!"},
		{PostID: posts[0].ID, Content: "Thanks for sharing"},
		{PostID: posts[1].ID, Content: "Interesting"},
	}

	for i := range comments {
		if err := db.Create(&comments[i]).Error; err != nil {
			t.Fatalf("Failed to create test comment: %v", err)
		}
	}
}

// Integration tests
func TestIntegration_CreateOperation(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Create a new user
	requestBody := map[string]interface{}{
		"operation": "create",
		"data": map[string]interface{}{
			"name":  "Test User",
			"email": "test@example.com",
			"age":   28,
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/public/test_users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got %v. Error: %v", response.Success, response.Error)
	}

	// Verify user was created
	var user TestUser
	if err := db.Where("email = ?", "test@example.com").First(&user).Error; err != nil {
		t.Errorf("Failed to find created user: %v", err)
	}

	if user.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got '%s'", user.Name)
	}
}

func TestIntegration_ReadOperation(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Read all users
	requestBody := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"limit": 10,
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/public/test_users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got %v", response.Success)
	}

	if response.Metadata == nil {
		t.Fatal("Expected metadata, got nil")
	}

	if response.Metadata.Total != 3 {
		t.Errorf("Expected 3 users, got %d", response.Metadata.Total)
	}
}

func TestIntegration_ReadWithFilters(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Read users with age > 25
	requestBody := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"filters": []map[string]interface{}{
				{
					"column":   "age",
					"operator": "gt",
					"value":    25,
				},
			},
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/public/test_users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got %v", response.Success)
	}

	// Should return 2 users (John: 30, Bob: 35)
	if response.Metadata.Total != 2 {
		t.Errorf("Expected 2 filtered users, got %d", response.Metadata.Total)
	}
}

func TestIntegration_UpdateOperation(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Get user ID
	var user TestUser
	db.Where("email = ?", "john@example.com").First(&user)

	// Update user
	requestBody := map[string]interface{}{
		"operation": "update",
		"data": map[string]interface{}{
			"id":   user.ID,
			"age":  31,
			"name": "John Doe Updated",
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", fmt.Sprintf("/public/test_users/%d", user.ID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify update
	var updatedUser TestUser
	db.First(&updatedUser, user.ID)

	if updatedUser.Age != 31 {
		t.Errorf("Expected age 31, got %d", updatedUser.Age)
	}
	if updatedUser.Name != "John Doe Updated" {
		t.Errorf("Expected name 'John Doe Updated', got '%s'", updatedUser.Name)
	}
}

func TestIntegration_DeleteOperation(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Get user ID
	var user TestUser
	db.Where("email = ?", "bob@example.com").First(&user)

	// Delete user
	requestBody := map[string]interface{}{
		"operation": "delete",
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", fmt.Sprintf("/public/test_users/%d", user.ID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify deletion
	var count int64
	db.Model(&TestUser{}).Where("id = ?", user.ID).Count(&count)

	if count != 0 {
		t.Errorf("Expected user to be deleted, but found %d records", count)
	}
}

func TestIntegration_MetadataOperation(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Get metadata
	requestBody := map[string]interface{}{
		"operation": "meta",
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/public/test_users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got %v", response.Success)
	}

	// Check that metadata includes columns
	// The response.Data is an interface{}, we need to unmarshal it properly
	dataBytes, _ := json.Marshal(response.Data)
	var metadata common.TableMetadata
	if err := json.Unmarshal(dataBytes, &metadata); err != nil {
		t.Fatalf("Failed to unmarshal metadata: %v. Raw data: %+v", err, response.Data)
	}

	if len(metadata.Columns) == 0 {
		t.Error("Expected metadata to contain columns")
	}

	// Verify some expected columns
	hasID := false
	hasName := false
	hasEmail := false
	for _, col := range metadata.Columns {
		if col.Name == "id" {
			hasID = true
			if !col.IsPrimary {
				t.Error("Expected 'id' column to be primary key")
			}
		}
		if col.Name == "name" {
			hasName = true
		}
		if col.Name == "email" {
			hasEmail = true
		}
	}

	if !hasID || !hasName || !hasEmail {
		t.Error("Expected metadata to contain 'id', 'name', and 'email' columns")
	}
}

func TestIntegration_ReadWithPreload(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.RegisterModel("public", "test_users", TestUser{})
	handler.RegisterModel("public", "test_posts", TestPost{})
	handler.RegisterModel("public", "test_comments", TestComment{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Read users with posts preloaded
	requestBody := map[string]interface{}{
		"operation": "read",
		"options": map[string]interface{}{
			"filters": []map[string]interface{}{
				{
					"column":   "email",
					"operator": "eq",
					"value":    "john@example.com",
				},
			},
			"preload": []map[string]interface{}{
				{"relation": "posts"},
			},
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest("POST", "/public/test_users", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var response common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got %v", response.Success)
	}

	// Verify posts are preloaded
	dataBytes, _ := json.Marshal(response.Data)
	var users []TestUser
	json.Unmarshal(dataBytes, &users)

	if len(users) == 0 {
		t.Fatal("Expected at least one user")
	}

	if len(users[0].Posts) == 0 {
		t.Error("Expected posts to be preloaded")
	}
}
