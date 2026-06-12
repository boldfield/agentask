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
