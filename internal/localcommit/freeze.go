package localcommit

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func Freeze(repoDir, slug, iid string) error {
	// Step 1: Check if wi/<slug> is checked out in any worktree
	cmd := exec.Command("git", "-C", repoDir, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list worktrees: %w", err)
	}

	targetBranch := "refs/heads/wi/" + slug
	var checkedOutPath string
	var currentWorktreePath string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktreePath = strings.TrimPrefix(line, "worktree ")
		}
		if strings.Contains(line, targetBranch) && currentWorktreePath != "" {
			checkedOutPath = currentWorktreePath
			break
		}
	}

	if checkedOutPath != "" {
		return fmt.Errorf("MR branch wi/%s is checked out at %s; cd out or run 'git checkout --detach' there, then re-approve", slug, checkedOutPath)
	}

	// Step 2: Force wi/<slug> to point at wip/<iid>
	wipBranch := "wip/" + iid
	mrBranch := "wi/" + slug
	cmd = exec.Command("git", "-C", repoDir, "branch", "-f", mrBranch, wipBranch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create/advance MR branch: %w", err)
	}

	// Step 3: Remove the worktree (tolerate already-removed)
	home, err := WorktreeHome()
	if err != nil {
		return err
	}
	worktreePath := filepath.Join(home, iid)
	cmd = exec.Command("git", "-C", repoDir, "worktree", "remove", worktreePath)
	cmd.Run() // Ignore error - it might already be removed

	// Step 4: Delete the wip/<iid> branch
	cmd = exec.Command("git", "-C", repoDir, "branch", "-d", wipBranch)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete WIP branch: %w", err)
	}

	return nil
}
