package procedure

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/security/lookup"
	"github.com/bitechdev/ResolveSpec/pkg/security/sectypes"
)

// Passkey implements lookup.PasskeyStore with the resolvespec_passkey_* procedures.
// Credential ids cross the lookup interface as base64 text; the procedures that take a
// bytea credential id receive the decoded bytes.
type Passkey struct {
	run   Runner
	procs lookup.ProcNames
}

var _ lookup.PasskeyStore = (*Passkey)(nil)

// NewPasskey creates the procedure-backed PasskeyStore.
func NewPasskey(run Runner, procs lookup.ProcNames) *Passkey {
	return &Passkey{run: run, procs: procs}
}

func decodeCredentialID(b64 string) ([]byte, error) {
	id, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid credential ID: %w", err)
	}
	return id, nil
}

// Store implements lookup.PasskeyStore.
func (p *Passkey) Store(ctx context.Context, rec lookup.PasskeyCredentialRecord) (int64, error) {
	credJSON, err := json.Marshal(map[string]any{
		"user_id":          rec.UserID,
		"credential_id":    rec.CredentialID,
		"public_key":       rec.PublicKey,
		"attestation_type": rec.AttestationType,
		"sign_count":       rec.SignCount,
		"transports":       rec.Transports,
		"backup_eligible":  rec.BackupEligible,
		"backup_state":     rec.BackupState,
		"name":             rec.Name,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal credential data: %w", err)
	}
	var success bool
	var errorMsg sql.NullString
	var credentialID sql.NullInt64
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_credential_id FROM %s($1::jsonb)`, p.procs.PasskeyStoreCredential)
		return db.QueryRowContext(ctx, query, string(credJSON)).Scan(&success, &errorMsg, &credentialID)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to store credential: %w", err)
	}
	if !success {
		return 0, failure(errorMsg, "failed to store credential")
	}
	return credentialID.Int64, nil
}

// Get implements lookup.PasskeyStore.
func (p *Passkey) Get(ctx context.Context, credentialID string) (userID int, signCount uint32, err error) {
	raw, err := decodeCredentialID(credentialID)
	if err != nil {
		return 0, 0, err
	}
	var success bool
	var errorMsg, credentialJSON sql.NullString
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_credential::text FROM %s($1)`, p.procs.PasskeyGetCredential)
		return db.QueryRowContext(ctx, query, raw).Scan(&success, &errorMsg, &credentialJSON)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get credential: %w", err)
	}
	if !success {
		return 0, 0, failure(errorMsg, "credential not found")
	}
	var cred struct {
		UserID    int    `json:"user_id"`
		SignCount uint32 `json:"sign_count"`
	}
	if err := json.Unmarshal(normalizeTimes([]byte(credentialJSON.String)), &cred); err != nil {
		return 0, 0, fmt.Errorf("failed to parse credential: %w", err)
	}
	return cred.UserID, cred.SignCount, nil
}

// UpdateCounter implements lookup.PasskeyStore. Like the code it replaces, it only reports
// an error when the query itself fails; the procedure's success flag is not checked.
func (p *Passkey) UpdateCounter(ctx context.Context, credentialID string, newCounter uint32) (bool, error) {
	raw, err := decodeCredentialID(credentialID)
	if err != nil {
		return false, err
	}
	var success bool
	var errorMsg sql.NullString
	var cloneWarning sql.NullBool
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_clone_warning FROM %s($1, $2)`, p.procs.PasskeyUpdateCounter)
		return db.QueryRowContext(ctx, query, raw, newCounter).Scan(&success, &errorMsg, &cloneWarning)
	})
	if err != nil {
		return false, err
	}
	return cloneWarning.Valid && cloneWarning.Bool, nil
}

// List implements lookup.PasskeyStore.
func (p *Passkey) List(ctx context.Context, userID int) ([]sectypes.PasskeyCredential, error) {
	var success bool
	var errorMsg, credentialsJSON sql.NullString
	err := p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_credentials::text FROM %s($1)`, p.procs.PasskeyGetUserCredentials)
		return db.QueryRowContext(ctx, query, userID).Scan(&success, &errorMsg, &credentialsJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "failed to get credentials")
	}

	var rawCreds []struct {
		ID              int       `json:"id"`
		UserID          int       `json:"user_id"`
		CredentialID    string    `json:"credential_id"`
		PublicKey       string    `json:"public_key"`
		AttestationType string    `json:"attestation_type"`
		AAGUID          string    `json:"aaguid"`
		SignCount       uint32    `json:"sign_count"`
		CloneWarning    bool      `json:"clone_warning"`
		Transports      []string  `json:"transports"`
		BackupEligible  bool      `json:"backup_eligible"`
		BackupState     bool      `json:"backup_state"`
		Name            string    `json:"name"`
		CreatedAt       time.Time `json:"created_at"`
		LastUsedAt      time.Time `json:"last_used_at"`
	}
	if err := json.Unmarshal(normalizeTimes([]byte(credentialsJSON.String)), &rawCreds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	credentials := make([]sectypes.PasskeyCredential, 0, len(rawCreds))
	for i := range rawCreds {
		raw := rawCreds[i]
		credID, err := base64.StdEncoding.DecodeString(raw.CredentialID)
		if err != nil {
			continue
		}
		pubKey, err := base64.StdEncoding.DecodeString(raw.PublicKey)
		if err != nil {
			continue
		}
		aaguid, _ := base64.StdEncoding.DecodeString(raw.AAGUID)
		credentials = append(credentials, sectypes.PasskeyCredential{
			ID:              fmt.Sprintf("%d", raw.ID),
			UserID:          raw.UserID,
			CredentialID:    credID,
			PublicKey:       pubKey,
			AttestationType: raw.AttestationType,
			AAGUID:          aaguid,
			SignCount:       raw.SignCount,
			CloneWarning:    raw.CloneWarning,
			Transports:      raw.Transports,
			BackupEligible:  raw.BackupEligible,
			BackupState:     raw.BackupState,
			Name:            raw.Name,
			CreatedAt:       raw.CreatedAt,
			LastUsedAt:      raw.LastUsedAt,
		})
	}
	return credentials, nil
}

