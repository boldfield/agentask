package main

import (
	"context"
	"errors"
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
