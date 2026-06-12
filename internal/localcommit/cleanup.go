package localcommit

import (
	"os/exec"
	"path/filepath"
)

// CleanupAbandon removes the worktree and wip branch for the given task ID.
// It is idempotent: an already-gone worktree/branch is not an error.
func CleanupAbandon(repoDir, iid string) error {
	home, err := WorktreeHome()
	if err != nil {
		return err
	}
	worktreePath := filepath.Join(home, iid)

	// Remove the worktree (ignore errors if it doesn't exist)
	cmd := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
	_ = cmd.Run()

	// Remove the wip branch (ignore errors if it doesn't exist)
	cmd = exec.Command("git", "-C", repoDir, "branch", "-D", WIPBranch(iid))
	_ = cmd.Run()

	return nil
}
