package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func executeDiff(ctx context.Context, baseURL, token string, args []string, out io.Writer) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	taskID := ""
	for _, arg := range args {
		if arg != "--json" {
			taskID = arg
			break
		}
	}

	if taskID == "" {
		return fmt.Errorf("task id required")
	}

	client := tuiclient.NewHTTPClient(baseURL, token)
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Find the PR link
	var prURL string
	for _, link := range task.Links {
		if link.Kind == "pr" {
			prURL = link.Value
			break
		}
	}

	if prURL == "" {
		return fmt.Errorf("task has no pull request link")
	}

	fmt.Fprintln(out, prURL)

	// Check if gh is available and run gh pr diff if it is
	if _, err := exec.LookPath("gh"); err == nil {
		cmd := exec.CommandContext(ctx, "gh", "pr", "diff", prURL)
		cmd.Stdout = out
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run gh pr diff: %w", err)
		}
	}

	return nil
}
