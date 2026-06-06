package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
	"github.com/boldfield/agentask/internal/tuiconfig"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// boardMode identifies the current interaction mode of the board.
// In any mode other than modeNormal, regular navigation keys are suppressed.
type boardMode int

const (
	// modeNormal is the default navigation mode.
	modeNormal boardMode = iota
	// modeApproveNote is the optional note input step for an approve action.
	modeApproveNote
	// modeApproveConfirm is the "Approve → done? [y/N]" confirmation step.
	modeApproveConfirm
	// modeRejectReason is the required reason input step for a reject action.
	modeRejectReason
)

// BoardModel is the Bubble Tea model for the task board view.
type BoardModel struct {
	client         tuiclient.Client
	config         *tuiconfig.Config
	project        tuiclient.Project
	tasks          map[string][]tuiclient.Task // keyed by state
	selectedTaskID string                      // current selection, keyed by ID
	selectedIndex  int                         // index position within selected column, for nearest-selection on disappear
	selectedColumn int                         // 0=backlog, 1=ready, 2=in_progress, 3=review, 4=done
	loading        bool
	error          string
	width          int
	height         int
	lastRefresh    time.Time
	// newTickCmd is the function used to arm the next poll tick.
	// Overridable in tests to avoid real timers and to introspect arming.
	newTickCmd func() tea.Cmd

	// Review action state
	mode          boardMode
	reviewInput   textinput.Model // shared textinput for note/reason
	pendingNote   *string         // captured note (nil = omitted) when in modeApproveConfirm
	pendingTaskID string          // ID of the task being reviewed
	inputHint     string          // hint displayed below the input (e.g. "reason required")
}

const (
	stateBacklog    = "backlog"
	stateReady      = "ready"
	stateInProgress = "in_progress"
	stateReview     = "review"
	stateDone       = "done"
)

var stateOrder = []string{stateBacklog, stateReady, stateInProgress, stateReview, stateDone}
var stateColors = map[string]lipgloss.Color{
	stateBacklog:    lipgloss.Color("8"),
	stateReady:      lipgloss.Color("4"),
	stateInProgress: lipgloss.Color("3"),
	stateReview:     lipgloss.Color("5"),
	stateDone:       lipgloss.Color("2"),
}

// NewBoardModel creates a new board model and starts the initial fetch.
func NewBoardModel(client tuiclient.Client, config *tuiconfig.Config, project tuiclient.Project) *BoardModel {
	inputWidget := textinput.New()
	inputWidget.CharLimit = 512

	m := &BoardModel{
		client:         client,
		config:         config,
		project:        project,
		tasks:          make(map[string][]tuiclient.Task),
		selectedColumn: 2, // in_progress by default
		loading:        true,
		reviewInput:    inputWidget,
	}
	m.newTickCmd = m.defaultTickCmd
	return m
}

// defaultTickCmd is the production tick: fires after PollInterval.
func (m *BoardModel) defaultTickCmd() tea.Cmd {
	return tea.Tick(m.config.PollInterval, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

// Init starts the initial fetch and the polling loop.
// Exactly one tick chain is started here; it perpetuates itself in the tickMsg handler.
func (m *BoardModel) Init() tea.Cmd {
	return tea.Batch(
		m.fetchTasks(),
		m.newTickCmd(),
	)
}

// promoteTask creates a command that promotes a task and then refetches.
func (m *BoardModel) promoteTask(taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := m.client.PromoteTask(ctx, taskID)
		if err != nil {
			// Use typed error inspection so the friendly branch is reached even when the
			// server returns a structured body (e.g. code=CONFLICT) rather than a raw "409".
			var apiErr *tuiclient.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
				return promoteErrorMsg{
					taskID: taskID,
					err:    "not in backlog (already moved?)",
				}
			}
			return promoteErrorMsg{
				taskID: taskID,
				err:    fmt.Sprintf("promote failed: %v", err),
			}
		}

		// Promotion succeeded; refetch to get updated board state
		// Issue a new fetch command and return its result
		return m.fetchTasks()()
	}
}

// promoteErrorMsg carries an error from a promote action.
type promoteErrorMsg struct {
	taskID string
	err    string
}

// reviewActionMsg is returned when a review action (approve/reject) completes.
// It carries either a successful refetch (tasks != nil) or an error string.
type reviewActionMsg struct {
	// tasks is non-nil on success; it holds the refreshed board data.
	tasks map[string][]tuiclient.Task
	err   string
}

