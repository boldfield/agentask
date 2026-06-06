package tuiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the interface for TUI interactions with the Agentask API.
type Client interface {
	ListProjects(ctx context.Context) ([]Project, error)
	ListTasks(ctx context.Context, projectID string) ([]Task, error)
	GetTask(ctx context.Context, id string) (TaskDetail, error)
	ListDocuments(ctx context.Context, projectID string) ([]Document, error)
	PromoteTask(ctx context.Context, id string) error
	ReviewTask(ctx context.Context, id, actor, verdict string, note *string) error
	TransitionTask(ctx context.Context, id, to string, note *string) error
}

// Response structs for the TUI client (distinct from internal/store)

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	CreatedAt string `json:"created_at"`
}

type Task struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	DocumentID     string `json:"document_id"`
	Title          string `json:"title"`
	Spec           string `json:"spec"`
	State          string `json:"state"`
	Assignee       *string `json:"assignee"`
	LeaseExpiresAt *string `json:"lease_expires_at"`
	Result         *string `json:"result"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type TaskDetail struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	DocumentID     string     `json:"document_id"`
	Title          string     `json:"title"`
	Spec           string     `json:"spec"`
	State          string     `json:"state"`
	Assignee       *string    `json:"assignee"`
	LeaseExpiresAt *string    `json:"lease_expires_at"`
	Result         *string    `json:"result"`
	CreatedAt      string     `json:"created_at"`
	UpdatedAt      string     `json:"updated_at"`
	DependsOn      []string   `json:"depends_on"`
	Links          []TaskLink `json:"links"`
}

type TaskLink struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type Document struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Ref       string `json:"ref"`
	Commit    *string `json:"commit"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// HTTPClient implements the Client interface.
type HTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewHTTPClient creates a new HTTP client for the Agentask API.
func NewHTTPClient(baseURL, token string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		token:   token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// errorResponse represents the structured error response from the server.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do performs an HTTP request with bearer token authentication.
// On non-2xx status, it reads and returns the server's structured error if available.
func (c *HTTPClient) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	url := c.baseURL + path
	var req *http.Request
	var err error

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return resp, err
	}

	// On non-2xx status, try to read and decode the structured error body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Try to decode the error response
		var errResp errorResponse
		if readErr == nil && len(bodyBytes) > 0 {
			if unmarshalErr := json.Unmarshal(bodyBytes, &errResp); unmarshalErr == nil && errResp.Error.Message != "" {
				return nil, fmt.Errorf("server error (%s): %s", errResp.Error.Code, errResp.Error.Message)
			}
		}

		// Fallback to generic error if we couldn't decode the body
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return resp, nil
}

// ListProjects fetches all projects.
func (c *HTTPClient) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := c.do(ctx, "GET", "/projects", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var projects []Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return projects, nil
}

// ListTasks fetches all tasks for a project.
func (c *HTTPClient) ListTasks(ctx context.Context, projectID string) ([]Task, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/projects/%s/tasks", projectID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tasks []Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tasks, nil
}

// GetTask fetches a single task with full details including dependencies and links.
func (c *HTTPClient) GetTask(ctx context.Context, id string) (TaskDetail, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/tasks/%s", id), nil)
	if err != nil {
		return TaskDetail{}, err
	}
	defer resp.Body.Close()

	var task TaskDetail
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return TaskDetail{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return task, nil
}

// ListDocuments fetches all documents for a project.
func (c *HTTPClient) ListDocuments(ctx context.Context, projectID string) ([]Document, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/projects/%s/documents", projectID), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var docs []Document
	if err := json.NewDecoder(resp.Body).Decode(&docs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return docs, nil
}

// PromoteTask promotes a backlog task to ready.
func (c *HTTPClient) PromoteTask(ctx context.Context, id string) error {
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/tasks/%s/promote", id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// reviewTaskRequest is the request body for ReviewTask.
type reviewTaskRequest struct {
	Actor   string  `json:"actor"`
	Verdict string  `json:"verdict"`
	Note    *string `json:"note,omitempty"`
}

// ReviewTask posts a review verdict on a task.
func (c *HTTPClient) ReviewTask(ctx context.Context, id, actor, verdict string, note *string) error {
	body := reviewTaskRequest{
		Actor:   actor,
		Verdict: verdict,
		Note:    note,
	}

	resp, err := c.do(ctx, "POST", fmt.Sprintf("/tasks/%s/review", id), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// transitionTaskRequest is the request body for TransitionTask.
type transitionTaskRequest struct {
	To   string  `json:"to"`
	Note *string `json:"note,omitempty"`
}

// TransitionTask moves a task to a new state.
func (c *HTTPClient) TransitionTask(ctx context.Context, id, to string, note *string) error {
	body := transitionTaskRequest{
		To:   to,
		Note: note,
	}

	resp, err := c.do(ctx, "POST", fmt.Sprintf("/tasks/%s/transition", id), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
