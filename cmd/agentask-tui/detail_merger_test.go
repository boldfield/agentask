package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestDefaultGHMergerRESTAPI tests that defaultGHMerger uses the REST API with correct arguments.
func TestDefaultGHMergerRESTAPI(t *testing.T) {
	// Save the original commandContextFunc
	oldCommandContextFunc := commandContextFunc
	defer func() { commandContextFunc = oldCommandContextFunc }()

	// Track calls to the command executor
	var capturedName string
	var capturedArgs []string

	// Mock the command execution to capture arguments
	commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = append([]string{}, args...)
		return exec.Command("true")
	}

	ctx := context.Background()
	_ = defaultGHMerger(ctx, "https://github.com/boldfield/agentask/pull/42")

	// Check that the command would be set up correctly
	if capturedName != "gh" {
		t.Errorf("expected command 'gh', got %q", capturedName)
	}

	// Validate REST API arguments
	if len(capturedArgs) < 6 {
		t.Fatalf("expected at least 6 args, got %d: %v", len(capturedArgs), capturedArgs)
	}

	if capturedArgs[0] != "api" {
		t.Errorf("expected first arg 'api', got %q", capturedArgs[0])
	}

	if capturedArgs[1] != "--method" {
		t.Errorf("expected second arg '--method', got %q", capturedArgs[1])
	}

	if capturedArgs[2] != "PUT" {
		t.Errorf("expected third arg 'PUT', got %q", capturedArgs[2])
	}

	expectedEndpoint := "repos/boldfield/agentask/pulls/42/merge"
	if capturedArgs[3] != expectedEndpoint {
		t.Errorf("expected endpoint %q, got %q", expectedEndpoint, capturedArgs[3])
	}

	if capturedArgs[4] != "-f" {
		t.Errorf("expected 5th arg '-f', got %q", capturedArgs[4])
	}

	if capturedArgs[5] != "merge_method=squash" {
		t.Errorf("expected 6th arg 'merge_method=squash', got %q", capturedArgs[5])
	}
}

// TestDefaultGHMergerWithTokenEnv tests that GH_TOKEN is set in environment when a token exists.
func TestDefaultGHMergerWithTokenEnv(t *testing.T) {
	// Create a temporary forge-tokens file
	tmpDir := t.TempDir()
	tokensDir := tmpDir + "/.agentask"
	os.MkdirAll(tokensDir, 0o700)
	tokensFile := tokensDir + "/forge-tokens"

	err := os.WriteFile(tokensFile, []byte("boldfield=test_token_xyz\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create test forge-tokens file: %v", err)
	}

	// Patch userHomeDirFunc to return our temp directory
	oldUserHomeDir := userHomeDirFunc
	userHomeDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() { userHomeDirFunc = oldUserHomeDir }()

	// Save the original commandContextFunc
	oldCommandContextFunc := commandContextFunc
	defer func() { commandContextFunc = oldCommandContextFunc }()

	// Capture the command and its environment
	var capturedCmd *exec.Cmd

	commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.Command("true")
		capturedCmd = cmd // Save for inspection after Env is set
		return cmd
	}

	ctx := context.Background()
	_ = defaultGHMerger(ctx, "https://github.com/boldfield/agentask/pull/42")

	// Check that GH_TOKEN was set in the environment
	if capturedCmd == nil {
		t.Fatal("expected command to be created, but it was nil")
	}

	foundGHToken := false
	for _, env := range capturedCmd.Env {
		if strings.HasPrefix(env, "GH_TOKEN=test_token_xyz") {
			foundGHToken = true
			break
		}
	}

	if !foundGHToken {
		t.Errorf("expected GH_TOKEN=test_token_xyz in command environment, got %v", capturedCmd.Env)
	}
}

// TestDefaultGHMergerInvalidURL tests error handling for invalid PR URLs.
func TestDefaultGHMergerInvalidURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "invalid URL",
			url:     "not a url",
			wantErr: true,
		},
		{
			name:    "non-github URL",
			url:     "https://gitlab.com/owner/repo/pull/1",
			wantErr: true,
		},
		{
			name:    "not a pull request",
			url:     "https://github.com/owner/repo/issues/1",
			wantErr: true,
		},
		{
			name:    "valid PR URL",
			url:     "https://github.com/boldfield/agentask/pull/42",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCommandContextFunc := commandContextFunc
			defer func() { commandContextFunc = oldCommandContextFunc }()

			commandCalled := false
			commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
				commandCalled = true
				return exec.Command("true")
			}

			ctx := context.Background()
			err := defaultGHMerger(ctx, tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for URL %q, got nil", tt.url)
				}
				if !strings.Contains(err.Error(), "gh api merge failed") {
					t.Errorf("expected error containing 'gh api merge failed', got %q", err.Error())
				}
			} else {
				// For valid URLs, command should be called
				if !commandCalled {
					t.Errorf("expected command to be called for valid URL %q", tt.url)
				}
			}
		})
	}
}

// TestDefaultGHMergerGHNotFound tests that an appropriate error is returned when gh is not found.
func TestDefaultGHMergerGHNotFound(t *testing.T) {
	oldLookPath := lookPathFunc
	defer func() { lookPathFunc = oldLookPath }()

	// Mock lookPathFunc to simulate gh not being found
	lookPathFunc = func(file string) (string, error) {
		if file == "gh" {
			return "", exec.ErrNotFound
		}
		return oldLookPath(file)
	}

	ctx := context.Background()
	err := defaultGHMerger(ctx, "https://github.com/boldfield/agentask/pull/42")

	if err == nil {
		t.Error("expected error when gh is not found, got nil")
	}

	if !strings.Contains(err.Error(), "gh command not found") {
		t.Errorf("expected error containing 'gh command not found', got %q", err.Error())
	}
}
