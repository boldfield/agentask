package prwatch

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/boldfield/agentask/internal/forge"
	"github.com/boldfield/agentask/internal/notify"
	"github.com/boldfield/agentask/internal/store"
)

type taskSource interface {
	ListProjects(ctx context.Context, filter store.ProjectListFilter) ([]store.Project, error)
	ListTasks(ctx context.Context, projectID string, filter store.TaskListFilter) ([]store.Task, error)
	GetTask(ctx context.Context, id string) (store.TaskWithDepsAndLinks, error)
	TransitionTask(ctx context.Context, taskID, to string, note *string) (store.Task, error)
}

type PRWatchReconciler struct {
	taskSource        taskSource
	notifier          notify.Notifier
	tokenLookup       func(owner string) (string, error)
	logger            *slog.Logger
	getPRState        func(ctx context.Context, owner, repo string, prNumber int, token string) (string, error)
	getReviewDecision func(ctx context.Context, owner, repo string, prNumber int, token string) (string, time.Time, error)
	postPRComment     func(ctx context.Context, owner, repo string, prNumber int, token, comment string) error
}

func NewPRWatchReconciler(
	taskSource taskSource,
	notifier notify.Notifier,
	tokenLookup func(owner string) (string, error),
	logger *slog.Logger,
) *PRWatchReconciler {
	return &PRWatchReconciler{
		taskSource:        taskSource,
		notifier:          notifier,
		tokenLookup:       tokenLookup,
		logger:            logger,
		getPRState:        forge.GetPRState,
		getReviewDecision: forge.GetReviewDecision,
		postPRComment:     forge.PostPRComment,
	}
}

func (r *PRWatchReconciler) Name() string {
	return "pr-watch"
}

func (r *PRWatchReconciler) Reconcile(ctx context.Context) error {
	projects, err := r.taskSource.ListProjects(ctx, store.ProjectListFilter{})
	if err != nil {
		return err
	}

	for _, project := range projects {
		if err := r.reconcileProject(ctx, project.ID); err != nil {
			r.logger.Error("reconcile project error", "project_id", project.ID, "error", err)
		}
	}

	// Retrofit: close PRs of already-superseded or abandoned tasks
	for _, project := range projects {
		if err := r.retrofitClosePRsForTerminalTasks(ctx, project.ID); err != nil {
			r.logger.Error("retrofit close PR error", "project_id", project.ID, "error", err)
		}
	}

	return nil
}

func (r *PRWatchReconciler) reconcileProject(ctx context.Context, projectID string) error {
	approvedState := "approved"
	tasks, err := r.taskSource.ListTasks(ctx, projectID, store.TaskListFilter{
		State: &approvedState,
	})
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if err := r.reconcileTask(ctx, task); err != nil {
			r.logger.Error("reconcile task error", "task_id", task.ID, "error", err)
		}
	}

	return nil
}

func (r *PRWatchReconciler) reconcileTask(ctx context.Context, task store.Task) error {
	if task.AgentMerge {
		return nil
	}

	fullTask, err := r.taskSource.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}

	var prLink *store.TaskLink
	for i := range fullTask.Links {
		if fullTask.Links[i].Kind == "pr" {
			prLink = &fullTask.Links[i]
			break
		}
	}

	if prLink == nil {
		return nil
	}

	owner, repo, prNumber, err := parsePRURL(prLink.Value)
	if err != nil {
		r.logger.Error("parse PR URL error", "task_id", task.ID, "pr_url", prLink.Value, "error", err)
		return nil
	}

	token, err := r.tokenLookup(owner)
	if err != nil {
		r.logger.Error("token lookup error", "task_id", task.ID, "owner", owner, "error", err)
		return nil
	}

	state, err := r.getPRState(ctx, owner, repo, prNumber, token)
	if err != nil {
		r.logger.Error("get PR state error", "task_id", task.ID, "owner", owner, "repo", repo, "pr_number", prNumber, "error", err)
		return nil
	}

	decision, latestReviewAt, err := r.getReviewDecision(ctx, owner, repo, prNumber, token)
	if err != nil {
		r.logger.Error("get review decision error", "task_id", task.ID, "owner", owner, "repo", repo, "pr_number", prNumber, "error", err)
		return nil
	}

	approvedAt, err := parseTime(task.UpdatedAt)
	if err != nil {
		r.logger.Error("parse approved at time error", "task_id", task.ID, "updated_at", task.UpdatedAt, "error", err)
		return nil
	}

	action := decideAction(state, decision, latestReviewAt, approvedAt)

	switch action {
	case Done:
		if err := applyMerged(ctx, r.taskSource, r.notifier, taskWithDepsLinksToTask(fullTask)); err != nil {
			r.logger.Error("apply merged error", "task_id", task.ID, "error", err)
			return nil
		}
	case Abandon:
		reason := "PR closed without merging"
		if err := applyAbandoned(ctx, r.taskSource, taskWithDepsLinksToTask(fullTask), reason); err != nil {
			r.logger.Error("apply abandoned error", "task_id", task.ID, "error", err)
			return nil
		}
	case Bounce:
		if err := applyBounce(ctx, r.taskSource, taskWithDepsLinksToTask(fullTask), owner, repo, prNumber, token); err != nil {
			r.logger.Error("apply bounce error", "task_id", task.ID, "error", err)
			return nil
		}
	case Noop:
	}

	return nil
}

