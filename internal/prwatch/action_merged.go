package prwatch

import (
	"context"
	"log/slog"

	"github.com/boldfield/agentask/internal/notify"
	"github.com/boldfield/agentask/internal/store"
)

type taskTx interface {
	TransitionTask(ctx context.Context, id, toState string, note *string) error
}

func applyMerged(ctx context.Context, tx taskTx, n notify.Notifier, task store.Task) error {
	err := tx.TransitionTask(ctx, task.ID, "done", nil)
	if err != nil {
		return err
	}

	notification := notify.Notification{
		Event:    "agentask-merged",
		Title:    "Merged: " + task.Title,
		Priority: 4,
		DedupKey: "agentask-merged:" + task.ID,
	}

	if err := n.Publish(ctx, notification); err != nil {
		slog.Error("failed to publish agentask-merged notification", "task_id", task.ID, "error", err)
	}

	return nil
}
