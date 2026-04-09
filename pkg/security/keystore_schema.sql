-- Keystore schema for per-user auth keys
-- Apply alongside database_schema.sql (requires the users table)

CREATE TABLE IF NOT EXISTS user_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_type     VARCHAR(50) NOT NULL,
    key_hash     VARCHAR(64) NOT NULL UNIQUE,  -- SHA-256 hex digest (64 chars)
    name         VARCHAR(255) NOT NULL DEFAULT '',
    scopes       TEXT,                          -- JSON array, e.g. '["read","write"]'
    meta         JSONB,
    expires_at   TIMESTAMP,
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    is_active    BOOLEAN DEFAULT true
);

CREATE INDEX IF NOT EXISTS idx_user_keys_user_id  ON user_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_user_keys_key_hash ON user_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_user_keys_key_type ON user_keys(key_type);

-- resolvespec_keystore_get_user_keys
-- Returns all active, non-expired keys for a user.
-- Pass empty p_key_type to return all key types.
CREATE OR REPLACE FUNCTION resolvespec_keystore_get_user_keys(
    p_user_id  INTEGER,
    p_key_type TEXT DEFAULT ''
)
RETURNS TABLE(p_success BOOLEAN, p_error TEXT, p_keys JSONB)
LANGUAGE plpgsql AS $$
DECLARE
    v_keys JSONB;
BEGIN
    SELECT COALESCE(
        jsonb_agg(
            jsonb_build_object(
                'id',           k.id,
                'user_id',      k.user_id,
                'key_type',     k.key_type,
                'name',         k.name,
                'scopes',       CASE WHEN k.scopes IS NOT NULL THEN k.scopes::jsonb ELSE '[]'::jsonb END,
                'meta',         COALESCE(k.meta, '{}'::jsonb),
                'expires_at',   k.expires_at,
                'created_at',   k.created_at,
                'last_used_at', k.last_used_at,
                'is_active',    k.is_active
            )
        ),
        '[]'::jsonb
    )
    INTO v_keys
    FROM user_keys k
    WHERE k.user_id = p_user_id
      AND k.is_active = true
      AND (k.expires_at IS NULL OR k.expires_at > NOW())
      AND (p_key_type = '' OR k.key_type = p_key_type);

    RETURN QUERY SELECT true, NULL::TEXT, v_keys;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, NULL::JSONB;
END;
$$;

-- resolvespec_keystore_create_key
-- Inserts a new key row. key_hash is provided by the caller (Go hashes the raw key).
-- Returns the created key record (without key_hash).
CREATE OR REPLACE FUNCTION resolvespec_keystore_create_key(
    p_request JSONB
)
RETURNS TABLE(p_success BOOLEAN, p_error TEXT, p_key JSONB)
LANGUAGE plpgsql AS $$
DECLARE
    v_id         BIGINT;
    v_created_at TIMESTAMP;
    v_key        JSONB;
BEGIN
    INSERT INTO user_keys (user_id, key_type, key_hash, name, scopes, meta, expires_at)
    VALUES (
        (p_request->>'user_id')::INTEGER,
        p_request->>'key_type',
        p_request->>'key_hash',
        COALESCE(p_request->>'name', ''),
        p_request->>'scopes',
        p_request->'meta',
        CASE WHEN p_request->>'expires_at' IS NOT NULL
             THEN (p_request->>'expires_at')::TIMESTAMP
             ELSE NULL
        END
    )
    RETURNING id, created_at INTO v_id, v_created_at;

    v_key := jsonb_build_object(
        'id',         v_id,
        'user_id',    (p_request->>'user_id')::INTEGER,
        'key_type',   p_request->>'key_type',
        'name',       COALESCE(p_request->>'name', ''),
        'scopes',     CASE WHEN p_request->>'scopes' IS NOT NULL
                           THEN (p_request->>'scopes')::jsonb
                           ELSE '[]'::jsonb END,
        'meta',       COALESCE(p_request->'meta', '{}'::jsonb),
        'expires_at', p_request->>'expires_at',
        'created_at', v_created_at,
        'is_active',  true
    );

    RETURN QUERY SELECT true, NULL::TEXT, v_key;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, NULL::JSONB;
END;
$$;

-- resolvespec_keystore_delete_key
-- Soft-deletes a key (is_active = false) after verifying ownership.
-- Returns p_key_hash so the caller can invalidate cache entries without a separate query.
CREATE OR REPLACE FUNCTION resolvespec_keystore_delete_key(
    p_user_id INTEGER,
    p_key_id  BIGINT
)
RETURNS TABLE(p_success BOOLEAN, p_error TEXT, p_key_hash TEXT)
LANGUAGE plpgsql AS $$
DECLARE
    v_hash TEXT;
BEGIN
    UPDATE user_keys
    SET is_active = false
    WHERE id = p_key_id AND user_id = p_user_id AND is_active = true
    RETURNING key_hash INTO v_hash;

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'key not found or already deleted'::TEXT, NULL::TEXT;
        RETURN;
    END IF;

    RETURN QUERY SELECT true, NULL::TEXT, v_hash;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, NULL::TEXT;
END;
$$;

-- resolvespec_keystore_validate_key
-- Looks up a key by its SHA-256 hash, checks active status and expiry,
-- updates last_used_at, and returns the key record.
-- p_key_type can be empty to accept any key type.
CREATE OR REPLACE FUNCTION resolvespec_keystore_validate_key(
    p_key_hash TEXT,
    p_key_type TEXT DEFAULT ''
)
RETURNS TABLE(p_success BOOLEAN, p_error TEXT, p_key JSONB)
LANGUAGE plpgsql AS $$
DECLARE
    v_key_rec user_keys%ROWTYPE;
    v_key     JSONB;
BEGIN
    SELECT * INTO v_key_rec
    FROM user_keys
    WHERE key_hash = p_key_hash
      AND is_active = true
      AND (expires_at IS NULL OR expires_at > NOW())
      AND (p_key_type = '' OR key_type = p_key_type);

    IF NOT FOUND THEN
        RETURN QUERY SELECT false, 'invalid or expired key'::TEXT, NULL::JSONB;
        RETURN;
    END IF;

    UPDATE user_keys SET last_used_at = NOW() WHERE id = v_key_rec.id;

    v_key := jsonb_build_object(
        'id',           v_key_rec.id,
        'user_id',      v_key_rec.user_id,
        'key_type',     v_key_rec.key_type,
        'name',         v_key_rec.name,
        'scopes',       CASE WHEN v_key_rec.scopes IS NOT NULL
                             THEN v_key_rec.scopes::jsonb
                             ELSE '[]'::jsonb END,
        'meta',         COALESCE(v_key_rec.meta, '{}'::jsonb),
        'expires_at',   v_key_rec.expires_at,
        'created_at',   v_key_rec.created_at,
        'last_used_at', NOW(),
        'is_active',    v_key_rec.is_active
    );

    RETURN QUERY SELECT true, NULL::TEXT, v_key;
EXCEPTION WHEN OTHERS THEN
    RETURN QUERY SELECT false, SQLERRM, NULL::JSONB;
END;
$$;
