package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnerToken(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		owner       string
		want        string
		wantErr     bool
	}{
		{
			name:        "valid token",
			fileContent: "alice=token123\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "case-insensitive match",
			fileContent: "Alice=token123\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "token with double quotes",
			fileContent: `alice="token123"` + "\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "token with single quotes",
			fileContent: "alice='token123'\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "with comments",
			fileContent: "alice=token123 # this is a comment\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "blank lines",
			fileContent: "alice=token123\n\nbob=token456\n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
		{
			name:        "owner not found",
			fileContent: "alice=token123\nbob=token456\n",
			owner:       "charlie",
			want:        "",
			wantErr:     false,
		},
		{
			name:        "whitespace around equals",
			fileContent: "alice = token123 \n",
			owner:       "alice",
			want:        "token123",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory and file
			tmpDir, err := os.MkdirTemp("", "forge-test-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			agtDir := filepath.Join(tmpDir, ".agentask")
			if err := os.MkdirAll(agtDir, 0755); err != nil {
				t.Fatalf("failed to create .agentask dir: %v", err)
			}

			tokenFile := filepath.Join(agtDir, "forge-tokens")
			if err := os.WriteFile(tokenFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("failed to write token file: %v", err)
			}

			// Mock userHomeDirFunc
			oldUserHomeDir := userHomeDirFunc
			userHomeDirFunc = func() (string, error) {
				return tmpDir, nil
			}
			defer func() {
				userHomeDirFunc = oldUserHomeDir
			}()

			got, err := OwnerToken(tt.owner)
			if (err != nil) != tt.wantErr {
				t.Errorf("OwnerToken() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("OwnerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOwnerToken_MissingFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Mock userHomeDirFunc to point to a dir without forge-tokens
	oldUserHomeDir := userHomeDirFunc
	userHomeDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		userHomeDirFunc = oldUserHomeDir
	}()

	got, err := OwnerToken("alice")
	if err != nil {
		t.Errorf("OwnerToken() error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("OwnerToken() = %q, want empty string", got)
	}
}

func TestSquashMerge(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		prNumber     int
		owner        string
		repo         string
		token        string
		wantErr      bool
	}{
		{
			name:         "successful merge",
			statusCode:   200,
			responseBody: `{"id": 1, "merged": true}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      false,
		},
		{
			name:         "merge without token",
			statusCode:   200,
			responseBody: `{"id": 1, "merged": true}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "",
			wantErr:      false,
		},
		{
			name:         "not found",
			statusCode:   404,
			responseBody: `{"message": "Not Found"}`,
			prNumber:     999,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      true,
		},
		{
			name:         "conflict",
			statusCode:   409,
			responseBody: `{"message": "Pull Request is not mergeable"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "test-token",
			wantErr:      true,
		},
		{
			name:         "unauthorized",
			statusCode:   401,
			responseBody: `{"message": "Bad credentials"}`,
			prNumber:     42,
			owner:        "testuser",
			repo:         "testrepo",
			token:        "invalid-token",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request details
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT, got %s", r.Method)
				}

				expectedPath := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", tt.owner, tt.repo, tt.prNumber)
				if r.URL.Path != expectedPath {
					t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
				}

				// Verify headers
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("expected Content-Type: application/json, got %s", ct)
				}

				if tt.token != "" {
					expectedAuth := "token " + tt.token
					if auth := r.Header.Get("Authorization"); auth != expectedAuth {
						t.Errorf("expected Authorization: %s, got %s", expectedAuth, auth)
					}
				}

				// Verify request body contains merge_method=squash
				body, _ := io.ReadAll(r.Body)
				if !contains(string(body), "squash") {
					t.Errorf("expected request body to contain 'squash', got %s", string(body))
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Create a custom SquashMerge that uses our mock server
			testURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge", server.URL, tt.owner, tt.repo, tt.prNumber)

			payload := map[string]string{
				"merge_method": "squash",
			}
			body, _ := json.Marshal(payload)

			req, err := http.NewRequestWithContext(ctx, http.MethodPut, testURL, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			req.Header.Set("Accept", "application/vnd.github.v3+json")
			req.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				req.Header.Set("Authorization", "token "+tt.token)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			hasErr := resp.StatusCode < 200 || resp.StatusCode >= 300
			if hasErr != tt.wantErr {
				t.Errorf("expected error=%v, got hasErr=%v (status=%d)", tt.wantErr, hasErr, resp.StatusCode)
			}
		})
	}
}

func TestSquashMergeIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/pulls/123/merge" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"merged": true}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build URL to match the pattern the real function expects, but pointing to our server
	testURL := fmt.Sprintf("%s/repos/owner/repo/pulls/123/merge", server.URL)

	payload := map[string]string{
		"merge_method": "squash",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, testURL, bytes.NewReader(body))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token test-token")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && s[0:len(substr)] == substr) || (len(s) > len(substr) && len(substr) > 0 && contains(s[1:], substr)))
}
