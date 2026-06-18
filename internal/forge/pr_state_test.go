package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetPRState(t *testing.T) {
	tests := []struct {
		name        string
		mergedAt    *string
		state       string
		expectedRes string
	}{
		{
			name:        "merged PR",
			mergedAt:    strPtr("2024-01-15T10:30:00Z"),
			state:       "closed",
			expectedRes: "merged",
		},
		{
			name:        "closed but not merged PR",
			mergedAt:    nil,
			state:       "closed",
			expectedRes: "closed",
		},
		{
			name:        "open PR",
			mergedAt:    nil,
			state:       "open",
			expectedRes: "open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}

				if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				prData := map[string]interface{}{
					"merged_at": tt.mergedAt,
					"state":     tt.state,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(prData)
			}))
			defer server.Close()

			// Override GitHubBaseURL for this test
			oldURL := GitHubBaseURL
			GitHubBaseURL = server.URL
			defer func() { GitHubBaseURL = oldURL }()

			res, err := GetPRState(context.Background(), "owner", "repo", 42, "token123")
			if err != nil {
				t.Fatalf("GetPRState failed: %v", err)
			}
			if res != tt.expectedRes {
				t.Errorf("expected %q, got %q", tt.expectedRes, res)
			}
		})
	}
}

func TestGetPRStateNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message": "Not Found"}`)
	}))
	defer server.Close()

	oldURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() { GitHubBaseURL = oldURL }()

	_, err := GetPRState(context.Background(), "owner", "repo", 999, "token123")
	if err == nil {
		t.Fatal("expected error on non-2xx status code")
	}
}

func strPtr(s string) *string {
	return &s
}
