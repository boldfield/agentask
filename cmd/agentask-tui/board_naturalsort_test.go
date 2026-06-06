package main

import (
	"testing"

	"github.com/boldfield/agentask/internal/tuiclient"
)

func TestNaturalLess(t *testing.T) {
	// Each element must sort strictly before the next.
	ordered := []string{"MR-1a", "MR-1b", "MR-2", "MR-3", "MR-9", "MR-10", "MR-11", "MR-100"}
	for i := 0; i+1 < len(ordered); i++ {
		if !naturalLess(ordered[i], ordered[i+1]) {
			t.Errorf("expected %q < %q", ordered[i], ordered[i+1])
		}
		if naturalLess(ordered[i+1], ordered[i]) {
			t.Errorf("expected NOT %q < %q", ordered[i+1], ordered[i])
		}
	}
	// Leading zeros compare by numeric value, not string length.
	if naturalLess("MR-10", "MR-009") {
		t.Errorf("expected MR-009 (9) < MR-10")
	}
}

func TestSortTasksNatural(t *testing.T) {
	tasks := []tuiclient.Task{
		{ID: "a", Title: "MR-10 — approved lane"},
		{ID: "b", Title: "MR-2 — columns"},
		{ID: "c", Title: "MR-1b — rebuild"},
		{ID: "d", Title: "MR-1a — runner"},
	}
	sortTasksNatural(tasks)
	want := []string{
		"MR-1a — runner",
		"MR-1b — rebuild",
		"MR-2 — columns",
		"MR-10 — approved lane",
	}
	for i := range want {
		if tasks[i].Title != want[i] {
			t.Errorf("position %d: got %q want %q", i, tasks[i].Title, want[i])
		}
	}
}

func TestSortTasksNaturalStableByID(t *testing.T) {
	// Identical titles fall back to ID order for stable rendering across refreshes.
	tasks := []tuiclient.Task{
		{ID: "z", Title: "same"},
		{ID: "a", Title: "same"},
	}
	sortTasksNatural(tasks)
	if tasks[0].ID != "a" || tasks[1].ID != "z" {
		t.Errorf("expected ID-stable order [a z], got [%s %s]", tasks[0].ID, tasks[1].ID)
	}
}
