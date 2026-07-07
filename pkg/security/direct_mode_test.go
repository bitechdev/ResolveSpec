package security

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func futureTime() time.Time {
	return time.Now().Add(1 * time.Hour)
}

// newDirectTestDB opens a fresh in-memory SQLite database and applies the
// portable Direct-mode schema (database_schema_sqlite.sql), giving every
// Direct-mode test a real, isolated database to exercise end-to-end.
func newDirectTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1) // keep the shared in-memory db single-connection so state isn't lost
	t.Cleanup(func() { _ = db.Close() })

	schemaPath := filepath.Join("database_schema_sqlite.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("failed to apply schema: %v", err)
	}
	return db
}

func authenticatedRequest(token string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestDirectMode_RegisterThenLogin(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	regResp, err := auth.Register(ctx, RegisterRequest{
		Username: "alice",
		Password: "hunter2",
		Email:    "alice@example.com",
		Roles:    []string{"user", "admin"},
		Claims:   map[string]any{"ip_address": "127.0.0.1", "user_agent": "test-agent"},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if regResp.Token == "" || regResp.User == nil {
		t.Fatalf("Register() returned incomplete response: %+v", regResp)
	}
	if len(regResp.User.Roles) != 2 {
		t.Errorf("expected 2 roles, got %v", regResp.User.Roles)
	}

	loginResp, err := auth.Login(ctx, LoginRequest{Username: "alice", Password: "hunter2"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loginResp.User.UserName != "alice" {
		t.Errorf("Login() user = %q, want alice", loginResp.User.UserName)
	}

	// Duplicate registration should fail.
	if _, err := auth.Register(ctx, RegisterRequest{Username: "alice", Password: "x", Email: "other@example.com"}); err == nil {
		t.Error("expected duplicate username registration to fail")
	}
}

func TestDirectMode_SessionLifecycle(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	loginResp, err := auth.Register(ctx, RegisterRequest{Username: "bob", Password: "p", Email: "bob@example.com"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	userCtx, err := auth.Authenticate(authenticatedRequest(loginResp.Token))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if userCtx.UserName != "bob" {
		t.Errorf("Authenticate() user = %q, want bob", userCtx.UserName)
	}

	refreshResp, err := auth.RefreshToken(ctx, loginResp.Token)
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if refreshResp.Token == "" || refreshResp.Token == loginResp.Token {
		t.Errorf("RefreshToken() should return a new token, got %q", refreshResp.Token)
	}

	if err := auth.Logout(ctx, LogoutRequest{Token: refreshResp.Token, UserID: refreshResp.User.UserID}); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	if _, err := auth.RefreshToken(ctx, refreshResp.Token); err == nil {
		t.Error("expected RefreshToken() after logout to fail")
	}
}

func TestDirectMode_PasswordReset(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	if _, err := auth.Register(ctx, RegisterRequest{Username: "carol", Password: "old", Email: "carol@example.com"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resetResp, err := auth.RequestPasswordReset(ctx, PasswordResetRequest{Email: "carol@example.com"})
	if err != nil {
		t.Fatalf("RequestPasswordReset() error = %v", err)
	}
	if resetResp.Token == "" {
		t.Fatal("expected a non-empty reset token")
	}

	if err := auth.CompletePasswordReset(ctx, PasswordResetCompleteRequest{Token: resetResp.Token, NewPassword: "new"}); err != nil {
		t.Fatalf("CompletePasswordReset() error = %v", err)
	}

	// Reusing the same token should now fail.
	if err := auth.CompletePasswordReset(ctx, PasswordResetCompleteRequest{Token: resetResp.Token, NewPassword: "again"}); err == nil {
		t.Error("expected reusing a consumed reset token to fail")
	}
}

func TestDirectMode_JWTLoginAndLogout(t *testing.T) {
	db := newDirectTestDB(t)
	jwtAuth := NewJWTAuthenticator("secret", db).WithQueryMode(ModeDirect)
	directAuth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	if _, err := directAuth.Register(ctx, RegisterRequest{Username: "dave", Password: "p", Email: "dave@example.com"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resp, err := jwtAuth.Login(ctx, LoginRequest{Username: "dave", Password: "p"})
	if err != nil {
		t.Fatalf("JWTAuthenticator.Login() error = %v", err)
	}
	if resp.User.UserName != "dave" {
		t.Errorf("JWT login user = %q, want dave", resp.User.UserName)
	}

	if err := jwtAuth.Logout(ctx, LogoutRequest{Token: resp.Token, UserID: resp.User.UserID}); err != nil {
		t.Fatalf("JWTAuthenticator.Logout() error = %v", err)
	}
}

func TestDirectMode_TOTPEnableAndValidateBackupCode(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	regResp, err := auth.Register(ctx, RegisterRequest{Username: "erin", Password: "p", Email: "erin@example.com"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	userID := regResp.User.UserID

	totp := NewDatabaseTwoFactorProvider(db, nil).WithQueryMode(ModeDirect)

	if err := totp.Enable2FA(userID, "SECRET123", []string{"code1", "code2"}); err != nil {
		t.Fatalf("Enable2FA() error = %v", err)
	}

	enabled, err := totp.Get2FAStatus(userID)
	if err != nil {
		t.Fatalf("Get2FAStatus() error = %v", err)
	}
	if !enabled {
		t.Error("expected 2FA to be enabled")
	}

	secret, err := totp.Get2FASecret(userID)
	if err != nil {
		t.Fatalf("Get2FASecret() error = %v", err)
	}
	if secret != "SECRET123" {
		t.Errorf("Get2FASecret() = %q, want SECRET123", secret)
	}

	valid, err := totp.ValidateBackupCode(userID, "code1")
	if err != nil {
		t.Fatalf("ValidateBackupCode() error = %v", err)
	}
	if !valid {
		t.Error("expected backup code to be valid")
	}

	// Reusing the same backup code should fail.
	if _, err := totp.ValidateBackupCode(userID, "code1"); err == nil {
		t.Error("expected reusing a consumed backup code to fail")
	}

	if err := totp.Disable2FA(userID); err != nil {
		t.Fatalf("Disable2FA() error = %v", err)
	}
	enabled, err = totp.Get2FAStatus(userID)
	if err != nil {
		t.Fatalf("Get2FAStatus() error = %v", err)
	}
	if enabled {
		t.Error("expected 2FA to be disabled")
	}
}

func TestDirectMode_PasskeyStoreAndFetch(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	regResp, err := auth.Register(ctx, RegisterRequest{Username: "frank", Password: "p", Email: "frank@example.com"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	userID := regResp.User.UserID

	passkeys := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID: "example.com", RPName: "Example", RPOrigin: "https://example.com", QueryMode: ModeDirect,
	})

	cred, err := passkeys.CompleteRegistration(ctx, userID, PasskeyRegistrationResponse{
		RawID: []byte("credential-1"),
		Response: PasskeyAuthenticatorAttestationResponse{
			AttestationObject: []byte("public-key-bytes"),
		},
		Transports: []string{"internal", "usb"},
	}, nil)
	if err != nil {
		t.Fatalf("CompleteRegistration() error = %v", err)
	}
	if cred.UserID != userID {
		t.Errorf("CompleteRegistration() UserID = %d, want %d", cred.UserID, userID)
	}

	creds, err := passkeys.GetCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetCredentials() error = %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	if string(creds[0].CredentialID) != "credential-1" {
		t.Errorf("GetCredentials() CredentialID = %q, want credential-1", creds[0].CredentialID)
	}
	if len(creds[0].Transports) != 2 {
		t.Errorf("expected 2 transports, got %v", creds[0].Transports)
	}

	credentialIDB64 := base64.StdEncoding.EncodeToString(creds[0].CredentialID)

	if err := passkeys.UpdateCredentialName(ctx, userID, credentialIDB64, "My Phone"); err != nil {
		t.Fatalf("UpdateCredentialName() error = %v", err)
	}

	updated, err := passkeys.GetCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetCredentials() error = %v", err)
	}
	if updated[0].Name != "My Phone" {
		t.Errorf("expected updated name 'My Phone', got %q", updated[0].Name)
	}

	if err := passkeys.DeleteCredential(ctx, userID, credentialIDB64); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	remaining, err := passkeys.GetCredentials(ctx, userID)
	if err != nil {
		t.Fatalf("GetCredentials() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 credentials after delete, got %d", len(remaining))
	}
}

func TestDirectMode_OAuthGetOrCreateUserAndSession(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	userCtx := &UserContext{UserName: "gina", Email: "gina@example.com", Roles: []string{"user"}}
	userID, err := auth.oauth2GetOrCreateUser(ctx, userCtx, "google")
	if err != nil {
		t.Fatalf("oauth2GetOrCreateUser() error = %v", err)
	}
	if userID == 0 {
		t.Fatal("expected non-zero user ID")
	}

	// Calling again with the same email should return the same user, not create a duplicate.
	userID2, err := auth.oauth2GetOrCreateUser(ctx, userCtx, "google")
	if err != nil {
		t.Fatalf("oauth2GetOrCreateUser() second call error = %v", err)
	}
	if userID2 != userID {
		t.Errorf("expected same user ID on repeat call, got %d and %d", userID, userID2)
	}
}

func TestDirectMode_KeyStoreCreateAndValidate(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	regResp, err := auth.Register(ctx, RegisterRequest{Username: "henry", Password: "p", Email: "henry@example.com"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ks := NewDatabaseKeyStore(db, DatabaseKeyStoreOptions{QueryMode: ModeDirect})

	createResp, err := ks.CreateKey(ctx, CreateKeyRequest{
		UserID:  regResp.User.UserID,
		KeyType: KeyTypeGenericAPI,
		Name:    "test key",
		Scopes:  []string{"read", "write"},
		Meta:    map[string]any{"note": "test"},
	})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if createResp.RawKey == "" {
		t.Fatal("expected a non-empty raw key")
	}

	validated, err := ks.ValidateKey(ctx, createResp.RawKey, KeyTypeGenericAPI)
	if err != nil {
		t.Fatalf("ValidateKey() error = %v", err)
	}
	if validated.UserID != regResp.User.UserID {
		t.Errorf("ValidateKey() UserID = %d, want %d", validated.UserID, regResp.User.UserID)
	}
	if len(validated.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %v", validated.Scopes)
	}

	keys, err := ks.GetUserKeys(ctx, regResp.User.UserID, "")
	if err != nil {
		t.Fatalf("GetUserKeys() error = %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	if err := ks.DeleteKey(ctx, regResp.User.UserID, keys[0].ID); err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}

	remaining, err := ks.GetUserKeys(ctx, regResp.User.UserID, "")
	if err != nil {
		t.Fatalf("GetUserKeys() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 keys after delete, got %d", len(remaining))
	}
}

func TestDirectMode_OAuthServerClientAndCode(t *testing.T) {
	db := newDirectTestDB(t)
	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{QueryMode: ModeDirect})
	ctx := context.Background()

	client := &OAuthServerClient{
		ClientID:     "client-1",
		RedirectURIs: []string{"https://app.example.com/callback"},
		ClientName:   "Example App",
	}
	registered, err := auth.OAuthRegisterClient(ctx, client)
	if err != nil {
		t.Fatalf("OAuthRegisterClient() error = %v", err)
	}
	if len(registered.GrantTypes) != 1 || registered.GrantTypes[0] != "authorization_code" {
		t.Errorf("expected default grant types, got %v", registered.GrantTypes)
	}
	if len(registered.AllowedScopes) != 3 {
		t.Errorf("expected default allowed scopes, got %v", registered.AllowedScopes)
	}

	fetched, err := auth.OAuthGetClient(ctx, "client-1")
	if err != nil {
		t.Fatalf("OAuthGetClient() error = %v", err)
	}
	if fetched.ClientName != "Example App" {
		t.Errorf("OAuthGetClient() ClientName = %q, want %q", fetched.ClientName, "Example App")
	}
	if len(fetched.RedirectURIs) != 1 || fetched.RedirectURIs[0] != "https://app.example.com/callback" {
		t.Errorf("OAuthGetClient() RedirectURIs = %v", fetched.RedirectURIs)
	}

	regResp, err := auth.Register(ctx, RegisterRequest{Username: "ivan", Password: "p", Email: "ivan@example.com"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := auth.OAuthSaveCode(ctx, &OAuthCode{
		Code:          "auth-code-2",
		ClientID:      "client-1",
		RedirectURI:   "https://app.example.com/callback",
		CodeChallenge: "challenge",
		SessionToken:  regResp.Token,
		Scopes:        []string{"openid", "profile"},
		ExpiresAt:     futureTime(),
	}); err != nil {
		t.Fatalf("OAuthSaveCode() error = %v", err)
	}

	exchanged, err := auth.OAuthExchangeCode(ctx, "auth-code-2")
	if err != nil {
		t.Fatalf("OAuthExchangeCode() error = %v", err)
	}
	if exchanged.ClientID != "client-1" {
		t.Errorf("OAuthExchangeCode() ClientID = %q, want client-1", exchanged.ClientID)
	}
	if len(exchanged.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %v", exchanged.Scopes)
	}

	// Code should be single-use.
	if _, err := auth.OAuthExchangeCode(ctx, "auth-code-2"); err == nil {
		t.Error("expected re-exchanging a used code to fail")
	}

	info, err := auth.OAuthIntrospectToken(ctx, regResp.Token)
	if err != nil {
		t.Fatalf("OAuthIntrospectToken() error = %v", err)
	}
	if !info.Active {
		t.Error("expected token to be active")
	}
	if info.Username != "ivan" {
		t.Errorf("OAuthIntrospectToken() Username = %q, want ivan", info.Username)
	}

	if err := auth.OAuthRevokeToken(ctx, regResp.Token); err != nil {
		t.Fatalf("OAuthRevokeToken() error = %v", err)
	}

	info, err = auth.OAuthIntrospectToken(ctx, regResp.Token)
	if err != nil {
		t.Fatalf("OAuthIntrospectToken() after revoke error = %v", err)
	}
	if info.Active {
		t.Error("expected token to be inactive after revoke")
	}
}
