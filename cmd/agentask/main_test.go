package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/boldfield/agentask/internal/forge"
	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestRunNoArgs(t *testing.T) {
	err := run([]string{"agentask"})
	if err != nil {
		t.Errorf("expected no error for bare agentask, got: %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"--help", []string{"agentask", "--help"}},
		{"-h", []string{"agentask", "-h"}},
		{"help", []string{"agentask", "help"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args)
			if err != nil {
				t.Errorf("expected no error for %s, got: %v", tt.name, err)
			}
		})
	}
}

func TestRunServer(t *testing.T) {
	// Note: runServer() will try to start a real server, so we can't test it directly.
	// This test just ensures the routing recognizes "server" as a valid command.
	// In a real test environment, we'd mock runServer().
}

func TestRunUnknownCommand(t *testing.T) {
	// Capture stderr
	stderrBackup := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := run([]string{"agentask", "invalid"})

	w.Close()
	os.Stderr = stderrBackup
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if err == nil {
		t.Error("expected error for unknown command, got nil")
	}

	var handledErr *handledError
	if !errors.As(err, &handledErr) {
		t.Errorf("expected handledError, got %T: %v", err, err)
	}

	if !strings.Contains(stderrOutput, "error: unknown command") {
		t.Errorf("expected stderr to contain 'error: unknown command', got: %s", stderrOutput)
	}

	if !strings.Contains(stderrOutput, "usage: agentask") {
		t.Errorf("expected stderr to contain usage, got: %s", stderrOutput)
	}

	if !strings.Contains(stderrOutput, "projects") {
		t.Errorf("expected stderr to contain verb list with 'projects', got: %s", stderrOutput)
	}
}

