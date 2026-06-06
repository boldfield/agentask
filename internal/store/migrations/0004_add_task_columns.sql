-- Add task model assignment and review task columns

-- Add model column for model-matched claiming (defaults to haiku for backward compatibility)
ALTER TABLE task ADD COLUMN model TEXT NOT NULL DEFAULT 'haiku';

-- Add kind discriminator for distinguish implement vs review tasks
ALTER TABLE task ADD COLUMN kind TEXT NOT NULL DEFAULT 'implement';

-- Add review_models column (JSON list of required reviewer models, implement tasks only)
ALTER TABLE task ADD COLUMN review_models TEXT;

-- Add review_round counter (tracks review cycle for implement/review tasks)
ALTER TABLE task ADD COLUMN review_round INTEGER NOT NULL DEFAULT 0;

-- Add target_task_id for review tasks to reference their parent implement task
ALTER TABLE task ADD COLUMN target_task_id TEXT REFERENCES task(id);

-- Add verdict column for review tasks (stores approve/reject)
ALTER TABLE task ADD COLUMN verdict TEXT;

-- Create index for model-matched claimable lookups: project + state + model
CREATE INDEX idx_task_claimable ON task(project_id, state, model);
