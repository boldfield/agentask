package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestProjectPickerModel(t *testing.T) {
	projects := []tuiclient.Project{
		{ID: "proj1", Name: "Project 1", Repo: "repo1", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: "proj2", Name: "Project 2", Repo: "repo2", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: "proj3", Name: "Project 3", Repo: "repo3", CreatedAt: "2024-01-01T00:00:00Z"},
	}

	m := NewProjectPickerModel(projects)

	// Test initial state
	if m.Selected != 0 {
		t.Errorf("expected Selected=0, got %d", m.Selected)
	}

	if m.Quit {
		t.Error("expected Quit=false")
	}

	// Test navigation down
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = model.(ProjectPickerModel)
	if m.Selected != 1 {
		t.Errorf("expected Selected=1 after j, got %d", m.Selected)
	}

	// Test navigation up
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = model.(ProjectPickerModel)
	if m.Selected != 0 {
		t.Errorf("expected Selected=0 after k, got %d", m.Selected)
	}

	// Test boundary: can't go above 0
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = model.(ProjectPickerModel)
	if m.Selected != 0 {
		t.Errorf("expected Selected=0 at boundary, got %d", m.Selected)
	}

	// Test down key
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("down")})
	m = model.(ProjectPickerModel)
	if m.Selected != 1 {
		t.Errorf("expected Selected=1 after down, got %d", m.Selected)
	}

	// Test boundary: can't go beyond last
	m.Selected = 2
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("down")})
	m = model.(ProjectPickerModel)
	if m.Selected != 2 {
		t.Errorf("expected Selected=2 at boundary, got %d", m.Selected)
	}

	// Test quit
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = model.(ProjectPickerModel)
	if !m.Quit {
		t.Error("expected Quit=true after q")
	}
	if cmd == nil {
		t.Error("expected cmd to be tea.Quit")
	}

	// Test enter
	m = NewProjectPickerModel(projects)
	model, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("expected cmd to be tea.Quit after enter")
	}
}

func TestProjectPickerView(t *testing.T) {
	projects := []tuiclient.Project{
		{ID: "proj1", Name: "Project 1", Repo: "repo1", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: "proj2", Name: "Project 2", Repo: "repo2", CreatedAt: "2024-01-01T00:00:00Z"},
	}

	m := NewProjectPickerModel(projects)
	view := m.View()

	// Check that both projects are in the view
	if !contains(view, "Project 1") {
		t.Error("expected 'Project 1' in view")
	}
	if !contains(view, "Project 2") {
		t.Error("expected 'Project 2' in view")
	}

	// Check that cursor is on first project
	if !contains(view, "> Project 1") {
		t.Error("expected '> Project 1' in view")
	}
}

func contains(s, substring string) bool {
	for i := 0; i <= len(s)-len(substring); i++ {
		if s[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