func TestSplitJSONFlag(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantJSON bool
		wantRest []string
	}{
		{"trailing json with filters", []string{"--claimable", "--model", "haiku", "--json"}, true, []string{"--claimable", "--model", "haiku"}},
		{"leading json", []string{"--json", "--kind", "review"}, true, []string{"--kind", "review"}},
		{"no json", []string{"--model", "haiku"}, false, []string{"--model", "haiku"}},
		{"only json", []string{"--json"}, true, []string{}},
		{"empty", []string{}, false, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotJSON, gotRest := splitJSONFlag(tc.args)
			if gotJSON != tc.wantJSON {
				t.Errorf("json = %v, want %v", gotJSON, tc.wantJSON)
			}
			if len(gotRest) != len(tc.wantRest) {
				t.Fatalf("rest = %v, want %v", gotRest, tc.wantRest)
			}
			for i := range gotRest {
				if gotRest[i] != tc.wantRest[i] {
					t.Errorf("rest[%d] = %q, want %q", i, gotRest[i], tc.wantRest[i])
				}
			}
		})
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
	err := executeProjects(context.Background(), server.URL, "test-token", false, []string{}, buf)
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
	err := executeProjects(context.Background(), server.URL, "test-token", true, []string{}, buf)
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
	err := executeProjects(context.Background(), "", "test-token", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteProjectsMissingToken(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), "http://localhost:8080", "", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteProjectsWithFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			query := r.URL.Query()
			model := query.Get("model")
			kind := query.Get("kind")
			claimable := query.Get("claimable")

			w.Header().Set("Content-Type", "application/json")
			if model == "haiku" && kind == "implement" && claimable == "true" {
				json.NewEncoder(w).Encode([]tuiclient.Project{
					{ID: "proj-1", Name: "Project 1", Repo: "repo-1", CreatedAt: "2026-01-01T00:00:00Z"},
				})
			} else {
				json.NewEncoder(w).Encode([]tuiclient.Project{})
			}
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeProjects(context.Background(), server.URL, "test-token", false, []string{"--model", "haiku", "--kind", "implement", "--claimable"}, buf)
	if err != nil {
		t.Fatalf("executeProjects with filters failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Project 1") {
		t.Errorf("expected output to contain 'Project 1', got: %s", output)
	}
}

func TestExecuteProjectTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.Project{
				ID:        "proj-1",
				Name:      "Project 1",
				Repo:      "repo-1",
				CreatedAt: "2026-01-01T00:00:00Z",
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeProject(context.Background(), server.URL, "test-token", false, []string{"proj-1"}, buf)
	if err != nil {
		t.Fatalf("executeProject failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ID: proj-1") {
		t.Errorf("expected 'ID: proj-1' in output, got: %s", output)
	}
	if !strings.Contains(output, "Name: Project 1") {
		t.Errorf("expected 'Name: Project 1' in output, got: %s", output)
	}
	if !strings.Contains(output, "Repo: repo-1") {
		t.Errorf("expected 'Repo: repo-1' in output, got: %s", output)
	}
	if !strings.Contains(output, "Created At:") {
		t.Errorf("expected 'Created At:' in output, got: %s", output)
	}
}

func TestExecuteProjectJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.Project{
				ID:        "proj-1",
				Name:      "Project 1",
				Repo:      "repo-1",
				CreatedAt: "2026-01-01T00:00:00Z",
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeProject(context.Background(), server.URL, "test-token", true, []string{"--json", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executeProject with JSON failed: %v", err)
	}

	output := buf.String()
	var result tuiclient.Project
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result.ID != "proj-1" {
		t.Errorf("expected project ID 'proj-1', got %q", result.ID)
	}
	if result.Name != "Project 1" {
		t.Errorf("expected project name 'Project 1', got %q", result.Name)
	}
}

func TestExecuteProjectMissingID(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeProject(context.Background(), "http://localhost:8080", "test-token", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing project id, got nil")
	}
	if !strings.Contains(err.Error(), "project id required") {
		t.Errorf("expected error to mention project id, got: %v", err)
	}
}

func TestExecuteShowTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:           "task-1",
				State:        "in_progress",
				Model:        "opus",
				Kind:         "implement",
				Title:        "Test Task",
				Spec:         "Test spec",
				TargetTaskID: nil,
				Links: []tuiclient.TaskLink{
					{Kind: "pr", Value: "https://github.com/boldfield/agentask/pull/102"},
				},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), server.URL, "test-token", false, []string{"task-1"}, buf)
	if err != nil {
		t.Fatalf("executeShow failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ID: task-1") {
		t.Errorf("expected 'ID: task-1' in output, got: %s", output)
	}
	if !strings.Contains(output, "State: in_progress") {
		t.Errorf("expected 'State: in_progress' in output, got: %s", output)
	}
	if !strings.Contains(output, "Model: opus") {
		t.Errorf("expected 'Model: opus' in output, got: %s", output)
	}
	if !strings.Contains(output, "Kind: implement") {
		t.Errorf("expected 'Kind: implement' in output, got: %s", output)
	}
	if !strings.Contains(output, "Title: Test Task") {
		t.Errorf("expected 'Title: Test Task' in output, got: %s", output)
	}
	if !strings.Contains(output, "Spec: Test spec") {
		t.Errorf("expected 'Spec: Test spec' in output, got: %s", output)
	}
	if !strings.Contains(output, "Links:") {
		t.Errorf("expected 'Links:' in output, got: %s", output)
	}
	if !strings.Contains(output, "pr: https://github.com/boldfield/agentask/pull/102") {
		t.Errorf("expected link in output, got: %s", output)
	}
}

func TestExecuteShowJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-1",
				State: "ready",
				Model: "haiku",
				Kind:  "implement",
				Title: "Test Task",
				Spec:  "Test spec",
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), server.URL, "test-token", true, []string{"--json", "task-1"}, buf)
	if err != nil {
		t.Fatalf("executeShow failed: %v", err)
	}

	output := buf.String()
	var result tuiclient.TaskDetail
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if result.ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", result.ID)
	}
	if result.Title != "Test Task" {
		t.Errorf("expected title 'Test Task', got %q", result.Title)
	}
}

func TestExecuteShowMissingID(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), "http://localhost:8080", "test-token", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task id required") {
		t.Errorf("expected error to mention 'task id required', got: %v", err)
	}
}

func TestExecuteShowMissingURL(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), "", "test-token", false, []string{"task-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteShowMissingToken(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), "http://localhost:8080", "", false, []string{"task-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteShowServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.WriteHeader(http.StatusNotFound)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"error": "task not found"}`)
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeShow(context.Background(), server.URL, "test-token", false, []string{"nonexistent-id"}, buf)
	if err == nil {
		t.Fatal("expected error for non-existent task, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get task") {
		t.Errorf("expected error to mention 'failed to get task', got: %v", err)
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

func TestExecuteSubmitSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/submit" {
			var req struct {
				AgentID string
				Result  string
				Verdict *string
				Links   []struct {
					Kind  string
					Value string
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if req.Result != "implementation done" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(req.Links) != 2 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "implementation done",
		"--pr", "https://github.com/example/repo/pull/1",
		"--branch", "mr/a1b2c3d4",
		"task123",
	})
	if err != nil {
		t.Fatalf("executeSubmit failed: %v", err)
	}
}

func TestExecuteSubmitNoOp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/submit" {
			var req struct {
				AgentID string
				Result  string
				Links   []struct {
					Kind  string
					Value string
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(req.Links) != 1 || req.Links[0].Kind != "no_op" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "already satisfied",
		"--no-op",
		"task123",
	})
	if err != nil {
		t.Fatalf("executeSubmit failed: %v", err)
	}
}

func TestExecuteSubmitVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/submit" {
			var req struct {
				Verdict *string
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if req.Verdict == nil || *req.Verdict != "approve" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "looks good",
		"--verdict", "approve",
		"task123",
	})
	if err != nil {
		t.Fatalf("executeSubmit failed: %v", err)
	}
}

