package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
