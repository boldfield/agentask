package main

import (
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
