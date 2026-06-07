package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
	"github.com/boldfield/agentask/internal/tuiconfig"
	tea "github.com/charmbracelet/bubbletea"
)

// approvedTestBucketed returns a bucketed task map with one approved task.
// The PR link is only available in the TaskDetail (fetched when entering detail mode).
func approvedTestBucketed(approvedTaskID string) map[string][]tuiclient.Task {
	bucketed := make(map[string][]tuiclient.Task)
	for _, state := range []string{"backlog", "ready", "in_progress", "review", "approved", "done"} {
		bucketed[state] = []tuiclient.Task{}
	}
	bucketed["approved"] = []tuiclient.Task{
		{
			ID:    approvedTaskID,
			Title: "Approved Task",
			State: "approved",
		},
	}
	return bucketed
}

// buildApprovedModel returns a board model set up in the approved column with one approved
// task selected. approvedClient is the mock client to inject.
func buildApprovedModel(t *testing.T, approvedClient *tuiclient.MockClient) *BoardModel {
	t.Helper()

	config := &tuiconfig.Config{
		URL:          "http://test",
		Token:        "test",
		Actor:        "reviewer-bot",
		PollInterval: 100 * time.Millisecond,
	}
	project := tuiclient.Project{ID: "project-1", Name: "Test"}

	model := NewBoardModel(approvedClient, config, project)
	m, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = m.(*BoardModel)

	// Load an approved task into the board.
	m, _ = model.Update(tasksFetchedMsg{tasks: approvedTestBucketed("approved-task-1")})
	model = m.(*BoardModel)

	// Navigate to the approved column (index 4). Starting from in_progress (index 2), press right twice.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = m.(*BoardModel)
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = m.(*BoardModel)

	if model.selectedColumn != 4 {
		t.Fatalf("buildApprovedModel: expected selectedColumn 4 (approved), got %d", model.selectedColumn)
	}
	if model.selectedTaskID != "approved-task-1" {
		t.Fatalf("buildApprovedModel: expected selectedTaskID approved-task-1, got %q", model.selectedTaskID)
	}

	return model
}

// TestBoardModel_MergePRConfirmFlow tests the merge PR confirmation flow in detail view.
// When a task in approved state is selected and 'm' is pressed, it should show a merge confirmation prompt.
func TestBoardModel_MergePRConfirmFlow(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		GetTaskFunc: func(ctx context.Context, taskID string) (tuiclient.TaskDetail, error) {
			return tuiclient.TaskDetail{
				ID:    "approved-task-1",
				Title: "Approved Task",
				State: "approved",
				Links: []tuiclient.TaskLink{
					{Kind: "pr", Value: "owner/repo#123"},
				},
				Spec: "Task spec",
			}, nil
		},
		ListDocumentsFunc: func(ctx context.Context, projectID string) ([]tuiclient.Document, error) {
			return []tuiclient.Document{}, nil
		},
	}

	model := buildApprovedModel(t, mockClient)

	// Enter detail mode by pressing enter on the approved task.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	if model.mode != modeDetail {
		t.Fatalf("Expected modeDetail after enter, got mode %d", model.mode)
	}

	// Press 'm' to start merge flow.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = m.(*BoardModel)

	if model.mode != modeMergeConfirm {
		t.Fatalf("Expected modeMergeConfirm after 'm', got mode %d", model.mode)
	}

	if model.pendingPRURL != "owner/repo#123" {
		t.Errorf("Expected pendingPRURL 'owner/repo#123', got %q", model.pendingPRURL)
	}

	// Verify the output contains the merge confirmation prompt.
	output := model.View()
	if !strings.Contains(output, "owner/repo#123") {
		t.Errorf("Expected PR URL in merge confirmation, got:\n%s", output)
	}
}

// TestBoardModel_MergePRNoPRLink tests that pressing 'm' on an approved task with no PR link
// shows a clear message.
func TestBoardModel_MergePRNoPRLink(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		GetTaskFunc: func(ctx context.Context, taskID string) (tuiclient.TaskDetail, error) {
			return tuiclient.TaskDetail{
				ID:    "approved-task-1",
				Title: "Approved Task",
				State: "approved",
				Links: []tuiclient.TaskLink{}, // No PR link
				Spec:  "Task spec",
			}, nil
		},
		ListDocumentsFunc: func(ctx context.Context, projectID string) ([]tuiclient.Document, error) {
			return []tuiclient.Document{}, nil
		},
	}

	model := buildApprovedModel(t, mockClient)

	// Enter detail mode and try to merge.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = m.(*BoardModel)

	// Should stay in detail mode with an error message.
	if model.mode != modeDetail {
		t.Fatalf("Expected modeDetail (merge not initiated), got mode %d", model.mode)
	}

	if !strings.Contains(model.detailMessage, "no PR link") {
		t.Errorf("Expected 'no PR link' message, got %q", model.detailMessage)
	}
}

