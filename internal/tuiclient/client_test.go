package tuiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProjects(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/projects" {
			t.Errorf("expected /projects, got %s", r.URL.Path)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testtoken" {
			t.Errorf("expected Bearer testtoken, got %s", auth)
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		projects := []Project{
			{ID: "proj1", Name: "Project 1", Repo: "repo1", CreatedAt: "2024-01-01T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(projects)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}

	if projects[0].ID != "proj1" {
		t.Errorf("expected ID proj1, got %s", projects[0].ID)
	}
}

func TestListTasks(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/projects/proj123/tasks" {
			t.Errorf("expected /projects/proj123/tasks, got %s", r.URL.Path)
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		tasks := []Task{
			{
				ID:        "task1",
				ProjectID: "proj123",
				Title:     "Task 1",
				State:     "backlog",
				CreatedAt: "2024-01-01T00:00:00Z",
				UpdatedAt: "2024-01-01T00:00:00Z",
			},
		}
		json.NewEncoder(w).Encode(tasks)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	tasks, err := client.ListTasks(context.Background(), "proj123")
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	}

	if tasks[0].ID != "task1" {
		t.Errorf("expected ID task1, got %s", tasks[0].ID)
	}
}

func TestGetTask(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123" {
			t.Errorf("expected /tasks/task123, got %s", r.URL.Path)
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		task := TaskDetail{
			ID:        "task123",
			ProjectID: "proj1",
			Title:     "Task 1",
			Spec:      "Do something",
			State:     "backlog",
			DependsOn: []string{},
			Links:     []TaskLink{},
			CreatedAt: "2024-01-01T00:00:00Z",
			UpdatedAt: "2024-01-01T00:00:00Z",
		}
		json.NewEncoder(w).Encode(task)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	task, err := client.GetTask(context.Background(), "task123")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.ID != "task123" {
		t.Errorf("expected ID task123, got %s", task.ID)
	}
}

func TestListDocuments(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/projects/proj123/documents" {
			t.Errorf("expected /projects/proj123/documents, got %s", r.URL.Path)
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		docs := []Document{
			{
				ID:        "doc1",
				ProjectID: "proj123",
				Kind:      "design",
				Title:     "Design Doc",
				Ref:       "docs/design.md",
				CreatedAt: "2024-01-01T00:00:00Z",
				UpdatedAt: "2024-01-01T00:00:00Z",
			},
		}
		json.NewEncoder(w).Encode(docs)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	docs, err := client.ListDocuments(context.Background(), "proj123")
	if err != nil {
		t.Fatalf("ListDocuments failed: %v", err)
	}

	if len(docs) != 1 {
		t.Errorf("expected 1 document, got %d", len(docs))
	}

	if docs[0].ID != "doc1" {
		t.Errorf("expected ID doc1, got %s", docs[0].ID)
	}
}
