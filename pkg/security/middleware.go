package security

import (
	"context"
	"net/http"
	"strconv"

	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// Context keys for user information
	UserIDKey       contextKey = "user_id"
	UserNameKey     contextKey = "user_name"
	UserLevelKey    contextKey = "user_level"
	SessionIDKey    contextKey = "session_id"
	SessionRIDKey   contextKey = "session_rid"
	RemoteIDKey     contextKey = "remote_id"
	UserRolesKey    contextKey = "user_roles"
	UserEmailKey    contextKey = "user_email"
	UserContextKey  contextKey = "user_context"
	UserMetaKey     contextKey = "user_meta"
	SkipAuthKey     contextKey = "skip_auth"
	OptionalAuthKey contextKey = "optional_auth"
	ModelRulesKey   contextKey = "model_rules"
)

// SkipAuth returns a context with skip auth flag set to true
// Use this to mark routes that should bypass authentication middleware
func SkipAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, SkipAuthKey, true)
}

// OptionalAuth returns a context with optional auth flag set to true
// Use this to mark routes that should try to authenticate, but fall back to guest if authentication fails
func OptionalAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, OptionalAuthKey, true)
}

// createGuestContext creates a guest user context for unauthenticated requests
func createGuestContext(r *http.Request) *UserContext {
	return &UserContext{
		UserID:    0,
		UserName:  "guest",
		UserLevel: 0,
		SessionID: "",
		RemoteID:  r.RemoteAddr,
		Roles:     []string{"guest"},
		Email:     "",
		Claims:    map[string]any{},
		Meta:      map[string]any{},
	}
}

// setUserContext adds a user context to the request context
func setUserContext(r *http.Request, userCtx *UserContext) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, UserContextKey, userCtx)
	ctx = context.WithValue(ctx, UserIDKey, userCtx.UserID)
	ctx = context.WithValue(ctx, UserNameKey, userCtx.UserName)
	ctx = context.WithValue(ctx, UserLevelKey, userCtx.UserLevel)
	ctx = context.WithValue(ctx, SessionIDKey, userCtx.SessionID)
	ctx = context.WithValue(ctx, SessionRIDKey, userCtx.SessionRID)
	ctx = context.WithValue(ctx, RemoteIDKey, userCtx.RemoteID)
	ctx = context.WithValue(ctx, UserRolesKey, userCtx.Roles)

	if userCtx.Email != "" {
		ctx = context.WithValue(ctx, UserEmailKey, userCtx.Email)
	}
	if len(userCtx.Meta) > 0 {
		ctx = context.WithValue(ctx, UserMetaKey, userCtx.Meta)
	}

	return r.WithContext(ctx)
}

// authenticateRequest performs authentication and adds user context to the request
// This is the shared authentication logic used by both handler and middleware
func authenticateRequest(w http.ResponseWriter, r *http.Request, provider SecurityProvider) (*http.Request, bool) {
	// Call the provider's Authenticate method
	userCtx, err := provider.Authenticate(r)
	if err != nil {
		http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
		return nil, false
	}

	return setUserContext(r, userCtx), true
}

// NewAuthHandler creates an authentication handler that can be used standalone
// This handler performs authentication and returns 401 if authentication fails
// Use this when you need authentication logic without middleware wrapping
func NewAuthHandler(securityList *SecurityList, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the security provider
		provider := securityList.Provider()
		if provider == nil {
			http.Error(w, "Security provider not configured", http.StatusInternalServerError)
			return
		}

		// Authenticate the request
		authenticatedReq, ok := authenticateRequest(w, r, provider)
		if !ok {
			return // authenticateRequest already wrote the error response
		}

		// Continue with authenticated context
		next.ServeHTTP(w, authenticatedReq)
	})
}

// NewOptionalAuthHandler creates an optional authentication handler that can be used standalone
// This handler tries to authenticate but falls back to guest context if authentication fails
// Use this for routes that should show personalized content for authenticated users but still work for guests
func NewOptionalAuthHandler(securityList *SecurityList, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the security provider
		provider := securityList.Provider()
		if provider == nil {
			http.Error(w, "Security provider not configured", http.StatusInternalServerError)
			return
		}

		// Try to authenticate
		userCtx, err := provider.Authenticate(r)
		if err != nil {
			// Authentication failed - set guest context and continue
			guestCtx := createGuestContext(r)
			next.ServeHTTP(w, setUserContext(r, guestCtx))
			return
		}

		// Authentication succeeded - set user context
		next.ServeHTTP(w, setUserContext(r, userCtx))
	})
}

