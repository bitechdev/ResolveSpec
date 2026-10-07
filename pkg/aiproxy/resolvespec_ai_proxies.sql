-- aiproxy upstream definitions (PostgreSQL), same contract as the resolvespec security procedures.
-- ProcStore calls: SELECT p_success, p_error, p_data FROM resolvespec_ai_proxies()
-- Replace the function body to source the definitions from anywhere; keep the signature.
--
-- p_data: JSON array, one object per upstream
--   name, kind ('openai'|'mcp'), base_url                              required
--   api_key, auth_header, auth_format, headers (object)                optional
--   allowed_roles (array), allowed (array: models for openai, tools for mcp)
--   rate_per_second, rate_burst, timeout_seconds, enabled (default true)

CREATE TABLE IF NOT EXISTS ai_proxies (
    name            text PRIMARY KEY,
    kind            text NOT NULL CHECK (kind IN ('openai', 'mcp')),
    base_url        text NOT NULL,
    api_key         text,
    auth_header     text,
    auth_format     text,
    headers         jsonb,
    allowed_roles   jsonb,
    allowed         jsonb,
    rate_per_second double precision,
    rate_burst      integer,
    timeout_seconds integer,
    enabled         boolean NOT NULL DEFAULT true
);

CREATE OR REPLACE FUNCTION resolvespec_ai_proxies()
RETURNS TABLE(p_success boolean, p_error text, p_data jsonb) AS $$
DECLARE
    v_data jsonb;
BEGIN
    SELECT COALESCE(jsonb_agg(to_jsonb(p) - 'enabled' ORDER BY p.name), '[]'::jsonb)
    INTO v_data
    FROM ai_proxies p
    WHERE p.enabled = true;

    RETURN QUERY SELECT true, NULL::text, v_data;
EXCEPTION
    WHEN OTHERS THEN
        RETURN QUERY SELECT false, SQLERRM::text, '[]'::jsonb;
END;
$$ LANGUAGE plpgsql;
