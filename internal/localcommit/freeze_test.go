package localcommit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCmd(t *testing.T, dir string, args ...string) string {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v (output: %s)", err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func getBranch(t *testing.T, dir, branchName string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", branchName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func branchExists(t *testing.T, dir, branchName string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", branchName)
	return cmd.Run() == nil
}

func TestFreeze_FirstFreeze(t *testing.T) {
	// Setup: create main repo with origin/main and wip/<iid> branch
	repoDir := t.TempDir()
	// Use current working directory for worktree home (validation rejects /tmp and /var/folders)
	cwd, _ := os.Getwd()
	worktreeHome := filepath.Join(cwd, "test-worktree-home")
	os.MkdirAll(worktreeHome, 0755)
	t.Cleanup(func() { os.RemoveAll(worktreeHome) })
	t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

	runCmd(t, repoDir, "git", "init")
	runCmd(t, repoDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoDir, "git", "config", "user.name", "Test User")

	// Create initial commit on main
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "initial")
	mainCommit := runCmd(t, repoDir, "git", "rev-parse", "HEAD")

	// Create fake origin/main
	runCmd(t, repoDir, "git", "branch", "-f", "origin/main", "main")

	// Create wip/<iid> as a new commit
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "wip commit")
	wipCommit := runCmd(t, repoDir, "git", "rev-parse", "HEAD")

	// Create wip/task-123 branch at this commit
	runCmd(t, repoDir, "git", "branch", "-f", "wip/task-123", wipCommit)

	// Before freeze: wi/my-feature should not exist
	if branchExists(t, repoDir, "wi/my-feature") {
		t.Fatal("wi/my-feature should not exist yet")
	}

	// Call Freeze
	slug := "my-feature"
	iid := "task-123"
	err := Freeze(repoDir, slug, iid)
	if err != nil {
		t.Fatalf("Freeze failed: %v", err)
	}

	// After freeze: wi/my-feature should point to wip/task-123 commit
	if !branchExists(t, repoDir, "wi/my-feature") {
		t.Fatal("wi/my-feature should exist after Freeze")
	}

	mrCommit := getBranch(t, repoDir, "wi/my-feature")
	if mrCommit != wipCommit {
		t.Errorf("wi/my-feature points to %s, want %s", mrCommit, wipCommit)
	}

	// After freeze: wip/task-123 should be deleted
	if branchExists(t, repoDir, "wip/task-123") {
		t.Fatal("wip/task-123 should be deleted after Freeze")
	}

	_ = mainCommit // Use it to avoid unused warning
}

func TestFreeze_FFAdvance(t *testing.T) {
	// Setup: wi/<slug> exists as ancestor of wip/<iid>
	repoDir := t.TempDir()
	// Use current working directory for worktree home (validation rejects /tmp and /var/folders)
	cwd, _ := os.Getwd()
	worktreeHome := filepath.Join(cwd, "test-worktree-home-ffadvance")
	os.MkdirAll(worktreeHome, 0755)
	t.Cleanup(func() { os.RemoveAll(worktreeHome) })
	t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

	runCmd(t, repoDir, "git", "init")
	runCmd(t, repoDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoDir, "git", "config", "user.name", "Test User")

	// Create initial commit on main
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "initial")
	initialCommit := runCmd(t, repoDir, "git", "rev-parse", "HEAD")

	// Create fake origin/main
	runCmd(t, repoDir, "git", "branch", "-f", "origin/main", "main")

	// Create wi/my-feature at initial commit
	runCmd(t, repoDir, "git", "branch", "-f", "wi/my-feature", initialCommit)

	// Create wip/task-123 as a new commit
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "wip commit")
	wipCommit := runCmd(t, repoDir, "git", "rev-parse", "HEAD")

	// Create wip/task-123 branch at this commit
	runCmd(t, repoDir, "git", "branch", "-f", "wip/task-123", wipCommit)

	// Verify wi/my-feature is ancestor of wip/task-123
	cmd := exec.Command("git", "-C", repoDir, "merge-base", "--is-ancestor", "wi/my-feature", "wip/task-123")
	if err := cmd.Run(); err != nil {
		t.Fatal("wi/my-feature should be ancestor of wip/task-123")
	}

	// Call Freeze
	slug := "my-feature"
	iid := "task-123"
	err := Freeze(repoDir, slug, iid)
	if err != nil {
		t.Fatalf("Freeze failed: %v", err)
	}

	// After freeze: wi/my-feature should advance to wip/task-123 commit
	mrCommit := getBranch(t, repoDir, "wi/my-feature")
	if mrCommit != wipCommit {
		t.Errorf("wi/my-feature points to %s, want %s", mrCommit, wipCommit)
	}

	// After freeze: wip/task-123 should be deleted
	if branchExists(t, repoDir, "wip/task-123") {
		t.Fatal("wip/task-123 should be deleted after Freeze")
	}
}

