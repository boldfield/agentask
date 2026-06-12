package localcommit

import (
	"fmt"
	"os"
)

func WorktreeHome() (string, error) {
	// Try AGENTASK_WORKTREE_HOME first
	if path := os.Getenv("AGENTASK_WORKTREE_HOME"); path != "" {
		return path, nil
	}

	// Fall back to AGENTASK_HOME
	if path := os.Getenv("AGENTASK_HOME"); path != "" {
		return path, nil
	}

	return "", fmt.Errorf("neither AGENTASK_WORKTREE_HOME nor AGENTASK_HOME is set")
}
