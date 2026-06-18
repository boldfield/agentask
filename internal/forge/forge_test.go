package forge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnerToken(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		owner       string
		want        string
		wantErr     bool
	}{
		{
			name:        "valid token",
			fileContent: "alice=token123\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "case-insensitive match",
			fileContent: "Alice=token123\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "token with double quotes",
			fileContent: `alice="token123"` + "\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "token with single quotes",
			fileContent: "alice='token123'\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "with comments",
			fileContent: "alice=token123 # this is a comment\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "blank lines",
			fileContent: "alice=token123\n\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "owner not found",
			fileContent: "alice=token123\nbob=token456\n",
			owner:       "charlie",
			want:        "",
			wantErr:     false,
		},
		{
			name:        "whitespace around equals",
			fileContent: "alice = token123 \n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory and file
			tmpDir, err := os.MkdirTemp("", "forge-test-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			agtDir := filepath.Join(tmpDir, ".agentask")
			if err := os.MkdirAll(agtDir, 0755); err != nil {
				t.Fatalf("failed to create .agentask dir: %v", err)
			}

			tokenFile := filepath.Join(agtDir, "forge-tokens")
			if err := os.WriteFile(tokenFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("failed to write token file: %v", err)
			}

			// Mock userHomeDirFunc
			oldUserHomeDir := userHomeDirFunc
			userHomeDirFunc = func() (string, error) {
				return tmpDir, nil
			}
			defer func() {
				userHomeDirFunc = oldUserHomeDir
			}()

			got, err := OwnerToken(tt.owner)
			if (err != nil) != tt.wantErr {
				t.Errorf("OwnerToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("OwnerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOwnerToken_MissingFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Mock userHomeDirFunc to point to a dir without forge-tokens
	oldUserHomeDir := userHomeDirFunc
	userHomeDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		userHomeDirFunc = oldUserHomeDir
	}()

	got, err := OwnerToken("alice")
	if err != nil {
		t.Errorf("OwnerToken() error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("OwnerToken() = %q, want empty string", got)
	}
}

func TestOwnerToken_UserHomeDirError(t *testing.T) {
	oldUserHomeDir := userHomeDirFunc
	userHomeDirFunc = func() (string, error) {
		return "", fmt.Errorf("home dir lookup failed")
	}
	defer func() {
		userHomeDirFunc = oldUserHomeDir
	}()

	_, err := OwnerToken("alice")
	if err == nil {
		t.Errorf("OwnerToken() error = nil, want error")
	}
}

func TestOwnerToken_ForgeTokensEnvVar(t *testing.T) {
	// Create a temporary directory and file at a custom path
	tmpDir, err := os.MkdirTemp("", "forge-test-env-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	customTokenFile := filepath.Join(tmpDir, "custom-forge-tokens")
	if err := os.WriteFile(customTokenFile, []byte("alice=custom-token\n"), 0644); err != nil {
		t.Fatalf("failed to write custom token file: %v", err)
	}

	// Set FORGE_TOKENS environment variable
	oldForgeTokens := os.Getenv("FORGE_TOKENS")
	os.Setenv("FORGE_TOKENS", customTokenFile)
	defer func() {
		if oldForgeTokens == "" {
			os.Unsetenv("FORGE_TOKENS")
		} else {
			os.Setenv("FORGE_TOKENS", oldForgeTokens)
		}
	}()

	// OwnerToken should read from the custom path (and not try to access the home dir)
	got, err := OwnerToken("alice")
	if err != nil {
		t.Errorf("OwnerToken() error = %v, want nil", err)
	}
	if got != "custom-token" {
		t.Errorf("OwnerToken() = %q, want %q", got, "custom-token")
	}
}

func TestSquashMerge(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		prNumber     int
		owner        string
		repo         string
		token        string
		wantErr      bool
	}{
		{
			name:         "successful merge",
			statusCode:   200,
			responseBody: `{"id": 1, "merged": true}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      false,
		},
		{
			name:         "merge without token",
			statusCode:   200,
			responseBody: `{"id": 1, "merged": true}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "",
			wantErr:      false,
		},
		{
			name:         "not found",
			statusCode:   404,
			responseBody: `{"message": "Not Found"}`,
			prNumber:     999,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      true,
		},
		{
			name:         "conflict",
			statusCode:   409,
			responseBody: `{"message": "Pull Request is not mergeable"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      true,
		},
		{
			name:         "unauthorized",
			statusCode:   401,
			responseBody: `{"message": "Bad credentials"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "invalid-token",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// On a failed merge, SquashMerge follows up with a GET to the merge-status
				// endpoint to check for an already-merged PR. Mirror tt.statusCode for that
				// GET so error cases stay errors (404 -> not merged; other -> check errors,
				// both preserve the original merge failure), and skip the PUT-only asserts.
				if r.Method == http.MethodGet {
					w.WriteHeader(tt.statusCode)
					w.Write([]byte(tt.responseBody))
					return
				}

				// Verify the request details
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}

				expectedPath := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", tt.owner, tt.repo, tt.prNumber)
				if r.URL.Path != expectedPath {
					t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
				}

				// Verify headers
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("expected Content-Type: application/json, got %s", ct)
				}

				if tt.token != "" {
					expectedAuth := "token " + tt.token
					if auth := r.Header.Get("Authorization"); auth != expectedAuth {
						t.Errorf("expected Authorization: %s, got %s", expectedAuth, auth)
					}
				}

				// Verify request body contains merge_method=squash
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "squash") {
					t.Errorf("expected request body to contain 'squash', got %s", string(body))
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Mock GitHubBaseURL to use our test server
			oldBaseURL := GitHubBaseURL
			GitHubBaseURL = server.URL
			defer func() {
				GitHubBaseURL = oldBaseURL
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Call the real SquashMerge function with mocked base URL
			err := SquashMerge(ctx, tt.owner, tt.repo, tt.prNumber, tt.token)

			if (err != nil) != tt.wantErr {
				t.Errorf("SquashMerge() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSquashMergeAlreadyMerged verifies idempotency: when the PUT merge fails because
// the PR is already merged (GitHub answers 405), SquashMerge confirms via the
// merge-status endpoint (204 = merged) and returns nil so a retried merge job
// converges instead of looping.
func TestSquashMergeAlreadyMerged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			// Already-merged PR: GitHub returns 405 Method Not Allowed.
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"message": "Pull Request is not mergeable"}`))
		case http.MethodGet:
			// Merge-status check: 204 means the PR is merged.
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := SquashMerge(ctx, "testuser", "testrepo", 42, "test-token"); err != nil {
		t.Errorf("expected nil error for an already-merged PR, got: %v", err)
	}
}

// TestSquashMergeFailsWhenNotMergeable verifies that a genuine non-mergeable PR (PUT
// 405, and the merge-status check confirms NOT merged with 404) still surfaces an
// error rather than being masked by the idempotency path.
func TestSquashMergeFailsWhenNotMergeable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"message": "Pull Request is not mergeable"}`))
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound) // not merged
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldBaseURL }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := SquashMerge(ctx, "testuser", "testrepo", 42, "test-token"); err == nil {
		t.Error("expected error for a non-mergeable PR that is not already merged")
	}
}

func TestSquashMergeIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/123/merge" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"merged": true}`))
	}))
	defer server.Close()

	// Mock GitHubBaseURL to use our test server
	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call the real SquashMerge function with mocked base URL
	err := SquashMerge(ctx, "owner", "repo", 123, "test-token")
	if err != nil {
		t.Fatalf("SquashMerge() error = %v, want nil", err)
	}
}

