package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
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
	ListProjects(ctx context.Context) ([]Project, error)
	CreateDocument(ctx context.Context, projectID, kind, title, ref string, commit *string) (Document, error)
	ListDocuments(ctx context.Context, projectID string, kind *string) ([]Document, error)
	CreateTasks(ctx context.Context, projectID string, tasks []TaskInput) ([]Task, error)
	GetTask(ctx context.Context, id string) (TaskWithDepsAndLinks, error)
	ListTasks(ctx context.Context, projectID string, filter TaskListFilter) ([]Task, error)
	ClaimTask(ctx context.Context, taskID, agentID, model string, leaseTTL time.Duration) (Task, error)
	HeartbeatTask(ctx context.Context, taskID, agentID string, leaseTTL time.Duration) (Task, error)
	PromoteTask(ctx context.Context, taskID string) (Task, error)
	SubmitTask(ctx context.Context, taskID, agentID, result string, links []LinkInput) (TaskWithDepsAndLinks, error)
	AddReview(ctx context.Context, taskID, actor, verdict string, note *string) (Event, error)
	TransitionTask(ctx context.Context, taskID, to string, note *string) (Task, error)
}

// sqliteStore wraps a SQLite database connection and provides migration functionality.
type sqliteStore struct {
	conn           *sql.DB
	allowedModels  []string
	allowedModelsM map[string]bool
}

// Open opens a database connection and applies all pending migrations.
// The dbPath should be a file path (e.g., "agentask.db") or "file::memory:?cache=shared"
// for an in-memory database.
// It configures WAL mode, foreign keys, and busy timeout via DSN pragmas.
// allowedModels is the list of valid model identifiers for task creation.
func Open(dbPath string, allowedModels []string) (Store, error) {
	// Build the DSN with pragmas for WAL, foreign_keys, and busy_timeout.
	// This ensures every connection from the pool has these pragmas applied.
	dsn := buildDSN(dbPath)

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set MaxOpenConns to 1 for single-writer SQLite (as per DESIGN.md §7)
	conn.SetMaxOpenConns(1)

	// Build a map of allowed models for O(1) lookup
	allowedModelsM := make(map[string]bool)
	for _, m := range allowedModels {
		allowedModelsM[m] = true
	}

	store := &sqliteStore{
		conn:           conn,
		allowedModels:  allowedModels,
		allowedModelsM: allowedModelsM,
	}

	// Apply migrations in a transaction
	if err := store.migrate(migrationsFS); err != nil {
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

// migrate applies all pending migrations from the given filesystem.
// It disables foreign key enforcement before the migration transaction begins (since PRAGMA inside
// a transaction is a no-op) and restores it afterward. Before commit, it runs an integrity check
// to ensure no foreign key violations occurred, failing the migration if any are found.
func (s *sqliteStore) migrate(fsys fs.FS) error {
	// Disable foreign keys on the connection before starting the transaction
	// SQLite requires PRAGMA foreign_keys to be set outside a transaction
	if _, err := s.conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("failed to disable foreign keys before migration: %w", err)
	}
	defer func() {
		if _, err := s.conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
			panic(fmt.Sprintf("failed to restore foreign keys after migration: %v", err))
		}
	}()

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
	migrations, err := listMigrations(fsys)
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
		data, err := fs.ReadFile(fsys, migration.path)
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

	// Before commit, run a foreign key integrity check
	// This catches any migrations that would leave dangling foreign key references.
	// PRAGMA foreign_key_check returns 4 columns (table, rowid, parent, fkid) per violation.
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("failed to run foreign key integrity check: %w", err)
	}
	defer rows.Close()

	var violationCount int
	for rows.Next() {
		violationCount++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read foreign key check results: %w", err)
	}
	if violationCount > 0 {
		return fmt.Errorf("migration integrity check failed: found %d foreign key violations", violationCount)
	}

	return tx.Commit()
}

type migration struct {
	version string
	path    string
}

// listMigrations returns all migration files sorted by version from the given filesystem.
func listMigrations(fsys fs.FS) ([]migration, error) {
	var migrations []migration

	// Read all files in the migrations directory
	entries, err := fs.ReadDir(fsys, "migrations")
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
	ID        string `db:"id" json:"id"`
	Name      string `db:"name" json:"name"`
	Repo      string `db:"repo" json:"repo"`
	CreatedAt string `db:"created_at" json:"created_at"`
}