func TestExecuteSubmitNoOpWithPR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "test",
		"--no-op",
		"--pr", "https://github.com/example/repo/pull/1",
		"task123",
	})
	if err == nil {
		t.Fatal("expected error for --no-op with --pr, got nil")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Errorf("expected error to mention conflict, got: %v", err)
	}
}

func TestExecuteSubmitMissingResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing --result, got nil")
	}
	if !strings.Contains(err.Error(), "--result") {
		t.Errorf("expected error to mention --result, got: %v", err)
	}
}

func TestExecuteSubmitMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "test",
	})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("expected error to mention task ID, got: %v", err)
	}
}

func TestExecuteSubmitPRWithoutBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "test",
		"--pr", "https://github.com/example/repo/pull/1",
		"task123",
	})
	if err == nil {
		t.Fatal("expected error for --pr without --branch, got nil")
	}
	if !strings.Contains(err.Error(), "must be provided together") {
		t.Errorf("expected error to mention together, got: %v", err)
	}
}

func TestExecuteSubmitInvalidVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)
	os.Setenv("AGENT_ID", "test-agent")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "test",
		"--verdict", "invalid",
		"task123",
	})
	if err == nil {
		t.Fatal("expected error for invalid verdict, got nil")
	}
	if !strings.Contains(err.Error(), "must be 'approve' or 'reject'") {
		t.Errorf("expected error to mention verdict values, got: %v", err)
	}
}

func TestExecuteSubmitMissingAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAgent := os.Getenv("AGENT_ID")
	defer func() {
		if oldAgent != "" {
			os.Setenv("AGENT_ID", oldAgent)
		} else {
			os.Unsetenv("AGENT_ID")
		}
	}()
	os.Unsetenv("AGENT_ID")

	err := executeSubmit(context.Background(), server.URL, "test-token", []string{
		"--result", "test",
		"task123",
	})
	if err == nil {
		t.Fatal("expected error for missing agent ID, got nil")
	}
	if !strings.Contains(err.Error(), "agent ID is required") {
		t.Errorf("expected error to mention agent ID, got: %v", err)
	}
}

func TestExecuteTasksTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "in_progress", Model: "sonnet", Kind: "review", Title: "Task 2"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), server.URL, "test-token", false, []string{"--project", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "task-1") {
		t.Errorf("expected output to contain 'task-1', got: %s", output)
	}
	if !strings.Contains(output, "task-2") {
		t.Errorf("expected output to contain 'task-2', got: %s", output)
	}
	if !strings.Contains(output, "ID") || !strings.Contains(output, "STATE") {
		t.Errorf("expected table headers in output, got: %s", output)
	}
}

func TestExecuteTasksWithStateFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "in_progress", Model: "sonnet", Kind: "review", Title: "Task 2"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), server.URL, "test-token", false, []string{"--project", "proj-1", "--state", "ready"}, buf)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "task-1") {
		t.Errorf("expected output to contain 'task-1', got: %s", output)
	}
	if strings.Contains(output, "task-2") {
		t.Errorf("expected output to NOT contain 'task-2' (filtered by state), got: %s", output)
	}
}

func TestExecuteTasksWithModelFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "in_progress", Model: "sonnet", Kind: "review", Title: "Task 2"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), server.URL, "test-token", false, []string{"--project", "proj-1", "--model", "sonnet"}, buf)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "task-1") {
		t.Errorf("expected output to NOT contain 'task-1' (filtered by model), got: %s", output)
	}
	if !strings.Contains(output, "task-2") {
		t.Errorf("expected output to contain 'task-2', got: %s", output)
	}
}

func TestExecuteTasksJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), server.URL, "test-token", true, []string{"--project", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}

	output := buf.String()
	var result []tuiclient.Task
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 task in JSON, got %d", len(result))
	}
	if result[0].ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got %q", result[0].ID)
	}
}

