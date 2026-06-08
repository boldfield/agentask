package tuiclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPromoteTask(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123/promote" {
			t.Errorf("expected /tasks/task123/promote, got %s", r.URL.Path)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testtoken" {
			t.Errorf("expected Bearer testtoken, got %s", auth)
		}

		// Write response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	err := client.PromoteTask(context.Background(), "task123")
	if err != nil {
		t.Fatalf("PromoteTask failed: %v", err)
	}
}

func TestReviewTask(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123/review" {
			t.Errorf("expected /tasks/task123/review, got %s", r.URL.Path)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testtoken" {
			t.Errorf("expected Bearer testtoken, got %s", auth)
		}

		// Verify request body
		var req reviewTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if req.Actor != "test-actor" {
			t.Errorf("expected actor test-actor, got %s", req.Actor)
		}

		if req.Verdict != "approve" {
			t.Errorf("expected verdict approve, got %s", req.Verdict)
		}

		if req.Note == nil || *req.Note != "looks good" {
			t.Errorf("expected note 'looks good', got %v", req.Note)
		}

		// Write response
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test with note
	note := "looks good"
	err := client.ReviewTask(context.Background(), "task123", "test-actor", "approve", &note)
	if err != nil {
		t.Fatalf("ReviewTask failed: %v", err)
	}
}

func TestReviewTaskWithoutNote(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read raw body to verify "note" key is genuinely absent
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read raw body: %v", err)
		}
		bodyStr := string(rawBody)

		// Verify request body via JSON decode
		var req reviewTaskRequest
		if err := json.Unmarshal(rawBody, &req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if req.Note != nil {
			t.Errorf("expected note to be omitted (nil), got %v", req.Note)
		}

		// Verify "note" key is genuinely absent from the raw JSON body
		if strings.Contains(bodyStr, `"note"`) {
			t.Errorf("expected 'note' key to be absent from raw body, but found it: %s", bodyStr)
		}

		// Write response
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test without note (nil)
	err := client.ReviewTask(context.Background(), "task123", "test-actor", "approve", nil)
	if err != nil {
		t.Fatalf("ReviewTask failed: %v", err)
	}
}

func TestTransitionTask(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123/transition" {
			t.Errorf("expected /tasks/task123/transition, got %s", r.URL.Path)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testtoken" {
			t.Errorf("expected Bearer testtoken, got %s", auth)
		}

		// Verify request body
		var req transitionTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if req.To != "done" {
			t.Errorf("expected to=done, got %s", req.To)
		}

		if req.Note == nil || *req.Note != "completed" {
			t.Errorf("expected note 'completed', got %v", req.Note)
		}

		// Write response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test with note
	note := "completed"
	err := client.TransitionTask(context.Background(), "task123", "done", &note)
	if err != nil {
		t.Fatalf("TransitionTask failed: %v", err)
	}
}

func TestTransitionTaskWithoutNote(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read raw body to verify "note" key is genuinely absent
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read raw body: %v", err)
		}
		bodyStr := string(rawBody)

		// Verify request body via JSON decode
		var req transitionTaskRequest
		if err := json.Unmarshal(rawBody, &req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}

		if req.Note != nil {
			t.Errorf("expected note to be omitted (nil), got %v", req.Note)
		}

		// Verify "note" key is genuinely absent from the raw JSON body
		if strings.Contains(bodyStr, `"note"`) {
			t.Errorf("expected 'note' key to be absent from raw body, but found it: %s", bodyStr)
		}

		// Write response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test without note (nil)
	err := client.TransitionTask(context.Background(), "task123", "blocked", nil)
	if err != nil {
		t.Fatalf("TransitionTask failed: %v", err)
	}
}

