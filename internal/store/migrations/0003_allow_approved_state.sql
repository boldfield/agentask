-- Allow the 'approved' state in the task table's state CHECK constraint
-- This is a table rebuild because SQLite cannot alter CHECK constraints in place

-- Disable foreign key constraints temporarily for the table swap
PRAGMA foreign_keys = OFF;

-- Create the new task table with 'approved' added to the state CHECK constraint
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

-- Copy all existing rows from the old table to the new table
INSERT INTO task_new SELECT * FROM task;

-- Drop the old table
DROP TABLE task;

-- Rename the new table to task
ALTER TABLE task_new RENAME TO task;

-- Recreate the indexes that were on the original task table
CREATE INDEX idx_task_project_state ON task(project_id, state);

-- Re-enable foreign key constraints
PRAGMA foreign_keys = ON;