// reviewApprove creates a command that calls ReviewTask(approve) then TransitionTask(done),
// in that order. If ReviewTask fails, TransitionTask is not called. Both calls use note,
// which may be nil. On success, the command triggers a refetch.
func (m *BoardModel) reviewApprove(taskID string, note *string) tea.Cmd {
	actor := m.config.Actor
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Step 1: record the review verdict.
		if err := m.client.ReviewTask(ctx, taskID, actor, "approve", note); err != nil {
			return reviewActionMsg{err: fmt.Sprintf("approve review failed: %v", err)}
		}

		// Step 2: transition to done (terminal state).
		if err := m.client.TransitionTask(ctx, taskID, "done", note); err != nil {
			var apiErr *tuiclient.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
				// 409: the task already moved (race). Refetch so the board reflects reality.
				return m.fetchTasksInline(ctx, fmt.Sprintf("approve transition 409: %s", apiErr.Message))
			}
			return reviewActionMsg{err: fmt.Sprintf("approve transition failed: %v", err)}
		}

		// Success: refetch to reflect the updated board.
		return m.fetchTasksInline(ctx, "")
	}
}

// reviewReject creates a command that calls ReviewTask(reject) then TransitionTask(ready),
// in that order. If ReviewTask fails, TransitionTask is not called. reason is required and
// must not be empty (the caller must enforce this before invoking). On success, the command
// triggers a refetch.
func (m *BoardModel) reviewReject(taskID string, reason string) tea.Cmd {
	actor := m.config.Actor
	reasonPtr := &reason
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Step 1: record the review verdict.
		if err := m.client.ReviewTask(ctx, taskID, actor, "reject", reasonPtr); err != nil {
			return reviewActionMsg{err: fmt.Sprintf("reject review failed: %v", err)}
		}

		// Step 2: transition back to ready so the task can be reworked.
		if err := m.client.TransitionTask(ctx, taskID, "ready", reasonPtr); err != nil {
			var apiErr *tuiclient.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
				return m.fetchTasksInline(ctx, fmt.Sprintf("reject transition 409: %s", apiErr.Message))
			}
			return reviewActionMsg{err: fmt.Sprintf("reject transition failed: %v", err)}
		}

		return m.fetchTasksInline(ctx, "")
	}
}

// fetchTasksInline performs a synchronous ListTasks call within an already-running command
// closure (i.e. uses an already-created context) and returns a reviewActionMsg so the
// result surfaces through the reviewActionMsg handler instead of tasksFetchedMsg. This
// avoids starting a second tea.Cmd goroutine from within the first.
func (m *BoardModel) fetchTasksInline(ctx context.Context, errPrefix string) reviewActionMsg {
	tasks, fetchErr := m.client.ListTasks(ctx, m.project.ID)
	if fetchErr != nil {
		errMsg := fmt.Sprintf("refetch failed: %v", fetchErr)
		if errPrefix != "" {
			errMsg = errPrefix + "; " + errMsg
		}
		return reviewActionMsg{err: errMsg}
	}

	bucketed := make(map[string][]tuiclient.Task)
	for _, state := range stateOrder {
		bucketed[state] = []tuiclient.Task{}
	}
	for _, task := range tasks {
		if _, exists := bucketed[task.State]; exists {
			bucketed[task.State] = append(bucketed[task.State], task)
		}
	}
	for _, taskList := range bucketed {
		sort.Slice(taskList, func(i, j int) bool {
			return taskList[i].UpdatedAt > taskList[j].UpdatedAt
		})
	}

	msg := reviewActionMsg{tasks: bucketed}
	if errPrefix != "" {
		msg.err = errPrefix
	}
	return msg
}

// fetchTasks creates a command that fetches tasks and returns them.
func (m *BoardModel) fetchTasks() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		tasks, err := m.client.ListTasks(ctx, m.project.ID)
		if err != nil {
			return tasksFetchedMsg{
				err: err,
			}
		}

		// Bucket tasks by state
		bucketed := make(map[string][]tuiclient.Task)
		for _, state := range stateOrder {
			bucketed[state] = []tuiclient.Task{}
		}

		for _, task := range tasks {
			if _, exists := bucketed[task.State]; exists {
				bucketed[task.State] = append(bucketed[task.State], task)
			}
		}

		// Sort each state's tasks by update time (descending)
		for _, taskList := range bucketed {
			sort.Slice(taskList, func(i, j int) bool {
				return taskList[i].UpdatedAt > taskList[j].UpdatedAt
			})
		}

		return tasksFetchedMsg{
			tasks: bucketed,
		}
	}
}

