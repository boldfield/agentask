package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

// Store is the interface for database operations.
// Concrete implementations (sqliteStore) satisfy this interface.
type Store interface {
	Close() error
	Conn() *sql.DB
	AppendEvent(ctx context.Context, tx *sql.Tx, taskID, actor, kind string, verdict, note *string) (Event, error)
	ListEvents(ctx context.Context, taskID string) ([]Event, error)
	CreateProject(ctx context.Context, name, repo string) (Project, error)
	GetProject(ctx context.Context, id string) (Project, error)
	CreateDocument(ctx context.Context, projectID, kind, title, ref string, commit *string) (Document, error)
	ListDocuments(ctx context.Context, projectID string, kind *string) ([]Document, error)
	CreateTasks(ctx context.Context, projectID string, tasks []TaskInput) ([]Task, error)
	GetTask(ctx context.Context, id string) (TaskWithDepsAndLinks, error)
	ListTasks(ctx context.Context, projectID string, filter TaskListFilter) ([]Task, error)
	ClaimTask(ctx context.Context, taskID, agentID string, leaseTTL time.Duration) (Task, error)
	HeartbeatTask(ctx context.Context, taskID, agentID string, leaseTTL time.Duration) (Task, error)
	PromoteTask(ctx context.Context, taskID string) (Task, error)
	SubmitTask(ctx context.Context, taskID, agentID, result string, links []LinkInput) (TaskWithDepsAndLinks, error)
}

// sqliteStore wraps a SQLite database connection and provides migration functionality.
type sqliteStore struct {
	conn *sql.DB
}

// Open opens a database connection and applies all pending migrations.
// The dbPath should be a file path (e.g., "agentask.db") or "file::memory:?cache=shared"
// for an in-memory database.
// It configures WAL mode, foreign keys, and busy timeout via DSN pragmas.
func Open(dbPath string) (Store, error) {
	// Build the DSN with pragmas for WAL, foreign_keys, and busy_timeout.
	// This ensures every connection from the pool has these pragmas applied.
	dsn := buildDSN(dbPath)

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set MaxOpenConns to 1 for single-writer SQLite (as per DESIGN.md §7)
	conn.SetMaxOpenConns(1)

	store := &sqliteStore{conn: conn}

	// Apply migrations in a transaction
	if err := store.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return store, nil
}

// buildDSN constructs a SQLite DSN with pragmas.
func buildDSN(dbPath string) string {
	// If the path is already a DSN (contains scheme), use it directly but append pragmas.
	if strings.Contains(dbPath, "://") || strings.Contains(dbPath, ":memory:") {
		return dsn_addPragmas(dbPath)
	}

	// Otherwise, treat it as a file path.
	// Escape the path and add pragmas.
	escaped := url.QueryEscape(dbPath)
	dsn := "file:" + escaped
	return dsn_addPragmas(dsn)
}

// dsn_addPragmas appends pragma query parameters to a DSN.
func dsn_addPragmas(dsn string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
}

// migrate applies all pending migrations from the embedded migrations directory.
func (s *sqliteStore) migrate() error {
	tx, err := s.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create schema_migrations table if it doesn't exist
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Get list of all migration files
	migrations, err := listMigrations()
	if err != nil {
		return err
	}

	// Apply each migration that hasn't been applied yet
	for _, migration := range migrations {
		var applied int
		err := tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("failed to query schema_migrations: %w", err)
		}

		if applied > 0 {
			// Migration already applied, skip it
			continue
		}

		// Read the migration file
		data, err := fs.ReadFile(migrationsFS, migration.path)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", migration.path, err)
		}

		// Execute the migration (split by semicolon to handle multiple statements)
		statements := splitStatements(string(data))
		for _, stmt := range statements {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", migration.version, err)
			}
		}

		// Record that the migration was applied
		now := nowTimestamp()
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			migration.version, now); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", migration.version, err)
		}
	}

	return tx.Commit()
}

type migration struct {
	version string
	path    string
}