func TestExecuteTasksMissingProject(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), "http://localhost:8080", "test-token", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("expected error to mention --project, got: %v", err)
	}
}

func TestExecuteTasksMissingURL(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), "", "test-token", false, []string{"--project", "proj-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteTasksMissingToken(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeTasks(context.Background(), "http://localhost:8080", "", false, []string{"--project", "proj-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecutePendingTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "review", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "approved", Kind: "review", Title: "Task 2"},
				{ID: "task-3", State: "ready", Kind: "implement", Title: "Task 3"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executePending(context.Background(), server.URL, "test-token", false, []string{"--project", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executePending failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "task-1") {
		t.Errorf("expected output to contain 'task-1', got: %s", output)
	}
	if !strings.Contains(output, "task-2") {
		t.Errorf("expected output to contain 'task-2', got: %s", output)
	}
	if strings.Contains(output, "task-3") {
		t.Errorf("expected output to NOT contain 'task-3' (not review/approved), got: %s", output)
	}
	if !strings.Contains(output, "ID") || !strings.Contains(output, "STATE") {
		t.Errorf("expected table headers in output, got: %s", output)
	}
}

func TestExecutePendingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "review", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "approved", Kind: "review", Title: "Task 2"},
				{ID: "task-3", State: "ready", Kind: "implement", Title: "Task 3"},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executePending(context.Background(), server.URL, "test-token", true, []string{"--project", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executePending with JSON failed: %v", err)
	}

	output := buf.String()
	var result []tuiclient.Task
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 tasks in JSON, got %d", len(result))
	}
	if result[0].State != "review" && result[0].State != "approved" {
		t.Errorf("expected task state to be review or approved, got %q", result[0].State)
	}
	if result[1].State != "review" && result[1].State != "approved" {
		t.Errorf("expected task state to be review or approved, got %q", result[1].State)
	}
}

func TestExecutePendingEmptyQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executePending(context.Background(), server.URL, "test-token", false, []string{"--project", "proj-1"}, buf)
	if err != nil {
		t.Fatalf("executePending with empty queue failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ID") || !strings.Contains(output, "STATE") || !strings.Contains(output, "KIND") || !strings.Contains(output, "TITLE") {
		t.Errorf("expected table headers in output for empty queue, got: %s", output)
	}
}

func TestExecutePendingMissingProject(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executePending(context.Background(), "http://localhost:8080", "test-token", false, []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("expected error to mention --project, got: %v", err)
	}
}

func TestExecuteHeartbeatSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/heartbeat" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)

	os.Setenv("AGENT_ID", "test-agent")

	err := executeHeartbeat(context.Background(), server.URL, "test-token", []string{"task123"})
	if err != nil {
		t.Fatalf("executeHeartbeat failed: %v", err)
	}
}

func TestExecuteHeartbeatWithAgentFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/heartbeat" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executeHeartbeat(context.Background(), server.URL, "test-token", []string{"--agent", "flag-agent", "task123"})
	if err != nil {
		t.Fatalf("executeHeartbeat failed: %v", err)
	}
}

func TestExecuteHeartbeatMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)

	os.Setenv("AGENT_ID", "test-agent")

	err := executeHeartbeat(context.Background(), server.URL, "test-token", []string{})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}

	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("expected error to mention task ID, got: %v", err)
	}
}

func TestExecuteHeartbeatMissingAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer func() {
		if oldAgent != "" {
			os.Setenv("AGENT_ID", oldAgent)
		} else {
			os.Unsetenv("AGENT_ID")
		}
	}()

	os.Unsetenv("AGENT_ID")

	err := executeHeartbeat(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing agent ID, got nil")
	}

	if !strings.Contains(err.Error(), "agent ID is required") {
		t.Errorf("expected error to mention agent ID, got: %v", err)
	}
}

func TestExecuteHeartbeatMissingURL(t *testing.T) {
	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)

	os.Setenv("AGENT_ID", "test-agent")

	err := executeHeartbeat(context.Background(), "", "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}

	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteHeartbeatMissingToken(t *testing.T) {
	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)

	os.Setenv("AGENT_ID", "test-agent")

	err := executeHeartbeat(context.Background(), "http://localhost:8080", "", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}

	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteHeartbeatServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/tasks/task123/heartbeat" {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		}
	}))
	defer server.Close()

	// Save current env values
	oldAgent := os.Getenv("AGENT_ID")
	defer os.Setenv("AGENT_ID", oldAgent)

	os.Setenv("AGENT_ID", "test-agent")

	err := executeHeartbeat(context.Background(), server.URL, "test-token", []string{"task123"})
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestExecuteNextSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" && r.URL.Query().Get("claimable") == "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
				{ID: "task-2", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 2"},
			})
		}
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--model", "haiku",
		"--kind", "implement",
	})
	if err != nil {
		t.Fatalf("executeNext failed: %v", err)
	}
}

