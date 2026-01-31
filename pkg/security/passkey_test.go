package security

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDatabasePasskeyProvider_BeginRegistration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:     "example.com",
		RPName:   "Example App",
		RPOrigin: "https://example.com",
	})

	ctx := context.Background()

	// Mock get credentials query
	rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_credentials"}).
		AddRow(true, nil, "[]")
	mock.ExpectQuery(`SELECT p_success, p_error, p_credentials::text FROM resolvespec_passkey_get_user_credentials`).
		WithArgs(1).
		WillReturnRows(rows)

	opts, err := provider.BeginRegistration(ctx, 1, "testuser", "Test User")
	if err != nil {
		t.Fatalf("BeginRegistration failed: %v", err)
	}

	if opts.RelyingParty.ID != "example.com" {
		t.Errorf("expected RP ID 'example.com', got '%s'", opts.RelyingParty.ID)
	}

	if opts.User.Name != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", opts.User.Name)
	}

	if len(opts.Challenge) != 32 {
		t.Errorf("expected challenge length 32, got %d", len(opts.Challenge))
	}

	if len(opts.PubKeyCredParams) != 2 {
		t.Errorf("expected 2 credential params, got %d", len(opts.PubKeyCredParams))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabasePasskeyProvider_BeginAuthentication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:     "example.com",
		RPName:   "Example App",
		RPOrigin: "https://example.com",
	})

	ctx := context.Background()

	// Mock get credentials by username query
	rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_user_id", "p_credentials"}).
		AddRow(true, nil, 1, `[{"credential_id":"YWJjZGVm","transports":["internal"]}]`)
	mock.ExpectQuery(`SELECT p_success, p_error, p_user_id, p_credentials::text FROM resolvespec_passkey_get_credentials_by_username`).
		WithArgs("testuser").
		WillReturnRows(rows)

	opts, err := provider.BeginAuthentication(ctx, "testuser")
	if err != nil {
		t.Fatalf("BeginAuthentication failed: %v", err)
	}

	if opts.RelyingPartyID != "example.com" {
		t.Errorf("expected RP ID 'example.com', got '%s'", opts.RelyingPartyID)
	}

	if len(opts.Challenge) != 32 {
		t.Errorf("expected challenge length 32, got %d", len(opts.Challenge))
	}

	if len(opts.AllowCredentials) != 1 {
		t.Errorf("expected 1 allowed credential, got %d", len(opts.AllowCredentials))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabasePasskeyProvider_GetCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:   "example.com",
		RPName: "Example App",
	})

	ctx := context.Background()

	credentialsJSON := `[{
		"id": 1,
		"user_id": 1,
		"credential_id": "YWJjZGVmMTIzNDU2",
		"public_key": "cHVibGlja2V5",
		"attestation_type": "none",
		"aaguid": "",
		"sign_count": 5,
		"clone_warning": false,
		"transports": ["internal"],
		"backup_eligible": true,
		"backup_state": false,
		"name": "My Phone",
		"created_at": "2026-01-01T00:00:00Z",
		"last_used_at": "2026-01-31T00:00:00Z"
	}]`

	rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_credentials"}).
		AddRow(true, nil, credentialsJSON)
	mock.ExpectQuery(`SELECT p_success, p_error, p_credentials::text FROM resolvespec_passkey_get_user_credentials`).
		WithArgs(1).
		WillReturnRows(rows)

	credentials, err := provider.GetCredentials(ctx, 1)
	if err != nil {
		t.Fatalf("GetCredentials failed: %v", err)
	}

	if len(credentials) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(credentials))
	}

	cred := credentials[0]
	if cred.UserID != 1 {
		t.Errorf("expected user ID 1, got %d", cred.UserID)
	}
	if cred.Name != "My Phone" {
		t.Errorf("expected name 'My Phone', got '%s'", cred.Name)
	}
	if cred.SignCount != 5 {
		t.Errorf("expected sign count 5, got %d", cred.SignCount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabasePasskeyProvider_DeleteCredential(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:   "example.com",
		RPName: "Example App",
	})

	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"p_success", "p_error"}).
		AddRow(true, nil)
	mock.ExpectQuery(`SELECT p_success, p_error FROM resolvespec_passkey_delete_credential`).
		WithArgs(1, sqlmock.AnyArg()).
		WillReturnRows(rows)

	err = provider.DeleteCredential(ctx, 1, "YWJjZGVmMTIzNDU2")
	if err != nil {
		t.Errorf("DeleteCredential failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabasePasskeyProvider_UpdateCredentialName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:   "example.com",
		RPName: "Example App",
	})

	ctx := context.Background()

	rows := sqlmock.NewRows([]string{"p_success", "p_error"}).
		AddRow(true, nil)
	mock.ExpectQuery(`SELECT p_success, p_error FROM resolvespec_passkey_update_name`).
		WithArgs(1, sqlmock.AnyArg(), "New Name").
		WillReturnRows(rows)

	err = provider.UpdateCredentialName(ctx, 1, "YWJjZGVmMTIzNDU2", "New Name")
	if err != nil {
		t.Errorf("UpdateCredentialName failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabaseAuthenticator_PasskeyMethods(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	passkeyProvider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:   "example.com",
		RPName: "Example App",
	})

	auth := NewDatabaseAuthenticatorWithOptions(db, DatabaseAuthenticatorOptions{
		PasskeyProvider: passkeyProvider,
	})

	ctx := context.Background()

	t.Run("BeginPasskeyRegistration", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_credentials"}).
			AddRow(true, nil, "[]")
		mock.ExpectQuery(`SELECT p_success, p_error, p_credentials::text FROM resolvespec_passkey_get_user_credentials`).
			WithArgs(1).
			WillReturnRows(rows)

		opts, err := auth.BeginPasskeyRegistration(ctx, PasskeyBeginRegistrationRequest{
			UserID:      1,
			Username:    "testuser",
			DisplayName: "Test User",
		})

		if err != nil {
			t.Errorf("BeginPasskeyRegistration failed: %v", err)
		}

		if opts == nil {
			t.Error("expected options, got nil")
		}
	})

	t.Run("GetPasskeyCredentials", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"p_success", "p_error", "p_credentials"}).
			AddRow(true, nil, "[]")
		mock.ExpectQuery(`SELECT p_success, p_error, p_credentials::text FROM resolvespec_passkey_get_user_credentials`).
			WithArgs(1).
			WillReturnRows(rows)

		credentials, err := auth.GetPasskeyCredentials(ctx, 1)
		if err != nil {
			t.Errorf("GetPasskeyCredentials failed: %v", err)
		}

		if credentials == nil {
			t.Error("expected credentials slice, got nil")
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDatabaseAuthenticator_WithoutPasskey(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer db.Close()

	auth := NewDatabaseAuthenticator(db)
	ctx := context.Background()

	_, err = auth.BeginPasskeyRegistration(ctx, PasskeyBeginRegistrationRequest{
		UserID:      1,
		Username:    "testuser",
		DisplayName: "Test User",
	})

	if err == nil {
		t.Error("expected error when passkey provider not configured, got nil")
	}

	expectedMsg := "passkey provider not configured"
	if err.Error() != expectedMsg {
		t.Errorf("expected error '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestPasskeyProvider_NilDB(t *testing.T) {
	// This test verifies that the provider can be created with nil DB
	// but operations will fail. In production, always provide a valid DB.
	var db *sql.DB
	provider := NewDatabasePasskeyProvider(db, DatabasePasskeyProviderOptions{
		RPID:   "example.com",
		RPName: "Example App",
	})

	if provider == nil {
		t.Error("expected provider to be created even with nil DB")
	}

	// Verify that the provider has the correct configuration
	if provider.rpID != "example.com" {
		t.Errorf("expected RP ID 'example.com', got '%s'", provider.rpID)
	}
}