type tasksFetchedMsg struct {
	tasks map[string][]tuiclient.Task
	err   error
}

// tickMsg is sent by the poll tick to trigger a fetch and re-arm the next tick.
type tickMsg struct{}

// Update handles messages from Bubble Tea.
func (m *BoardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// When a text-input or confirm mode is active, route keys to the review flow
		// instead of the normal navigation handlers.
		if m.mode != modeNormal {
			return m.updateReviewMode(msg)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		// Column navigation
		case "left", "h":
			if m.selectedColumn > 0 {
				m.selectedColumn--
				m.selectedIndex = 0 // Reset to top when changing columns
				m.ensureSelectionInColumn()
			}

		case "right", "l":
			if m.selectedColumn < len(stateOrder)-1 {
				m.selectedColumn++
				m.selectedIndex = 0 // Reset to top when changing columns
				m.ensureSelectionInColumn()
			}

		// Selection within column
		case "up", "k":
			tasksInColumn := m.getTasksInSelectedColumn()
			if len(tasksInColumn) == 0 {
				return m, nil
			}

			if m.selectedTaskID == "" {
				m.selectedTaskID = tasksInColumn[0].ID
				m.selectedIndex = 0
			} else {
				// Find current position
				for i, t := range tasksInColumn {
					if t.ID == m.selectedTaskID && i > 0 {
						m.selectedTaskID = tasksInColumn[i-1].ID
						m.selectedIndex = i - 1
						break
					}
				}
			}

		case "down", "j":
			tasksInColumn := m.getTasksInSelectedColumn()
			if len(tasksInColumn) == 0 {
				return m, nil
			}

			if m.selectedTaskID == "" {
				m.selectedTaskID = tasksInColumn[0].ID
				m.selectedIndex = 0
			} else {
				// Find current position
				for i, t := range tasksInColumn {
					if t.ID == m.selectedTaskID && i < len(tasksInColumn)-1 {
						m.selectedTaskID = tasksInColumn[i+1].ID
						m.selectedIndex = i + 1
						break
					}
				}
			}

		// Refresh: issue one-shot fetch; do NOT arm a new tick.
		// The single perpetual tick chain started in Init re-arms itself from tickMsg.
		case "r":
			return m, m.fetchTasks()

		// Promote: only on backlog tasks
		case "p":
			if m.selectedColumn == 0 && m.selectedTaskID != "" {
				return m, m.promoteTask(m.selectedTaskID)
			}

		// Approve: only on review column tasks
		case "a":
			if m.selectedColumn == 3 && m.selectedTaskID != "" {
				m.pendingTaskID = m.selectedTaskID
				m.reviewInput.Placeholder = "optional note (enter to skip)"
				m.reviewInput.SetValue("")
				m.reviewInput.Focus()
				m.inputHint = ""
				m.mode = modeApproveNote
				var cmd tea.Cmd
				m.reviewInput, cmd = m.reviewInput.Update(nil)
				return m, cmd
			}

		// Reject: only on review column tasks
		case "x":
			if m.selectedColumn == 3 && m.selectedTaskID != "" {
				m.pendingTaskID = m.selectedTaskID
				m.reviewInput.Placeholder = "rejection reason (required)"
				m.reviewInput.SetValue("")
				m.reviewInput.Focus()
				m.inputHint = ""
				m.mode = modeRejectReason
				var cmd tea.Cmd
				m.reviewInput, cmd = m.reviewInput.Update(nil)
				return m, cmd
			}

		// Help (stub for TUI-3+)
		case "?":
			// TODO: show help overlay
		}

	case promoteErrorMsg:
		m.error = msg.err
		return m, nil

	case reviewActionMsg:
		if msg.err != "" {
			m.error = msg.err
		} else {
			m.error = ""
		}
		if msg.tasks != nil {
			m.loading = false
			m.tasks = msg.tasks
			m.lastRefresh = time.Now()
			m.ensureSelectionInColumn()
		}
		return m, nil

	case tasksFetchedMsg:
		if msg.err != nil {
			m.error = fmt.Sprintf("Error: %v", msg.err)
			// Return without arming a tick; the single tick chain in tickMsg handles
			// re-arming itself independently.
			return m, nil
		}

		m.loading = false
		m.error = ""
		m.tasks = msg.tasks
		m.lastRefresh = time.Now()
		m.ensureSelectionInColumn()

		// Just update data; do NOT arm a tick. The single tick chain in tickMsg handles
		// re-arming itself.
		return m, nil

	case tickMsg:
		// Re-arm exactly one next tick and issue a fetch.
		// This is the ONLY place (besides Init) where a new tick is armed.
		return m, tea.Batch(
			m.fetchTasks(),
			m.newTickCmd(),
		)
	}

	return m, nil
}