func TestExecuteNextWithClaim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" && r.URL.Query().Get("claimable") == "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
			})
		} else if r.URL.Path == "/tasks/task-1/claim" {
			w.WriteHeader(http.StatusOK)
		} else if r.URL.Path == "/tasks/task-1" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-1",
				State: "in_progress",
				Model: "haiku",
				Kind:  "implement",
				Title: "Task 1",
			})
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

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--model", "haiku",
		"--kind", "implement",
		"--claim",
	})
	if err != nil {
		t.Fatalf("executeNext with claim failed: %v", err)
	}
}

func TestExecuteNextRaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" && r.URL.Query().Get("claimable") == "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
			})
		} else if r.URL.Path == "/tasks/task-1/claim" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{
					"code":    "already_claimed",
					"message": "already claimed",
				},
			})
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

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--model", "haiku",
		"--kind", "implement",
		"--claim",
	})
	if err == nil {
		t.Fatal("expected error for raced claim, got nil")
	}

	var claimErr *claimError
	if !errors.As(err, &claimErr) {
		t.Fatalf("expected claimError, got %T: %v", err, err)
	}

	if claimErr.code != 2 {
		t.Errorf("expected exit code 2, got %d", claimErr.code)
	}

	if !strings.Contains(claimErr.Error(), "raced") {
		t.Errorf("expected error message to contain 'raced', got: %v", claimErr.Error())
	}
}

func TestExecuteNextNothingClaimable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" && r.URL.Query().Get("claimable") == "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{})
		}
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--model", "haiku",
		"--kind", "implement",
	})
	if err == nil {
		t.Fatal("expected error for nothing claimable, got nil")
	}

	var claimErr *claimError
	if !errors.As(err, &claimErr) {
		t.Fatalf("expected claimError, got %T: %v", err, err)
	}

	if claimErr.code != 2 {
		t.Errorf("expected exit code 2, got %d", claimErr.code)
	}

	if !strings.Contains(claimErr.Error(), "nothing claimable") {
		t.Errorf("expected error message to contain 'nothing claimable', got: %v", claimErr.Error())
	}
}

func TestExecuteNextJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/proj-1/tasks" && r.URL.Query().Get("claimable") == "true" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Task{
				{ID: "task-1", State: "ready", Model: "haiku", Kind: "implement", Title: "Task 1"},
			})
		}
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", true, []string{
		"--project", "proj-1",
		"--model", "haiku",
		"--kind", "implement",
	})
	if err != nil {
		t.Fatalf("executeNext failed: %v", err)
	}
}

func TestExecuteNextMissingProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--model", "haiku",
		"--kind", "implement",
	})
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}

	if !strings.Contains(err.Error(), "--project") {
		t.Errorf("expected error to mention --project, got: %v", err)
	}
}

func TestExecuteNextMissingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// With --model now optional, return empty task list (nothing claimable)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--kind", "implement",
	})
	if err == nil {
		t.Fatal("expected error for nothing claimable, got nil")
	}

	if !strings.Contains(err.Error(), "nothing claimable") {
		t.Errorf("expected error to mention nothing claimable, got: %v", err)
	}
}

func TestExecuteNextMissingKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executeNext(context.Background(), server.URL, "test-token", false, []string{
		"--project", "proj-1",
		"--model", "haiku",
	})
	if err == nil {
		t.Fatal("expected error for missing --kind, got nil")
	}

	if !strings.Contains(err.Error(), "--kind") {
		t.Errorf("expected error to mention --kind, got: %v", err)
	}
}

func TestExecutePromoteSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/promote") {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executePromote(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err != nil {
		t.Fatalf("executePromote failed: %v", err)
	}
}

func TestExecutePromoteMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executePromote(context.Background(), server.URL, "test-token", []string{})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task ID is required") {
		t.Errorf("expected error to mention 'task ID is required', got: %v", err)
	}
}

