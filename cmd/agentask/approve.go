package main

import (
	"context"
	"fmt"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func executeApprove(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	if len(args) < 1 {
		return fmt.Errorf("task id required")
	}

	taskID := args[0]

	client := tuiclient.NewHTTPClient(baseURL, token)
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if task.State != "approved" {
		return fmt.Errorf("task is in %q state, expected approved", task.State)
	}

	if err := client.TransitionTask(ctx, taskID, "done", nil); err != nil {
		return fmt.Errorf("failed to transition task to done: %w", err)
	}

	return nil
}
