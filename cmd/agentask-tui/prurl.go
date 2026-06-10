package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// parsePRURL parses a GitHub PR URL into its components.
// Expected format: https://github.com/<owner>/<repo>/pull/<number>
// Tolerates a trailing slash.
func parsePRURL(prURL string) (owner, repo string, number int, err error) {
	// Remove trailing slash
	prURL = strings.TrimSuffix(prURL, "/")

	// Parse the URL
	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid URL: %w", err)
	}

	// Check that it's a github.com URL
	if u.Host != "github.com" {
		return "", "", 0, fmt.Errorf("not a github.com URL")
	}

	// Split the path
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	// Expected format: <owner>/<repo>/pull/<number>
	if len(parts) != 4 || parts[2] != "pull" {
		return "", "", 0, fmt.Errorf("not a pull request URL")
	}

	owner = parts[0]
	repo = parts[1]

	// Parse the number
	number, err = strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid pull request number: %w", err)
	}

	return owner, repo, number, nil
}

// extractGitHubOwnerRepo parses a GitHub repo URL and returns the owner and repo.
// Expected format: https://github.com/<owner>/<repo> or https://github.com/<owner>/<repo>.git
// Returns ("", "", error) if the URL cannot be parsed.
func extractGitHubOwnerRepo(repoURL string) (owner, repo string, err error) {
	if repoURL == "" {
		return "", "", fmt.Errorf("project has no repo configured")
	}

	// Parse the URL
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid repo URL: %w", err)
	}

	// Check that it's a github.com URL
	if u.Host != "github.com" {
		return "", "", fmt.Errorf("not a github.com URL")
	}

	// Split the path and extract owner/repo
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	// Expected format: <owner>/<repo> or <owner>/<repo>.git
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repo URL format")
	}

	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")

	return owner, repo, nil
}

// findOpenPRURL queries GitHub for the first open PR on the given branch.
// Uses gh api repos/{owner}/{repo}/pulls?head={owner}:{branch}&state=open
// and returns the first PR's html_url. Uses per-owner token via forgeTokenForOwner.
func findOpenPRURL(ctx context.Context, owner, repo, branch string) (string, error) {
	if _, err := lookPathFunc("gh"); err != nil {
		return "", fmt.Errorf("gh command not found: install GitHub CLI (https://cli.github.com)")
	}

	endpoint := fmt.Sprintf("repos/%s/%s/pulls", owner, repo)
	query := fmt.Sprintf("head=%s:%s", owner, branch)

	cmd := commandContextFunc(ctx, "gh", "api", endpoint, "-f", query, "-f", "state=open")

	// Get the per-owner token from forge-tokens file.
	token := forgeTokenForOwner(owner)

	// If a token was found, set GH_TOKEN in the command's environment.
	if token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
	}

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh api query failed: %w", err)
	}

	// Parse the JSON response: expect an array of PR objects
	var prs []map[string]interface{}
	if err := json.Unmarshal(output, &prs); err != nil {
		return "", fmt.Errorf("failed to parse gh api response: %w", err)
	}

	// Return the first PR's html_url if found
	if len(prs) > 0 {
		if htmlURL, ok := prs[0]["html_url"].(string); ok {
			return htmlURL, nil
		}
	}

	return "", fmt.Errorf("no open PR found on branch %s:%s", owner, branch)
}
