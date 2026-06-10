package main

import (
	"context"
	"os/exec"
	"testing"
)

// TestExtractGitHubOwnerRepo tests parsing of GitHub repo URLs.
func TestExtractGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantOwn  string
		wantRepo string
		wantErr  bool
	}{
		{
			name:     "valid https url",
			url:      "https://github.com/boldfield/agentask",
			wantOwn:  "boldfield",
			wantRepo: "agentask",
		},
		{
			name:     "valid https url with .git",
			url:      "https://github.com/boldfield/agentask.git",
			wantOwn:  "boldfield",
			wantRepo: "agentask",
		},
		{
			name:    "empty url",
			url:     "",
			wantErr: true,
		},
		{
			name:    "non-github url",
			url:     "https://gitlab.com/boldfield/agentask",
			wantErr: true,
		},
		{
			name:    "invalid url format",
			url:     "https://github.com/boldfield",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := extractGitHubOwnerRepo(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractGitHubOwnerRepo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if owner != tt.wantOwn {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwn)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
			}
		})
	}
}

// TestFindOpenPRURL tests the PR resolution from a branch.
func TestFindOpenPRURL(t *testing.T) {
	// Save the original functions
	oldCommandContextFunc := commandContextFunc
	oldLookPathFunc := lookPathFunc
	defer func() {
		commandContextFunc = oldCommandContextFunc
		lookPathFunc = oldLookPathFunc
	}()

	// Mock lookPathFunc to simulate gh being available
	lookPathFunc = func(file string) (string, error) {
		return "/usr/bin/gh", nil
	}

	// Test: successful PR resolution
	t.Run("successful pr resolution", func(t *testing.T) {
		commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Return a mock command that outputs a JSON array with a PR
			return exec.Command("echo", `[{"html_url": "https://github.com/boldfield/agentask/pull/123"}]`)
		}

		ctx := context.Background()
		prURL, err := findOpenPRURL(ctx, "boldfield", "agentask", "feature-branch")
		if err != nil {
			t.Fatalf("findOpenPRURL() error = %v, want nil", err)
		}
		if prURL != "https://github.com/boldfield/agentask/pull/123" {
			t.Errorf("prURL = %q, want https://github.com/boldfield/agentask/pull/123", prURL)
		}
	})

	// Test: no PR found
	t.Run("no pr found", func(t *testing.T) {
		commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Return a mock command that outputs an empty array
			return exec.Command("echo", `[]`)
		}

		ctx := context.Background()
		_, err := findOpenPRURL(ctx, "boldfield", "agentask", "feature-branch")
		if err == nil {
			t.Errorf("findOpenPRURL() error = nil, want error")
		}
	})

	// Test: gh command not found
	t.Run("gh not found", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) {
			return "", exec.ErrNotFound
		}

		ctx := context.Background()
		_, err := findOpenPRURL(ctx, "boldfield", "agentask", "feature-branch")
		if err == nil {
			t.Errorf("findOpenPRURL() error = nil, want error")
		}
	})

	// Test: invalid JSON response
	t.Run("invalid json response", func(t *testing.T) {
		commandContextFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", `not valid json`)
		}

		lookPathFunc = func(file string) (string, error) {
			return "/usr/bin/gh", nil
		}

		ctx := context.Background()
		_, err := findOpenPRURL(ctx, "boldfield", "agentask", "feature-branch")
		if err == nil {
			t.Errorf("findOpenPRURL() error = nil, want error")
		}
	})
}

// TestParsePRURL tests parsing of PR URLs.
func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name      string
		prURL     string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{
			name:      "valid pr url",
			prURL:     "https://github.com/boldfield/agentask/pull/42",
			wantOwner: "boldfield",
			wantRepo:  "agentask",
			wantNum:   42,
		},
		{
			name:      "valid pr url with trailing slash",
			prURL:     "https://github.com/boldfield/agentask/pull/42/",
			wantOwner: "boldfield",
			wantRepo:  "agentask",
			wantNum:   42,
		},
		{
			name:    "invalid url",
			prURL:   "not a url",
			wantErr: true,
		},
		{
			name:    "not a github url",
			prURL:   "https://gitlab.com/boldfield/agentask/pull/42",
			wantErr: true,
		},
		{
			name:    "not a pull request url",
			prURL:   "https://github.com/boldfield/agentask/issues/42",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, num, err := parsePRURL(tt.prURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePRURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
				if num != tt.wantNum {
					t.Errorf("num = %d, want %d", num, tt.wantNum)
				}
			}
		})
	}
}
