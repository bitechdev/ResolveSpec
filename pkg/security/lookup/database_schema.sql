-- Database Schema for DatabaseAuthenticator
-- ============================================

-- pgcrypto provides gen_random_bytes(), crypt() and gen_salt(); it is required
-- for session token generation and for password hashing/verification below.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255), -- bcrypt hash (nullable for OAuth2 users); legacy cleartext is accepted at login (upgrade to bcrypt is opt-in)
    user_level INTEGER DEFAULT 0,
    roles VARCHAR(500), -- Comma-separated roles: "admin,manager,user"
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP,
    -- Program-level user mapping
    program_user_id INTEGER DEFAULT 0,
    program_user_table VARCHAR(255) DEFAULT '',
    -- OAuth2 fields
    remote_id VARCHAR(255), -- Provider's user ID (e.g., Google sub, GitHub id)
    auth_provider VARCHAR(50), -- 'local', 'google', 'github', 'microsoft', 'facebook', etc.
    -- Two-Factor Authentication fields
    totp_secret VARCHAR(255), -- Base32 encoded TOTP secret (encrypted recommended)
    totp_enabled BOOLEAN DEFAULT false,
    totp_enabled_at TIMESTAMP
);

-- User sessions table for DatabaseAuthenticator and OAuth2Authenticator
CREATE TABLE IF NOT EXISTS user_sessions (
    id SERIAL PRIMARY KEY,
    session_token VARCHAR(500) NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45), -- IPv4 or IPv6
    user_agent TEXT,
    -- OAuth2 fields (nullable for non-OAuth2 sessions)
    access_token TEXT,
    refresh_token TEXT,
    token_type VARCHAR(50) DEFAULT 'Bearer',
    auth_provider VARCHAR(50) -- 'local', 'google', 'github', 'microsoft', 'facebook', etc.
);

CREATE INDEX IF NOT EXISTS idx_session_token ON user_sessions(session_token);
CREATE INDEX IF NOT EXISTS idx_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_expires_at ON user_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_token ON user_sessions(refresh_token);

