-- Rebuild task table to widen state CHECK constraint to include 'approved'
-- SQLite cannot alter CHECK constraints in place, so we rebuild the table.

-- Create new task table with widened state CHECK
CREATE TABLE task_new (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  title TEXT NOT NULL,
  spec TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('backlog', 'ready', 'in_progress', 'review', 'approved', 'done', 'blocked', 'failed')),
  assignee TEXT,
  lease_expires_at TEXT,
  result TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES project(id),
  FOREIGN KEY (document_id) REFERENCES document(id)
);

-- Copy all existing rows to the new table
INSERT INTO task_new
SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
FROM task;

-- Drop the old indexes
DROP INDEX IF EXISTS idx_task_project_state;

-- Drop the old table
DROP TABLE task;

-- Rename the new table to task
ALTER TABLE task_new RENAME TO task;

-- Recreate the index on task(project_id, state) for board queries
CREATE INDEX idx_task_project_state ON task(project_id, state);
