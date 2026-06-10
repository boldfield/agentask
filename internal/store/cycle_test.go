package store

import (
	"testing"
)

func TestWouldCreateCycle(t *testing.T) {
	tests := []struct {
		name    string
		edges   map[string][]string
		taskID  string
		newDeps []string
		want    bool
	}{
		{
			name:    "linear chain A->B->C, no cycle",
			edges:   map[string][]string{"A": {"B"}, "B": {"C"}},
			taskID:  "A",
			newDeps: []string{"D"},
			want:    false,
		},
		{
			name:    "self-dependency",
			edges:   map[string][]string{},
			taskID:  "A",
			newDeps: []string{"A"},
			want:    true,
		},
		{
			name:    "would create cycle A->B, B->A",
			edges:   map[string][]string{"A": {"B"}},
			taskID:  "B",
			newDeps: []string{"A"},
			want:    true,
		},
		{
			name:    "diamond graph, no cycle",
			edges:   map[string][]string{"A": {"B", "C"}, "B": {"D"}, "C": {"D"}},
			taskID:  "D",
			newDeps: []string{"E"},
			want:    false,
		},
		{
			name:    "empty edges, no cycle",
			edges:   map[string][]string{},
			taskID:  "A",
			newDeps: []string{"B"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wouldCreateCycle(tt.edges, tt.taskID, tt.newDeps)
			if got != tt.want {
				t.Errorf("wouldCreateCycle() = %v, want %v", got, tt.want)
			}
		})
	}
}
