-- Add agent_merge flag for per-task opt-in auto-merge behavior

ALTER TABLE task ADD COLUMN agent_merge INTEGER NOT NULL DEFAULT 0;
