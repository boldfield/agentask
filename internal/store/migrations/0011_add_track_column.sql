-- Add track column for task work type (build, review, design, coherence)

ALTER TABLE task ADD COLUMN track TEXT NOT NULL DEFAULT 'build';
