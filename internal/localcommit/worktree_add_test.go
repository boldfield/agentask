package localcommit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func createDurableWorktreeHome(t *testing.T) string {
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

func setupRepoForWorktree(t *testing.T) string {
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

func TestAddWorktree_Fresh(t *testing.T) {
	repoDir := setupRepoForWorktree(t)
	wtHome := createDurableWorktreeHome(t)
	t.Setenv("AGENTASK_WORKTREE_HOME", wtHome)

	iid := "task-123"
	wtPath, err := AddWorktree(repoDir, iid, "origin/main")
	if err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	expectedPath := filepath.Join(wtHome, iid)
	if wtPath != expectedPath {
		t.Errorf("AddWorktree() returned %q, want %q", wtPath, expectedPath)
	}

	// Verify worktree dir exists
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree directory does not exist: %v", err)
	}

	// Verify HEAD is on wip/<iid>
	cmd := exec.Command("git", "-C", wtPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	branchName := string(output)[:len(string(output))-1] // Remove newline
	expectedBranch := "wip/" + iid
	if branchName != expectedBranch {
		t.Errorf("HEAD is on %q, want %q", branchName, expectedBranch)
	}

	// Verify clean status
	cmd = exec.Command("git", "-C", wtPath, "status", "--porcelain")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if len(output) > 0 {
		t.Errorf("worktree has uncommitted changes: %s", string(output))
	}
}

func TestAddWorktree_Idempotent(t *testing.T) {
	repoDir := setupRepoForWorktree(t)
	wtHome := createDurableWorktreeHome(t)
	t.Setenv("AGENTASK_WORKTREE_HOME", wtHome)

	iid := "task-456"

	// First call
	wtPath1, err := AddWorktree(repoDir, iid, "origin/main")
	if err != nil {
		t.Fatalf("first AddWorktree() error = %v", err)
	}

	// Second call with same iid
	wtPath2, err := AddWorktree(repoDir, iid, "origin/main")
	if err != nil {
		t.Fatalf("second AddWorktree() error = %v", err)
	}

	if wtPath1 != wtPath2 {
		t.Errorf("second call returned different path: %q vs %q", wtPath1, wtPath2)
	}
}

func TestAddWorktree_WithMRBranch(t *testing.T) {
	repoDir := setupRepoForWorktree(t)
	wtHome := createDurableWorktreeHome(t)
	t.Setenv("AGENTASK_WORKTREE_HOME", wtHome)

	// Create a wi/<slug> branch
	slug := "my-feature"
	cmd := exec.Command("git", "-C", repoDir, "checkout", "-b", "wi/"+slug)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create wi/%s branch: %v", slug, err)
	}

	// Create a commit on the new branch
	cmd = exec.Command("git", "-C", repoDir, "commit", "--allow-empty", "-m", "feature work")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create commit: %v", err)
	}

	// Return to main
	cmd = exec.Command("git", "-C", repoDir, "checkout", "main")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to checkout main: %v", err)
	}

	// Create worktree with wi/<slug> as tip
	iid := "task-789"
	wtPath, err := AddWorktree(repoDir, iid, "wi/"+slug)
	if err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	expectedPath := filepath.Join(wtHome, iid)
	if wtPath != expectedPath {
		t.Errorf("AddWorktree() returned %q, want %q", wtPath, expectedPath)
	}

	// Verify worktree exists and is on correct branch
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree directory does not exist: %v", err)
	}

	cmd = exec.Command("git", "-C", wtPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	branchName := string(output)[:len(string(output))-1]
	expectedBranch := "wip/" + iid
	if branchName != expectedBranch {
		t.Errorf("HEAD is on %q, want %q", branchName, expectedBranch)
	}
}

func TestAddWorktree_WithOriginMain(t *testing.T) {
	repoDir := setupRepoForWorktree(t)
	wtHome := createDurableWorktreeHome(t)
	t.Setenv("AGENTASK_WORKTREE_HOME", wtHome)

	iid := "task-origin-main"
	wtPath, err := AddWorktree(repoDir, iid, "origin/main")
	if err != nil {
		t.Fatalf("AddWorktree() error = %v", err)
	}

	expectedPath := filepath.Join(wtHome, iid)
	if wtPath != expectedPath {
		t.Errorf("AddWorktree() returned %q, want %q", wtPath, expectedPath)
	}

	// Verify worktree exists
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree directory does not exist: %v", err)
	}

	// Verify HEAD is on wip/<iid>
	cmd := exec.Command("git", "-C", wtPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}

	branchName := string(output)[:len(string(output))-1]
	expectedBranch := "wip/" + iid
	if branchName != expectedBranch {
		t.Errorf("HEAD is on %q, want %q", branchName, expectedBranch)
	}
}
