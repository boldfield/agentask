-- Add held flag for operator HOLD feature

-- Add held column to task table (boolean, default false, NOT NULL)
-- This allows an operator to manually pin a task out of automated flow from any state
ALTER TABLE task ADD COLUMN held INTEGER NOT NULL DEFAULT 0;

-- Create index for efficient filtering in claimable queries
CREATE INDEX idx_task_held ON task(held);