// TestBoardModel_MergePRSuccess tests the full merge and transition flow when confirmed with 'y'.
// The merge must be called first, then the transition to done only if merge succeeds.
func TestBoardModel_MergePRSuccess(t *testing.T) {
	var callLog []string
	var capturedTransitionID, capturedTransitionTo string
	var capturedMergePRURL string

	mockClient := &tuiclient.MockClient{
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			callLog = append(callLog, "transition")
			capturedTransitionID = id
			capturedTransitionTo = to
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			callLog = append(callLog, "refetch")
			return []tuiclient.Task{
				{ID: "approved-task-1", Title: "Approved Task", State: "done"},
			}, nil
		},
	}

	model := buildApprovedModel(t, mockClient)

	// Enter detail mode and initiate merge.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = m.(*BoardModel)

	// Confirm with 'y'.
	m, mergeCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = m.(*BoardModel)

	if model.mode != modeNormal {
		t.Errorf("Expected modeNormal after confirm, got mode %d", model.mode)
	}

	// Inject a mock ghMerger that records the PR URL.
	model.ghMerger = func(prURL string) error {
		callLog = append(callLog, "merge")
		capturedMergePRURL = prURL
		return nil
	}

	// Execute the merge command.
	model = executeReviewCmd(t, model, mergeCmd)

	// Verify call order: merge → transition → refetch.
	if len(callLog) != 3 {
		t.Fatalf("Expected 3 calls (merge, transition, refetch), got %d: %v", len(callLog), callLog)
	}
	if callLog[0] != "merge" {
		t.Errorf("Expected first call to be 'merge', got %q", callLog[0])
	}
	if callLog[1] != "transition" {
		t.Errorf("Expected second call to be 'transition', got %q", callLog[1])
	}
	if callLog[2] != "refetch" {
		t.Errorf("Expected third call to be 'refetch', got %q", callLog[2])
	}

	// Verify merge was called with the correct PR URL.
	if capturedMergePRURL != "owner/repo#123" {
		t.Errorf("Expected merge with PR 'owner/repo#123', got %q", capturedMergePRURL)
	}

	// Verify transition was called with the correct ID and target state.
	if capturedTransitionID != "approved-task-1" {
		t.Errorf("TransitionTask: expected task ID approved-task-1, got %q", capturedTransitionID)
	}
	if capturedTransitionTo != "done" {
		t.Errorf("TransitionTask: expected to=done, got %q", capturedTransitionTo)
	}
}

// TestBoardModel_MergePRFailure tests that when the gh merge fails, the error is surfaced
// and the task is not transitioned to done.
func TestBoardModel_MergePRFailure(t *testing.T) {
	var callLog []string
	var transitionCalled bool

	mockClient := &tuiclient.MockClient{
		TransitionTaskFunc: func(ctx context.Context, id, to string, note *string) error {
			transitionCalled = true
			return nil
		},
		ListTasksFunc: func(ctx context.Context, projectID string) ([]tuiclient.Task, error) {
			callLog = append(callLog, "refetch")
			return []tuiclient.Task{
				{ID: "approved-task-1", Title: "Approved Task", State: "approved"},
			}, nil
		},
	}

	model := buildApprovedModel(t, mockClient)

	// Enter detail mode and initiate merge.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = m.(*BoardModel)

	// Confirm with 'y'.
	m, mergeCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = m.(*BoardModel)

	// Inject a failing ghMerger.
	model.ghMerger = func(prURL string) error {
		callLog = append(callLog, "merge")
		return fmt.Errorf("merge rejected: PR has conflicts")
	}

	// Execute the merge command.
	model = executeReviewCmd(t, model, mergeCmd)

	// Verify merge was called but transition was not.
	if len(callLog) == 0 || callLog[0] != "merge" {
		t.Errorf("Expected merge to be called first, got %v", callLog)
	}

	if transitionCalled {
		t.Errorf("TransitionTask should not have been called after merge failure")
	}

	// Verify error message contains merge failure.
	if !strings.Contains(model.error, "merge rejected") {
		t.Errorf("Expected error to contain merge failure, got %q", model.error)
	}

	// Verify the task is still in approved state (refetch happened but no transition).
	output := model.View()
	if !strings.Contains(output, "approved(1)") {
		t.Errorf("Expected task to remain in approved state, got:\n%s", output)
	}
}

// TestBoardModel_ApprovedHelpBar tests that the help bar shows 'm merge & complete' when
// viewing an approved task in detail, and hides it elsewhere.
func TestBoardModel_ApprovedHelpBar(t *testing.T) {
	mockClient := &tuiclient.MockClient{
		GetTaskFunc: func(ctx context.Context, taskID string) (tuiclient.TaskDetail, error) {
			return tuiclient.TaskDetail{
				ID:    "approved-task-1",
				Title: "Approved Task",
				State: "approved",
				Links: []tuiclient.TaskLink{
					{Kind: "pr", Value: "owner/repo#123"},
				},
				Spec: "Task spec",
			}, nil
		},
		ListDocumentsFunc: func(ctx context.Context, projectID string) ([]tuiclient.Document, error) {
			return []tuiclient.Document{}, nil
		},
	}

	model := buildApprovedModel(t, mockClient)

	// Enter detail mode on approved task.
	m, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = m.(*BoardModel)

	approvedOutput := model.View()
	if !strings.Contains(approvedOutput, "m merge & complete") {
		t.Errorf("Expected 'm merge & complete' in help bar for approved task, got:\n%s", approvedOutput)
	}

	// Exit detail mode and return to board.
	m, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = m.(*BoardModel)

	// The help bar on the board should not show the merge action.
	boardOutput := model.View()
	if strings.Contains(boardOutput, "m merge") {
		t.Errorf("Did not expect 'm merge' in board help bar, got:\n%s", boardOutput)
	}
}