// Document represents a design or feature spec document.
type Document struct {
	ID        string  `db:"id" json:"id"`
	ProjectID string  `db:"project_id" json:"project_id"`
	Kind      string  `db:"kind" json:"kind"` // 'design' or 'feature_spec'
	Title     string  `db:"title" json:"title"`
	Ref       string  `db:"ref" json:"ref"`
	Commit    *string `db:"commit" json:"commit"` // nullable
	CreatedAt string  `db:"created_at" json:"created_at"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// Task represents a task on the board.
type Task struct {
	ID             string   `db:"id" json:"id"`
	ProjectID      string   `db:"project_id" json:"project_id"`
	DocumentID     string   `db:"document_id" json:"document_id"`
	Title          string   `db:"title" json:"title"`
	Spec           string   `db:"spec" json:"spec"`
	State          string   `db:"state" json:"state"`
	Assignee       *string  `db:"assignee" json:"assignee"`                 // nullable
	LeaseExpiresAt *string  `db:"lease_expires_at" json:"lease_expires_at"` // nullable
	Result         *string  `db:"result" json:"result"`                     // nullable
	Model          string   `db:"model" json:"model"`
	Kind           string   `db:"kind" json:"kind"`
	ReviewModels   []string `db:"review_models" json:"review_models"`
	ReviewRound    int      `db:"review_round" json:"review_round"`
	TargetTaskID   *string  `db:"target_task_id" json:"target_task_id"` // nullable
	CreatedAt      string   `db:"created_at" json:"created_at"`
	UpdatedAt      string   `db:"updated_at" json:"updated_at"`
}

// TaskLink represents a link from a task to external resources (PR, branch, commit, CI).
type TaskLink struct {
	ID     string `db:"id" json:"id"`
	TaskID string `db:"task_id" json:"task_id"`
	Kind   string `db:"kind" json:"kind"` // 'pr', 'branch', 'commit', or 'ci'
	Value  string `db:"value" json:"value"`
}

// TaskInput is the input format for bulk task creation.
type TaskInput struct {
	Key          string   `json:"key"` // optional client-provided key for intra-batch deps
	Title        string   `json:"title"`
	Spec         string   `json:"spec"`
	DocumentID   string   `json:"document_id"`
	DependsOn    []string `json:"depends_on"` // refs to task ids or keys in the batch
	Model        string   `json:"model"`
	ReviewModels []string `json:"review_models"`
}

// LinkInput is the input format for task links during submission.
type LinkInput struct {
	Kind  string `json:"kind"` // 'pr', 'branch', 'commit', or 'ci'
	Value string `json:"value"`
}

// TaskWithDepsAndLinks combines a Task with its dependencies and links.
type TaskWithDepsAndLinks struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	DocumentID     string     `json:"document_id"`
	Title          string     `json:"title"`
	Spec           string     `json:"spec"`
	State          string     `json:"state"`
	Assignee       *string    `json:"assignee"`
	LeaseExpiresAt *string    `json:"lease_expires_at"`
	Result         *string    `json:"result"`
	Model          string     `json:"model"`
	Kind           string     `json:"kind"`
	ReviewModels   []string   `json:"review_models"`
	ReviewRound    int        `json:"review_round"`
	TargetTaskID   *string    `json:"target_task_id"`
	CreatedAt      string     `json:"created_at"`
	UpdatedAt      string     `json:"updated_at"`
	DependsOn      []string   `json:"depends_on"`
	Links          []TaskLink `json:"links"`
}

// TaskListFilter contains filters for listing tasks.
type TaskListFilter struct {
	State     *string
	Assignee  *string
	Model     *string
	Claimable bool
}

// Event represents an audit/event log entry.
type Event struct {
	ID        string  `db:"id" json:"id"`
	TaskID    string  `db:"task_id" json:"task_id"`
	Actor     string  `db:"actor" json:"actor"`
	Kind      string  `db:"kind" json:"kind"`
	Verdict   *string `db:"verdict" json:"verdict"` // nullable
	Note      *string `db:"note" json:"note"`       // nullable
	CreatedAt string  `db:"created_at" json:"created_at"`
}

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a constraint is violated (e.g., second design per project).
var ErrConflict = errors.New("conflict")

// ConflictError is a typed conflict with an error code and message.
// Handlers map it to HTTP 409 via errors.As.
type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func conflict(code, message string) error {
	return &ConflictError{Code: code, Message: message}
}

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

// getDefaultModel returns the default model from the allowlist.
// Prefers 'haiku' if available (backward compatibility), otherwise returns the first allowlisted model.
func (s *sqliteStore) getDefaultModel() string {
	if s.allowedModelsM["haiku"] {
		return "haiku"
	}
	if len(s.allowedModels) > 0 {
		return s.allowedModels[0]
	}
	return "haiku"
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

// ListProjects lists all projects ordered by created_at.
// Returns an empty slice (not nil) when no projects exist.
func (s *sqliteStore) ListProjects(ctx context.Context) ([]Project, error) {
	query := `
		SELECT id, name, repo, created_at FROM project
		ORDER BY created_at
	`

	rows, err := s.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var p Project
		err := rows.Scan(&p.ID, &p.Name, &p.Repo, &p.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projects = append(projects, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating projects: %w", err)
	}

	return projects, nil
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

		model := input.Model
		if model == "" {
			model = s.getDefaultModel()
		}

		// Validate model against allowlist
		if !s.allowedModelsM[model] {
			return nil, invalid("UNKNOWN_MODEL", fmt.Sprintf("unknown model: %s", model))
		}

		// Validate each review_models entry against allowlist
		for _, reviewModel := range input.ReviewModels {
			if !s.allowedModelsM[reviewModel] {
				return nil, invalid("UNKNOWN_MODEL", fmt.Sprintf("unknown review model: %s", reviewModel))
			}
		}

		var reviewModelsJSON *string
		if len(input.ReviewModels) > 0 {
			data, err := json.Marshal(input.ReviewModels)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal review_models: %w", err)
			}
			str := string(data)
			reviewModelsJSON = &str
		}

		task := Task{
			ID:           taskID,
			ProjectID:    projectID,
			DocumentID:   input.DocumentID,
			Title:        input.Title,
			Spec:         input.Spec,
			State:        "backlog",
			Model:        model,
			Kind:         "implement",
			ReviewModels: input.ReviewModels,
			ReviewRound:  0,
			TargetTaskID: nil,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		// Normalize ReviewModels to empty slice when nil
		if task.ReviewModels == nil {
			task.ReviewModels = []string{}
		}

		// Insert task
		_, err = tx.ExecContext(ctx, `
			INSERT INTO task (id, project_id, document_id, title, spec, state, model, kind, review_models, review_round, target_task_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, task.ID, task.ProjectID, task.DocumentID, task.Title, task.Spec, task.State, task.Model, task.Kind, reviewModelsJSON, task.ReviewRound, task.TargetTaskID, task.CreatedAt, task.UpdatedAt)
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
	var reviewModelsJSON *string
	err := s.conn.QueryRowContext(ctx, `
		SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
		FROM task WHERE id = ?
	`, id).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return TaskWithDepsAndLinks{}, ErrNotFound
	}
	if err != nil {
		return TaskWithDepsAndLinks{}, fmt.Errorf("failed to get task: %w", err)
	}

	// Unmarshal review_models from JSON
	t.ReviewModels = []string{}
	if reviewModelsJSON != nil {
		if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
		}
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
		ID:             t.ID,
		ProjectID:      t.ProjectID,
		DocumentID:     t.DocumentID,
		Title:          t.Title,
		Spec:           t.Spec,
		State:          t.State,
		Assignee:       t.Assignee,
		LeaseExpiresAt: t.LeaseExpiresAt,
		Result:         t.Result,
		Model:          t.Model,
		Kind:           t.Kind,
		ReviewModels:   t.ReviewModels,
		ReviewRound:    t.ReviewRound,
		TargetTaskID:   t.TargetTaskID,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		DependsOn:      dependsOn,
		Links:          links,
	}, nil
}