// listMigrations returns all migration files sorted by version.
func listMigrations() ([]migration, error) {
	var migrations []migration

	// Read all files in the migrations directory
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			// Extract version from filename (e.g., "0001_init.sql" -> "0001")
			version := entry.Name()[:len(entry.Name())-4] // Remove .sql extension
			migrations = append(migrations, migration{
				version: version,
				path:    filepath.Join("migrations", entry.Name()),
			})
		}
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	return migrations, nil
}

// Close closes the database connection.
func (s *sqliteStore) Close() error {
	return s.conn.Close()
}

// Conn returns the underlying database connection for direct access.
func (s *sqliteStore) Conn() *sql.DB {
	return s.conn
}

// AppendEvent inserts a new event into the event table within an existing transaction.
// It must be called within a transaction so that a state change and its event can be
// committed atomically.
func (s *sqliteStore) AppendEvent(ctx context.Context, tx *sql.Tx, taskID, actor, kind string, verdict, note *string) (Event, error) {
	eventID := GenerateID()
	now := nowTimestamp()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO event (id, task_id, actor, kind, verdict, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, eventID, taskID, actor, kind, verdict, note, now)
	if err != nil {
		return Event{}, fmt.Errorf("failed to append event: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Event{}, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected != 1 {
		return Event{}, fmt.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	return Event{
		ID:        eventID,
		TaskID:    taskID,
		Actor:     actor,
		Kind:      kind,
		Verdict:   verdict,
		Note:      note,
		CreatedAt: now,
	}, nil
}

// ListEvents retrieves all events for a given task, ordered by created_at and id.
func (s *sqliteStore) ListEvents(ctx context.Context, taskID string) ([]Event, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, task_id, actor, kind, verdict, note, created_at
		FROM event
		WHERE task_id = ?
		ORDER BY created_at, id
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		err := rows.Scan(&e.ID, &e.TaskID, &e.Actor, &e.Kind, &e.Verdict, &e.Note, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating events: %w", err)
	}

	return events, nil
}

// splitStatements splits SQL statements by semicolon, handling comments.
func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	lines := strings.Split(sql, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")

		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				// Remove trailing semicolon
				stmt = strings.TrimSuffix(stmt, ";")
				statements = append(statements, stmt)
			}
			current.Reset()
		}
	}

	// Add any remaining statement
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			stmt = strings.TrimSuffix(stmt, ";")
			statements = append(statements, stmt)
		}
	}

	return statements
}

// GenerateID generates a new unique ID using UUID v4.
// This is the reusable ID pattern that all tasks (T06+) will use.
func GenerateID() string {
	return uuid.NewString()
}

// timestampLayout is a fixed-width, nanosecond-precision RFC3339 layout. Unlike
// time.RFC3339Nano (which drops trailing-zero fractions and so yields variable-width
// strings that do not sort lexically), every timestamp here is the same width, so a
// lexical ORDER BY on the stored TEXT equals chronological order. All persisted
// timestamps use this single format to keep ordering consistent across tables — the
// event log (DESIGN §2/§5) depends on it for chronological audit ordering.
const timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// nowTimestamp returns the current UTC time formatted with timestampLayout.
func nowTimestamp() string {
	return time.Now().UTC().Format(timestampLayout)
}

// leaseExpiryTimestamp returns the time at the given future time (now + ttl) formatted with timestampLayout.
// This ensures the lease expires_at timestamp is in the same fixed-width format so string comparison
// in claimableSQL works correctly.
func leaseExpiryTimestamp(ttl time.Duration) string {
	return time.Now().UTC().Add(ttl).Format(timestampLayout)
}

// Domain structs mirroring the schema (DESIGN.md §2)

// Project represents a code project.
type Project struct {
	ID        string `db:"id"`
	Name      string `db:"name"`
	Repo      string `db:"repo"`
	CreatedAt string `db:"created_at"`
}

