-- Rebuild task table to widen state CHECK constraint to include 'superseded'
-- and add a nullable superseded_by TEXT column for tracking which task superseded this one.
-- SQLite cannot alter CHECK constraints in place, so we rebuild the table.

-- Create new task table with widened state CHECK and new superseded_by column
CREATE TABLE task_new (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  title TEXT NOT NULL,
  spec TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('backlog', 'ready', 'in_progress', 'review', 'approved', 'done', 'blocked', 'failed', 'superseded')),
  assignee TEXT,
  lease_expires_at TEXT,
  result TEXT,
  model TEXT NOT NULL DEFAULT 'haiku',
  kind TEXT NOT NULL DEFAULT 'implement',
  review_models TEXT,
  review_round INTEGER NOT NULL DEFAULT 0,
  target_task_id TEXT REFERENCES task(id),
  verdict TEXT,
  agent_merge INTEGER NOT NULL DEFAULT 0,
  held INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  archived_at TEXT,
  superseded_by TEXT,
  FOREIGN KEY (project_id) REFERENCES project(id),
  FOREIGN KEY (document_id) REFERENCES document(id)
);

-- Copy all existing rows to the new table
INSERT INTO task_new
SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, verdict, agent_merge, held, created_at, updated_at, archived_at, NULL
FROM task;

-- Drop the old table (indexes are automatically dropped with the table)
DROP TABLE task;

-- Rename the new table to task
ALTER TABLE task_new RENAME TO task;

-- Recreate the indexes on task
CREATE INDEX idx_task_project_state ON task(project_id, state);
CREATE INDEX idx_task_claimable ON task(project_id, state, model);
CREATE INDEX idx_task_archived_at ON task(archived_at);
CREATE INDEX idx_task_held ON task(held);
