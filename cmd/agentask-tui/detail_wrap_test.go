package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestWrapText(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog"
	out := wrapText(in, 20)
	for _, line := range strings.Split(out, "\n") {
		if utf8.RuneCountInString(line) > 20 {
			t.Errorf("line %q exceeds width 20", line)
		}
	}
	if strings.Join(strings.Fields(out), " ") != in {
		t.Errorf("words not preserved across wrap: %q", out)
	}
	if got := wrapText("a\nb", 10); got != "a\nb" {
		t.Errorf("existing newlines must be preserved, got %q", got)
	}
	if got := wrapText("x y", 0); got != "x y" {
		t.Errorf("width<=0 should pass through, got %q", got)
	}
}

func TestBuildDetailContent(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)
	title := "Test Task"
	state := "in_progress"
	spec := "This is the spec content"

	task := tuiclient.TaskDetail{
		ID:        "task-123",
		Title:     title,
		State:     state,
		Spec:      spec,
		CreatedAt: now,
		UpdatedAt: now,
	}

	model := &BoardModel{
		width: 80,
	}
	model.detailEvents = nil

	content := model.buildDetailContent(task)

	// Verify header text is part of the scrollable content
	if !strings.Contains(content, title) {
		t.Errorf("header title not in scrollable content")
	}
	if !strings.Contains(content, state) {
		t.Errorf("header state not in scrollable content")
	}
	if !strings.Contains(content, spec) {
		t.Errorf("spec not in scrollable content")
	}

	// Verify content is in the expected order: header, then metadata, then separator, then spec
	titleIdx := strings.Index(content, title)
	specIdx := strings.Index(content, spec)
	if titleIdx >= specIdx {
		t.Errorf("title should appear before spec in scrollable content")
	}
}

func TestBuildDetailContentWrapping(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)
	// Create a title that's longer than the viewport width
	longTitle := "This is a very long task title that should be wrapped when displayed in the detail view because it exceeds the width"
	longSpec := "This is a very long specification that contains multiple sentences and should wrap correctly when rendered in the detail view without causing horizontal overflow or truncation of important content"

	tests := []struct {
		name  string
		width int
	}{
		{"narrow terminal", 40},
		{"standard terminal", 80},
		{"wide terminal", 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tuiclient.TaskDetail{
				ID:        "task-123",
				Title:     longTitle,
				State:     "in_progress",
				Spec:      longSpec,
				CreatedAt: now,
				UpdatedAt: now,
			}

			model := &BoardModel{
				width: tt.width,
			}
			model.detailEvents = nil

			content := model.buildDetailContent(task)

			// Verify that no line exceeds the viewport width
			for i, line := range strings.Split(content, "\n") {
				lineWidth := utf8.RuneCountInString(line)
				if lineWidth > tt.width {
					t.Errorf("line %d exceeds width %d (got %d): %q", i, tt.width, lineWidth, line)
				}
			}
		})
	}
}

func TestBuildDetailContentWithLongURL(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)

	// Use a very long URL to test hard-breaking
	longURL := "https://github.com/boldfield/agentask/pull/144/review/new/very/long/path/that/exceeds/viewport"

	task := tuiclient.TaskDetail{
		ID:        "task-123",
		Title:     "Test Task",
		State:     "in_progress",
		Spec:      "Test spec",
		CreatedAt: now,
		UpdatedAt: now,
		Links: []tuiclient.TaskLink{
			{Kind: "pr", Value: longURL},
		},
	}

	width := 40
	model := &BoardModel{
		width: width,
	}
	model.detailEvents = nil

	content := model.buildDetailContent(task)

	// Verify that no line exceeds the viewport width, including the PR URL line
	for i, line := range strings.Split(content, "\n") {
		lineWidth := utf8.RuneCountInString(line)
		if lineWidth > width {
			t.Errorf("line %d exceeds width %d (got %d): %q", i, width, lineWidth, line)
		}
	}
}

func TestBuildDetailContentWithLongDependency(t *testing.T) {
	now := time.Now().Format(time.RFC3339Nano)
	longDepID := "dep-with-long-title-id"
	longDepTitle := "This is a very long dependency title that exceeds viewport width"

	depTask := tuiclient.Task{
		ID:    longDepID,
		Title: longDepTitle,
		State: "in_progress",
	}

	task := tuiclient.TaskDetail{
		ID:        "task-123",
		Title:     "Test Task",
		State:     "in_progress",
		Spec:      "Test spec",
		CreatedAt: now,
		UpdatedAt: now,
		DependsOn: []string{longDepID},
	}

	width := 40
	model := &BoardModel{
		width: width,
		tasks: map[string][]tuiclient.Task{
			"ready": {depTask},
		},
	}
	model.detailEvents = nil

	content := model.buildDetailContent(task)

	// Verify that no line exceeds the viewport width, including dependency lines
	for i, line := range strings.Split(content, "\n") {
		lineWidth := utf8.RuneCountInString(line)
		if lineWidth > width {
			t.Errorf("line %d exceeds width %d (got %d): %q", i, width, lineWidth, line)
		}
	}
}
