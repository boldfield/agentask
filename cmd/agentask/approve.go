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

func executeApprove(ctx context.Context, baseURL, token string, args []string) error {
	if baseURL == "" {
		return fmt.Errorf("AGENTASK_URL environment variable not set")
	}
	if token == "" {
		return fmt.Errorf("AGENTASK_TOKEN environment variable not set")
	}

	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoFlag := fs.String("repo", "", "repository directory")
	freezeOnlyFlag := fs.Bool("freeze-only", false, "only retry freeze, skip transition")
	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	if len(positionals) < 1 {
		return fmt.Errorf("task id required")
	}

	taskID := positionals[0]

	client := tuiclient.NewHTTPClient(baseURL, token)
	task, err := client.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// Transition to done (unless --freeze-only)
	if !*freezeOnlyFlag {
		if task.State != "approved" {
			return fmt.Errorf("task is in %q state, expected approved", task.State)
		}

		if err := client.TransitionTask(ctx, taskID, "done", nil); err != nil {
			return fmt.Errorf("failed to transition task to done: %w", err)
		}
	}

	// If local_commit mode, freeze the branch
	if localcommit.IsLocalCommit() {
		repoDir := *repoFlag
		if repoDir == "" {
			repoDir = os.Getenv("AGENTASK_REPO")
		}
		if repoDir == "" {
			return fmt.Errorf("--repo flag or AGENTASK_REPO environment variable required")
		}

		slug := localcommit.Slugify(task.Title)
		if err := localcommit.Freeze(repoDir, slug, taskID); err != nil {
			return err
		}
	}

	return nil
}
