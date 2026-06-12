package localcommit

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func CommitAll(wtPath, message string) (sha string, err error) {
	// Stage all changes
	cmd := exec.Command("git", "-C", wtPath, "add", "-A")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// Try to commit
	cmd = exec.Command("git", "-C", wtPath, "commit", "-m", message)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Check if the error is "nothing to commit"
		if strings.Contains(stderr.String(), "nothing to commit") {
			return "", fmt.Errorf("nothing to commit")
		}
		return "", fmt.Errorf("git commit failed: %w", err)
	}

	// Get the new HEAD SHA
	cmd = exec.Command("git", "-C", wtPath, "rev-parse", "HEAD")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	sha = strings.TrimSpace(stdout.String())
	return sha, nil
}

func AmendAll(wtPath, message string) (sha string, err error) {
	// Stage all changes
	cmd := exec.Command("git", "-C", wtPath, "add", "-A")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// Amend the last commit (allowed to have no new changes)
	cmd = exec.Command("git", "-C", wtPath, "commit", "--amend", "-m", message)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git commit --amend failed: %w", err)
	}

	// Get the new HEAD SHA
	cmd = exec.Command("git", "-C", wtPath, "rev-parse", "HEAD")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	sha = strings.TrimSpace(stdout.String())
	return sha, nil
}
