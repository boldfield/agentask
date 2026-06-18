package prwatch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/forge"
	"github.com/boldfield/agentask/internal/store"
)

func TestApplyBouncePostsComment(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{}
	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		expectedPath := "/repos/testuser/testrepo/issues/42/comments"
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "changes requested") {
			t.Errorf("expected comment body to contain 'changes requested', got %s", string(body))
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1, "body": "🔁 changes requested — reworking; address the review feedback on this PR"}`))
	}))
	defer server.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = server.URL
	defer func() {
		forge.GitHubBaseURL = oldBaseURL
	}()

	err := applyBounce(ctx, tx, task, "testuser", "testrepo", 42, "test-token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !tx.transitionCalled {
		t.Fatal("TransitionTask was not called")
	}

	if tx.transitionState != "ready" {
		t.Errorf("expected state 'ready', got %q", tx.transitionState)
	}

	if tx.transitionNote == nil || *tx.transitionNote != "changes requested — bouncing back to ready for rework" {
		t.Errorf("expected note 'changes requested — bouncing back to ready for rework', got %v", tx.transitionNote)
	}
}

func TestApplyBounceTransitionError(t *testing.T) {
	ctx := context.Background()
	tx := &fakeTaskTx{transitionErr: errors.New("transition failed")}
	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 1}`))
	}))
	defer server.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = server.URL
	defer func() {
		forge.GitHubBaseURL = oldBaseURL
	}()

	err := applyBounce(ctx, tx, task, "testuser", "testrepo", 42, "test-token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "transition failed" {
		t.Errorf("expected error 'transition failed', got %v", err)
	}
}

func TestApplyBouncePostCommentError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	tx := &fakeTaskTx{}
	task := store.Task{
		ID:    "task-123",
		Title: "Test Task",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "Bad credentials"}`))
	}))
	defer server.Close()

	oldBaseURL := forge.GitHubBaseURL
	forge.GitHubBaseURL = server.URL
	defer func() {
		forge.GitHubBaseURL = oldBaseURL
	}()

	err := applyBounce(ctx, tx, task, "testuser", "testrepo", 42, "invalid-token")
	if err == nil {
		t.Fatal("expected error from post comment, got nil")
	}

	if !strings.Contains(err.Error(), "post comment failed") {
		t.Errorf("expected error containing 'post comment failed', got %v", err)
	}

	if tx.transitionCalled {
		t.Fatal("TransitionTask should not be called when PostPRComment fails")
	}
}
