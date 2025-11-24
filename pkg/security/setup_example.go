package security

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/bitechdev/ResolveSpec/pkg/restheadspec"
)

// SetupSecurityProvider initializes and configures the security provider
// This function creates a SecurityList with the given provider and registers hooks
//
// Example usage:
//
//	// Create your security provider (use composite or single provider)
//	auth := security.NewJWTAuthenticator("your-secret-key", db)
//	colSec := security.NewDatabaseColumnSecurityProvider(db)
//	rowSec := security.NewDatabaseRowSecurityProvider(db)
//	provider := security.NewCompositeSecurityProvider(auth, colSec, rowSec)
//
//	// Setup security with the provider
//	handler := restheadspec.NewHandlerWithGORM(db)
//	securityList := security.SetupSecurityProvider(handler, provider)
//
//	// Apply middleware
//	router.Use(security.NewAuthMiddleware(securityList))
//	router.Use(security.SetSecurityMiddleware(securityList))
func SetupSecurityProvider(handler *restheadspec.Handler, provider SecurityProvider) *SecurityList {
	if provider == nil {
		panic("security provider cannot be nil")
	}

	// Create security list with the provider
	securityList := NewSecurityList(provider)

	// Register all security hooks
	RegisterSecurityHooks(handler, securityList)

	return securityList
}

// Example 1: Complete Setup with Composite Provider and Database-Backed Security
// ===============================================================================
// Note: Security providers use *sql.DB, but restheadspec.Handler may use *gorm.DB
// You can get *sql.DB from gorm.DB using: sqlDB, _ := gormDB.DB()

func ExampleDatabaseSecurity(gormDB interface{}, sqlDB *sql.DB) (http.Handler, error) {
	// Step 1: Create the ResolveSpec handler
	// handler := restheadspec.NewHandlerWithGORM(gormDB.(*gorm.DB))
	handler := &restheadspec.Handler{} // Placeholder - use your handler initialization

	// Step 2: Register your models
	// handler.RegisterModel("public", "users", User{})
	// handler.RegisterModel("public", "orders", Order{})

	// Step 3: Create security provider components (using sql.DB)
	auth := NewJWTAuthenticator("your-secret-key", sqlDB)
	colSec := NewDatabaseColumnSecurityProvider(sqlDB)
	rowSec := NewDatabaseRowSecurityProvider(sqlDB)

	// Step 4: Combine into composite provider
	provider := NewCompositeSecurityProvider(auth, colSec, rowSec)

	// Step 5: Setup security
	securityList := SetupSecurityProvider(handler, provider)

	// Step 6: Create router and setup routes
	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, handler)

	// Step 7: Apply middleware in correct order
	router.Use(NewAuthMiddleware(securityList))
	router.Use(SetSecurityMiddleware(securityList))

	return router, nil
}

// Example 2: Simple Header-Based Authentication
// ==============================================

func ExampleHeaderAuthentication(gormDB interface{}, sqlDB *sql.DB) (*mux.Router, error) {
	// handler := restheadspec.NewHandlerWithGORM(gormDB.(*gorm.DB))
	handler := &restheadspec.Handler{} // Placeholder - use your handler initialization

	// Use header-based auth with database security providers
	auth := NewHeaderAuthenticatorExample()
	colSec := NewDatabaseColumnSecurityProvider(sqlDB)
	rowSec := NewDatabaseRowSecurityProvider(sqlDB)

	provider := NewCompositeSecurityProvider(auth, colSec, rowSec)
	securityList := SetupSecurityProvider(handler, provider)

	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, handler)

	router.Use(NewAuthMiddleware(securityList))
	router.Use(SetSecurityMiddleware(securityList))

	return router, nil
}

// Example 3: Config-Based Security (No Database for Security)
// ===========================================================

func ExampleConfigSecurity(gormDB interface{}) (*mux.Router, error) {
	// handler := restheadspec.NewHandlerWithGORM(gormDB.(*gorm.DB))
	handler := &restheadspec.Handler{} // Placeholder - use your handler initialization

	// Define column security rules in code
	columnRules := map[string][]ColumnSecurity{
		"public.employees": {
			{
				Schema:     "public",
				Tablename:  "employees",
				Path:       []string{"ssn"},
				Accesstype: "mask",
				MaskStart:  5,
				MaskChar:   "*",
			},
			{
				Schema:     "public",
				Tablename:  "employees",
				Path:       []string{"salary"},
				Accesstype: "hide",
			},
		},
	}

	// Define row security templates
	rowTemplates := map[string]string{
		"public.orders":    "user_id = {UserID}",
		"public.documents": "user_id = {UserID} OR is_public = true",
	}

	// Define blocked tables
	blockedTables := map[string]bool{
		"public.admin_logs": true,
	}

	// Create providers
	auth := NewHeaderAuthenticatorExample()
	colSec := NewConfigColumnSecurityProvider(columnRules)
	rowSec := NewConfigRowSecurityProvider(rowTemplates, blockedTables)

	provider := NewCompositeSecurityProvider(auth, colSec, rowSec)
	securityList := SetupSecurityProvider(handler, provider)

	router := mux.NewRouter()
	restheadspec.SetupMuxRoutes(router, handler)

	router.Use(NewAuthMiddleware(securityList))
	router.Use(SetSecurityMiddleware(securityList))

	return router, nil
}

