package main

import (
	"testing"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{
			name:      "valid URL",
			url:       "https://github.com/boldfield/agentask/pull/42",
			wantOwner: "boldfield",
			wantRepo:  "agentask",
			wantNum:   42,
			wantErr:   false,
		},
		{
			name:      "trailing slash",
			url:       "https://github.com/boldfield/agentask/pull/42/",
			wantOwner: "boldfield",
			wantRepo:  "agentask",
			wantNum:   42,
			wantErr:   false,
		},
		{
			name:    "non-PR URL",
			url:     "https://github.com/boldfield/agentask/issues/42",
			wantErr: true,
		},
		{
			name:    "garbage",
			url:     "not a url",
			wantErr: true,
		},
		{
			name:    "malformed PR number",
			url:     "https://github.com/boldfield/agentask/pull/abc",
			wantErr: true,
		},
		{
			name:    "missing number",
			url:     "https://github.com/boldfield/agentask/pull",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, num, err := parsePRURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePRURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
				if num != tt.wantNum {
					t.Errorf("number = %d, want %d", num, tt.wantNum)
				}
			}
		})
	}
}
