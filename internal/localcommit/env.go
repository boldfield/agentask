package localcommit

import (
	"os"
	"strings"
)

// DeliveryMode reads AGENTASK_DELIVERY_MODE, trims whitespace, lowercases it,
// and defaults to "pull_request" if unset, empty, or unknown.
func DeliveryMode() string {
	mode := os.Getenv("AGENTASK_DELIVERY_MODE")
	mode = strings.TrimSpace(mode)
	mode = strings.ToLower(mode)
	if mode != "local_commit" {
		mode = "pull_request"
	}
	return mode
}

// IsLocalCommit returns true iff DeliveryMode() == "local_commit".
func IsLocalCommit() bool {
	return DeliveryMode() == "local_commit"
}

// WorktreeHome returns the durable worktree root directory.
// It reads AGENTASK_WORKTREE_HOME, with fallback to AGENTASK_HOME.
func WorktreeHome() string {
	if home := os.Getenv("AGENTASK_WORKTREE_HOME"); home != "" {
		return home
	}
	return os.Getenv("AGENTASK_HOME")
}
