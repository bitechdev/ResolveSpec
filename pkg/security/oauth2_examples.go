package security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// Example: OAuth2 Authentication with Google
func ExampleOAuth2Google() {
	db, _ := sql.Open("postgres", "connection-string")

	// Create OAuth2 authenticator for Google
	oauth2Auth := NewGoogleAuthenticator(
		"your-client-id",
		"your-client-secret",
		"http://localhost:8080/auth/google/callback",
		db,
	)

	router := mux.NewRouter()

	// Login endpoint - redirects to Google
	router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := oauth2Auth.OAuth2GenerateState()
		authURL, _ := oauth2Auth.OAuth2GetAuthURL("google", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	// Callback endpoint - handles Google response
	router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := oauth2Auth.OAuth2HandleCallback(r.Context(), "google", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResp.Token,
			Path:     "/",
			MaxAge:   int(loginResp.ExpiresIn),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		// Return user info as JSON
		json.NewEncoder(w).Encode(loginResp)
	})

	http.ListenAndServe(":8080", router)
}

// Example: OAuth2 Authentication with GitHub
func ExampleOAuth2GitHub() {
	db, _ := sql.Open("postgres", "connection-string")

	oauth2Auth := NewGitHubAuthenticator(
		"your-github-client-id",
		"your-github-client-secret",
		"http://localhost:8080/auth/github/callback",
		db,
	)

	router := mux.NewRouter()

	router.HandleFunc("/auth/github/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := oauth2Auth.OAuth2GenerateState()
		authURL, _ := oauth2Auth.OAuth2GetAuthURL("github", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	router.HandleFunc("/auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := oauth2Auth.OAuth2HandleCallback(r.Context(), "github", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(loginResp)
	})

	http.ListenAndServe(":8080", router)
}

// Example: Custom OAuth2 Provider
func ExampleOAuth2Custom() {
	db, _ := sql.Open("postgres", "connection-string")

	// Custom OAuth2 provider configuration
	oauth2Auth := NewDatabaseAuthenticator(db).WithOAuth2(OAuth2Config{
		ClientID:     "your-client-id",
		ClientSecret: "your-client-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		Scopes:       []string{"openid", "profile", "email"},
		AuthURL:      "https://your-provider.com/oauth/authorize",
		TokenURL:     "https://your-provider.com/oauth/token",
		UserInfoURL:  "https://your-provider.com/oauth/userinfo",
		ProviderName: "custom-provider",

		// Custom user info parser
		UserInfoParser: func(userInfo map[string]any) (*UserContext, error) {
			// Extract custom fields from your provider
			return &UserContext{
				UserName:  userInfo["username"].(string),
				Email:     userInfo["email"].(string),
				RemoteID:  userInfo["id"].(string),
				UserLevel: 1,
				Roles:     []string{"user"},
				Claims:    userInfo,
			}, nil
		},
	})

	router := mux.NewRouter()

	router.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := oauth2Auth.OAuth2GenerateState()
		authURL, _ := oauth2Auth.OAuth2GetAuthURL("custom-provider", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	router.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := oauth2Auth.OAuth2HandleCallback(r.Context(), "custom-provider", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(loginResp)
	})

	http.ListenAndServe(":8080", router)
}

// Example: Multi-Provider OAuth2 with Security Integration
func ExampleOAuth2MultiProvider() {
	db, _ := sql.Open("postgres", "connection-string")

	// Create OAuth2 authenticators for multiple providers
	googleAuth := NewGoogleAuthenticator(
		"google-client-id",
		"google-client-secret",
		"http://localhost:8080/auth/google/callback",
		db,
	)

	githubAuth := NewGitHubAuthenticator(
		"github-client-id",
		"github-client-secret",
		"http://localhost:8080/auth/github/callback",
		db,
	)

	// Create column and row security providers
	colSec := NewDatabaseColumnSecurityProvider(db)
	rowSec := NewDatabaseRowSecurityProvider(db)

	router := mux.NewRouter()

	// Google OAuth2 routes
	router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := googleAuth.OAuth2GenerateState()
		authURL, _ := googleAuth.OAuth2GetAuthURL("google", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := googleAuth.OAuth2HandleCallback(r.Context(), "google", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResp.Token,
			Path:     "/",
			MaxAge:   int(loginResp.ExpiresIn),
			HttpOnly: true,
		})

		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	})

	// GitHub OAuth2 routes
	router.HandleFunc("/auth/github/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := githubAuth.OAuth2GenerateState()
		authURL, _ := githubAuth.OAuth2GetAuthURL("github", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	router.HandleFunc("/auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := githubAuth.OAuth2HandleCallback(r.Context(), "github", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResp.Token,
			Path:     "/",
			MaxAge:   int(loginResp.ExpiresIn),
			HttpOnly: true,
		})

		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	})

	// Use Google auth for protected routes (or GitHub - both work)
	provider, _ := NewCompositeSecurityProvider(googleAuth, colSec, rowSec)
	securityList, _ := NewSecurityList(provider)

	// Protected route with authentication
	protectedRouter := router.PathPrefix("/api").Subrouter()
	protectedRouter.Use(NewAuthMiddleware(securityList))
	protectedRouter.Use(SetSecurityMiddleware(securityList))

	protectedRouter.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		userCtx, _ := GetUserContext(r.Context())
		json.NewEncoder(w).Encode(userCtx)
	})

	http.ListenAndServe(":8080", router)
}

