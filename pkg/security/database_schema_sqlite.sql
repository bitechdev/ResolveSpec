-- Portable schema for Direct-mode (non-stored-procedure) operation.
-- Plain CREATE TABLE statements only, no functions/triggers, using types
-- understood by SQLite (and portable to MySQL). Used by Direct-mode tests
-- and as a reference for deployments without Postgres.

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255),
    user_level INTEGER DEFAULT 0,
    roles VARCHAR(500),
    is_active BOOLEAN DEFAULT 1,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    last_login_at TIMESTAMP,
    program_user_id INTEGER DEFAULT 0,
    program_user_table VARCHAR(255) DEFAULT '',
    remote_id VARCHAR(255),
    auth_provider VARCHAR(50),
    totp_secret VARCHAR(255),
    totp_enabled BOOLEAN DEFAULT 0,
    totp_enabled_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_token VARCHAR(500) NOT NULL UNIQUE,
    user_id INTEGER NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP,
    last_activity_at TIMESTAMP,
    ip_address VARCHAR(45),
    user_agent TEXT,
    access_token TEXT,
    refresh_token TEXT,
    token_type VARCHAR(50) DEFAULT 'Bearer',
    auth_provider VARCHAR(50)
);

CREATE INDEX IF NOT EXISTS idx_session_token ON user_sessions(session_token);
CREATE INDEX IF NOT EXISTS idx_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_expires_at ON user_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_token ON user_sessions(refresh_token);

CREATE TABLE IF NOT EXISTS token_blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token VARCHAR(500) NOT NULL,
    user_id INTEGER,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_totp_backup_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    code_hash VARCHAR(64) NOT NULL,
    used BOOLEAN DEFAULT 0,
    used_at TIMESTAMP,
    created_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_totp_user_id ON user_totp_backup_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_totp_code_hash ON user_totp_backup_codes(code_hash);

CREATE TABLE IF NOT EXISTS user_passkey_credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    credential_id TEXT NOT NULL UNIQUE, -- base64 text (Direct mode), not native bytea
    public_key TEXT NOT NULL,           -- base64 text
    attestation_type VARCHAR(50) DEFAULT 'none',
    aaguid TEXT,                        -- base64 text
    sign_count INTEGER DEFAULT 0,
    clone_warning BOOLEAN DEFAULT 0,
    transports TEXT,                    -- JSON-encoded []string
    backup_eligible BOOLEAN DEFAULT 0,
    backup_state BOOLEAN DEFAULT 0,
    name VARCHAR(255),
    created_at TIMESTAMP,
    last_used_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_passkey_user_id ON user_passkey_credentials(user_id);
CREATE INDEX IF NOT EXISTS idx_passkey_credential_id ON user_passkey_credentials(credential_id);

CREATE TABLE IF NOT EXISTS user_password_resets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP,
    used BOOLEAN DEFAULT 0,
    used_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS oauth_clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id VARCHAR(255) NOT NULL UNIQUE,
    redirect_uris TEXT NOT NULL,   -- JSON-encoded []string
    client_name VARCHAR(255),
    grant_types TEXT,              -- JSON-encoded []string
    allowed_scopes TEXT,           -- JSON-encoded []string
    is_active BOOLEAN DEFAULT 1,
    created_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS oauth_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code VARCHAR(255) NOT NULL UNIQUE,
    client_id VARCHAR(255) NOT NULL,
    redirect_uri TEXT NOT NULL,
    client_state TEXT,
    code_challenge VARCHAR(255) NOT NULL,
    code_challenge_method VARCHAR(10) DEFAULT 'S256',
    session_token TEXT NOT NULL,
    refresh_token TEXT,
    scopes TEXT,                   -- JSON-encoded []string
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oauth_codes_code ON oauth_codes(code);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_codes(expires_at);

CREATE TABLE IF NOT EXISTS user_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    key_type VARCHAR(50) NOT NULL,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL DEFAULT '',
    scopes TEXT,                   -- JSON-encoded []string
    meta TEXT,                     -- JSON-encoded map
    expires_at TIMESTAMP,
    created_at TIMESTAMP,
    last_used_at TIMESTAMP,
    is_active BOOLEAN DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_user_keys_user_id  ON user_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_user_keys_key_hash ON user_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_user_keys_key_type ON user_keys(key_type);
