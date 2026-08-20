package forge

import (
	"regexp"
	"strings"
)

var agentMarkerRegex = regexp.MustCompile(`^[a-z0-9.-]+-(worker|reviewer|merger|reconciler):`)

// IsAgentAuthoredComment reports whether a PR comment body is agent-authored.
// A comment is agent-authored iff, after leading whitespace, it matches:
// <token>-(worker|reviewer|merger|reconciler): where <token> is one or more of [a-z0-9.-]
func IsAgentAuthoredComment(body string) bool {
	trimmed := strings.TrimLeft(body, " \t\n\r")
	return agentMarkerRegex.MatchString(trimmed)
}

// MarkerPrefix composes a marker prefix from role and model strings.
// role should be one of "worker", "reviewer", or "merger".
func MarkerPrefix(model, role string) string {
	return model + "-" + role + ": "
}
