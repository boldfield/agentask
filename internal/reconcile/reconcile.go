package reconcile

import (
	"context"
	"log/slog"
	"time"
)

// Reconciler defines the interface for a reconciliation handler.
type Reconciler interface {
	Name() string
	Reconcile(ctx context.Context) error
}

// Runner orchestrates multiple reconcilers on a fixed interval.
type Runner struct {
	interval   time.Duration
	logger     *slog.Logger
	reconcilers []Reconciler
}

// NewRunner creates a new Runner with the given interval, logger, and reconcilers.
func NewRunner(interval time.Duration, logger *slog.Logger, recs ...Reconciler) *Runner {
	return &Runner{
		interval:    interval,
		logger:      logger,
		reconcilers: recs,
	}
}

// Run executes all reconcilers on each tick of the interval until ctx is cancelled.
// It runs an initial pass immediately, then continues on each tick.
// Errors from individual reconcilers are logged but do not stop the loop.
func (r *Runner) Run(ctx context.Context) {
	// Run an initial pass immediately
	r.runPass(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runPass(ctx)
		}
	}
}

// runPass executes all reconcilers once.
func (r *Runner) runPass(ctx context.Context) {
	for _, rec := range r.reconcilers {
		if err := rec.Reconcile(ctx); err != nil {
			r.logger.Error("reconciler error", "name", rec.Name(), "error", err)
		}
	}
}
