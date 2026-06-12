package localcommit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeHome resolves the worktree root directory: AGENTASK_WORKTREE_HOME if set,
// otherwise AGENTASK_HOME. It errors only when neither is set.
//
// The tmpfs durability check lives in EnsureDurableWorktreeHome, NOT here, on purpose:
// a bounced item's wip/<iid> worktree must survive the rework loop in production, but
// the library's consumers (CleanupAbandon, AddWorktree, Freeze) and their tests
// legitimately use temp dirs. Folding the durability guard into the resolver made every
// consumer untestable. So the resolver stays pure; the fleet/harness calls the guard
// once at startup.
func WorktreeHome() (string, error) {
	if path := os.Getenv("AGENTASK_WORKTREE_HOME"); path != "" {
		return path, nil
	}
	if path := os.Getenv("AGENTASK_HOME"); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("neither AGENTASK_WORKTREE_HOME nor AGENTASK_HOME is set")
}

// EnsureDurableWorktreeHome verifies the resolved worktree home is on a durable
// filesystem — NOT under /tmp or /var/folders — because a bounced item's wip/<iid>
// worktree must persist across the submit→reject→reclaim→amend rework loop; a tmpfs
// home would wipe it. The fleet/harness calls this once at startup. It is intentionally
// separate from WorktreeHome() so library consumers stay testable with temp dirs.
func EnsureDurableWorktreeHome() error {
	path, err := WorktreeHome()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if strings.HasPrefix(abs, "/tmp") || strings.HasPrefix(abs, "/var/folders") {
		return fmt.Errorf("worktree home cannot be under temporary filesystem: %s", abs)
	}
	return nil
}
