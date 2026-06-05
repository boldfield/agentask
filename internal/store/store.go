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
