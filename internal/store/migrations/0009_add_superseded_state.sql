-- Add superseded state to the state CHECK constraint and superseded_by column.
-- The 'superseded' state is distinct from 'failed': it marks a task as retired in favor
-- of a fresh attempt. A task may enter 'superseded' only via the supersede operation
-- (which lands in MR3); for now, the state is exposed and the guard (requiring
-- superseded_by to be set) is enforced at the application layer (TransitionTask).
--
-- SQLite does not allow altering CHECK constraints in place, so we rebuild the table
-- to widen the state CHECK constraint to include 'superseded'.

-- Create new task table with widened state CHECK to include 'superseded'
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
  target_task_id TEXT,
  verdict TEXT,
  agent_merge INTEGER NOT NULL DEFAULT 0,
  held INTEGER NOT NULL DEFAULT 0,
  superseded_by TEXT REFERENCES task(id),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  archived_at TEXT,
  FOREIGN KEY (project_id) REFERENCES project(id),
  FOREIGN KEY (document_id) REFERENCES document(id)
);

-- Copy all existing rows to the new table
INSERT INTO task_new
(id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, verdict, agent_merge, held, superseded_by, created_at, updated_at, archived_at)
SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, verdict, agent_merge, held, NULL, created_at, updated_at, archived_at
FROM task;

-- Drop the old table (indexes are automatically dropped with the table)
DROP TABLE task;

-- Rename the new table to task
ALTER TABLE task_new RENAME TO task;

-- Recreate the index on task(project_id, state) for board queries
CREATE INDEX idx_task_project_state ON task(project_id, state);

-- Recreate the other indexes that were on the original table
CREATE INDEX idx_task_claimable ON task(project_id, state, model);
CREATE INDEX idx_task_held ON task(held);
CREATE INDEX idx_task_archived_at ON task(archived_at);
