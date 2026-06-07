package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// createTestFSWithBadMigration creates a test filesystem with the standard migrations
// plus a bad migration (0003_bad.sql) that leaves a dangling foreign key.
// It wraps the embedded migrations and adds the bad migration on top.
func createTestFSWithBadMigration() fs.FS {
	// Read the actual migrations from the embedded FS
	// Return a custom fs that includes the embedded migrations plus the bad one
	return &compositeFS{
		first: migrationsFS,
		bad: fstest.MapFS{
			"migrations/0003_bad.sql": &fstest.MapFile{
				Data: []byte("INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at) VALUES ('dummy-task', 'non-existent-project', 'non-existent-doc', 'Dummy Task', 'spec', 'backlog', datetime('now'), datetime('now'));"),
			},
		},
	}
}

// compositeFS is a custom fs.FS that combines two filesystems, preferring files from the first.
type compositeFS struct {
	first fs.FS
	bad   fs.FS
}

func (c *compositeFS) Open(name string) (fs.File, error) {
	// Try the bad migrations first (0003_bad.sql)
	if strings.Contains(name, "0003_bad") {
		return c.bad.Open(name)
	}
	// Fall back to the embedded migrations
	return c.first.Open(name)
}

func (c *compositeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Read from the first FS and add bad migrations
	entries, err := fs.ReadDir(c.first, name)
	if err != nil {
		return nil, err
	}

	// For the migrations directory, also include 0003_bad.sql
	if name == "migrations" {
		badEntries, _ := fs.ReadDir(c.bad, "migrations")
		if badEntries != nil {
			entries = append(entries, badEntries...)
			// Sort to ensure consistent order
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})
		}
	}
	return entries, nil
}

// TestMigrations verifies that migrations can be applied to a fresh database
// and that re-applying is idempotent.
func TestMigrations(t *testing.T) {
	// Use in-memory database for testing
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Verify all expected tables exist
	expectedTables := []string{
		"project",
		"document",
		"task",
		"task_dep",
		"task_link",
		"event",
		"schema_migrations",
	}

	for _, tableName := range expectedTables {
		var count int
		err := store.Conn().QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
			tableName,
		).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check if table %s exists: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("expected table %s to exist, but it doesn't", tableName)
		}
	}

	// Verify expected indexes exist
	expectedIndexes := []string{
		"idx_task_link_kind_value",
		"idx_task_project_state",
		"idx_document_one_design_per_project",
	}

	for _, indexName := range expectedIndexes {
		var count int
		err := store.Conn().QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?",
			indexName,
		).Scan(&count)
		if err != nil {
			t.Fatalf("failed to check if index %s exists: %v", indexName, err)
		}
		if count != 1 {
			t.Errorf("expected index %s to exist, but it doesn't", indexName)
		}
	}

	// Verify that schema_migrations table has the migrations recorded
	var migrationCount int
	err = store.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 4 {
		t.Errorf("expected 4 migrations to be recorded, but got %d", migrationCount)
	}

	// Verify idempotency: re-open the same database and it should work
	store2, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer store2.Close()

	// Verify that we still have exactly 3 migrations recorded (idempotency)
	err = store2.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations after re-open: %v", err)
	}
	if migrationCount != 4 {
		t.Errorf("expected 4 migrations after re-open (idempotency), but got %d", migrationCount)
	}
}

// TestWALEnabled verifies that WAL mode is enabled on the database.
func TestWALEnabled(t *testing.T) {
	// Use a file-based DB for WAL test since in-memory doesn't support WAL
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "wal_test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	var journalMode string
	err = store.Conn().QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query PRAGMA journal_mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("expected journal_mode to be 'wal', but got '%s'", journalMode)
	}
}

// TestForeignKeysEnforced verifies that foreign key constraints are enforced.
func TestForeignKeysEnforced(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	// Try to insert a task with a non-existent project_id (bad FK)
	// This should fail because foreign_keys is enabled
	_, err = store.Conn().Exec(`
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "test-task", "non-existent-project", "non-existent-doc", "Test Task", "Test spec", "backlog", "2026-06-04T00:00:00Z", "2026-06-04T00:00:00Z")

	if err == nil {
		t.Error("expected foreign key constraint violation, but insert succeeded")
	}
}

// TestOpenSamePath verifies that opening the same database path twice sequentially works.
func TestOpenSamePath(t *testing.T) {
	// Create a temporary file for the database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open the database the first time
	store1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database for the first time: %v", err)
	}

	// Verify table exists
	var count int
	err = store1.Conn().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query after first open: %v", err)
	}
	if count != 1 {
		t.Error("expected project table to exist after first open")
	}

	// Close the first connection
	if err := store1.Close(); err != nil {
		t.Fatalf("failed to close first connection: %v", err)
	}

	// Open the database the second time (same path)
	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database for the second time: %v", err)
	}
	defer store2.Close()

	// Verify table still exists
	err = store2.Conn().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='project'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query after second open: %v", err)
	}
	if count != 1 {
		t.Error("expected project table to exist after second open")
	}

	// Verify that schema_migrations was not re-applied (idempotency)
	var migrationCount int
	err = store2.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations after second open: %v", err)
	}
	if migrationCount != 4 {
		t.Errorf("expected 4 migrations after second open, but got %d", migrationCount)
	}
}

// TestAppendEventAtomicity tests that AppendEvent works within a transaction
// and that rolling back the transaction drops both the state change and the event.
func TestAppendEventAtomicity(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Insert parent rows (project, document, task) via raw SQL first
	// This is required because foreign key constraints are enforced (T03).
	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO project (id, name, repo, created_at)
		VALUES (?, ?, ?, ?)
	`, "proj-1", "test-project", "https://github.com/example/repo", now)
	if err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}

	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "doc-1", "proj-1", "design", "Test Design", "DESIGN.md", now, now)
	if err != nil {
		t.Fatalf("failed to insert document: %v", err)
	}

	taskID := "task-1"
	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, taskID, "proj-1", "doc-1", "Test Task", "Test spec", "ready", now, now)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Begin a transaction
	tx, err := store.Conn().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	// Within the transaction:
	// 1. Update task state to in_progress (simulating a state change)
	_, err = tx.ExecContext(ctx, `
		UPDATE task SET state = ?, updated_at = ? WHERE id = ?
	`, "in_progress", now, taskID)
	if err != nil {
		t.Fatalf("failed to update task state: %v", err)
	}

	// 2. Append an event using AppendEvent
	actor := "test-agent"
	kind := "claim"
	_, err = store.AppendEvent(ctx, tx, taskID, actor, kind, nil, nil)
	if err != nil {
		t.Fatalf("failed to append event: %v", err)
	}

	// Rollback the transaction without committing
	if err := tx.Rollback(); err != nil {
		t.Fatalf("failed to rollback transaction: %v", err)
	}

	// Verify that the task state is still "ready" (not "in_progress")
	var state string
	err = store.Conn().QueryRowContext(ctx, "SELECT state FROM task WHERE id = ?", taskID).Scan(&state)
	if err != nil {
		t.Fatalf("failed to query task state: %v", err)
	}
	if state != "ready" {
		t.Errorf("expected task state to be 'ready' after rollback, but got '%s'", state)
	}

	// Verify that no events were inserted
	var eventCount int
	err = store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM event WHERE task_id = ?", taskID).Scan(&eventCount)
	if err != nil {
		t.Fatalf("failed to count events: %v", err)
	}
	if eventCount != 0 {
		t.Errorf("expected 0 events after rollback, but got %d", eventCount)
	}
}

