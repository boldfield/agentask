package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/boldfield/odonian/internal/forge"
)

// odonianTaskServer returns a mock odonian server serving GET /tasks/{id} with the given
// review round and pr link, and accepting POST /tasks/{id}/submit unconditionally.
func odonianTaskServer(t *testing.T, taskID string, reviewRound int, prLink string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/"+taskID:
			links := []map[string]string{}
			if prLink != "" {
				links = append(links, map[string]string{"kind": "pr", "value": prLink})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":           taskID,
				"kind":         "implement",
				"title":        "test task",
				"review_round": reviewRound,
				"links":        links,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/"+taskID+"/submit":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// githubFeedbackServer returns a mock GitHub API serving /user and /graphql. If item is
// non-empty it is returned as a single unaddressed global comment; otherwise both queries
// return empty results.
func githubFeedbackServer(t *testing.T, itemBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"login": "test-bot"})
		case "/graphql":
			w.Header().Set("Content-Type", "application/json")
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			query := body["query"]
			switch {
			case strings.Contains(query, "reviewThreads"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"repository": map[string]interface{}{
							"pullRequest": map[string]interface{}{
								"reviewThreads": map[string]interface{}{
									"pageInfo": map[string]interface{}{"hasNextPage": false},
									"nodes":    []map[string]interface{}{},
								},
							},
						},
					},
				})
			case strings.Contains(query, "comments(first:"):
				nodes := []map[string]interface{}{}
				if itemBody != "" {
					nodes = append(nodes, map[string]interface{}{
						"id":             "global-comment-1",
						"databaseId":     123,
						"body":           itemBody,
						"createdAt":      "2026-01-01T00:00:00Z",
						"author":         map[string]string{"login": "reviewer"},
						"reactionGroups": []interface{}{},
					})
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"repository": map[string]interface{}{
							"pullRequest": map[string]interface{}{
								"id": "pr-node-1",
								"comments": map[string]interface{}{
									"pageInfo": map[string]interface{}{"hasNextPage": false},
									"nodes":    nodes,
								},
							},
						},
					},
				})
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestExecuteSubmitFeedbackGateBlocksWithRemainingItems(t *testing.T) {
	prURL := "https://github.com/owner/repo/pull/42"

	odonianServer := odonianTaskServer(t, "task123", 1, prURL)
	defer odonianServer.Close()

	ghServer := githubFeedbackServer(t, "please fix this")
	defer ghServer.Close()

	oldBase := forge.GitHubBaseURL
	forge.GitHubBaseURL = ghServer.URL
	defer func() { forge.GitHubBaseURL = oldBase }()

	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), odonianServer.URL, "test-token", []string{
		"--result", "reworked",
		"--pr", prURL,
		"--branch", "mr/a1b2c3d4",
		"task123",
	})
	if err == nil {
		t.Fatal("expected executeSubmit to be blocked by the pr-feedback gate, got nil error")
	}
	if !strings.Contains(err.Error(), "pr-feedback ack") {
		t.Errorf("expected error to point at pr-feedback ack, got: %v", err)
	}
}

func TestExecuteSubmitFeedbackGateCleanPass(t *testing.T) {
	prURL := "https://github.com/owner/repo/pull/42"

	odonianServer := odonianTaskServer(t, "task123", 1, prURL)
	defer odonianServer.Close()

	ghServer := githubFeedbackServer(t, "")
	defer ghServer.Close()

	oldBase := forge.GitHubBaseURL
	forge.GitHubBaseURL = ghServer.URL
	defer func() { forge.GitHubBaseURL = oldBase }()

	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), odonianServer.URL, "test-token", []string{
		"--result", "reworked",
		"--pr", prURL,
		"--branch", "mr/a1b2c3d4",
		"task123",
	})
	if err != nil {
		t.Fatalf("expected executeSubmit to pass with no unaddressed feedback, got: %v", err)
	}
}

func TestExecuteSubmitFeedbackGateBypassFlag(t *testing.T) {
	prURL := "https://github.com/owner/repo/pull/42"

	odonianServer := odonianTaskServer(t, "task123", 1, prURL)
	defer odonianServer.Close()

	// A real GitHub mock with an outstanding item, and a working token: without
	// --skip-feedback-gate this setup blocks the submit (see
	// TestExecuteSubmitFeedbackGateBlocksWithRemainingItems). Passing here proves the flag
	// genuinely bypasses the check rather than coincidentally hitting the
	// check-error-proceeds path.
	ghServer := githubFeedbackServer(t, "please fix this")
	defer ghServer.Close()

	oldBase := forge.GitHubBaseURL
	forge.GitHubBaseURL = ghServer.URL
	defer func() { forge.GitHubBaseURL = oldBase }()

	t.Setenv("GH_TOKEN", "test-token")
	t.Setenv("AGENT_ID", "test-agent")

	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("failed to create pipe: %v", pipeErr)
	}
	oldStderr := os.Stderr
	os.Stderr = w

	err := executeSubmit(context.Background(), odonianServer.URL, "test-token", []string{
		"--result", "reworked",
		"--pr", prURL,
		"--branch", "mr/a1b2c3d4",
		"--skip-feedback-gate",
		"task123",
	})

	os.Stderr = oldStderr
	w.Close()
	var stderr bytes.Buffer
	io.Copy(&stderr, r)

	if err != nil {
		t.Fatalf("expected --skip-feedback-gate to bypass the check and submit to succeed, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "WARNING") {
		t.Errorf("expected a loud warning on bypass, got stderr: %q", stderr.String())
	}
}

func TestExecuteSubmitFeedbackGateCheckErrorProceeds(t *testing.T) {
	prURL := "https://github.com/owner/repo/pull/42"

	odonianServer := odonianTaskServer(t, "task123", 1, prURL)
	defer odonianServer.Close()

	// No GH token available anywhere, so the feedback check itself fails (not a "found
	// items" result) — this must warn and let the submit through, not block it.
	oldToken := os.Getenv("GH_TOKEN")
	os.Unsetenv("GH_TOKEN")
	defer func() {
		if oldToken != "" {
			os.Setenv("GH_TOKEN", oldToken)
		}
	}()
	t.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), odonianServer.URL, "test-token", []string{
		"--result", "reworked",
		"--pr", prURL,
		"--branch", "mr/a1b2c3d4",
		"task123",
	})
	if err != nil {
		t.Fatalf("expected a check failure to warn and proceed, got blocking error: %v", err)
	}
}
