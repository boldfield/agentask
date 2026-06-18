package prwatch

import (
	"testing"
	"time"
)

func TestDecideAction(t *testing.T) {
	now := time.Now()
	before := now.Add(-1 * time.Hour)
	after := now.Add(1 * time.Hour)

	tests := []struct {
		name           string
		prState        string
		reviewDecision string
		latestReviewAt time.Time
		taskApprovedAt time.Time
		expected       Action
	}{
		{
			name:           "merged returns done",
			prState:        "merged",
			reviewDecision: "approved",
			latestReviewAt: now,
			taskApprovedAt: now,
			expected:       Done,
		},
		{
			name:           "closed returns abandon",
			prState:        "closed",
			reviewDecision: "approved",
			latestReviewAt: now,
			taskApprovedAt: now,
			expected:       Abandon,
		},
		{
			name:           "open with changes_requested newer than approval returns bounce",
			prState:        "open",
			reviewDecision: "changes_requested",
			latestReviewAt: after,
			taskApprovedAt: now,
			expected:       Bounce,
		},
		{
			name:           "open with stale changes_requested returns noop",
			prState:        "open",
			reviewDecision: "changes_requested",
			latestReviewAt: before,
			taskApprovedAt: now,
			expected:       Noop,
		},
		{
			name:           "open with approved decision returns noop",
			prState:        "open",
			reviewDecision: "approved",
			latestReviewAt: now,
			taskApprovedAt: now,
			expected:       Noop,
		},
		{
			name:           "open with no review decision returns noop",
			prState:        "open",
			reviewDecision: "",
			latestReviewAt: now,
			taskApprovedAt: now,
			expected:       Noop,
		},
		{
			name:           "unknown prState returns noop",
			prState:        "unknown",
			reviewDecision: "changes_requested",
			latestReviewAt: after,
			taskApprovedAt: now,
			expected:       Noop,
		},
		{
			name:           "open with changes_requested at exact same time as approval returns noop",
			prState:        "open",
			reviewDecision: "changes_requested",
			latestReviewAt: now,
			taskApprovedAt: now,
			expected:       Noop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decideAction(tt.prState, tt.reviewDecision, tt.latestReviewAt, tt.taskApprovedAt)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}