-- Optional: Token blacklist for logout tracking (useful for JWT too)
CREATE TABLE IF NOT EXISTS token_blacklist (
    id SERIAL PRIMARY KEY,
    token VARCHAR(500) NOT NULL,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_token ON token_blacklist(token);
CREATE INDEX IF NOT EXISTS idx_blacklist_expires_at ON token_blacklist(expires_at);

-- Two-Factor Authentication backup codes table
CREATE TABLE IF NOT EXISTS user_totp_backup_codes (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash VARCHAR(64) NOT NULL, -- SHA-256 hash of backup code
    used BOOLEAN DEFAULT false,
    used_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_totp_user_id ON user_totp_backup_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_totp_code_hash ON user_totp_backup_codes(code_hash);

-- Example: Seed admin user (password should be hashed with bcrypt)
-- INSERT INTO users (username, email, password, user_level, roles, is_active)
-- VALUES ('admin', 'admin@example.com', '$2a$10$...', 10, 'admin,user', true);

-- Cleanup expired sessions (run periodically)
-- DELETE FROM user_sessions WHERE expires_at < NOW();

-- Cleanup expired blacklisted tokens (run periodically)
-- DELETE FROM token_blacklist WHERE expires_at < NOW();

-- ============================================
-- Stored Procedures for DatabaseAuthenticator
-- ============================================

-- 1. resolvespec_login - Authenticates user and creates session
-- Input: LoginRequest as jsonb {username: string, password: string, claims: object}
-- Output: p_success (bool), p_error (text), p_data (LoginResponse as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_login(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_password_hash TEXT;
    v_supplied_password TEXT;
    v_password_ok BOOLEAN := false;
    v_session_token TEXT;
    v_expires_at TIMESTAMP;
    v_ip_address TEXT;
    v_user_agent TEXT;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Extract login request fields
    v_username := p_request->>'username';
    v_supplied_password := p_request->>'password';
    v_ip_address := p_request->'claims'->>'ip_address';
    v_user_agent := p_request->'claims'->>'user_agent';

    -- Validate user credentials
    SELECT id, username, email, password, user_level, roles, program_user_id, program_user_table
    INTO v_user_id, v_username, v_email, v_password_hash, v_user_level, v_roles, v_program_user_id, v_program_user_table
    FROM users
    WHERE username = v_username AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Verify the password. bcrypt hashes are checked with crypt(); a legacy
    -- cleartext value is still accepted (and only rewritten as bcrypt if the
    -- upgrade is explicitly enabled).
    -- bcrypt only uses the first 72 bytes, so longer input is rejected.
    IF v_password_hash IS NOT NULL AND v_password_hash <> ''
       AND v_supplied_password IS NOT NULL AND v_supplied_password <> ''
       AND octet_length(v_supplied_password) <= 72 THEN
        IF v_password_hash ~ '^\$2[aby]\$' THEN
            v_password_ok := (crypt(v_supplied_password, v_password_hash) = v_password_hash);
        ELSE
            v_password_ok := (v_password_hash = v_supplied_password);
            -- Upgrading the stored value is opt-in:
            --   ALTER DATABASE <db> SET resolvespec.upgrade_password_hash = 'on';
            IF v_password_ok AND COALESCE(current_setting('resolvespec.upgrade_password_hash', true), 'off') = 'on' THEN
                UPDATE users SET password = crypt(v_supplied_password, gen_salt('bf')), updated_at = now()
                WHERE id = v_user_id;
            END IF;
        END IF;
    END IF;

    IF NOT v_password_ok THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Generate session token
    v_session_token := 'sess_' || encode(gen_random_bytes(32), 'hex') || '_' || extract(epoch from now())::bigint::text;
    v_expires_at := now() + interval '24 hours';

    -- Create session
    INSERT INTO user_sessions (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at)
    VALUES (v_session_token, v_user_id, v_expires_at, v_ip_address, v_user_agent, now());

    -- Update last login time
    UPDATE users SET last_login_at = now() WHERE id = v_user_id;

    -- Return success with LoginResponse
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'token', v_session_token,
            'user', jsonb_build_object(
                'user_id', v_user_id,
                'user_name', v_username,
                'email', v_email,
                'user_level', v_user_level,
                'roles', string_to_array(COALESCE(v_roles, ''), ','),
                'session_id', v_session_token,
                'program_user_id', COALESCE(v_program_user_id, 0),
                'program_user_table', COALESCE(v_program_user_table, '')
            ),
            'expires_in', 86400 -- 24 hours in seconds
        );
END;
$$ LANGUAGE plpgsql;

-- 2. resolvespec_logout - Invalidates session
-- Input: LogoutRequest as jsonb {token: string, user_id: int}
-- Output: p_success (bool), p_error (text), p_data (jsonb)
CREATE OR REPLACE FUNCTION resolvespec_logout(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_token TEXT;
    v_user_id INTEGER;
    v_deleted INTEGER;
BEGIN
    v_token := p_request->>'token';
    v_user_id := (p_request->>'user_id')::integer;

    -- Remove Bearer prefix if present
    v_token := regexp_replace(v_token, '^Bearer ', '', 'i');

    -- Delete the session
    DELETE FROM user_sessions
    WHERE session_token = v_token AND user_id = v_user_id;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;

    IF v_deleted = 0 THEN
        RETURN QUERY SELECT false, 'Session not found'::text, NULL::jsonb;
    ELSE
        RETURN QUERY SELECT true, NULL::text, jsonb_build_object('success', true);
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 3. resolvespec_session - Validates session and returns user context
-- Input: sessionid (text), reference (text)
-- Output: p_success (bool), p_error (text), p_user (UserContext as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_session(p_session_token text, p_reference text)
RETURNS TABLE(p_success boolean, p_error text, p_user jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_session_id TEXT;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Query session and user data
    SELECT
        s.user_id, u.username, u.email, u.user_level, u.roles, s.session_token,
        u.program_user_id, u.program_user_table
    INTO
        v_user_id, v_username, v_email, v_user_level, v_roles, v_session_id,
        v_program_user_id, v_program_user_table
    FROM user_sessions s
    JOIN users u ON s.user_id = u.id
    WHERE s.session_token = p_session_token
      AND s.expires_at > now()
      AND u.is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid or expired session'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Return UserContext
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'user_id', v_user_id,
            'user_name', v_username,
            'email', v_email,
            'user_level', v_user_level,
            'session_id', v_session_id,
            'roles', string_to_array(COALESCE(v_roles, ''), ','),
            'program_user_id', COALESCE(v_program_user_id, 0),
            'program_user_table', COALESCE(v_program_user_table, '')
        );
END;
$$ LANGUAGE plpgsql;

-- 4. resolvespec_session_update - Updates session activity timestamp
-- Input: sessionid (text), user_context (jsonb)
-- Output: p_success (bool), p_error (text), p_user (UserContext as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_session_update(p_session_token text, p_user_context jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_user jsonb) AS $$
DECLARE
    v_updated INTEGER;
BEGIN
    -- Update last activity timestamp
    UPDATE user_sessions
    SET last_activity_at = now()
    WHERE session_token = p_session_token AND expires_at > now();

    GET DIAGNOSTICS v_updated = ROW_COUNT;

    IF v_updated = 0 THEN
        RETURN QUERY SELECT false, 'Session not found or expired'::text, NULL::jsonb;
    ELSE
        -- Return the user context as-is
        RETURN QUERY SELECT true, NULL::text, p_user_context;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 5. resolvespec_refresh_token - Generates new session from existing one
-- Input: sessionid (text), user_context (jsonb)
-- Output: p_success (bool), p_error (text), p_user (UserContext as jsonb with new session_id)
CREATE OR REPLACE FUNCTION resolvespec_refresh_token(p_old_session_token text, p_user_context jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_user jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_new_session_token TEXT;
    v_expires_at TIMESTAMP;
    v_ip_address TEXT;
    v_user_agent TEXT;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Verify old session exists and is valid
    SELECT s.user_id, u.username, u.email, u.user_level, u.roles, s.ip_address, s.user_agent,
           u.program_user_id, u.program_user_table
    INTO v_user_id, v_username, v_email, v_user_level, v_roles, v_ip_address, v_user_agent,
         v_program_user_id, v_program_user_table
    FROM user_sessions s
    JOIN users u ON s.user_id = u.id
    WHERE s.session_token = p_old_session_token
      AND s.expires_at > now()
      AND u.is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid or expired refresh token'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Generate new session token
    v_new_session_token := 'sess_' || encode(gen_random_bytes(32), 'hex') || '_' || extract(epoch from now())::bigint::text;
    v_expires_at := now() + interval '24 hours';

    -- Create new session
    INSERT INTO user_sessions (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at)
    VALUES (v_new_session_token, v_user_id, v_expires_at, v_ip_address, v_user_agent, now());

    -- Delete old session
    DELETE FROM user_sessions WHERE session_token = p_old_session_token;

    -- Return UserContext with new session_id
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'user_id', v_user_id,
            'user_name', v_username,
            'email', v_email,
            'user_level', v_user_level,
            'session_id', v_new_session_token,
            'roles', string_to_array(COALESCE(v_roles, ''), ','),
            'program_user_id', COALESCE(v_program_user_id, 0),
            'program_user_table', COALESCE(v_program_user_table, '')
        );
END;
$$ LANGUAGE plpgsql;

-- 6. resolvespec_jwt_login - JWT-based login (queries user and returns data for JWT token generation)
-- Input: username (text), password (text)
-- Output: p_success (bool), p_error (text), p_user (user data as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_jwt_login(p_username text, p_password text)
RETURNS TABLE(p_success boolean, p_error text, p_user jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_password TEXT;
    v_password_ok BOOLEAN := false;
    v_user_level INTEGER;
    v_roles TEXT;
BEGIN
    -- Query user data
    SELECT id, username, email, password, user_level, roles
    INTO v_user_id, v_username, v_email, v_password, v_user_level, v_roles
    FROM users
    WHERE username = p_username AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Verify the password (bcrypt, or legacy cleartext).
    IF v_password IS NOT NULL AND v_password <> ''
       AND p_password IS NOT NULL AND p_password <> ''
       AND octet_length(p_password) <= 72 THEN
        IF v_password ~ '^\$2[aby]\$' THEN
            v_password_ok := (crypt(p_password, v_password) = v_password);
        ELSE
            v_password_ok := (v_password = p_password);
            -- Upgrading the stored value is opt-in (see resolvespec_login).
            IF v_password_ok AND COALESCE(current_setting('resolvespec.upgrade_password_hash', true), 'off') = 'on' THEN
                UPDATE users SET password = crypt(p_password, gen_salt('bf')), updated_at = now()
                WHERE id = v_user_id;
            END IF;
        END IF;
    END IF;

    IF NOT v_password_ok THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Return user data for JWT token generation
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'id', v_user_id,
            'username', v_username,
            'email', v_email,
            'user_level', v_user_level,
            'roles', v_roles
        );
END;
$$ LANGUAGE plpgsql;

-- 7. resolvespec_jwt_logout - Adds token to blacklist
-- Input: token (text), user_id (int)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_jwt_logout(p_token text, p_user_id integer)
RETURNS TABLE(p_success boolean, p_error text) AS $$
BEGIN
    -- Add token to blacklist
    INSERT INTO token_blacklist (token, user_id, expires_at)
    VALUES (p_token, p_user_id, now() + interval '24 hours');

    RETURN QUERY SELECT true, NULL::text;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text;
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- Column / row security tables
-- ============================================
-- A rule applies either to one user (user_id) or to every member of a group
-- (group_id via sec_group_members); exactly one of the two is set.

CREATE TABLE IF NOT EXISTS sec_group_members (
    group_id INTEGER NOT NULL,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE IF NOT EXISTS sec_column_rules (
    id           SERIAL PRIMARY KEY,
    user_id      INTEGER REFERENCES users(id) ON DELETE CASCADE,
    group_id     INTEGER,
    schema_name  TEXT NOT NULL,
    table_name   TEXT NOT NULL,
    column_path  TEXT NOT NULL,            -- dot path under the table: col or col.sub.field
    access_type  TEXT NOT NULL,            -- e.g. mask, hide, read
    mask_start   INTEGER DEFAULT 0,
    mask_end     INTEGER DEFAULT 0,
    mask_invert  BOOLEAN DEFAULT false,
    mask_char    TEXT DEFAULT '*',
    extra_filters TEXT,                    -- JSON object
    is_active    BOOLEAN NOT NULL DEFAULT true,
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_sec_column_rules_table ON sec_column_rules(lower(schema_name), lower(table_name));

CREATE TABLE IF NOT EXISTS sec_row_rules (
    id          SERIAL PRIMARY KEY,
    user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    group_id    INTEGER,
    schema_name TEXT NOT NULL,
    table_name  TEXT NOT NULL,
    template    TEXT,                      -- SQL fragment, e.g. 'user_id = {UserID}'
    has_block   BOOLEAN NOT NULL DEFAULT false,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_sec_row_rules_table ON sec_row_rules(lower(schema_name), lower(table_name));

-- 8. resolvespec_column_security - Loads column security rules for user
-- Input: user_id (int), schema (text), table_name (text)
-- Output: p_success (bool), p_error (text), p_rules (array of security rules as jsonb)
-- Rules are the active sec_column_rules for the exact schema + table (case-insensitive)
-- that belong to the user or to a group the user is a member of.
-- 'control' is returned as schema.table.column_path.
CREATE OR REPLACE FUNCTION resolvespec_column_security(p_user_id integer, p_schema text, p_table_name text)
RETURNS TABLE(p_success boolean, p_error text, p_rules jsonb) AS $$
DECLARE
    v_rules jsonb;
BEGIN
    SELECT jsonb_agg(
        jsonb_build_object(
            'control', r.schema_name || '.' || r.table_name || '.' || r.column_path,
            'accesstype', r.access_type,
            'jsonvalue', COALESCE(r.extra_filters, '')
        )
    )
    INTO v_rules
    FROM sec_column_rules r
    WHERE r.is_active = true
      AND lower(r.schema_name) = lower(p_schema)
      AND lower(r.table_name) = lower(p_table_name)
      AND (
          r.user_id = p_user_id
          OR r.group_id IN (SELECT m.group_id FROM sec_group_members m WHERE m.user_id = p_user_id)
      );

    IF v_rules IS NULL THEN
        v_rules := '[]'::jsonb;
    END IF;

    RETURN QUERY SELECT true, NULL::text, v_rules;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, '[]'::jsonb;
END;
$$ LANGUAGE plpgsql;

-- 9. resolvespec_row_security - Loads the row security template for user
-- Input: schema (text), table_name (text), user_id (int)
-- Output: p_template (text), p_block (bool)
-- Applicable rules = active sec_row_rules of the user and of the user's groups for the exact
-- schema + table. Any has_block wins (template empty); otherwise templates are AND-combined,
-- each wrapped in parentheses.
CREATE OR REPLACE FUNCTION resolvespec_row_security(p_schema text, p_table_name text, p_user_id integer)
RETURNS TABLE(p_template text, p_block boolean) AS $$
DECLARE
    v_block boolean;
    v_template text;
BEGIN
    SELECT COALESCE(bool_or(r.has_block), false),
           COALESCE(string_agg('(' || r.template || ')', ' AND ' ORDER BY r.id)
                    FILTER (WHERE r.template IS NOT NULL AND r.template <> ''), '')
    INTO v_block, v_template
    FROM sec_row_rules r
    WHERE r.is_active = true
      AND lower(r.schema_name) = lower(p_schema)
      AND lower(r.table_name) = lower(p_table_name)
      AND (
          r.user_id = p_user_id
          OR r.group_id IN (SELECT m.group_id FROM sec_group_members m WHERE m.user_id = p_user_id)
      );

    IF v_block THEN
        v_template := '';
    END IF;

    RETURN QUERY SELECT v_template, v_block;
END;
$$ LANGUAGE plpgsql;

-- 10. resolvespec_register - Registers a new user and creates session
-- Input: RegisterRequest as jsonb {username: string, password: string, email: string, claims: object, meta: object}
-- (user_level / roles in the request are ignored; new users are unprivileged)
-- Output: p_success (bool), p_error (text), p_data (LoginResponse as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_register(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_password TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_session_token TEXT;
    v_expires_at TIMESTAMP;
    v_ip_address TEXT;
    v_user_agent TEXT;
    v_roles_array TEXT[];
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Extract registration request fields
    v_username := p_request->>'username';
    v_email := p_request->>'email';
    v_password := p_request->>'password';
    -- Privileges are never taken from the request: self-registration always
    -- creates an unprivileged user (level 0, no roles, no program user link).
    v_user_level := 0;
    v_roles := '';
    v_ip_address := p_request->'claims'->>'ip_address';
    v_user_agent := p_request->'claims'->>'user_agent';
    v_program_user_id := 0;
    v_program_user_table := '';

    -- Validate required fields
    IF v_username IS NULL OR v_username = '' THEN
        RETURN QUERY SELECT false, 'Username is required'::text, NULL::jsonb;
        RETURN;
    END IF;

    IF v_email IS NULL OR v_email = '' THEN
        RETURN QUERY SELECT false, 'Email is required'::text, NULL::jsonb;
        RETURN;
    END IF;

    IF v_password IS NULL OR v_password = '' THEN
        RETURN QUERY SELECT false, 'Password is required'::text, NULL::jsonb;
        RETURN;
    END IF;

    IF octet_length(v_password) > 72 THEN
        RETURN QUERY SELECT false, 'Password must be at most 72 bytes'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Check if username already exists
    IF EXISTS (SELECT 1 FROM users WHERE username = v_username) THEN
        RETURN QUERY SELECT false, 'Username already exists'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Check if email already exists
    IF EXISTS (SELECT 1 FROM users WHERE email = v_email) THEN
        RETURN QUERY SELECT false, 'Email already exists'::text, NULL::jsonb;
        RETURN;
    END IF;

    v_password := crypt(v_password, gen_salt('bf'));

    -- Create new user
    INSERT INTO users (username, email, password, user_level, roles, is_active, created_at, updated_at, program_user_id, program_user_table)
    VALUES (v_username, v_email, v_password, v_user_level, v_roles, true, now(), now(), v_program_user_id, v_program_user_table)
    RETURNING id INTO v_user_id;

    -- Generate session token
    v_session_token := 'sess_' || encode(gen_random_bytes(32), 'hex') || '_' || extract(epoch from now())::bigint::text;
    v_expires_at := now() + interval '24 hours';

    -- Create session
    INSERT INTO user_sessions (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at)
    VALUES (v_session_token, v_user_id, v_expires_at, v_ip_address, v_user_agent, now());

    -- Update last login time
    UPDATE users SET last_login_at = now() WHERE id = v_user_id;

    -- Return success with LoginResponse
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'token', v_session_token,
            'user', jsonb_build_object(
                'user_id', v_user_id,
                'user_name', v_username,
                'email', v_email,
                'user_level', v_user_level,
                'roles', string_to_array(COALESCE(v_roles, ''), ','),
                'session_id', v_session_token,
                'program_user_id', v_program_user_id,
                'program_user_table', v_program_user_table
            ),
            'expires_in', 86400 -- 24 hours in seconds
        );
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::jsonb;
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- Example: Test stored procedures
-- ============================================

-- Test register
-- SELECT * FROM resolvespec_register('{"username": "newuser", "password": "test123", "email": "newuser@example.com", "user_level": 1, "roles": ["user"], "claims": {"ip_address": "127.0.0.1", "user_agent": "test"}}'::jsonb);

-- Test login
-- SELECT * FROM resolvespec_login('{"username": "admin", "password": "test123", "claims": {"ip_address": "127.0.0.1", "user_agent": "test"}}'::jsonb);

-- Test session validation
-- SELECT * FROM resolvespec_session('sess_abc123', 'test_reference');

-- Test session update
-- SELECT * FROM resolvespec_session_update('sess_abc123', '{"user_id": 1, "user_name": "admin"}'::jsonb);

-- Test token refresh
-- SELECT * FROM resolvespec_refresh_token('sess_abc123', '{"user_id": 1, "user_name": "admin"}'::jsonb);

-- Test logout
-- SELECT * FROM resolvespec_logout('{"token": "sess_abc123", "user_id": 1}'::jsonb);

-- Test JWT login
-- SELECT * FROM resolvespec_jwt_login('admin', 'password123');

-- Test JWT logout
-- SELECT * FROM resolvespec_jwt_logout('jwt_token_here', 1);

-- Test column security
-- SELECT * FROM resolvespec_column_security(1, 'public', 'users');

-- Test row security
-- SELECT * FROM resolvespec_row_security('public', 'users', 1);

-- ============================================
-- OAuth2 Stored Procedures
-- ============================================

-- 11. resolvespec_oauth_getorcreateuser - Gets existing user by email or creates new OAuth2 user
-- Input: p_user_data (jsonb) {username: string, email: string, remote_id: string, user_level: int, roles: array, auth_provider: string}
-- Output: p_success (bool), p_error (text), p_user_id (int)
CREATE OR REPLACE FUNCTION resolvespec_oauth_getorcreateuser(p_user_data jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_user_id integer) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_remote_id TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_auth_provider TEXT;
BEGIN
    -- Extract user data
    v_username := p_user_data->>'username';
    v_email := p_user_data->>'email';
    v_remote_id := p_user_data->>'remote_id';
    v_user_level := COALESCE((p_user_data->>'user_level')::integer, 0);
    v_auth_provider := COALESCE(p_user_data->>'auth_provider', 'oauth2');
    
    -- Convert roles array to comma-separated string
    SELECT array_to_string(ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(p_user_data->'roles') = 'array' THEN p_user_data->'roles' ELSE '[]'::jsonb END)), ',')
    INTO v_roles;

    -- Try to find existing user by email
    SELECT id INTO v_user_id FROM users WHERE email = v_email;

    IF FOUND THEN
        -- Update last login and remote_id if not set
        UPDATE users 
        SET last_login_at = now(), 
            updated_at = now(),
            remote_id = COALESCE(remote_id, v_remote_id),
            auth_provider = COALESCE(auth_provider, v_auth_provider)
        WHERE id = v_user_id;
        
        RETURN QUERY SELECT true, NULL::text, v_user_id;
        RETURN;
    END IF;

    -- Create new user (OAuth2 users don't have password)
    INSERT INTO users (username, email, password, user_level, roles, is_active, created_at, updated_at, last_login_at, remote_id, auth_provider)
    VALUES (v_username, v_email, NULL, v_user_level, v_roles, true, now(), now(), now(), v_remote_id, v_auth_provider)
    RETURNING id INTO v_user_id;

    RETURN QUERY SELECT true, NULL::text, v_user_id;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::integer;
END;
$$ LANGUAGE plpgsql;

-- 12. resolvespec_oauth_createsession - Creates or updates OAuth2 session in user_sessions table
-- Input: p_session_data (jsonb) {session_token: string, user_id: int, access_token: string, refresh_token: string, token_type: string, expires_at: timestamp, auth_provider: string}
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_oauth_createsession(p_session_data jsonb)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_session_token TEXT;
    v_user_id INTEGER;
    v_access_token TEXT;
    v_refresh_token TEXT;
    v_token_type TEXT;
    v_expires_at TIMESTAMP;
    v_auth_provider TEXT;
BEGIN
    -- Extract session data
    v_session_token := p_session_data->>'session_token';
    v_user_id := (p_session_data->>'user_id')::integer;
    v_access_token := p_session_data->>'access_token';
    v_refresh_token := p_session_data->>'refresh_token';
    v_token_type := COALESCE(p_session_data->>'token_type', 'Bearer');
    v_expires_at := (p_session_data->>'expires_at')::timestamptz::timestamp;
    v_auth_provider := COALESCE(p_session_data->>'auth_provider', 'oauth2');

    -- Insert or update session
    INSERT INTO user_sessions (
        session_token, user_id, expires_at, created_at, last_activity_at,
        access_token, refresh_token, token_type, auth_provider
    )
    VALUES (
        v_session_token, v_user_id, v_expires_at, now(), now(),
        v_access_token, v_refresh_token, v_token_type, v_auth_provider
    )
    ON CONFLICT (session_token) DO UPDATE
    SET access_token = EXCLUDED.access_token,
        refresh_token = EXCLUDED.refresh_token,
        token_type = EXCLUDED.token_type,
        expires_at = EXCLUDED.expires_at,
        last_activity_at = now();

    RETURN QUERY SELECT true, NULL::text;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text;
END;
$$ LANGUAGE plpgsql;

-- 13. resolvespec_oauth_getsession - Gets OAuth2 session and user data by session token
-- Input: p_session_token (text)
-- Output: p_success (bool), p_error (text), p_data (jsonb) with user and session info
CREATE OR REPLACE FUNCTION resolvespec_oauth_getsession(p_session_token text)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_expires_at TIMESTAMP;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Query session and user data from user_sessions table
    SELECT
        s.user_id, u.username, u.email, u.user_level, u.roles, s.expires_at,
        u.program_user_id, u.program_user_table
    INTO
        v_user_id, v_username, v_email, v_user_level, v_roles, v_expires_at,
        v_program_user_id, v_program_user_table
    FROM user_sessions s
    JOIN users u ON s.user_id = u.id
    WHERE s.session_token = p_session_token
      AND s.expires_at > now()
      AND u.is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid or expired session'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Return user context
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'user_id', v_user_id,
            'user_name', v_username,
            'email', v_email,
            'user_level', v_user_level,
            'session_id', p_session_token,
            'roles', string_to_array(COALESCE(v_roles, ''), ','),
            'program_user_id', COALESCE(v_program_user_id, 0),
            'program_user_table', COALESCE(v_program_user_table, '')
        );
END;
$$ LANGUAGE plpgsql;

-- 14. resolvespec_oauth_deletesession - Deletes OAuth2 session from user_sessions (logout)
-- Input: p_session_token (text)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_oauth_deletesession(p_session_token text)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_deleted INTEGER;
BEGIN
    -- Delete the session
    DELETE FROM user_sessions
    WHERE session_token = p_session_token;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;

    IF v_deleted = 0 THEN
        RETURN QUERY SELECT false, 'Session not found'::text;
    ELSE
        RETURN QUERY SELECT true, NULL::text;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 15. resolvespec_oauth_getrefreshtoken - Gets OAuth2 session data by refresh token from user_sessions
-- Input: p_refresh_token (text)
-- Output: p_success (bool), p_error (text), p_data (jsonb) with session info
CREATE OR REPLACE FUNCTION resolvespec_oauth_getrefreshtoken(p_refresh_token text)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_access_token TEXT;
    v_token_type TEXT;
    v_expires_at TIMESTAMP;
BEGIN
    -- Query session by refresh token
    SELECT
        user_id, access_token, token_type, expires_at
    INTO
        v_user_id, v_access_token, v_token_type, v_expires_at
    FROM user_sessions
    WHERE refresh_token = p_refresh_token
      AND expires_at > now();

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Refresh token not found or expired'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Return session data
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'user_id', v_user_id,
            'access_token', v_access_token,
            'token_type', v_token_type,
            'expiry', v_expires_at
        );
END;
$$ LANGUAGE plpgsql;

-- 16. resolvespec_oauth_updaterefreshtoken - Updates OAuth2 session with new tokens in user_sessions
-- Input: p_update_data (jsonb) {user_id: int, old_refresh_token: string, new_session_token: string, new_access_token: string, new_refresh_token: string, expires_at: timestamp}
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_oauth_updaterefreshtoken(p_update_data jsonb)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_user_id INTEGER;
    v_old_refresh_token TEXT;
    v_new_session_token TEXT;
    v_new_access_token TEXT;
    v_new_refresh_token TEXT;
    v_expires_at TIMESTAMP;
    v_updated INTEGER;
BEGIN
    -- Extract update data
    v_user_id := (p_update_data->>'user_id')::integer;
    v_old_refresh_token := p_update_data->>'old_refresh_token';
    v_new_session_token := p_update_data->>'new_session_token';
    v_new_access_token := p_update_data->>'new_access_token';
    v_new_refresh_token := p_update_data->>'new_refresh_token';
    v_expires_at := (p_update_data->>'expires_at')::timestamptz::timestamp;

    -- Update session in user_sessions table
    UPDATE user_sessions
    SET session_token = v_new_session_token,
        access_token = v_new_access_token,
        refresh_token = v_new_refresh_token,
        expires_at = v_expires_at,
        last_activity_at = now()
    WHERE user_id = v_user_id
      AND refresh_token = v_old_refresh_token;

    GET DIAGNOSTICS v_updated = ROW_COUNT;

    IF v_updated = 0 THEN
        RETURN QUERY SELECT false, 'Session not found'::text;
    ELSE
        RETURN QUERY SELECT true, NULL::text;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 17. resolvespec_oauth_getuser - Gets user data by user ID for OAuth2 token refresh
-- Input: p_user_id (int)
-- Output: p_success (bool), p_error (text), p_data (jsonb) with user info
CREATE OR REPLACE FUNCTION resolvespec_oauth_getuser(p_user_id integer)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
BEGIN
    -- Query user data
    SELECT username, email, user_level, roles, program_user_id, program_user_table
    INTO v_username, v_email, v_user_level, v_roles, v_program_user_id, v_program_user_table
    FROM users
    WHERE id = p_user_id
      AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Return user data
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'user_id', p_user_id,
            'user_name', v_username,
            'email', v_email,
            'user_level', v_user_level,
            'roles', string_to_array(COALESCE(v_roles, ''), ','),
            'program_user_id', COALESCE(v_program_user_id, 0),
            'program_user_table', COALESCE(v_program_user_table, '')
        );
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- Example: Test OAuth2 stored procedures
-- ============================================

-- Test get or create user
-- SELECT * FROM resolvespec_oauth_getorcreateuser('{"username": "johndoe", "email": "john@example.com", "remote_id": "google-123", "user_level": 1, "roles": ["user"], "auth_provider": "google"}'::jsonb);

-- Test create session
-- SELECT * FROM resolvespec_oauth_createsession('{"session_token": "sess_abc123", "user_id": 1, "access_token": "access_token_xyz", "refresh_token": "refresh_token_xyz", "token_type": "Bearer", "expires_at": "2026-02-01 00:00:00", "auth_provider": "google"}'::jsonb);

-- Test get session
-- SELECT * FROM resolvespec_oauth_getsession('sess_abc123');

-- Test delete session
-- SELECT * FROM resolvespec_oauth_deletesession('sess_abc123');

-- Test get refresh token
-- SELECT * FROM resolvespec_oauth_getrefreshtoken('refresh_token_xyz');

-- Test update refresh token
-- SELECT * FROM resolvespec_oauth_updaterefreshtoken('{"user_id": 1, "old_refresh_token": "refresh_token_xyz", "new_session_token": "sess_new123", "new_access_token": "new_access_token", "new_refresh_token": "new_refresh_token", "expires_at": "2026-02-02 00:00:00"}'::jsonb);

-- Test get user
-- SELECT * FROM resolvespec_oauth_getuser(1);


-- ============================================
-- Stored Procedures for Two-Factor Authentication
-- ============================================

-- 1. resolvespec_totp_enable - Enable 2FA for a user
-- Input: p_user_id (integer), p_secret (text), p_backup_codes (jsonb array)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_totp_enable(
    p_user_id INTEGER,
    p_secret TEXT,
    p_backup_codes jsonb
)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_code TEXT;
    v_code_hash TEXT;
BEGIN
    -- Update user record with TOTP secret
    UPDATE users
    SET totp_secret = p_secret,
        totp_enabled = true,
        totp_enabled_at = CURRENT_TIMESTAMP
    WHERE id = p_user_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text;
        RETURN;
    END IF;

    -- Delete old backup codes
    DELETE FROM user_totp_backup_codes WHERE user_id = p_user_id;

    -- Insert new backup codes
    FOR i IN 0..jsonb_array_length(p_backup_codes)-1 LOOP
        v_code_hash := p_backup_codes->>i;
        INSERT INTO user_totp_backup_codes (user_id, code_hash)
        VALUES (p_user_id, v_code_hash);
    END LOOP;

    RETURN QUERY SELECT true, NULL::text;
END;
$$ LANGUAGE plpgsql;

-- 2. resolvespec_totp_disable - Disable 2FA for a user
-- Input: p_user_id (integer)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_totp_disable(p_user_id INTEGER)
RETURNS TABLE(p_success boolean, p_error text) AS $$
BEGIN
    -- Clear TOTP secret and disable 2FA
    UPDATE users
    SET totp_secret = NULL,
        totp_enabled = false
    WHERE id = p_user_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text;
        RETURN;
    END IF;

    -- Delete all backup codes
    DELETE FROM user_totp_backup_codes WHERE user_id = p_user_id;

    RETURN QUERY SELECT true, NULL::text;
END;
$$ LANGUAGE plpgsql;

-- 3. resolvespec_totp_get_status - Check if user has 2FA enabled
-- Input: p_user_id (integer)
-- Output: p_success (bool), p_error (text), p_enabled (bool)
CREATE OR REPLACE FUNCTION resolvespec_totp_get_status(p_user_id INTEGER)
RETURNS TABLE(p_success boolean, p_error text, p_enabled boolean) AS $$
DECLARE
    v_enabled BOOLEAN;
BEGIN
    SELECT totp_enabled
    INTO v_enabled
    FROM users
    WHERE id = p_user_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text, false;
        RETURN;
    END IF;

    RETURN QUERY SELECT true, NULL::text, COALESCE(v_enabled, false);
END;
$$ LANGUAGE plpgsql;

-- 4. resolvespec_totp_get_secret - Get user's TOTP secret
-- Input: p_user_id (integer)
-- Output: p_success (bool), p_error (text), p_secret (text)
CREATE OR REPLACE FUNCTION resolvespec_totp_get_secret(p_user_id INTEGER)
RETURNS TABLE(p_success boolean, p_error text, p_secret text) AS $$
DECLARE
    v_secret TEXT;
    v_enabled BOOLEAN;
BEGIN
    SELECT totp_secret, totp_enabled
    INTO v_secret, v_enabled
    FROM users
    WHERE id = p_user_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text, NULL::text;
        RETURN;
    END IF;

    IF NOT COALESCE(v_enabled, false) THEN
        RETURN QUERY SELECT false, 'TOTP not enabled for user'::text, NULL::text;
        RETURN;
    END IF;

    RETURN QUERY SELECT true, NULL::text, v_secret;
END;
$$ LANGUAGE plpgsql;

-- 5. resolvespec_totp_regenerate_backup_codes - Generate new backup codes
-- Input: p_user_id (integer), p_backup_codes (jsonb array of hashed codes)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_totp_regenerate_backup_codes(
    p_user_id INTEGER,
    p_backup_codes jsonb
)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_code_hash TEXT;
BEGIN
    -- Verify user exists and has 2FA enabled
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = p_user_id AND totp_enabled = true) THEN
        RETURN QUERY SELECT false, 'User not found or TOTP not enabled'::text;
        RETURN;
    END IF;

    -- Delete old backup codes
    DELETE FROM user_totp_backup_codes WHERE user_id = p_user_id;

    -- Insert new backup codes
    FOR i IN 0..jsonb_array_length(p_backup_codes)-1 LOOP
        v_code_hash := p_backup_codes->>i;
        INSERT INTO user_totp_backup_codes (user_id, code_hash)
        VALUES (p_user_id, v_code_hash);
    END LOOP;

    RETURN QUERY SELECT true, NULL::text;
END;
$$ LANGUAGE plpgsql;

-- 6. resolvespec_totp_validate_backup_code - Validate and mark backup code as used
-- Input: p_user_id (integer), p_code_hash (text)
-- Output: p_success (bool), p_error (text), p_valid (bool)
CREATE OR REPLACE FUNCTION resolvespec_totp_validate_backup_code(
    p_user_id INTEGER,
    p_code_hash TEXT
)
RETURNS TABLE(p_success boolean, p_error text, p_valid boolean) AS $$
DECLARE
    v_code_id INTEGER;
    v_used BOOLEAN;
BEGIN
    -- Find the backup code
    SELECT id, used
    INTO v_code_id, v_used
    FROM user_totp_backup_codes
    WHERE user_id = p_user_id AND code_hash = p_code_hash;

    IF NOT FOUND THEN
        RETURN QUERY SELECT true, NULL::text, false;
        RETURN;
    END IF;

    -- Check if already used
    IF v_used THEN
        RETURN QUERY SELECT false, 'Backup code already used'::text, false;
        RETURN;
    END IF;

    -- Mark as used
    UPDATE user_totp_backup_codes
    SET used = true, used_at = CURRENT_TIMESTAMP
    WHERE id = v_code_id;

    RETURN QUERY SELECT true, NULL::text, true;
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- Example: Test TOTP stored procedures
-- ============================================

-- Enable 2FA
-- SELECT * FROM resolvespec_totp_enable(1, 'JBSWY3DPEHPK3PXP', '["abc123", "def456"]'::jsonb);

-- Disable 2FA
-- SELECT * FROM resolvespec_totp_disable(1);

-- Get 2FA status
-- SELECT * FROM resolvespec_totp_get_status(1);

-- Get TOTP secret
-- SELECT * FROM resolvespec_totp_get_secret(1);

-- Regenerate backup codes
-- SELECT * FROM resolvespec_totp_regenerate_backup_codes(1, '["new123", "new456"]'::jsonb);

-- Validate backup code
-- SELECT * FROM resolvespec_totp_validate_backup_code(1, 'abc123');

-- ============================================
-- Passkey/WebAuthn Credentials Table
-- ============================================

-- Passkey credentials table for WebAuthn/FIDO2 authentication
CREATE TABLE IF NOT EXISTS user_passkey_credentials (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE, -- Raw credential ID from authenticator
    public_key BYTEA NOT NULL, -- COSE public key
    attestation_type VARCHAR(50) DEFAULT 'none', -- none, indirect, direct
    aaguid BYTEA, -- Authenticator AAGUID
    sign_count INTEGER DEFAULT 0, -- Signature counter for clone detection
    clone_warning BOOLEAN DEFAULT false, -- True if cloning detected
    transports TEXT[], -- Array of transports: usb, nfc, ble, internal
    backup_eligible BOOLEAN DEFAULT false, -- Credential can be backed up
    backup_state BOOLEAN DEFAULT false, -- Credential is currently backed up
    name VARCHAR(255), -- User-friendly name for the credential
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_passkey_user_id ON user_passkey_credentials(user_id);
CREATE INDEX IF NOT EXISTS idx_passkey_credential_id ON user_passkey_credentials(credential_id);

-- ============================================
-- Stored Procedures for Passkey Authentication
-- ============================================

-- 1. resolvespec_passkey_store_credential - Store a new passkey credential
-- Input: p_credential (jsonb) {user_id: int, credential_id: bytea, public_key: bytea, attestation_type: string, aaguid: bytea, sign_count: int, transports: array, backup_eligible: bool, backup_state: bool, name: string}
-- Output: p_success (bool), p_error (text), p_credential_id (int)
CREATE OR REPLACE FUNCTION resolvespec_passkey_store_credential(p_credential jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_credential_id integer) AS $$
DECLARE
    v_credential_id INTEGER;
    v_user_id INTEGER;
    v_cred_id BYTEA;
    v_public_key BYTEA;
    v_attestation_type TEXT;
    v_aaguid BYTEA;
    v_sign_count INTEGER;
    v_transports TEXT[];
    v_backup_eligible BOOLEAN;
    v_backup_state BOOLEAN;
    v_name TEXT;
BEGIN
    -- Extract credential data
    v_user_id := (p_credential->>'user_id')::integer;
    v_cred_id := decode(p_credential->>'credential_id', 'base64');
    v_public_key := decode(p_credential->>'public_key', 'base64');
    v_attestation_type := COALESCE(p_credential->>'attestation_type', 'none');
    v_aaguid := decode(COALESCE(p_credential->>'aaguid', ''), 'base64');
    v_sign_count := COALESCE((p_credential->>'sign_count')::integer, 0);
    v_backup_eligible := COALESCE((p_credential->>'backup_eligible')::boolean, false);
    v_backup_state := COALESCE((p_credential->>'backup_state')::boolean, false);
    v_name := p_credential->>'name';

    -- Convert transports array
    IF p_credential->'transports' IS NOT NULL THEN
        SELECT ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(p_credential->'transports') = 'array' THEN p_credential->'transports' ELSE '[]'::jsonb END))
        INTO v_transports;
    END IF;

    -- Check if user exists
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = v_user_id) THEN
        RETURN QUERY SELECT false, 'User not found'::text, NULL::integer;
        RETURN;
    END IF;

    -- Insert credential
    INSERT INTO user_passkey_credentials (
        user_id, credential_id, public_key, attestation_type, aaguid,
        sign_count, transports, backup_eligible, backup_state, name, created_at, last_used_at
    )
    VALUES (
        v_user_id, v_cred_id, v_public_key, v_attestation_type, v_aaguid,
        v_sign_count, v_transports, v_backup_eligible, v_backup_state, v_name, now(), now()
    )
    RETURNING id INTO v_credential_id;

    RETURN QUERY SELECT true, NULL::text, v_credential_id;
EXCEPTION
    WHEN unique_violation THEN
        RETURN QUERY SELECT false, 'Credential already exists'::text, NULL::integer;
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::integer;
END;
$$ LANGUAGE plpgsql;

-- 2. resolvespec_passkey_get_credential - Get credential by credential_id
-- Input: p_credential_id (bytea)
-- Output: p_success (bool), p_error (text), p_credential (jsonb)
CREATE OR REPLACE FUNCTION resolvespec_passkey_get_credential(p_credential_id bytea)
RETURNS TABLE(p_success boolean, p_error text, p_credential jsonb) AS $$
DECLARE
    v_credential jsonb;
BEGIN
    SELECT jsonb_build_object(
        'id', id,
        'user_id', user_id,
        'credential_id', encode(credential_id, 'base64'),
        'public_key', encode(public_key, 'base64'),
        'attestation_type', attestation_type,
        'aaguid', encode(COALESCE(aaguid, ''::bytea), 'base64'),
        'sign_count', sign_count,
        'clone_warning', clone_warning,
        'transports', COALESCE(to_jsonb(transports), '[]'::jsonb),
        'backup_eligible', backup_eligible,
        'backup_state', backup_state,
        'name', name,
        'created_at', created_at,
        'last_used_at', last_used_at
    )
    INTO v_credential
    FROM user_passkey_credentials
    WHERE credential_id = p_credential_id;

    IF v_credential IS NULL THEN
        RETURN QUERY SELECT false, 'Credential not found'::text, NULL::jsonb;
    ELSE
        RETURN QUERY SELECT true, NULL::text, v_credential;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 3. resolvespec_passkey_get_user_credentials - Get all credentials for a user
-- Input: p_user_id (integer)
-- Output: p_success (bool), p_error (text), p_credentials (jsonb array)
CREATE OR REPLACE FUNCTION resolvespec_passkey_get_user_credentials(p_user_id integer)
RETURNS TABLE(p_success boolean, p_error text, p_credentials jsonb) AS $$
DECLARE
    v_credentials jsonb;
BEGIN
    SELECT COALESCE(jsonb_agg(
        jsonb_build_object(
            'id', id,
            'user_id', user_id,
            'credential_id', encode(credential_id, 'base64'),
            'public_key', encode(public_key, 'base64'),
            'attestation_type', attestation_type,
            'aaguid', encode(COALESCE(aaguid, ''::bytea), 'base64'),
            'sign_count', sign_count,
            'clone_warning', clone_warning,
            'transports', COALESCE(to_jsonb(transports), '[]'::jsonb),
            'backup_eligible', backup_eligible,
            'backup_state', backup_state,
            'name', name,
            'created_at', created_at,
            'last_used_at', last_used_at
        ) ORDER BY created_at DESC
    ), '[]'::jsonb)
    INTO v_credentials
    FROM user_passkey_credentials
    WHERE user_id = p_user_id;

    RETURN QUERY SELECT true, NULL::text, v_credentials;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, '[]'::jsonb;
END;
$$ LANGUAGE plpgsql;

-- 4. resolvespec_passkey_update_counter - Update sign counter and check for cloning
-- Input: p_credential_id (bytea), p_new_counter (integer)
-- Output: p_success (bool), p_error (text), p_clone_warning (bool)
CREATE OR REPLACE FUNCTION resolvespec_passkey_update_counter(
    p_credential_id bytea,
    p_new_counter integer
)
RETURNS TABLE(p_success boolean, p_error text, p_clone_warning boolean) AS $$
DECLARE
    v_old_counter INTEGER;
    v_clone_warning BOOLEAN := false;
BEGIN
    -- Get current counter
    SELECT sign_count INTO v_old_counter
    FROM user_passkey_credentials
    WHERE credential_id = p_credential_id;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Credential not found'::text, false;
        RETURN;
    END IF;

    -- Check for cloning (counter should always increase)
    IF p_new_counter <= v_old_counter THEN
        v_clone_warning := true;
        
        -- Update clone warning flag
        UPDATE user_passkey_credentials
        SET clone_warning = true
        WHERE credential_id = p_credential_id;
    ELSE
        -- Normal counter update
        UPDATE user_passkey_credentials
        SET sign_count = p_new_counter,
            last_used_at = now()
        WHERE credential_id = p_credential_id;
    END IF;

    RETURN QUERY SELECT true, NULL::text, v_clone_warning;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, false;
END;
$$ LANGUAGE plpgsql;

-- 5. resolvespec_passkey_delete_credential - Delete a passkey credential
-- Input: p_user_id (integer), p_credential_id (bytea)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_passkey_delete_credential(
    p_user_id integer,
    p_credential_id bytea
)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_deleted INTEGER;
BEGIN
    DELETE FROM user_passkey_credentials
    WHERE user_id = p_user_id AND credential_id = p_credential_id;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;

    IF v_deleted = 0 THEN
        RETURN QUERY SELECT false, 'Credential not found'::text;
    ELSE
        RETURN QUERY SELECT true, NULL::text;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 6. resolvespec_passkey_update_name - Update credential friendly name
-- Input: p_user_id (integer), p_credential_id (bytea), p_name (text)
-- Output: p_success (bool), p_error (text)
CREATE OR REPLACE FUNCTION resolvespec_passkey_update_name(
    p_user_id integer,
    p_credential_id bytea,
    p_name text
)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_updated INTEGER;
BEGIN
    UPDATE user_passkey_credentials
    SET name = p_name
    WHERE user_id = p_user_id AND credential_id = p_credential_id;

    GET DIAGNOSTICS v_updated = ROW_COUNT;

    IF v_updated = 0 THEN
        RETURN QUERY SELECT false, 'Credential not found'::text;
    ELSE
        RETURN QUERY SELECT true, NULL::text;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 7. resolvespec_passkey_get_credentials_by_username - Get credentials for passkey authentication
-- Input: p_username (text)
-- Output: p_success (bool), p_error (text), p_user_id (int), p_credentials (jsonb array)
CREATE OR REPLACE FUNCTION resolvespec_passkey_get_credentials_by_username(p_username text)
RETURNS TABLE(p_success boolean, p_error text, p_user_id integer, p_credentials jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_credentials jsonb;
BEGIN
    -- Get user ID
    SELECT id INTO v_user_id
    FROM users
    WHERE username = p_username AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text, NULL::integer, NULL::jsonb;
        RETURN;
    END IF;

    -- Get user's credentials
    SELECT COALESCE(jsonb_agg(
        jsonb_build_object(
            'id', id,
            'credential_id', encode(credential_id, 'base64'),
            'transports', COALESCE(to_jsonb(transports), '[]'::jsonb)
        )
    ), '[]'::jsonb)
    INTO v_credentials
    FROM user_passkey_credentials
    WHERE user_id = v_user_id;

    RETURN QUERY SELECT true, NULL::text, v_user_id, v_credentials;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::integer, NULL::jsonb;
END;
$$ LANGUAGE plpgsql;

-- 8. resolvespec_passkey_login - Creates a session for a user whose passkey assertion was verified
-- Input: p_request (jsonb) {user_id: int, ip_address: string, user_agent: string}
-- Output: p_success (bool), p_error (text), p_data (LoginResponse as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_passkey_login(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id INTEGER;
    v_username TEXT;
    v_email TEXT;
    v_user_level INTEGER;
    v_roles TEXT;
    v_program_user_id INTEGER;
    v_program_user_table TEXT;
    v_session_token TEXT;
BEGIN
    v_user_id := (p_request->>'user_id')::integer;

    SELECT username, email, user_level, roles, program_user_id, program_user_table
    INTO v_username, v_email, v_user_level, v_roles, v_program_user_id, v_program_user_table
    FROM users
    WHERE id = v_user_id AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'User not found'::text, NULL::jsonb;
        RETURN;
    END IF;

    v_session_token := 'sess_' || encode(gen_random_bytes(32), 'hex') || '_' || extract(epoch from now())::bigint::text;

    INSERT INTO user_sessions (session_token, user_id, expires_at, ip_address, user_agent, last_activity_at)
    VALUES (v_session_token, v_user_id, now() + interval '24 hours',
            p_request->>'ip_address', p_request->>'user_agent', now());

    UPDATE users SET last_login_at = now() WHERE id = v_user_id;

    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'token', v_session_token,
            'user', jsonb_build_object(
                'user_id', v_user_id,
                'user_name', v_username,
                'email', v_email,
                'user_level', v_user_level,
                'roles', string_to_array(COALESCE(v_roles, ''), ','),
                'session_id', v_session_token,
                'program_user_id', COALESCE(v_program_user_id, 0),
                'program_user_table', COALESCE(v_program_user_table, '')
            ),
            'expires_in', 86400
        );
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::jsonb;
END;
$$ LANGUAGE plpgsql;

-- ============================================
-- Example: Test Passkey stored procedures
-- ============================================

-- Store credential
-- SELECT * FROM resolvespec_passkey_store_credential('{"user_id": 1, "credential_id": "YWJjZGVmMTIzNDU2", "public_key": "MIIBIjAN...", "attestation_type": "none", "sign_count": 0, "transports": ["internal"], "backup_eligible": true, "backup_state": false, "name": "My Phone"}'::jsonb);

-- Get credential
-- SELECT * FROM resolvespec_passkey_get_credential(decode('YWJjZGVmMTIzNDU2', 'base64'));

-- Get user credentials
-- SELECT * FROM resolvespec_passkey_get_user_credentials(1);

-- Update counter
-- SELECT * FROM resolvespec_passkey_update_counter(decode('YWJjZGVmMTIzNDU2', 'base64'), 1);

-- Delete credential
-- SELECT * FROM resolvespec_passkey_delete_credential(1, decode('YWJjZGVmMTIzNDU2', 'base64'));

-- Update name
-- SELECT * FROM resolvespec_passkey_update_name(1, decode('YWJjZGVmMTIzNDU2', 'base64'), 'New Name');

-- Get credentials by username
-- SELECT * FROM resolvespec_passkey_get_credentials_by_username('admin');

-- ============================================
-- Password Reset Tables
-- ============================================

-- Password reset tokens table
CREATE TABLE IF NOT EXISTS user_password_resets (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE, -- SHA-256 hex of the raw token
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    used BOOLEAN DEFAULT false,
    used_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pw_reset_token_hash ON user_password_resets(token_hash);
CREATE INDEX IF NOT EXISTS idx_pw_reset_user_id ON user_password_resets(user_id);
CREATE INDEX IF NOT EXISTS idx_pw_reset_expires_at ON user_password_resets(expires_at);

-- ============================================
-- Stored Procedures for Password Reset
-- ============================================

-- 1. resolvespec_password_reset_request - Creates a password reset token for a user
-- Input: p_request jsonb {email: string, username: string}
-- Output: p_success (bool), p_error (text), p_data jsonb {token: string, expires_in: int}
-- NOTE: The raw token is returned so the caller can deliver it out-of-band (e.g. email).
--       Only the SHA-256 hash is stored. Invalidates any previous unused tokens for the user.
CREATE OR REPLACE FUNCTION resolvespec_password_reset_request(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_user_id   INTEGER;
    v_email     TEXT;
    v_username  TEXT;
    v_raw_token TEXT;
    v_token_hash TEXT;
    v_expires_at TIMESTAMP;
BEGIN
    v_email    := p_request->>'email';
    v_username := p_request->>'username';

    -- Require at least one identifier
    IF (v_email IS NULL OR v_email = '') AND (v_username IS NULL OR v_username = '') THEN
        RETURN QUERY SELECT false, 'email or username is required'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- Look up active user
    IF v_email IS NOT NULL AND v_email <> '' THEN
        SELECT id INTO v_user_id FROM users WHERE email = v_email AND is_active = true;
    ELSE
        SELECT id INTO v_user_id FROM users WHERE username = v_username AND is_active = true;
    END IF;

    -- Return generic success even when user not found to avoid user enumeration
    IF NOT FOUND THEN
        RETURN QUERY SELECT true, NULL::text, jsonb_build_object('token', '', 'expires_in', 0);
        RETURN;
    END IF;

    -- Invalidate previous unused tokens for this user
    DELETE FROM user_password_resets WHERE user_id = v_user_id AND used = false;

    -- Generate a random 32-byte token and store its SHA-256 hash
    v_raw_token  := encode(gen_random_bytes(32), 'hex');
    v_token_hash := encode(digest(v_raw_token, 'sha256'), 'hex');
    v_expires_at := now() + interval '1 hour';

    INSERT INTO user_password_resets (user_id, token_hash, expires_at)
    VALUES (v_user_id, v_token_hash, v_expires_at);

    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'token',      v_raw_token,
            'expires_in', 3600
        );
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, NULL::jsonb;
END;
$$ LANGUAGE plpgsql;

-- 2. resolvespec_password_reset - Validates the token and updates the user's password
-- Input: p_request jsonb {token: string, new_password: string}
-- Output: p_success (bool), p_error (text)
-- NOTE: The new password is hashed with bcrypt (pgcrypto crypt/gen_salt) before storing.
CREATE OR REPLACE FUNCTION resolvespec_password_reset(p_request jsonb)
RETURNS TABLE(p_success boolean, p_error text) AS $$
DECLARE
    v_raw_token  TEXT;
    v_token_hash TEXT;
    v_new_pw     TEXT;
    v_reset_id   INTEGER;
    v_user_id    INTEGER;
    v_expires_at TIMESTAMP;
BEGIN
    v_raw_token := p_request->>'token';
    v_new_pw    := p_request->>'new_password';

    IF v_raw_token IS NULL OR v_raw_token = '' THEN
        RETURN QUERY SELECT false, 'token is required'::text;
        RETURN;
    END IF;

    IF v_new_pw IS NULL OR v_new_pw = '' THEN
        RETURN QUERY SELECT false, 'new_password is required'::text;
        RETURN;
    END IF;

    v_token_hash := encode(digest(v_raw_token, 'sha256'), 'hex');

    -- Find valid, unused reset token
    SELECT id, user_id, expires_at
    INTO v_reset_id, v_user_id, v_expires_at
    FROM user_password_resets
    WHERE token_hash = v_token_hash AND used = false;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'invalid or expired token'::text;
        RETURN;
    END IF;

    IF v_expires_at <= now() THEN
        RETURN QUERY SELECT false, 'invalid or expired token'::text;
        RETURN;
    END IF;

    IF octet_length(v_new_pw) > 72 THEN
        RETURN QUERY SELECT false, 'new_password must be at most 72 bytes'::text;
        RETURN;
    END IF;
    v_new_pw := crypt(v_new_pw, gen_salt('bf'));

    -- Update password and invalidate all sessions
    UPDATE users SET password = v_new_pw, updated_at = now() WHERE id = v_user_id;
    DELETE FROM user_sessions WHERE user_id = v_user_id;

    -- Mark token as used
    UPDATE user_password_resets SET used = true, used_at = now() WHERE id = v_reset_id;

    RETURN QUERY SELECT true, NULL::text;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text;
END;
$$ LANGUAGE plpgsql;

-- Example: Test password reset stored procedures
-- SELECT * FROM resolvespec_password_reset_request('{"email": "user@example.com"}'::jsonb);
-- SELECT * FROM resolvespec_password_reset('{"token": "<raw_token>", "new_password": "newpass123"}'::jsonb);

-- ============================================
-- OAuth2 Server Tables (OAuthServer persistence)
-- ============================================

-- oauth_clients: persistent RFC 7591 registered clients
CREATE TABLE IF NOT EXISTS oauth_clients (
    id SERIAL PRIMARY KEY,
    client_id VARCHAR(255) NOT NULL UNIQUE,
    redirect_uris TEXT[] NOT NULL,
    client_name VARCHAR(255),
    grant_types TEXT[] DEFAULT ARRAY['authorization_code'],
    allowed_scopes TEXT[] DEFAULT ARRAY['openid','profile','email'],
    client_secret_hash TEXT,       -- sha256 hex of the confidential-client secret; NULL for public clients
    token_endpoint_auth_method VARCHAR(30) DEFAULT 'none',
    is_active BOOLEAN DEFAULT true,
    metadata jsonb,                -- every other RFC 7591 field (see sectypes.OAuthServerClient)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS metadata jsonb;

-- oauth_codes: short-lived authorization codes (for multi-instance deployments)
-- Note: client_id is stored without a foreign key so codes can be persisted even
-- when OAuth clients are managed in memory rather than persisted in oauth_clients.
CREATE TABLE IF NOT EXISTS oauth_codes (
    id SERIAL PRIMARY KEY,
    code VARCHAR(255) NOT NULL UNIQUE,
    client_id VARCHAR(255) NOT NULL,
    redirect_uri TEXT NOT NULL,
    client_state TEXT,
    code_challenge VARCHAR(255) NOT NULL,
    code_challenge_method VARCHAR(10) DEFAULT 'S256',
    session_token TEXT NOT NULL,
    refresh_token TEXT,
    scopes TEXT[],
    expires_at TIMESTAMP NOT NULL,
    extra jsonb,                   -- nonce, auth_time, acr, claims, user_id, dpop_jkt ... (see sectypes.OAuthCode)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE oauth_codes ADD COLUMN IF NOT EXISTS extra jsonb;
CREATE INDEX IF NOT EXISTS idx_oauth_codes_code ON oauth_codes(code);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_codes(expires_at);

-- ============================================
-- OAuth2 Server Stored Procedures
-- ============================================

CREATE OR REPLACE FUNCTION resolvespec_oauth_register_client(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_client_id   text;
    v_row         jsonb;
BEGIN
    v_client_id := p_request->>'client_id';

    INSERT INTO oauth_clients (client_id, redirect_uris, client_name, grant_types, allowed_scopes, client_secret_hash, token_endpoint_auth_method, metadata)
    VALUES (
        v_client_id,
        ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(p_request->'redirect_uris') = 'array' THEN p_request->'redirect_uris' ELSE '[]'::jsonb END)),
        p_request->>'client_name',
        CASE WHEN jsonb_typeof(p_request->'grant_types') = 'array' AND jsonb_array_length(p_request->'grant_types') > 0 THEN ARRAY(SELECT jsonb_array_elements_text(p_request->'grant_types')) ELSE ARRAY['authorization_code'] END,
        CASE WHEN jsonb_typeof(p_request->'allowed_scopes') = 'array' AND jsonb_array_length(p_request->'allowed_scopes') > 0 THEN ARRAY(SELECT jsonb_array_elements_text(p_request->'allowed_scopes')) ELSE ARRAY['openid','profile','email'] END,
        NULLIF(p_request->>'client_secret_hash', ''),
        COALESCE(NULLIF(p_request->>'token_endpoint_auth_method', ''), 'none'),
        NULLIF(p_request - ARRAY['client_id','redirect_uris','client_name','grant_types','allowed_scopes','client_secret_hash','token_endpoint_auth_method'], '{}'::jsonb)
    )
    RETURNING (to_jsonb(oauth_clients.*) - 'metadata') || COALESCE(metadata, '{}'::jsonb) INTO v_row;

    RETURN QUERY SELECT true, null::text, v_row;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_get_client(p_client_id text)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    SELECT (to_jsonb(oauth_clients.*) - 'metadata') || COALESCE(metadata, '{}'::jsonb)
    INTO v_row
    FROM oauth_clients
    WHERE client_id = p_client_id AND is_active = true;

    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'client not found'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_save_code(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text)
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO oauth_codes (code, client_id, redirect_uri, client_state, code_challenge, code_challenge_method, session_token, refresh_token, scopes, expires_at, extra)
    VALUES (
        p_request->>'code',
        p_request->>'client_id',
        p_request->>'redirect_uri',
        p_request->>'client_state',
        p_request->>'code_challenge',
        COALESCE(p_request->>'code_challenge_method', 'S256'),
        p_request->>'session_token',
        p_request->>'refresh_token',
        ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(p_request->'scopes') = 'array' THEN p_request->'scopes' ELSE '[]'::jsonb END)),
        (p_request->>'expires_at')::timestamptz::timestamp,
        NULLIF(p_request - ARRAY['code','client_id','redirect_uri','client_state','code_challenge','code_challenge_method','session_token','refresh_token','scopes','expires_at'], '{}'::jsonb)
    );

    RETURN QUERY SELECT true, null::text;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_exchange_code(p_code text)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    DELETE FROM oauth_codes
    WHERE code = p_code AND expires_at > now()
    RETURNING jsonb_build_object(
        'client_id',             client_id,
        'redirect_uri',          redirect_uri,
        'client_state',          client_state,
        'code_challenge',        code_challenge,
        'code_challenge_method', code_challenge_method,
        'session_token',         session_token,
        'refresh_token',         refresh_token,
        'scopes',                to_jsonb(scopes)
    ) || COALESCE(extra, '{}'::jsonb) INTO v_row;

    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'invalid or expired code'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_introspect(p_token text)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    SELECT jsonb_build_object(
        'active',     true,
        'sub',        u.id::text,
        'username',   u.username,
        'email',      u.email,
        'user_level', u.user_level,
        -- NULLIF converts empty string to NULL; string_to_array(NULL) returns NULL;
        -- to_jsonb(NULL) returns NULL; COALESCE then returns '[]' for NULL/empty roles.
        'roles',      COALESCE(to_jsonb(string_to_array(NULLIF(u.roles, ''), ',')), '[]'::jsonb),
        'exp',        EXTRACT(EPOCH FROM s.expires_at)::bigint,
        'iat',        EXTRACT(EPOCH FROM s.created_at)::bigint
    )
    INTO v_row
    FROM user_sessions s
    JOIN users u ON u.id = s.user_id
    WHERE s.session_token = p_token
      AND s.expires_at > now()
      AND u.is_active = true;

    IF v_row IS NULL THEN
        RETURN QUERY SELECT true, null::text, '{"active":false}'::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_revoke(p_token text)
RETURNS TABLE(p_success bool, p_error text)
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM user_sessions WHERE session_token = p_token;
    RETURN QUERY SELECT true, null::text;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_update_client(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text)
LANGUAGE plpgsql AS $$
DECLARE
    v_rows int;
BEGIN
    UPDATE oauth_clients SET
        redirect_uris = ARRAY(SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(p_request->'redirect_uris') = 'array' THEN p_request->'redirect_uris' ELSE '[]'::jsonb END)),
        client_name = p_request->>'client_name',
        grant_types = CASE WHEN jsonb_typeof(p_request->'grant_types') = 'array' AND jsonb_array_length(p_request->'grant_types') > 0 THEN ARRAY(SELECT jsonb_array_elements_text(p_request->'grant_types')) ELSE grant_types END,
        allowed_scopes = CASE WHEN jsonb_typeof(p_request->'allowed_scopes') = 'array' AND jsonb_array_length(p_request->'allowed_scopes') > 0 THEN ARRAY(SELECT jsonb_array_elements_text(p_request->'allowed_scopes')) ELSE allowed_scopes END,
        client_secret_hash = NULLIF(p_request->>'client_secret_hash', ''),
        token_endpoint_auth_method = COALESCE(NULLIF(p_request->>'token_endpoint_auth_method', ''), token_endpoint_auth_method),
        metadata = NULLIF(p_request - ARRAY['client_id','redirect_uris','client_name','grant_types','allowed_scopes','client_secret_hash','token_endpoint_auth_method'], '{}'::jsonb)
    WHERE client_id = p_request->>'client_id' AND is_active = true;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    IF v_rows = 0 THEN
        RETURN QUERY SELECT false, 'client not found'::text;
    ELSE
        RETURN QUERY SELECT true, null::text;
    END IF;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_delete_client(p_client_id text)
RETURNS TABLE(p_success bool, p_error text)
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE oauth_clients SET is_active = false WHERE client_id = p_client_id;
    RETURN QUERY SELECT true, null::text;
END;
$$;

-- ============================================
-- OAuth2 Server grant state (consents, refresh tokens, device codes, PAR, replay cache)
-- ============================================
-- Procedure-backend tables use jsonb for scopes/extra/params. Every procedure takes one jsonb
-- request and returns (p_success, p_error, p_data). p_error carries a stable code for the
-- failures the Go side maps to errors: not_found, refresh_invalid, refresh_reused,
-- device_pending, device_slowdown, device_denied, device_expired.

CREATE TABLE IF NOT EXISTS oauth_consents (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    scopes jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_consents_user_client ON oauth_consents(user_id, client_id);

CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
    id SERIAL PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,   -- sha256 hex of the raw refresh token
    family_id VARCHAR(64) NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    user_id INTEGER NOT NULL,
    session_token VARCHAR(255),
    scopes jsonb,
    extra jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    used_at TIMESTAMP,
    revoked_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_family ON oauth_refresh_tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_session ON oauth_refresh_tokens(session_token);
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_expires ON oauth_refresh_tokens(expires_at);

CREATE TABLE IF NOT EXISTS oauth_device_codes (
    id SERIAL PRIMARY KEY,
    device_hash VARCHAR(64) NOT NULL UNIQUE,
    user_code VARCHAR(32) NOT NULL UNIQUE,
    client_id VARCHAR(255) NOT NULL,
    scopes jsonb,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    user_id INTEGER,
    session_token VARCHAR(255),
    poll_interval INTEGER NOT NULL DEFAULT 5,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    last_polled_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_oauth_device_expires ON oauth_device_codes(expires_at);

CREATE TABLE IF NOT EXISTS oauth_par_requests (
    id SERIAL PRIMARY KEY,
    request_uri VARCHAR(255) NOT NULL UNIQUE,
    client_id VARCHAR(255) NOT NULL,
    params jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_par_expires ON oauth_par_requests(expires_at);

CREATE TABLE IF NOT EXISTS oauth_jti (
    id SERIAL PRIMARY KEY,
    jti_key VARCHAR(255) NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_jti_expires ON oauth_jti(expires_at);

CREATE OR REPLACE FUNCTION resolvespec_oauth_save_consent(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM oauth_consents
    WHERE user_id = (p_request->>'user_id')::int AND client_id = p_request->>'client_id';
    INSERT INTO oauth_consents (user_id, client_id, scopes, expires_at)
    VALUES ((p_request->>'user_id')::int, p_request->>'client_id', COALESCE(p_request->'scopes', '[]'::jsonb),
            (p_request->>'expires_at')::timestamptz::timestamp);
    RETURN QUERY SELECT true, null::text, null::jsonb;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_get_consent(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    SELECT jsonb_build_object('user_id', user_id, 'client_id', client_id, 'scopes', COALESCE(scopes, '[]'::jsonb), 'expires_at', expires_at)
    INTO v_row
    FROM oauth_consents
    WHERE user_id = (p_request->>'user_id')::int AND client_id = p_request->>'client_id' AND expires_at > now();
    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'not_found'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_revoke_consent(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM oauth_consents
    WHERE user_id = (p_request->>'user_id')::int AND client_id = p_request->>'client_id';
    RETURN QUERY SELECT true, null::text, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_save_refresh(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO oauth_refresh_tokens (token_hash, family_id, client_id, user_id, session_token, scopes, extra, expires_at)
    VALUES (p_request->>'token_hash', p_request->>'family_id', p_request->>'client_id', (p_request->>'user_id')::int,
            p_request->>'session_token', p_request->'scopes', p_request->'extra',
            (p_request->>'expires_at')::timestamptz::timestamp);
    RETURN QUERY SELECT true, null::text, null::jsonb;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_rotate_refresh(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    r oauth_refresh_tokens%ROWTYPE;
    v_next jsonb := p_request->'next';
    v_old jsonb;
BEGIN
    SELECT * INTO r FROM oauth_refresh_tokens WHERE token_hash = p_request->>'old_hash' FOR UPDATE;
    IF NOT FOUND OR r.revoked_at IS NOT NULL OR r.expires_at <= now() THEN
        RETURN QUERY SELECT false, 'refresh_invalid'::text, null::jsonb;
        RETURN;
    END IF;
    v_old := jsonb_build_object('token_hash', r.token_hash, 'family_id', r.family_id, 'client_id', r.client_id,
                                'user_id', r.user_id, 'session_token', r.session_token,
                                'scopes', COALESCE(r.scopes, '[]'::jsonb), 'extra', COALESCE(r.extra, '{}'::jsonb),
                                'expires_at', r.expires_at);
    IF r.used_at IS NOT NULL THEN
        -- A rotated token came back: revoke the whole family. Returning (not raising) keeps the revoke.
        UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE family_id = r.family_id AND revoked_at IS NULL;
        RETURN QUERY SELECT false, 'refresh_reused'::text, v_old;
        RETURN;
    END IF;
    UPDATE oauth_refresh_tokens SET used_at = now() WHERE id = r.id;
    INSERT INTO oauth_refresh_tokens (token_hash, family_id, client_id, user_id, session_token, scopes, extra, expires_at)
    VALUES (v_next->>'token_hash', r.family_id, r.client_id, r.user_id,
            COALESCE(NULLIF(v_next->>'session_token', ''), r.session_token),
            v_next->'scopes', v_next->'extra', (v_next->>'expires_at')::timestamptz::timestamp);
    RETURN QUERY SELECT true, null::text, v_old;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_peek_refresh(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    SELECT jsonb_build_object('token_hash', token_hash, 'family_id', family_id, 'client_id', client_id,
                              'user_id', user_id, 'session_token', session_token,
                              'scopes', COALESCE(scopes, '[]'::jsonb), 'extra', COALESCE(extra, '{}'::jsonb),
                              'expires_at', expires_at)
    INTO v_row
    FROM oauth_refresh_tokens
    WHERE token_hash = p_request->>'token_hash' AND revoked_at IS NULL AND expires_at > now();
    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'refresh_invalid'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_revoke_refresh_family(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE family_id = p_request->>'family_id' AND revoked_at IS NULL;
    RETURN QUERY SELECT true, null::text, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_revoke_refresh_session(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE session_token = p_request->>'session_token' AND revoked_at IS NULL;
    RETURN QUERY SELECT true, null::text, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_create_device(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO oauth_device_codes (device_hash, user_code, client_id, scopes, status, poll_interval, expires_at)
    VALUES (p_request->>'device_hash', upper(p_request->>'user_code'), p_request->>'client_id', p_request->'scopes',
            COALESCE(NULLIF(p_request->>'status', ''), 'pending'), COALESCE((p_request->>'interval')::int, 5),
            (p_request->>'expires_at')::timestamptz::timestamp);
    RETURN QUERY SELECT true, null::text, null::jsonb;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_device_by_user_code(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    SELECT jsonb_build_object('device_hash', device_hash, 'user_code', user_code, 'client_id', client_id,
                              'scopes', COALESCE(scopes, '[]'::jsonb), 'status', status, 'interval', poll_interval,
                              'expires_at', expires_at)
    INTO v_row
    FROM oauth_device_codes
    WHERE user_code = upper(p_request->>'user_code') AND status = 'pending' AND expires_at > now();
    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'not_found'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_device_decide(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_rows int;
    v_approve boolean := COALESCE((p_request->>'approve')::boolean, false);
BEGIN
    UPDATE oauth_device_codes
    SET status = CASE WHEN v_approve THEN 'approved' ELSE 'denied' END,
        user_id = CASE WHEN v_approve THEN (p_request->>'user_id')::int ELSE user_id END,
        session_token = CASE WHEN v_approve THEN p_request->>'session_token' ELSE session_token END
    WHERE user_code = upper(p_request->>'user_code') AND status = 'pending' AND expires_at > now();
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    IF v_rows = 0 THEN
        RETURN QUERY SELECT false, 'not_found'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, null::jsonb;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_device_poll(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    d oauth_device_codes%ROWTYPE;
    v_slow boolean;
BEGIN
    SELECT * INTO d FROM oauth_device_codes WHERE device_hash = p_request->>'device_hash' FOR UPDATE;
    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'device_expired'::text, null::jsonb;
        RETURN;
    END IF;
    IF d.expires_at <= now() THEN
        DELETE FROM oauth_device_codes WHERE id = d.id;
        RETURN QUERY SELECT false, 'device_expired'::text, null::jsonb;
        RETURN;
    END IF;
    v_slow := d.last_polled_at IS NOT NULL AND (now() - d.last_polled_at) < make_interval(secs => d.poll_interval);
    UPDATE oauth_device_codes SET last_polled_at = now() WHERE id = d.id;
    IF v_slow THEN
        RETURN QUERY SELECT false, 'device_slowdown'::text, null::jsonb;
    ELSIF d.status = 'denied' THEN
        DELETE FROM oauth_device_codes WHERE id = d.id;
        RETURN QUERY SELECT false, 'device_denied'::text, null::jsonb;
    ELSIF d.status = 'approved' THEN
        DELETE FROM oauth_device_codes WHERE id = d.id;
        RETURN QUERY SELECT true, null::text, jsonb_build_object('device_hash', d.device_hash, 'user_code', d.user_code,
            'client_id', d.client_id, 'scopes', COALESCE(d.scopes, '[]'::jsonb), 'status', d.status,
            'user_id', d.user_id, 'session_token', d.session_token, 'interval', d.poll_interval, 'expires_at', d.expires_at);
    ELSE
        RETURN QUERY SELECT false, 'device_pending'::text, null::jsonb;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_save_par(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO oauth_par_requests (request_uri, client_id, params, expires_at)
    VALUES (p_request->>'request_uri', p_request->>'client_id', p_request->'params',
            (p_request->>'expires_at')::timestamptz::timestamp);
    RETURN QUERY SELECT true, null::text, null::jsonb;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, null::jsonb;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_consume_par(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_row jsonb;
BEGIN
    DELETE FROM oauth_par_requests
    WHERE request_uri = p_request->>'request_uri' AND expires_at > now()
    RETURNING jsonb_build_object('request_uri', request_uri, 'client_id', client_id,
                                 'params', COALESCE(params, '{}'::jsonb), 'expires_at', expires_at)
    INTO v_row;
    IF v_row IS NULL THEN
        RETURN QUERY SELECT false, 'not_found'::text, null::jsonb;
    ELSE
        RETURN QUERY SELECT true, null::text, v_row;
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION resolvespec_oauth_seen_jti(p_request jsonb)
RETURNS TABLE(p_success bool, p_error text, p_data jsonb)
LANGUAGE plpgsql AS $$
DECLARE
    v_rows int;
BEGIN
    DELETE FROM oauth_jti WHERE expires_at < now();
    INSERT INTO oauth_jti (jti_key, expires_at)
    VALUES (p_request->>'key', (p_request->>'expires_at')::timestamptz::timestamp)
    ON CONFLICT (jti_key) DO NOTHING;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN QUERY SELECT true, null::text, jsonb_build_object('seen', v_rows = 0);
END;
$$;
