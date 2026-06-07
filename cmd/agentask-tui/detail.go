package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/boldfield/agentask/internal/tuiclient"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// detailFetchedMsg carries the result of a GetTask + ListDocuments call for the detail view.
type detailFetchedMsg struct {
	task      tuiclient.TaskDetail
	documents []tuiclient.Document
	err       error
}

// openerResultMsg carries a brief status message after an open-in-browser action.
// An empty message means success (no banner shown).
type openerResultMsg struct {
	message string
}

// fetchDetailCmd creates a command that fetches full task detail and the project's document list.
func (m *BoardModel) fetchDetailCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		task, err := m.client.GetTask(ctx, taskID)
		if err != nil {
			return detailFetchedMsg{err: fmt.Errorf("get task failed: %w", err)}
		}

		docs, err := m.client.ListDocuments(ctx, m.project.ID)
		if err != nil {
			// Non-fatal: we still show the task; opener actions will fail gracefully.
			docs = nil
		}

		return detailFetchedMsg{task: task, documents: docs}
	}
}

// resolveDocURL builds a VCS browse URL from a Document ref using the project's repo.
// If ref is already a URL (has a scheme), it is returned directly.
// Otherwise: <repo>/blob/<commit-or-main>/<ref>.
// Returns ("", errMsg) when the URL cannot be built.
//
// Note: only https-form repos (e.g. "https://github.com/owner/repo") are supported.
// SSH-form repos (e.g. "git@github.com:owner/repo") are out of scope.
func resolveDocURL(ref string, doc tuiclient.Document, projectRepo string) (string, string) {
	if ref == "" {
		return "", "document has no ref"
	}

	// If ref already looks like a URL, open it directly.
	if parsed, err := url.Parse(ref); err == nil && parsed.Scheme != "" {
		return ref, ""
	}

	// Need a repo to build the URL.
	if projectRepo == "" {
		return "", "project has no repo configured"
	}

	// Normalize the repo: strip trailing slash then trailing ".git" to avoid
	// producing a path like "…/repo.git/blob/…" which results in a 404 on GitHub.
	normalizedRepo := strings.TrimRight(projectRepo, "/")
	normalizedRepo = strings.TrimSuffix(normalizedRepo, ".git")

	branch := "main"
	if doc.Commit != nil && *doc.Commit != "" {
		branch = *doc.Commit
	}

	builtURL := normalizedRepo + "/blob/" + branch + "/" + strings.TrimLeft(ref, "/")
	return builtURL, ""
}

// openPRCmd creates a command that opens the task's PR link in the browser.
func (m *BoardModel) openPRCmd(task tuiclient.TaskDetail) tea.Cmd {
	return func() tea.Msg {
		var prURL string
		for _, link := range task.Links {
			if link.Kind == "pr" {
				prURL = link.Value
				break
			}
		}

		if prURL == "" {
			return openerResultMsg{message: "no PR link on this task"}
		}

		if err := m.urlOpener(prURL); err != nil {
			return openerResultMsg{message: fmt.Sprintf("open failed: %v", err)}
		}
		return openerResultMsg{}
	}
}

// openSourceDocCmd creates a command that opens the task's source document in the browser.
func (m *BoardModel) openSourceDocCmd(task tuiclient.TaskDetail, documents []tuiclient.Document) tea.Cmd {
	return func() tea.Msg {
		if task.DocumentID == "" {
			return openerResultMsg{message: "task has no source document"}
		}

		var foundDoc *tuiclient.Document
		for i := range documents {
			if documents[i].ID == task.DocumentID {
				foundDoc = &documents[i]
				break
			}
		}

		if foundDoc == nil {
			return openerResultMsg{message: "source document not found in project"}
		}

		openURL, errMsg := resolveDocURL(foundDoc.Ref, *foundDoc, m.project.Repo)
		if errMsg != "" {
			return openerResultMsg{message: errMsg}
		}

		if err := m.urlOpener(openURL); err != nil {
			return openerResultMsg{message: fmt.Sprintf("open failed: %v", err)}
		}
		return openerResultMsg{}
	}
}

// openDesignDocCmd creates a command that opens the project's base design document in the browser.
func (m *BoardModel) openDesignDocCmd(documents []tuiclient.Document) tea.Cmd {
	return func() tea.Msg {
		var foundDoc *tuiclient.Document
		for i := range documents {
			if documents[i].Kind == "design" {
				foundDoc = &documents[i]
				break
			}
		}

		if foundDoc == nil {
			return openerResultMsg{message: "no design document found for this project"}
		}

		openURL, errMsg := resolveDocURL(foundDoc.Ref, *foundDoc, m.project.Repo)
		if errMsg != "" {
			return openerResultMsg{message: errMsg}
		}

		if err := m.urlOpener(openURL); err != nil {
			return openerResultMsg{message: fmt.Sprintf("open failed: %v", err)}
		}
		return openerResultMsg{}
	}
}