func TestExecutePromoteMissingURL(t *testing.T) {
	err := executePromote(context.Background(), "", "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecutePromoteMissingToken(t *testing.T) {
	err := executePromote(context.Background(), "http://localhost:8080", "", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecutePromoteServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/promote") {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{
					"code":    "internal_error",
					"message": "server error",
				},
			})
		}
	}))
	defer server.Close()

	err := executePromote(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for failed promote, got nil")
	}
	if !strings.Contains(err.Error(), "failed to promote task") {
		t.Errorf("expected error to mention 'failed to promote task', got: %v", err)
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

// TestParseFlagsWithPositionals_OrderIndependent pins the submit arg-order bug:
// `submit <id> --result x` (id first) must parse --result, not silently drop it.
func TestParseFlagsWithPositionals_OrderIndependent(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantID     string
		wantResult string
		wantErr    bool
	}{
		{"flags-first", []string{"--result", "ok", "TASK1"}, "TASK1", "ok", false},
		{"id-first (the bug)", []string{"TASK1", "--result", "ok"}, "TASK1", "ok", false},
		{"interspersed", []string{"--result", "ok", "TASK1", "--pr", "u"}, "TASK1", "ok", false},
		{"id-only", []string{"TASK1"}, "TASK1", "", false},
		{"no positional", []string{"--result", "ok"}, "", "ok", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("submit", flag.ContinueOnError)
			fs.SetOutput(&bytes.Buffer{})
			result := fs.String("result", "", "")
			fs.String("pr", "", "")

			pos, err := parseFlagsWithPositionals(fs, tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			gotID := ""
			if len(pos) > 0 {
				gotID = pos[0]
			}
			if gotID != tc.wantID {
				t.Errorf("task id = %q, want %q", gotID, tc.wantID)
			}
			if *result != tc.wantResult {
				t.Errorf("--result = %q, want %q", *result, tc.wantResult)
			}
		})
	}
}

// TestParsePRURL tests URL parsing for GitHub PR URLs
func TestParsePRURL(t *testing.T) {
	cases := []struct {
		name       string
		url        string
		wantOwner  string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{"valid github url", "https://github.com/boldfield/agentask/pull/174", "boldfield", "agentask", 174, false},
		{"valid with trailing slash", "https://github.com/boldfield/agentask/pull/174/", "boldfield", "agentask", 174, false},
		{"invalid not github", "https://gitlab.com/boldfield/agentask/pull/174", "", "", 0, true},
		{"invalid path", "https://github.com/boldfield/agentask", "", "", 0, true},
		{"invalid pr number", "https://github.com/boldfield/agentask/pull/abc", "", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, number, err := parsePRURL(tc.url)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr {
				if owner != tc.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tc.wantOwner)
				}
				if repo != tc.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tc.wantRepo)
				}
				if number != tc.wantNumber {
					t.Errorf("number = %d, want %d", number, tc.wantNumber)
				}
			}
		})
	}
}

// TestExecuteMergeSuccess tests the happy path: successful merge and task transitions
func TestExecuteMergeSuccess(t *testing.T) {
	// Create a mock forge server (GitHub API)
	forgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/merge") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"merged": true})
		}
	}))
	defer forgeServer.Close()

	// Temporarily replace the GitHub base URL
	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = forgeServer.URL
	t.Cleanup(func() { forge.GitHubBaseURL = oldBaseURL })

	// Create a mock agentask API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/tasks/") {
			if strings.HasSuffix(r.URL.Path, "/merge-123") {
				// Return merge task with target_task_id pointing to parent
				json.NewEncoder(w).Encode(tuiclient.TaskDetail{
					ID:           "merge-123",
					State:        "approved",
					TargetTaskID: ptrString("parent-456"),
				})
			} else if strings.HasSuffix(r.URL.Path, "/parent-456") {
				// Return parent task
				json.NewEncoder(w).Encode(tuiclient.TaskDetail{
					ID:         "parent-456",
					State:      "approved",
					AgentMerge: true,
					Links: []tuiclient.TaskLink{
						{Kind: "pr", Value: "https://github.com/boldfield/agentask/pull/174"},
					},
				})
			}
		} else if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition") {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer apiServer.Close()

	err := executeMerge(context.Background(), apiServer.URL, "test-token", []string{"merge-123"})
	if err != nil {
		t.Fatalf("executeMerge failed: %v", err)
	}
}

