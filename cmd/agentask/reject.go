package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/boldfield/agentask/internal/localcommit"
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
	abandonFlag := fs.Bool("abandon", false, "abandon the task (cleanup worktree and wip branch in local_commit mode)")
	repoFlag := fs.String("repo", "", "repository directory")
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

	// If --abandon, transition to failed; otherwise transition to ready
	newState := "ready"
	if *abandonFlag {
		newState = "failed"
	}

	if err := client.TransitionTask(ctx, taskID, newState, &*noteFlag); err != nil {
		return fmt.Errorf("failed to transition task to %s: %w", newState, err)
	}

	// If --abandon and in local_commit mode, cleanup the worktree and wip branch
	if *abandonFlag && localcommit.IsLocalCommit() {
		repoDir := *repoFlag
		if repoDir == "" {
			repoDir = os.Getenv("AGENTASK_REPO")
		}
		if repoDir == "" {
			return fmt.Errorf("--repo flag or AGENTASK_REPO environment variable required")
		}

		if err := localcommit.CleanupAbandon(repoDir, taskID); err != nil {
			return err
		}
	}

	return nil
}
