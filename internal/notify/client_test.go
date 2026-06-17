package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublishSuccess(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	var capturedBody Notification

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")

		var n Notification
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		capturedBody = n

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", nil, slog.Default())
	notification := Notification{
		Event:    "test",
		Title:    "Test Title",
		Body:     "Test Body",
		Link:     "http://example.com",
		Priority: 1,
		Tags:     []string{"tag1"},
		DedupKey: "key1",
	}

	err := client.Publish(context.Background(), notification)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if capturedPath != "/notify" {
		t.Errorf("expected path /notify, got %s", capturedPath)
	}

	if capturedAuth != "Bearer test-token" {
		t.Errorf("expected Bearer test-token, got %s", capturedAuth)
	}

	if capturedBody.Event != "test" {
		t.Errorf("expected event test, got %s", capturedBody.Event)
	}
}

func TestPublishNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", nil, slog.Default())
	notification := Notification{
		Event: "test",
	}

	err := client.Publish(context.Background(), notification)
	if err == nil {
		t.Fatal("expected error for non-2xx status, got nil")
	}

	if err.Error() != "publish failed with status 500: Internal Server Error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPublishBearerHeader(t *testing.T) {
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "my-secret-token", nil, slog.Default())
	notification := Notification{
		Event: "test",
	}

	err := client.Publish(context.Background(), notification)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if capturedAuth != "Bearer my-secret-token" {
		t.Errorf("expected Bearer my-secret-token, got %s", capturedAuth)
	}
}

func TestPublishJSONBody(t *testing.T) {
	var capturedBody Notification

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", contentType)
		}

		var n Notification
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		capturedBody = n

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", nil, slog.Default())
	notification := Notification{
		Event:    "test-event",
		Title:    "Test",
		Body:     "Body",
		Link:     "http://test.com",
		Priority: 5,
		Tags:     []string{"a", "b"},
		DedupKey: "dedup-123",
	}

	err := client.Publish(context.Background(), notification)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if capturedBody.Event != "test-event" ||
		capturedBody.Title != "Test" ||
		capturedBody.Body != "Body" ||
		capturedBody.Link != "http://test.com" ||
		capturedBody.Priority != 5 ||
		len(capturedBody.Tags) != 2 ||
		capturedBody.DedupKey != "dedup-123" {
		t.Errorf("notification body not correctly transmitted: %+v", capturedBody)
	}
}
