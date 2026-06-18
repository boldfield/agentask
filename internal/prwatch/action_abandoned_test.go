package prwatch

import (
	"context"
	"errors"
	"testing"

	"github.com/boldfield/agentask/internal/store"
)

func TestApplyAbandonedTransition(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{}

	task := store.Task{
		ID:    "task-456",
		Title: "Abandoned Task",
	}

	reason := "PR closed without merging"

	err := applyAbandoned(ctx, tx, task, reason)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !tx.transitionCalled {
		t.Fatal("TransitionTask was not called")
	}

	if tx.transitionID != "task-456" {
		t.Errorf("expected task ID 'task-456', got %q", tx.transitionID)
	}

	if tx.transitionState != "abandoned" {
		t.Errorf("expected state 'abandoned', got %q", tx.transitionState)
	}

	if tx.transitionNote == nil {
		t.Fatal("expected non-nil note")
	}

	if *tx.transitionNote != reason {
		t.Errorf("expected note %q, got %q", reason, *tx.transitionNote)
	}
}

func TestApplyAbandonedTransitionError(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{transitionErr: errors.New("transition failed")}

	task := store.Task{
		ID:    "task-456",
		Title: "Abandoned Task",
	}

	err := applyAbandoned(ctx, tx, task, "PR closed without merging")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "transition failed" {
		t.Errorf("expected error 'transition failed', got %v", err)
	}
}