// TestExecuteMergeForgeFails tests handling of forge (GitHub API) failure
func TestExecuteMergeForgeFails(t *testing.T) {
	// Create a mock forge server that fails
	forgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/merge") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("PR is not mergeable"))
		}
	}))
	defer forgeServer.Close()

	// Temporarily replace the GitHub base URL
	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = forgeServer.URL
	t.Cleanup(func() { forge.GitHubBaseURL = oldBaseURL })

	// Create a mock agentask API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/tasks/") {
			if strings.HasSuffix(r.URL.Path, "/merge-123") {
				json.NewEncoder(w).Encode(tuiclient.TaskDetail{
					ID:           "merge-123",
					State:        "approved",
					TargetTaskID: ptrString("parent-456"),
				})
			} else if strings.HasSuffix(r.URL.Path, "/parent-456") {
				json.NewEncoder(w).Encode(tuiclient.TaskDetail{
					ID:         "parent-456",
					State:      "approved",
					AgentMerge: true,
					Links: []tuiclient.TaskLink{
						{Kind: "pr", Value: "https://github.com/boldfield/agentask/pull/174"},
					},
				})
			}
		}
	}))
	defer apiServer.Close()

	err := executeMerge(context.Background(), apiServer.URL, "test-token", []string{"merge-123"})
	if err == nil {
		t.Fatal("expected error for failed forge merge")
	}
	if !strings.Contains(err.Error(), "failed to squash merge PR") {
		t.Errorf("expected 'failed to squash merge PR' in error, got: %v", err)
	}
}

// TestExecuteMergeIdempotent tests that a merge task already in 'done' is a no-op:
// the command returns nil without re-merging or transitioning anything.
func TestExecuteMergeIdempotent(t *testing.T) {
	forgePutCalled := false
	forgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/merge") {
			forgePutCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"merged": true})
		}
	}))
	defer forgeServer.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = forgeServer.URL
	t.Cleanup(func() { forge.GitHubBaseURL = oldBaseURL })

	transitionCalled := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge-123") {
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:           "merge-123",
				State:        "done",
				TargetTaskID: ptrString("parent-456"),
			})
		} else if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition") {
			transitionCalled = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer apiServer.Close()

	// A merge task already in 'done' should be a clean no-op (idempotent).
	err := executeMerge(context.Background(), apiServer.URL, "test-token", []string{"merge-123"})
	if err != nil {
		t.Fatalf("expected no-op success for an already-done merge task, got: %v", err)
	}
	if forgePutCalled {
		t.Error("expected no forge merge call for an already-done merge task")
	}
	if transitionCalled {
		t.Error("expected no transition for an already-done merge task")
	}
}

