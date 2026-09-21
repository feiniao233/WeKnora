ALTER TABLE messages DROP COLUMN IF EXISTS execution_result;
DROP INDEX IF EXISTS idx_knowledge_bases_tenant_category;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS category;
