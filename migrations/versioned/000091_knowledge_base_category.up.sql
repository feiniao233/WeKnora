ALTER TABLE knowledge_bases ADD COLUMN IF NOT EXISTS category VARCHAR(64) NOT NULL DEFAULT 'general';

UPDATE knowledge_bases SET category = 'general' WHERE category = '';

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_category
    ON knowledge_bases (tenant_id, category);
