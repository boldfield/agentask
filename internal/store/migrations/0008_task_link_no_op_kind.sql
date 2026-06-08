-- Rebuild task_link table to widen the kind CHECK constraint to include 'no_op'.
-- The 'no_op' marker is used for review-verified no-op resolution: a worker whose
-- acceptance criteria are already satisfied on main (empty diff) submits with this
-- marker and no pr link. SQLite cannot alter a CHECK constraint in place, so we
-- rebuild the table. Foreign-key enforcement is disabled by the migration runner.

-- Create new task_link table with widened kind CHECK
CREATE TABLE task_link_new (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('pr', 'branch', 'commit', 'ci', 'no_op')),
  value TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES task(id)
);

-- Copy all existing rows to the new table
INSERT INTO task_link_new
SELECT id, task_id, kind, value
FROM task_link;

-- Drop the old table (indexes are automatically dropped with the table)
DROP TABLE task_link;

-- Rename the new table into place
ALTER TABLE task_link_new RENAME TO task_link;

-- Recreate the index on task_link(kind, value) for reverse lookup
CREATE INDEX idx_task_link_kind_value ON task_link(kind, value);
