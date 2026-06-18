package forge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetReviewDecision_Approved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/graphql" {
			t.Errorf("expected path /graphql, got %s", r.URL.Path)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type: application/json, got %s", ct)
		}

		expectedAuth := "Bearer test-token"
		if auth := r.Header.Get("Authorization"); auth != expectedAuth {
			t.Errorf("expected Authorization: %s, got %s", expectedAuth, auth)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "testowner") {
			t.Errorf("expected request body to contain 'testowner', got %s", string(body))
		}

		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "APPROVED",
        "reviews": {
          "nodes": [
            {
              "submittedAt": "2024-06-18T12:30:00Z"
            }
          ]
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, latestReviewAt, err := GetReviewDecision(ctx, "testowner", "testrepo", 42, "test-token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "approved" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "approved")
	}

	expectedTime := time.Date(2024, 6, 18, 12, 30, 0, 0, time.UTC)
	if !latestReviewAt.Equal(expectedTime) {
		t.Errorf("GetReviewDecision() latestReviewAt = %v, want %v", latestReviewAt, expectedTime)
	}
}

func TestGetReviewDecision_ChangesRequested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "CHANGES_REQUESTED",
        "reviews": {
          "nodes": [
            {
              "submittedAt": "2024-06-17T10:15:00Z"
            }
          ]
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, latestReviewAt, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "changes_requested" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "changes_requested")
	}

	expectedTime := time.Date(2024, 6, 17, 10, 15, 0, 0, time.UTC)
	if !latestReviewAt.Equal(expectedTime) {
		t.Errorf("GetReviewDecision() latestReviewAt = %v, want %v", latestReviewAt, expectedTime)
	}
}

func TestGetReviewDecision_ReviewRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "REVIEW_REQUIRED",
        "reviews": {
          "nodes": []
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, latestReviewAt, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "pending" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "pending")
	}

	if !latestReviewAt.IsZero() {
		t.Errorf("GetReviewDecision() latestReviewAt = %v, want zero time", latestReviewAt)
	}
}

func TestGetReviewDecision_Null(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": null,
        "reviews": {
          "nodes": []
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, latestReviewAt, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "pending" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "pending")
	}

	if !latestReviewAt.IsZero() {
		t.Errorf("GetReviewDecision() latestReviewAt = %v, want zero time", latestReviewAt)
	}
}

func TestGetReviewDecision_NoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %s", auth)
		}

		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "APPROVED",
        "reviews": {
          "nodes": []
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, _, err := GetReviewDecision(ctx, "owner", "repo", 1, "")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "approved" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "approved")
	}
}

func TestGetReviewDecision_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "Bad credentials"}`))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := GetReviewDecision(ctx, "owner", "repo", 1, "invalid-token")

	if err == nil {
		t.Error("GetReviewDecision() error = nil, want error")
	}
}

func TestGetReviewDecision_JSONParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err == nil {
		t.Error("GetReviewDecision() error = nil, want error")
	}
}

func TestGetReviewDecision_MultipleReviews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "APPROVED",
        "reviews": {
          "nodes": [
            {
              "submittedAt": "2024-06-18T14:00:00Z"
            },
            {
              "submittedAt": "2024-06-18T13:00:00Z"
            }
          ]
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, latestReviewAt, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "approved" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "approved")
	}

	expectedTime := time.Date(2024, 6, 18, 14, 0, 0, 0, time.UTC)
	if !latestReviewAt.Equal(expectedTime) {
		t.Errorf("GetReviewDecision() latestReviewAt = %v, want %v", latestReviewAt, expectedTime)
	}
}

func TestGetReviewDecision_UnknownDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphqlResp := `{
  "data": {
    "repository": {
      "pullRequest": {
        "reviewDecision": "UNKNOWN_VALUE",
        "reviews": {
          "nodes": []
        }
      }
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(graphqlResp))
	}))
	defer server.Close()

	oldBaseURL := GitHubBaseURL
	GitHubBaseURL = server.URL
	defer func() {
		GitHubBaseURL = oldBaseURL
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	decision, _, err := GetReviewDecision(ctx, "owner", "repo", 1, "token")

	if err != nil {
		t.Errorf("GetReviewDecision() error = %v, want nil", err)
	}

	if decision != "pending" {
		t.Errorf("GetReviewDecision() decision = %q, want %q", decision, "pending")
	}
}
