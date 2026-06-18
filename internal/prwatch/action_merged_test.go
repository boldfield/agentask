package prwatch

import (
	"context"
	"errors"
	"testing"

	"github.com/boldfield/agentask/internal/notify"
	"github.com/boldfield/agentask/internal/store"
)

type fakeTaskTx struct {
	transitionCalled bool
	transitionID     string
	transitionState  string
	transitionNote   *string
	transitionErr    error
}

func (f *fakeTaskTx) TransitionTask(ctx context.Context, id, toState string, note *string) (store.Task, error) {
	f.transitionCalled = true
	f.transitionID = id
	f.transitionState = toState
	f.transitionNote = note
	return store.Task{}, f.transitionErr
}

type fakeNotifier struct {
	publishCalled bool
	notification  notify.Notification
	publishErr    error
}

func (f *fakeNotifier) Publish(ctx context.Context, n notify.Notification) error {
	f.publishCalled = true
	f.notification = n
	return f.publishErr
}

func TestApplyMergedTransition(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{}
	notifier := &fakeNotifier{}

	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	err := applyMerged(ctx, tx, notifier, task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !tx.transitionCalled {
		t.Fatal("TransitionTask was not called")
	}

	if tx.transitionID != "task-123" {
		t.Errorf("expected task ID 'task-123', got %q", tx.transitionID)
	}

	if tx.transitionState != "done" {
		t.Errorf("expected state 'done', got %q", tx.transitionState)
	}

	if tx.transitionNote != nil {
		t.Errorf("expected nil note, got %v", tx.transitionNote)
	}
}

func TestApplyMergedNotification(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{}
	notifier := &fakeNotifier{}

	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	err := applyMerged(ctx, tx, notifier, task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !notifier.publishCalled {
		t.Fatal("Publish was not called")
	}

	if notifier.notification.Event != "agentask-merged" {
		t.Errorf("expected event 'agentask-merged', got %q", notifier.notification.Event)
	}

	if notifier.notification.Title != "Merged: Test Task" {
		t.Errorf("expected title 'Merged: Test Task', got %q", notifier.notification.Title)
	}

	if notifier.notification.Priority != 4 {
		t.Errorf("expected priority 4, got %d", notifier.notification.Priority)
	}

	if notifier.notification.DedupKey != "agentask-merged:task-123" {
		t.Errorf("expected dedupkey 'agentask-merged:task-123', got %q", notifier.notification.DedupKey)
	}
}

func TestApplyMergedTransitionError(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{transitionErr: errors.New("transition failed")}
	notifier := &fakeNotifier{}

	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	err := applyMerged(ctx, tx, notifier, task)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "transition failed" {
		t.Errorf("expected error 'transition failed', got %v", err)
	}

	if notifier.publishCalled {
		t.Fatal("Publish should not be called when transition fails")
	}
}

func TestApplyMergedNotifyError(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{}
	notifier := &fakeNotifier{publishErr: errors.New("notify failed")}

	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	err := applyMerged(ctx, tx, notifier, task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !tx.transitionCalled {
		t.Fatal("TransitionTask should be called even if notify fails")
	}
}
