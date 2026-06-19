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
	"time"
)

// userHomeDirFunc is the function used to get the home directory (made mockable for testing).
var userHomeDirFunc func() (string, error) = os.UserHomeDir

// GitHubBaseURL is the base URL for GitHub API calls (made mockable for testing).
var GitHubBaseURL = "https://api.github.com"

// OwnerToken reads the forge tokens file and returns the token for the given owner.
// The file path is determined by the FORGE_TOKENS environment variable (if set),
// or defaults to ~/.agentask/forge-tokens.
// The file format is owner=token per line, with support for:
//   - Case-insensitive owner matching
//   - Comments (# and everything after)
//   - Blank lines (ignored)
//   - Quote-wrapped tokens (single or double quotes stripped if surrounding)
//
// Returns empty string if owner not found or file is missing.
func OwnerToken(owner string) (string, error) {
	var filePath string

	// Check for FORGE_TOKENS environment variable
	if forgeTokensEnv := os.Getenv("FORGE_TOKENS"); forgeTokensEnv != "" {
		filePath = forgeTokensEnv
	} else {
		// Default to ~/.agentask/forge-tokens
		home, err := userHomeDirFunc()
		if err != nil {
			return "", err
		}
		filePath = filepath.Join(home, ".agentask", "forge-tokens")
	}

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
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge", GitHubBaseURL, owner, repo, prNumber)

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
		// A PR that a prior (interrupted) merge run already merged returns a non-2xx
		// here (GitHub answers 405 "not mergeable" for an already-merged PR). Treat an
		// already-merged PR as success so a retried merge job converges instead of
		// looping forever. Verify via the merged flag rather than trusting the status
		// code, so genuinely non-mergeable PRs (conflicts, failing checks) still error.
		if merged, mErr := prAlreadyMerged(ctx, owner, repo, prNumber, token); mErr == nil && merged {
			return nil
		}
		return fmt.Errorf("merge failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// PostPRComment posts a top-level comment on a GitHub PR using the GitHub REST API.
// It makes a POST request to /repos/{owner}/{repo}/issues/{prNumber}/comments with the comment text.
func PostPRComment(ctx context.Context, owner, repo string, prNumber int, token, comment string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", GitHubBaseURL, owner, repo, prNumber)

	payload := map[string]string{
		"body": comment,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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
		return fmt.Errorf("post comment failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// prAlreadyMerged reports whether the given PR has already been merged, using
// GitHub's GET /pulls/{n}/merge endpoint: 204 means merged, 404 means not merged.
// Any other status (or a transport error) returns an error so the caller can fall
// back to surfacing the original merge failure rather than masking it.
func prAlreadyMerged(ctx context.Context, owner, repo string, prNumber int, token string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge", GitHubBaseURL, owner, repo, prNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create merge-status request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check merge status: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent: // 204 — PR is merged
		return true, nil
	case http.StatusNotFound: // 404 — PR exists but is not merged
		return false, nil
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("unexpected merge-status %d: %s", resp.StatusCode, string(respBody))
	}
}

// PRMergeability returns GitHub's computed `mergeable_state` for a PR — one of
// "clean", "dirty" (conflicts with base), "behind" (out of date), "blocked" (failing
// required checks/reviews), "unstable", "draft", or "unknown". GitHub computes this
// asynchronously, so a just-pushed PR reports mergeable=null / "unknown" briefly; this
// polls a few times until it settles, returning "unknown" if it never does (so the
// caller treats it as transient rather than a definitive conflict).
func PRMergeability(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", GitHubBaseURL, owner, repo, prNumber)
	const attempts = 3

	lastState := "unknown"
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create PR request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to fetch PR: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("failed to fetch PR: status %d: %s", resp.StatusCode, string(body))
		}

		var pr struct {
			Mergeable      *bool  `json:"mergeable"`
			MergeableState string `json:"mergeable_state"`
		}
		if err := json.Unmarshal(body, &pr); err != nil {
			return "", fmt.Errorf("failed to parse PR: %w", err)
		}
		if pr.MergeableState != "" {
			lastState = pr.MergeableState
		}
		// mergeable!=nil means GitHub finished computing; the state is now authoritative.
		if pr.Mergeable != nil && pr.MergeableState != "" && pr.MergeableState != "unknown" {
			return pr.MergeableState, nil
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return lastState, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	return lastState, nil
}

// NeedsRework reports whether a `mergeable_state` means the PR can only be fixed by
// reworking the branch — syncing the base in and resolving — rather than by retrying
// the merge. "dirty" is a real conflict; "behind" is out of date under a
// require-branches-up-to-date rule. Both are fixed by the worker's merge-with-main on
// rework. "blocked"/"unstable"/"draft"/"unknown" are NOT rework cases (failing checks,
// drafts, or not-yet-computed) and are left for the caller to retry/surface.
func NeedsRework(mergeableState string) bool {
	return mergeableState == "dirty" || mergeableState == "behind"
}
