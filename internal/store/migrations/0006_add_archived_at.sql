-- Add soft archive support for tasks and projects

-- Add archived_at column to task table (nullable, allows archiving any task)
ALTER TABLE task ADD COLUMN archived_at TEXT;

-- Add archived_at column to project table (nullable, allows archiving any project)
ALTER TABLE project ADD COLUMN archived_at TEXT;

-- Create index for efficient filtering of archived rows in list queries
CREATE INDEX idx_task_archived_at ON task(archived_at);
CREATE INDEX idx_project_archived_at ON project(archived_at);
