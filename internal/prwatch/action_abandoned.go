package prwatch

import (
	"context"

	"github.com/boldfield/agentask/internal/store"
)

func applyAbandoned(ctx context.Context, tx taskTx, task store.Task, reason string) error {
	_, err := tx.TransitionTask(ctx, task.ID, "abandoned", &reason)
	return err
}
