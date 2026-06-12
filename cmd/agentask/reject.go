package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func executeReject(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("reject", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noteFlag := fs.String("note", "", "note for rejection")
	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if len(positionals) < 1 {
		return fmt.Errorf("task id required")
	}

	if *noteFlag == "" {
		return fmt.Errorf("--note flag is required")
	}

	taskID := positionals[0]

	client := tuiclient.NewHTTPClient(baseURL, token)
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if task.State != "review" && task.State != "approved" {
		return fmt.Errorf("task is in %q state, expected review or approved", task.State)
	}

	if err := client.TransitionTask(ctx, taskID, "ready", &*noteFlag); err != nil {
		return fmt.Errorf("failed to transition task to ready: %w", err)
	}

	return nil
}