// TestExecuteMergeFinalizesAfterPartialRun reproduces the zombie-merge bug: a prior
// run merged the PR and advanced the parent to 'done' but died before finalizing the
// merge task, leaving it 'in_progress'. The retry must NOT re-merge (the parent is
// already done) and must finalize the merge task to 'done' instead of erroring.
func TestExecuteMergeFinalizesAfterPartialRun(t *testing.T) {
	forgePutCalled := false
	forgeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/merge") {
			forgePutCalled = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer forgeServer.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = forgeServer.URL
	t.Cleanup(func() { forge.GitHubBaseURL = oldBaseURL })

	mergeTransitionedTo := ""
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge-123") {
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:           "merge-123",
				State:        "in_progress",
				TargetTaskID: ptrString("parent-456"),
			})
		} else if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/parent-456") {
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:         "parent-456",
				State:      "done", // prior run already merged + finalized the parent
				AgentMerge: true,
				Links: []tuiclient.TaskLink{
					{Kind: "pr", Value: "https://github.com/boldfield/agentask/pull/174"},
				},
			})
		} else if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition") {
			var body struct {
				To string `json:"to"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(r.URL.Path, "/merge-123/") {
				mergeTransitionedTo = body.To
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer apiServer.Close()

	err := executeMerge(context.Background(), apiServer.URL, "test-token", []string{"merge-123"})
	if err != nil {
		t.Fatalf("expected partial-run retry to converge, got: %v", err)
	}
	if forgePutCalled {
		t.Error("expected no re-merge when parent is already done")
	}
	if mergeTransitionedTo != "done" {
		t.Errorf("expected merge task transitioned to done, got %q", mergeTransitionedTo)
	}
}

func TestExecuteDiffWithPR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-1",
				State: "in_progress",
				Links: []tuiclient.TaskLink{
					{Kind: "pr", Value: "https://github.com/boldfield/agentask/pull/123"},
				},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeDiff(context.Background(), server.URL, "test-token", []string{"task-1"}, buf)
	if err != nil {
		t.Fatalf("executeDiff failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "https://github.com/boldfield/agentask/pull/123") {
		t.Errorf("expected PR URL in output, got: %s", output)
	}
}

func TestExecuteDiffNoPR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-1",
				State: "in_progress",
				Links: []tuiclient.TaskLink{},
			})
		}
	}))
	defer server.Close()

	buf := &bytes.Buffer{}
	err := executeDiff(context.Background(), server.URL, "test-token", []string{"task-1"}, buf)
	if err == nil {
		t.Fatal("expected error for task with no PR link, got nil")
	}
	if !strings.Contains(err.Error(), "no pull request link") {
		t.Errorf("expected error to mention 'no pull request link', got: %v", err)
	}
}

func TestExecuteDiffMissingID(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeDiff(context.Background(), "http://localhost:8080", "test-token", []string{}, buf)
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task id required") {
		t.Errorf("expected error to mention 'task id required', got: %v", err)
	}
}

func TestExecuteDiffMissingURL(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeDiff(context.Background(), "", "test-token", []string{"task-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteDiffMissingToken(t *testing.T) {
	buf := &bytes.Buffer{}
	err := executeDiff(context.Background(), "http://localhost:8080", "", []string{"task-1"}, buf)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteApproveSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "approved",
				Title: "Test Task",
			})
		} else if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/transition") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executeApprove(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err != nil {
		t.Fatalf("executeApprove failed: %v", err)
	}
}

func TestExecuteApproveWrongState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "ready",
				Title: "Test Task",
			})
		}
	}))
	defer server.Close()

	err := executeApprove(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for task not in approved state, got nil")
	}
	if !strings.Contains(err.Error(), "expected approved") {
		t.Errorf("expected error to mention 'expected approved', got: %v", err)
	}
}

func TestExecuteApproveMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executeApprove(context.Background(), server.URL, "test-token", []string{})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task id required") {
		t.Errorf("expected error to mention 'task id required', got: %v", err)
	}
}

func TestExecuteApproveMissingURL(t *testing.T) {
	err := executeApprove(context.Background(), "", "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteApproveMissingToken(t *testing.T) {
	err := executeApprove(context.Background(), "http://localhost:8080", "", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteApproveServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}
	}))
	defer server.Close()

	err := executeApprove(context.Background(), server.URL, "test-token", []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get task") {
		t.Errorf("expected error to mention 'failed to get task', got: %v", err)
	}
}

// ptrString returns a pointer to a string
func ptrString(s string) *string {
	return &s
}

func TestExecuteRejectFromReviewSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "review",
				Title: "Test Task",
			})
		} else if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/transition") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"task-123", "--note", "needs rework"})
	if err != nil {
		t.Fatalf("executeReject failed: %v", err)
	}
}

func TestExecuteRejectFromApprovedSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "approved",
				Title: "Test Task",
			})
		} else if strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/transition") && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"task-123", "--note", "rejected"})
	if err != nil {
		t.Fatalf("executeReject failed: %v", err)
	}
}

func TestExecuteRejectMissingNote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "review",
				Title: "Test Task",
			})
		}
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"task-123"})
	if err == nil {
		t.Fatal("expected error for missing --note, got nil")
	}
	if !strings.Contains(err.Error(), "--note") {
		t.Errorf("expected error to mention '--note', got: %v", err)
	}
}

func TestExecuteRejectWrongState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tuiclient.TaskDetail{
				ID:    "task-123",
				State: "ready",
				Title: "Test Task",
			})
		}
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"task-123", "--note", "bad state"})
	if err == nil {
		t.Fatal("expected error for task not in review or approved state, got nil")
	}
	if !strings.Contains(err.Error(), "expected review or approved") {
		t.Errorf("expected error to mention 'expected review or approved', got: %v", err)
	}
}

func TestExecuteRejectMissingTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"--note", "some note"})
	if err == nil {
		t.Fatal("expected error for missing task ID, got nil")
	}
	if !strings.Contains(err.Error(), "task id required") {
		t.Errorf("expected error to mention 'task id required', got: %v", err)
	}
}

func TestExecuteRejectMissingURL(t *testing.T) {
	err := executeReject(context.Background(), "", "test-token", []string{"task-123", "--note", "test"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}
}

func TestExecuteRejectMissingToken(t *testing.T) {
	err := executeReject(context.Background(), "http://localhost:8080", "", []string{"task-123", "--note", "test"})
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}
	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

func TestExecuteRejectServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}
	}))
	defer server.Close()

	err := executeReject(context.Background(), server.URL, "test-token", []string{"nonexistent", "--note", "test"})
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to get task") {
		t.Errorf("expected error to mention 'failed to get task', got: %v", err)
	}
}
