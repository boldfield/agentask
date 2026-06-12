package localcommit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WorktreeHome() (string, error) {
	// Try AGENTASK_WORKTREE_HOME first
	if path := os.Getenv("AGENTASK_WORKTREE_HOME"); path != "" {
		if err := validatePath(path); err != nil {
			return "", err
		}
		return path, nil
	}

	// Fall back to AGENTASK_HOME
	if path := os.Getenv("AGENTASK_HOME"); path != "" {
		if err := validatePath(path); err != nil {
			return "", err
		}
		return path, nil
	}

	return "", fmt.Errorf("neither AGENTASK_WORKTREE_HOME nor AGENTASK_HOME is set")
}

func validatePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if strings.HasPrefix(abs, "/tmp") {
		return fmt.Errorf("worktree home cannot be under temporary filesystem: %s", abs)
	}

	return nil
}
