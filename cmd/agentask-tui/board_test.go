package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
	"github.com/boldfield/agentask/internal/tuiconfig"
	tea "github.com/charmbracelet/bubbletea"
)

// TestBoardModel_RenderLayout tests that the board renders with all five columns and counts.
func TestBoardModel_RenderLayout(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		Tasks: []tuiclient.Task{
			{ID: "task-1", Title: "Task 1", State: "backlog"},
			{ID: "task-2", Title: "Task 2", State: "backlog"},
			{ID: "task-3", Title: "Task 3", State: "in_progress"},
			{ID: "task-4", Title: "Task 4", State: "done"},
			{ID: "task-5", Title: "Task 5", State: "done"},
			{ID: "task-6", Title: "Task 6", State: "done"},
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test Project"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate the task fetch by constructing the bucketed tasks
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{{ID: "task-1", Title: "Task 1", State: "backlog"}, {ID: "task-2", Title: "Task 2", State: "backlog"}}
	bucketed["ready"] = []tuiclient.Task{}
	bucketed["in_progress"] = []tuiclient.Task{{ID: "task-3", Title: "Task 3", State: "in_progress"}}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{{ID: "task-4", Title: "Task 4", State: "done"}, {ID: "task-5", Title: "Task 5", State: "done"}, {ID: "task-6", Title: "Task 6", State: "done"}}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	output := model.View()

	// Verify the tabs are present with correct counts
	if !strings.Contains(output, "backlog(2)") {
		t.Errorf("Expected 'backlog(2)' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "ready(0)") {
		t.Errorf("Expected 'ready(0)' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "in_progress(1)") {
		t.Errorf("Expected 'in_progress(1)' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "review(0)") {
		t.Errorf("Expected 'review(0)' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "done(3)") {
		t.Errorf("Expected 'done(3)' in output, got:\n%s", output)
	}
}

// TestBoardModel_Navigation tests column and selection navigation.
func TestBoardModel_Navigation(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		Tasks: []tuiclient.Task{
			{ID: "task-1", Title: "Task 1", State: "backlog"},
			{ID: "task-2", Title: "Task 2", State: "ready"},
			{ID: "task-3", Title: "Task 3", State: "in_progress"},
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate the task fetch
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{{ID: "task-1", Title: "Task 1", State: "backlog"}}
	bucketed["ready"] = []tuiclient.Task{{ID: "task-2", Title: "Task 2", State: "ready"}}
	bucketed["in_progress"] = []tuiclient.Task{{ID: "task-3", Title: "Task 3", State: "in_progress"}}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Initially in in_progress column (index 2)
	initialOutput := model.View()
	if !strings.Contains(initialOutput, "in_progress(1)") {
		t.Errorf("Expected active column to show in_progress(1), got:\n%s", initialOutput)
	}

	// Move left to ready
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = m.(*BoardModel)
	output := model.View()
	if !strings.Contains(output, "ready(1)") {
		t.Errorf("Expected active column to be ready after left, got:\n%s", output)
	}

	// Move left to backlog
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = m.(*BoardModel)
	output = model.View()
	if !strings.Contains(output, "backlog(1)") {
		t.Errorf("Expected active column to be backlog after left, got:\n%s", output)
	}
}

// TestBoardModel_SelectionStability tests that selection is preserved across refreshes.
func TestBoardModel_SelectionStability(t *testing.T) {
	mockClient := &tuiclient.MockClient{}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate first fetch
	bucketed1 := make(map[string][]tuiclient.Task)
	bucketed1["backlog"] = []tuiclient.Task{}
	bucketed1["ready"] = []tuiclient.Task{}
	bucketed1["in_progress"] = []tuiclient.Task{
		{ID: "task-1", Title: "Task 1", State: "in_progress"},
		{ID: "task-2", Title: "Task 2", State: "in_progress"},
	}
	bucketed1["review"] = []tuiclient.Task{}
	bucketed1["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed1})
	model = m.(*BoardModel)

	// Verify task-1 is selected
	if model.selectedTaskID != "task-1" {
		t.Errorf("Expected initial selection to be task-1, got %s", model.selectedTaskID)
	}

	// Move down to select task-2
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = m.(*BoardModel)
	if model.selectedTaskID != "task-2" {
		t.Errorf("Expected selection to be task-2 after down, got %s", model.selectedTaskID)
	}

	// Simulate a refresh with reordered tasks
	bucketed2 := make(map[string][]tuiclient.Task)
	bucketed2["backlog"] = []tuiclient.Task{}
	bucketed2["ready"] = []tuiclient.Task{}
	bucketed2["in_progress"] = []tuiclient.Task{
		{ID: "task-2", Title: "Task 2", State: "in_progress"},
		{ID: "task-1", Title: "Task 1", State: "in_progress"},
	}
	bucketed2["review"] = []tuiclient.Task{}
	bucketed2["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed2})
	model = m.(*BoardModel)

	// Verify task-2 is still selected
	if model.selectedTaskID != "task-2" {
		t.Errorf("Selection not preserved: was task-2, became %s", model.selectedTaskID)
	}
}

// TestBoardModel_ErrorState tests that errors are rendered correctly.
func TestBoardModel_ErrorState(t *testing.T) {
	mockClient := &tuiclient.MockClient{}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// First set a successful state
	bucketed := make(map[string][]tuiclient.Task)
	for _, state := range []string{"backlog", "ready", "in_progress", "review", "done"} {
		bucketed[state] = []tuiclient.Task{}
	}
	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Now simulate an error
	m, _ = model.Update(tasksFetchedMsg{err: errors.New("test error message")})
	model = m.(*BoardModel)

	output := model.View()
	if !strings.Contains(output, "Error:") || !strings.Contains(output, "test error message") {
		t.Errorf("Expected error message in output, got:\n%s", output)
	}

	if !strings.Contains(output, "Press 'r' to retry") {
		t.Errorf("Expected retry hint in output, got:\n%s", output)
	}
}

// TestBoardModel_EmptyColumn tests that empty columns show the empty message.
func TestBoardModel_EmptyColumn(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		Tasks: []tuiclient.Task{
			{ID: "task-1", Title: "Task 1", State: "backlog"},
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate fetch
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{{ID: "task-1", Title: "Task 1", State: "backlog"}}
	bucketed["ready"] = []tuiclient.Task{}
	bucketed["in_progress"] = []tuiclient.Task{}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Navigate from in_progress (index 2) to ready (index 1): one left
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = m.(*BoardModel)

	output := model.View()
	if !strings.Contains(output, "(empty)") {
		t.Errorf("Expected '(empty)' message in output, got:\n%s", output)
	}
}

// TestBoardModel_InProgressCardDetails tests that in_progress cards show assignee and lease info.
func TestBoardModel_InProgressCardDetails(t *testing.T) {
	expiresAt := time.Now().Add(5 * time.Minute).Format(time.RFC3339Nano)
	assignee := "alice"

	mockClient := &tuiclient.MockClient{}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate fetch
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{}
	bucketed["ready"] = []tuiclient.Task{}
	bucketed["in_progress"] = []tuiclient.Task{{
		ID:             "task-1",
		Title:          "Task 1",
		State:          "in_progress",
		Assignee:       &assignee,
		LeaseExpiresAt: &expiresAt,
	}}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	output := model.View()
	if !strings.Contains(output, "alice") {
		t.Errorf("Expected assignee 'alice' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "lease") {
		t.Errorf("Expected lease info in output, got:\n%s", output)
	}
}

// TestBoardModel_DisappearedTaskSelection tests that when the selected task disappears,
// the cursor lands on the positionally-nearest remaining task, not the first.
func TestBoardModel_DisappearedTaskSelection(t *testing.T) {
	mockClient := &tuiclient.MockClient{}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Simulate first fetch with three tasks
	bucketed1 := make(map[string][]tuiclient.Task)
	bucketed1["backlog"] = []tuiclient.Task{}
	bucketed1["ready"] = []tuiclient.Task{}
	bucketed1["in_progress"] = []tuiclient.Task{
		{ID: "task-1", Title: "Task 1", State: "in_progress"},
		{ID: "task-2", Title: "Task 2", State: "in_progress"},
		{ID: "task-3", Title: "Task 3", State: "in_progress"},
	}
	bucketed1["review"] = []tuiclient.Task{}
	bucketed1["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed1})
	model = m.(*BoardModel)

	// Select task-2 (index 1)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = m.(*BoardModel)
	if model.selectedTaskID != "task-2" {
		t.Errorf("Expected selection to be task-2, got %s", model.selectedTaskID)
	}

	// Simulate refresh where task-2 is REMOVED (only task-1 and task-3 remain)
	bucketed2 := make(map[string][]tuiclient.Task)
	bucketed2["backlog"] = []tuiclient.Task{}
	bucketed2["ready"] = []tuiclient.Task{}
	bucketed2["in_progress"] = []tuiclient.Task{
		{ID: "task-1", Title: "Task 1", State: "in_progress"},
		{ID: "task-3", Title: "Task 3", State: "in_progress"},
	}
	bucketed2["review"] = []tuiclient.Task{}
	bucketed2["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed2})
	model = m.(*BoardModel)

	// Should have selected task-3 (at clamped index 1, which is now the second task)
	// not task-1 (the first task).
	if model.selectedTaskID != "task-3" {
		t.Errorf("Expected selection to move to task-3 (nearest), got %s", model.selectedTaskID)
	}
	if model.selectedIndex != 1 {
		t.Errorf("Expected selectedIndex to be 1, got %d", model.selectedIndex)
	}
}

// tickSentinel is returned by the test tick command to identify that a tick was armed.
type tickSentinel struct{}

// countTickCmds installs a counting tick function on the model and returns a pointer to the counter.
// Each time the model arms a new tick (via newTickCmd), the counter increments by 1
// and returns a tea.Cmd that immediately sends tickSentinel{} when executed.
func countTickCmds(model *BoardModel) *int {
	count := 0
	model.newTickCmd = func() tea.Cmd {
		count++
		return func() tea.Msg { return tickSentinel{} }
	}
	return &count
}

// runCmd executes a tea.Cmd synchronously and collects all leaf messages it produces.
// tea.Batch returns a BatchMsg ([]tea.Cmd); this function recursively executes those sub-cmds
// and collects their results, so callers can inspect the full set of messages produced.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	resultChan := make(chan tea.Msg, 64)
	var execCmd func(tea.Cmd)
	execCmd = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if msg == nil {
				resultChan <- nil
				return
			}
			// tea.Batch returns a BatchMsg; recurse into each sub-cmd.
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, subCmd := range batch {
					execCmd(subCmd)
				}
			} else {
				resultChan <- msg
			}
		}()
	}

	execCmd(cmd)

	// Collect results with a timeout to handle the async tick sentinel.
	var messages []tea.Msg
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	// We need to know how many goroutines to expect. Since we don't track that precisely,
	// use a small fixed wait after the first message arrives.
	firstArrived := false
	drainTimer := time.NewTimer(24 * time.Hour) // starts stopped effectively
	defer drainTimer.Stop()

	for {
		select {
		case msg := <-resultChan:
			if msg != nil {
				messages = append(messages, msg)
			}
			if !firstArrived {
				firstArrived = true
				drainTimer.Reset(50 * time.Millisecond)
			}
		case <-drainTimer.C:
			return messages
		case <-timer.C:
			return messages
		}
	}
}

// TestBoardModel_PromoteTask tests promoting a backlog task to ready.
func TestBoardModel_PromoteTask(t *testing.T) {
	promoteWasCalled := false
	var promoteTaskID string

	mockClient := &tuiclient.MockClient{
		PromoteTaskFunc: func(ctx context.Context, id string) error {
			promoteWasCalled = true
			promoteTaskID = id
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			// On refetch after promotion, task-1 should be in ready
			return []tuiclient.Task{
				{ID: "task-1", Title: "Task 1", State: "ready"},
				{ID: "task-2", Title: "Task 2", State: "backlog"},
			}, nil
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Set up initial backlog with two tasks
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{
		{ID: "task-1", Title: "Task 1", State: "backlog"},
		{ID: "task-2", Title: "Task 2", State: "backlog"},
	}
	bucketed["ready"] = []tuiclient.Task{}
	bucketed["in_progress"] = []tuiclient.Task{}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Move to backlog column (it starts at in_progress)
	for i := 0; i < 2; i++ {
		m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
		model = m.(*BoardModel)
	}

	// Verify we're in backlog and task-1 is selected
	if model.selectedColumn != 0 {
		t.Errorf("Expected selectedColumn 0 (backlog), got %d", model.selectedColumn)
	}
	if model.selectedTaskID != "task-1" {
		t.Errorf("Expected selected task task-1, got %s", model.selectedTaskID)
	}

	// Press 'p' to promote
	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = m.(*BoardModel)

	// Execute the promote command
	if cmd != nil {
		msg := cmd()
		m, _ = model.Update(msg)
		model = m.(*BoardModel)
	}

	// Verify promote was called on task-1
	if !promoteWasCalled {
		t.Errorf("Expected PromoteTask to be called")
	}
	if promoteTaskID != "task-1" {
		t.Errorf("Expected PromoteTask called on task-1, got %s", promoteTaskID)
	}

	// After refetch, task-1 should now be in ready column
	// and the board should show updated state
	output := model.View()
	if !strings.Contains(output, "ready(1)") {
		t.Errorf("Expected 'ready(1)' after promotion, got:\n%s", output)
	}
}

// promoteWithError is a helper that runs a promote action against a model
// already positioned in the backlog column with a task selected.
// It returns the model after the promote command has been executed.
func promoteWithError(t *testing.T, returnedErr error) *BoardModel {
	t.Helper()

	mockClient := &tuiclient.MockClient{
		PromoteTaskFunc: func(ctx context.Context, id string) error {
			return returnedErr
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{
		{ID: "task-1", Title: "Task 1", State: "backlog"},
	}
	for _, state := range []string{"ready", "in_progress", "review", "done"} {
		bucketed[state] = []tuiclient.Task{}
	}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Navigate from in_progress (index 2) to backlog (index 0): two lefts
	for i := 0; i < 2; i++ {
		m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
		model = m.(*BoardModel)
	}

	// Press 'p' to promote
	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = m.(*BoardModel)

	if cmd != nil {
		msg := cmd()
		m, _ = model.Update(msg)
		model = m.(*BoardModel)
	}

	return model
}

// TestBoardModel_PromoteTask409 verifies that a real *tuiclient.APIError with StatusCode 409
// produces the friendly "not in backlog (already moved?)" message, not the generic branch.
// This test exercises the typed-error detection path (errors.As + StatusCode check), so it
// WILL FAIL if the StatusCode detection is broken (e.g. if someone reverts to strings.Contains).
func TestBoardModel_PromoteTask409(t *testing.T) {
	// Use the exact error the real HTTPClient returns for a server 409 + structured body.
	conflictErr := &tuiclient.APIError{
		StatusCode: 409,
		Code:       "CONFLICT",
		Message:    "Task is not in backlog",
	}

	model := promoteWithError(t, conflictErr)

	// The FRIENDLY branch must be taken — not just "not in backlog" from the generic path.
	// The generic path would produce "promote failed: server error (CONFLICT): Task is not in backlog".
	// The friendly path produces exactly "not in backlog (already moved?)".
	const friendlyMsg = "not in backlog (already moved?)"
	if model.error != friendlyMsg {
		t.Errorf("Expected friendly 409 message %q, got: %q", friendlyMsg, model.error)
	}

	// Verify we didn't crash and the model is still usable
	output := model.View()
	if !strings.Contains(output, "backlog(1)") {
		t.Errorf("Expected board to still be functional, got:\n%s", output)
	}
}

// TestBoardModel_PromoteTask500 verifies that a non-409 APIError (e.g. 500) takes the
// generic branch and does NOT produce the friendly conflict message.
func TestBoardModel_PromoteTask500(t *testing.T) {
	serverErr := &tuiclient.APIError{
		StatusCode: 500,
		Code:       "INTERNAL_ERROR",
		Message:    "something went wrong",
	}

	model := promoteWithError(t, serverErr)

	// Must show the generic error, not the friendly conflict message.
	if model.error == "not in backlog (already moved?)" {
		t.Errorf("Expected generic error for 500, but got the friendly 409 message")
	}
	if !strings.Contains(model.error, "promote failed:") {
		t.Errorf("Expected 'promote failed:' prefix for generic error, got: %q", model.error)
	}
	if !strings.Contains(model.error, "something went wrong") {
		t.Errorf("Expected server message in generic error, got: %q", model.error)
	}
}

// TestBoardModel_PromoteOnNonBacklogIsNoOp tests that pressing 'p' on non-backlog column does nothing.
func TestBoardModel_PromoteOnNonBacklogIsNoOp(t *testing.T) {
	promoteWasCalled := false

	mockClient := &tuiclient.MockClient{
		PromoteTaskFunc: func(ctx context.Context, id string) error {
			promoteWasCalled = true
			return nil
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Set up initial state with ready column having a task
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{}
	bucketed["ready"] = []tuiclient.Task{
		{ID: "task-2", Title: "Task 2", State: "ready"},
	}
	bucketed["in_progress"] = []tuiclient.Task{}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Move to ready column (from in_progress at index 2)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = m.(*BoardModel)

	// Verify we're in ready column
	if model.selectedColumn != 1 {
		t.Errorf("Expected selectedColumn 1 (ready), got %d", model.selectedColumn)
	}

	// Press 'p' on ready column (should be no-op)
	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model = m.(*BoardModel)

	// Verify promote was NOT called
	if promoteWasCalled {
		t.Errorf("Expected PromoteTask NOT to be called when on ready column")
	}

	// Command should be nil
	if cmd != nil {
		t.Errorf("Expected nil command when promoting on non-backlog column")
	}
}

// TestBoardModel_TickGeneration verifies the no-fork invariant:
//   - Init arms exactly ONE tick.
//   - A tickMsg arms exactly ONE tick (the re-arm) and issues a fetch.
//   - 'r' (manual refresh) arms ZERO ticks (issues a fetch only).
//   - tasksFetchedMsg arms ZERO ticks.
//
// The test uses an injectable newTickCmd that counts arming calls instead of using real timers.
// This test WILL FAIL if a tick is re-armed from the 'r' handler or from tasksFetchedMsg.
func TestBoardModel_TickGeneration(t *testing.T) {
	mockClient := &tuiclient.MockClient{}
	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "testuser",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	// --- Init arms exactly one tick ---
	model := NewBoardModel(mockClient, config, project)
	tickCount := countTickCmds(model)

	_ = model.Init()
	if *tickCount != 1 {
		t.Errorf("Init: expected exactly 1 tick armed, got %d", *tickCount)
	}

	// --- tasksFetchedMsg arms ZERO ticks ---
	*tickCount = 0
	bucketed := make(map[string][]tuiclient.Task)
	for _, state := range []string{"backlog", "ready", "in_progress", "review", "done"} {
		bucketed[state] = []tuiclient.Task{}
	}
	m, fetchedCmd := model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	if *tickCount != 0 {
		t.Errorf("tasksFetchedMsg: expected 0 ticks armed, got %d (tick fork regression)", *tickCount)
	}

	// fetchedCmd should be nil (tasksFetchedMsg returns no command)
	if fetchedCmd != nil {
		// Run it and check that no tickSentinel is produced
		msgs := runCmd(fetchedCmd)
		for _, msg := range msgs {
			if _, isTickSentinel := msg.(tickSentinel); isTickSentinel {
				t.Errorf("tasksFetchedMsg returned a cmd that produced a tick (tick fork regression)")
			}
		}
	}

	// --- 'r' (manual refresh) arms ZERO ticks ---
	*tickCount = 0
	m, refreshCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = m.(*BoardModel)

	if *tickCount != 0 {
		t.Errorf("'r' key: expected 0 ticks armed, got %d (tick fork regression)", *tickCount)
	}
	if refreshCmd == nil {
		t.Errorf("'r' key: expected a fetch cmd (non-nil), got nil")
	}

	// Execute the refreshCmd and confirm it produces tasksFetchedMsg, NOT a tickSentinel.
	refreshMsgs := runCmd(refreshCmd)
	hasTasksFetched := false
	hasTick := false
	for _, msg := range refreshMsgs {
		switch msg.(type) {
		case tasksFetchedMsg:
			hasTasksFetched = true
		case tickSentinel:
			hasTick = true
		}
	}
	if !hasTasksFetched {
		t.Errorf("'r' key cmd: expected a tasksFetchedMsg, got none (msgs: %v)", refreshMsgs)
	}
	if hasTick {
		t.Errorf("'r' key cmd: produced a tick (tick fork regression)")
	}

	// --- tickMsg arms exactly ONE tick and issues a fetch ---
	*tickCount = 0
	m, tickCmdResult := model.Update(tickMsg{})
	model = m.(*BoardModel)

	if *tickCount != 1 {
		t.Errorf("tickMsg: expected exactly 1 tick armed, got %d", *tickCount)
	}
	if tickCmdResult == nil {
		t.Errorf("tickMsg: expected a non-nil batch cmd (fetch + tick), got nil")
	}

	// Execute the batch from tickMsg and confirm it produces BOTH a tasksFetchedMsg AND a tickSentinel.
	tickMsgs := runCmd(tickCmdResult)
	hasTasksFetchedFromTick := false
	hasTickFromTick := false
	for _, msg := range tickMsgs {
		switch msg.(type) {
		case tasksFetchedMsg:
			hasTasksFetchedFromTick = true
		case tickSentinel:
			hasTickFromTick = true
		}
	}
	if !hasTasksFetchedFromTick {
		t.Errorf("tickMsg batch: expected a tasksFetchedMsg, got none (msgs: %v)", tickMsgs)
	}
	if !hasTickFromTick {
		t.Errorf("tickMsg batch: expected a tickSentinel (re-armed tick), got none (msgs: %v)", tickMsgs)
	}

	// --- tasksFetchedMsg with error arms ZERO ticks ---
	*tickCount = 0
	m, errFetchedCmd := model.Update(tasksFetchedMsg{err: errors.New("network error")})
	_ = m

	if *tickCount != 0 {
		t.Errorf("tasksFetchedMsg(error): expected 0 ticks armed, got %d (tick fork regression)", *tickCount)
	}
	if errFetchedCmd != nil {
		msgs := runCmd(errFetchedCmd)
		for _, msg := range msgs {
			if _, isTickSentinel := msg.(tickSentinel); isTickSentinel {
				t.Errorf("tasksFetchedMsg(error) returned a cmd that produced a tick (tick fork regression)")
			}
		}
	}
}

// --- TUI-4: Review action tests ---

// reviewTestBucketed returns a bucketed task map with one review task.
func reviewTestBucketed(reviewTaskID string) map[string][]tuiclient.Task {
	bucketed := make(map[string][]tuiclient.Task)
	for _, state := range []string{"backlog", "ready", "in_progress", "review", "done"} {
		bucketed[state] = []tuiclient.Task{}
	}
	bucketed["review"] = []tuiclient.Task{
		{ID: reviewTaskID, Title: "Review Task", State: "review"},
	}
	return bucketed
}

// buildReviewModel returns a board model set up in the review column with one review task
// selected. reviewClient is the mock client to inject.
func buildReviewModel(t *testing.T, reviewClient *tuiclient.MockClient) *BoardModel {
	t.Helper()

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "reviewer-bot",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(reviewClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Load a review task into the board.
	m, _ = model.Update(tasksFetchedMsg{tasks: reviewTestBucketed("review-task-1")})
	model = m.(*BoardModel)

	// Navigate to the review column (index 3). Starting from in_progress (index 2), press right once.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = m.(*BoardModel)

	if model.selectedColumn != 3 {
		t.Fatalf("buildReviewModel: expected selectedColumn 3 (review), got %d", model.selectedColumn)
	}
	if model.selectedTaskID != "review-task-1" {
		t.Fatalf("buildReviewModel: expected selectedTaskID review-task-1, got %q", model.selectedTaskID)
	}

	return model
}

// executeReviewCmd synchronously runs a tea.Cmd produced by a review action and delivers
// the resulting message back into the model. It returns the updated model.
func executeReviewCmd(t *testing.T, model *BoardModel, cmd tea.Cmd) *BoardModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("executeReviewCmd: expected non-nil cmd")
	}
	resultMsg := cmd()
	m, _ := model.Update(resultMsg)
	return m.(*BoardModel)
}

// TestBoardModel_ApproveFlow_ConfirmY tests the full approve flow with a non-empty note,
// confirmed with 'y'. Both ReviewTask(approve) and TransitionTask(done) must be called
// with the correct actor, verdict, and note — in that order. A refetch follows.
func TestBoardModel_ApproveFlow_ConfirmY(t *testing.T) {
	var callLog []string // ordered log of calls
	var capturedReviewID, capturedReviewActor, capturedReviewVerdict string
	var capturedReviewNote *string
	var capturedTransitionID, capturedTransitionTo string
	var capturedTransitionNote *string

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			callLog = append(callLog, "review")
			capturedReviewID = id
			capturedReviewActor = actor
			capturedReviewVerdict = verdict
			capturedReviewNote = note
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			callLog = append(callLog, "transition")
			capturedTransitionID = id
			capturedTransitionTo = to
			capturedTransitionNote = note
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			callLog = append(callLog, "refetch")
			// After approve, the task should be in done.
			return []tuiclient.Task{
				{ID: "review-task-1", Title: "Review Task", State: "done"},
			}, nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Press 'a' to start approve flow.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)

	if model.mode != modeApproveNote {
		t.Fatalf("Expected modeApproveNote after 'a', got mode %d", model.mode)
	}

	// Type a note.
	for _, ch := range "LGTM" {
		m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		model = m.(*BoardModel)
	}

	// Press enter to advance to confirm step.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	if model.mode != modeApproveConfirm {
		t.Fatalf("Expected modeApproveConfirm after enter, got mode %d", model.mode)
	}

	// Confirm the output contains the confirm prompt.
	output := model.View()
	if !strings.Contains(output, "→ done?") {
		t.Errorf("Expected confirm prompt in view, got:\n%s", output)
	}

	// Press 'y' to confirm.
	m, approveCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = m.(*BoardModel)

	if model.mode != modeNormal {
		t.Errorf("Expected modeNormal after confirm, got mode %d", model.mode)
	}

	// Execute the approve command (runs ReviewTask then TransitionTask then refetch inline).
	model = executeReviewCmd(t, model, approveCmd)

	// Verify call order: review → transition → refetch.
	if len(callLog) != 3 {
		t.Fatalf("Expected 3 calls (review, transition, refetch), got %d: %v", len(callLog), callLog)
	}
	if callLog[0] != "review" {
		t.Errorf("Expected first call to be 'review', got %q", callLog[0])
	}
	if callLog[1] != "transition" {
		t.Errorf("Expected second call to be 'transition', got %q", callLog[1])
	}
	if callLog[2] != "refetch" {
		t.Errorf("Expected third call to be 'refetch', got %q", callLog[2])
	}

	// Verify ReviewTask arguments.
	if capturedReviewID != "review-task-1" {
		t.Errorf("ReviewTask: expected task ID review-task-1, got %q", capturedReviewID)
	}
	if capturedReviewActor != "reviewer-bot" {
		t.Errorf("ReviewTask: expected actor reviewer-bot, got %q", capturedReviewActor)
	}
	if capturedReviewVerdict != "approve" {
		t.Errorf("ReviewTask: expected verdict approve, got %q", capturedReviewVerdict)
	}
	if capturedReviewNote == nil || *capturedReviewNote != "LGTM" {
		t.Errorf("ReviewTask: expected note 'LGTM', got %v", capturedReviewNote)
	}

	// Verify TransitionTask arguments.
	if capturedTransitionID != "review-task-1" {
		t.Errorf("TransitionTask: expected task ID review-task-1, got %q", capturedTransitionID)
	}
	if capturedTransitionTo != "done" {
		t.Errorf("TransitionTask: expected to=done, got %q", capturedTransitionTo)
	}
	if capturedTransitionNote == nil || *capturedTransitionNote != "LGTM" {
		t.Errorf("TransitionTask: expected note 'LGTM', got %v", capturedTransitionNote)
	}

	// Board should now show the task in done.
	output = model.View()
	if !strings.Contains(output, "done(1)") {
		t.Errorf("Expected done(1) after approve, got:\n%s", output)
	}
}

// TestBoardModel_ApproveFlow_EmptyNote tests that approving with an empty note passes nil
// (not a pointer to an empty string) to both API calls.
func TestBoardModel_ApproveFlow_EmptyNote(t *testing.T) {
	var capturedReviewNote *string
	var capturedTransitionNote *string

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			capturedReviewNote = note
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			capturedTransitionNote = note
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			return []tuiclient.Task{}, nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Press 'a' → skip note (press enter immediately) → confirm 'y'.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)

	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	m, approveCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = m.(*BoardModel)

	model = executeReviewCmd(t, model, approveCmd)

	// Both calls should receive nil (not an empty string pointer).
	if capturedReviewNote != nil {
		t.Errorf("ReviewTask: expected nil note for empty input, got %q", *capturedReviewNote)
	}
	if capturedTransitionNote != nil {
		t.Errorf("TransitionTask: expected nil note for empty input, got %q", *capturedTransitionNote)
	}
	_ = model
}

// TestBoardModel_ApproveFlow_CancelWithEscAtNote tests that pressing esc at the note step
// cancels the action entirely without making any API calls.
func TestBoardModel_ApproveFlow_CancelWithEscAtNote(t *testing.T) {
	reviewCalled := false
	transitionCalled := false

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			reviewCalled = true
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			return nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Press 'a' then esc.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)

	if model.mode != modeApproveNote {
		t.Fatalf("Expected modeApproveNote, got %d", model.mode)
	}

	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = m.(*BoardModel)

	if model.mode != modeNormal {
		t.Errorf("Expected modeNormal after esc, got mode %d", model.mode)
	}
	if cmd != nil {
		t.Errorf("Expected nil cmd after esc cancel, got non-nil")
	}
	if reviewCalled {
		t.Errorf("ReviewTask was called but should not have been (action cancelled)")
	}
	if transitionCalled {
		t.Errorf("TransitionTask was called but should not have been (action cancelled)")
	}
}

// TestBoardModel_ApproveFlow_CancelWithN tests that pressing 'n' at the confirm step
// cancels without making any API calls.
func TestBoardModel_ApproveFlow_CancelWithN(t *testing.T) {
	reviewCalled := false
	transitionCalled := false

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			reviewCalled = true
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			return nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// 'a' → enter (skip note) → 'n' (decline confirm).
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	if model.mode != modeApproveConfirm {
		t.Fatalf("Expected modeApproveConfirm, got %d", model.mode)
	}

	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = m.(*BoardModel)

	if model.mode != modeNormal {
		t.Errorf("Expected modeNormal after 'n', got mode %d", model.mode)
	}
	if cmd != nil {
		t.Errorf("Expected nil cmd after 'n' cancel, got non-nil")
	}
	if reviewCalled {
		t.Errorf("ReviewTask was called but should not have been (declined)")
	}
	if transitionCalled {
		t.Errorf("TransitionTask was called but should not have been (declined)")
	}
}

// TestBoardModel_RejectFlow_EmptyReasonBlocked tests that submitting an empty reason does
// not call any API and shows a hint to the user.
func TestBoardModel_RejectFlow_EmptyReasonBlocked(t *testing.T) {
	reviewCalled := false
	transitionCalled := false

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			reviewCalled = true
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			return nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Press 'x' to start reject flow.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = m.(*BoardModel)

	if model.mode != modeRejectReason {
		t.Fatalf("Expected modeRejectReason after 'x', got %d", model.mode)
	}

	// Press enter immediately with no reason typed.
	m, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	// Must still be in input mode.
	if model.mode != modeRejectReason {
		t.Errorf("Expected to stay in modeRejectReason on empty submit, got mode %d", model.mode)
	}
	// No API calls.
	if reviewCalled {
		t.Errorf("ReviewTask was called but should not have been (empty reason)")
	}
	if transitionCalled {
		t.Errorf("TransitionTask was called but should not have been (empty reason)")
	}
	// Should produce no command.
	if cmd != nil {
		t.Errorf("Expected nil cmd on empty reason submit, got non-nil")
	}

	// Hint should be visible in the view.
	output := model.View()
	if !strings.Contains(output, "reason is required") {
		t.Errorf("Expected 'reason is required' hint in view, got:\n%s", output)
	}
}

// TestBoardModel_RejectFlow_NonEmptyReason tests the full reject flow with a non-empty reason.
// Both ReviewTask(reject) and TransitionTask(ready) must be called with the correct args in order.
func TestBoardModel_RejectFlow_NonEmptyReason(t *testing.T) {
	var callLog []string
	var capturedReviewID, capturedReviewActor, capturedReviewVerdict string
	var capturedReviewNote *string
	var capturedTransitionID, capturedTransitionTo string
	var capturedTransitionNote *string

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			callLog = append(callLog, "review")
			capturedReviewID = id
			capturedReviewActor = actor
			capturedReviewVerdict = verdict
			capturedReviewNote = note
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			callLog = append(callLog, "transition")
			capturedTransitionID = id
			capturedTransitionTo = to
			capturedTransitionNote = note
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			callLog = append(callLog, "refetch")
			// After reject, the task returns to ready.
			return []tuiclient.Task{
				{ID: "review-task-1", Title: "Review Task", State: "ready"},
			}, nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Press 'x' to start reject flow.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = m.(*BoardModel)

	// Type a reason.
	for _, ch := range "needs tests" {
		m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		model = m.(*BoardModel)
	}

	// Submit with enter.
	m, rejectCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	if model.mode != modeNormal {
		t.Errorf("Expected modeNormal after submit, got mode %d", model.mode)
	}

	// Execute the reject command.
	model = executeReviewCmd(t, model, rejectCmd)

	// Verify call order.
	if len(callLog) != 3 {
		t.Fatalf("Expected 3 calls (review, transition, refetch), got %d: %v", len(callLog), callLog)
	}
	if callLog[0] != "review" {
		t.Errorf("Expected first call 'review', got %q", callLog[0])
	}
	if callLog[1] != "transition" {
		t.Errorf("Expected second call 'transition', got %q", callLog[1])
	}
	if callLog[2] != "refetch" {
		t.Errorf("Expected third call 'refetch', got %q", callLog[2])
	}

	// Verify ReviewTask arguments.
	if capturedReviewID != "review-task-1" {
		t.Errorf("ReviewTask: expected task ID review-task-1, got %q", capturedReviewID)
	}
	if capturedReviewActor != "reviewer-bot" {
		t.Errorf("ReviewTask: expected actor reviewer-bot, got %q", capturedReviewActor)
	}
	if capturedReviewVerdict != "reject" {
		t.Errorf("ReviewTask: expected verdict reject, got %q", capturedReviewVerdict)
	}
	if capturedReviewNote == nil || *capturedReviewNote != "needs tests" {
		t.Errorf("ReviewTask: expected note 'needs tests', got %v", capturedReviewNote)
	}

	// Verify TransitionTask arguments.
	if capturedTransitionID != "review-task-1" {
		t.Errorf("TransitionTask: expected task ID review-task-1, got %q", capturedTransitionID)
	}
	if capturedTransitionTo != "ready" {
		t.Errorf("TransitionTask: expected to=ready, got %q", capturedTransitionTo)
	}
	if capturedTransitionNote == nil || *capturedTransitionNote != "needs tests" {
		t.Errorf("TransitionTask: expected note 'needs tests', got %v", capturedTransitionNote)
	}

	// Board should reflect the refetch.
	output := model.View()
	if !strings.Contains(output, "ready(1)") {
		t.Errorf("Expected ready(1) after reject refetch, got:\n%s", output)
	}
}

// TestBoardModel_ApproveTransition409 tests that a 409 from TransitionTask (after a successful
// ReviewTask) surfaces an error message in the model and triggers a refetch — it must not crash.
func TestBoardModel_ApproveTransition409(t *testing.T) {
	reviewCalled := false
	transitionCalled := false

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			reviewCalled = true
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			// Return a 409 as the real HTTPClient would.
			return &tuiclient.APIError{
				StatusCode: http.StatusConflict,
				Code:       "CONFLICT",
				Message:    "task already transitioned",
			}
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			// After the 409, refetch should still work.
			return []tuiclient.Task{
				{ID: "review-task-1", Title: "Review Task", State: "done"},
			}, nil
		},
	}

	model := buildReviewModel(t, mockClient)

	// Walk through the approve flow.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter}) // skip note
	model = m.(*BoardModel)
	m, approveCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}) // confirm
	model = m.(*BoardModel)

	// Execute the command.
	model = executeReviewCmd(t, model, approveCmd)

	// Both calls must have been made.
	if !reviewCalled {
		t.Errorf("ReviewTask was not called")
	}
	if !transitionCalled {
		t.Errorf("TransitionTask was not called")
	}

	// The model must surface the 409 via the error field but still render (no crash).
	if model.error == "" {
		t.Errorf("Expected non-empty error after 409 transition, got empty")
	}
	if !strings.Contains(model.error, "409") && !strings.Contains(model.error, "task already transitioned") {
		t.Errorf("Expected 409 message in error, got: %q", model.error)
	}

	// Model must still be usable — View must not panic.
	output := model.View()
	if output == "" {
		t.Errorf("Expected non-empty view after 409 error, got empty")
	}

	// The board was refreshed even after the 409.
	if !strings.Contains(output, "done(1)") {
		t.Errorf("Expected done(1) in view after refetch following 409, got:\n%s", output)
	}
}

// TestBoardModel_ReviewActionsNoopOutsideReviewColumn tests that pressing 'a' or 'x'
// when the active column is NOT review (index 3) does nothing — no API calls, no mode change.
func TestBoardModel_ReviewActionsNoopOutsideReviewColumn(t *testing.T) {
	reviewCalled := false
	transitionCalled := false

	mockClient := &tuiclient.MockClient{
		ReviewTaskFunc: func(ctx context.Context, id, actor, verdict string, note *string) error {
			reviewCalled = true
			return nil
		},
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			return nil
		},
	}

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "reviewer-bot",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Load a task in in_progress (default column, index 2) and one in backlog.
	bucketed := make(map[string][]tuiclient.Task)
	bucketed["backlog"] = []tuiclient.Task{{ID: "task-b", Title: "Backlog Task", State: "backlog"}}
	bucketed["ready"] = []tuiclient.Task{}
	bucketed["in_progress"] = []tuiclient.Task{{ID: "task-ip", Title: "IP Task", State: "in_progress"}}
	bucketed["review"] = []tuiclient.Task{}
	bucketed["done"] = []tuiclient.Task{}

	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Sanity check: model starts in in_progress (index 2).
	if model.selectedColumn != 2 {
		t.Fatalf("Expected selectedColumn 2 (in_progress), got %d", model.selectedColumn)
	}

	// Press 'a' in in_progress — must be a no-op.
	m, cmdA := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)
	if model.mode != modeNormal {
		t.Errorf("'a' in in_progress: expected modeNormal, got %d", model.mode)
	}
	if cmdA != nil {
		t.Errorf("'a' in in_progress: expected nil cmd, got non-nil")
	}

	// Press 'x' in in_progress — must be a no-op.
	m, cmdX := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = m.(*BoardModel)
	if model.mode != modeNormal {
		t.Errorf("'x' in in_progress: expected modeNormal, got %d", model.mode)
	}
	if cmdX != nil {
		t.Errorf("'x' in in_progress: expected nil cmd, got non-nil")
	}

	// Navigate to backlog (two lefts), press 'a' — still a no-op.
	for i := 0; i < 2; i++ {
		m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
		model = m.(*BoardModel)
	}
	m, cmdABacklog := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = m.(*BoardModel)
	if model.mode != modeNormal {
		t.Errorf("'a' in backlog: expected modeNormal, got %d", model.mode)
	}
	if cmdABacklog != nil {
		t.Errorf("'a' in backlog: expected nil cmd, got non-nil")
	}

	// None of the API calls should have fired.
	if reviewCalled {
		t.Errorf("ReviewTask was called but should not have been outside review column")
	}
	if transitionCalled {
		t.Errorf("TransitionTask was called but should not have been outside review column")
	}
}

// TestBoardModel_ReviewHelpBar verifies that the help bar shows approve/reject hints only
// when the active column is review, and omits them elsewhere.
func TestBoardModel_ReviewHelpBar(t *testing.T) {
	mockClient := &tuiclient.MockClient{}
	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "reviewer-bot",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(mockClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	bucketed := make(map[string][]tuiclient.Task)
	for _, s := range []string{"backlog", "ready", "in_progress", "done"} {
		bucketed[s] = []tuiclient.Task{}
	}
	bucketed["review"] = []tuiclient.Task{{ID: "r1", Title: "R1", State: "review"}}
	m, _ = model.Update(tasksFetchedMsg{tasks: bucketed})
	model = m.(*BoardModel)

	// Navigate to review column (index 3): one right from in_progress (index 2).
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = m.(*BoardModel)

	if model.selectedColumn != 3 {
		t.Fatalf("Expected review column (3), got %d", model.selectedColumn)
	}

	reviewOutput := model.View()
	if !strings.Contains(reviewOutput, "a approve") {
		t.Errorf("Expected 'a approve' in help bar when in review column, got:\n%s", reviewOutput)
	}
	if !strings.Contains(reviewOutput, "x reject") {
		t.Errorf("Expected 'x reject' in help bar when in review column, got:\n%s", reviewOutput)
	}

	// Navigate away to in_progress.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = m.(*BoardModel)

	nonReviewOutput := model.View()
	if strings.Contains(nonReviewOutput, "a approve") {
		t.Errorf("Did not expect 'a approve' in help bar when NOT in review column, got:\n%s", nonReviewOutput)
	}
	if strings.Contains(nonReviewOutput, "x reject") {
		t.Errorf("Did not expect 'x reject' in help bar when NOT in review column, got:\n%s", nonReviewOutput)
	}
}