// NewAuthMiddleware creates an authentication middleware with the given security list
// This middleware extracts user authentication from the request and adds it to context
// Routes can skip authentication by setting SkipAuthKey context value (use SkipAuth helper)
// Routes can use optional authentication by setting OptionalAuthKey context value (use OptionalAuth helper)
// When authentication is skipped or fails with optional auth, a guest user context is set instead
func NewAuthMiddleware(securityList *SecurityList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this route should skip authentication
			if skip, ok := r.Context().Value(SkipAuthKey).(bool); ok && skip {
				// Set guest user context for skipped routes
				guestCtx := createGuestContext(r)
				next.ServeHTTP(w, setUserContext(r, guestCtx))
				return
			}

			// Get the security provider
			provider := securityList.Provider()
			if provider == nil {
				http.Error(w, "Security provider not configured", http.StatusInternalServerError)
				return
			}

			// Check if this route has optional authentication
			optional, _ := r.Context().Value(OptionalAuthKey).(bool)

			// Try to authenticate
			userCtx, err := provider.Authenticate(r)
			if err != nil {
				if optional {
					// Optional auth failed - set guest context and continue
					guestCtx := createGuestContext(r)
					next.ServeHTTP(w, setUserContext(r, guestCtx))
					return
				}
				// Required auth failed - return error
				http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Authentication succeeded - set user context
			next.ServeHTTP(w, setUserContext(r, userCtx))
		})
	}
}

// NewModelAuthMiddleware creates authentication middleware that respects ModelRules for the given model name.
// It first checks if ModelRules are set for the model:
//   - If SecurityDisabled is true, authentication is skipped and a guest context is set.
//   - Otherwise, all checks from NewAuthMiddleware apply (SkipAuthKey, provider check, OptionalAuthKey, Authenticate).
//
// If the model is not found in any registry, the middleware falls back to standard NewAuthMiddleware behaviour.
func NewModelAuthMiddleware(securityList *SecurityList, modelName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check ModelRules first
			if rules, err := modelregistry.GetModelRulesByName(modelName); err == nil {
				// Store rules in context for downstream use (e.g., security hooks)
				r = r.WithContext(context.WithValue(r.Context(), ModelRulesKey, rules))

				if rules.SecurityDisabled {
					guestCtx := createGuestContext(r)
					next.ServeHTTP(w, setUserContext(r, guestCtx))
					return
				}
				isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
				isUpdate := r.Method == http.MethodPut || r.Method == http.MethodPatch
				if (isRead && rules.CanPublicRead) || (isUpdate && rules.CanPublicUpdate) {
					guestCtx := createGuestContext(r)
					next.ServeHTTP(w, setUserContext(r, guestCtx))
					return
				}
			}

			// Check if this route should skip authentication
			if skip, ok := r.Context().Value(SkipAuthKey).(bool); ok && skip {
				guestCtx := createGuestContext(r)
				next.ServeHTTP(w, setUserContext(r, guestCtx))
				return
			}

			// Get the security provider
			provider := securityList.Provider()
			if provider == nil {
				http.Error(w, "Security provider not configured", http.StatusInternalServerError)
				return
			}

			// Check if this route has optional authentication
			optional, _ := r.Context().Value(OptionalAuthKey).(bool)

			// Try to authenticate
			userCtx, err := provider.Authenticate(r)
			if err != nil {
				if optional {
					guestCtx := createGuestContext(r)
					next.ServeHTTP(w, setUserContext(r, guestCtx))
					return
				}
				http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, setUserContext(r, userCtx))
		})
	}
}

// SetSecurityMiddleware adds security context to requests
// This middleware should be applied after AuthMiddleware
func SetSecurityMiddleware(securityList *SecurityList) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), SECURITY_CONTEXT_KEY, securityList)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithAuth wraps an HTTPFuncType handler with required authentication
// This function performs authentication and returns 401 if authentication fails
// Use this for handlers that require authenticated users
//
// Usage:
//
//	handler := funcspec.NewHandler(db)
//	wrappedHandler := security.WithAuth(handler.SqlQueryList("SELECT * FROM orders WHERE user_id = [rid_user]", false, false, false), securityList)
//	router.HandleFunc("/api/orders", wrappedHandler)
func WithAuth(handler func(http.ResponseWriter, *http.Request), securityList *SecurityList) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the security provider
		provider := securityList.Provider()
		if provider == nil {
			http.Error(w, "Security provider not configured", http.StatusInternalServerError)
			return
		}

		// Authenticate the request
		authenticatedReq, ok := authenticateRequest(w, r, provider)
		if !ok {
			return // authenticateRequest already wrote the error response
		}

		// Continue with authenticated context
		handler(w, authenticatedReq)
	}
}