// claimableSQL is the SQL predicate for the claimable condition, reused by both ListTasks and ClaimTask.
// A task is claimable iff:
// - state = 'ready'
// - all dependencies are done
// - no live lease (lease_expires_at IS NULL OR lease_expires_at < now)
// IMPORTANT: This predicate is reused in ListTasks and ClaimTask (in UPDATE statement).
// If you change this, update all usages.
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
	query := `SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
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

	if filter.Model != nil {
		query += ` AND model = ?`
		args = append(args, *filter.Model)
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
		var reviewModelsJSON *string
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		// Unmarshal review_models from JSON
		t.ReviewModels = []string{}
		if reviewModelsJSON != nil {
			if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
				return nil, fmt.Errorf("failed to unmarshal review_models: %w", err)
			}
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
// and has no live lease. The claim also checks that the task's model matches the declared model.
// The claim is a single conditional UPDATE.
// Returns the claimed Task on success (rowsAffected == 1).
// Returns ErrNotFound if the task doesn't exist.
// Returns MODEL_MISMATCH ConflictError if the task's model doesn't match.
// Returns ErrConflict if the task is not claimable (already claimed, not ready, unfinished deps, etc).
func (s *sqliteStore) ClaimTask(ctx context.Context, taskID, agentID, model string, leaseTTL time.Duration) (Task, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := nowTimestamp()
	leaseExpiry := leaseExpiryTimestamp(leaseTTL)

	// Single conditional UPDATE reusing claimableSQL with additional model check
	result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET state='in_progress', assignee=?, lease_expires_at=?, updated_at=?
		WHERE id=? AND model=? AND `+claimableSQL,
		agentID, leaseExpiry, now, taskID, model, now)
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
		var reviewModelsJSON *string
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch claimed task: %w", err)
		}

		// Unmarshal review_models from JSON
		t.ReviewModels = []string{}
		if reviewModelsJSON != nil {
			if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
				return Task{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return Task{}, fmt.Errorf("failed to commit transaction: %w", err)
		}

		return t, nil
	}

	// rowsAffected == 0: task was not claimed. Determine the cause for the right error.
	var taskExists bool
	var taskModel string
	var isOtherwiseClaimable bool
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) > 0, COALESCE(model, ''), EXISTS(SELECT 1 FROM task WHERE id = ? AND `+claimableSQL+`)
		FROM task WHERE id = ?
	`, taskID, now, taskID).Scan(&taskExists, &taskModel, &isOtherwiseClaimable)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("failed to check task: %w", err)
	}

	if !taskExists {
		// Task does not exist -> ErrNotFound
		tx.Rollback()
		return Task{}, ErrNotFound
	}

	// Task exists. Check if the model doesn't match and the task is otherwise claimable.
	if taskModel != model && isOtherwiseClaimable {
		tx.Rollback()
		return Task{}, conflict("MODEL_MISMATCH", fmt.Sprintf("Task model '%s' does not match declared model '%s'", taskModel, model))
	}

	// Task exists but is not claimable for some reason (not ready, unfinished deps, live lease, or model mismatch) -> ErrConflict
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
		var reviewModelsJSON *string
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch updated task: %w", err)
		}

		// Unmarshal review_models from JSON
		t.ReviewModels = []string{}
		if reviewModelsJSON != nil {
			if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
				return Task{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
			}
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
		var reviewModelsJSON *string
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return Task{}, fmt.Errorf("failed to fetch promoted task: %w", err)
		}

		// Unmarshal review_models from JSON
		t.ReviewModels = []string{}
		if reviewModelsJSON != nil {
			if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
				return Task{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
			}
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
		// Submit succeeded. Insert task_link rows (dedup by task_id, kind, value).
		for _, link := range links {
			// Check if this link already exists
			var existingCount int
			err := tx.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM task_link WHERE task_id = ? AND kind = ? AND value = ?
			`, taskID, link.Kind, link.Value).Scan(&existingCount)
			if err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to check link existence: %w", err)
			}

			// Only insert if it doesn't exist
			if existingCount == 0 {
				linkID := GenerateID()
				_, err := tx.ExecContext(ctx, `
					INSERT INTO task_link (id, task_id, kind, value)
					VALUES (?, ?, ?, ?)
				`, linkID, taskID, link.Kind, link.Value)
				if err != nil {
					return TaskWithDepsAndLinks{}, fmt.Errorf("failed to insert link: %w", err)
				}
			}
		}

		// Append submit event in the same transaction
		_, err := s.AppendEvent(ctx, tx, taskID, agentID, "submit", nil, nil)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to append submit event: %w", err)
		}

		// Fetch the submitted task to check if it's an implement task
		var t Task
		var reviewModelsJSON *string
		err = tx.QueryRowContext(ctx, `
			SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
			FROM task WHERE id = ?
		`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			return TaskWithDepsAndLinks{}, fmt.Errorf("failed to fetch submitted task: %w", err)
		}

		// Unmarshal review_models from JSON
		t.ReviewModels = []string{}
		if reviewModelsJSON != nil {
			if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
			}
		}

		// If this is an implement task entering review, auto-spawn review tasks
		if t.Kind == "implement" {
			// Increment review_round
			newReviewRound := t.ReviewRound + 1
			_, err := tx.ExecContext(ctx, `
				UPDATE task SET review_round = ? WHERE id = ?
			`, newReviewRound, taskID)
			if err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to increment review_round: %w", err)
			}

			// Extract PR link from the submitted links
			var prLink string
			for _, link := range links {
				if link.Kind == "pr" {
					prLink = link.Value
					break
				}
			}

			// Determine reviewers (default to ["opus"] if empty)
			reviewers := t.ReviewModels
			if len(reviewers) == 0 {
				reviewers = []string{"opus"}
			}

			// Create a review task for each reviewer
			for _, reviewerModel := range reviewers {
				reviewTaskID := GenerateID()
				reviewTitle := "Review: " + t.Title + " [" + reviewerModel + "]"

				// Build the spec for the review task: a strict-review brief pointing at the parent's PR link
				reviewSpec := "Review the implementation:\n\n"
				if prLink != "" {
					reviewSpec += "Implementation PR: " + prLink + "\n\n"
				}
				reviewSpec += "Parent task: " + t.ID + "\n\n"
				reviewSpec += "## Instructions\n\n"
				reviewSpec += "Examine the submitted implementation and provide approval or rejection with written feedback.\n\n"
				reviewSpec += "Approve if:\n"
				reviewSpec += "- The implementation matches the specification\n"
				reviewSpec += "- The code is correct and follows the project conventions\n"
				reviewSpec += "- Tests pass and coverage is adequate\n\n"
				reviewSpec += "Reject if:\n"
				reviewSpec += "- The implementation has issues or does not match the specification\n"
				reviewSpec += "- Further work is needed before merging\n\n"
				reviewSpec += "Provide your verdict: approve or reject"

				reviewModelsJSON := (*string)(nil) // review tasks don't have review_models
				_, err := tx.ExecContext(ctx, `
					INSERT INTO task (id, project_id, document_id, title, spec, state, model, kind, review_models, review_round, target_task_id, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, reviewTaskID, t.ProjectID, t.DocumentID, reviewTitle, reviewSpec, "ready", reviewerModel, "review", reviewModelsJSON, newReviewRound, &t.ID, now, now)
				if err != nil {
					return TaskWithDepsAndLinks{}, fmt.Errorf("failed to create review task: %w", err)
				}
			}

			// Append spawn_review event on the parent task
			reviewersList, _ := json.Marshal(reviewers)
			eventNote := "Round " + fmt.Sprintf("%d", newReviewRound) + " with models: " + string(reviewersList)
			_, err = s.AppendEvent(ctx, tx, taskID, "system", "spawn_review", nil, &eventNote)
			if err != nil {
				return TaskWithDepsAndLinks{}, fmt.Errorf("failed to append spawn_review event: %w", err)
			}

			// Update t.ReviewRound for the response
			t.ReviewRound = newReviewRound
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
			ID:             t.ID,
			ProjectID:      t.ProjectID,
			DocumentID:     t.DocumentID,
			Title:          t.Title,
			Spec:           t.Spec,
			State:          t.State,
			Assignee:       t.Assignee,
			LeaseExpiresAt: t.LeaseExpiresAt,
			Result:         t.Result,
			Model:          t.Model,
			Kind:           t.Kind,
			ReviewModels:   t.ReviewModels,
			ReviewRound:    t.ReviewRound,
			TargetTaskID:   t.TargetTaskID,
			CreatedAt:      t.CreatedAt,
			UpdatedAt:      t.UpdatedAt,
			DependsOn:      dependsOn,
			Links:          fetchedLinks,
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

// AddReview records a review verdict event for a task.
// The task must be in 'review' state. Verdict must be 'approve' or 'reject'.
// Returns the created Event on success.
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the task is not in 'review' state.
// Returns ValidationError if verdict is invalid.
func (s *sqliteStore) AddReview(ctx context.Context, taskID, actor, verdict string, note *string) (Event, error) {
	// Validate verdict
	if verdict != "approve" && verdict != "reject" {
		return Event{}, invalid("INVALID_VERDICT", "verdict must be 'approve' or 'reject'")
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Load the task's current state
	var taskState string
	err = tx.QueryRowContext(ctx, "SELECT state FROM task WHERE id = ?", taskID).Scan(&taskState)
	if err != nil {
		if err == sql.ErrNoRows {
			return Event{}, ErrNotFound
		}
		return Event{}, fmt.Errorf("failed to load task state: %w", err)
	}

	// Task must be in 'review' state
	if taskState != "review" {
		return Event{}, ErrConflict
	}

	// Append the review event
	verdictPtr := &verdict
	event, err := s.AppendEvent(ctx, tx, taskID, actor, "review", verdictPtr, note)
	if err != nil {
		return Event{}, err
	}

	if err := tx.Commit(); err != nil {
		return Event{}, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return event, nil
}

// TransitionTask moves a task to a new state according to the transition rules.
// Valid transitions:
//   - to='done': allowed ONLY from 'approved'
//   - to='ready': allowed ONLY from 'approved'
//   - to='blocked' or 'failed': allowed from any ACTIVE state (backlog, ready, in_progress, review, approved)
//   - anything else: ErrConflict
//
// Returns the updated Task on success.
// Returns ErrNotFound if the task doesn't exist.
// Returns ErrConflict if the transition is not allowed.
// Returns ValidationError if 'to' state is invalid.
func (s *sqliteStore) TransitionTask(ctx context.Context, taskID, to string, note *string) (Task, error) {
	// Validate 'to' state
	validTargets := map[string]bool{"done": true, "ready": true, "blocked": true, "failed": true}
	if !validTargets[to] {
		return Task{}, invalid("INVALID_TARGET_STATE", "target state must be one of: done, ready, blocked, failed")
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Load current state
	var taskState string
	err = tx.QueryRowContext(ctx, "SELECT state FROM task WHERE id = ?", taskID).Scan(&taskState)
	if err != nil {
		if err == sql.ErrNoRows {
			return Task{}, ErrNotFound
		}
		return Task{}, fmt.Errorf("failed to load task state: %w", err)
	}

	// Apply transition rules
	canTransition := false

	switch to {
	case "done":
		// Allowed from 'approved' (being in approved implies a passed review)
		if taskState == "approved" {
			canTransition = true
		}

	case "ready":
		// Allowed from 'approved'
		if taskState == "approved" {
			canTransition = true
		}

	case "blocked", "failed":
		// Allowed from any active state (backlog, ready, in_progress, review, approved)
		// Terminal states are done, blocked, failed
		activeStates := map[string]bool{"backlog": true, "ready": true, "in_progress": true, "review": true, "approved": true}
		if activeStates[taskState] {
			canTransition = true
		}
	}

	if !canTransition {
		return Task{}, ErrConflict
	}

	// Perform the conditional UPDATE
	now := nowTimestamp()
	result, err := tx.ExecContext(ctx, `
		UPDATE task
		SET state=?, updated_at=?
		WHERE id=? AND state=?
	`, to, now, taskID, taskState)
	if err != nil {
		return Task{}, fmt.Errorf("failed to transition task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Task{}, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected != 1 {
		// This shouldn't happen given our checks above, but be safe
		return Task{}, ErrConflict
	}

	// Append transition event (actor="system", verdict=nil)
	_, err = s.AppendEvent(ctx, tx, taskID, "system", "transition", nil, note)
	if err != nil {
		return Task{}, err
	}

	// SELECT the updated task
	var t Task
	var reviewModelsJSON *string
	err = tx.QueryRowContext(ctx, `
		SELECT id, project_id, document_id, title, spec, state, assignee, lease_expires_at, result, model, kind, review_models, review_round, target_task_id, created_at, updated_at
		FROM task WHERE id = ?
	`, taskID).Scan(&t.ID, &t.ProjectID, &t.DocumentID, &t.Title, &t.Spec, &t.State, &t.Assignee, &t.LeaseExpiresAt, &t.Result, &t.Model, &t.Kind, &reviewModelsJSON, &t.ReviewRound, &t.TargetTaskID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return Task{}, fmt.Errorf("failed to fetch transitioned task: %w", err)
	}

	// Unmarshal review_models from JSON
	t.ReviewModels = []string{}
	if reviewModelsJSON != nil {
		if err := json.Unmarshal([]byte(*reviewModelsJSON), &t.ReviewModels); err != nil {
			return Task{}, fmt.Errorf("failed to unmarshal review_models: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return t, nil
}