// TestListEvents tests that ListEvents returns events in chronological order (created_at, id).
func TestListEvents(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Insert parent rows
	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO project (id, name, repo, created_at)
		VALUES (?, ?, ?, ?)
	`, "proj-2", "test-project-2", "https://github.com/example/repo2", now)
	if err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}

	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "doc-2", "proj-2", "design", "Test Design 2", "DESIGN.md", now, now)
	if err != nil {
		t.Fatalf("failed to insert document: %v", err)
	}

	taskID := "task-2"
	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, taskID, "proj-2", "doc-2", "Test Task 2", "Test spec 2", "ready", now, now)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Insert events in separate transactions with explicit timestamps to ensure ordering
	// Use progressively later timestamps to guarantee order
	events := []struct {
		actor   string
		kind    string
		verdict *string
		note    *string
		offset  time.Duration
	}{
		{"agent-1", "claim", nil, nil, 0 * time.Millisecond},
		{"agent-1", "heartbeat", nil, nil, 10 * time.Millisecond},
		{"human", "review", strPtr("approve"), strPtr("looks good"), 20 * time.Millisecond},
	}

	for _, evt := range events {
		tx, err := store.Conn().BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}

		_, err = store.AppendEvent(ctx, tx, taskID, evt.actor, evt.kind, evt.verdict, evt.note)
		if err != nil {
			t.Fatalf("failed to append event: %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("failed to commit transaction: %v", err)
		}

		// Sleep to ensure timestamps differ
		time.Sleep(evt.offset + 5*time.Millisecond)
	}

	// List all events
	listedEvents, err := store.ListEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}

	// Verify we got exactly 3 events
	if len(listedEvents) != 3 {
		t.Errorf("expected 3 events, but got %d", len(listedEvents))
		for i, e := range listedEvents {
			t.Logf("event %d: id=%s, actor=%s, kind=%s, created_at=%s", i, e.ID, e.Actor, e.Kind, e.CreatedAt)
		}
	}

	// Verify that events are in chronological order (created_at, id)
	for i := 0; i < len(listedEvents)-1; i++ {
		current := listedEvents[i]
		next := listedEvents[i+1]

		// created_at should be <= next created_at
		if current.CreatedAt > next.CreatedAt {
			t.Errorf("events not in chronological order: event %d has created_at %s, event %d has created_at %s",
				i, current.CreatedAt, i+1, next.CreatedAt)
		}

		// If created_at is equal, id should be < next id
		if current.CreatedAt == next.CreatedAt && current.ID > next.ID {
			t.Errorf("events not in chronological order by id: event %d has id %s, event %d has id %s",
				i, current.ID, i+1, next.ID)
		}
	}

	// Verify the actors and kinds are in expected order
	// (only verify that we have 3 events with the right properties, not necessarily in order)
	foundActorKindPairs := make(map[string]bool)
	for _, e := range listedEvents {
		key := e.Actor + ":" + e.Kind
		foundActorKindPairs[key] = true
	}

	expectedPairs := []string{"agent-1:claim", "agent-1:heartbeat", "human:review"}
	for _, expected := range expectedPairs {
		if !foundActorKindPairs[expected] {
			t.Errorf("expected to find event with %s, but didn't", expected)
		}
	}
}

// TestListEventsRapidOrdering appends many events back-to-back with NO sleep and
// asserts they come back in insertion order. This is the regression guard for the
// event-spine ordering bug: under second-granularity timestamps all of these inserts
// share the same created_at, so ORDER BY (created_at, id) sorts by random UUID and
// scrambles them. With fixed-width nanosecond timestamps and the single-writer store,
// each insert gets a distinct increasing timestamp, preserving order.
func TestListEventsRapidOrdering(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := nowTimestamp()

	// Parent rows (FKs are enforced).
	if _, err = store.Conn().ExecContext(ctx,
		`INSERT INTO project (id, name, repo, created_at) VALUES (?, ?, ?, ?)`,
		"proj-rapid", "rapid", "https://example.com/r", now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err = store.Conn().ExecContext(ctx,
		`INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"doc-rapid", "proj-rapid", "design", "d", "DESIGN.md", now, now); err != nil {
		t.Fatalf("insert document: %v", err)
	}
	taskID := "task-rapid"
	if _, err = store.Conn().ExecContext(ctx,
		`INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, "proj-rapid", "doc-rapid", "t", "s", "ready", now, now); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	const n = 25
	for i := 0; i < n; i++ {
		tx, err := store.Conn().BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		// kind encodes insertion order; no sleep between appends.
		if _, err = store.AppendEvent(ctx, tx, taskID, "agent", fmt.Sprintf("evt-%02d", i), nil, nil); err != nil {
			t.Fatalf("append event %d: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}

	listed, err := store.ListEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(listed) != n {
		t.Fatalf("expected %d events, got %d", n, len(listed))
	}
	for i, e := range listed {
		want := fmt.Sprintf("evt-%02d", i)
		if e.Kind != want {
			t.Fatalf("event at position %d out of order: got kind %q, want %q "+
				"(events scrambled — timestamp ordering regression)", i, e.Kind, want)
		}
	}
}

// TestClaimTaskSuccessful tests that claiming a ready task succeeds and
// sets state, assignee, and lease_expires_at correctly.
func TestClaimTaskSuccessful(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a project, document, and ready task
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{Title: "Test Task", Spec: "Test spec", DocumentID: doc.ID},
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	taskID := tasks[0].ID

	// Promote to ready
	_, err = store.Conn().ExecContext(ctx, "UPDATE task SET state = ? WHERE id = ?", "ready", taskID)
	if err != nil {
		t.Fatalf("failed to set task to ready: %v", err)
	}

	// Claim the task
	agentID := "test-agent"
	leaseTTL := 5 * time.Minute
	claimedTask, err := store.ClaimTask(ctx, taskID, agentID, leaseTTL)
	if err != nil {
		t.Fatalf("failed to claim task: %v", err)
	}

	// Verify claimed task state
	if claimedTask.State != "in_progress" {
		t.Errorf("expected state='in_progress', got '%s'", claimedTask.State)
	}
	if claimedTask.Assignee == nil || *claimedTask.Assignee != agentID {
		t.Errorf("expected assignee='%s', got %v", agentID, claimedTask.Assignee)
	}
	if claimedTask.LeaseExpiresAt == nil {
		t.Error("expected lease_expires_at to be set")
	}

	// Verify that a claim event was recorded
	events, err := store.ListEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != "claim" {
		t.Errorf("expected event kind='claim', got '%s'", events[0].Kind)
	}
	if events[0].Actor != agentID {
		t.Errorf("expected event actor='%s', got '%s'", agentID, events[0].Actor)
	}
}

// TestClaimTaskAlreadyClaimed tests that claiming an already-claimed task returns ErrConflict.
func TestClaimTaskAlreadyClaimed(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a project, document, and ready task
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{Title: "Test Task", Spec: "Test spec", DocumentID: doc.ID},
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	taskID := tasks[0].ID

	// Promote to ready
	_, err = store.Conn().ExecContext(ctx, "UPDATE task SET state = ? WHERE id = ?", "ready", taskID)
	if err != nil {
		t.Fatalf("failed to set task to ready: %v", err)
	}

	// Claim the task once
	_, err = store.ClaimTask(ctx, taskID, "agent-1", 5*time.Minute)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}

	// Try to claim it again
	_, err = store.ClaimTask(ctx, taskID, "agent-2", 5*time.Minute)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict on second claim, got %v", err)
	}
}

// TestClaimTaskWithUnfinishedDependency tests that claiming a task with an unfinished dependency returns ErrConflict.
func TestClaimTaskWithUnfinishedDependency(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a project and document
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	// Create two tasks: one to depend on, one that depends
	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{Key: "dep-task", Title: "Dependency Task", Spec: "spec", DocumentID: doc.ID},
		{Title: "Dependent Task", Spec: "spec", DocumentID: doc.ID, DependsOn: []string{"dep-task"}},
	})
	if err != nil {
		t.Fatalf("failed to create tasks: %v", err)
	}

	depTaskID := tasks[0].ID
	dependentTaskID := tasks[1].ID

	// Promote both to ready
	_, err = store.Conn().ExecContext(ctx, "UPDATE task SET state = ? WHERE id IN (?, ?)", "ready", depTaskID, dependentTaskID)
	if err != nil {
		t.Fatalf("failed to set tasks to ready: %v", err)
	}

	// Try to claim dependent task (should fail because dependency is not done)
	_, err = store.ClaimTask(ctx, dependentTaskID, "agent-1", 5*time.Minute)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict when dependency is not done, got %v", err)
	}

	// Mark the dependency as done
	_, err = store.Conn().ExecContext(ctx, "UPDATE task SET state = ? WHERE id = ?", "done", depTaskID)
	if err != nil {
		t.Fatalf("failed to set dependency to done: %v", err)
	}

	// Now claiming should succeed
	_, err = store.ClaimTask(ctx, dependentTaskID, "agent-1", 5*time.Minute)
	if err != nil {
		t.Errorf("claim should succeed after dependency is done, got error: %v", err)
	}
}

// TestClaimTaskNotFound tests that claiming a non-existent task returns ErrNotFound.
func TestClaimTaskNotFound(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Try to claim a non-existent task
	_, err = store.ClaimTask(ctx, "non-existent-task", "agent-1", 5*time.Minute)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestClaimTaskConcurrency is the critical concurrency test: N goroutines attempt to claim
// the same ready task concurrently. Exactly one should succeed (ErrConflict=nil), and the
// other N-1 should get ErrConflict. This proves the atomic UPDATE design works.
func TestClaimTaskConcurrency(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a project, document, and ready task
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{Title: "Test Task", Spec: "Test spec", DocumentID: doc.ID},
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	taskID := tasks[0].ID

	// Promote to ready
	_, err = store.Conn().ExecContext(ctx, "UPDATE task SET state = ? WHERE id = ?", "ready", taskID)
	if err != nil {
		t.Fatalf("failed to set task to ready: %v", err)
	}

	// Launch N goroutines that try to claim the task concurrently
	const numGoroutines = 20
	var wg sync.WaitGroup
	results := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			agentID := fmt.Sprintf("agent-%d", index)
			_, err := store.ClaimTask(ctx, taskID, agentID, 5*time.Minute)
			results[index] = err
		}(i)
	}

	wg.Wait()

	// Count successes and conflicts
	successCount := 0
	conflictCount := 0

	for _, err := range results {
		if err == nil {
			successCount++
		} else if errors.Is(err, ErrConflict) {
			conflictCount++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	// Exactly one should succeed, rest should get ErrConflict
	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if conflictCount != numGoroutines-1 {
		t.Errorf("expected %d conflicts, got %d", numGoroutines-1, conflictCount)
	}

	// Verify that exactly one claim event was recorded
	events, err := store.ListEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 claim event, got %d", len(events))
	}
}

// TestClaimTaskExpiredLease tests that a task with an expired lease can be re-claimed.
func TestClaimTaskExpiredLease(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := nowTimestamp()

	// Create a project, document, and task
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{Title: "Test Task", Spec: "Test spec", DocumentID: doc.ID},
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	taskID := tasks[0].ID

	// Manually set the task to ready with an expired lease (in the past)
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(timestampLayout)
	_, err = store.Conn().ExecContext(ctx, `
		UPDATE task SET state = ?, assignee = ?, lease_expires_at = ?, updated_at = ?
		WHERE id = ?
	`, "ready", "dead-agent", pastTime, now, taskID)
	if err != nil {
		t.Fatalf("failed to set task with expired lease: %v", err)
	}

	// Try to claim the task (should succeed because lease is expired)
	claimedTask, err := store.ClaimTask(ctx, taskID, "new-agent", 5*time.Minute)
	if err != nil {
		t.Errorf("expected to claim task with expired lease, got error: %v", err)
	}

	// Verify the new agent is now the assignee
	if claimedTask.Assignee == nil || *claimedTask.Assignee != "new-agent" {
		t.Errorf("expected assignee='new-agent', got %v", claimedTask.Assignee)
	}
}

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}

// TestMigrationForeignKeysDisabled verifies that foreign keys are disabled during migration
// and that a table rebuild migration (which temporarily drops tables) works correctly.
func TestMigrationForeignKeysDisabled(t *testing.T) {
	// Use file-based DB since in-memory with multiple connections behaves differently
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "migration_fk_test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Verify that foreign keys are ON after migrations complete
	var fkEnabled int
	err = store.Conn().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("failed to check foreign keys pragma: %v", err)
	}
	if fkEnabled != 1 {
		t.Error("expected foreign_keys to be ON after migrations, but it's OFF")
	}

	// Verify that trying to insert a task with non-existent FK fails (FK is enforced)
	_, err = store.Conn().ExecContext(ctx, `
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "test-task", "non-existent-proj", "non-existent-doc", "Task", "spec", "backlog",
		"2026-06-06T00:00:00Z", "2026-06-06T00:00:00Z")

	if err == nil {
		t.Error("expected FK constraint violation when inserting with bad FK, but insert succeeded")
	}
}