// WithOptionalAuth wraps an HTTPFuncType handler with optional authentication
// This function tries to authenticate but falls back to guest context if authentication fails
// Use this for handlers that should show personalized content for authenticated users but still work for guests
//
// Usage:
//
//	handler := funcspec.NewHandler(db)
//	wrappedHandler := security.WithOptionalAuth(handler.SqlQueryList("SELECT * FROM products", false, false, false), securityList)
//	router.HandleFunc("/api/products", wrappedHandler)
func WithOptionalAuth(handler func(http.ResponseWriter, *http.Request), securityList *SecurityList) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the security provider
		provider := securityList.Provider()
		if provider == nil {
			http.Error(w, "Security provider not configured", http.StatusInternalServerError)
			return
		}

		// Try to authenticate
		userCtx, err := provider.Authenticate(r)
		if err != nil {
			// Authentication failed - set guest context and continue
			guestCtx := createGuestContext(r)
			handler(w, setUserContext(r, guestCtx))
			return
		}

		// Authentication succeeded - set user context
		handler(w, setUserContext(r, userCtx))
	}
}

// WithSecurityContext wraps an HTTPFuncType handler with security context
// This function allows you to add security context to specific handler functions
// without needing to apply middleware globally
//
// Usage:
//
//	handler := funcspec.NewHandler(db)
//	wrappedHandler := security.WithSecurityContext(handler.SqlQueryList("SELECT * FROM users", false, false, false), securityList)
//	router.HandleFunc("/api/users", wrappedHandler)
func WithSecurityContext(handler func(http.ResponseWriter, *http.Request), securityList *SecurityList) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), SECURITY_CONTEXT_KEY, securityList)
		handler(w, r.WithContext(ctx))
	}
}

// WithAuthAndSecurity wraps an HTTPFuncType handler with both authentication and security context
// This is a convenience function that combines WithAuth and WithSecurityContext
// Use this when you need both authentication and security context for a handler
//
// Usage:
//
//	handler := funcspec.NewHandler(db)
//	wrappedHandler := security.WithAuthAndSecurity(handler.SqlQueryList("SELECT * FROM users", false, false, false), securityList)
//	router.HandleFunc("/api/users", wrappedHandler)
func WithAuthAndSecurity(handler func(http.ResponseWriter, *http.Request), securityList *SecurityList) func(http.ResponseWriter, *http.Request) {
	return WithAuth(WithSecurityContext(handler, securityList), securityList)
}

// WithOptionalAuthAndSecurity wraps an HTTPFuncType handler with optional authentication and security context
// This is a convenience function that combines WithOptionalAuth and WithSecurityContext
// Use this when you want optional authentication and security context for a handler
//
// Usage:
//
//	handler := funcspec.NewHandler(db)
//	wrappedHandler := security.WithOptionalAuthAndSecurity(handler.SqlQueryList("SELECT * FROM products", false, false, false), securityList)
//	router.HandleFunc("/api/products", wrappedHandler)
func WithOptionalAuthAndSecurity(handler func(http.ResponseWriter, *http.Request), securityList *SecurityList) func(http.ResponseWriter, *http.Request) {
	return WithOptionalAuth(WithSecurityContext(handler, securityList), securityList)
}

// GetSecurityList extracts the SecurityList from request context
func GetSecurityList(ctx context.Context) (*SecurityList, bool) {
	securityList, ok := ctx.Value(SECURITY_CONTEXT_KEY).(*SecurityList)
	return securityList, ok
}

// GetUserContext extracts the full user context from request context
func GetUserContext(ctx context.Context) (*UserContext, bool) {
	userCtx, ok := ctx.Value(UserContextKey).(*UserContext)
	return userCtx, ok
}

// GetUserID extracts the user ID from context
func GetUserID(ctx context.Context) (int, bool) {
	userID, ok := ctx.Value(UserIDKey).(int)
	return userID, ok
}

// GetUserName extracts the user name from context
func GetUserName(ctx context.Context) (string, bool) {
	userName, ok := ctx.Value(UserNameKey).(string)
	return userName, ok
}