func TestFreeze_Footgun(t *testing.T) {
	// Setup: wi/<slug> is checked out in a second worktree
	mainDir := t.TempDir()
	// Use current working directory for worktree home (validation rejects /tmp and /var/folders)
	cwd, _ := os.Getwd()
	worktreeHome := filepath.Join(cwd, "test-worktree-home-footgun")
	os.MkdirAll(worktreeHome, 0755)
	t.Cleanup(func() { os.RemoveAll(worktreeHome) })
	t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

	runCmd(t, mainDir, "git", "init")
	runCmd(t, mainDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, mainDir, "git", "config", "user.name", "Test User")

	// Create initial commit
	runCmd(t, mainDir, "git", "commit", "--allow-empty", "-m", "initial")

	// Create fake origin/main
	runCmd(t, mainDir, "git", "branch", "-f", "origin/main", "main")

	// Create wi/my-feature branch at initial commit
	initialCommit := runCmd(t, mainDir, "git", "rev-parse", "HEAD")
	runCmd(t, mainDir, "git", "branch", "-f", "wi/my-feature", initialCommit)

	// Create wip/task-123 with a new commit
	runCmd(t, mainDir, "git", "commit", "--allow-empty", "-m", "wip commit")
	wipCommit := runCmd(t, mainDir, "git", "rev-parse", "HEAD")
	runCmd(t, mainDir, "git", "branch", "-f", "wip/task-123", wipCommit)

	// Create a second worktree with wi/my-feature checked out
	worktreeDir := filepath.Join(worktreeHome, "worktree")
	runCmd(t, mainDir, "git", "worktree", "add", worktreeDir, "wi/my-feature")

	// Call Freeze - should return footgun error
	slug := "my-feature"
	iid := "task-123"
	err := Freeze(mainDir, slug, iid)
	if err == nil {
		t.Fatal("Freeze should return an error when wi/my-feature is checked out")
	}

	// Check error message
	if !strings.Contains(err.Error(), "is checked out at") {
		t.Errorf("error should mention checked out location, got: %v", err)
	}

	// Verify wi/my-feature is unchanged (should still be at initial commit)
	mrCommit := getBranch(t, mainDir, "wi/my-feature")
	if mrCommit != initialCommit {
		t.Errorf("wi/my-feature was changed, should have stayed at %s", initialCommit)
	}

	// Verify wip/task-123 still exists
	if !branchExists(t, mainDir, "wip/task-123") {
		t.Fatal("wip/task-123 should still exist after failed Freeze")
	}

	// Verify the worktree is still intact
	cmd := exec.Command("git", "-C", mainDir, "worktree", "list", "--porcelain")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), worktreeDir) {
		t.Fatal("worktree should still be intact")
	}
}

