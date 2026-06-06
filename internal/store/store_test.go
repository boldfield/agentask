package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

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
	if migrationCount != 2 {
		t.Errorf("expected 2 migrations to be recorded, but got %d", migrationCount)
	}

	// Verify idempotency: re-open the same database and it should work
	store2, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer store2.Close()

	// Verify that we still have exactly 2 migrations recorded (idempotency)
	err = store2.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations after re-open: %v", err)
	}
	if migrationCount != 2 {
		t.Errorf("expected 2 migrations after re-open (idempotency), but got %d", migrationCount)
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
	if migrationCount != 2 {
		t.Errorf("expected 2 migrations after second open, but got %d", migrationCount)
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
