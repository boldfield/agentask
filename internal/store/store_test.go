package store

import (
	"testing"
)

// TestMigrations verifies that migrations can be applied to a fresh database
// and that re-applying is idempotent.
func TestMigrations(t *testing.T) {
	// Use in-memory database for testing
	db, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

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
		err := db.Conn().QueryRow(
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
		err := db.Conn().QueryRow(
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
	err = db.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("expected 1 migration to be recorded, but got %d", migrationCount)
	}

	// Verify idempotency: re-open the same database and it should work
	db2, err := Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to re-open database: %v", err)
	}
	defer db2.Close()

	// Verify that we still have exactly 1 migration recorded (idempotency)
	err = db2.Conn().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations after re-open: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("expected 1 migration after re-open (idempotency), but got %d", migrationCount)
	}
}
