package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
	"github.com/boldfield/agentask/internal/tuiconfig"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	m := &BoardModel{
		client:         client,
		config:         config,
		project:        project,
		tasks:          make(map[string][]tuiclient.Task),
		selectedColumn: 2, // in_progress by default
		loading:        true,
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
			// Check if it's a 409 (not in backlog)
			errStr := err.Error()
			if strings.Contains(errStr, "409") {
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

		// Help (stub for TUI-3+)
		case "?":
			// TODO: show help overlay
		}

	case promoteErrorMsg:
		m.error = msg.err
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

	// Render the active column's tasks
	b.WriteString(m.renderColumnTasks())

	// Help bar
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	b.WriteString(m.renderHelpBar())

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
// Show promote action only when in backlog column.
func (m *BoardModel) renderHelpBar() string {
	var bar string
	if m.selectedColumn == 0 {
		bar = "←/→ column   ↑/↓ select   p promote   r refresh   q quit"
	} else {
		bar = "←/→ column   ↑/↓ select   r refresh   q quit"
	}
	return bar
}
