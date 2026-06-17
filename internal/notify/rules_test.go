package notify

import (
	"testing"

	"github.com/boldfield/agentask/internal/store"
)

func TestBuildNotification(t *testing.T) {
	tests := []struct {
		name              string
		state             string
		prLink            string
		expectedOk        bool
		expectedEvent     string
		expectedPriority  int
		expectedDedupKey  string
		expectedTitleHas  string
		expectedLinkEmpty bool
	}{
		{
			name:             "approved state",
			state:            "approved",
			prLink:           "https://github.com/example/pr/123",
			expectedOk:       true,
			expectedEvent:    "agentask-review",
			expectedPriority: 2,
			expectedTitleHas: "Review & merge: ",
			expectedDedupKey: "agentask-review:task-1",
		},
		{
			name:             "blocked state",
			state:            "blocked",
			prLink:           "https://github.com/example/pr/456",
			expectedOk:       true,
			expectedEvent:    "agentask-blocked",
			expectedPriority: 2,
			expectedTitleHas: "Blocked: ",
			expectedDedupKey: "agentask-blocked:task-1",
		},
		{
			name:             "failed state",
			state:            "failed",
			prLink:           "https://github.com/example/pr/789",
			expectedOk:       true,
			expectedEvent:    "agentask-failed",
			expectedPriority: 3,
			expectedTitleHas: "Failed: ",
			expectedDedupKey: "agentask-failed:task-1",
		},
		{
			name:       "non-notify state (ready)",
			state:      "ready",
			prLink:     "https://github.com/example/pr/999",
			expectedOk: false,
		},
		{
			name:       "non-notify state (backlog)",
			state:      "backlog",
			prLink:     "https://github.com/example/pr/999",
			expectedOk: false,
		},
		{
			name:              "approved with empty prLink",
			state:             "approved",
			prLink:            "",
			expectedOk:        true,
			expectedEvent:     "agentask-review",
			expectedPriority:  2,
			expectedLinkEmpty: true,
			expectedDedupKey:  "agentask-review:task-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := store.Task{
				ID:        "task-1",
				ProjectID: "proj-1",
				Title:     "Sample Task",
				State:     tt.state,
			}

			notif, ok := buildNotification(task, tt.prLink)

			if ok != tt.expectedOk {
				t.Errorf("got ok=%v, want %v", ok, tt.expectedOk)
			}

			if !tt.expectedOk {
				return
			}

			if notif.Event != tt.expectedEvent {
				t.Errorf("got event=%q, want %q", notif.Event, tt.expectedEvent)
			}

			if notif.Priority != tt.expectedPriority {
				t.Errorf("got priority=%d, want %d", notif.Priority, tt.expectedPriority)
			}

			if notif.DedupKey != tt.expectedDedupKey {
				t.Errorf("got dedup_key=%q, want %q", notif.DedupKey, tt.expectedDedupKey)
			}

			if !contains(notif.Title, tt.expectedTitleHas) {
				t.Errorf("got title=%q, want it to contain %q", notif.Title, tt.expectedTitleHas)
			}

			if tt.expectedLinkEmpty && notif.Link != "" {
				t.Errorf("got link=%q, want empty", notif.Link)
			}

			if !tt.expectedLinkEmpty && notif.Link != tt.prLink {
				t.Errorf("got link=%q, want %q", notif.Link, tt.prLink)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