// Example 4: Custom Security Provider
// ====================================

// You can implement your own SecurityProvider by implementing all three interfaces
type CustomSecurityProvider struct {
	// Your custom fields
}

func (p *CustomSecurityProvider) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	// Your custom login logic
	return nil, fmt.Errorf("not implemented")
}

func (p *CustomSecurityProvider) Logout(ctx context.Context, req LogoutRequest) error {
	// Your custom logout logic
	return nil
}

func (p *CustomSecurityProvider) Authenticate(r *http.Request) (*UserContext, error) {
	// Your custom authentication logic
	return nil, fmt.Errorf("not implemented")
}

func (p *CustomSecurityProvider) GetColumnSecurity(ctx context.Context, userID int, schema, table string) ([]ColumnSecurity, error) {
	// Your custom column security logic
	return []ColumnSecurity{}, nil
}

func (p *CustomSecurityProvider) GetRowSecurity(ctx context.Context, userID int, schema, table string) (RowSecurity, error) {
	// Your custom row security logic
	return RowSecurity{
		Schema:    schema,
		Tablename: table,
		UserID:    userID,
	}, nil
}

// Example 5: Adding Login/Logout Endpoints
// =========================================

func SetupAuthRoutes(router *mux.Router, securityList *SecurityList) {
	// Login endpoint
	router.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		// Parse login request
		var loginReq LoginRequest
		// json.NewDecoder(r.Body).Decode(&loginReq)

		// Call provider's Login method
		resp, err := securityList.Provider().Login(r.Context(), loginReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Return token
		w.Header().Set("Content-Type", "application/json")
		// json.NewEncoder(w).Encode(resp)
		fmt.Fprintf(w, `{"token": "%s", "expires_in": %d}`, resp.Token, resp.ExpiresIn)
	}).Methods("POST")

	// Logout endpoint
	router.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		// Extract token from header
		token := r.Header.Get("Authorization")

		// Get user ID from context (if authenticated)
		userID, _ := GetUserID(r.Context())

		// Call provider's Logout method
		err := securityList.Provider().Logout(r.Context(), LogoutRequest{
			Token:  token,
			UserID: userID,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success": true}`)
	}).Methods("POST")

	// Optional: Token refresh endpoint
	router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		refreshToken := r.Header.Get("X-Refresh-Token")

		// Check if provider supports refresh
		if refreshable, ok := securityList.Provider().(Refreshable); ok {
			resp, err := refreshable.RefreshToken(r.Context(), refreshToken)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"token": "%s", "expires_in": %d}`, resp.Token, resp.ExpiresIn)
		} else {
			http.Error(w, "Token refresh not supported", http.StatusNotImplemented)
		}
	}).Methods("POST")
}

// Example 6: Complete Server Setup
// =================================

func CompleteServerExample(gormDB interface{}, sqlDB *sql.DB) http.Handler {
	// Create handler and register models
	// handler := restheadspec.NewHandlerWithGORM(gormDB.(*gorm.DB))
	handler := &restheadspec.Handler{} // Placeholder - use your handler initialization
	// handler.RegisterModel("public", "users", User{})

	// Setup security (using sql.DB for security providers)
	auth := NewJWTAuthenticator("secret-key", sqlDB)
	colSec := NewDatabaseColumnSecurityProvider(sqlDB)
	rowSec := NewDatabaseRowSecurityProvider(sqlDB)
	provider := NewCompositeSecurityProvider(auth, colSec, rowSec)
	securityList := SetupSecurityProvider(handler, provider)

	// Create router
	router := mux.NewRouter()

	// Add auth routes (login/logout)
	SetupAuthRoutes(router, securityList)

	// Add API routes with security middleware
	apiRouter := router.PathPrefix("/api").Subrouter()
	restheadspec.SetupMuxRoutes(apiRouter, handler)
	apiRouter.Use(NewAuthMiddleware(securityList))
	apiRouter.Use(SetSecurityMiddleware(securityList))

	return router
}
