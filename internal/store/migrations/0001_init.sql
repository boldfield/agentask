-- Create project table
CREATE TABLE IF NOT EXISTS project (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  repo TEXT NOT NULL,
  created_at TEXT NOT NULL
);

-- Create document table
CREATE TABLE IF NOT EXISTS document (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('design', 'feature_spec')),
  title TEXT NOT NULL,
  ref TEXT NOT NULL,
  "commit" TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES project(id)
);

-- Create task table
CREATE TABLE IF NOT EXISTS task (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  document_id TEXT NOT NULL,
  title TEXT NOT NULL,
  spec TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('backlog', 'ready', 'in_progress', 'review', 'done', 'blocked', 'failed')),
  assignee TEXT,
  lease_expires_at TEXT,
  result TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES project(id),
  FOREIGN KEY (document_id) REFERENCES document(id)
);

-- Create task_dep table (DAG edges)
CREATE TABLE IF NOT EXISTS task_dep (
  task_id TEXT NOT NULL,
  depends_on_id TEXT NOT NULL,
  PRIMARY KEY (task_id, depends_on_id),
  FOREIGN KEY (task_id) REFERENCES task(id),
  FOREIGN KEY (depends_on_id) REFERENCES task(id)
);

-- Create task_link table
CREATE TABLE IF NOT EXISTS task_link (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('pr', 'branch', 'commit', 'ci')),
  value TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES task(id)
);

-- Create index on task_link(kind, value) for reverse lookup
CREATE INDEX IF NOT EXISTS idx_task_link_kind_value ON task_link(kind, value);

-- Create index on task(project_id, state) for board queries
CREATE INDEX IF NOT EXISTS idx_task_project_state ON task(project_id, state);

-- Create event table (append-only audit/spine)
CREATE TABLE IF NOT EXISTS event (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  kind TEXT NOT NULL,
  verdict TEXT,
  note TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES task(id)
);

-- Create schema_migrations table to track applied migrations
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);
