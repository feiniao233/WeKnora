ALTER TABLE messages DROP COLUMN execution_result;
DROP INDEX idx_knowledge_bases_tenant_category;
ALTER TABLE knowledge_bases DROP COLUMN category;
