package security

// This file contains usage examples for integrating security with funcspec handlers
// These are example snippets - not executable code

/*
Example 1: Wrap handlers with authentication (required)

	import (
		"github.com/bitechdev/ResolveSpec/pkg/funcspec"
		"github.com/bitechdev/ResolveSpec/pkg/security"
		"github.com/gorilla/mux"
	)

	// Setup
	db := ... // your database connection
	securityList := ... // your security list
	handler := funcspec.NewHandler(db)
	router := mux.NewRouter()

	// Wrap handler with required authentication (returns 401 if not authenticated)
	ordersHandler := security.WithAuth(
		handler.SqlQueryList("SELECT * FROM orders WHERE user_id = [rid_user]", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/orders", ordersHandler).Methods("GET")

Example 2: Wrap handlers with optional authentication

	// Wrap handler with optional authentication (falls back to guest if not authenticated)
	productsHandler := security.WithOptionalAuth(
		handler.SqlQueryList("SELECT * FROM products WHERE deleted = false", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/products", productsHandler).Methods("GET")

	// The handler will show all products for guests, but could show personalized pricing
	// or recommendations for authenticated users based on [rid_user]

Example 3: Wrap handlers with both authentication and security context

	// Use the convenience function for both auth and security context
	usersHandler := security.WithAuthAndSecurity(
		handler.SqlQueryList("SELECT * FROM users WHERE active = true", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/users", usersHandler).Methods("GET")

	// Or use WithOptionalAuthAndSecurity for optional auth
	postsHandler := security.WithOptionalAuthAndSecurity(
		handler.SqlQueryList("SELECT * FROM posts WHERE published = true", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/posts", postsHandler).Methods("GET")

Example 4: Wrap a single funcspec handler with security context only

	import (
		"github.com/bitechdev/ResolveSpec/pkg/funcspec"
		"github.com/bitechdev/ResolveSpec/pkg/security"
		"github.com/gorilla/mux"
	)

	// Setup
	db := ... // your database connection
	securityList := ... // your security list
	handler := funcspec.NewHandler(db)
	router := mux.NewRouter()

	// Wrap a specific handler with security context
	usersHandler := security.WithSecurityContext(
		handler.SqlQueryList("SELECT * FROM users WHERE active = true", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/users", usersHandler).Methods("GET")

Example 5: Wrap multiple handlers for different paths

	// Products list endpoint
	productsHandler := security.WithSecurityContext(
		handler.SqlQueryList("SELECT * FROM products WHERE deleted = false", false, true, true),
		securityList,
	)
	router.HandleFunc("/api/products", productsHandler).Methods("GET")

	// Single product endpoint
	productHandler := security.WithSecurityContext(
		handler.SqlQuery("SELECT * FROM products WHERE id = [id]", true),
		securityList,
	)
	router.HandleFunc("/api/products/{id}", productHandler).Methods("GET")

	// Orders endpoint with user filtering
	ordersHandler := security.WithSecurityContext(
		handler.SqlQueryList("SELECT * FROM orders WHERE user_id = [rid_user]", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/orders", ordersHandler).Methods("GET")

Example 6: Helper function to wrap multiple handlers

	// Create a helper function for your application
	func secureHandler(h funcspec.HTTPFuncType, sl *SecurityList) funcspec.HTTPFuncType {
		return security.WithSecurityContext(h, sl)
	}

	// Use it to wrap handlers
	router.HandleFunc("/api/users", secureHandler(
		handler.SqlQueryList("SELECT * FROM users", false, false, false),
		securityList,
	)).Methods("GET")

	router.HandleFunc("/api/roles", secureHandler(
		handler.SqlQueryList("SELECT * FROM roles", false, false, false),
		securityList,
	)).Methods("GET")

Example 7: Access SecurityList and user context in hooks

	// In your funcspec hook, you can now access the SecurityList and user context
	handler.Hooks().Register(funcspec.BeforeQueryList, func(ctx *funcspec.HookContext) error {
		// Get SecurityList from context
		if secList, ok := security.GetSecurityList(ctx.Context); ok {
			// Use secList to apply security rules
			// e.g., apply row-level security, column masking, etc.
			_ = secList
		}

		// Get user context
		if userCtx, ok := security.GetUserContext(ctx.Context); ok {
			// Access user information
			logger.Info("User %s (ID: %d) accessing resource", userCtx.UserName, userCtx.UserID)
		}

		return nil
	})

Example 8: Mixing authentication and security patterns

	// Public endpoint - no auth required, but has security context
	publicHandler := security.WithSecurityContext(
		handler.SqlQueryList("SELECT * FROM public_data", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/public", publicHandler).Methods("GET")

	// Optional auth - personalized for logged-in users, works for guests
	personalizedHandler := security.WithOptionalAuth(
		handler.SqlQueryList("SELECT * FROM products WHERE category = [category]", false, true, false),
		securityList,
	)
	router.HandleFunc("/api/products/category/{category}", personalizedHandler).Methods("GET")

	// Required auth - must be logged in
	privateHandler := security.WithAuthAndSecurity(
		handler.SqlQueryList("SELECT * FROM private_data WHERE user_id = [rid_user]", false, false, false),
		securityList,
	)
	router.HandleFunc("/api/private", privateHandler).Methods("GET")
*/
