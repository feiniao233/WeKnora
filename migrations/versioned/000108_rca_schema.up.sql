ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS category VARCHAR(64) NOT NULL DEFAULT 'general';

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_category
    ON knowledge_bases (tenant_id, category);

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS execution_result JSONB;
