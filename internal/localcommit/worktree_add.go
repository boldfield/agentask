package localcommit

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func AddWorktree(repoDir, iid, tip string) (wtPath string, err error) {
	home, err := WorktreeHome()
	if err != nil {
		return "", err
	}

	wtPath = filepath.Join(home, iid)

	// Check if worktree is already registered
	cmd := exec.Command("git", "-C", repoDir, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil && err.Error() != "exit status 1" {
		return "", err
	}

	// Resolve wtPath to canonical form for comparison (handles symlinks)
	canonicalWtPath, _ := filepath.EvalSymlinks(wtPath)
	if canonicalWtPath == "" {
		canonicalWtPath = wtPath
	}

	// Parse the worktree list output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// Compare both original and canonical paths
				if parts[1] == wtPath || parts[1] == canonicalWtPath {
					// Already registered, return unchanged
					return wtPath, nil
				}
			}
		}
	}

	// Add new worktree
	cmd = exec.Command("git", "-C", repoDir, "worktree", "add", wtPath, "-b", "wip/"+iid, tip)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return wtPath, nil
}
