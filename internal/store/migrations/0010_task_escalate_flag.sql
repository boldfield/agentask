-- Add escalate flag for per-task escalation opt-out

ALTER TABLE task ADD COLUMN escalate INTEGER NOT NULL DEFAULT 1;