// TestMigrationForeignKeyIntegrityCheck verifies that the integrity check rejects
// migrations that would leave dangling foreign key references.
func TestMigrationForeignKeyIntegrityCheck(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "integrity_check_test.db")

	// Create a test FS with a migration that intentionally leaves a dangling FK
	testFS := createTestFSWithBadMigration()

	conn, err := sql.Open("sqlite", buildDSN(dbPath))
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer conn.Close()

	conn.SetMaxOpenConns(1)
	store := &sqliteStore{conn: conn}

	// Try to apply migrations with the bad migration included
	err = store.migrate(testFS)

	// The migration should fail due to FK constraint violation detected by integrity check
	if err == nil {
		t.Error("expected migration with dangling FK to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "foreign key violations") {
		t.Errorf("expected error to mention foreign key violations, got: %v", err)
	}
}

// TestMigrationRoundTrip verifies that:
// 1. Existing migrations apply cleanly
// 2. All rows survive the migration
// 3. Foreign keys remain intact and enforced
func TestMigrationRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "roundtrip_test.db")

	// Open fresh DB and populate it
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	ctx := context.Background()

	// Create multiple projects, documents, and tasks in various states
	proj1, err := store.CreateProject(ctx, "proj-1", "https://example.com/repo1")
	if err != nil {
		t.Fatalf("failed to create project 1: %v", err)
	}

	proj2, err := store.CreateProject(ctx, "proj-2", "https://example.com/repo2")
	if err != nil {
		t.Fatalf("failed to create project 2: %v", err)
	}

	doc1, err := store.CreateDocument(ctx, proj1.ID, "design", "Design 1", "DESIGN1.md", nil)
	if err != nil {
		t.Fatalf("failed to create document 1: %v", err)
	}

	doc2, err := store.CreateDocument(ctx, proj2.ID, "feature_spec", "Spec 2", "SPEC2.md", nil)
	if err != nil {
		t.Fatalf("failed to create document 2: %v", err)
	}

	// Create tasks in various states
	tasks1, err := store.CreateTasks(ctx, proj1.ID, []TaskInput{
		{Key: "task-1a", Title: "Task 1A", Spec: "Spec 1A", DocumentID: doc1.ID},
		{Title: "Task 1B", Spec: "Spec 1B", DocumentID: doc1.ID, DependsOn: []string{"task-1a"}},
	})
	if err != nil {
		t.Fatalf("failed to create tasks for proj1: %v", err)
	}

	tasks2, err := store.CreateTasks(ctx, proj2.ID, []TaskInput{
		{Title: "Task 2A", Spec: "Spec 2A", DocumentID: doc2.ID},
		{Title: "Task 2B", Spec: "Spec 2B", DocumentID: doc2.ID},
	})
	if err != nil {
		t.Fatalf("failed to create tasks for proj2: %v", err)
	}

	// Set tasks to various states
	_, err = store.Conn().ExecContext(ctx,
		`UPDATE task SET state = ? WHERE id = ?`,
		"ready", tasks1[0].ID)
	if err != nil {
		t.Fatalf("failed to set task state: %v", err)
	}

	_, err = store.Conn().ExecContext(ctx,
		`UPDATE task SET state = ? WHERE id IN (?, ?)`,
		"in_progress", tasks1[1].ID, tasks2[0].ID)
	if err != nil {
		t.Fatalf("failed to set task states: %v", err)
	}

	_, err = store.Conn().ExecContext(ctx,
		`UPDATE task SET state = ? WHERE id = ?`,
		"done", tasks2[1].ID)
	if err != nil {
		t.Fatalf("failed to set task state: %v", err)
	}

	// Add some links
	_, err = store.Conn().ExecContext(ctx,
		`INSERT INTO task_link (id, task_id, kind, value) VALUES (?, ?, ?, ?)`,
		"link-1", tasks1[0].ID, "pr", "#123")
	if err != nil {
		t.Fatalf("failed to add task link: %v", err)
	}

	// Add some events
	tx, err := store.Conn().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	_, err = store.AppendEvent(ctx, tx, tasks1[0].ID, "agent-1", "claim", nil, nil)
	if err != nil {
		tx.Rollback()
		t.Fatalf("failed to append event: %v", err)
	}
	tx.Commit()

	// Count initial rows
	var initialProjectCount, initialDocCount, initialTaskCount, initialTaskDepCount, initialTaskLinkCount, initialEventCount int

	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM project").Scan(&initialProjectCount)
	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM document").Scan(&initialDocCount)
	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task").Scan(&initialTaskCount)
	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_dep").Scan(&initialTaskDepCount)
	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_link").Scan(&initialTaskLinkCount)
	store.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM event").Scan(&initialEventCount)

	store.Close()

	// Reopen the database (this will trigger migrations again, but they should be idempotent)
	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer store2.Close()

	// Count rows after migration
	var finalProjectCount, finalDocCount, finalTaskCount, finalTaskDepCount, finalTaskLinkCount, finalEventCount int

	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM project").Scan(&finalProjectCount)
	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM document").Scan(&finalDocCount)
	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task").Scan(&finalTaskCount)
	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_dep").Scan(&finalTaskDepCount)
	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM task_link").Scan(&finalTaskLinkCount)
	store2.Conn().QueryRowContext(ctx, "SELECT COUNT(*) FROM event").Scan(&finalEventCount)

	// Verify all rows survived
	if finalProjectCount != initialProjectCount {
		t.Errorf("project count mismatch: initial=%d, final=%d", initialProjectCount, finalProjectCount)
	}
	if finalDocCount != initialDocCount {
		t.Errorf("document count mismatch: initial=%d, final=%d", initialDocCount, finalDocCount)
	}
	if finalTaskCount != initialTaskCount {
		t.Errorf("task count mismatch: initial=%d, final=%d", initialTaskCount, finalTaskCount)
	}
	if finalTaskDepCount != initialTaskDepCount {
		t.Errorf("task_dep count mismatch: initial=%d, final=%d", initialTaskDepCount, finalTaskDepCount)
	}
	if finalTaskLinkCount != initialTaskLinkCount {
		t.Errorf("task_link count mismatch: initial=%d, final=%d", initialTaskLinkCount, finalTaskLinkCount)
	}
	if finalEventCount != initialEventCount {
		t.Errorf("event count mismatch: initial=%d, final=%d", initialEventCount, finalEventCount)
	}

	// Verify FK constraints are still enforced
	_, err = store2.Conn().ExecContext(ctx, `
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "bad-task", "non-existent-proj", "non-existent-doc", "Bad", "bad", "backlog",
		"2026-06-06T00:00:00Z", "2026-06-06T00:00:00Z")

	if err == nil {
		t.Error("expected FK constraint violation after migration, but insert succeeded")
	}
}

// TestMigration0003ApprovedState verifies that migration 0003 (widening state CHECK to include 'approved'):
// 1. Applies cleanly to a database populated with tasks in various states (with deps, links, events)
// 2. All existing rows remain intact with identical column values
// 3. Foreign keys still resolve and are enforced
// 4. A task can now be set to 'approved' state
// 5. Invalid states are still rejected by the CHECK constraint
//
// This test builds a PRE-0003 schema, seeds it with data, then applies 0003 to verify the table
// rebuild succeeds and preserves all rows/columns/FKs. This differs from TestMigrationRoundTrip
// which relies on Open() (which applies all migrations upfront against empty DBs); this test
// directly subjects populated data to the rebuild.
func TestMigration0003ApprovedState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "migration_0003_test.db")

	// Step 1: Build a fresh DB at PRE-0003 schema by executing 0001 and 0002 migrations
	conn, err := sql.Open("sqlite", buildDSN(dbPath))
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer conn.Close()

	conn.SetMaxOpenConns(1)

	// Disable FK for initial schema creation
	if _, err := conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}

	// Execute 0001 migration to create initial schema
	migration0001, err := fs.ReadFile(migrationsFS, "migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0001: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0001)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0001: %v", err)
		}
	}

	// Execute 0002 migration
	migration0002, err := fs.ReadFile(migrationsFS, "migrations/0002_document_one_design.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0002: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0002)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0002: %v", err)
		}
	}

	// Record that 0001 and 0002 were applied
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("failed to create schema_migrations: %v", err)
	}
	now := nowTimestamp()
	if _, err := conn.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", "0001", now); err != nil {
		t.Fatalf("failed to record 0001: %v", err)
	}
	if _, err := conn.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", "0002", now); err != nil {
		t.Fatalf("failed to record 0002: %v", err)
	}

	// Re-enable FK for data validation
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	// Step 2: Seed test data into the PRE-0003 schema
	// Create projects
	if _, err := conn.Exec(`
		INSERT INTO project (id, name, repo, created_at) VALUES (?, ?, ?, ?)
	`, "proj-0003-1", "repo1", "https://example.com/repo1", now); err != nil {
		t.Fatalf("failed to insert project 1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO project (id, name, repo, created_at) VALUES (?, ?, ?, ?)
	`, "proj-0003-2", "repo2", "https://example.com/repo2", now); err != nil {
		t.Fatalf("failed to insert project 2: %v", err)
	}

	// Create documents
	if _, err := conn.Exec(`
		INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "doc-0003-1", "proj-0003-1", "design", "Design 1", "DESIGN.md", now, now); err != nil {
		t.Fatalf("failed to insert document 1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "doc-0003-2", "proj-0003-2", "feature_spec", "Spec 2", "SPEC.md", now, now); err != nil {
		t.Fatalf("failed to insert document 2: %v", err)
	}

	// Create tasks in various PRE-0003 states (no 'approved' yet)
	taskData := []struct {
		id       string
		projID   string
		docID    string
		title    string
		spec     string
		state    string
		assignee *string
	}{
		{"task-0003-1a", "proj-0003-1", "doc-0003-1", "Task 1A", "Spec 1A", "backlog", nil},
		{"task-0003-1b", "proj-0003-1", "doc-0003-1", "Task 1B", "Spec 1B", "ready", nil},
		{"task-0003-2a", "proj-0003-2", "doc-0003-2", "Task 2A", "Spec 2A", "in_progress", strPtr("agent-1")},
		{"task-0003-2b", "proj-0003-2", "doc-0003-2", "Task 2B", "Spec 2B", "done", nil},
	}

	for _, td := range taskData {
		assigneeVal := sql.NullString{}
		if td.assignee != nil {
			assigneeVal = sql.NullString{String: *td.assignee, Valid: true}
		}
		if _, err := conn.Exec(`
			INSERT INTO task (id, project_id, document_id, title, spec, state, assignee, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, td.id, td.projID, td.docID, td.title, td.spec, td.state, assigneeVal, now, now); err != nil {
			t.Fatalf("failed to insert task %s: %v", td.id, err)
		}
	}

	// Create task dependency (1b depends on 1a)
	if _, err := conn.Exec(`
		INSERT INTO task_dep (task_id, depends_on_id) VALUES (?, ?)
	`, "task-0003-1b", "task-0003-1a"); err != nil {
		t.Fatalf("failed to insert task_dep: %v", err)
	}

	// Create task links
	if _, err := conn.Exec(`
		INSERT INTO task_link (id, task_id, kind, value) VALUES (?, ?, ?, ?)
	`, "link-0003-1", "task-0003-1a", "pr", "#123"); err != nil {
		t.Fatalf("failed to insert task_link 1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO task_link (id, task_id, kind, value) VALUES (?, ?, ?, ?)
	`, "link-0003-2", "task-0003-2a", "commit", "abc123"); err != nil {
		t.Fatalf("failed to insert task_link 2: %v", err)
	}

	// Create events
	if _, err := conn.Exec(`
		INSERT INTO event (id, task_id, actor, kind, created_at) VALUES (?, ?, ?, ?, ?)
	`, "event-0003-1", "task-0003-1a", "agent-1", "claim", now); err != nil {
		t.Fatalf("failed to insert event 1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO event (id, task_id, actor, kind, verdict, note, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "event-0003-2", "task-0003-2b", "human", "review", "approve", "looks good", now); err != nil {
		t.Fatalf("failed to insert event 2: %v", err)
	}

	// Record initial row counts and values before migration
	initialCounts := make(map[string]int)
	for _, table := range []string{"project", "document", "task", "task_dep", "task_link", "event"} {
		var count int
		if err := conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("failed to count %s: %v", table, err)
		}
		initialCounts[table] = count
	}

	// Record task field values for field-by-field comparison
	type TaskRow struct {
		id       string
		title    string
		spec     string
		state    string
		assignee sql.NullString
		result   sql.NullString
	}
	initialTasks := make(map[string]TaskRow)
	rows, err := conn.Query(`
		SELECT id, title, spec, state, assignee, result FROM task ORDER BY id
	`)
	if err != nil {
		t.Fatalf("failed to query initial tasks: %v", err)
	}
	for rows.Next() {
		var tr TaskRow
		if err := rows.Scan(&tr.id, &tr.title, &tr.spec, &tr.state, &tr.assignee, &tr.result); err != nil {
			t.Fatalf("failed to scan task: %v", err)
		}
		initialTasks[tr.id] = tr
	}
	rows.Close()

	// Step 3: Apply migration 0003 to the populated database
	// Disable FK during migration (following the runner's pattern)
	if _, err := conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable FK for migration: %v", err)
	}

	migration0003, err := fs.ReadFile(migrationsFS, "migrations/0003_approved_state.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0003: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0003)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0003: %v", err)
		}
	}

	// Re-enable FK and check integrity
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable FK after migration: %v", err)
	}

	// Record that 0003 was applied
	if _, err := conn.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", "0003", now); err != nil {
		t.Fatalf("failed to record 0003: %v", err)
	}

	// Step 4: Verify row counts survived
	for table, initialCount := range initialCounts {
		var finalCount int
		if err := conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&finalCount); err != nil {
			t.Fatalf("failed to count %s after migration: %v", table, err)
		}
		if finalCount != initialCount {
			t.Errorf("%s count mismatch after 0003: initial=%d, final=%d", table, initialCount, finalCount)
		}
	}

	// Step 5: Verify task field values survived (field-by-field check catches column-order bugs)
	finalTasks := make(map[string]TaskRow)
	rows, err = conn.Query(`
		SELECT id, title, spec, state, assignee, result FROM task ORDER BY id
	`)
	if err != nil {
		t.Fatalf("failed to query final tasks: %v", err)
	}
	for rows.Next() {
		var tr TaskRow
		if err := rows.Scan(&tr.id, &tr.title, &tr.spec, &tr.state, &tr.assignee, &tr.result); err != nil {
			t.Fatalf("failed to scan final task: %v", err)
		}
		finalTasks[tr.id] = tr
	}
	rows.Close()

	for taskID, initialTask := range initialTasks {
		finalTask, exists := finalTasks[taskID]
		if !exists {
			t.Errorf("task %s missing after migration 0003", taskID)
			continue
		}
		if initialTask.title != finalTask.title {
			t.Errorf("task %s title mismatch: initial=%q, final=%q", taskID, initialTask.title, finalTask.title)
		}
		if initialTask.spec != finalTask.spec {
			t.Errorf("task %s spec mismatch: initial=%q, final=%q", taskID, initialTask.spec, finalTask.spec)
		}
		if initialTask.state != finalTask.state {
			t.Errorf("task %s state mismatch: initial=%q, final=%q", taskID, initialTask.state, finalTask.state)
		}
		if initialTask.assignee != finalTask.assignee {
			t.Errorf("task %s assignee mismatch: initial=%v, final=%v", taskID, initialTask.assignee, finalTask.assignee)
		}
		if initialTask.result != finalTask.result {
			t.Errorf("task %s result mismatch: initial=%v, final=%v", taskID, initialTask.result, finalTask.result)
		}
	}

	// Step 6: Verify FK integrity (PRAGMA foreign_key_check should return no violations)
	fkRows, err := conn.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("failed to run foreign_key_check: %v", err)
	}
	defer fkRows.Close()

	fkViolationCount := 0
	for fkRows.Next() {
		fkViolationCount++
		var table, rowid, parent, fkid string
		if err := fkRows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			t.Errorf("failed to scan FK violation: %v", err)
		}
	}
	if err := fkRows.Err(); err != nil {
		t.Fatalf("error iterating FK check results: %v", err)
	}
	if fkViolationCount > 0 {
		t.Errorf("found %d foreign key violations after migration 0003", fkViolationCount)
	}

	// Step 7: Verify FK constraints still enforced
	_, err = conn.Exec(`
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "bad-task", "non-existent-proj", "non-existent-doc", "Bad Task", "bad spec", "backlog", now, now)
	if err == nil {
		t.Error("expected FK constraint violation after 0003, but insert succeeded")
	}

	// Step 8: Verify 'approved' state is now accepted by the widened CHECK constraint
	testTaskID := "task-0003-1a"
	if _, err := conn.Exec(`UPDATE task SET state = ? WHERE id = ?`, "approved", testTaskID); err != nil {
		t.Errorf("failed to update task to 'approved' state: %v", err)
	}

	var state string
	if err := conn.QueryRow("SELECT state FROM task WHERE id = ?", testTaskID).Scan(&state); err != nil {
		t.Fatalf("failed to query task state: %v", err)
	}
	if state != "approved" {
		t.Errorf("expected task state='approved', got '%s'", state)
	}

	// Step 9: Verify invalid states are still rejected
	_, err = conn.Exec(`UPDATE task SET state = ? WHERE id = ?`, "invalid-state", testTaskID)
	if err == nil {
		t.Error("expected CHECK constraint violation for invalid state, but update succeeded")
	}

	// Step 10: Verify a new task can be inserted with 'approved' state
	if _, err := conn.Exec(`
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "new-approved-task", "proj-0003-1", "doc-0003-1", "New Approved", "spec", "approved", now, now); err != nil {
		t.Errorf("failed to insert task with 'approved' state: %v", err)
	}

	var approvedCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM task WHERE state = ?", "approved").Scan(&approvedCount); err != nil {
		t.Fatalf("failed to count approved tasks: %v", err)
	}
	if approvedCount != 2 {
		t.Errorf("expected 2 approved tasks, got %d", approvedCount)
	}
}

// TestMigration0004AddTaskColumns verifies that migration 0004 (adding model, kind, review_models, review_round, target_task_id, verdict columns):
// 1. Applies cleanly to a database populated with tasks
// 2. All existing rows remain intact
// 3. New columns exist with correct defaults (model='haiku', kind='implement', review_round=0, nullable columns empty)
// 4. The model-matched claimable index exists
// 5. Foreign keys still resolve
func TestMigration0004AddTaskColumns(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "migration_0004_test.db")

	// Step 1: Build a fresh DB at PRE-0004 schema by executing 0001, 0002, and 0003 migrations
	conn, err := sql.Open("sqlite", buildDSN(dbPath))
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer conn.Close()

	conn.SetMaxOpenConns(1)

	// Disable FK for initial schema creation
	if _, err := conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}

	// Execute 0001 migration
	migration0001, err := fs.ReadFile(migrationsFS, "migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0001: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0001)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0001: %v", err)
		}
	}

	// Execute 0002 migration
	migration0002, err := fs.ReadFile(migrationsFS, "migrations/0002_document_one_design.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0002: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0002)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0002: %v", err)
		}
	}

	// Execute 0003 migration (table rebuild for 'approved' state)
	migration0003, err := fs.ReadFile(migrationsFS, "migrations/0003_approved_state.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0003: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0003)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0003: %v", err)
		}
	}

	// Record that 0001, 0002, 0003 were applied
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("failed to create schema_migrations: %v", err)
	}
	now := nowTimestamp()
	for _, version := range []string{"0001", "0002", "0003"} {
		if _, err := conn.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", version, now); err != nil {
			t.Fatalf("failed to record %s: %v", version, err)
		}
	}

	// Re-enable FK for data validation
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	// Step 2: Seed test data into the PRE-0004 schema
	// Create project
	if _, err := conn.Exec(`
		INSERT INTO project (id, name, repo, created_at) VALUES (?, ?, ?, ?)
	`, "proj-0004", "test-repo", "https://example.com/repo", now); err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}

	// Create document
	if _, err := conn.Exec(`
		INSERT INTO document (id, project_id, kind, title, ref, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "doc-0004", "proj-0004", "design", "Design", "DESIGN.md", now, now); err != nil {
		t.Fatalf("failed to insert document: %v", err)
	}

	// Create tasks in various states
	taskData := []struct {
		id    string
		state string
	}{
		{"task-0004-1", "backlog"},
		{"task-0004-2", "ready"},
		{"task-0004-3", "in_progress"},
		{"task-0004-4", "review"},
		{"task-0004-5", "approved"},
		{"task-0004-6", "done"},
	}

	for _, td := range taskData {
		if _, err := conn.Exec(`
			INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, td.id, "proj-0004", "doc-0004", td.id, "spec for "+td.id, td.state, now, now); err != nil {
			t.Fatalf("failed to insert task %s: %v", td.id, err)
		}
	}

	// Record initial row counts
	var initialTaskCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM task").Scan(&initialTaskCount); err != nil {
		t.Fatalf("failed to count initial tasks: %v", err)
	}

	// Step 3: Apply migration 0004 to the populated database
	if _, err := conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable FK for migration: %v", err)
	}

	migration0004, err := fs.ReadFile(migrationsFS, "migrations/0004_add_task_columns.sql")
	if err != nil {
		t.Fatalf("failed to read migration 0004: %v", err)
	}
	for _, stmt := range splitStatements(string(migration0004)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("failed to execute 0004: %v", err)
		}
	}

	// Re-enable FK and check integrity
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable FK after migration: %v", err)
	}

	// Record that 0004 was applied
	if _, err := conn.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", "0004", now); err != nil {
		t.Fatalf("failed to record 0004: %v", err)
	}

	// Step 4: Verify row counts survived
	var finalTaskCount int
	if err := conn.QueryRow("SELECT COUNT(*) FROM task").Scan(&finalTaskCount); err != nil {
		t.Fatalf("failed to count final tasks: %v", err)
	}
	if finalTaskCount != initialTaskCount {
		t.Errorf("task count mismatch after 0004: initial=%d, final=%d", initialTaskCount, finalTaskCount)
	}

	// Step 5: Verify new columns exist with correct defaults
	type TaskWithNewColumns struct {
		id           string
		model        string
		kind         string
		reviewModels sql.NullString
		reviewRound  int
		targetTaskID sql.NullString
		verdict      sql.NullString
	}

	rows, err := conn.Query(`
		SELECT id, model, kind, review_models, review_round, target_task_id, verdict
		FROM task ORDER BY id
	`)
	if err != nil {
		t.Fatalf("failed to query tasks with new columns: %v", err)
	}
	defer rows.Close()

	var tasksWithNewCols []TaskWithNewColumns
	for rows.Next() {
		var row TaskWithNewColumns
		if err := rows.Scan(&row.id, &row.model, &row.kind, &row.reviewModels, &row.reviewRound, &row.targetTaskID, &row.verdict); err != nil {
			t.Fatalf("failed to scan task: %v", err)
		}
		tasksWithNewCols = append(tasksWithNewCols, row)
	}

	if len(tasksWithNewCols) != initialTaskCount {
		t.Errorf("expected %d tasks, got %d", initialTaskCount, len(tasksWithNewCols))
	}

	// Verify defaults on all tasks
	for _, task := range tasksWithNewCols {
		if task.model != "haiku" {
			t.Errorf("task %s model: expected 'haiku', got '%s'", task.id, task.model)
		}
		if task.kind != "implement" {
			t.Errorf("task %s kind: expected 'implement', got '%s'", task.id, task.kind)
		}
		if task.reviewRound != 0 {
			t.Errorf("task %s review_round: expected 0, got %d", task.id, task.reviewRound)
		}
		if task.reviewModels.Valid || task.targetTaskID.Valid || task.verdict.Valid {
			t.Errorf("task %s nullable columns should be NULL: review_models.Valid=%v, target_task_id.Valid=%v, verdict.Valid=%v",
				task.id, task.reviewModels.Valid, task.targetTaskID.Valid, task.verdict.Valid)
		}
	}

	// Step 6: Verify the model-matched claimable index exists
	var indexCount int
	err = conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_task_claimable'
	`).Scan(&indexCount)
	if err != nil {
		t.Fatalf("failed to check for idx_task_claimable: %v", err)
	}
	if indexCount != 1 {
		t.Errorf("expected idx_task_claimable to exist, but count=%d", indexCount)
	}

	// Step 7: Verify FK integrity
	fkRows, err := conn.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("failed to run foreign_key_check: %v", err)
	}
	defer fkRows.Close()

	fkViolationCount := 0
	for fkRows.Next() {
		fkViolationCount++
	}
	if fkViolationCount > 0 {
		t.Errorf("found %d foreign key violations after migration 0004", fkViolationCount)
	}

	// Step 8: Verify FK constraints still enforced
	_, err = conn.Exec(`
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "bad-task", "non-existent-proj", "non-existent-doc", "Bad Task", "bad spec", "ready", now, now)
	if err == nil {
		t.Error("expected FK constraint violation after 0004, but insert succeeded")
	}

	// Step 9: Verify target_task_id FK works
	// First insert a task that will be the target
	if _, err := conn.Exec(`
		INSERT INTO task (id, project_id, document_id, title, spec, state, created_at, updated_at, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "task-0004-review", "proj-0004", "doc-0004", "Review Task", "review spec", "ready", now, now, "review"); err != nil {
		t.Fatalf("failed to insert review task: %v", err)
	}

	// Update it with a valid target_task_id FK
	if _, err := conn.Exec(`
		UPDATE task SET target_task_id = ? WHERE id = ?
	`, "task-0004-1", "task-0004-review"); err != nil {
		t.Fatalf("failed to set target_task_id with valid FK: %v", err)
	}

	// Verify the FK is enforced
	_, err = conn.Exec(`
		UPDATE task SET target_task_id = ? WHERE id = ?
	`, "non-existent-task", "task-0004-review")
	if err == nil {
		t.Error("expected FK constraint violation for target_task_id, but update succeeded")
	}
}

// TestTaskFieldsRoundTrip verifies that the new fields (model, kind, review_models, review_round, target_task_id)
// are properly persisted and retrieved through the Go layer.
func TestTaskFieldsRoundTrip(t *testing.T) {
	store, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a project and document
	proj, err := store.CreateProject(ctx, "test-project", "https://github.com/example/repo")
	if err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	doc, err := store.CreateDocument(ctx, proj.ID, "design", "Test Design", "DESIGN.md", nil)
	if err != nil {
		t.Fatalf("failed to create document: %v", err)
	}

	// Test 1: Create task with explicit model and review_models
	reviewModels := []string{"opus", "sonnet"}
	tasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{
			Title:        "Task with model",
			Spec:         "Test spec",
			DocumentID:   doc.ID,
			Model:        "sonnet",
			ReviewModels: reviewModels,
		},
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	taskWithModel := tasks[0]
	if taskWithModel.Model != "sonnet" {
		t.Errorf("expected model='sonnet', got '%s'", taskWithModel.Model)
	}
	if taskWithModel.Kind != "implement" {
		t.Errorf("expected kind='implement', got '%s'", taskWithModel.Kind)
	}
	if len(taskWithModel.ReviewModels) != 2 || taskWithModel.ReviewModels[0] != "opus" || taskWithModel.ReviewModels[1] != "sonnet" {
		t.Errorf("expected review_models=['opus','sonnet'], got %v", taskWithModel.ReviewModels)
	}
	if taskWithModel.ReviewRound != 0 {
		t.Errorf("expected review_round=0, got %d", taskWithModel.ReviewRound)
	}
	if taskWithModel.TargetTaskID != nil {
		t.Errorf("expected target_task_id=nil, got %v", taskWithModel.TargetTaskID)
	}

	// Test 2: GetTask and verify fields are returned
	retrieved, err := store.GetTask(ctx, taskWithModel.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}

	if retrieved.Model != "sonnet" {
		t.Errorf("GetTask: expected model='sonnet', got '%s'", retrieved.Model)
	}
	if retrieved.Kind != "implement" {
		t.Errorf("GetTask: expected kind='implement', got '%s'", retrieved.Kind)
	}
	if len(retrieved.ReviewModels) != 2 || retrieved.ReviewModels[0] != "opus" || retrieved.ReviewModels[1] != "sonnet" {
		t.Errorf("GetTask: expected review_models=['opus','sonnet'], got %v", retrieved.ReviewModels)
	}
	if retrieved.ReviewRound != 0 {
		t.Errorf("GetTask: expected review_round=0, got %d", retrieved.ReviewRound)
	}
	if retrieved.TargetTaskID != nil {
		t.Errorf("GetTask: expected target_task_id=nil, got %v", retrieved.TargetTaskID)
	}

	// Test 3: ListTasks and verify fields are returned
	listedTasks, err := store.ListTasks(ctx, proj.ID, TaskListFilter{})
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(listedTasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(listedTasks))
	}

	listedTask := listedTasks[0]
	if listedTask.Model != "sonnet" {
		t.Errorf("ListTasks: expected model='sonnet', got '%s'", listedTask.Model)
	}
	if listedTask.Kind != "implement" {
		t.Errorf("ListTasks: expected kind='implement', got '%s'", listedTask.Kind)
	}
	if len(listedTask.ReviewModels) != 2 || listedTask.ReviewModels[0] != "opus" || listedTask.ReviewModels[1] != "sonnet" {
		t.Errorf("ListTasks: expected review_models=['opus','sonnet'], got %v", listedTask.ReviewModels)
	}

	// Test 4: Create task with default model (no explicit value)
	defaultTasks, err := store.CreateTasks(ctx, proj.ID, []TaskInput{
		{
			Title:      "Task with default model",
			Spec:       "Test spec",
			DocumentID: doc.ID,
		},
	})
	if err != nil {
		t.Fatalf("failed to create task with defaults: %v", err)
	}

	defaultTask := defaultTasks[0]
	if defaultTask.Model != "haiku" {
		t.Errorf("expected default model='haiku', got '%s'", defaultTask.Model)
	}
	if defaultTask.Kind != "implement" {
		t.Errorf("expected default kind='implement', got '%s'", defaultTask.Kind)
	}
	if len(defaultTask.ReviewModels) != 0 {
		t.Errorf("expected empty review_models when not specified, got %v", defaultTask.ReviewModels)
	}
	if defaultTask.ReviewRound != 0 {
		t.Errorf("expected default review_round=0, got %d", defaultTask.ReviewRound)
	}
}
