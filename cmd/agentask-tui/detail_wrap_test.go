package main

import (
	"strings"
	"testing"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestWrapText(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog"
	out := wrapText(in, 20)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 20 {
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
