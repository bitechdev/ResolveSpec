-- Database Schema for DatabaseAuthenticator
-- ============================================

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL, -- bcrypt hashed password
    user_level INTEGER DEFAULT 0,
    roles VARCHAR(500), -- Comma-separated roles: "admin,manager,user"
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP
);

-- User sessions table for DatabaseAuthenticator
CREATE TABLE IF NOT EXISTS user_sessions (
    id SERIAL PRIMARY KEY,
    session_token VARCHAR(500) NOT NULL UNIQUE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_activity_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    ip_address VARCHAR(45), -- IPv4 or IPv6
    user_agent TEXT
);

CREATE INDEX IF NOT EXISTS idx_session_token ON user_sessions(session_token);
CREATE INDEX IF NOT EXISTS idx_user_id ON user_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_expires_at ON user_sessions(expires_at);

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
    v_session_token TEXT;
    v_expires_at TIMESTAMP;
    v_ip_address TEXT;
    v_user_agent TEXT;
BEGIN
    -- Extract login request fields
    v_username := p_request->>'username';
    v_ip_address := p_request->'claims'->>'ip_address';
    v_user_agent := p_request->'claims'->>'user_agent';

    -- Validate user credentials
    SELECT id, username, email, password, user_level, roles
    INTO v_user_id, v_username, v_email, v_password_hash, v_user_level, v_roles
    FROM users
    WHERE username = v_username AND is_active = true;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
        RETURN;
    END IF;

    -- TODO: Verify password hash using pgcrypto extension
    -- Enable pgcrypto: CREATE EXTENSION IF NOT EXISTS pgcrypto;
    -- IF NOT (crypt(p_request->>'password', v_password_hash) = v_password_hash) THEN
    --     RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
    --     RETURN;
    -- END IF;

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
                'session_id', v_session_token
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
BEGIN
    -- Query session and user data
    SELECT
        s.user_id, u.username, u.email, u.user_level, u.roles, s.session_token
    INTO
        v_user_id, v_username, v_email, v_user_level, v_roles, v_session_id
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
            'roles', string_to_array(COALESCE(v_roles, ''), ',')
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
BEGIN
    -- Verify old session exists and is valid
    SELECT s.user_id, u.username, u.email, u.user_level, u.roles, s.ip_address, s.user_agent
    INTO v_user_id, v_username, v_email, v_user_level, v_roles, v_ip_address, v_user_agent
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
            'roles', string_to_array(COALESCE(v_roles, ''), ',')
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

    -- TODO: Verify password hash
    -- IF NOT (crypt(p_password, v_password) = v_password) THEN
    --     RETURN QUERY SELECT false, 'Invalid credentials'::text, NULL::jsonb;
    --     RETURN;
    -- END IF;

    -- Return user data for JWT token generation
    RETURN QUERY SELECT
        true,
        NULL::text,
        jsonb_build_object(
            'id', v_user_id,
            'username', v_username,
            'email', v_email,
            'password', v_password,
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

-- 8. resolvespec_column_security - Loads column security rules for user
-- Input: user_id (int), schema (text), table_name (text)
-- Output: p_success (bool), p_error (text), p_rules (array of security rules as jsonb)
CREATE OR REPLACE FUNCTION resolvespec_column_security(p_user_id integer, p_schema text, p_table_name text)
RETURNS TABLE(p_success boolean, p_error text, p_rules jsonb) AS $$
DECLARE
    v_rules jsonb;
BEGIN
    -- Query column security rules from core.secaccess
    SELECT jsonb_agg(
        jsonb_build_object(
            'control', control,
            'accesstype', accesstype,
            'jsonvalue', jsonvalue
        )
    )
    INTO v_rules
    FROM core.secaccess
    WHERE rid_hub IN (
        SELECT rid_hub_parent
        FROM core.hub_link
        WHERE rid_hub_child = p_user_id AND parent_hubtype = 'secgroup'
    )
    AND control ILIKE (p_schema || '.' || p_table_name || '%');

    IF v_rules IS NULL THEN
        v_rules := '[]'::jsonb;
    END IF;

    RETURN QUERY SELECT true, NULL::text, v_rules;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, '[]'::jsonb;
END;
$$ LANGUAGE plpgsql;

-- 9. resolvespec_row_security - Loads row security template for user (replaces core.api_sec_rowtemplate)
-- Input: schema (text), table_name (text), user_id (int)
-- Output: p_template (text), p_block (bool)
CREATE OR REPLACE FUNCTION resolvespec_row_security(p_schema text, p_table_name text, p_user_id integer)
RETURNS TABLE(p_template text, p_block boolean) AS $$
BEGIN
    -- Call the existing core function if it exists, or implement your own logic
    -- This is a placeholder that you should customize based on your core.api_sec_rowtemplate logic
    RETURN QUERY SELECT ''::text, false;

    -- Example implementation:
    -- RETURN QUERY SELECT template, has_block
    -- FROM core.row_security_config
    -- WHERE schema_name = p_schema AND table_name = p_table_name AND user_id = p_user_id;
END;
$$ LANGUAGE plpgsql;

-- 10. resolvespec_register - Registers a new user and creates session
-- Input: RegisterRequest as jsonb {username: string, password: string, email: string, user_level: int, roles: array, claims: object, meta: object}
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
BEGIN
    -- Extract registration request fields
    v_username := p_request->>'username';
    v_email := p_request->>'email';
    v_password := p_request->>'password';
    v_user_level := COALESCE((p_request->>'user_level')::integer, 0);
    v_ip_address := p_request->'claims'->>'ip_address';
    v_user_agent := p_request->'claims'->>'user_agent';
    
    -- Convert roles array from JSON to comma-separated string
    SELECT array_to_string(ARRAY(SELECT jsonb_array_elements_text(p_request->'roles')), ',')
    INTO v_roles;

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

    -- TODO: Hash password using pgcrypto extension
    -- Enable pgcrypto: CREATE EXTENSION IF NOT EXISTS pgcrypto;
    -- v_password := crypt(v_password, gen_salt('bf'));

    -- Create new user
    INSERT INTO users (username, email, password, user_level, roles, is_active, created_at, updated_at)
    VALUES (v_username, v_email, v_password, v_user_level, v_roles, true, now(), now())
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
                'session_id', v_session_token
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