// updateReviewMode handles key events when the model is in one of the review input modes.
// It returns the updated model and any command to run. Normal navigation is suppressed.
func (m *BoardModel) updateReviewMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeApproveNote:
		switch msg.String() {
		case "esc":
			// Cancel the approve action entirely.
			m.cancelReviewMode()
			return m, nil
		case "enter":
			// Capture the note (nil if empty), then move to confirm step.
			noteValue := strings.TrimSpace(m.reviewInput.Value())
			if noteValue == "" {
				m.pendingNote = nil
			} else {
				m.pendingNote = &noteValue
			}
			m.mode = modeApproveConfirm
			return m, nil
		default:
			// Pass the key to the text input.
			var cmd tea.Cmd
			m.reviewInput, cmd = m.reviewInput.Update(msg)
			return m, cmd
		}

	case modeApproveConfirm:
		switch msg.String() {
		case "esc", "n", "N":
			// User declined or pressed escape — cancel.
			m.cancelReviewMode()
			return m, nil
		case "y", "Y":
			// User confirmed — fire the approve command.
			taskID := m.pendingTaskID
			note := m.pendingNote
			m.cancelReviewMode()
			return m, m.reviewApprove(taskID, note)
		}
		// Ignore all other keys in confirm mode.
		return m, nil

	case modeRejectReason:
		switch msg.String() {
		case "esc":
			m.cancelReviewMode()
			return m, nil
		case "enter":
			reasonValue := strings.TrimSpace(m.reviewInput.Value())
			if reasonValue == "" {
				// Empty reason is not allowed — show hint and stay in mode.
				m.inputHint = "reason is required"
				return m, nil
			}
			// Submit: fire the reject command.
			taskID := m.pendingTaskID
			m.cancelReviewMode()
			return m, m.reviewReject(taskID, reasonValue)
		default:
			var cmd tea.Cmd
			m.reviewInput, cmd = m.reviewInput.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// cancelReviewMode resets all review-mode state and returns to normal navigation.
func (m *BoardModel) cancelReviewMode() {
	m.mode = modeNormal
	m.pendingNote = nil
	m.pendingTaskID = ""
	m.inputHint = ""
	m.reviewInput.SetValue("")
	m.reviewInput.Blur()
}

// getTasksInSelectedColumn returns the tasks in the currently selected column.
func (m *BoardModel) getTasksInSelectedColumn() []tuiclient.Task {
	state := stateOrder[m.selectedColumn]
	return m.tasks[state]
}

// ensureSelectionInColumn ensures the selected task exists in the current column.
// If the task is gone, select the nearest task at the prior index position (clamped to available).
// If no tasks, clear selection.
func (m *BoardModel) ensureSelectionInColumn() {
	tasksInColumn := m.getTasksInSelectedColumn()

	if len(tasksInColumn) == 0 {
		m.selectedTaskID = ""
		m.selectedIndex = 0
		return
	}

	// Check if selected task is still in this column
	for i, t := range tasksInColumn {
		if t.ID == m.selectedTaskID {
			m.selectedIndex = i
			return // Selection is valid
		}
	}

	// Selection lost (task disappeared). Select at the same index position,
	// clamped to the available range.
	newIdx := m.selectedIndex
	if newIdx >= len(tasksInColumn) {
		newIdx = len(tasksInColumn) - 1
	}
	m.selectedIndex = newIdx
	m.selectedTaskID = tasksInColumn[newIdx].ID
}

// View renders the board.
func (m *BoardModel) View() string {
	if m.width < 40 {
		return "Terminal too narrow. Please resize."
	}

	var b strings.Builder

	// Render tabs (column headers)
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// Separator
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// When a review mode is active, overlay the input/confirm prompt instead of the task list.
	if m.mode != modeNormal {
		b.WriteString(m.renderReviewOverlay())
	} else {
		b.WriteString(m.renderColumnTasks())
	}

	// Help bar
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.renderHelpBar())

	return b.String()
}