func TestFreeze_AlreadyRemovedWorktree(t *testing.T) {
	// Setup: worktree already removed but wip/<iid> exists
	repoDir := t.TempDir()
	// Use current working directory for worktree home (validation rejects /tmp and /var/folders)
	cwd, _ := os.Getwd()
	worktreeHome := filepath.Join(cwd, "test-worktree-home-removed")
	os.MkdirAll(worktreeHome, 0755)
	t.Cleanup(func() { os.RemoveAll(worktreeHome) })
	t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

	runCmd(t, repoDir, "git", "init")
	runCmd(t, repoDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoDir, "git", "config", "user.name", "Test User")

	// Create initial commit
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "initial")

	// Create fake origin/main
	runCmd(t, repoDir, "git", "branch", "-f", "origin/main", "main")

	// Create wip/task-123 with a new commit
	runCmd(t, repoDir, "git", "commit", "--allow-empty", "-m", "wip commit")
	wipCommit := runCmd(t, repoDir, "git", "rev-parse", "HEAD")
	runCmd(t, repoDir, "git", "branch", "-f", "wip/task-123", wipCommit)

	// Create a worktree directory (simulate already removed worktree)
	worktreeDir := filepath.Join(worktreeHome, "task-123")
	os.MkdirAll(worktreeDir, 0755)
	// Don't register it with git, just have the directory

	// Call Freeze - should not fail even though worktree path doesn't exist in git
	slug := "my-feature"
	iid := "task-123"
	err := Freeze(repoDir, slug, iid)
	if err != nil {
		t.Fatalf("Freeze should not fail for already-removed worktree: %v", err)
	}

	// Verify wi/my-feature was created
	if !branchExists(t, repoDir, "wi/my-feature") {
		t.Fatal("wi/my-feature should exist after Freeze")
	}

	// Verify wip/task-123 was deleted
	if branchExists(t, repoDir, "wip/task-123") {
		t.Fatal("wip/task-123 should be deleted after Freeze")
	}
}

func TestFreeze_PrefixCollision(t *testing.T) {
	// Setup: wi/auth-v2 is checked out but Freeze is called with slug="auth"
	// The guard should NOT trip because wi/auth (exact) is not checked out
	mainDir := t.TempDir()
	// Use current working directory for worktree home (validation rejects /tmp and /var/folders)
	cwd, _ := os.Getwd()
	worktreeHome := filepath.Join(cwd, "test-worktree-home-prefix")
	os.MkdirAll(worktreeHome, 0755)
	t.Cleanup(func() { os.RemoveAll(worktreeHome) })
	t.Setenv("AGENTASK_WORKTREE_HOME", worktreeHome)

	runCmd(t, mainDir, "git", "init")
	runCmd(t, mainDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, mainDir, "git", "config", "user.name", "Test User")

	// Create initial commit
	runCmd(t, mainDir, "git", "commit", "--allow-empty", "-m", "initial")
	initialCommit := runCmd(t, mainDir, "git", "rev-parse", "HEAD")

	// Create fake origin/main
	runCmd(t, mainDir, "git", "branch", "-f", "origin/main", "main")

	// Create wi/auth-v2 branch at initial commit
	runCmd(t, mainDir, "git", "branch", "-f", "wi/auth-v2", initialCommit)

	// Create a second worktree with wi/auth-v2 checked out
	worktreeDir := filepath.Join(worktreeHome, "worktree-v2")
	runCmd(t, mainDir, "git", "worktree", "add", worktreeDir, "wi/auth-v2")

	// Create wip/task-456 with a new commit
	runCmd(t, mainDir, "git", "commit", "--allow-empty", "-m", "wip commit for auth")
	wipCommit := runCmd(t, mainDir, "git", "rev-parse", "HEAD")
	runCmd(t, mainDir, "git", "branch", "-f", "wip/task-456", wipCommit)

	// Call Freeze with slug="auth" - should NOT fail even though wi/auth-v2 is checked out
	slug := "auth"
	iid := "task-456"
	err := Freeze(mainDir, slug, iid)
	if err != nil {
		t.Fatalf("Freeze should not fail when wi/auth-v2 is checked out but slug=auth: %v", err)
	}

	// Verify wi/auth was created
	if !branchExists(t, mainDir, "wi/auth") {
		t.Fatal("wi/auth should exist after Freeze")
	}

	// Verify wip/task-456 was deleted
	if branchExists(t, mainDir, "wip/task-456") {
		t.Fatal("wip/task-456 should be deleted after Freeze")
	}

	// Verify wi/auth-v2 and its worktree are still intact (the guard didn't block)
	if !branchExists(t, mainDir, "wi/auth-v2") {
		t.Fatal("wi/auth-v2 should still exist")
	}
	cmd := exec.Command("git", "-C", mainDir, "worktree", "list", "--porcelain")
	output, _ := cmd.Output()
	if !strings.Contains(string(output), worktreeDir) {
		t.Fatal("worktree with wi/auth-v2 should still exist")
	}
}