// Document represents a design or feature spec document.
type Document struct {
	ID        string `db:"id"`
	ProjectID string `db:"project_id"`
	Kind      string `db:"kind"` // 'design' or 'feature_spec'
	Title     string `db:"title"`
	Ref       string `db:"ref"`
	Commit    *string `db:"commit"` // nullable
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// Task represents a task on the board.
type Task struct {
	ID            string `db:"id"`
	ProjectID     string `db:"project_id"`
	DocumentID    string `db:"document_id"`
	Title         string `db:"title"`
	Spec          string `db:"spec"`
	State         string `db:"state"`
	Assignee      *string `db:"assignee"` // nullable
	LeaseExpiresAt *string `db:"lease_expires_at"` // nullable
	Result        *string `db:"result"` // nullable
	CreatedAt     string `db:"created_at"`
	UpdatedAt     string `db:"updated_at"`
}

// TaskLink represents a link from a task to external resources (PR, branch, commit, CI).
type TaskLink struct {
	ID     string `db:"id"`
	TaskID string `db:"task_id"`
	Kind   string `db:"kind"` // 'pr', 'branch', 'commit', or 'ci'
	Value  string `db:"value"`
}

// TaskInput is the input format for bulk task creation.
type TaskInput struct {
	Key        string   `json:"key"`        // optional client-provided key for intra-batch deps
	Title      string   `json:"title"`
	Spec       string   `json:"spec"`
	DocumentID string   `json:"document_id"`
	DependsOn  []string `json:"depends_on"` // refs to task ids or keys in the batch
}

// LinkInput is the input format for task links during submission.
type LinkInput struct {
	Kind  string `json:"kind"`  // 'pr', 'branch', 'commit', or 'ci'
	Value string `json:"value"`
}

// TaskWithDepsAndLinks combines a Task with its dependencies and links.
type TaskWithDepsAndLinks struct {
	ID            string      `json:"id"`
	ProjectID     string      `json:"project_id"`
	DocumentID    string      `json:"document_id"`
	Title         string      `json:"title"`
	Spec          string      `json:"spec"`
	State         string      `json:"state"`
	Assignee      *string     `json:"assignee"`
	LeaseExpiresAt *string     `json:"lease_expires_at"`
	Result        *string     `json:"result"`
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
	DependsOn     []string    `json:"depends_on"`
	Links         []TaskLink  `json:"links"`
}

// TaskListFilter contains filters for listing tasks.
type TaskListFilter struct {
	State     *string
	Assignee  *string
	Claimable bool
}

// Event represents an audit/event log entry.
type Event struct {
	ID        string `db:"id"`
	TaskID    string `db:"task_id"`
	Actor     string `db:"actor"`
	Kind      string `db:"kind"`
	Verdict   *string `db:"verdict"` // nullable
	Note      *string `db:"note"` // nullable
	CreatedAt string `db:"created_at"`
}

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a constraint is violated (e.g., second design per project).
var ErrConflict = errors.New("conflict")

// ValidationError is a client-input error. Handlers map it to HTTP 400 via errors.As,
// surfacing Code and Message. Use invalid() to construct one. This is the validation
// convention for all mutation endpoints — prefer it over bare fmt.Errorf so the
// 400-vs-500 distinction never depends on string-matching error messages.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func invalid(code, message string) error {
	return &ValidationError{Code: code, Message: message}
}

// CreateProject creates a new project with the given name and repo.
// It generates the id and sets created_at automatically.
func (s *sqliteStore) CreateProject(ctx context.Context, name, repo string) (Project, error) {
	id := GenerateID()
	createdAt := nowTimestamp()

	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO project (id, name, repo, created_at)
		VALUES (?, ?, ?, ?)
	`, id, name, repo, createdAt)
	if err != nil {
		return Project{}, fmt.Errorf("failed to create project: %w", err)
	}

	return Project{
		ID:        id,
		Name:      name,
		Repo:      repo,
		CreatedAt: createdAt,
	}, nil
}

// GetProject retrieves a project by id.
// Returns ErrNotFound if the project does not exist.
func (s *sqliteStore) GetProject(ctx context.Context, id string) (Project, error) {
	var p Project
	err := s.conn.QueryRowContext(ctx, `
		SELECT id, name, repo, created_at FROM project WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Repo, &p.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("failed to get project: %w", err)
	}

	return p, nil
}