// renderReviewOverlay renders the text input or confirm prompt used during review actions.
func (m *BoardModel) renderReviewOverlay() string {
	var b strings.Builder
	switch m.mode {
	case modeApproveNote:
		b.WriteString("Approve — add an optional note:\n")
		b.WriteString(m.reviewInput.View())
		b.WriteString("\n")
		b.WriteString("(enter to continue, esc to cancel)\n")
	case modeApproveConfirm:
		taskID := m.pendingTaskID
		if len(taskID) > 8 {
			taskID = taskID[:8]
		}
		b.WriteString(fmt.Sprintf("Approve %s → done? [y/N] ", taskID))
		b.WriteString("(done is terminal and cannot be undone)\n")
		b.WriteString("(y to confirm, n/esc to cancel)\n")
	case modeRejectReason:
		b.WriteString("Reject — reason (required):\n")
		b.WriteString(m.reviewInput.View())
		b.WriteString("\n")
		if m.inputHint != "" {
			b.WriteString("hint: " + m.inputHint + "\n")
		}
		b.WriteString("(enter to submit, esc to cancel)\n")
	}
	return b.String()
}

// renderTabs renders the column tabs with counts.
func (m *BoardModel) renderTabs() string {
	var tabs []string
	for i, state := range stateOrder {
		count := len(m.tasks[state])
		tab := fmt.Sprintf("%s(%d)", state, count)

		if i == m.selectedColumn {
			// Active tab: surrounded by angle brackets
			tab = fmt.Sprintf("‹%s›", tab)
		}

		tabs = append(tabs, tab)
	}

	return strings.Join(tabs, "  ")
}

// renderColumnTasks renders the tasks in the selected column.
func (m *BoardModel) renderColumnTasks() string {
	if m.loading {
		return "Loading..."
	}

	if m.error != "" {
		return fmt.Sprintf("Error: %s\nPress 'r' to retry.", m.error)
	}

	tasksInColumn := m.getTasksInSelectedColumn()

	if len(tasksInColumn) == 0 {
		return "(empty)"
	}

	var b strings.Builder
	for _, task := range tasksInColumn {
		isSelected := task.ID == m.selectedTaskID
		prefix := " "
		if isSelected {
			prefix = "▸"
		}

		taskIDDisplay := task.ID
		if len(task.ID) > 8 {
			taskIDDisplay = task.ID[:8]
		}
		b.WriteString(fmt.Sprintf("%s %s  %s\n", prefix, taskIDDisplay, task.Title))

		// In in_progress state, show assignee and lease countdown
		if task.State == stateInProgress {
			assignee := "(unassigned)"
			if task.Assignee != nil {
				assignee = *task.Assignee
			}

			leaseStatus := "no lease"
			if task.LeaseExpiresAt != nil {
				leaseStatus = m.formatLeaseCountdown(*task.LeaseExpiresAt)
			}

			b.WriteString(fmt.Sprintf("    %s · lease %s · updated %s ago\n", assignee, leaseStatus, m.formatTime(task.UpdatedAt)))
		}
	}

	return b.String()
}

// formatLeaseCountdown formats the lease expiration time as a countdown.
func (m *BoardModel) formatLeaseCountdown(expiresAt string) string {
	// Parse the RFC3339 timestamp
	t, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return "invalid"
	}

	now := time.Now()
	if now.After(t) {
		return "EXPIRED"
	}

	remaining := t.Sub(now)
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	seconds := int(remaining.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// formatTime formats a timestamp relative to now.
func (m *BoardModel) formatTime(timestamp string) string {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return "unknown"
	}

	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return fmt.Sprintf("%ds", int(diff.Seconds()))
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	}
	if diff < 24*time.Hour {
		return fmt.Sprintf("%dh", int(diff.Hours()))
	}

	return fmt.Sprintf("%dd", int(diff.Hours()/24))
}

// renderHelpBar renders the bottom help bar.
// Show column-specific actions: `p promote` on backlog, `a approve` / `x reject` on review.
func (m *BoardModel) renderHelpBar() string {
	// While in a review input mode, show mode-specific hints.
	if m.mode != modeNormal {
		return "esc cancel"
	}
	switch m.selectedColumn {
	case 0: // backlog
		return "←/→ column   ↑/↓ select   p promote   r refresh   q quit"
	case 3: // review
		return "←/→ column   ↑/↓ select   a approve   x reject   r refresh   q quit"
	default:
		return "←/→ column   ↑/↓ select   r refresh   q quit"
	}
}
