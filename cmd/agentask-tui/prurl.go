package main

import (
	"fmt"
	"net/url"
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
