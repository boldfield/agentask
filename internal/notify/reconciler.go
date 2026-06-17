package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/boldfield/agentask/internal/store"
)

type taskSource interface {
	ListProjects(ctx context.Context, filter store.ProjectListFilter) ([]store.Project, error)
	ListTasks(ctx context.Context, projectID string, filter store.TaskListFilter) ([]store.Task, error)
	GetTask(ctx context.Context, id string) (store.TaskWithDepsAndLinks, error)
}

type NotifyReconciler struct {
	src          taskSource
	notifier     Notifier
	failedWindow time.Duration
	now          func() time.Time
	logger       *slog.Logger
}

func NewNotifyReconciler(src taskSource, n Notifier, failedWindow time.Duration, now func() time.Time, logger *slog.Logger) *NotifyReconciler {
	if now == nil {
		now = time.Now
	}
	return &NotifyReconciler{
		src:          src,
		notifier:     n,
		failedWindow: failedWindow,
		now:          now,
		logger:       logger,
	}
}

func (r *NotifyReconciler) Name() string {
	return "notify"
}

func (r *NotifyReconciler) Reconcile(ctx context.Context) error {
	projects, err := r.src.ListProjects(ctx, store.ProjectListFilter{})
	if err != nil {
		return err
	}

	states := []string{"approved", "blocked", "failed"}

	for _, project := range projects {
		for _, state := range states {
			statePtr := &state
			tasks, err := r.src.ListTasks(ctx, project.ID, store.TaskListFilter{State: statePtr})
			if err != nil {
				r.logger.Error("failed to list tasks", "project_id", project.ID, "state", state, "error", err)
				continue
			}

			for _, task := range tasks {
				if task.AgentMerge {
					continue
				}

				if state == "failed" {
					updatedAt, err := time.Parse(time.RFC3339Nano, task.UpdatedAt)
					if err != nil {
						r.logger.Error("failed to parse UpdatedAt", "task_id", task.ID, "updated_at", task.UpdatedAt, "error", err)
						continue
					}

					if r.now().Sub(updatedAt) > r.failedWindow {
						continue
					}
				}

				taskWithLinks, err := r.src.GetTask(ctx, task.ID)
				if err != nil {
					r.logger.Error("failed to get task", "task_id", task.ID, "error", err)
					continue
				}

				var prLink string
				for _, link := range taskWithLinks.Links {
					if link.Kind == "pr" {
						prLink = link.Value
						break
					}
				}

				notification, ok := buildNotification(task, prLink)
				if !ok {
					continue
				}

				if err := r.notifier.Publish(ctx, notification); err != nil {
					r.logger.Error("failed to publish notification", "task_id", task.ID, "error", err)
					continue
				}
			}
		}
	}

	return nil
}
