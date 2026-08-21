package forge

import (
	"testing"
)

func TestIsAgentAuthoredComment(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		// Positives
		{
			name: "haiku-worker",
			body: "haiku-worker: Some feedback",
			want: true,
		},
		{
			name: "gpt-5.5-reviewer",
			body: "gpt-5.5-reviewer: This looks good",
			want: true,
		},
		{
			name: "fable-merger",
			body: "fable-merger: Merging now",
			want: true,
		},
		{
			name: "odonian-reconciler",
			body: "odonian-reconciler: changes requested — bouncing back",
			want: true,
		},
		{
			name: "leading space",
			body: " haiku-worker: Comment",
			want: true,
		},
		{
			name: "leading tab",
			body: "\thaiku-worker: Comment",
			want: true,
		},
		{
			name: "leading newline",
			body: "\nhaiku-worker: Comment",
			want: true,
		},
		{
			name: "multiple leading whitespace",
			body: "  \n\t haiku-worker: Comment",
			want: true,
		},
		// Negatives
		{
			name: "plain human text",
			body: "This is a human comment",
			want: false,
		},
		{
			name: "marker mid-body",
			body: "Some text haiku-worker: More text",
			want: false,
		},
		{
			name: "uppercase in token",
			body: "Haiku-worker: Comment",
			want: false,
		},
		{
			name: "missing colon",
			body: "haiku-worker Comment",
			want: false,
		},
		{
			name: "wrong role",
			body: "haiku-maintainer: Comment",
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
		{
			name: "only whitespace",
			body: "   \n\t  ",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAgentAuthoredComment(tt.body)
			if got != tt.want {
				t.Errorf("IsAgentAuthoredComment(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestMarkerPrefix(t *testing.T) {
	tests := []struct {
		name  string
		model string
		role  string
		want  string
	}{
		{
			name:  "haiku worker",
			model: "haiku",
			role:  "worker",
			want:  "haiku-worker: ",
		},
		{
			name:  "gpt-5.5 reviewer",
			model: "gpt-5.5",
			role:  "reviewer",
			want:  "gpt-5.5-reviewer: ",
		},
		{
			name:  "fable merger",
			model: "fable",
			role:  "merger",
			want:  "fable-merger: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarkerPrefix(tt.model, tt.role)
			if got != tt.want {
				t.Errorf("MarkerPrefix(%q, %q) = %q, want %q", tt.model, tt.role, got, tt.want)
			}
		})
	}
}
