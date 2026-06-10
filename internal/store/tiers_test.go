package store

import "testing"

func TestNextTier(t *testing.T) {
	store := &sqliteStore{
		allowedModels: []string{"haiku", "sonnet", "opus"},
	}

	tests := []struct {
		model    string
		wantTier string
		wantOk   bool
	}{
		{"haiku", "sonnet", true},
		{"sonnet", "opus", true},
		{"opus", "", false},
		{"unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			gotTier, gotOk := store.nextTier(tt.model)
			if gotTier != tt.wantTier || gotOk != tt.wantOk {
				t.Errorf("nextTier(%q) = (%q, %v), want (%q, %v)", tt.model, gotTier, gotOk, tt.wantTier, tt.wantOk)
			}
		})
	}
}

func TestIsTopTier(t *testing.T) {
	store := &sqliteStore{
		allowedModels: []string{"haiku", "sonnet", "opus"},
	}

	tests := []struct {
		model string
		want  bool
	}{
		{"haiku", false},
		{"sonnet", false},
		{"opus", true},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := store.isTopTier(tt.model)
			if got != tt.want {
				t.Errorf("isTopTier(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