// GetUserLevel extracts the user level from context
func GetUserLevel(ctx context.Context) (int, bool) {
	userLevel, ok := ctx.Value(UserLevelKey).(int)
	return userLevel, ok
}

// GetSessionID extracts the session ID from context
func GetSessionID(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(SessionIDKey).(string)
	return sessionID, ok
}

// GetSessionID extracts the session ID from context
func GetSessionRID(ctx context.Context) (int64, bool) {
	sessionRIDStr, ok := ctx.Value(SessionRIDKey).(string)
	sessionRID, err := strconv.ParseInt(sessionRIDStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return sessionRID, ok
}

// GetRemoteID extracts the remote ID from context
func GetRemoteID(ctx context.Context) (string, bool) {
	remoteID, ok := ctx.Value(RemoteIDKey).(string)
	return remoteID, ok
}

// GetUserRoles extracts user roles from context
func GetUserRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(UserRolesKey).([]string)
	return roles, ok
}

// GetUserEmail extracts user email from context
func GetUserEmail(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(UserEmailKey).(string)
	return email, ok
}

// GetUserMeta extracts user metadata from context
func GetUserMeta(ctx context.Context) (map[string]any, bool) {
	meta, ok := ctx.Value(UserMetaKey).(map[string]any)
	return meta, ok
}

// GetModelRulesFromContext extracts ModelRules stored by NewModelAuthMiddleware
func GetModelRulesFromContext(ctx context.Context) (modelregistry.ModelRules, bool) {
	rules, ok := ctx.Value(ModelRulesKey).(modelregistry.ModelRules)
	return rules, ok
}

// // Handler adapters for resolvespec/restheadspec compatibility
// // These functions allow using NewAuthHandler and NewOptionalAuthHandler with custom handler abstractions

// // SpecHandlerAdapter is an interface for handler adapters that need authentication
// // Implement this interface to create adapters for custom handler types
// type SpecHandlerAdapter interface {
// 	// AdaptToHTTPHandler converts the custom handler to a standard http.Handler
// 	AdaptToHTTPHandler() http.Handler
// }

// // ResolveSpecHandlerAdapter adapts a resolvespec/restheadspec handler method to http.Handler
// type ResolveSpecHandlerAdapter struct {
// 	// HandlerMethod is the method to call (e.g., handler.Handle, handler.HandleGet)
// 	HandlerMethod func(w any, r any, params map[string]string)
// 	// Params are the route parameters (e.g., {"schema": "public", "entity": "users"})
// 	Params map[string]string
// 	// RequestAdapter converts *http.Request to the custom Request interface
// 	// Use router.NewHTTPRequest from pkg/common/adapters/router
// 	RequestAdapter func(*http.Request) any
// 	// ResponseAdapter converts http.ResponseWriter to the custom ResponseWriter interface
// 	// Use router.NewHTTPResponseWriter from pkg/common/adapters/router
// 	ResponseAdapter func(http.ResponseWriter) any
// }

// // AdaptToHTTPHandler implements SpecHandlerAdapter
// func (a *ResolveSpecHandlerAdapter) AdaptToHTTPHandler() http.Handler {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		req := a.RequestAdapter(r)
// 		resp := a.ResponseAdapter(w)
// 		a.HandlerMethod(resp, req, a.Params)
// 	})
// }

// // WrapSpecHandler wraps a spec handler adapter with authentication
// // Use this to apply NewAuthHandler or NewOptionalAuthHandler to resolvespec/restheadspec handlers
// //
// // Example with required auth:
// //
// //	adapter := &security.ResolveSpecHandlerAdapter{
// //	    HandlerMethod: handler.Handle,
// //	    Params: map[string]string{"schema": "public", "entity": "users"},
// //	    RequestAdapter: func(r *http.Request) any { return router.NewHTTPRequest(r) },
// //	    ResponseAdapter: func(w http.ResponseWriter) any { return router.NewHTTPResponseWriter(w) },
// //	}
// //	authHandler := security.WrapSpecHandler(securityList, adapter, false)
// //	muxRouter.Handle("/api/users", authHandler)
// func WrapSpecHandler(securityList *SecurityList, adapter SpecHandlerAdapter, optional bool) http.Handler {
// 	httpHandler := adapter.AdaptToHTTPHandler()
// 	if optional {
// 		return NewOptionalAuthHandler(securityList, httpHandler)
// 	}
// 	return NewAuthHandler(securityList, httpHandler)
// }

