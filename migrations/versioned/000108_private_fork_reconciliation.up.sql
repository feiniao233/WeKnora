-- Existing private-fork databases used 000091-000093 for different changes,
-- so upstream's migrations with those numbers are skipped during an upgrade.
ALTER TABLE mcp_tool_approvals ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE mcp_services ADD COLUMN IF NOT EXISTS usage_instructions TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS mcp_metadata (
    tenant_id BIGINT NOT NULL,
    service_id VARCHAR(36) NOT NULL REFERENCES mcp_services(id) ON DELETE CASCADE,
    principal VARCHAR(255) NOT NULL DEFAULT '',
    config_fingerprint VARCHAR(64) NOT NULL,
    tools JSONB NOT NULL,
    instructions TEXT NOT NULL DEFAULT '',
    server_name TEXT NOT NULL DEFAULT '',
    server_version TEXT NOT NULL DEFAULT '',
    server_description TEXT NOT NULL DEFAULT '',
    synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, service_id, principal)
);

CREATE TABLE IF NOT EXISTS browser_devices (
    scope_key VARCHAR(32) PRIMARY KEY,
    id VARCHAR(32) NOT NULL UNIQUE,
    tenant BIGINT NOT NULL,
    "user" VARCHAR(36) NOT NULL,
    label VARCHAR(100) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    previous_hash VARCHAR(64) NOT NULL DEFAULT '',
    previous_until TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    renew_after TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    owner VARCHAR(32) NOT NULL DEFAULT '',
    owner_url VARCHAR(500) NOT NULL DEFAULT '',
    lease_key VARCHAR(32) NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS browser_pairings (
    scope_key VARCHAR(32) PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    tenant BIGINT NOT NULL,
    "user" VARCHAR(36) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS browser_pairings_expiry ON browser_pairings(expires_at);
CREATE TABLE IF NOT EXISTS browser_task_interruptions (
    scope_key VARCHAR(32) NOT NULL,
    session VARCHAR(36) NOT NULL,
    PRIMARY KEY (scope_key, session)
);

ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS category VARCHAR(64) NOT NULL DEFAULT 'general';
UPDATE knowledge_bases SET category = 'general' WHERE category = '';
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_category ON knowledge_bases (tenant_id, category);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS usage JSONB;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS execution_result JSONB;
