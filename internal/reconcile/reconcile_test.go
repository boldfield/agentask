package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeReconciler is a test implementation of Reconciler.
type fakeReconciler struct {
	name        string
	callCount   int
	shouldError bool
}

func (f *fakeReconciler) Name() string {
	return f.name
}

func (f *fakeReconciler) Reconcile(ctx context.Context) error {
	f.callCount++
	if f.shouldError {
		return errors.New("fake error")
	}
	return nil
}

func TestRunnerRunsMultipleTimes(t *testing.T) {
	rec := &fakeReconciler{name: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(10*time.Millisecond, logger, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	runner.Run(ctx)

	// Should have run at least 2 times (initial pass + at least 1 tick)
	if rec.callCount < 2 {
		t.Errorf("expected at least 2 runs, got %d", rec.callCount)
	}
}

func TestErroringReconcilerDoesNotStopRunner(t *testing.T) {
	errRec := &fakeReconciler{name: "error", shouldError: true}
	goodRec := &fakeReconciler{name: "good", shouldError: false}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(10*time.Millisecond, logger, errRec, goodRec)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	runner.Run(ctx)

	// Both should have run multiple times
	if errRec.callCount < 2 {
		t.Errorf("error reconciler: expected at least 2 runs, got %d", errRec.callCount)
	}
	if goodRec.callCount < 2 {
		t.Errorf("good reconciler: expected at least 2 runs, got %d", goodRec.callCount)
	}

	// Verify they both ran the same number of times (same number of passes)
	if errRec.callCount != goodRec.callCount {
		t.Errorf("reconcilers ran different number of times: %d vs %d", errRec.callCount, goodRec.callCount)
	}
}

func TestContextCancelReturnsPromptly(t *testing.T) {
	rec := &fakeReconciler{name: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(1*time.Second, logger, rec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	// Wait a bit, then cancel
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Should return promptly (within 100ms)
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Run did not return promptly after context cancel")
	}
}

func TestInitialPassRunsImmediately(t *testing.T) {
	rec := &fakeReconciler{name: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(100*time.Millisecond, logger, rec)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	runner.Run(ctx)

	// Should have run at least once (initial pass) even with short timeout
	if rec.callCount < 1 {
		t.Errorf("expected at least 1 run (initial pass), got %d", rec.callCount)
	}
}
