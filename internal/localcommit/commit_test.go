package localcommit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTempRepo(t *testing.T) string {
	tmpDir := t.TempDir()

	// Initialize git repo with initial commit on main
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	// Create a fake origin/main
	cmd := exec.Command("git", "-C", tmpDir, "update-ref", "refs/remotes/origin/main", "HEAD")
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup failed to create origin/main: %v", err)
	}

	return tmpDir
}

func TestCommitAll_Success(t *testing.T) {
	tmpDir := setupTempRepo(t)

	// Create a file to commit
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Commit the file
	sha, err := CommitAll(tmpDir, "add test file")
	if err != nil {
		t.Errorf("CommitAll() error = %v", err)
	}

	// Verify SHA is 40 characters (SHA-1 hex)
	if len(sha) != 40 {
		t.Errorf("CommitAll() returned SHA of length %d, want 40", len(sha))
	}

	// Verify the file is in the tree
	cmd := exec.Command("git", "-C", tmpDir, "ls-tree", "-r", "HEAD")
	var output []byte
	var err2 error
	if output, err2 = cmd.Output(); err2 != nil {
		t.Fatalf("failed to check tree: %v", err2)
	}

	if !contains(string(output), "test.txt") {
		t.Errorf("test.txt not found in tree: %s", string(output))
	}
}

func TestCommitAll_EmptyTree(t *testing.T) {
	tmpDir := setupTempRepo(t)

	// Try to commit with no changes
	_, err := CommitAll(tmpDir, "nothing to commit")
	if err == nil {
		t.Errorf("CommitAll() expected error for empty tree, got nil")
	}
}

func TestAmendAll_ChangeMessage(t *testing.T) {
	tmpDir := setupTempRepo(t)

	// Create and commit a file
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	sha1, err := CommitAll(tmpDir, "first message")
	if err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	// Amend the message (no new changes)
	sha2, err := AmendAll(tmpDir, "second message")
	if err != nil {
		t.Errorf("AmendAll() error = %v", err)
	}

	// SHAs should be different
	if sha1 == sha2 {
		t.Errorf("AmendAll() did not change SHA: %s == %s", sha1, sha2)
	}

	// Verify we still have only one commit on top of base
	cmd := exec.Command("git", "-C", tmpDir, "rev-list", "--count", "origin/main..HEAD")
	var output []byte
	var err2 error
	if output, err2 = cmd.Output(); err2 != nil {
		t.Fatalf("failed to count commits: %v", err2)
	}

	if string(output) != "1\n" {
		t.Errorf("AmendAll() expected 1 commit on top of base, got %s", string(output))
	}

	// Verify the new message is present
	cmd = exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%s")
	if output, err2 = cmd.Output(); err2 != nil {
		t.Fatalf("failed to get commit message: %v", err2)
	}

	if string(output) != "second message\n" {
		t.Errorf("AmendAll() expected message 'second message', got %s", string(output))
	}
}

func TestAmendAll_AddChanges(t *testing.T) {
	tmpDir := setupTempRepo(t)

	// Create and commit a file
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("v1"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	sha1, err := CommitAll(tmpDir, "first commit")
	if err != nil {
		t.Fatalf("CommitAll() error = %v", err)
	}

	// Modify the file
	if err := os.WriteFile(filePath, []byte("v2"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Amend with the new changes
	sha2, err := AmendAll(tmpDir, "amended with changes")
	if err != nil {
		t.Errorf("AmendAll() error = %v", err)
	}

	// SHAs should be different
	if sha1 == sha2 {
		t.Errorf("AmendAll() did not change SHA: %s == %s", sha1, sha2)
	}

	// Verify we still have only one commit on top of base
	cmd := exec.Command("git", "-C", tmpDir, "rev-list", "--count", "origin/main..HEAD")
	var output []byte
	var err2 error
	if output, err2 = cmd.Output(); err2 != nil {
		t.Fatalf("failed to count commits: %v", err2)
	}

	if string(output) != "1\n" {
		t.Errorf("AmendAll() expected 1 commit on top of base, got %s", string(output))
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || (len(s) > len(substr) && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