func TestPostPRComment(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		prNumber     int
		owner        string
		repo         string
		token        string
		comment      string
		wantErr      bool
	}{
		{
			name:         "successful comment",
			statusCode:   201,
			responseBody: `{"id": 1, "body": "test comment"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			comment:      "test comment",
			wantErr:      false,
		},
		{
			name:         "comment without token",
			statusCode:   201,
			responseBody: `{"id": 1, "body": "test comment"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "",
			comment:      "test comment",
			wantErr:      false,
		},
		{
			name:         "not found",
			statusCode:   404,
			responseBody: `{"message": "Not Found"}`,
			prNumber:     999,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			comment:      "test comment",
			wantErr:      true,
		},
		{
			name:         "unauthorized",
			statusCode:   401,
			responseBody: `{"message": "Bad credentials"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "invalid-token",
			comment:      "test comment",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request details
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}

				expectedPath := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", tt.owner, tt.repo, tt.prNumber)
				if r.URL.Path != expectedPath {
					t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
				}

				// Verify headers
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("expected Content-Type: application/json, got %s", ct)
				}

				if tt.token != "" {
					expectedAuth := "token " + tt.token
					if auth := r.Header.Get("Authorization"); auth != expectedAuth {
						t.Errorf("expected Authorization: %s, got %s", expectedAuth, auth)
					}
				}

				// Verify request body contains the comment
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), tt.comment) {
					t.Errorf("expected request body to contain %q, got %s", tt.comment, string(body))
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Mock GitHubBaseURL to use our test server
			oldBaseURL := GitHubBaseURL
			GitHubBaseURL = server.URL
			defer func() {
				GitHubBaseURL = oldBaseURL
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Call the real PostPRComment function with mocked base URL
			err := PostPRComment(ctx, tt.owner, tt.repo, tt.prNumber, tt.token, tt.comment)

			if (err != nil) != tt.wantErr {
				t.Errorf("PostPRComment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