// renderDetailView renders the full-screen detail view for the current task.
func (m *BoardModel) renderDetailView() string {
	task := m.detailTask
	var b strings.Builder

	// Header: title and state
	b.WriteString(fmt.Sprintf("Task: %s\n", task.Title))
	stateStr := fmt.Sprintf("State: %s", task.State)
	if task.Assignee != nil {
		stateStr += fmt.Sprintf("  Assignee: %s", *task.Assignee)
	}
	b.WriteString(stateStr + "\n")

	// Lease countdown
	if task.LeaseExpiresAt != nil {
		b.WriteString(fmt.Sprintf("Lease: %s\n", m.formatLeaseCountdown(*task.LeaseExpiresAt)))
	}

	// Timestamps
	b.WriteString(fmt.Sprintf("Created: %s\n", m.formatAbsTime(task.CreatedAt)))
	b.WriteString(fmt.Sprintf("Updated: %s\n", m.formatAbsTime(task.UpdatedAt)))

	// Dependencies resolved to titles
	if len(task.DependsOn) > 0 {
		b.WriteString("Depends on:\n")
		for _, depID := range task.DependsOn {
			title := m.resolveTaskTitle(depID)
			shortID := depID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			b.WriteString(fmt.Sprintf("  - %s (%s)\n", title, shortID))
		}
	}

	// Links
	if len(task.Links) > 0 {
		b.WriteString("Links:\n")
		for _, link := range task.Links {
			b.WriteString(fmt.Sprintf("  [%s] %s\n", link.Kind, link.Value))
		}
	}

	// Result (wrapped to the view width so long summaries are fully readable, not truncated)
	if task.Result != nil && *task.Result != "" {
		b.WriteString("Result:\n")
		b.WriteString(wrapText(*task.Result, m.width))
		b.WriteString("\n")
	}

	// Status/opener message
	if m.detailMessage != "" {
		b.WriteString(fmt.Sprintf("» %s\n", m.detailMessage))
	}

	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	// Scrollable spec viewport
	b.WriteString(m.detailViewport.View())

	return b.String()
}

// wrapText wraps s to the given width on word boundaries, preserving existing newlines.
// Used for free-text fields (like a task result) that would otherwise overrun the terminal.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		col := 0
		for j, word := range strings.Fields(line) {
			wlen := len(word)
			if j > 0 {
				if col+1+wlen > width {
					out.WriteByte('\n')
					col = 0
				} else {
					out.WriteByte(' ')
					col++
				}
			}
			out.WriteString(word)
			col += wlen
		}
	}
	return out.String()
}

// resolveTaskTitle looks up a task ID in the board's full task list and returns the title.
// Falls back to the truncated ID if not found.
func (m *BoardModel) resolveTaskTitle(taskID string) string {
	for _, tasks := range m.tasks {
		for _, t := range tasks {
			if t.ID == taskID {
				return t.Title
			}
		}
	}
	return taskID
}

// formatAbsTime formats a timestamp as a human-readable absolute time.
func (m *BoardModel) formatAbsTime(timestamp string) string {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return timestamp
	}
	return t.Format("2006-01-02 15:04:05 UTC")
}

// initDetailViewport initialises the spec viewport for the detail view.
// Call this whenever the detail mode is entered or the window is resized.
func (m *BoardModel) initDetailViewport(specContent string) {
	// Reserve lines for header fields + separator + help bar.
	// The exact count varies with task data; a conservative fixed offset is fine.
	const reservedLines = 14
	vpHeight := m.height - reservedLines
	if vpHeight < 3 {
		vpHeight = 3
	}

	vp := viewport.New(m.width, vpHeight)
	vp.SetContent(specContent)
	// Preserve scroll position on resize.
	if m.detailViewport.YOffset > 0 {
		vp.YOffset = m.detailViewport.YOffset
	}
	m.detailViewport = vp
}

// renderDetailHelpBar returns the help bar text appropriate for detail view.
func (m *BoardModel) renderDetailHelpBar() string {
	base := "esc back   ↑/↓/pgup/pgdn scroll spec   o open PR   s source doc   d design doc"
	if m.detailTask.State == stateReview {
		base += "   a approve   x reject"
	}
	if m.detailTask.State == stateApproved {
		base += "   m merge & complete"
	}
	return base
}

// defaultURLOpener is the production opener. It tries $BROWSER, then platform open commands.
func defaultURLOpener(rawURL string) error {
	// Try $BROWSER first.
	if browserEnv := os.Getenv("BROWSER"); browserEnv != "" {
		return exec.Command(browserEnv, rawURL).Start()
	}

	// macOS: "open" command.
	if _, err := exec.LookPath("open"); err == nil {
		return exec.Command("open", rawURL).Start()
	}

	// Linux: xdg-open.
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", rawURL).Start()
	}

	return fmt.Errorf("no browser found: set $BROWSER or install xdg-open")
}

// defaultGHMerger merges a PR via `gh pr merge <prURL>`.
// Returns an error if gh is not found or the merge fails.
func defaultGHMerger(prURL string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh command not found: install GitHub CLI (https://cli.github.com)")
	}

	// Run gh pr merge with the PR URL.
	// The PR URL is expected to be in the form "owner/repo#PR_NUMBER" or a full GitHub URL.
	cmd := exec.Command("gh", "pr", "merge", prURL)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh pr merge failed: %w", err)
	}
	return nil
}
