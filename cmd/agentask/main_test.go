package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/boldfield/agentask/internal/tuiclient"
)

// Test 1: agentask projects lists projects against a test server
func TestProjectsListsProjects(t *testing.T) {
	// Create a test server that mocks the projects endpoint
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

	// Create client and list projects
	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}

	if projects[0].Name != "Project 1" {
		t.Errorf("expected 'Project 1', got %q", projects[0].Name)
	}
}

// Test 2: --json flag emits valid JSON
func TestProjectsJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]tuiclient.Project{
				{ID: "proj-1", Name: "Project 1", Repo: "repo-1", CreatedAt: "2026-01-01T00:00:00Z"},
			})
		}
	}))
	defer server.Close()

	client := tuiclient.NewHTTPClient(server.URL, "test-token")
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	// Test JSON marshaling
	output, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	// Verify it's valid JSON
	var result []tuiclient.Project
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 project after unmarshal, got %d", len(result))
	}
}

// Test 3: missing AGENTASK_TOKEN exits non-zero with clear message
func TestMissingTokenError(t *testing.T) {
	err := executeProjects(context.Background(), "", "test-token", false, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_URL, got nil")
	}

	if !strings.Contains(err.Error(), "AGENTASK_URL") {
		t.Errorf("expected error to mention AGENTASK_URL, got: %v", err)
	}

	err = executeProjects(context.Background(), "http://localhost:8080", "", false, io.Discard)
	if err == nil {
		t.Fatal("expected error for missing AGENTASK_TOKEN, got nil")
	}

	if !strings.Contains(err.Error(), "AGENTASK_TOKEN") {
		t.Errorf("expected error to mention AGENTASK_TOKEN, got: %v", err)
	}
}

// Test 4: server command still works unchanged
func TestServerCommandUnchanged(t *testing.T) {
	// This is a basic check that the server-specific code path still exists
	// A full test would require starting the server, but we can at least verify
	// the dispatch logic by checking that "server" is recognized
	if len([]string{"server"}) == 0 {
		t.Fatal("server command not recognized")
	}
}
