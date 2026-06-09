package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestResolveAgentID(t *testing.T) {
	tests := []struct {
		name          string
		agentFlag     string
		envAgent      string
		expectedAgent string
		expectError   bool
		errorContains string
	}{
		{
			name:          "flag wins",
			agentFlag:     "flag-agent",
			envAgent:      "env-agent",
			expectedAgent: "flag-agent",
		},
		{
			name:          "fallback to env when flag empty",
			agentFlag:     "",
			envAgent:      "env-agent",
			expectedAgent: "env-agent",
		},
		{
			name:          "error when both missing",
			agentFlag:     "",
			envAgent:      "",
			expectError:   true,
			errorContains: "agent ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldAgent := os.Getenv("AGENT_ID")
			defer func() {
				os.Setenv("AGENT_ID", oldAgent)
			}()

			if tt.envAgent != "" {
				os.Setenv("AGENT_ID", tt.envAgent)
			} else {
				os.Unsetenv("AGENT_ID")
			}

			agent, err := resolveAgentID(tt.agentFlag)

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
			}
		})
	}
}

func TestExecuteHeartbeat(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		envAgent    string
		serverError bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "heartbeat with task ID and agent flag",
			args:        []string{"task-123", "--agent", "agent-xyz"},
			expectError: false,
		},
		{
			name:        "heartbeat with task ID from env",
			args:        []string{"task-123"},
			envAgent:    "env-agent",
			expectError: false,
		},
		{
			name:        "heartbeat missing task ID",
			args:        []string{},
			expectError: true,
			errorMsg:    "task ID is required",
		},
		{
			name:        "heartbeat missing agent ID",
			args:        []string{"task-123"},
			envAgent:    "",
			expectError: true,
			errorMsg:    "agent ID is required",
		},
		{
			name:        "heartbeat server error",
			args:        []string{"task-123", "--agent", "agent-xyz"},
			serverError: true,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldAgent := os.Getenv("AGENT_ID")
			defer func() {
				os.Setenv("AGENT_ID", oldAgent)
			}()

			if tt.envAgent != "" {
				os.Setenv("AGENT_ID", tt.envAgent)
			} else {
				os.Unsetenv("AGENT_ID")
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.serverError {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/heartbeat") {
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			err := executeHeartbeat(context.Background(), server.URL, "test-token", tt.args)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}
