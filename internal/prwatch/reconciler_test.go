package prwatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/forge"
	"github.com/boldfield/agentask/internal/notify"
	"github.com/boldfield/agentask/internal/store"
)

type fakeTaskSource struct {
	projects             []store.Project
	projectsErr          error
	tasks                map[string][]store.Task
	tasksErr             error
	taskWithDepsAndLinks map[string]store.TaskWithDepsAndLinks
	getTaskErrs          map[string]error
	transitionCalls      []transitionCall
	transitionErr        error
}

type transitionCall struct {
	taskID  string
	toState string
	note    *string
}

func (f *fakeTaskSource) ListProjects(ctx context.Context, filter store.ProjectListFilter) ([]store.Project, error) {
	return f.projects, f.projectsErr
}

func (f *fakeTaskSource) ListTasks(ctx context.Context, projectID string, filter store.TaskListFilter) ([]store.Task, error) {
	if f.tasksErr != nil {
		return nil, f.tasksErr
	}
	return f.tasks[projectID], nil
}

func (f *fakeTaskSource) GetTask(ctx context.Context, id string) (store.TaskWithDepsAndLinks, error) {
	if err, exists := f.getTaskErrs[id]; exists {
		return store.TaskWithDepsAndLinks{}, err
	}
	return f.taskWithDepsAndLinks[id], nil
}

func (f *fakeTaskSource) TransitionTask(ctx context.Context, taskID, to string, note *string) (store.Task, error) {
	f.transitionCalls = append(f.transitionCalls, transitionCall{
		taskID:  taskID,
		toState: to,
		note:    note,
	})
	return store.Task{}, f.transitionErr
}

type fakeNotifierForReconciler struct {
	publishCalls []notify.Notification
	publishErr   error
}

func (f *fakeNotifierForReconciler) Publish(ctx context.Context, n notify.Notification) error {
	f.publishCalls = append(f.publishCalls, n)
	return f.publishErr
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestReconcilerName(t *testing.T) {
	ts := &fakeTaskSource{}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, nil)

	if reconciler.Name() != "pr-watch" {
		t.Errorf("expected name 'pr-watch', got %q", reconciler.Name())
	}
}

func TestReconcileActionDone(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				Title:     "Test Task",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/1"},
				},
			},
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	var getStateCalled bool
	getPRState := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
		getStateCalled = true
		return "merged", nil
	}

	getReviewDecision := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error) {
		return "approved", time.Time{}, nil
	}

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	reconciler.getPRState = getPRState
	reconciler.getReviewDecision = getReviewDecision

	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !getStateCalled {
		t.Fatal("getPRState was not called")
	}

	if len(ts.transitionCalls) != 1 {
		t.Errorf("expected 1 transition call, got %d", len(ts.transitionCalls))
	}

	if ts.transitionCalls[0].taskID != "task-1" || ts.transitionCalls[0].toState != "done" {
		t.Errorf("expected transition to done, got %v", ts.transitionCalls[0])
	}

	if len(notifier.publishCalls) != 1 {
		t.Errorf("expected 1 notification, got %d", len(notifier.publishCalls))
	}

	if notifier.publishCalls[0].Event != "agentask-merged" {
		t.Errorf("expected event 'agentask-merged', got %q", notifier.publishCalls[0].Event)
	}
}

func TestReconcileActionAbandon(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				Title:     "Test Task",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/1"},
				},
			},
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	getPRState := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
		return "closed", nil
	}

	getReviewDecision := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error) {
		return "pending", time.Time{}, nil
	}

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	reconciler.getPRState = getPRState
	reconciler.getReviewDecision = getReviewDecision

	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ts.transitionCalls) != 1 {
		t.Errorf("expected 1 transition call, got %d", len(ts.transitionCalls))
	}

	call := ts.transitionCalls[0]
	if call.taskID != "task-1" || call.toState != "abandoned" {
		t.Errorf("expected transition to abandoned, got %v", call)
	}

	if call.note == nil || *call.note != "PR closed without merging" {
		t.Errorf("expected note 'PR closed without merging', got %v", call.note)
	}
}

func TestReconcileActionBounce(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				Title:     "Test Task",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/1"},
				},
			},
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	getPRState := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
		return "open", nil
	}

	latestReviewAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	getReviewDecision := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error) {
		return "changes_requested", latestReviewAt, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = server.URL
	defer func() {
		forge.GitHubBaseURL = oldBaseURL
	}()

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	reconciler.getPRState = getPRState
	reconciler.getReviewDecision = getReviewDecision

	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ts.transitionCalls) != 1 {
		t.Errorf("expected 1 transition call, got %d", len(ts.transitionCalls))
	}

	call := ts.transitionCalls[0]
	if call.taskID != "task-1" || call.toState != "ready" {
		t.Errorf("expected transition to ready, got %v", call)
	}

	if call.note == nil || *call.note != "changes requested — bouncing back to ready for rework" {
		t.Errorf("expected note 'changes requested — bouncing back to ready for rework', got %v", call.note)
	}
}

