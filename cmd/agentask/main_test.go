package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestParseCommandServer(t *testing.T) {
	isClient, _ := parseCommand([]string{"agentask", "server"})
	if isClient {
		t.Error("expected server command, got client")
	}
}

func TestParseCommandDefault(t *testing.T) {
	isClient, _ := parseCommand([]string{"agentask"})
	if isClient {
		t.Error("expected server command (default), got client")
	}
}

func TestParseCommandClient(t *testing.T) {
	isClient, verb := parseCommand([]string{"agentask", "projects"})
	if !isClient {
		t.Error("expected client command, got server")
	}
	if verb != "projects" {
		t.Errorf("expected verb 'projects', got %q", verb)
	}
}

func TestExecuteProjectsTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Project{
				{ID: "proj-1", Name: "Project 1", Repo: "repo-1", CreatedAt: "2026-01-01T00:00:00Z"},
				{ID: "proj-2", Name: "Project 2", Repo: "repo-2", CreatedAt: "2026-01-02T00:00:00Z"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), server.URL, "test-token", false, buf)
	if err != nil {
		t.Fatalf("executeProjects failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Project 1") {
		t.Errorf("expected output to contain 'Project 1', got: %s", output)
	}
	if !strings.Contains(output, "Project 2") {
		t.Errorf("expected output to contain 'Project 2', got: %s", output)
	}
	if !strings.Contains(output, "ID") || !strings.Contains(output, "NAME") {
		t.Errorf("expected table headers in output, got: %s", output)
	}
}

func TestExecuteProjectsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Project{
				{ID: "proj-1", Name: "Project 1", Repo: "repo-1", CreatedAt: "2026-01-01T00:00:00Z"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), server.URL, "test-token", true, buf)
	if err != nil {
		t.Fatalf("executeProjects failed: %v", err)
	}

	output := buf.String()
	var result []tuiclient.Project
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 project in JSON, got %d", len(result))
	}
	if result[0].Name != "Project 1" {
		t.Errorf("expected project name 'Project 1', got %q", result[0].Name)
	}
}

func TestExecuteProjectsMissingURL(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), "", "test-token", false, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteProjectsMissingToken(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), "http://localhost:8080", "", false, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestDoNextNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/projects/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{})
		}
	}))
	defer server.Close()

	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	result := doNext(context.Background(), server.URL, "test-token", false,
		[]string{"--project", "proj-123", "--model", "haiku", "--kind", "implement"},
		client)

	if result.exitCode != 2 {
		t.Errorf("expected exit code 2 for nothing claimable, got %d", result.exitCode)
	}
	if !strings.Contains(result.stdout, "nothing claimable") {
		t.Errorf("expected 'nothing claimable' in stdout, got: %q", result.stdout)
	}
}

func TestDoNextFindsTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/projects/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-123", Title: "Test Task", State: "ready", Kind: "implement", Model: "haiku"},
				{ID: "task-456", Title: "Another Task", State: "ready", Kind: "implement", Model: "haiku"},
			})
		}
	}))
	defer server.Close()

	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	result := doNext(context.Background(), server.URL, "test-token", false,
		[]string{"--project", "proj-123", "--model", "haiku", "--kind", "implement"},
		client)

	if result.exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.exitCode)
	}
	if !strings.Contains(result.stdout, "task-123") {
		t.Errorf("expected 'task-123' in stdout, got: %q", result.stdout)
	}
}

func TestDoNextClaimsTask(t *testing.T) {
	claimCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/projects/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-123", Title: "Test Task", State: "ready", Kind: "implement", Model: "haiku"},
			})
		}
		if strings.HasSuffix(r.URL.Path, "/claim") {
			claimCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"id": "task-123"})
		}
	}))
	defer server.Close()

	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	result := doNext(context.Background(), server.URL, "test-token", false,
		[]string{"--project", "proj-123", "--model", "haiku", "--kind", "implement", "--claim"},
		client)

	if result.exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.exitCode)
	}
	if !claimCalled {
		t.Errorf("expected claim to be called")
	}
}

func TestDoNextRacedClaim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/projects/") && strings.HasSuffix(r.URL.Path, "/tasks") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-123", Title: "Test Task", State: "ready", Kind: "implement", Model: "haiku"},
			})
		}
		if strings.HasSuffix(r.URL.Path, "/claim") {
			w.WriteHeader(http.StatusConflict)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{"code": "CONFLICT", "message": "Task is not claimable"},
			})
		}
	}))
	defer server.Close()

	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	result := doNext(context.Background(), server.URL, "test-token", false,
		[]string{"--project", "proj-123", "--model", "haiku", "--kind", "implement", "--claim"},
		client)

	if result.exitCode != 3 {
		t.Errorf("expected exit code 3 for raced claim, got %d", result.exitCode)
	}
	if !strings.Contains(result.stdout, "raced, none claimed") {
		t.Errorf("expected 'raced, none claimed' in stdout, got: %q", result.stdout)
	}
}

func TestDoNextMissingProject(t *testing.T) {
	result := doNext(context.Background(), "http://localhost:8080", "test-token", false,
		[]string{"--model", "haiku", "--kind", "implement"},
		nil)

	if result.exitCode != 1 {
		t.Errorf("expected exit code 1 for missing project, got %d", result.exitCode)
	}
	if !strings.Contains(result.stderr, "--project is required") {
		t.Errorf("expected '--project is required' in stderr, got: %q", result.stderr)
	}
}

func TestExecuteTransitionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/transition") {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executeTransition(context.Background(), server.URL, "test-token", []string{"task-123", "--to", "blocked", "--note", "test note"})
	if err != nil {
		t.Fatalf("executeTransition failed: %v", err)
	}
}

func TestExecuteTransitionMissingTo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executeTransition(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing --to, got nil")
	}
	if !strings.Contains(err.Error(), "--to") {
		t.Errorf("expected error to mention --to, got: %v", err)
	}
}

func TestExecuteTransitionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"code":    "internal_error",
				"message": "server error",
			},
		})
	}))
	defer server.Close()

	err := executeTransition(context.Background(), server.URL, "test-token", []string{"task-123", "--to", "blocked"})
	if err == nil {
		t.Fatal("expected error for failed transition, got nil")
	}
}

func TestExecuteClaimSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/claim" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Setenv("AGENT_ID", "test-agent")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), server.URL, "test-token", []string{"task123"})
	if err != nil {
		t.Fatalf("executeClaim failed: %v", err)
	}
}

func TestExecuteClaimAlreadyClaimed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/claim" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "already claimed"})
		}
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Setenv("AGENT_ID", "test-agent")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var claimErr *claimError
	if !errors.As(err, &claimErr) {
		t.Fatalf("expected claimError, got %T: %v", err, err)
	}

	if claimErr.code != 3 {
		t.Errorf("expected exit code 3, got %d", claimErr.code)
	}

	if !strings.Contains(claimErr.Error(), "already claimed") {
		t.Errorf("expected error message to contain 'already claimed', got: %v", claimErr.Error())
	}
}

func TestExecuteClaimMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Setenv("AGENT_ID", "test-agent")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), server.URL, "test-token", []string{})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}

	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("expected error to mention task ID, got: %v", err)
	}
}

func TestExecuteClaimMissingAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Unsetenv("AGENT_ID")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing agent ID, got nil")
	}

	if !strings.Contains(err.Error(), "agent ID is required") {
		t.Errorf("expected error to mention agent ID, got: %v", err)
	}
}

func TestExecuteClaimMissingURL(t *testing.T) {
	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Setenv("AGENT_ID", "test-agent")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), "", "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}

	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteClaimServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/claim" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		}
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	oldModel := os.Getenv("AGENT_MODEL")
	defer func() {
		os.Setenv("AGENT_ID", oldAgent)
		os.Setenv("AGENT_MODEL", oldModel)
	}()

	os.Setenv("AGENT_ID", "test-agent")
	os.Setenv("AGENT_MODEL", "haiku")

	err := executeClaim(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestResolveAgentIdentity(t *testing.T) {
	tests := []struct {
		name          string
		agentFlag     string
		modelFlag     string
		envAgent      string
		envModel      string
		expectedAgent string
		expectedModel string
		expectError   bool
		errorContains string
	}{
		{
			name:          "flag wins",
			agentFlag:     "flag-agent",
			modelFlag:     "flag-model",
			envAgent:      "env-agent",
			envModel:      "env-model",
			expectedAgent: "flag-agent",
			expectedModel: "flag-model",
		},
		{
			name:          "agent flag wins over env",
			agentFlag:     "flag-agent",
			modelFlag:     "",
			envAgent:      "env-agent",
			envModel:      "env-model",
			expectedAgent: "flag-agent",
			expectedModel: "env-model",
		},
		{
			name:          "model flag wins over env",
			agentFlag:     "",
			modelFlag:     "flag-model",
			envAgent:      "env-agent",
			envModel:      "env-model",
			expectedAgent: "env-agent",
			expectedModel: "flag-model",
		},
		{
			name:          "fallback to env when flags empty",
			agentFlag:     "",
			modelFlag:     "",
			envAgent:      "env-agent",
			envModel:      "env-model",
			expectedAgent: "env-agent",
			expectedModel: "env-model",
		},
		{
			name:          "error when agent ID missing",
			agentFlag:     "",
			modelFlag:     "flag-model",
			envAgent:      "",
			envModel:      "env-model",
			expectError:   true,
			errorContains: "agent ID is required",
		},
		{
			name:          "error when model missing",
			agentFlag:     "flag-agent",
			modelFlag:     "",
			envAgent:      "env-agent",
			envModel:      "",
			expectError:   true,
			errorContains: "model is required",
		},
		{
			name:          "error when both missing",
			agentFlag:     "",
			modelFlag:     "",
			envAgent:      "",
			envModel:      "",
			expectError:   true,
			errorContains: "agent ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save current env values
			oldAgent := os.Getenv("AGENT_ID")
			oldModel := os.Getenv("AGENT_MODEL")
			defer func() {
				os.Setenv("AGENT_ID", oldAgent)
				os.Setenv("AGENT_MODEL", oldModel)
			}()

			// Set test env values
			if tt.envAgent != "" {
				os.Setenv("AGENT_ID", tt.envAgent)
			} else {
				os.Unsetenv("AGENT_ID")
			}
			if tt.envModel != "" {
				os.Setenv("AGENT_MODEL", tt.envModel)
			} else {
				os.Unsetenv("AGENT_MODEL")
			}

			// Call function
			agent, model, err := resolveAgentIdentity(tt.agentFlag, tt.modelFlag)

			// Check error
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if agent != tt.expectedAgent {
					t.Errorf("expected agent %q, got %q", tt.expectedAgent, agent)
				}
				if model != tt.expectedModel {
					t.Errorf("expected model %q, got %q", tt.expectedModel, model)
				}
			}
		})
	}
}