// Example: OAuth2 with Token Refresh
func ExampleOAuth2TokenRefresh() {
	db, _ := sql.Open("postgres", "connection-string")

	oauth2Auth := NewGoogleAuthenticator(
		"your-client-id",
		"your-client-secret",
		"http://localhost:8080/auth/google/callback",
		db,
	)

	router := mux.NewRouter()

	// Refresh token endpoint
	router.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RefreshToken string `json:"refresh_token"`
			Provider     string `json:"provider"` // "google", "github", etc.
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Default to google if not specified
		if req.Provider == "" {
			req.Provider = "google"
		}

		// Use OAuth2-specific refresh method
		loginResp, err := oauth2Auth.OAuth2RefreshToken(r.Context(), req.RefreshToken, req.Provider)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		// Set new session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResp.Token,
			Path:     "/",
			MaxAge:   int(loginResp.ExpiresIn),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})

		json.NewEncoder(w).Encode(loginResp)
	})

	http.ListenAndServe(":8080", router)
}

// Example: OAuth2 Logout
func ExampleOAuth2Logout() {
	db, _ := sql.Open("postgres", "connection-string")

	oauth2Auth := NewGoogleAuthenticator(
		"your-client-id",
		"your-client-secret",
		"http://localhost:8080/auth/google/callback",
		db,
	)

	router := mux.NewRouter()

	router.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			cookie, err := r.Cookie("session_token")
			if err == nil {
				token = cookie.Value
			}
		}

		if token != "" {
			// Get user ID from session
			userCtx, err := oauth2Auth.Authenticate(r)
			if err == nil {
				oauth2Auth.Logout(r.Context(), LogoutRequest{
					Token:  token,
					UserID: userCtx.UserID,
				})
			}
		}

		// Clear cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Logged out successfully"))
	})

	http.ListenAndServe(":8080", router)
}

// Example: Complete OAuth2 Integration with Database Setup
func ExampleOAuth2Complete() {
	db, _ := sql.Open("postgres", "connection-string")

	// Create tables (run once)
	setupOAuth2Tables(db)

	// Create OAuth2 authenticator
	oauth2Auth := NewGoogleAuthenticator(
		"your-client-id",
		"your-client-secret",
		"http://localhost:8080/auth/google/callback",
		db,
	)

	// Create security providers
	colSec := NewDatabaseColumnSecurityProvider(db)
	rowSec := NewDatabaseRowSecurityProvider(db)
	provider, _ := NewCompositeSecurityProvider(oauth2Auth, colSec, rowSec)
	securityList, _ := NewSecurityList(provider)

	router := mux.NewRouter()

	// Public routes
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome! <a href='/auth/google/login'>Login with Google</a>"))
	})

	router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := oauth2Auth.OAuth2GenerateState()
		authURL, _ := oauth2Auth.OAuth2GetAuthURL("github", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})

	router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		loginResp, err := oauth2Auth.OAuth2HandleCallback(r.Context(), "github", code, state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    loginResp.Token,
			Path:     "/",
			MaxAge:   int(loginResp.ExpiresIn),
			HttpOnly: true,
		})

		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	})

	// Protected routes
	protectedRouter := router.PathPrefix("/").Subrouter()
	protectedRouter.Use(NewAuthMiddleware(securityList))
	protectedRouter.Use(SetSecurityMiddleware(securityList))

	protectedRouter.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		userCtx, _ := GetUserContext(r.Context())
		w.Write([]byte(fmt.Sprintf("Welcome, %s! Your email: %s", userCtx.UserName, userCtx.Email)))
	})

	protectedRouter.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		userCtx, _ := GetUserContext(r.Context())
		json.NewEncoder(w).Encode(userCtx)
	})

	protectedRouter.HandleFunc("/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		userCtx, _ := GetUserContext(r.Context())
		oauth2Auth.Logout(r.Context(), LogoutRequest{
			Token:  userCtx.SessionID,
			UserID: userCtx.UserID,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "session_token",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})

		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	})

	http.ListenAndServe(":8080", router)
}

func setupOAuth2Tables(db *sql.DB) {
	// Create tables from database_schema.sql
	// This is a helper function - in production, use migrations
	ctx := context.Background()

	// Create users table if not exists
	db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username VARCHAR(255) NOT NULL UNIQUE,
			email VARCHAR(255) NOT NULL UNIQUE,
			password VARCHAR(255),
			user_level INTEGER DEFAULT 0,
			roles VARCHAR(500),
			is_active BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_login_at TIMESTAMP,
			remote_id VARCHAR(255),
			auth_provider VARCHAR(50)
		)
	`)

	// Create user_sessions table (used for both regular and OAuth2 sessions)
	db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS user_sessions (
			id SERIAL PRIMARY KEY,
			session_token VARCHAR(500) NOT NULL UNIQUE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			ip_address VARCHAR(45),
			user_agent TEXT,
			access_token TEXT,
			refresh_token TEXT,
			token_type VARCHAR(50) DEFAULT 'Bearer',
			auth_provider VARCHAR(50)
		)
	`)
}