func TestReconcileActionNoop(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				Title:     "Test Task",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/1"},
				},
			},
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	getPRState := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
		return "open", nil
	}

	getReviewDecision := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error) {
		return "pending", time.Time{}, nil
	}

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	reconciler.getPRState = getPRState
	reconciler.getReviewDecision = getReviewDecision

	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ts.transitionCalls) != 0 {
		t.Errorf("expected 0 transition calls (noop), got %d", len(ts.transitionCalls))
	}

	if len(notifier.publishCalls) != 0 {
		t.Errorf("expected 0 notifications (noop), got %d", len(notifier.publishCalls))
	}
}

func TestReconcileSkipAgentMerge(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: true,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ts.transitionCalls) != 0 {
		t.Errorf("expected 0 transition calls (skipped due to AgentMerge), got %d", len(ts.transitionCalls))
	}
}

func TestReconcileSkipNoPRLink(t *testing.T) {
	ctx := context.Background()
	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					Title:      "Test Task",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				Title:     "Test Task",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "branch", Value: "https://github.com/owner/repo"},
				},
			},
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	err := reconciler.Reconcile(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(ts.transitionCalls) != 0 {
		t.Errorf("expected 0 transition calls (skipped due to no PR link), got %d", len(ts.transitionCalls))
	}
}

func TestReconcilePerTaskErrorIsolation(t *testing.T) {
	ctx := context.Background()

	getPRState := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error) {
		return "merged", nil
	}

	getReviewDecision := func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error) {
		return "approved", time.Time{}, nil
	}

	ts := &fakeTaskSource{
		projects: []store.Project{
			{ID: "proj-1"},
		},
		tasks: map[string][]store.Task{
			"proj-1": {
				{
					ID:         "task-1",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
				{
					ID:         "task-2",
					Title:      "Test Task 2",
					State:      "approved",
					UpdatedAt:  "2024-01-01T00:00:00Z",
					AgentMerge: false,
				},
			},
		},
		taskWithDepsAndLinks: map[string]store.TaskWithDepsAndLinks{
			"task-1": {
				ID:        "task-1",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/1"},
				},
			},
			"task-2": {
				ID:        "task-2",
				Title:     "Test Task 2",
				State:     "approved",
				UpdatedAt: "2024-01-01T00:00:00Z",
				Links: []store.TaskLink{
					{Kind: "pr", Value: "https://github.com/owner/repo/pull/2"},
				},
			},
		},
		getTaskErrs: map[string]error{
			"task-1": errors.New("get task error for task-1"),
		},
	}
	notifier := &fakeNotifierForReconciler{}
	tokenLookup := func(owner string) (string, error) { return "token", nil }

	reconciler := NewPRWatchReconciler(ts, notifier, tokenLookup, newTestLogger())
	reconciler.getPRState = getPRState
	reconciler.getReviewDecision = getReviewDecision

	t.Run("error on task-1 should not affect task-2 processing", func(t *testing.T) {
		err := reconciler.reconcileProject(ctx, "proj-1")
		if err != nil {
			t.Fatalf("expected no error from reconcileProject, got %v", err)
		}

		if len(ts.transitionCalls) == 0 {
			t.Fatal("expected some transition call from task-2 despite task-1 error")
		}
	})
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		owner   string
		repo    string
		number  int
		wantErr bool
	}{
		{
			name:   "valid URL",
			url:    "https://github.com/owner/repo/pull/123",
			owner:  "owner",
			repo:   "repo",
			number: 123,
		},
		{
			name:   "valid URL with trailing slash",
			url:    "https://github.com/owner/repo/pull/456/",
			owner:  "owner",
			repo:   "repo",
			number: 456,
		},
		{
			name:    "invalid host",
			url:     "https://gitlab.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "invalid path",
			url:     "https://github.com/owner/repo/issues/123",
			wantErr: true,
		},
		{
			name:    "invalid PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parsePRURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePRURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.owner || repo != tt.repo || number != tt.number {
					t.Errorf("parsePRURL() = (%q, %q, %d), want (%q, %q, %d)", owner, repo, number, tt.owner, tt.repo, tt.number)
				}
			}
		})
	}
}