// Delete implements lookup.PasskeyStore.
func (p *Passkey) Delete(ctx context.Context, userID int, credentialID string) error {
	raw, err := decodeCredentialID(credentialID)
	if err != nil {
		return err
	}
	var success bool
	var errorMsg sql.NullString
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2)`, p.procs.PasskeyDeleteCredential)
		return db.QueryRowContext(ctx, query, userID, raw).Scan(&success, &errorMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to delete credential: %w", err)
	}
	if !success {
		return failure(errorMsg, "failed to delete credential")
	}
	return nil
}

// Rename implements lookup.PasskeyStore.
func (p *Passkey) Rename(ctx context.Context, userID int, credentialID, name string) error {
	raw, err := decodeCredentialID(credentialID)
	if err != nil {
		return err
	}
	var success bool
	var errorMsg sql.NullString
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error FROM %s($1, $2, $3)`, p.procs.PasskeyUpdateName)
		return db.QueryRowContext(ctx, query, userID, raw, name).Scan(&success, &errorMsg)
	})
	if err != nil {
		return fmt.Errorf("failed to update credential name: %w", err)
	}
	if !success {
		return failure(errorMsg, "failed to update credential name")
	}
	return nil
}

// ByUsername implements lookup.PasskeyStore.
func (p *Passkey) ByUsername(ctx context.Context, username string) (int, []lookup.PasskeyCredentialRef, error) {
	var success bool
	var errorMsg, credentialsJSON sql.NullString
	var userID sql.NullInt64
	err := p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_user_id, p_credentials::text FROM %s($1)`, p.procs.PasskeyGetCredsByUsername)
		return db.QueryRowContext(ctx, query, username).Scan(&success, &errorMsg, &userID, &credentialsJSON)
	})
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	if !success {
		return 0, nil, failure(errorMsg, "failed to get credentials")
	}
	var creds []lookup.PasskeyCredentialRef
	if err := json.Unmarshal([]byte(credentialsJSON.String), &creds); err != nil {
		return 0, nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return int(userID.Int64), creds, nil
}

// Login implements lookup.PasskeyStore: it creates the session for a user whose passkey
// assertion was already verified.
func (p *Passkey) Login(ctx context.Context, userID int, claims map[string]any) (*sectypes.LoginResponse, error) {
	reqData := map[string]any{"user_id": userID}
	if claims != nil {
		if ip, ok := claims["ip_address"].(string); ok {
			reqData["ip_address"] = ip
		}
		if ua, ok := claims["user_agent"].(string); ok {
			reqData["user_agent"] = ua
		}
	}
	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal passkey login request: %w", err)
	}
	var success bool
	var errorMsg, dataJSON sql.NullString
	err = p.run.Run(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT p_success, p_error, p_data::text FROM %s($1::jsonb)`, p.procs.PasskeyLogin)
		return db.QueryRowContext(ctx, query, string(reqJSON)).Scan(&success, &errorMsg, &dataJSON)
	})
	if err != nil {
		return nil, fmt.Errorf("passkey login query failed: %w", err)
	}
	if !success {
		return nil, failure(errorMsg, "passkey login failed")
	}
	var response sectypes.LoginResponse
	if err := json.Unmarshal([]byte(dataJSON.String), &response); err != nil {
		return nil, fmt.Errorf("failed to parse passkey login response: %w", err)
	}
	return &response, nil
}