func parsePRURL(prURL string) (owner, repo string, number int, err error) {
	prURL = strings.TrimSuffix(prURL, "/")

	u, err := url.Parse(prURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid URL: %w", err)
	}

	if u.Host != "github.com" {
		return "", "", 0, fmt.Errorf("not a github.com URL")
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")

	if len(parts) != 4 || parts[2] != "pull" {
		return "", "", 0, fmt.Errorf("not a pull request URL")
	}

	owner = parts[0]
	repo = parts[1]

	number, err = strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid pull request number: %w", err)
	}

	return owner, repo, number, nil
}

func parseTime(timeStr string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, timeStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

// retrofitClosePRsForTerminalTasks closes pull requests belonging to tasks in terminal states
// (superseded, abandoned) that are still open. This is a retrofit operation to clean up the
// backlog of stale PRs from before PR-closing was implemented on supersession.
func (r *PRWatchReconciler) retrofitClosePRsForTerminalTasks(ctx context.Context, projectID string) error {
	terminalStates := []string{"superseded", "abandoned"}
	for _, state := range terminalStates {
		tasks, err := r.taskSource.ListTasks(ctx, projectID, store.TaskListFilter{
			State:             &state,
			IncludeSuperseded: true,
		})
		if err != nil {
			r.logger.Error("retrofit list tasks error", "project_id", projectID, "state", state, "error", err)
			continue
		}

		for _, task := range tasks {
			if err := r.retrofitCloseTaskPR(ctx, task); err != nil {
				r.logger.Error("retrofit close task PR error", "task_id", task.ID, "error", err)
			}
		}
	}

	return nil
}

// retrofitCloseTaskPR closes the open PR belonging to a task in a terminal state.
func (r *PRWatchReconciler) retrofitCloseTaskPR(ctx context.Context, task store.Task) error {
	fullTask, err := r.taskSource.GetTask(ctx, task.ID)
	if err != nil {
		return err
	}

	var prLink *store.TaskLink
	for i := range fullTask.Links {
		if fullTask.Links[i].Kind == "pr" {
			prLink = &fullTask.Links[i]
			break
		}
	}

	if prLink == nil {
		return nil
	}

	owner, repo, prNumber, err := parsePRURL(prLink.Value)
	if err != nil {
		r.logger.Error("retrofit parse PR URL error", "task_id", task.ID, "pr_url", prLink.Value, "error", err)
		return nil
	}

	token, err := r.tokenLookup(owner)
	if err != nil {
		r.logger.Error("retrofit token lookup error", "task_id", task.ID, "owner", owner, "error", err)
		return nil
	}

	state, err := r.getPRState(ctx, owner, repo, prNumber, token)
	if err != nil {
		r.logger.Error("retrofit get PR state error", "task_id", task.ID, "owner", owner, "repo", repo, "pr_number", prNumber, "error", err)
		return nil
	}

	// Only close open PRs
	if state != "open" {
		return nil
	}

	// Post a comment indicating the PR is being closed as superseded/abandoned
	comment := fmt.Sprintf("This pull request is being closed because the task is now in %s state.", task.State)
	if err := r.postPRComment(ctx, owner, repo, prNumber, token, comment); err != nil {
		r.logger.Error("retrofit post comment error", "task_id", task.ID, "owner", owner, "repo", repo, "pr_number", prNumber, "error", err)
		// Continue to close the PR even if comment fails
	}

	// Close the PR
	if err := forge.ClosePR(ctx, owner, repo, prNumber, token); err != nil {
		r.logger.Error("retrofit close PR error", "task_id", task.ID, "owner", owner, "repo", repo, "pr_number", prNumber, "error", err)
		return nil
	}

	r.logger.Info("retrofit closed PR for terminal task", "task_id", task.ID, "state", task.State, "owner", owner, "repo", repo, "pr_number", prNumber)
	return nil
}

func taskWithDepsLinksToTask(t store.TaskWithDepsAndLinks) store.Task {
	return store.Task{
		ID:             t.ID,
		ProjectID:      t.ProjectID,
		DocumentID:     t.DocumentID,
		Title:          t.Title,
		Spec:           t.Spec,
		State:          t.State,
		Assignee:       t.Assignee,
		LeaseExpiresAt: t.LeaseExpiresAt,
		Result:         t.Result,
		Model:          t.Model,
		Kind:           t.Kind,
		ReviewModels:   t.ReviewModels,
		ReviewRound:    t.ReviewRound,
		TargetTaskID:   t.TargetTaskID,
		Verdict:        t.Verdict,
		AgentMerge:     t.AgentMerge,
		Held:           t.Held,
		Escalate:       t.Escalate,
		Track:          t.Track,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		ArchivedAt:     t.ArchivedAt,
		SupersededBy:   t.SupersededBy,
	}
}
