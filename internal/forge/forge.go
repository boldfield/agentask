package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// userHomeDirFunc is the function used to get the home directory (made mockable for testing).
var userHomeDirFunc func() (string, error) = os.UserHomeDir

// OwnerToken reads ~/.agentask/forge-tokens and returns the token for the given owner.
// The file format is owner=token per line, with support for:
//   - Case-insensitive owner matching
//   - Comments (# and everything after)
//   - Blank lines (ignored)
//   - Quote-wrapped tokens (single or double quotes stripped if surrounding)
//
// Returns empty string if owner not found or file is missing.
func OwnerToken(owner string) (string, error) {
	home, err := userHomeDirFunc()
	if err != nil {
		return "", nil
	}

	filePath := filepath.Join(home, ".agentask", "forge-tokens")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", nil
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
			return token, nil
		}
	}

	return "", nil
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

// SquashMerge performs a squash merge of a GitHub PR using the GitHub REST API.
// It makes a PUT request to /repos/{owner}/{repo}/pulls/{prNumber}/merge with merge_method=squash.
func SquashMerge(ctx context.Context, owner, repo string, prNumber int, token string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/merge", owner, repo, prNumber)

	payload := map[string]string{
		"merge_method": "squash",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("merge failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
