package store

import (
	"path/filepath"
	"testing"
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

	// Verify that schema_migrations table has the migration recorded
	var migrationCount int
	err = store.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("expected 1 migration to be recorded, but got %d", migrationCount)
	}

	// Verify idempotency: re-open the same database and it should work
	store2, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer store2.Close()

	// Verify that we still have exactly 1 migration recorded (idempotency)
	err = store2.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations after re-open: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("expected 1 migration after re-open (idempotency), but got %d", migrationCount)
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
	if migrationCount != 1 {
		t.Errorf("expected 1 migration after second open, but got %d", migrationCount)
	}
}
