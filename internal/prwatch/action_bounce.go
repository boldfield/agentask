package prwatch

import (
	"context"

	"github.com/boldfield/agentask/internal/forge"
	"github.com/boldfield/agentask/internal/store"
)

func applyBounce(ctx context.Context, tx taskTx, task store.Task, owner, repo string, prNumber int, token string) error {
	comment := "🔁 changes requested — reworking; address the review feedback on this PR"
	if err := forge.PostPRComment(ctx, owner, repo, prNumber, token, comment); err != nil {
		return err
	}

	note := "changes requested — bouncing back to ready for rework"
	_, err := tx.TransitionTask(ctx, task.ID, "ready", &note)
	return err
}
