ALTER TABLE knowledge_bases ADD COLUMN category TEXT NOT NULL DEFAULT 'general';
CREATE INDEX idx_knowledge_bases_tenant_category ON knowledge_bases (tenant_id, category);
ALTER TABLE messages ADD COLUMN execution_result TEXT;
