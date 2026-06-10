-- Add superseded_by column for the superseded state feature.
-- The 'superseded' state is distinct from 'failed': it marks a task as retired in favor
-- of a fresh attempt. A task may enter 'superseded' only via the supersede operation
-- (which lands in MR3); for now, the state is exposed and the guard (requiring
-- superseded_by to be set) is enforced at the application layer (TransitionTask).
--
-- NOTE: SQLite does not allow altering CHECK constraints in place. The state CHECK
-- constraint in the task table still excludes 'superseded' at the database level.
-- However, the application layer (TransitionTask function) enforces the validation,
-- making this safe. A future migration (MR2 or later) will rebuild the table to
-- update the CHECK constraint if needed.

ALTER TABLE task ADD COLUMN superseded_by TEXT REFERENCES task(id);