// // MuxRouteBuilder helps build authenticated routes with Gorilla Mux
// type MuxRouteBuilder struct {
// 	securityList    *SecurityList
// 	requestAdapter  func(*http.Request) any
// 	responseAdapter func(http.ResponseWriter) any
// 	paramExtractor  func(*http.Request) map[string]string
// }

// // NewMuxRouteBuilder creates a route builder for Gorilla Mux with standard router adapters
// // Usage:
// //
// //	builder := security.NewMuxRouteBuilder(securityList, router.NewHTTPRequest, router.NewHTTPResponseWriter)
// func NewMuxRouteBuilder(
// 	securityList *SecurityList,
// 	requestAdapter func(*http.Request) any,
// 	responseAdapter func(http.ResponseWriter) any,
// ) *MuxRouteBuilder {
// 	return &MuxRouteBuilder{
// 		securityList:    securityList,
// 		requestAdapter:  requestAdapter,
// 		responseAdapter: responseAdapter,
// 		paramExtractor:  nil, // Will be set per route using mux.Vars
// 	}
// }

// // HandleAuth creates an authenticated route handler
// // pattern: the route pattern (e.g., "/{schema}/{entity}")
// // handler: the handler method to call (e.g., handler.Handle)
// // optional: true for optional auth (guest fallback), false for required auth (401 on failure)
// // methods: HTTP methods (e.g., "GET", "POST")
// //
// // Usage:
// //
// //	builder.HandleAuth(router, "/{schema}/{entity}", handler.Handle, false, "POST")
// func (b *MuxRouteBuilder) HandleAuth(
// 	router interface {
// 		HandleFunc(pattern string, f func(http.ResponseWriter, *http.Request)) interface{ Methods(...string) interface{} }
// 	},
// 	pattern string,
// 	handlerMethod func(w any, r any, params map[string]string),
// 	optional bool,
// 	methods ...string,
// ) {
// 	router.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
// 		// Extract params using the registered extractor or default to empty map
// 		var params map[string]string
// 		if b.paramExtractor != nil {
// 			params = b.paramExtractor(r)
// 		} else {
// 			params = make(map[string]string)
// 		}

// 		adapter := &ResolveSpecHandlerAdapter{
// 			HandlerMethod:   handlerMethod,
// 			Params:          params,
// 			RequestAdapter:  b.requestAdapter,
// 			ResponseAdapter: b.responseAdapter,
// 		}
// 		authHandler := WrapSpecHandler(b.securityList, adapter, optional)
// 		authHandler.ServeHTTP(w, r)
// 	}).Methods(methods...)
// }

// // SetParamExtractor sets a custom parameter extractor function
// // For Gorilla Mux, you would use: builder.SetParamExtractor(mux.Vars)
// func (b *MuxRouteBuilder) SetParamExtractor(extractor func(*http.Request) map[string]string) {
// 	b.paramExtractor = extractor
// }

// // SetupAuthenticatedSpecRoutes sets up all standard resolvespec/restheadspec routes with authentication
// // This is a convenience function that sets up the common route patterns
// //
// // Usage:
// //
// //	security.SetupAuthenticatedSpecRoutes(router, handler, securityList, router.NewHTTPRequest, router.NewHTTPResponseWriter, mux.Vars)
// func SetupAuthenticatedSpecRoutes(
// 	router interface {
// 		HandleFunc(pattern string, f func(http.ResponseWriter, *http.Request)) interface{ Methods(...string) interface{} }
// 	},
// 	handler interface {
// 		Handle(w any, r any, params map[string]string)
// 		HandleGet(w any, r any, params map[string]string)
// 	},
// 	securityList *SecurityList,
// 	requestAdapter func(*http.Request) any,
// 	responseAdapter func(http.ResponseWriter) any,
// 	paramExtractor func(*http.Request) map[string]string,
// ) {
// 	builder := NewMuxRouteBuilder(securityList, requestAdapter, responseAdapter)
// 	builder.SetParamExtractor(paramExtractor)

// 	// POST /{schema}/{entity}
// 	builder.HandleAuth(router, "/{schema}/{entity}", handler.Handle, false, "POST")

// 	// POST /{schema}/{entity}/{id}
// 	builder.HandleAuth(router, "/{schema}/{entity}/{id}", handler.Handle, false, "POST")

// 	// GET /{schema}/{entity}
// 	builder.HandleAuth(router, "/{schema}/{entity}", handler.HandleGet, false, "GET")
// }
