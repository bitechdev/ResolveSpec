// +build integration

package restheadspec

import (
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
		dsn = "host=localhost user=postgres password=postgres dbname=restheadspec_test port=5434 sslmode=disable"
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
func TestIntegration_GetAllUsers(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("GET", "/public/test_users", nil)
	req.Header.Set("X-DetailApi", "true")
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

func TestIntegration_GetUsersWithFilters(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Filter: age > 25
	req := httptest.NewRequest("GET", "/public/test_users", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-SearchOp-Gt-Age", "25")
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

	// Should return 2 users (John: 30, Bob: 35)
	if response.Metadata.Total != 2 {
		t.Errorf("Expected 2 filtered users, got %d", response.Metadata.Total)
	}
}

func TestIntegration_GetUsersWithPagination(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("GET", "/public/test_users", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-Limit", "2")
	req.Header.Set("X-Offset", "1")
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

	// Total should still be 3, but we're only retrieving 2 records starting from offset 1
	if response.Metadata.Total != 3 {
		t.Errorf("Expected total 3 users, got %d", response.Metadata.Total)
	}

	if response.Metadata.Limit != 2 {
		t.Errorf("Expected limit 2, got %d", response.Metadata.Limit)
	}

	if response.Metadata.Offset != 1 {
		t.Errorf("Expected offset 1, got %d", response.Metadata.Offset)
	}
}

func TestIntegration_GetUsersWithSorting(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Sort by age descending
	req := httptest.NewRequest("GET", "/public/test_users", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-Sort", "-age")
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

	// Parse data to verify sort order
	dataBytes, _ := json.Marshal(response.Data)
	var users []TestUser
	json.Unmarshal(dataBytes, &users)

	if len(users) < 3 {
		t.Fatal("Expected at least 3 users")
	}

	// Check that users are sorted by age descending (Bob:35, John:30, Jane:25)
	if users[0].Age < users[1].Age || users[1].Age < users[2].Age {
		t.Error("Expected users to be sorted by age descending")
	}
}

func TestIntegration_GetUsersWithColumnsSelection(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("GET", "/public/test_users", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-Columns", "id,name,email")
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

	// Verify data was returned (column selection doesn't affect metadata count)
	if response.Metadata.Total != 3 {
		t.Errorf("Expected 3 users, got %d", response.Metadata.Total)
	}
}

func TestIntegration_GetUsersWithPreload(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("GET", "/public/test_users?x-fieldfilter-email=john@example.com", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-Preload", "Posts")
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

func TestIntegration_GetMetadata(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("GET", "/public/test_users/metadata", nil)
	req.Header.Set("X-DetailApi", "true")
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
		t.Errorf("Expected success=true, got %v. Body: %s, Error: %v", response.Success, w.Body.String(), response.Error)
	}

	// Check that metadata includes columns
	metadataBytes, _ := json.Marshal(response.Data)
	var metadata common.TableMetadata
	json.Unmarshal(metadataBytes, &metadata)

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

func TestIntegration_OptionsRequest(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	req := httptest.NewRequest("OPTIONS", "/public/test_users", nil)
	w := httptest.NewRecorder()

	muxRouter.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check CORS headers
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected Access-Control-Allow-Origin header")
	}

	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

func TestIntegration_QueryParamsOverHeaders(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Query params should override headers
	req := httptest.NewRequest("GET", "/public/test_users?x-limit=1", nil)
	req.Header.Set("X-DetailApi", "true")
	req.Header.Set("X-Limit", "10")
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

	// Query param should win (limit=1)
	if response.Metadata.Limit != 1 {
		t.Errorf("Expected limit 1 from query param, got %d", response.Metadata.Limit)
	}
}

func TestIntegration_GetSingleRecord(t *testing.T) {
	db := setupTestDB(t)
	defer cleanupTestDB(t, db)
	createTestData(t, db)

	handler := NewHandlerWithGORM(db)
	handler.registry.RegisterModel("public.test_users", TestUser{})

	muxRouter := mux.NewRouter()
	SetupMuxRoutes(muxRouter, handler, nil)

	// Get first user ID
	var user TestUser
	db.Where("email = ?", "john@example.com").First(&user)

	req := httptest.NewRequest("GET", fmt.Sprintf("/public/test_users/%d", user.ID), nil)
	req.Header.Set("X-DetailApi", "true")
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

	// Verify it's a single record
	dataBytes, _ := json.Marshal(response.Data)
	var resultUser TestUser
	json.Unmarshal(dataBytes, &resultUser)

	if resultUser.Email != "john@example.com" {
		t.Errorf("Expected user with email 'john@example.com', got '%s'", resultUser.Email)
	}
}
