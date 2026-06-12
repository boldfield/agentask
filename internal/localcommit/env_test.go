package localcommit

import (
	"testing"
)

func TestDeliveryMode(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected string
	}{
		{
			name:     "unset defaults to pull_request",
			env:      "",
			expected: "pull_request",
		},
		{
			name:     "local_commit",
			env:      "local_commit",
			expected: "local_commit",
		},
		{
			name:     "mixed case normalized to lowercase",
			env:      "LOCAL_COMMIT",
			expected: "local_commit",
		},
		{
			name:     "surrounding whitespace trimmed",
			env:      "  local_commit  ",
			expected: "local_commit",
		},
		{
			name:     "unknown value treated as pull_request",
			env:      "unknown",
			expected: "pull_request",
		},
		{
			name:     "pull_request explicit",
			env:      "pull_request",
			expected: "pull_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				t.Setenv("AGENTASK_DELIVERY_MODE", "")
			} else {
				t.Setenv("AGENTASK_DELIVERY_MODE", tt.env)
			}
			got := DeliveryMode()
			if got != tt.expected {
				t.Errorf("DeliveryMode() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsLocalCommit(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{
			name:     "unset returns false",
			env:      "",
			expected: false,
		},
		{
			name:     "local_commit returns true",
			env:      "local_commit",
			expected: true,
		},
		{
			name:     "mixed case local_commit returns true",
			env:      "LOCAL_COMMIT",
			expected: true,
		},
		{
			name:     "whitespace-padded local_commit returns true",
			env:      "  local_commit  ",
			expected: true,
		},
		{
			name:     "unknown value returns false",
			env:      "unknown",
			expected: false,
		},
		{
			name:     "pull_request returns false",
			env:      "pull_request",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				t.Setenv("AGENTASK_DELIVERY_MODE", "")
			} else {
				t.Setenv("AGENTASK_DELIVERY_MODE", tt.env)
			}
			got := IsLocalCommit()
			if got != tt.expected {
				t.Errorf("IsLocalCommit() = %v, want %v", got, tt.expected)
			}
		})
	}
}
