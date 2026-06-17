package notify

import (
	"fmt"

	"github.com/boldfield/agentask/internal/store"
)

func buildNotification(task store.Task, prLink string) (Notification, bool) {
	var event string
	var priority int
	var titlePrefix string
	var tags []string

	switch task.State {
	case "approved":
		event = "agentask-review"
		priority = 2
		titlePrefix = "Review & merge: "
		tags = []string{"eyes"}
	case "blocked":
		event = "agentask-blocked"
		priority = 2
		titlePrefix = "Blocked: "
		tags = []string{"no_entry"}
	case "failed":
		event = "agentask-failed"
		priority = 3
		titlePrefix = "Failed: "
		tags = []string{"x"}
	default:
		return Notification{}, false
	}

	link := prLink
	if prLink == "" {
		link = ""
	}

	return Notification{
		Event:    event,
		Title:    titlePrefix + task.Title,
		Body:     fmt.Sprintf("Project: %s\nTask: %s", task.ProjectID, task.ID),
		Link:     link,
		Priority: priority,
		Tags:     tags,
		DedupKey: event + ":" + task.ID,
	}, true
}