// CreateDocument creates a new document (design or feature_spec) for a project.
// Verifies the project exists, and if kind is 'design', ensures at most one design per project.
// Returns ErrNotFound if the project does not exist.
// Returns ErrConflict if attempting to create a second design for the same project.
func (s *sqliteStore) CreateDocument(ctx context.Context, projectID, kind, title, ref string, commit *string) (Document, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Verify project exists
	var projectExists bool
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM project WHERE id = ?", projectID).Scan(&projectExists)
	if err != nil {
		return Document{}, fmt.Errorf("failed to check project: %w", err)
	}
	if !projectExists {
		return Document{}, ErrNotFound
	}

	// If kind is 'design', check that no design already exists for this project
	if kind == "design" {
		var designCount int
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM document WHERE project_id = ? AND kind = 'design'", projectID).Scan(&designCount)
		if err != nil {
			return Document{}, fmt.Errorf("failed to check existing designs: %w", err)
		}
		if designCount > 0 {
			return Document{}, ErrConflict
		}
	}

	// Create the document
	id := GenerateID()
	now := nowTimestamp()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO document (id, project_id, kind, title, ref, "commit", created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, projectID, kind, title, ref, commit, now, now)
	if err != nil {
		return Document{}, fmt.Errorf("failed to create document: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Document{}, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return Document{
		ID:        id,
		ProjectID: projectID,
		Kind:      kind,
		Title:     title,
		Ref:       ref,
		Commit:    commit,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ListDocuments retrieves all documents for a project, optionally filtered by kind.
// Returns an empty slice (not nil) if no documents exist.
func (s *sqliteStore) ListDocuments(ctx context.Context, projectID string, kind *string) ([]Document, error) {
	query := `
		SELECT id, project_id, kind, title, ref, "commit", created_at, updated_at
		FROM document
		WHERE project_id = ?
	`
	args := []interface{}{projectID}

	if kind != nil {
		query += ` AND kind = ?`
		args = append(args, *kind)
	}

	query += ` ORDER BY created_at, id`

	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	docs := make([]Document, 0)
	for rows.Next() {
		var d Document
		err := rows.Scan(&d.ID, &d.ProjectID, &d.Kind, &d.Title, &d.Ref, &d.Commit, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}
		docs = append(docs, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating documents: %w", err)
	}

	return docs, nil
}

// CreateTasks bulk-creates tasks in a single transaction.
// - All tasks must have non-empty title, spec, and document_id.
// - Validates that document_id references a document in the given project.
// - Resolves depends_on refs as either batch keys or existing task ids in the project.
// - Returns 400-level error for validation failures; the tx rolls back and nothing is created.
func (s *sqliteStore) CreateTasks(ctx context.Context, projectID string, tasks []TaskInput) ([]Task, error) {
	if len(tasks) == 0 {
		return []Task{}, nil
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Validate all tasks and resolve dependencies
	keyToID := make(map[string]string)
	createdTasks := make([]Task, 0, len(tasks))
	now := nowTimestamp()

	for _, input := range tasks {
		// Validate title and spec are non-empty
		if strings.TrimSpace(input.Title) == "" {
			return nil, invalid("EMPTY_TITLE", "title is required")
		}
		if strings.TrimSpace(input.Spec) == "" {
			return nil, invalid("EMPTY_SPEC", "spec is required")
		}
		if strings.TrimSpace(input.DocumentID) == "" {
			return nil, invalid("MISSING_DOCUMENT_ID", "document_id is required")
		}

		// Verify document_id references a document in this project
		var docProjectID string
		err := tx.QueryRowContext(ctx, "SELECT project_id FROM document WHERE id = ?", input.DocumentID).Scan(&docProjectID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, invalid("INVALID_DOCUMENT_ID", "document_id does not exist")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to verify document: %w", err)
		}
		if docProjectID != projectID {
			return nil, invalid("DOCUMENT_NOT_IN_PROJECT", "document_id is not in this project")
		}

		// Generate task id
		taskID := GenerateID()
		keyToID[input.Key] = taskID

		task := Task{
			ID:        taskID,
			ProjectID: projectID,
			DocumentID: input.DocumentID,
			Title:     input.Title,
			Spec:      input.Spec,
			State:     "backlog",
			CreatedAt: now,
			UpdatedAt: now,
		}

		// Insert task
		_, err = tx.ExecContext(ctx, `
			INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, task.ID, task.ProjectID, task.DocumentID, task.Title, task.Spec, task.State, task.CreatedAt, task.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to insert task: %w", err)
		}

		createdTasks = append(createdTasks, task)
	}

	// Now insert task_dep edges, resolving references
	for i, input := range tasks {
		if len(input.DependsOn) == 0 {
			continue
		}

		taskID := createdTasks[i].ID

		for _, ref := range input.DependsOn {
			// Check for self-dependency
			if ref == input.Key && input.Key != "" {
				return nil, invalid("SELF_DEPENDENCY", "a task cannot depend on itself")
			}

			var dependsOnID string

			// Try to resolve as a batch key first
			if keyID, exists := keyToID[ref]; exists {
				dependsOnID = keyID
			} else {
				// Try to resolve as an existing task id in this project
				var existingProjectID string
				err := tx.QueryRowContext(ctx, "SELECT project_id FROM task WHERE id = ?", ref).Scan(&existingProjectID)
				if errors.Is(err, sql.ErrNoRows) {
					return nil, invalid("UNKNOWN_DEPENDENCY", "depends_on references an unknown task")
				}
				if err != nil {
					return nil, fmt.Errorf("failed to verify dependency: %w", err)
				}
				if existingProjectID != projectID {
					return nil, invalid("DEPENDENCY_NOT_IN_PROJECT", "depends_on references a task in another project")
				}
				dependsOnID = ref
			}

			// Insert edge
			_, err = tx.ExecContext(ctx, `
				INSERT INTO task_dep (task_id, depends_on_id)
				VALUES (?, ?)
			`, taskID, dependsOnID)
			if err != nil {
				return nil, fmt.Errorf("failed to insert task_dep: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return createdTasks, nil
}

// GetTask retrieves a task by id, including its dependencies and links.
// Returns ErrNotFound if the task does not exist.
func (s *sqliteStore) GetTask(ctx context.Context, id string) (TaskWithDepsAndLinks, error) {
	var t Task
	err := s.conn.QueryRowContext(ctx, `
		SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
		FROM task WHERE id = ?
	`, id).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return TaskWithDepsAndLinks{}, ErrNotFound
	}
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to get task: %w", err)
	}

	// Fetch dependencies
	depRows, err := s.conn.QueryContext(ctx, `
		SELECT depends_on_id FROM task_dep WHERE task_id = ? ORDER BY depends_on_id
	`, id)
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to query dependencies: %w", err)
	}
	defer depRows.Close()

	dependsOn := make([]string, 0)
	for depRows.Next() {
		var depID string
		if err := depRows.Scan(&depID); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to scan dependency: %w", err)
		}
		dependsOn = append(dependsOn, depID)
	}
	if err := depRows.Err(); err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("error iterating dependencies: %w", err)
	}

	// Fetch links
	linkRows, err := s.conn.QueryContext(ctx, `
		SELECT id, task_id, kind, value FROM task_link WHERE task_id = ? ORDER BY id
	`, id)
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to query links: %w", err)
	}
	defer linkRows.Close()

	links := make([]TaskLink, 0)
	for linkRows.Next() {
		var link TaskLink
		if err := linkRows.Scan(&link.ID, &link.TaskID, &link.Kind, &link.Value); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to scan link: %w", err)
		}
		links = append(links, link)
	}
	if err := linkRows.Err(); err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("error iterating links: %w", err)
	}

	return TaskWithDepsAndLinks{
		ID:            t.ID,
		ProjectID:     t.ProjectID,
		DocumentID:    t.DocumentID,
		Title:         t.Title,
		Spec:          t.Spec,
		State:         t.State,
		Assignee:      t.Assignee,
		LeaseExpiresAt: t.LeaseExpiresAt,
		Result:        t.Result,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
		DependsOn:     dependsOn,
		Links:         links,
	}, nil
}

// claimableSQL is the SQL predicate for the claimable condition, reused by both GetTask and T09's claim.
// A task is claimable iff:
// - state = 'ready'
// - all dependencies are done
// - no live lease (lease_expires_at IS NULL OR lease_expires_at < now)
// IMPORTANT: T09 (atomic claim) reuses this exact predicate in its UPDATE statement.
// If you change this, update both places.
const claimableSQL = `state = 'ready'
	AND NOT EXISTS (
		SELECT 1 FROM task_dep d
		JOIN task t2 ON t2.id = d.depends_on_id
		WHERE d.task_id = task.id AND t2.state != 'done'
	)
	AND (lease_expires_at IS NULL OR lease_expires_at < ?)`

// ListTasks retrieves tasks for a project with optional filters.
// Filters compose with AND logic.
func (s *sqliteStore) ListTasks(ctx context.Context, projectID string, filter TaskListFilter) ([]Task, error) {
	query := `SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
		FROM task
		WHERE project_id = ?`
	args := []interface{}{projectID}

	if filter.State != nil {
		query += ` AND state = ?`
		args = append(args, *filter.State)
	}

	if filter.Assignee != nil {
		query += ` AND assignee = ?`
		args = append(args, *filter.Assignee)
	}

	if filter.Claimable {
		query += ` AND ` + claimableSQL
		args = append(args, nowTimestamp())
	}

	query += ` ORDER BY created_at, id`

	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tasks: %w", err)
	}

	return tasks, nil
}

// ClaimTask atomically claims a task as in_progress by a given agent.
// It reuses the claimableSQL predicate to ensure the task is ready, has no unfinished deps,
// and has no live lease. The claim is a single conditional UPDATE.
// Returns the claimed Task on success (rowsAffected == 1).
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the task is not claimable (already claimed, not ready, unfinished deps, etc).
func (s *sqliteStore) ClaimTask(ctx context.Context, taskID, agentID string, leaseTTL time.Duration) (Task, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := nowTimestamp()
	leaseExpiry := leaseExpiryTimestamp(leaseTTL)

	// Single conditional UPDATE reusing claimableSQL
	result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET state='in_progress', assignee=?, lease_expires_at=?, updated_at=?
		WHERE id=? AND `+claimableSQL,
		agentID, leaseExpiry, now, taskID, now)
	if err != nil {
		return Task{}, fmt.Errorf("failed to claim task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 1 {
		// Claim succeeded. Append event in the same transaction.
		_, err := s.AppendEvent(ctx, tx, taskID, agentID, "claim", nil, nil)
		if err != nil {
			return Task{}, fmt.Errorf("failed to append claim event: %w", err)
		}

		// SELECT the claimed task within the same transaction
		var t Task
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch claimed task: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return Task{}, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return t, nil
	}

	// rowsAffected == 0: task was not claimable. Determine the cause for the right error.
	var taskExists bool
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM task WHERE id = ?", taskID).Scan(&taskExists)
	if err != nil {
		return Task{}, fmt.Errorf("failed to check task existence: %w", err)
	}

	if !taskExists {
		// Task does not exist -> ErrNotFound
		tx.Rollback()
		return Task{}, ErrNotFound
	}

	// Task exists but not claimable (not ready, unfinished deps, or live lease) -> ErrConflict
	tx.Rollback()
	return Task{}, ErrConflict
}

// HeartbeatTask atomically extends the lease on an in_progress task.
// It reuses the pattern from ClaimTask: a single conditional UPDATE within a transaction,
// updating lease_expires_at and appending a heartbeat event in the same tx.
// Returns the updated Task on success (rowsAffected == 1).
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the task is not in_progress or not assigned to the given agentID.
func (s *sqliteStore) HeartbeatTask(ctx context.Context, taskID, agentID string, leaseTTL time.Duration) (Task, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := nowTimestamp()
	leaseExpiry := leaseExpiryTimestamp(leaseTTL)

	// Single conditional UPDATE: only update if state is 'in_progress' AND assignee matches
	result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET lease_expires_at=?, updated_at=?
		WHERE id=? AND state='in_progress' AND assignee=?
	`, leaseExpiry, now, taskID, agentID)
	if err != nil {
		return Task{}, fmt.Errorf("failed to heartbeat task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 1 {
		// Heartbeat succeeded. Append event in the same transaction.
		_, err := s.AppendEvent(ctx, tx, taskID, agentID, "heartbeat", nil, nil)
		if err != nil {
			return Task{}, fmt.Errorf("failed to append heartbeat event: %w", err)
		}

		// SELECT the updated task within the same transaction
		var t Task
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch updated task: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return Task{}, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return t, nil
	}

	// rowsAffected == 0: task was not heartbeateable. Determine the cause for the right error.
	var taskExists bool
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM task WHERE id = ?", taskID).Scan(&taskExists)
	if err != nil {
		return Task{}, fmt.Errorf("failed to check task existence: %w", err)
	}

	if !taskExists {
		// Task does not exist -> ErrNotFound
		tx.Rollback()
		return Task{}, ErrNotFound
	}

	// Task exists but not heartbeateable (not in_progress or wrong assignee) -> ErrConflict
	tx.Rollback()
	return Task{}, ErrConflict
}

// PromoteTask atomically promotes a task from backlog to ready.
// It performs a single conditional UPDATE statement within a transaction.
// If the task is in backlog, it updates state to 'ready', appends a transition event,
// and returns the promoted Task.
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the task is not in backlog.
func (s *sqliteStore) PromoteTask(ctx context.Context, taskID string) (Task, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := nowTimestamp()

	// Single conditional UPDATE: only update if state is 'backlog'
	result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET state='ready', updated_at=?
		WHERE id=? AND state='backlog'
	`, now, taskID)
	if err != nil {
		return Task{}, fmt.Errorf("failed to promote task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 1 {
		// Promotion succeeded. Append transition event in the same transaction.
		note := "backlog->ready"
		_, err := s.AppendEvent(ctx, tx, taskID, "system", "transition", nil, &note)
		if err != nil {
			return Task{}, fmt.Errorf("failed to append transition event: %w", err)
		}

		// SELECT the promoted task within the same transaction
		var t Task
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch promoted task: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return Task{}, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return t, nil
	}

	// rowsAffected == 0: task was not in backlog. Determine the cause for the right error.
	var taskExists bool
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM task WHERE id = ?", taskID).Scan(&taskExists)
	if err != nil {
		return Task{}, fmt.Errorf("failed to check task existence: %w", err)
	}

	if !taskExists {
		// Task does not exist -> ErrNotFound
		tx.Rollback()
		return Task{}, ErrNotFound
	}

	// Task exists but not in backlog -> ErrConflict
	tx.Rollback()
	return Task{}, ErrConflict
}

// SubmitTask atomically transitions a task from in_progress to review.
// It validates link kinds, updates the task (clearing the lease), inserts task_link rows,
// appends a submit event, and returns the updated task with all links, all within one transaction.
// Returns the updated TaskWithDepsAndLinks on success.
// Returns ValidationError if a link kind is invalid.
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the task is not in_progress or not assigned to the given agentID.
func (s *sqliteStore) SubmitTask(ctx context.Context, taskID, agentID, result string, links []LinkInput) (TaskWithDepsAndLinks, error) {
	// Validate link kinds first (before mutating anything)
	validKinds := map[string]bool{"pr": true, "branch": true, "commit": true, "ci": true}
	for _, link := range links {
		if !validKinds[link.Kind] {
			return TaskWithDepsAndLinks{}, invalid("INVALID_LINK_KIND", fmt.Sprintf("invalid link kind: %s", link.Kind))
		}
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := nowTimestamp()

	// Single conditional UPDATE: transition from in_progress to review, store result, clear lease
	result_ptr := &result
	update_result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET state='review', result=?, lease_expires_at=NULL, updated_at=?
		WHERE id=? AND state='in_progress' AND assignee=?
	`, result_ptr, now, taskID, agentID)
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to submit task: %w", err)
	}

	rowsAffected, err := update_result.RowsAffected()
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 1 {
		// Submit succeeded. Insert task_link rows.
		for _, link := range links {
			linkID := GenerateID()
			_, err := tx.ExecContext(ctx, `
				INSERT INTO task_link (id, task_id, kind, value)
				VALUES (?, ?, ?, ?)
			`, linkID, taskID, link.Kind, link.Value)
			if err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to insert link: %w", err)
			}
		}

		// Append submit event in the same transaction
		_, err := s.AppendEvent(ctx, tx, taskID, agentID, "submit", nil, nil)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to append submit event: %w", err)
		}

		// SELECT the submitted task within the same transaction (reuse GetTask logic)
		var t Task
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to fetch submitted task: %w", err)
		}

		// Fetch dependencies
		depRows, err := tx.QueryContext(ctx, `
			SELECT depends_on_id FROM task_dep WHERE task_id = ? ORDER BY depends_on_id
		`, taskID)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to query dependencies: %w", err)
		}
		defer depRows.Close()

		dependsOn := make([]string, 0)
		for depRows.Next() {
			var depID string
			if err := depRows.Scan(&depID); err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to scan dependency: %w", err)
			}
			dependsOn = append(dependsOn, depID)
		}
		if err := depRows.Err(); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("error iterating dependencies: %w", err)
		}

		// Fetch links (including those we just inserted)
		linkRows, err := tx.QueryContext(ctx, `
			SELECT id, task_id, kind, value FROM task_link WHERE task_id = ? ORDER BY id
		`, taskID)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to query links: %w", err)
		}
		defer linkRows.Close()

		fetchedLinks := make([]TaskLink, 0)
		for linkRows.Next() {
			var link TaskLink
			if err := linkRows.Scan(&link.ID, &link.TaskID, &link.Kind, &link.Value); err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to scan link: %w", err)
			}
			fetchedLinks = append(fetchedLinks, link)
		}
		if err := linkRows.Err(); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("error iterating links: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return TaskWithDepsAndLinks{
			ID:            t.ID,
			ProjectID:     t.ProjectID,
			DocumentID:    t.DocumentID,
			Title:         t.Title,
			Spec:          t.Spec,
			State:         t.State,
			Assignee:      t.Assignee,
			LeaseExpiresAt: t.LeaseExpiresAt,
			Result:        t.Result,
			CreatedAt:     t.CreatedAt,
			UpdatedAt:     t.UpdatedAt,
			DependsOn:     dependsOn,
			Links:         fetchedLinks,
		}, nil
	}

	// rowsAffected == 0: task was not submittable. Determine the cause for the right error.
	var taskExists bool
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM task WHERE id = ?", taskID).Scan(&taskExists)
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to check task existence: %w", err)
	}

	if !taskExists {
		// Task does not exist -> ErrNotFound
		tx.Rollback()
		return TaskWithDepsAndLinks{}, ErrNotFound
	}

	// Task exists but not submittable (not in_progress or wrong assignee) -> ErrConflict
	tx.Rollback()
	return TaskWithDepsAndLinks{}, ErrConflict
}
