package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/store"
)

type fakeTaskSource struct {
	projects map[string][]store.Project
	tasks    map[string][]store.Task
	taskData map[string]store.TaskWithDepsAndLinks
}

func newFakeTaskSource() *fakeTaskSource {
	return &fakeTaskSource{
		projects: make(map[string][]store.Project),
		tasks:    make(map[string][]store.Task),
		taskData: make(map[string]store.TaskWithDepsAndLinks),
	}
}

func (f *fakeTaskSource) ListProjects(ctx context.Context, filter store.ProjectListFilter) ([]store.Project, error) {
	var projects []store.Project
	for _, p := range f.projects {
		projects = append(projects, p...)
	}
	return projects, nil
}

func (f *fakeTaskSource) ListTasks(ctx context.Context, projectID string, filter store.TaskListFilter) ([]store.Task, error) {
	var result []store.Task
	for _, task := range f.tasks[projectID] {
		if filter.State != nil && task.State != *filter.State {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

func (f *fakeTaskSource) GetTask(ctx context.Context, id string) (store.TaskWithDepsAndLinks, error) {
	if task, exists := f.taskData[id]; exists {
		return task, nil
	}
	return store.TaskWithDepsAndLinks{}, fmt.Errorf("not found")
}

type fakeNotifier struct {
	published []Notification
	err       error
}

func (f *fakeNotifier) Publish(ctx context.Context, n Notification) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, n)
	return nil
}

func TestNotifyReconcilerEmitsForApprovedAndBlocked(t *testing.T) {
	src := newFakeTaskSource()
	notifier := &fakeNotifier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	reconciler := NewNotifyReconciler(src, notifier, time.Hour, func() time.Time { return now }, logger)

	projectID := "proj1"
	src.projects[""] = []store.Project{{ID: projectID}}

	src.tasks[projectID] = []store.Task{
		{ID: "task1", ProjectID: projectID, State: "approved", Title: "Test Approved", UpdatedAt: now.Format(time.RFC3339Nano)},
		{ID: "task2", ProjectID: projectID, State: "blocked", Title: "Test Blocked", UpdatedAt: now.Format(time.RFC3339Nano)},
	}

	src.taskData["task1"] = store.TaskWithDepsAndLinks{
		ID:        "task1",
		State:     "approved",
		Title:     "Test Approved",
		UpdatedAt: now.Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/1"}},
	}

	src.taskData["task2"] = store.TaskWithDepsAndLinks{
		ID:        "task2",
		State:     "blocked",
		Title:     "Test Blocked",
		UpdatedAt: now.Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/2"}},
	}

	err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if len(notifier.published) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(notifier.published))
	}

	if notifier.published[0].Event != "agentask-review" {
		t.Errorf("expected event 'agentask-review' for approved, got '%s'", notifier.published[0].Event)
	}

	if notifier.published[1].Event != "agentask-blocked" {
		t.Errorf("expected event 'agentask-blocked' for blocked, got '%s'", notifier.published[1].Event)
	}
}

func TestNotifyReconcilerFailedRecencyWindow(t *testing.T) {
	src := newFakeTaskSource()
	notifier := &fakeNotifier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	windowDuration := time.Hour
	reconciler := NewNotifyReconciler(src, notifier, windowDuration, func() time.Time { return now }, logger)

	projectID := "proj1"
	src.projects[""] = []store.Project{{ID: projectID}}

	recentTask := store.Task{
		ID:        "recent",
		ProjectID: projectID,
		State:     "failed",
		Title:     "Recent Failed",
		UpdatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
	}

	staleTask := store.Task{
		ID:        "stale",
		ProjectID: projectID,
		State:     "failed",
		Title:     "Stale Failed",
		UpdatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
	}

	src.tasks[projectID] = []store.Task{recentTask, staleTask}

	src.taskData["recent"] = store.TaskWithDepsAndLinks{
		ID:        "recent",
		State:     "failed",
		Title:     "Recent Failed",
		UpdatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/1"}},
	}

	src.taskData["stale"] = store.TaskWithDepsAndLinks{
		ID:        "stale",
		State:     "failed",
		Title:     "Stale Failed",
		UpdatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/2"}},
	}

	err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if len(notifier.published) != 1 {
		t.Errorf("expected 1 notification (recent only), got %d", len(notifier.published))
	}

	if notifier.published[0].Title != "Failed: Recent Failed" {
		t.Errorf("expected recent task notification, got '%s'", notifier.published[0].Title)
	}
}

func TestNotifyReconcilerSkipsAgentMergeTrue(t *testing.T) {
	src := newFakeTaskSource()
	notifier := &fakeNotifier{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	reconciler := NewNotifyReconciler(src, notifier, time.Hour, func() time.Time { return now }, logger)

	projectID := "proj1"
	src.projects[""] = []store.Project{{ID: projectID}}

	src.tasks[projectID] = []store.Task{
		{ID: "task1", ProjectID: projectID, State: "approved", Title: "Agent Merge", AgentMerge: true, UpdatedAt: now.Format(time.RFC3339Nano)},
		{ID: "task2", ProjectID: projectID, State: "approved", Title: "Manual Merge", AgentMerge: false, UpdatedAt: now.Format(time.RFC3339Nano)},
	}

	src.taskData["task1"] = store.TaskWithDepsAndLinks{
		ID:         "task1",
		State:      "approved",
		Title:      "Agent Merge",
		AgentMerge: true,
		UpdatedAt:  now.Format(time.RFC3339Nano),
		Links:      []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/1"}},
	}

	src.taskData["task2"] = store.TaskWithDepsAndLinks{
		ID:         "task2",
		State:      "approved",
		Title:      "Manual Merge",
		AgentMerge: false,
		UpdatedAt:  now.Format(time.RFC3339Nano),
		Links:      []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/2"}},
	}

	err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if len(notifier.published) != 1 {
		t.Errorf("expected 1 notification (manual merge only), got %d", len(notifier.published))
	}

	if notifier.published[0].Title != "Review & merge: Manual Merge" {
		t.Errorf("expected manual merge task notification, got '%s'", notifier.published[0].Title)
	}
}

func TestNotifyReconcilerPublishErrorDoesntAbort(t *testing.T) {
	src := newFakeTaskSource()
	notifier := &fakeNotifier{err: fmt.Errorf("publish failed")}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	reconciler := NewNotifyReconciler(src, notifier, time.Hour, func() time.Time { return now }, logger)

	projectID := "proj1"
	src.projects[""] = []store.Project{{ID: projectID}}

	src.tasks[projectID] = []store.Task{
		{ID: "task1", ProjectID: projectID, State: "approved", Title: "Task 1", UpdatedAt: now.Format(time.RFC3339Nano)},
		{ID: "task2", ProjectID: projectID, State: "approved", Title: "Task 2", UpdatedAt: now.Format(time.RFC3339Nano)},
	}

	src.taskData["task1"] = store.TaskWithDepsAndLinks{
		ID:        "task1",
		State:     "approved",
		Title:     "Task 1",
		UpdatedAt: now.Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/1"}},
	}

	src.taskData["task2"] = store.TaskWithDepsAndLinks{
		ID:        "task2",
		State:     "approved",
		Title:     "Task 2",
		UpdatedAt: now.Format(time.RFC3339Nano),
		Links:     []store.TaskLink{{Kind: "pr", Value: "https://github.com/test/pr/2"}},
	}

	err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile should not return error even when publish fails: %v", err)
	}
}
