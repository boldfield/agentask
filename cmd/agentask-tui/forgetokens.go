package main

import (
	"os"
	"path/filepath"
	"strings"
)

// forgeTokenForOwner reads $HOME/.agentask/forge-tokens and returns the token for the given owner.
// The file format is owner=token per line, with support for:
//   - Case-insensitive owner matching
//   - Comments (# and everything after)
//   - Blank lines (ignored)
//   - Quote-wrapped tokens (single or double quotes stripped if surrounding)
//
// Returns empty string if owner not found or file is missing.
func forgeTokenForOwner(owner string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	filePath := filepath.Join(home, ".agentask", "forge-tokens")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Strip inline comments (everything after #)
		if idx := strings.IndexByte(line, '#'); idx != -1 {
			line = line[:idx]
		}

		// Trim whitespace
		line = strings.TrimSpace(line)

		// Skip blank lines
		if line == "" {
			continue
		}

		// Split on first =
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		lineOwner := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])

		// Case-insensitive match
		if strings.EqualFold(lineOwner, owner) {
			// Strip surrounding quotes (same quote character on both ends)
			token = stripSurroundingQuotes(token)
			return token
		}
	}

	return ""
}

// stripSurroundingQuotes removes surrounding single or double quotes from a string.
// Only removes quotes if they match on both ends (e.g., "token" or 'token').
func stripSurroundingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}

	first := s[0]
	last := s[len(s)-1]

	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}

	return s
}