// Example: All OAuth2 Providers at Once
func ExampleOAuth2AllProviders() {
	db, _ := sql.Open("postgres", "connection-string")

	// Create authenticator with ALL OAuth2 providers
	auth := NewDatabaseAuthenticator(db).
		WithOAuth2(OAuth2Config{
			ClientID:     "google-client-id",
			ClientSecret: "google-client-secret",
			RedirectURL:  "http://localhost:8080/auth/google/callback",
			Scopes:       []string{"openid", "profile", "email"},
			AuthURL:      "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
			ProviderName: "google",
		}).
		WithOAuth2(OAuth2Config{
			ClientID:     "github-client-id",
			ClientSecret: "github-client-secret",
			RedirectURL:  "http://localhost:8080/auth/github/callback",
			Scopes:       []string{"user:email"},
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			UserInfoURL:  "https://api.github.com/user",
			ProviderName: "github",
		}).
		WithOAuth2(OAuth2Config{
			ClientID:     "microsoft-client-id",
			ClientSecret: "microsoft-client-secret",
			RedirectURL:  "http://localhost:8080/auth/microsoft/callback",
			Scopes:       []string{"openid", "profile", "email"},
			AuthURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL:  "https://graph.microsoft.com/v1.0/me",
			ProviderName: "microsoft",
		}).
		WithOAuth2(OAuth2Config{
			ClientID:     "facebook-client-id",
			ClientSecret: "facebook-client-secret",
			RedirectURL:  "http://localhost:8080/auth/facebook/callback",
			Scopes:       []string{"email"},
			AuthURL:      "https://www.facebook.com/v12.0/dialog/oauth",
			TokenURL:     "https://graph.facebook.com/v12.0/oauth/access_token",
			UserInfoURL:  "https://graph.facebook.com/me?fields=id,name,email",
			ProviderName: "facebook",
		})

	// Get list of configured providers
	providers := auth.OAuth2GetProviders()
	fmt.Printf("Configured OAuth2 providers: %v\n", providers)

	router := mux.NewRouter()

	// Google routes
	router.HandleFunc("/auth/google/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := auth.OAuth2GenerateState()
		authURL, _ := auth.OAuth2GetAuthURL("google", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})
	router.HandleFunc("/auth/google/callback", func(w http.ResponseWriter, r *http.Request) {
		loginResp, err := auth.OAuth2HandleCallback(r.Context(), "google", r.URL.Query().Get("code"), r.URL.Query().Get("state"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(loginResp)
	})

	// GitHub routes
	router.HandleFunc("/auth/github/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := auth.OAuth2GenerateState()
		authURL, _ := auth.OAuth2GetAuthURL("github", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})
	router.HandleFunc("/auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		loginResp, err := auth.OAuth2HandleCallback(r.Context(), "github", r.URL.Query().Get("code"), r.URL.Query().Get("state"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(loginResp)
	})

	// Microsoft routes
	router.HandleFunc("/auth/microsoft/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := auth.OAuth2GenerateState()
		authURL, _ := auth.OAuth2GetAuthURL("microsoft", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})
	router.HandleFunc("/auth/microsoft/callback", func(w http.ResponseWriter, r *http.Request) {
		loginResp, err := auth.OAuth2HandleCallback(r.Context(), "microsoft", r.URL.Query().Get("code"), r.URL.Query().Get("state"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(loginResp)
	})

	// Facebook routes
	router.HandleFunc("/auth/facebook/login", func(w http.ResponseWriter, r *http.Request) {
		state, _ := auth.OAuth2GenerateState()
		authURL, _ := auth.OAuth2GetAuthURL("facebook", state)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
	})
	router.HandleFunc("/auth/facebook/callback", func(w http.ResponseWriter, r *http.Request) {
		loginResp, err := auth.OAuth2HandleCallback(r.Context(), "facebook", r.URL.Query().Get("code"), r.URL.Query().Get("state"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(loginResp)
	})

	// Create security list for protected routes
	colSec := NewDatabaseColumnSecurityProvider(db)
	rowSec := NewDatabaseRowSecurityProvider(db)
	provider, _ := NewCompositeSecurityProvider(auth, colSec, rowSec)
	securityList, _ := NewSecurityList(provider)

	// Protected routes work for ALL OAuth2 providers + regular sessions
	protectedRouter := router.PathPrefix("/api").Subrouter()
	protectedRouter.Use(NewAuthMiddleware(securityList))
	protectedRouter.Use(SetSecurityMiddleware(securityList))

	protectedRouter.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		userCtx, _ := GetUserContext(r.Context())
		json.NewEncoder(w).Encode(userCtx)
	})

	http.ListenAndServe(":8080", router)
}
