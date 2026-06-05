-- Enforce at most one design document per project
-- This is a partial unique index that only constrains rows where kind='design'
CREATE UNIQUE INDEX IF NOT EXISTS idx_document_one_design_per_project ON document(project_id) WHERE kind = 'design';
