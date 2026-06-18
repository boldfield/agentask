package prwatch

import "time"

type Action string

const (
	Done    Action = "done"
	Abandon Action = "abandon"
	Bounce  Action = "bounce"
	Noop    Action = "noop"
)

func decideAction(prState, reviewDecision string, latestReviewAt, taskApprovedAt time.Time) Action {
	switch prState {
	case "merged":
		return Done
	case "closed":
		return Abandon
	case "open":
		if reviewDecision == "changes_requested" && latestReviewAt.After(taskApprovedAt) {
			return Bounce
		}
	}
	return Noop
}