// TestAPIError_StructuredBody verifies that do() returns *APIError with the correct StatusCode,
// Code, and Message when the server returns a non-2xx with a structured JSON error body.
func TestAPIError_StructuredBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"code":    "CONFLICT",
				"message": "Task is not in backlog",
			},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "testtoken")
	err := client.PromoteTask(context.Background(), "task123")
	if err == nil {
		t.Fatal("Expected error from 409 response, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("Expected StatusCode 409, got %d", apiErr.StatusCode)
	}
	if apiErr.Code != "CONFLICT" {
		t.Errorf("Expected Code CONFLICT, got %q", apiErr.Code)
	}
	if apiErr.Message != "Task is not in backlog" {
		t.Errorf("Expected Message 'Task is not in backlog', got %q", apiErr.Message)
	}
	// Error() string must remain human-readable (used in generic error display).
	if !strings.Contains(err.Error(), "CONFLICT") || !strings.Contains(err.Error(), "Task is not in backlog") {
		t.Errorf("APIError.Error() does not include server code/message: %q", err.Error())
	}
}

// TestAPIError_UndecodableBody verifies that do() returns *APIError with only StatusCode set
// when the server returns a non-2xx with a non-JSON body.
func TestAPIError_UndecodableBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "testtoken")
	err := client.PromoteTask(context.Background(), "task123")
	if err == nil {
		t.Fatal("Expected error from 500 response, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected StatusCode 500, got %d", apiErr.StatusCode)
	}
	// Code and Message should be empty when body is not structured JSON.
	if apiErr.Code != "" {
		t.Errorf("Expected empty Code for undecodable body, got %q", apiErr.Code)
	}
	// Error() must still be useful.
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("APIError.Error() for fallback should include status code, got: %q", err.Error())
	}
}

func TestListEvents(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123/events" {
			t.Errorf("expected /tasks/task123/events, got %s", r.URL.Path)
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer testtoken" {
			t.Errorf("expected Bearer testtoken, got %s", auth)
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		events := []Event{
			{
				ID:        "event1",
				TaskID:    "task123",
				Actor:     "system",
				Kind:      "transition",
				Verdict:   nil,
				Note:      stringPtr("backlog->ready"),
				CreatedAt: "2026-06-07T00:00:00Z",
			},
			{
				ID:        "event2",
				TaskID:    "task123",
				Actor:     "agent-1",
				Kind:      "claim",
				Verdict:   nil,
				Note:      nil,
				CreatedAt: "2026-06-07T00:01:00Z",
			},
			{
				ID:        "event3",
				TaskID:    "task123",
				Actor:     "agent-1",
				Kind:      "submit",
				Verdict:   nil,
				Note:      nil,
				CreatedAt: "2026-06-07T00:02:00Z",
			},
		}
		json.NewEncoder(w).Encode(events)
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	events, err := client.ListEvents(context.Background(), "task123")
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	if events[0].Kind != "transition" {
		t.Errorf("expected first event kind 'transition', got %s", events[0].Kind)
	}

	if events[1].Kind != "claim" {
		t.Errorf("expected second event kind 'claim', got %s", events[1].Kind)
	}

	if events[2].Kind != "submit" {
		t.Errorf("expected third event kind 'submit', got %s", events[2].Kind)
	}
}

func TestListEventsEmpty(t *testing.T) {
	// Create a test server that returns an empty array
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	// Create client
	client := NewHTTPClient(server.URL, "testtoken")

	// Test
	events, err := client.ListEvents(context.Background(), "task123")
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestArchiveTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tasks/task123/archive" {
			t.Errorf("expected /tasks/task123/archive, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "testtoken")
	err := client.ArchiveTask(context.Background(), "task123")
	if err != nil {
		t.Fatalf("ArchiveTask failed: %v", err)
	}
}

func TestArchiveProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/projects/proj123/archive" {
			t.Errorf("expected /projects/proj123/archive, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "testtoken")
	err := client.ArchiveProject(context.Background(), "proj123")
	if err != nil {
		t.Fatalf("ArchiveProject failed: %v", err)
	}
}
