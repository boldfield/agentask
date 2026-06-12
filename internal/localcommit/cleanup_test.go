package localcommit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func createDurableWorktreeHomeForTest(t *testing.T) string {
	// Create a durable temp dir in the home directory instead of /tmp or /var/folders
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home directory: %v", err)
	}

	// Create a test-specific directory with timestamp to avoid conflicts
	timestamp := time.Now().UnixNano()
	wtHomeDir := filepath.Join(homeDir, ".agentask-test", t.Name()+"-"+fmt.Sprintf("%d", timestamp))
	if err := os.MkdirAll(wtHomeDir, 0755); err != nil {
		t.Fatalf("failed to create durable worktree home: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(wtHomeDir)
	})

	return wtHomeDir
}

func TestCleanupAbandon(t *testing.T) {
	t.Run("removes worktree and wip branch", func(t *testing.T) {
		// Create a temporary git repo
		tmpDir := t.TempDir()

		// Initialize git repo
		cmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@example.com"},
			{"git", "config", "user.name", "Test User"},
			{"git", "commit", "--allow-empty", "-m", "initial"},
			{"git", "branch", "work"},
		}

		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		// Create worktree home directory
		worktreeHome := createDurableWorktreeHomeForTest(t)
		t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

		iid := "task-123"
		worktreePath := filepath.Join(worktreeHome, iid)

		// Create a worktree at worktreeHome/iid (using the work branch to avoid main being in use)
		cmd := exec.Command("git", "-C", tmpDir, "worktree", "add", worktreePath, "work")
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to add worktree: %v", err)
		}

		// Create a wip branch
		cmd = exec.Command("git", "-C", tmpDir, "branch", "wip/"+iid)
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to create wip branch: %v", err)
		}

		// Verify worktree exists
		if _, err := os.Stat(worktreePath); err != nil {
			t.Fatalf("worktree should exist before cleanup: %v", err)
		}

		// Verify wip branch exists
		cmd = exec.Command("git", "-C", tmpDir, "rev-parse", "--verify", "wip/"+iid)
		if err := cmd.Run(); err != nil {
			t.Fatalf("wip branch should exist before cleanup: %v", err)
		}

		// Call CleanupAbandon
		if err := CleanupAbandon(tmpDir, iid); err != nil {
			t.Errorf("CleanupAbandon() error = %v", err)
		}

		// Verify worktree is gone
		if _, err := os.Stat(worktreePath); err == nil {
			t.Errorf("worktree should not exist after cleanup")
		}

		// Verify wip branch is gone
		cmd = exec.Command("git", "-C", tmpDir, "rev-parse", "--verify", "wip/"+iid)
		if err := cmd.Run(); err == nil {
			t.Errorf("wip branch should not exist after cleanup")
		}
	})

	t.Run("does not touch wi/slug branch", func(t *testing.T) {
		// Create a temporary git repo
		tmpDir := t.TempDir()

		// Initialize git repo
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

		// Create worktree home directory
		worktreeHome := createDurableWorktreeHomeForTest(t)
		t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

		iid := "task-456"
		slug := "my-feature"

		// Create a wip branch
		cmd := exec.Command("git", "-C", tmpDir, "branch", "wip/"+iid)
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to create wip branch: %v", err)
		}

		// Create a wi/slug branch that should be untouched
		cmd = exec.Command("git", "-C", tmpDir, "branch", "wi/"+slug)
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to create wi/slug branch: %v", err)
		}

		// Call CleanupAbandon
		if err := CleanupAbandon(tmpDir, iid); err != nil {
			t.Errorf("CleanupAbandon() error = %v", err)
		}

		// Verify wi/slug branch still exists
		cmd = exec.Command("git", "-C", tmpDir, "rev-parse", "--verify", "wi/"+slug)
		if err := cmd.Run(); err != nil {
			t.Errorf("wi/slug branch should still exist after cleanup")
		}
	})

	t.Run("second call is a no-op", func(t *testing.T) {
		// Create a temporary git repo
		tmpDir := t.TempDir()

		// Initialize git repo
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

		// Create worktree home directory
		worktreeHome := createDurableWorktreeHomeForTest(t)
		t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

		iid := "task-789"

		// Call CleanupAbandon when nothing exists (should not error)
		if err := CleanupAbandon(tmpDir, iid); err != nil {
			t.Errorf("first CleanupAbandon() error = %v", err)
		}

		// Call again (should still not error)
		if err := CleanupAbandon(tmpDir, iid); err != nil {
			t.Errorf("second CleanupAbandon() error = %v", err)
		}
	})
}
